package com.vsq.phonvrstream.streaming

import android.media.MediaCodec
import android.media.MediaFormat
import android.os.Build
import android.os.Process
import android.util.Log
import android.view.Surface
import java.net.DatagramPacket
import java.net.DatagramSocket
import java.net.InetAddress
import java.net.InetSocketAddress
import java.net.SocketException
import java.util.concurrent.ArrayBlockingQueue
import java.util.concurrent.TimeUnit
import java.util.concurrent.atomic.AtomicBoolean
import java.util.concurrent.atomic.AtomicLong

/**
 * Receives PVRV H.264 SBS datagrams and decodes them onto the renderer's
 * SurfaceTexture. Incoming LAN UDP packets or USB-loopback packets are reassembled
 * on a worker thread before MediaCodec decoding.
 * The decoder never discards an access unit merely because an input buffer is
 * temporarily unavailable: it drains output and retries the same AU first.
 */
class VideoStreamReceiver(
    private val hostAddress: String,
    private val port: Int,
    private val width: Int,
    private val height: Int,
    private val frameRate: Int,
    private val decoderName: String,
    private val outputSurface: Surface,
    private val transportLabel: String = "UDP",
    private val onStatus: (String) -> Unit = {},
    private val onStats: (VideoStreamStats) -> Unit = {},
) {
    private val running = AtomicBoolean(false)
    private val needsKeyFrame = AtomicBoolean(true)
    private val framesReceived = AtomicLong(0)
    private val framesDecoded = AtomicLong(0)
    private val fragmentsReceived = AtomicLong(0)
    private val queueDrops = AtomicLong(0)
    private val decoderInputWaits = AtomicLong(0)
    private val decoderRecoveries = AtomicLong(0)
    private val decoderFlushRequested = AtomicBoolean(false)
    private val expiredAssemblies = AtomicLong(0)
    private val receivedWindow = AtomicLong(0)
    private val decodedWindow = AtomicLong(0)
    private val fragmentsWindow = AtomicLong(0)
    private val lastDatagramNs = AtomicLong(0)
    private val lastCompleteFrameNs = AtomicLong(0)
    private val lastDecodedFrameNs = AtomicLong(0)
    private val frameArrivalReported = AtomicBoolean(false)
    private val decodedFrameReported = AtomicBoolean(false)
    private val pendingFrames = ArrayBlockingQueue<CompletedVideoFrame>(2)
    private val reassembler = VideoFragmentReassembler()
    @Volatile private var socket: DatagramSocket? = null
    @Volatile private var boundLocalPort: Int = -1
    val localPort: Int get() = boundLocalPort
    @Volatile private var receiveThread: Thread? = null
    @Volatile private var decodeThread: Thread? = null

    fun start() {
        require(width > 0 && height > 0 && frameRate in 1..120) { "Invalid negotiated video profile" }
        check(outputSurface.isValid) { "Video decoder output Surface is no longer valid" }
        if (!running.compareAndSet(false, true)) return
        val udp = DatagramSocket(null)
        try {
            udp.reuseAddress = true
            udp.receiveBufferSize = REQUESTED_RECEIVE_BUFFER_BYTES
            udp.soTimeout = 100
            udp.bind(InetSocketAddress(port))
        } catch (error: Exception) {
            running.set(false)
            udp.close()
            throw error
        }
        socket = udp
        boundLocalPort = udp.localPort
        Log.i(TAG, "H.264 SBS receiver started: $hostAddress:$port ${width}x$height@$frameRate; UDP rcvbuf=${udp.receiveBufferSize}")
        onStatus("$transportLabel 视频接收已启动，等待 PC 首帧")
        onStats(VideoStreamStats(state = "等待视频"))
        decodeThread = Thread({ decodeLoop() }, "PhoneVR-Video-Decode").apply { start() }
        receiveThread = Thread({ receiveLoop(udp) }, "PhoneVR-Video-UDP").apply { start() }
    }

    fun stop() {
        if (!running.getAndSet(false)) return
        try { socket?.close() } catch (_: Exception) { }
        receiveThread?.interrupt()
        decodeThread?.interrupt()
        receiveThread?.join(500)
        decodeThread?.join(1500)
        receiveThread = null
        decodeThread = null
        socket = null
        boundLocalPort = -1
        pendingFrames.clear()
        reassembler.clear()
        onStats(VideoStreamStats(state = "视频已停止"))
        Log.i(TAG, "H.264 SBS receiver stopped; received=${framesReceived.get()} decoded=${framesDecoded.get()} fragments=${fragmentsReceived.get()} queueDrops=${queueDrops.get()} inputWaits=${decoderInputWaits.get()} recoveries=${decoderRecoveries.get()}")
    }

    private fun receiveLoop(udp: DatagramSocket) {
        setThreadPriority(Process.THREAD_PRIORITY_DEFAULT)
        val expectedAddress = runCatching { InetAddress.getByName(hostAddress).hostAddress }.getOrNull()
        val buffer = ByteArray(VideoFragmentReassembler.MAX_DATAGRAM_SIZE)
        val datagram = DatagramPacket(buffer, buffer.size)
        var previousFrameId: Long? = null
        var windowStartNs = System.nanoTime()
        while (running.get()) {
            datagram.length = buffer.size
            try {
                udp.receive(datagram)
            } catch (_: java.net.SocketTimeoutException) {
                reassembler.expire()
                windowStartNs = publishStatsIfDue(windowStartNs, System.nanoTime())
                continue
            } catch (_: SocketException) {
                if (running.get()) Log.w(TAG, "Video UDP socket closed unexpectedly")
                break
            } catch (error: Exception) {
                if (running.get()) Log.w(TAG, "Video UDP receive failed", error)
                windowStartNs = publishStatsIfDue(windowStartNs, System.nanoTime())
                continue
            }
            if (expectedAddress != null && datagram.address.hostAddress != expectedAddress) continue

            val now = System.nanoTime()
            val previousDatagramNs = lastDatagramNs.getAndSet(now)
            if (previousDatagramNs > 0L && now - previousDatagramNs >= UDP_RESUME_GAP_NS) {
                // After a Wi-Fi pause, P-frames may refer to reference pictures
                // that the decoder never received. Restart at the next IDR.
                pendingFrames.clear()
                reassembler.clear()
                needsKeyFrame.set(true)
                decoderFlushRequested.set(true)
                Log.w(TAG, "Video UDP resumed after ${(now - previousDatagramNs) / 1_000_000L}ms gap; flushing decoder and waiting for IDR")
            }
            fragmentsReceived.incrementAndGet()
            fragmentsWindow.incrementAndGet()
            val frame = reassembler.accept(datagram.data, datagram.length, now)
            if (frame != null) {
                previousFrameId?.let { previous ->
                    val delta = (frame.frameId - previous) and 0xffff_ffffL
                    if (delta > 1L && delta < 0x8000_0000L) {
                        needsKeyFrame.set(true)
                        Log.w(TAG, "H.264 frame sequence gap: previous=$previous current=${frame.frameId} missing=${delta - 1}; waiting for IDR")
                    }
                }
                previousFrameId = frame.frameId
                lastCompleteFrameNs.set(now)
                framesReceived.incrementAndGet()
                receivedWindow.incrementAndGet()
                if (!pendingFrames.offer(frame)) {
                    // Dropping a reference AU can invalidate following P-frames.
                    // Signal the decoder to resume only at the next IDR.
                    pendingFrames.poll()
                    pendingFrames.offer(frame)
                    queueDrops.incrementAndGet()
                    needsKeyFrame.set(true)
                }
                if (frameArrivalReported.compareAndSet(false, true)) {
                    onStatus("已收到完整 H.264 帧，等待解码输出")
                }
            }
            windowStartNs = publishStatsIfDue(windowStartNs, now)
        }
    }

    private fun publishStatsIfDue(windowStartNs: Long, now: Long): Long {
        val elapsed = now - windowStartNs
        if (elapsed < STATS_INTERVAL_NS) return windowStartNs
        val receiveCount = receivedWindow.getAndSet(0)
        val decodeCount = decodedWindowCounter.getAndSet(0)
        val fragmentCount = fragmentsWindow.getAndSet(0)
        val expiredNow = reassembler.takeExpiredAssemblyCount()
        if (expiredNow > 0L) expiredAssemblies.addAndGet(expiredNow)
        val lastDecoded = lastDecodedFrameNs.get()
        val lastComplete = lastCompleteFrameNs.get()
        val lastPacket = lastDatagramNs.get()
        val decodedAge = if (lastDecoded == 0L) -1L else ((now - lastDecoded).coerceAtLeast(0L) / 1_000_000L)
        val completeAge = if (lastComplete == 0L) -1L else (now - lastComplete).coerceAtLeast(0L) / 1_000_000L
        val packetAge = if (lastPacket == 0L) -1L else (now - lastPacket).coerceAtLeast(0L) / 1_000_000L
        val state = when {
            decodedAge in 0..STREAM_STALE_MS -> "解码流畅"
            completeAge in 0..STREAM_STALE_MS -> "解码停滞"
            packetAge in 0..STREAM_STALE_MS -> "$transportLabel 组帧中"
            framesReceived.get() == 0L -> "等待视频"
            else -> "视频无数据"
        }
        val seconds = elapsed / 1_000_000_000.0
        onStats(
            VideoStreamStats(
                state = state,
                receiveFps = receiveCount / seconds,
                decodeFps = decodeCount / seconds,
                fragmentsPerSecond = (fragmentCount / seconds).toInt(),
                decoderQueueDepth = pendingFrames.size,
                reassemblyInFlight = reassembler.inFlightCount(),
                expiredAssemblies = expiredAssemblies.get(),
                queueDrops = queueDrops.get(),
                decoderDrops = pendingDecoderDrops.get(),
                decoderInputWaits = decoderInputWaits.get(),
                decoderRecoveries = decoderRecoveries.get(),
                lastPacketAgeMs = packetAge,
                lastCompleteFrameAgeMs = completeAge,
                lastDecodedAgeMs = decodedAge,
            ),
        )
        if (state == "解码停滞") {
            Log.w(TAG, "H.264 output stalled: complete-frame age=${completeAge}ms decoded-age=${decodedAge}ms queue=${pendingFrames.size}")
        } else {
            Log.i(TAG, "Video state=$state RX=${receiveCount / seconds}fps decode=${decodeCount / seconds}fps fragments=${fragmentCount / seconds}/s, reassembly=${reassembler.inFlightCount()}, expired=${expiredAssemblies.get()}, decoderQueue=${pendingFrames.size}, queueDrops=${queueDrops.get()}, decoderDrops=${pendingDecoderDrops.get()}, inputWaits=${decoderInputWaits.get()}, recoveries=${decoderRecoveries.get()}, packetAge=${packetAge}ms completeAge=${completeAge}ms decodedAge=${decodedAge}ms")
        }
        return now
    }

    private fun decodeLoop() {
        // Keep the IMU sender and GL thread schedulable under decoder pressure.
        // A hardware decoder does not need a permanently elevated Java thread.
        setThreadPriority(Process.THREAD_PRIORITY_DEFAULT)
        while (running.get()) {
            var codec: MediaCodec? = null
            try {
                codec = createDecoder()
                decodeSession(codec)
            } catch (error: Throwable) {
                if (running.get()) {
                    Log.e(TAG, "MediaCodec H.264 decoder session failed", error)
                    onStatus("H.264 解码器异常，正在恢复：${error.message ?: error.javaClass.simpleName}")
                }
            } finally {
                try { codec?.stop() } catch (_: Exception) { }
                try { codec?.release() } catch (_: Exception) { }
            }
            if (running.get()) {
                needsKeyFrame.set(true)
                pendingFrames.clear()
                decoderRecoveries.incrementAndGet()
                sleepQuietly(DECODER_RESTART_DELAY_MS)
            }
        }
    }

    private fun createDecoder(): MediaCodec {
        val selectedCodec = MediaCodec.createByCodecName(decoderName)
        if (selectedCodec.codecInfo.isEncoder || selectedCodec.codecInfo.supportedTypes.none { type ->
                type.equals(MediaFormat.MIMETYPE_VIDEO_AVC, ignoreCase = true)
            }) {
            selectedCodec.release()
            error("Negotiated codec is not an H.264 decoder: $decoderName")
        }
        val format = MediaFormat.createVideoFormat(MediaFormat.MIMETYPE_VIDEO_AVC, width, height).apply {
            setInteger(MediaFormat.KEY_MAX_INPUT_SIZE, width * height * 2)
            setInteger(MediaFormat.KEY_FRAME_RATE, frameRate)
            if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.M) setInteger(MediaFormat.KEY_PRIORITY, 0)
            if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.R) setInteger(MediaFormat.KEY_LOW_LATENCY, 1)
        }
        selectedCodec.configure(format, outputSurface, null, 0)
        selectedCodec.start()
        Log.i(TAG, "MediaCodec decoder active: ${selectedCodec.name}, ${width}x$height@$frameRate")
        onStatus("H.264 解码器已启动，等待视频帧")
        return selectedCodec
    }

    private fun decodeSession(codec: MediaCodec) {
        val info = MediaCodec.BufferInfo()
        var presentationTimeUs = 0L
        var pendingFrame: CompletedVideoFrame? = null
        var pendingSinceNs = 0L
        var lastStatsNs = System.nanoTime()
        var lastRecoveryNs = 0L
        var decodedWindow = 0L
        var codecStartedNs = lastStatsNs
        while (running.get()) {
            if (decoderFlushRequested.getAndSet(false)) {
                codec.flush()
                pendingFrame = null
                pendingFrames.clear()
                needsKeyFrame.set(true)
                Log.i(TAG, "MediaCodec flushed after video transport gap; waiting for IDR")
            }
            // Always drain output before requesting another input buffer. This
            // prevents a full MediaCodec output queue from starving its input.
            var outputDrained = false
            while (running.get()) {
                val outputIndex = codec.dequeueOutputBuffer(info, 0)
                if (outputIndex >= 0) {
                    codec.releaseOutputBuffer(outputIndex, true)
                    val now = System.nanoTime()
                    decodedWindow++
                    framesDecoded.incrementAndGet()
                    decodedWindowCounter.incrementAndGet()
                    lastDecodedFrameNs.set(now)
                    outputDrained = true
                    if (decodedFrameReported.compareAndSet(false, true)) {
                        onStatus("H.264 帧已解码，等待双目显示")
                    }
                } else if (outputIndex == MediaCodec.INFO_OUTPUT_FORMAT_CHANGED) {
                    Log.i(TAG, "MediaCodec output format: ${codec.outputFormat}")
                } else {
                    break
                }
            }

            if (pendingFrame == null) {
                pendingFrame = try { pendingFrames.poll(5, TimeUnit.MILLISECONDS) } catch (_: InterruptedException) { null }
                if (pendingFrame != null) pendingSinceNs = System.nanoTime()
            }

            val frame = pendingFrame
            if (frame != null) {
                if (needsKeyFrame.get() && !frame.keyFrame) {
                    pendingFrame = null
                    pendingDecoderDrops.incrementAndGet()
                } else {
                    if (frame.keyFrame && needsKeyFrame.getAndSet(false)) {
                        codec.flush()
                        presentationTimeUs = 0L
                    }
                    val inputIndex = codec.dequeueInputBuffer(0)
                    if (inputIndex >= 0) {
                        val input = codec.getInputBuffer(inputIndex)
                        if (input == null || frame.bytes.size > input.capacity()) {
                            codec.queueInputBuffer(inputIndex, 0, 0, presentationTimeUs, 0)
                            needsKeyFrame.set(true)
                            pendingFrame = null
                            pendingDecoderDrops.incrementAndGet()
                        } else {
                            input.clear()
                            input.put(frame.bytes)
                            codec.queueInputBuffer(inputIndex, 0, frame.bytes.size, presentationTimeUs, 0)
                            presentationTimeUs += 1_000_000L / frameRate
                            pendingFrame = null
                        }
                    } else {
                        // Keep this AU and retry after draining output. A single
                        // transient input-buffer shortage must not force a GOP wait.
                        // Back off briefly instead of spinning at display priority:
                        // a busy loop here can starve the 180 Hz IMU and GL scheduler.
                        decoderInputWaits.incrementAndGet()
                        if (System.nanoTime() - pendingSinceNs > MAX_PENDING_INPUT_AGE_NS) {
                            pendingFrame = null
                            needsKeyFrame.set(true)
                            pendingDecoderDrops.incrementAndGet()
                            Log.w(TAG, "MediaCodec input remained unavailable for ${MAX_PENDING_INPUT_AGE_NS / 1_000_000L}ms; resynchronizing at next IDR")
                        } else {
                            sleepQuietly(1)
                        }
                    }
                }
            }

            val now = System.nanoTime()
            val lastDecoded = lastDecodedFrameNs.get()
            val lastComplete = lastCompleteFrameNs.get()
            val referenceDecoded = if (lastDecoded > 0L) lastDecoded else codecStartedNs
            if (lastComplete >= codecStartedNs && now - referenceDecoded > DECODER_STALL_NS && now - lastRecoveryNs > DECODER_STALL_NS) {
                Log.w(TAG, "MediaCodec produced no output while complete H.264 frames keep arriving; flushing and waiting for IDR")
                codec.flush()
                pendingFrames.clear()
                pendingFrame = null
                needsKeyFrame.set(true)
                pendingDecoderDrops.incrementAndGet()
                lastRecoveryNs = now
            }

            if (now - lastStatsNs >= 5_000_000_000L) {
                Log.i(TAG, "Video decode ${(decodedWindow * 1e9 / (now - lastStatsNs)).toInt()} FPS")
                decodedWindow = 0
                lastStatsNs = now
            }
            if (!outputDrained && pendingFrame == null && pendingFrames.isEmpty()) sleepQuietly(1)
        }
    }

    private fun setThreadPriority(priority: Int) {
        try { Process.setThreadPriority(priority) } catch (_: RuntimeException) { }
    }

    private fun sleepQuietly(milliseconds: Long) {
        try { Thread.sleep(milliseconds) } catch (_: InterruptedException) { Thread.currentThread().interrupt() }
    }

    companion object {
        private const val TAG = "PhoneVRVideo"
        private const val REQUESTED_RECEIVE_BUFFER_BYTES = 8 * 1024 * 1024
        private const val STATS_INTERVAL_NS = 1_000_000_000L
        private const val STREAM_STALE_MS = 1_500L
        private const val MAX_PENDING_INPUT_AGE_NS = 200_000_000L
        private const val DECODER_STALL_NS = 2_000_000_000L
        private const val DECODER_RESTART_DELAY_MS = 300L
        private const val UDP_RESUME_GAP_NS = 1_000_000_000L
    }

    private val pendingDecoderDrops = AtomicLong(0)
    private val decodedWindowCounter = AtomicLong(0)
}
