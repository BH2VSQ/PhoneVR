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
import java.util.concurrent.atomic.AtomicBoolean
import java.util.concurrent.atomic.AtomicLong

/**
 * Receives PhoneVR H.264 SBS datagrams and decodes them directly to a
 * SurfaceTexture-backed Surface. Receive/reassembly and MediaCodec run on
 * separate bounded worker threads; neither touches the Android UI thread.
 */
class VideoStreamReceiver(
    private val hostAddress: String,
    private val port: Int,
    private val width: Int,
    private val height: Int,
    private val frameRate: Int,
    private val decoderName: String,
    private val outputSurface: Surface,
    private val onStage: (stage: String, detail: String, datagrams: Long, accessUnits: Long, decoded: Long) -> Unit = { _, _, _, _, _ -> },
) {
    private val running = AtomicBoolean(false)
    private val needsKeyFrame = AtomicBoolean(true)
    private val framesReceived = AtomicLong(0)
    private val framesDecoded = AtomicLong(0)
    private val fragmentsRejected = AtomicLong(0)
    private val udpDatagrams = AtomicLong(0)
    private val udpBytes = AtomicLong(0)
    private val queueDrops = AtomicLong(0)
    private val pendingFrames = ArrayBlockingQueue<CompletedVideoFrame>(2)
    private val reassembler = VideoFragmentReassembler()
    @Volatile private var socket: DatagramSocket? = null
    @Volatile private var receiveThread: Thread? = null
    @Volatile private var decodeThread: Thread? = null

    fun start() {
        if (!running.compareAndSet(false, true)) return
        require(width > 0 && height > 0 && frameRate in 1..120) { "Invalid negotiated video profile" }
        if (!outputSurface.isValid) {
            running.set(false)
            throw IllegalStateException("Video decoder output Surface is no longer valid")
        }
        val udp = DatagramSocket(null)
        try {
            udp.reuseAddress = true
            udp.receiveBufferSize = 4 * 1024 * 1024
            udp.soTimeout = 100
            udp.bind(InetSocketAddress(port))
        } catch (error: Exception) {
            running.set(false)
            udp.close()
            throw error
        }
        socket = udp
        Log.i(TAG, "H.264 SBS receiver bound: $hostAddress:$port ${width}x$height@$frameRate")
        reportStage("receiver_started", "UDP $hostAddress:$port · ${width}x$height@$frameRate · $decoderName")
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
        pendingFrames.clear()
        reassembler.clear()
        Log.i(TAG, "H.264 SBS receiver stopped; datagrams=${udpDatagrams.get()} bytes=${udpBytes.get()} complete=${framesReceived.get()} decoded=${framesDecoded.get()} invalidOrForeign=${fragmentsRejected.get()} queueDrops=${queueDrops.get()}")
    }

    private fun reportStage(stage: String, detail: String) {
        runCatching {
            onStage(stage, detail, udpDatagrams.get(), framesReceived.get(), framesDecoded.get())
        }.onFailure { Log.w(TAG, "Unable to report video stage $stage", it) }
    }

    private fun receiveLoop(udp: DatagramSocket) {
        setDisplayThreadPriority()
        val expectedAddress = runCatching { InetAddress.getByName(hostAddress).hostAddress }.getOrNull()
        val buffer = ByteArray(VideoFragmentReassembler.MAX_DATAGRAM_SIZE)
        var previousFrameId: Long? = null
        var localCompleted = 0L
        var localDatagrams = 0L
        var localBytes = 0L
        var localQueueDrops = 0L
        var firstDatagramLogged = false
        var lastNoDatagramReportNs = System.nanoTime()
        var lastStatsNs = System.nanoTime()
        while (running.get()) {
            val datagram = DatagramPacket(buffer, buffer.size)
            try {
                udp.receive(datagram)
            } catch (_: java.net.SocketTimeoutException) {
                reassembler.expire()
                val idleNow = System.nanoTime()
                if (!firstDatagramLogged && idleNow - lastNoDatagramReportNs >= 2_000_000_000L) {
                    lastNoDatagramReportNs = idleNow
                    val detail = "No H.264 UDP datagrams received from $hostAddress:$port; receiver is bound and waiting"
                    Log.w(TAG, detail)
                    reportStage("udp_wait", detail)
                }
                continue
            } catch (_: SocketException) {
                if (running.get()) Log.w(TAG, "Video UDP socket closed unexpectedly")
                break
            } catch (error: Exception) {
                if (running.get()) Log.w(TAG, "Video UDP receive failed", error)
                continue
            }
            if (expectedAddress != null && datagram.address.hostAddress != expectedAddress) {
                fragmentsRejected.incrementAndGet()
                if (fragmentsRejected.get() == 1L) {
                    reportStage("udp_foreign_source", "Ignoring video datagrams from ${datagram.address.hostAddress}; expected $expectedAddress")
                }
                continue
            }
            lastNoDatagramReportNs = System.nanoTime()
            udpDatagrams.incrementAndGet()
            udpBytes.addAndGet(datagram.length.toLong())
            localDatagrams++
            localBytes += datagram.length
            if (!firstDatagramLogged) {
                firstDatagramLogged = true
                Log.i(TAG, "First H.264 UDP datagram received from ${datagram.address.hostAddress}:${datagram.port}")
                reportStage("udp_first_datagram", "Received from ${datagram.address.hostAddress}:${datagram.port}")
            }
            val frame = reassembler.accept(datagram.data, datagram.length)
            if (frame != null) {
                previousFrameId?.let { previous ->
                    val delta = (frame.frameId - previous) and 0xffff_ffffL
                    if (delta > 1L && delta < 0x8000_0000L) needsKeyFrame.set(true)
                }
                previousFrameId = frame.frameId
                if (!pendingFrames.offer(frame)) {
                    pendingFrames.poll()
                    pendingFrames.offer(frame)
                    needsKeyFrame.set(true)
                    queueDrops.incrementAndGet()
                    localQueueDrops++
                }
                localCompleted++
                framesReceived.incrementAndGet()
                if (framesReceived.get() == 1L) {
                    reportStage("first_access_unit", "Complete H.264 access unit ${frame.frameId} · ${frame.bytes.size} bytes · IDR=${frame.keyFrame}")
                }
            }
            val now = System.nanoTime()
            if (now - lastStatsNs >= 5_000_000_000L) {
                val seconds = (now - lastStatsNs) / 1e9
                val detail = "${(localDatagrams / seconds).toInt()} datagrams/s · ${(localBytes * 8 / seconds / 1_000_000).toInt()} Mbps · ${(localCompleted / seconds).toInt()} access units/s · queueDrops=$localQueueDrops · queue=${pendingFrames.size}"
                Log.i(TAG, "Video RX $detail")
                reportStage(if (localDatagrams > 0 && localCompleted == 0L) "fragment_wait" else "receiving", detail)
                localCompleted = 0
                localDatagrams = 0
                localBytes = 0
                localQueueDrops = 0
                lastStatsNs = now
            }
        }
    }

    private fun decodeLoop() {
        setDisplayThreadPriority()
        var codec: MediaCodec? = null
        try {
            val selectedCodec = MediaCodec.createByCodecName(decoderName)
            if (selectedCodec.codecInfo.isEncoder || selectedCodec.codecInfo.supportedTypes.none { type ->
                    type.equals(MediaFormat.MIMETYPE_VIDEO_AVC, ignoreCase = true)
                }) {
                selectedCodec.release()
                error("Negotiated codec is not an H.264 decoder: $decoderName")
            }
            codec = selectedCodec
            val format = MediaFormat.createVideoFormat(MediaFormat.MIMETYPE_VIDEO_AVC, width, height).apply {
                setInteger(MediaFormat.KEY_MAX_INPUT_SIZE, width * height * 2)
                setInteger(MediaFormat.KEY_FRAME_RATE, frameRate)
                // Prefer realtime decoding and low-latency operation when supported.
                if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.M) {
                    setInteger(MediaFormat.KEY_PRIORITY, 0)
                }
                if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.R) {
                    setInteger(MediaFormat.KEY_LOW_LATENCY, 1)
                }
            }
            codec.configure(format, outputSurface, null, 0)
            codec.start()
            Log.i(TAG, "MediaCodec decoder active: ${codec.name}, ${width}x$height@$frameRate; waiting for first complete IDR access unit")
            reportStage("decoder_ready", "${codec.name} configured for ${width}x$height@$frameRate")
            val info = MediaCodec.BufferInfo()
            var presentationTimeUs = 0L
            var lastStatsNs = System.nanoTime()
            var decodedWindow = 0L
            var firstInputLogged = false
            var firstOutputLogged = false
            while (running.get()) {
                val frame = try { pendingFrames.poll(50, java.util.concurrent.TimeUnit.MILLISECONDS) } catch (_: InterruptedException) { null }
                if (frame != null) {
                    if (needsKeyFrame.get() && !frame.keyFrame) {
                        // Do not feed dependent P-frames after a UDP loss or queue overflow.
                        continue
                    }
                    if (frame.keyFrame && needsKeyFrame.getAndSet(false)) {
                        runCatching { codec.flush() }
                        presentationTimeUs = 0L
                    }
                    val inputIndex = codec.dequeueInputBuffer(5_000)
                    if (inputIndex >= 0) {
                        val input = codec.getInputBuffer(inputIndex)
                        if (input == null || frame.bytes.size > input.capacity()) {
                            codec.queueInputBuffer(inputIndex, 0, 0, presentationTimeUs, 0)
                            needsKeyFrame.set(true)
                            continue
                        }
                        input.clear()
                        input.put(frame.bytes)
                        codec.queueInputBuffer(inputIndex, 0, frame.bytes.size, presentationTimeUs, 0)
                        if (!firstInputLogged) {
                            firstInputLogged = true
                            Log.i(TAG, "First H.264 access unit queued to MediaCodec: frame=${frame.frameId} keyFrame=${frame.keyFrame} bytes=${frame.bytes.size}")
                            reportStage("decoder_input", "Frame=${frame.frameId} · IDR=${frame.keyFrame} · ${frame.bytes.size} bytes")
                        }
                        presentationTimeUs += 1_000_000L / frameRate
                    } else {
                        // Dropping an H.264 reference access unit can invalidate
                        // subsequent P-frames. Resume only at the next IDR.
                        needsKeyFrame.set(true)
                        Log.w(TAG, "MediaCodec input queue saturated; waiting for the next IDR")
                    }
                }

                while (running.get()) {
                    val outputIndex = codec.dequeueOutputBuffer(info, 0)
                    if (outputIndex >= 0) {
                        codec.releaseOutputBuffer(outputIndex, true)
                        decodedWindow++
                        framesDecoded.incrementAndGet()
                        if (!firstOutputLogged) {
                            firstOutputLogged = true
                            Log.i(TAG, "First decoded H.264 frame released to SurfaceTexture")
                            reportStage("decoded_frame", "First output buffer released to SurfaceTexture")
                        }
                    } else if (outputIndex == MediaCodec.INFO_OUTPUT_FORMAT_CHANGED) {
                        Log.i(TAG, "MediaCodec output format: ${codec.outputFormat}")
                    } else {
                        break
                    }
                }
                val now = System.nanoTime()
                if (now - lastStatsNs >= 5_000_000_000L) {
                    Log.i(TAG, "Video decode ${(decodedWindow * 1e9 / (now - lastStatsNs)).toInt()} FPS")
                    decodedWindow = 0
                    lastStatsNs = now
                }
            }
        } catch (error: Throwable) {
            if (running.get()) {
                Log.e(TAG, "MediaCodec H.264 decoder failed", error)
                reportStage("decoder_error", error.message ?: error.javaClass.simpleName)
            }
        } finally {
            try { codec?.stop() } catch (_: Exception) { }
            try { codec?.release() } catch (_: Exception) { }
            codec = null
        }
    }

    private fun setDisplayThreadPriority() {
        try { Process.setThreadPriority(Process.THREAD_PRIORITY_DISPLAY) } catch (_: RuntimeException) { }
    }

    companion object {
        private const val TAG = "PhoneVRVideo"
    }
}
