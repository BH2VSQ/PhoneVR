package com.vsq.phonvrstream.network

import android.content.Context
import android.view.Surface
import com.vsq.phonvrstream.streaming.VideoStreamReceiver
import com.vsq.phonvrstream.streaming.VideoStreamStats
import android.content.Intent
import android.net.wifi.WifiManager
import android.os.BatteryManager
import android.os.Handler
import android.os.Looper
import android.os.Process
import android.util.Log
import org.json.JSONArray
import org.json.JSONException
import org.json.JSONObject
import java.io.BufferedWriter
import java.io.IOException
import java.io.OutputStreamWriter
import java.net.DatagramPacket
import java.net.DatagramSocket
import java.net.InetAddress
import java.net.Inet4Address
import java.net.InterfaceAddress
import java.net.NetworkInterface
import java.net.InetSocketAddress
import java.net.Socket
import java.net.SocketTimeoutException
import java.nio.charset.StandardCharsets
import java.util.concurrent.Executors
import java.util.concurrent.atomic.AtomicBoolean
import java.util.concurrent.atomic.AtomicLong
import java.util.concurrent.atomic.AtomicReference

/** LAN and USB-AOA connection management with low-latency pose and H.264 transports. */
class NetworkClient(
    context: Context,
    private val statusListener: (message: String, connected: Boolean) -> Unit,
    private val currentDisplaySettings: () -> JSONObject = { JSONObject() },
    private val applyRemoteDisplaySettings: (JSONObject) -> JSONObject = { it },
    private val currentOpticalPresets: () -> JSONArray = { JSONArray() },
    private val applyRemoteOpticalPresets: (JSONArray) -> JSONArray = { it },
    private val applyRemoteOpticalPreset: (String) -> JSONObject? = { null },
    private val applyRemoteDeviceAction: (String) -> Unit = {},
    private val currentMediaCapabilities: () -> JSONArray = { JSONArray() },
    private val videoStatsListener: (VideoStreamStats) -> Unit = {},
) {
    private val appContext = context.applicationContext
    private val mainHandler = Handler(Looper.getMainLooper())
    private val discoveryExecutor = Executors.newSingleThreadExecutor()
    private val sessionExecutor = Executors.newSingleThreadExecutor()
    // UI callbacks only enqueue control replies. All socket writes happen on this
    // dedicated worker, never on Android's main thread.
    private val controlWriteExecutor = Executors.newSingleThreadExecutor()
    // Head tracking is latency-sensitive but lightweight. Give the dedicated
    // sender display-thread scheduling priority so UI/GC work is less likely to
    // leave hundreds of milliseconds of stale UDP samples queued.
    private val poseExecutor = Executors.newSingleThreadExecutor { task ->
        Thread({
            try {
                Process.setThreadPriority(Process.THREAD_PRIORITY_DISPLAY)
            } catch (_: RuntimeException) {
                // Continue with the default priority if the platform denies it.
            }
            task.run()
        }, "PhoneVR-Pose-TX")
    }
    private val running = AtomicBoolean(false)
    private val sessionGeneration = AtomicLong(0L)
    private val latestPose = AtomicReference<HeadPose?>(null)
    private val socketLock = Any()
    private val sessionControlLock = Any()
    private val controlWriteLock = Any()

    @Volatile private var controlSocket: Socket? = null
    @Volatile private var poseSocket: DatagramSocket? = null
    @Volatile private var usbSession: UsbAccessorySession? = null
    @Volatile private var usbVideoForwardSocket: DatagramSocket? = null
    @Volatile private var usbVideoForwardPort: Int = -1
    @Volatile private var activeControlWriter: BufferedWriter? = null
    @Volatile private var activeControlGeneration: Long = -1L
    private data class VideoSessionConfig(val host: String, val port: Int, val width: Int, val height: Int, val fps: Int, val decoderName: String, val generation: Long, val transport: String)
    private val videoLifecycleExecutor = Executors.newSingleThreadExecutor()
    private val videoLock = Any()
    @Volatile private var videoSurface: Surface? = null
    @Volatile private var videoConfig: VideoSessionConfig? = null
    @Volatile private var videoReceiver: VideoStreamReceiver? = null
    @Volatile private var videoStatusText: String = "视频尚未协商"
    private val firstRenderedVideoGeneration = AtomicLong(-1L)
    private val renderedFramesSinceStats = AtomicLong(0L)
    private val lastVideoStatsNs = AtomicLong(0L)
    @Volatile private var latestVideoStats = VideoStreamStats()

    /** Called by the GL renderer whenever its SurfaceTexture-backed Surface is created or recreated. */
    fun attachVideoSurface(surface: Surface) {
        videoSurface = surface
        restartVideoReceiver()
    }

    private fun configureVideoStream(generation: Long, host: String, welcome: JSONObject) {
        if (!welcome.optBoolean("videoEnabled", false) ||
            welcome.optString("videoCodec", "") != "h264" ||
            welcome.optString("videoMimeType", "") != "video/avc") {
            videoConfig = null
            firstRenderedVideoGeneration.set(-1L)
            val reason = welcome.optString("videoReason", "当前未协商到兼容的 H.264 解码器")
            videoStatusText = "视频未启用：$reason"
            publishVideoStats(VideoStreamStats(state = "视频未启用"))
            stopVideoReceiverAsync(generation)
            return
        }
        val transport = welcome.optString("videoTransport", "udp").lowercase()
        val useUsb = transport == "usb"
        val config = VideoSessionConfig(
            host = if (useUsb) "127.0.0.1" else host,
            port = if (useUsb) 0 else welcome.optInt("videoPort", DEFAULT_VIDEO_PORT),
            width = welcome.optInt("videoWidth", DEFAULT_VIDEO_WIDTH),
            height = welcome.optInt("videoHeight", DEFAULT_VIDEO_HEIGHT),
            fps = welcome.optInt("videoFps", DEFAULT_VIDEO_FPS),
            decoderName = welcome.optString("videoDecoderName", "").trim(),
            generation = generation,
            transport = if (useUsb) "usb" else "udp",
        )
        if (config.width <= 0 || config.height <= 0 || config.fps !in 1..120 ||
            (config.transport == "udp" && config.port !in 1..65535) || config.decoderName.isBlank()) {
            videoConfig = null
            videoStatusText = "PC 返回的视频流参数无效"
            publishVideoStats(VideoStreamStats(state = "视频参数无效"))
            stopVideoReceiverAsync(generation)
            return
        }
        videoConfig = config
        firstRenderedVideoGeneration.set(-1L)
        renderedFramesSinceStats.set(0L)
        videoStatusText = "H.264 已协商，等待视频接收器启动"
        publishVideoStats(VideoStreamStats(state = "等待视频"))
        restartVideoReceiver()
    }

    private fun restartVideoReceiver() {
        videoLifecycleExecutor.execute {
            synchronized(videoLock) {
                videoReceiver?.stop()
                videoReceiver = null
                usbVideoForwardSocket?.close()
                usbVideoForwardSocket = null
                usbVideoForwardPort = -1
                val config = videoConfig ?: return@synchronized
                if (!isSessionActive(config.generation)) return@synchronized
                val surface = videoSurface ?: return@synchronized
                try {
                    val receiver = VideoStreamReceiver(
                        config.host, config.port, config.width, config.height, config.fps, config.decoderName, surface,
                        transportLabel = if (config.transport == "usb") "USB Bulk" else "UDP",
                        onStatus = { status -> updateVideoStatus(config.generation, status) },
                        onStats = { stats -> updateVideoStats(config.generation, stats) },
                    )
                    receiver.start()
                    videoReceiver = receiver
                    if (config.transport == "usb") {
                        val forwarder = DatagramSocket()
                        usbVideoForwardSocket?.close()
                        usbVideoForwardSocket = forwarder
                        usbVideoForwardPort = receiver.localPort
                        videoStatusText = "USB Bulk 视频通道已启动，等待 PC 首帧"
                    }
                } catch (error: Throwable) {
                    updateVideoStatus(config.generation, "视频接收启动失败：${error.message ?: error.javaClass.simpleName}")
                }
            }
        }
    }

    private fun forwardUsbVideoPacket(payload: ByteArray) {
        val socket = usbVideoForwardSocket
            ?: throw IllegalStateException("USB video loopback receiver is not ready")
        val port = usbVideoForwardPort
        check(!socket.isClosed && port in 1..65535 && payload.isNotEmpty()) {
            "USB video loopback receiver is unavailable"
        }
        // This callback now runs on PhoneVR-AOA-VideoDispatch, never on the
        // accessory Bulk IN reader. Let failures reach the dispatcher so its
        // callback-failure counter reflects packets that were not forwarded.
        socket.send(DatagramPacket(payload, payload.size, InetAddress.getByName("127.0.0.1"), port))
    }

    private fun updateVideoStatus(generation: Long, status: String) {
        if (!isSessionActive(generation)) return
        videoStatusText = status
    }

    private fun updateVideoStats(generation: Long, stats: VideoStreamStats) {
        if (!isSessionActive(generation)) return
        val now = System.nanoTime()
        val previous = lastVideoStatsNs.getAndSet(now)
        val elapsedSeconds = if (previous > 0L) ((now - previous).coerceAtLeast(1L) / 1_000_000_000.0) else 1.0
        val displayFps = renderedFramesSinceStats.getAndSet(0L) / elapsedSeconds
        val combined = if (stats.decodeFps >= 1.0 && displayFps < 1.0 && stats.lastDecodedAgeMs in 0..1_500L) {
            stats.copy(state = "显示停滞", displayFps = displayFps)
        } else {
            stats.copy(displayFps = displayFps)
        }
        latestVideoStats = combined
        mainHandler.post { videoStatsListener(combined) }
    }

    private fun publishVideoStats(stats: VideoStreamStats) {
        lastVideoStatsNs.set(System.nanoTime())
        renderedFramesSinceStats.set(0L)
        latestVideoStats = stats
        mainHandler.post { videoStatsListener(stats) }
    }

    /** Called by the GL renderer after SurfaceTexture accepts a newly decoded frame. */
    fun reportVideoFrameRendered() {
        val config = videoConfig ?: return
        if (!isSessionActive(config.generation)) return
        renderedFramesSinceStats.incrementAndGet()
        if (firstRenderedVideoGeneration.compareAndSet(-1L, config.generation)) {
            videoStatusText = "SteamVR 双目画面已到达显示纹理"
        }
    }

    private fun stopVideoReceiverAsync(generation: Long? = null) {
        videoLifecycleExecutor.execute {
            synchronized(videoLock) {
                val currentGeneration = videoConfig?.generation
                if (generation != null && currentGeneration != null && currentGeneration != generation) return@synchronized
                videoReceiver?.stop()
                videoReceiver = null
                usbVideoForwardSocket?.close()
                usbVideoForwardSocket = null
                usbVideoForwardPort = -1
                if (generation == null || videoConfig?.generation == null || videoConfig?.generation == generation) {
                    videoConfig = null
                    firstRenderedVideoGeneration.set(-1L)
                    videoStatusText = "视频已停止"
                }
            }
        }
    }

    fun publishDisplaySettings(settings: JSONObject) {
        val writer = activeControlWriter ?: return
        val generation = activeControlGeneration
        if (!isSessionActive(generation)) return
        queueControlMessage(writer, generation,
            JSONObject().put("type", "display_settings").put("settings", settings),
            "显示参数同步失败")
    }

    private fun queueControlMessage(
        writer: BufferedWriter,
        generation: Long,
        message: JSONObject,
        failurePrefix: String = "控制消息发送失败",
    ) {
        runCatching {
            controlWriteExecutor.execute {
                if (!isSessionActive(generation)) return@execute
                try {
                    writeControlMessage(writer, message)
                } catch (error: Exception) {
                    if (isSessionActive(generation)) {
                        postStatus("$failurePrefix：${error.message ?: error.javaClass.simpleName}", true)
                    }
                }
            }
        }.onFailure { error ->
            if (isSessionActive(generation)) postStatus("$failurePrefix：${error.message ?: error.javaClass.simpleName}", true)
        }
    }



    private fun isCharging(): Boolean {
        return try {
            val battery = appContext.registerReceiver(null, android.content.IntentFilter(Intent.ACTION_BATTERY_CHANGED))
            val status = battery?.getIntExtra(BatteryManager.EXTRA_STATUS, -1) ?: -1
            status == BatteryManager.BATTERY_STATUS_CHARGING || status == BatteryManager.BATTERY_STATUS_FULL
        } catch (_: Exception) { false }
    }

    private fun batteryLevel(): Int? {
        return try {
            val manager = appContext.getSystemService(Context.BATTERY_SERVICE) as? BatteryManager
            manager?.getIntProperty(BatteryManager.BATTERY_PROPERTY_CAPACITY)?.takeIf { it in 0..100 }
        } catch (_: Exception) { null }
    }

    fun updatePose(pose: HeadPose) {
        // Sensor callbacks only replace the latest value. They never perform I/O.
        latestPose.set(pose)
    }

    private data class DiscoveryTarget(
        val interfaceName: String,
        val localAddress: Inet4Address,
        val broadcastAddress: Inet4Address,
    )

    private data class DiscoverySocket(
        val target: DiscoveryTarget,
        val socket: DatagramSocket,
    )

    fun discover(onResults: (List<DiscoveredHost>) -> Unit) {
        discoveryExecutor.execute {
            val found = linkedMapOf<String, DiscoveredHost>()
            var multicastLock: WifiManager.MulticastLock? = null
            val sockets = mutableListOf<DiscoverySocket>()
            try {
                val wifiManager = appContext.getSystemService(Context.WIFI_SERVICE) as? WifiManager
                if (wifiManager != null) {
                    multicastLock = wifiManager.createMulticastLock("PhoneVR-LAN-Discovery").apply {
                        setReferenceCounted(false)
                        acquire()
                    }
                }

                val targets = enumerateDiscoveryTargets()
                val request = DISCOVERY_REQUEST.toByteArray(StandardCharsets.UTF_8)
                for (target in targets) {
                    try {
                        val socket = DatagramSocket(null).apply {
                            reuseAddress = false
                            broadcast = true
                            bind(InetSocketAddress(target.localAddress, 0))
                            soTimeout = DISCOVERY_READ_TIMEOUT_MS
                        }
                        socket.send(DatagramPacket(
                            request, request.size, target.broadcastAddress, DISCOVERY_PORT,
                        ))
                        sockets += DiscoverySocket(target, socket)
                    } catch (error: Exception) {
                        postStatus("接口 ${target.interfaceName} 搜索失败：${error.message ?: error.javaClass.simpleName}", false)
                    }
                }

                // Preserve discovery on VPNs or unusual networks that do not
                // expose an IPv4 directed-broadcast address through NetworkInterface.
                if (sockets.isEmpty()) {
                    DatagramSocket(null).use { socket ->
                        socket.reuseAddress = true
                        socket.broadcast = true
                        socket.bind(InetSocketAddress(0))
                        socket.soTimeout = DISCOVERY_READ_TIMEOUT_MS
                        socket.send(DatagramPacket(
                            request, request.size, InetAddress.getByName("255.255.255.255"), DISCOVERY_PORT,
                        ))
                        val deadline = System.currentTimeMillis() + DISCOVERY_WINDOW_MS
                        readDiscoveryResponses(socket, deadline, found)
                    }
                } else {
                    val deadline = System.currentTimeMillis() + DISCOVERY_WINDOW_MS
                    // Poll each per-interface socket with a short timeout so a
                    // slow or disconnected network cannot block other interfaces.
                    while (System.currentTimeMillis() < deadline && sockets.isNotEmpty()) {
                        var receivedAny = false
                        val remaining = (deadline - System.currentTimeMillis()).coerceAtLeast(1L)
                        val timeout = minOf(80L, remaining).toInt().coerceAtLeast(10)
                        for (entry in sockets) {
                            try {
                                entry.socket.soTimeout = timeout
                                val buffer = ByteArray(2048)
                                val packet = DatagramPacket(buffer, buffer.size)
                                entry.socket.receive(packet)
                                receivedAny = true
                                parseDiscoveryResponse(packet, found)
                            } catch (_: SocketTimeoutException) {
                                // Poll the next active interface.
                            }
                        }
                        if (!receivedAny && System.currentTimeMillis() >= deadline) break
                    }
                }
            } catch (error: Exception) {
                postStatus("搜索 PC 失败：${error.message ?: error.javaClass.simpleName}", false)
            } finally {
                sockets.forEach { runCatching { it.socket.close() } }
                try {
                    if (multicastLock?.isHeld == true) multicastLock?.release()
                } catch (_: Exception) { }
            }
            val sorted = found.values.sortedWith(compareBy<DiscoveredHost> { it.name }.thenBy { it.address })
            mainHandler.post { onResults(sorted) }
        }
    }

    private fun readDiscoveryResponses(
        socket: DatagramSocket,
        deadline: Long,
        found: MutableMap<String, DiscoveredHost>,
    ) {
        while (System.currentTimeMillis() < deadline) {
            try {
                val buffer = ByteArray(2048)
                val packet = DatagramPacket(buffer, buffer.size)
                socket.soTimeout = DISCOVERY_READ_TIMEOUT_MS
                socket.receive(packet)
                parseDiscoveryResponse(packet, found)
            } catch (_: SocketTimeoutException) {
                // Keep listening until the bounded discovery window expires.
            }
        }
    }

    private fun parseDiscoveryResponse(
        packet: DatagramPacket,
        found: MutableMap<String, DiscoveredHost>,
    ) {
        val response = String(packet.data, packet.offset, packet.length, StandardCharsets.UTF_8)
        if (!response.startsWith(DISCOVERY_RESPONSE_PREFIX)) return
        val json = JSONObject(response.removePrefix(DISCOVERY_RESPONSE_PREFIX))
        val address = packet.address.hostAddress ?: return
        val host = DiscoveredHost(
            address = address,
            controlPort = json.optInt("controlPort", DEFAULT_CONTROL_PORT),
            name = json.optString("name", "PhoneVR Host"),
        )
        found[address] = host
    }

    private fun enumerateDiscoveryTargets(): List<DiscoveryTarget> {
        val result = mutableListOf<DiscoveryTarget>()
        val interfaces = runCatching { NetworkInterface.getNetworkInterfaces()?.toList().orEmpty() }.getOrDefault(emptyList())
        for (networkInterface in interfaces) {
            val active = runCatching { networkInterface.isUp && !networkInterface.isLoopback }.getOrDefault(false)
            if (!active) continue
            val name = networkInterface.name ?: networkInterface.displayName ?: "unknown"
            for (entry: InterfaceAddress in networkInterface.interfaceAddresses) {
                val local = entry.address as? Inet4Address ?: continue
                var broadcast = entry.broadcast as? Inet4Address
                if (broadcast == null) broadcast = calculateBroadcast(local, entry) ?: continue
                result += DiscoveryTarget(name, local, broadcast)
            }
        }
        return result.distinctBy { "${it.interfaceName}|${it.localAddress.hostAddress}|${it.broadcastAddress.hostAddress}" }
    }

    private fun calculateBroadcast(local: Inet4Address, entry: InterfaceAddress): Inet4Address? {
        val prefix = entry.networkPrefixLength.toInt().coerceIn(0, 32)
        if (prefix !in 1..30) return null // /31 and /32 links do not have a usable broadcast host range.
        val address = local.address
        val result = ByteArray(4)
        for (index in 0 until 4) {
            val remainingBits = (prefix - index * 8).coerceIn(0, 8)
            val mask = if (remainingBits == 0) 0 else (0xFF shl (8 - remainingBits)) and 0xFF
            val hostMask = mask.inv() and 0xFF
            result[index] = ((address[index].toInt() and 0xFF) or hostMask).toByte()
        }
        return runCatching { InetAddress.getByAddress(result) as Inet4Address }.getOrNull()
    }


    /**
     * Starts a session-scoped connection loop. A short Wi-Fi interruption or a
     * missed heartbeat causes bounded reconnect attempts rather than leaving a
     * stale TCP session marked as connected. Generation IDs prevent cleanup
     * from an older session from stopping a newer user-requested connection.
     */
    fun connect(address: String, controlPort: Int = DEFAULT_CONTROL_PORT, deviceName: String = "Android") {
        val normalizedAddress = address.trim()
        require(normalizedAddress.isNotEmpty()) { "PC 地址不能为空" }

        val generation = synchronized(sessionControlLock) {
            val next = sessionGeneration.incrementAndGet()
            running.set(true)
            closeCurrentSockets()
            next
        }
        sessionExecutor.execute {
            connectionLoop(generation, normalizedAddress, controlPort, deviceName)
        }
    }

    /** Connect over the native Android Open Accessory (AOA) Bulk interface. */
    fun connectUsb(deviceName: String = "Android") {
        val generation = synchronized(sessionControlLock) {
            val next = sessionGeneration.incrementAndGet()
            running.set(true)
            closeCurrentSockets()
            next
        }
        sessionExecutor.execute { usbConnectionLoop(generation, deviceName) }
    }

    private fun usbConnectionLoop(generation: Long, deviceName: String) {
        var retryDelayMs = 1000L
        val mediaCapabilities = runCatching { currentMediaCapabilities() }.getOrDefault(JSONArray())
        while (isSessionActive(generation)) {
            var session: UsbAccessorySession? = null
            try {
                postStatus("等待 PC Host 切换到 Android Accessory Mode（AOA）…", false)
                val openedSession = UsbAccessorySession.openFirstAccessory(appContext)
                session = openedSession
                openedSession.videoPacketListener = { payload -> forwardUsbVideoPacket(payload) }
                synchronized(socketLock) {
                    if (isSessionActive(generation)) usbSession = openedSession else openedSession.close()
                }
                if (!isSessionActive(generation)) return

                val reader = ControlLineReader(openedSession.controlInput)
                val writer = BufferedWriter(OutputStreamWriter(openedSession.controlOutput, StandardCharsets.UTF_8))
                val hello = JSONObject()
                    .put("type", "hello")
                    .put("protocol", PROTOCOL_VERSION)
                    .put("deviceName", deviceName.take(80))
                    .put("client", CLIENT_VERSION)
                    .put("transport", "usb-aoa-bulk")
                    .put("mediaCapabilities", mediaCapabilities)
                writeControlMessage(writer, hello)

                val handshakeStartedNs = System.nanoTime()
                val welcomeLine = reader.readLine {
                    if (System.nanoTime() - handshakeStartedNs > USB_HANDSHAKE_TIMEOUT_NS) {
                        throw SocketTimeoutException("PC USB welcome handshake timed out")
                    }
                }
                if (!isSessionActive(generation)) return
                val welcome = parseControlJSONObject(
                    welcomeLine ?: error("PC 在 USB 配对前关闭了连接"),
                    "USB welcome",
                )
                check(welcome.optString("type") == "welcome") {
                    welcome.optString("message", "PC 返回了无法识别的 USB 响应")
                }
                if (welcome.optInt("protocol") != PROTOCOL_VERSION) {
                    throw ProtocolMismatchException("PC USB 通信协议版本不兼容")
                }
                configureVideoStream(generation, "127.0.0.1", welcome)
                synchronized(socketLock) {
                    activeControlWriter = writer
                    activeControlGeneration = generation
                }
                writeControlMessage(writer,
                    JSONObject().put("type", "display_settings").put("settings", currentDisplaySettings()))
                poseExecutor.execute { sendUsbPoseLoop(generation, openedSession) }
                postStatus("已通过 USB AOA Bulk 连接 PC · 姿态与视频共用 USB 通道", true)
                retryDelayMs = INITIAL_RECONNECT_DELAY_MS
                runControlLoop(generation, { !openedSession.isClosed }, reader, writer)
                if (!isSessionActive(generation)) return
                throw SocketTimeoutException("USB accessory control channel closed")
            } catch (error: ProtocolMismatchException) {
                if (isSessionActive(generation)) {
                    postStatus("USB 连接失败：${error.message}", false)
                    running.set(false)
                }
                return
            } catch (error: Exception) {
                if (!isSessionActive(generation)) return
                postStatus("USB 连接等待/中断：${error.message ?: error.javaClass.simpleName}", false)
            } finally {
                session?.close()
                synchronized(socketLock) {
                    if (usbSession === session) usbSession = null
                    if (activeControlGeneration == generation) {
                        activeControlWriter = null
                        activeControlGeneration = -1L
                    }
                }
                stopVideoReceiverAsync(generation)
            }
            if (!isSessionActive(generation)) return
            sleepBeforeRetry(retryDelayMs, generation)
            retryDelayMs = (retryDelayMs * 2L).coerceAtMost(MAX_RECONNECT_DELAY_MS)
        }
    }

    private fun sendUsbPoseLoop(generation: Long, session: UsbAccessorySession) {
        var sequence = 0
        var lastErrorReportAtNs = 0L
        val throttle = PoseSendThrottle(MAX_POSE_SEND_HZ)
        while (isSessionActive(generation) && !session.isClosed) {
            val pose = latestPose.get()
            if (pose == null) {
                sleepNanos(POSE_IDLE_SLEEP_NS)
                continue
            }
            val nowNs = System.nanoTime()
            if (throttle.tryAcquire(pose.timestampNs, nowNs)) {
                try {
                    session.sendPose(PosePacket.encode(sequence, pose))
                    sequence++
                } catch (error: Exception) {
                    if (!isSessionActive(generation) || session.isClosed) return
                    if (nowNs - lastErrorReportAtNs >= POSE_ERROR_REPORT_INTERVAL_NS) {
                        postStatus("USB 姿态 Bulk 通道暂时不可用：${error.message}", true)
                        lastErrorReportAtNs = nowNs
                    }
                }
            }
            val remainingNs = throttle.nanosUntilNextAllowed(System.nanoTime())
            sleepNanos(if (remainingNs > 0L) remainingNs else POSE_IDLE_SLEEP_NS)
        }
    }

    fun disconnect() {
        synchronized(sessionControlLock) {
            sessionGeneration.incrementAndGet()
            running.set(false)
            closeCurrentSockets()
        }
        stopVideoReceiverAsync()
        publishVideoStats(VideoStreamStats(state = "等待 PC"))
        postStatus("已断开 PC", false)
    }

    fun shutdown() {
        synchronized(sessionControlLock) {
            sessionGeneration.incrementAndGet()
            running.set(false)
            closeCurrentSockets()
        }
        stopVideoReceiverAsync()
        discoveryExecutor.shutdownNow()
        sessionExecutor.shutdownNow()
        poseExecutor.shutdownNow()
        controlWriteExecutor.shutdownNow()
        videoLifecycleExecutor.shutdown()
    }

    private fun connectionLoop(generation: Long, address: String, port: Int, deviceName: String) {
        var retryDelayMs = INITIAL_RECONNECT_DELAY_MS
        var firstAttempt = true
        // Enumerate MediaCodec once per user-requested session, off the UI
        // thread. Retries reuse this bounded, immutable capability snapshot.
        val mediaCapabilities = runCatching { currentMediaCapabilities() }.getOrDefault(JSONArray())

        while (isSessionActive(generation)) {
            var tcp: Socket? = null
            var udp: DatagramSocket? = null
            try {
                val attemptMessage = if (firstAttempt) {
                    "正在连接 $address:$port …"
                } else {
                    "正在重新连接 $address:$port …"
                }
                postStatus(attemptMessage, false)

                val openedTcp = Socket()
                tcp = openedTcp
                synchronized(socketLock) {
                    if (isSessionActive(generation)) controlSocket = openedTcp else openedTcp.close()
                }
                if (!isSessionActive(generation)) return

                openedTcp.tcpNoDelay = true
                openedTcp.keepAlive = true
                openedTcp.connect(InetSocketAddress(address, port), CONNECT_TIMEOUT_MS)
                openedTcp.soTimeout = HANDSHAKE_TIMEOUT_MS
                val reader = ControlLineReader(openedTcp.getInputStream())
                val writer = BufferedWriter(OutputStreamWriter(openedTcp.getOutputStream(), StandardCharsets.UTF_8))

                val hello = JSONObject()
                    .put("type", "hello")
                    .put("protocol", PROTOCOL_VERSION)
                    .put("deviceName", deviceName.take(80))
                    .put("client", CLIENT_VERSION)
                    .put("mediaCapabilities", mediaCapabilities)
                writer.write(hello.toString())
                writer.newLine()
                writer.flush()

                val welcomeLine = reader.readLine {
                    throw SocketTimeoutException("PC welcome handshake timed out")
                } ?: error("PC 在配对前关闭了连接")
                val welcome = parseControlJSONObject(welcomeLine, "LAN welcome")
                check(welcome.optString("type") == "welcome") {
                    welcome.optString("message", "PC 返回了无法识别的响应")
                }
                if (welcome.optInt("protocol") != PROTOCOL_VERSION) {
                    throw ProtocolMismatchException("PC 通信协议版本不兼容")
                }
                configureVideoStream(generation, address, welcome)

                synchronized(socketLock) {
                    activeControlWriter = writer
                    activeControlGeneration = generation
                }
                writeControlMessage(
                    writer,
                    JSONObject().put("type", "display_settings").put("settings", currentDisplaySettings()),
                )

                val posePort = welcome.optInt("posePort", DEFAULT_POSE_PORT)
                val serverName = welcome.optString("serverName", address)
                val destination = InetAddress.getByName(address)
                val openedUdp = DatagramSocket().apply {
                    sendBufferSize = UDP_SEND_BUFFER_BYTES
                }
                udp = openedUdp
                synchronized(socketLock) {
                    if (isSessionActive(generation)) poseSocket = openedUdp else openedUdp.close()
                }
                if (!isSessionActive(generation)) return

                openedTcp.soTimeout = CONTROL_READ_TIMEOUT_MS
                poseExecutor.execute {
                    sendPoseLoop(generation, openedUdp, destination, posePort)
                }
                postStatus("已连接 $serverName，姿态发送正常 · $videoStatusText", true)
                firstAttempt = false
                retryDelayMs = INITIAL_RECONNECT_DELAY_MS

                runControlLoop(generation, { !openedTcp.isClosed }, reader, writer)
                if (!isSessionActive(generation)) return
                throw SocketTimeoutException("控制连接已停止")
            } catch (error: ProtocolMismatchException) {
                if (isSessionActive(generation)) {
                    postStatus("连接失败：${error.message}", false)
                    running.set(false)
                }
                return
            } catch (error: Exception) {
                if (!isSessionActive(generation)) return
                postStatus(
                    "连接中断：${error.message ?: error.javaClass.simpleName}，${retryDelayMs / 1000} 秒后重试",
                    false,
                )
            } finally {
                closeAttemptSockets(tcp, udp)
                stopVideoReceiverAsync(generation)
            }

            if (!isSessionActive(generation)) return
            sleepBeforeRetry(retryDelayMs, generation)
            retryDelayMs = (retryDelayMs * 2L).coerceAtMost(MAX_RECONNECT_DELAY_MS)
        }
    }

    private fun runControlLoop(
        generation: Long,
        transportOpen: () -> Boolean,
        reader: ControlLineReader,
        writer: BufferedWriter,
    ) {
        var lastHeartbeatNs = System.nanoTime() - HEARTBEAT_INTERVAL_NS
        var lastPongNs = System.nanoTime()

        fun serviceHeartbeat() {
            val nowNs = System.nanoTime()
            if (nowNs - lastHeartbeatNs >= HEARTBEAT_INTERVAL_NS) {
                writeControlMessage(
                    writer,
                    JSONObject().put("type", "ping").put("sentAtNs", nowNs)
                        .put("batteryLevel", batteryLevel()).put("charging", isCharging()),
                )
                lastHeartbeatNs = nowNs
            }
            if (nowNs - lastPongNs > HEARTBEAT_TIMEOUT_NS) {
                throw SocketTimeoutException("PC 心跳响应超时")
            }
        }

        while (isSessionActive(generation) && transportOpen()) {
            serviceHeartbeat()
            val line = reader.readLine { serviceHeartbeat() } ?: error("PC 已关闭控制连接")
            if (line.isBlank()) continue
            val message = parseControlJSONObject(line, "control")
            when (message.optString("type")) {
                    "pong" -> lastPongNs = System.nanoTime()
                    "error" -> postStatus("PC：${message.optString("message", "未知错误")}", true)
                    "get_optical_presets" -> {
                        // This request only reads an immutable, @Volatile snapshot and
                        // serializes JSON. Respond on the TCP control thread instead of
                        // posting to the UI thread: a busy GL/UI frame must not delay the
                        // Host's initial preset catalogue handshake.
                        val requestId = message.optString("requestId", "")
                        if (isSessionActive(generation)) {
                            try {
                                val presets = currentOpticalPresets()
                                writeControlMessage(
                                    writer,
                                    JSONObject().put("type", "optical_presets")
                                        .put("requestId", requestId)
                                        .put("presets", presets),
                                )
                                postStatus("已向 PC 同步 ${presets.length()} 个光学预设", true)
                            } catch (error: Exception) {
                                postStatus(
                                    "光学预设回传失败（请求 $requestId）：${error.message ?: error.javaClass.simpleName}",
                                    true,
                                )
                            }
                        }
                    }
                    "set_optical_presets" -> {
                        val requestId = message.optString("requestId", "")
                        val presets = message.optJSONArray("presets") ?: JSONArray()
                        mainHandler.post {
                            try {
                                val applied = applyRemoteOpticalPresets(presets)
                                if (isSessionActive(generation)) {
                                    queueControlMessage(
                                        writer, generation,
                                        JSONObject().put("type", "optical_presets_ack")
                                            .put("requestId", requestId).put("status", "applied")
                                            .put("presets", applied),
                                        "光学预设确认发送失败",
                                    )
                                    postStatus("PC 已同步光学预设", true)
                                }
                            } catch (error: Throwable) {
                                if (isSessionActive(generation)) {
                                    queueControlMessage(
                                        writer, generation,
                                        JSONObject().put("type", "optical_presets_ack")
                                            .put("requestId", requestId).put("status", "rejected")
                                            .put("message", error.message ?: error.javaClass.simpleName),
                                        "光学预设拒绝响应发送失败",
                                    )
                                }
                            }
                        }
                    }
                    "apply_optical_preset" -> {
                        val requestId = message.optString("requestId", "")
                        val presetId = message.optString("presetId", "")
                        mainHandler.post {
                            try {
                                val applied = applyRemoteOpticalPreset(presetId)
                                    ?: error("手机中不存在该光学预设：$presetId")
                                if (isSessionActive(generation)) {
                                    queueControlMessage(
                                        writer, generation,
                                        JSONObject().put("type", "display_settings_ack")
                                            .put("requestId", requestId).put("status", "applied")
                                            .put("settings", applied),
                                        "显示参数确认发送失败",
                                    )
                                    postStatus("已应用光学预设", true)
                                }
                            } catch (error: Throwable) {
                                if (isSessionActive(generation)) {
                                    queueControlMessage(
                                        writer, generation,
                                        JSONObject().put("type", "display_settings_ack")
                                            .put("requestId", requestId).put("status", "rejected")
                                            .put("message", error.message ?: error.javaClass.simpleName),
                                        "显示参数拒绝响应发送失败",
                                    )
                                }
                            }
                        }
                    }
                    "set_display_settings" -> {
                        val requestId = message.optString("requestId", "")
                        val settingsJson = message.optJSONObject("settings") ?: JSONObject()
                        mainHandler.post {
                            // Keep this transaction isolated from panel rebuilding. A remote
                            // update must never detach/recreate Android views while the user
                            // is interacting with the calibration overlay.
                            try {
                                val applied = applyRemoteDisplaySettings(settingsJson)
                                if (isSessionActive(generation)) {
                                    queueControlMessage(
                                        writer, generation,
                                        JSONObject()
                                            .put("type", "display_settings_ack")
                                            .put("requestId", requestId)
                                            .put("status", "applied")
                                            .put("settings", applied),
                                        "显示参数确认发送失败",
                                    )
                                    postStatus("PC 已更新手机显示参数", true)
                                }
                            } catch (error: Throwable) {
                                if (isSessionActive(generation)) {
                                    queueControlMessage(
                                        writer, generation,
                                        JSONObject()
                                            .put("type", "display_settings_ack")
                                            .put("requestId", requestId)
                                            .put("status", "rejected")
                                            .put("message", error.message ?: error.javaClass.simpleName),
                                        "显示参数拒绝响应发送失败",
                                    )
                                }
                            }
                        }
                    }
                    "device_action" -> {
                        val requestId = message.optString("requestId", "")
                        val action = message.optString("action", "")
                        mainHandler.post {
                            try {
                                if (action !in setOf("center_view")) {
                                    throw IllegalArgumentException("unsupported device action")
                                }
                                applyRemoteDeviceAction(action)
                                if (isSessionActive(generation)) {
                                    queueControlMessage(
                                        writer, generation,
                                        JSONObject().put("type", "device_action_ack")
                                            .put("requestId", requestId).put("status", "applied"),
                                        "设备操作确认发送失败",
                                    )
                                }
                            } catch (error: Throwable) {
                                Log.e(TAG, "Phone device action failed: $action", error)
                                if (isSessionActive(generation)) {
                                    queueControlMessage(
                                        writer, generation,
                                        JSONObject().put("type", "device_action_ack")
                                            .put("requestId", requestId).put("status", "rejected")
                                            .put("message", error.message ?: error.javaClass.simpleName),
                                        "设备操作拒绝响应发送失败",
                                    )
                                }
                            }
                        }
                    }
                }
        }
    }

    private fun parseControlJSONObject(line: String, source: String): JSONObject {
        try {
            return JSONObject(line)
        } catch (error: JSONException) {
            val preview = line.take(120)
                .replace("\\", "\\\\")
                .replace("\r", "\\r")
                .replace("\n", "\\n")
            Log.e(TAG, "Malformed $source JSON line: chars=${line.length}, preview=[$preview]", error)
            throw IOException("PC 返回了格式错误的 $source JSON（${line.length} 字符）", error)
        }
    }

    /** Latest-sample-wins UDP sender, paced to the 200 Hz IMU request rate. */
    private fun sendPoseLoop(
        generation: Long,
        socket: DatagramSocket,
        destination: InetAddress,
        port: Int,
    ) {
        var sequence = 0
        var lastErrorReportAtNs = 0L
        val throttle = PoseSendThrottle(MAX_POSE_SEND_HZ)

        while (isSessionActive(generation) && !socket.isClosed) {
            val pose = latestPose.get()
            if (pose == null) {
                sleepNanos(POSE_IDLE_SLEEP_NS)
                continue
            }

            val nowNs = System.nanoTime()
            if (throttle.tryAcquire(pose.timestampNs, nowNs)) {
                try {
                    val payload = PosePacket.encode(sequence, pose)
                    socket.send(DatagramPacket(payload, payload.size, destination, port))
                    sequence++
                } catch (error: Exception) {
                    if (!isSessionActive(generation) || socket.isClosed) return
                    if (nowNs - lastErrorReportAtNs >= POSE_ERROR_REPORT_INTERVAL_NS) {
                        postStatus("姿态 UDP 暂时不可用，控制连接保持运行：${error.message}", true)
                        lastErrorReportAtNs = nowNs
                    }
                }
            }

            val remainingNs = throttle.nanosUntilNextAllowed(System.nanoTime())
            sleepNanos(if (remainingNs > 0L) remainingNs else POSE_IDLE_SLEEP_NS)
        }
    }

    private fun isSessionActive(generation: Long): Boolean =
        running.get() && sessionGeneration.get() == generation

    private fun closeCurrentSockets() {
        synchronized(socketLock) {
            try { poseSocket?.close() } catch (_: Exception) { }
            try { controlSocket?.close() } catch (_: Exception) { }
            try { usbSession?.close() } catch (_: Exception) { }
            usbSession = null
            poseSocket = null
            controlSocket = null
            activeControlWriter = null
            activeControlGeneration = -1L
        }
    }

    private fun closeAttemptSockets(tcp: Socket?, udp: DatagramSocket?) {
        try { udp?.close() } catch (_: Exception) { }
        try { tcp?.close() } catch (_: Exception) { }
        synchronized(socketLock) {
            if (poseSocket === udp) poseSocket = null
            if (controlSocket === tcp) {
                controlSocket = null
                activeControlWriter = null
                activeControlGeneration = -1L
            }
        }
    }

    private fun writeControlMessage(writer: BufferedWriter, message: JSONObject) {
        synchronized(controlWriteLock) {
            writer.write(message.toString())
            writer.newLine()
            writer.flush()
        }
    }

    private fun sleepBeforeRetry(durationMs: Long, generation: Long) {
        var remaining = durationMs
        while (remaining > 0L && isSessionActive(generation)) {
            val slice = minOf(remaining, 100L)
            try {
                Thread.sleep(slice)
            } catch (_: InterruptedException) {
                Thread.currentThread().interrupt()
                return
            }
            remaining -= slice
        }
    }

    private fun sleepNanos(durationNs: Long) {
        if (durationNs <= 0L) return
        try {
            Thread.sleep(durationNs / 1_000_000L, (durationNs % 1_000_000L).toInt())
        } catch (_: InterruptedException) {
            Thread.currentThread().interrupt()
        }
    }

    private fun postStatus(message: String, connected: Boolean) {
        mainHandler.post { statusListener(message, connected) }
    }

    private class ProtocolMismatchException(message: String) : Exception(message)

    companion object {
        private const val TAG = "PhoneVRNetwork"
        const val DEFAULT_CONTROL_PORT = 48101
        const val DEFAULT_POSE_PORT = 48102
        const val DEFAULT_VIDEO_PORT = 48104
        const val DEFAULT_VIDEO_WIDTH = 1600
        const val DEFAULT_VIDEO_HEIGHT = 720
        const val DEFAULT_VIDEO_FPS = 60
        const val DISCOVERY_PORT = 48100
        private const val PROTOCOL_VERSION = 1
        private const val CLIENT_VERSION = "PhoneVRStream-0.5.11"
        private const val DISCOVERY_REQUEST = "PHONEVR_DISCOVER_V1"
        private const val DISCOVERY_RESPONSE_PREFIX = "PHONEVR_HOST_V1 "
        private const val DISCOVERY_WINDOW_MS = 1800L
        private const val DISCOVERY_READ_TIMEOUT_MS = 300
        private const val CONNECT_TIMEOUT_MS = 4000
        private const val HANDSHAKE_TIMEOUT_MS = 10000
        private const val USB_HANDSHAKE_TIMEOUT_NS = 30_000_000_000L
        private const val CONTROL_READ_TIMEOUT_MS = 1000
        private const val HEARTBEAT_INTERVAL_NS = 2_000_000_000L
        private const val HEARTBEAT_TIMEOUT_NS = 10_000_000_000L
        private const val MAX_POSE_SEND_HZ = 200
        private const val UDP_SEND_BUFFER_BYTES = 64 * 1024
        private const val POSE_ERROR_REPORT_INTERVAL_NS = 5_000_000_000L
        private const val POSE_IDLE_SLEEP_NS = 2_000_000L
        private const val INITIAL_RECONNECT_DELAY_MS = 1000L
        private const val MAX_RECONNECT_DELAY_MS = 8000L
    }
}
