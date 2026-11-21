package com.vsq.phonvrstream.network

import android.content.Context
import android.content.Intent
import android.net.wifi.WifiManager
import android.os.BatteryManager
import android.os.Handler
import android.os.Looper
import org.json.JSONArray
import org.json.JSONObject
import java.io.BufferedReader
import java.io.BufferedWriter
import java.io.InputStreamReader
import java.io.OutputStreamWriter
import java.net.DatagramPacket
import java.net.DatagramSocket
import java.net.InetAddress
import java.net.InetSocketAddress
import java.net.Socket
import java.net.SocketTimeoutException
import java.nio.charset.StandardCharsets
import java.util.concurrent.Executors
import java.util.concurrent.atomic.AtomicBoolean
import java.util.concurrent.atomic.AtomicLong
import java.util.concurrent.atomic.AtomicReference

/** LAN discovery, resilient TCP control and bounded UDP 3DoF pose transport. */
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
) {
    private val appContext = context.applicationContext
    private val mainHandler = Handler(Looper.getMainLooper())
    private val discoveryExecutor = Executors.newSingleThreadExecutor()
    private val sessionExecutor = Executors.newSingleThreadExecutor()
    private val poseExecutor = Executors.newSingleThreadExecutor()
    private val running = AtomicBoolean(false)
    private val sessionGeneration = AtomicLong(0L)
    private val latestPose = AtomicReference<HeadPose?>(null)
    private val socketLock = Any()
    private val sessionControlLock = Any()
    private val controlWriteLock = Any()

    @Volatile private var controlSocket: Socket? = null
    @Volatile private var poseSocket: DatagramSocket? = null
    @Volatile private var activeControlWriter: BufferedWriter? = null
    @Volatile private var activeControlGeneration: Long = -1L

    fun publishDisplaySettings(settings: JSONObject) {
        val writer = activeControlWriter ?: return
        val generation = activeControlGeneration
        if (!isSessionActive(generation)) return
        try {
            writeControlMessage(writer, JSONObject().put("type", "display_settings").put("settings", settings))
        } catch (error: Exception) {
            postStatus("显示参数同步失败：${error.message ?: error.javaClass.simpleName}", true)
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

    fun discover(onResults: (List<DiscoveredHost>) -> Unit) {
        discoveryExecutor.execute {
            val found = linkedMapOf<String, DiscoveredHost>()
            var multicastLock: WifiManager.MulticastLock? = null
            try {
                val wifiManager = appContext.getSystemService(Context.WIFI_SERVICE) as? WifiManager
                if (wifiManager != null) {
                    multicastLock = wifiManager.createMulticastLock("PhoneVR-LAN-Discovery").apply {
                        setReferenceCounted(false)
                        acquire()
                    }
                }
                DatagramSocket(null).use { socket ->
                    socket.reuseAddress = true
                    socket.broadcast = true
                    socket.bind(InetSocketAddress(0))
                    socket.soTimeout = DISCOVERY_READ_TIMEOUT_MS
                    val request = DISCOVERY_REQUEST.toByteArray(StandardCharsets.UTF_8)
                    socket.send(
                        DatagramPacket(
                            request, request.size,
                            InetAddress.getByName("255.255.255.255"), DISCOVERY_PORT,
                        ),
                    )
                    val deadline = System.currentTimeMillis() + DISCOVERY_WINDOW_MS
                    val buffer = ByteArray(2048)
                    while (System.currentTimeMillis() < deadline) {
                        try {
                            val packet = DatagramPacket(buffer, buffer.size)
                            socket.receive(packet)
                            val response = String(packet.data, packet.offset, packet.length, StandardCharsets.UTF_8)
                            if (!response.startsWith(DISCOVERY_RESPONSE_PREFIX)) continue
                            val json = JSONObject(response.removePrefix(DISCOVERY_RESPONSE_PREFIX))
                            val address = packet.address.hostAddress ?: continue
                            found[address] = DiscoveredHost(
                                address = address,
                                controlPort = json.optInt("controlPort", DEFAULT_CONTROL_PORT),
                                name = json.optString("name", "PhoneVR Host"),
                            )
                        } catch (_: SocketTimeoutException) {
                            // Continue until the discovery window expires.
                        }
                    }
                }
            } catch (error: Exception) {
                postStatus("搜索 PC 失败：${error.message ?: error.javaClass.simpleName}", false)
            } finally {
                try {
                    if (multicastLock?.isHeld == true) multicastLock?.release()
                } catch (_: Exception) { }
            }
            mainHandler.post { onResults(found.values.toList()) }
        }
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

    fun disconnect() {
        synchronized(sessionControlLock) {
            sessionGeneration.incrementAndGet()
            running.set(false)
            closeCurrentSockets()
        }
        postStatus("已断开 PC", false)
    }

    fun shutdown() {
        synchronized(sessionControlLock) {
            sessionGeneration.incrementAndGet()
            running.set(false)
            closeCurrentSockets()
        }
        discoveryExecutor.shutdownNow()
        sessionExecutor.shutdownNow()
        poseExecutor.shutdownNow()
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
                val reader = BufferedReader(InputStreamReader(openedTcp.getInputStream(), StandardCharsets.UTF_8))
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

                val welcomeLine = reader.readLine() ?: error("PC 在配对前关闭了连接")
                val welcome = JSONObject(welcomeLine)
                check(welcome.optString("type") == "welcome") {
                    welcome.optString("message", "PC 返回了无法识别的响应")
                }
                if (welcome.optInt("protocol") != PROTOCOL_VERSION) {
                    throw ProtocolMismatchException("PC 通信协议版本不兼容")
                }

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
                postStatus("已连接 $serverName，姿态发送正常", true)
                firstAttempt = false
                retryDelayMs = INITIAL_RECONNECT_DELAY_MS

                runControlLoop(generation, openedTcp, reader, writer)
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
            }

            if (!isSessionActive(generation)) return
            sleepBeforeRetry(retryDelayMs, generation)
            retryDelayMs = (retryDelayMs * 2L).coerceAtMost(MAX_RECONNECT_DELAY_MS)
        }
    }

    private fun runControlLoop(
        generation: Long,
        tcp: Socket,
        reader: BufferedReader,
        writer: BufferedWriter,
    ) {
        var lastHeartbeatNs = System.nanoTime() - HEARTBEAT_INTERVAL_NS
        var lastPongNs = System.nanoTime()

        while (isSessionActive(generation) && !tcp.isClosed) {
            val nowNs = System.nanoTime()
            if (nowNs - lastHeartbeatNs >= HEARTBEAT_INTERVAL_NS) {
                writeControlMessage(writer, JSONObject().put("type", "ping").put("sentAtNs", nowNs).put("batteryLevel", batteryLevel()).put("charging", isCharging()))
                lastHeartbeatNs = nowNs
            }
            if (nowNs - lastPongNs > HEARTBEAT_TIMEOUT_NS) {
                throw SocketTimeoutException("PC 心跳响应超时")
            }

            try {
                val line = reader.readLine() ?: error("PC 已关闭控制连接")
                val message = JSONObject(line)
                when (message.optString("type")) {
                    "pong" -> lastPongNs = System.nanoTime()
                    "error" -> postStatus("PC：${message.optString("message", "未知错误")}", true)
                    "get_optical_presets" -> {
                        val requestId = message.optString("requestId", "")
                        mainHandler.post {
                            if (isSessionActive(generation)) {
                                try {
                                    writeControlMessage(
                                        writer,
                                        JSONObject().put("type", "optical_presets")
                                            .put("requestId", requestId)
                                            .put("presets", currentOpticalPresets()),
                                    )
                                } catch (error: Exception) {
                                    postStatus("光学预设读取失败：${error.message ?: error.javaClass.simpleName}", true)
                                }
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
                                    writeControlMessage(
                                        writer,
                                        JSONObject().put("type", "optical_presets_ack")
                                            .put("requestId", requestId).put("status", "applied")
                                            .put("presets", applied),
                                    )
                                    postStatus("PC 已同步光学预设", true)
                                }
                            } catch (error: Throwable) {
                                if (isSessionActive(generation)) {
                                    try {
                                        writeControlMessage(
                                            writer,
                                            JSONObject().put("type", "optical_presets_ack")
                                                .put("requestId", requestId).put("status", "rejected")
                                                .put("message", error.message ?: error.javaClass.simpleName),
                                        )
                                    } catch (_: Exception) { }
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
                                    writeControlMessage(
                                        writer,
                                        JSONObject().put("type", "display_settings_ack")
                                            .put("requestId", requestId).put("status", "applied")
                                            .put("settings", applied),
                                    )
                                    postStatus("已应用光学预设", true)
                                }
                            } catch (error: Throwable) {
                                if (isSessionActive(generation)) {
                                    try {
                                        writeControlMessage(
                                            writer,
                                            JSONObject().put("type", "display_settings_ack")
                                                .put("requestId", requestId).put("status", "rejected")
                                                .put("message", error.message ?: error.javaClass.simpleName),
                                        )
                                    } catch (_: Exception) { }
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
                                    writeControlMessage(
                                        writer,
                                        JSONObject()
                                            .put("type", "display_settings_ack")
                                            .put("requestId", requestId)
                                            .put("status", "applied")
                                            .put("settings", applied),
                                    )
                                    postStatus("PC 已更新手机显示参数", true)
                                }
                            } catch (error: Throwable) {
                                if (isSessionActive(generation)) {
                                    try {
                                        writeControlMessage(
                                            writer,
                                            JSONObject()
                                                .put("type", "display_settings_ack")
                                                .put("requestId", requestId)
                                                .put("status", "rejected")
                                                .put("message", error.message ?: error.javaClass.simpleName),
                                        )
                                    } catch (_: Exception) { }
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
                                    writeControlMessage(
                                        writer,
                                        JSONObject().put("type", "device_action_ack")
                                            .put("requestId", requestId).put("status", "applied"),
                                    )
                                }
                            } catch (error: Throwable) {
                                if (isSessionActive(generation)) {
                                    try {
                                        writeControlMessage(
                                            writer,
                                            JSONObject().put("type", "device_action_ack")
                                                .put("requestId", requestId).put("status", "rejected")
                                                .put("message", error.message ?: error.javaClass.simpleName),
                                        )
                                    } catch (_: Exception) { }
                                }
                            }
                        }
                    }
                }
            } catch (_: SocketTimeoutException) {
                // A read timeout is a polling interval, not a disconnection.
            }
        }
    }

    /** Latest-sample-wins UDP sender, capped at 120 Hz when the device supports it. */
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
        const val DEFAULT_CONTROL_PORT = 48101
        const val DEFAULT_POSE_PORT = 48102
        const val DISCOVERY_PORT = 48100
        private const val PROTOCOL_VERSION = 1
        private const val CLIENT_VERSION = "PhoneVRStream-0.4.7"
        private const val DISCOVERY_REQUEST = "PHONEVR_DISCOVER_V1"
        private const val DISCOVERY_RESPONSE_PREFIX = "PHONEVR_HOST_V1 "
        private const val DISCOVERY_WINDOW_MS = 1800L
        private const val DISCOVERY_READ_TIMEOUT_MS = 300
        private const val CONNECT_TIMEOUT_MS = 4000
        private const val HANDSHAKE_TIMEOUT_MS = 5000
        private const val CONTROL_READ_TIMEOUT_MS = 1000
        private const val HEARTBEAT_INTERVAL_NS = 2_000_000_000L
        private const val HEARTBEAT_TIMEOUT_NS = 10_000_000_000L
        private const val MAX_POSE_SEND_HZ = 120
        private const val UDP_SEND_BUFFER_BYTES = 64 * 1024
        private const val POSE_ERROR_REPORT_INTERVAL_NS = 5_000_000_000L
        private const val POSE_IDLE_SLEEP_NS = 2_000_000L
        private const val INITIAL_RECONNECT_DELAY_MS = 1000L
        private const val MAX_RECONNECT_DELAY_MS = 8000L
    }
}
