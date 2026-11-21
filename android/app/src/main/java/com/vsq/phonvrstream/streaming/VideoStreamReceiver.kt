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
    private val onStatus: (String) -> Unit = {},
) {
    private val running = AtomicBoolean(false)
    private val needsKeyFrame = AtomicBoolean(true)
    private val framesReceived = AtomicLong(0)
    private val framesDecoded = AtomicLong(0)
    private val fragmentsRejected = AtomicLong(0)
    private val frameArrivalReported = AtomicBoolean(false)
    private val decodedFrameReported = AtomicBoolean(false)
    private val pendingFrames = ArrayBlockingQueue<CompletedVideoFrame>(2)
    private val reassembler = VideoFragmentReassembler()
    @Volatile private var socket: DatagramSocket? = null
    @Volatile private var receiveThread: Thread? = null
    @Volatile private var decodeThread: Thread? = null

    fun start() {
        require(width > 0 && height > 0 && frameRate in 1..120) { "Invalid negotiated video profile" }
        if (!outputSurface.isValid) {
            throw IllegalStateException("Video decoder output Surface is no longer valid")
        }
        if (!running.compareAndSet(false, true)) return
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
        Log.i(TAG, "H.264 SBS receiver started: $hostAddress:$port ${width}x$height@$frameRate")
        onStatus("UDP 视频接收已启动，等待 PC 首帧")
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
        Log.i(TAG, "H.264 SBS receiver stopped; received=${framesReceived.get()} decoded=${framesDecoded.get()} rejected=${fragmentsRejected.get()}")
    }

    private fun receiveLoop(udp: DatagramSocket) {
        setDisplayThreadPriority()
        val expectedAddress = runCatching { InetAddress.getByName(hostAddress).hostAddress }.getOrNull()
        val buffer = ByteArray(VideoFragmentReassembler.MAX_DATAGRAM_SIZE)
        var previousFrameId: Long? = null
        var lastStatsNs = System.nanoTime()
        var localReceived = 0L
        var localRejected = 0L
        while (running.get()) {
            val datagram = DatagramPacket(buffer, buffer.size)
            try {
                udp.receive(datagram)
            } catch (_: java.net.SocketTimeoutException) {
                reassembler.expire()
                continue
            } catch (_: SocketException) {
                if (running.get()) Log.w(TAG, "Video UDP socket closed unexpectedly")
                break
            } catch (error: Exception) {
                if (running.get()) Log.w(TAG, "Video UDP receive failed", error)
                continue
            }
            if (expectedAddress != null && datagram.address.hostAddress != expectedAddress) continue
            val frame = reassembler.accept(datagram.data, datagram.length)
            if (frame == null) {
                localRejected++
            } else {
                previousFrameId?.let { previous ->
                    val delta = (frame.frameId - previous) and 0xffff_ffffL
                    if (delta > 1L && delta < 0x8000_0000L) needsKeyFrame.set(true)
                }
                previousFrameId = frame.frameId
                if (!pendingFrames.offer(frame)) {
                    pendingFrames.poll()
                    pendingFrames.offer(frame)
                    needsKeyFrame.set(true)
                }
                localReceived++
                framesReceived.incrementAndGet()
                if (frameArrivalReported.compareAndSet(false, true)) {
                    onStatus("已收到完整 H.264 帧，等待解码输出")
                }
            }
            val now = System.nanoTime()
            if (now - lastStatsNs >= 5_000_000_000L) {
                Log.i(TAG, "Video RX ${(localReceived * 1e9 / (now - lastStatsNs)).toInt()} FPS; incomplete/rejected=$localRejected; decoder queue=${pendingFrames.size}")
                localReceived = 0
                localRejected = 0
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
            Log.i(TAG, "MediaCodec decoder active: ${codec.name}, ${width}x$height@$frameRate")
            onStatus("H.264 解码器已启动，等待视频帧")
            val info = MediaCodec.BufferInfo()
            var presentationTimeUs = 0L
            var lastStatsNs = System.nanoTime()
            var decodedWindow = 0L
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
                        if (decodedFrameReported.compareAndSet(false, true)) {
                            onStatus("H.264 帧已解码，等待双目显示")
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
                onStatus("H.264 解码失败：${error.message ?: error.javaClass.simpleName}")
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
