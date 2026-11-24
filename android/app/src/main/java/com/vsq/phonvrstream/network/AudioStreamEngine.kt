package com.vsq.phonvrstream.network

import android.Manifest
import android.content.Context
import android.content.pm.PackageManager
import android.media.AudioAttributes
import android.media.AudioFormat
import android.media.AudioRecord
import android.media.AudioTrack
import android.media.MediaRecorder
import android.media.audiofx.AcousticEchoCanceler
import android.media.audiofx.NoiseSuppressor
import android.os.Process
import android.util.Log
import java.io.Closeable
import java.util.concurrent.ArrayBlockingQueue
import java.util.concurrent.TimeUnit
import java.util.concurrent.atomic.AtomicBoolean

/**
 * Independent low-latency phone audio pipeline. Network/USB transport is supplied
 * by NetworkClient; no audio operation runs on the video decoder or pose workers.
 * The first implementation uses fixed 20 ms PCM frames for broad Android support.
 */
class AudioStreamEngine(
    context: Context,
    private val sendPacket: (ByteArray) -> Unit,
) : Closeable {
    private val appContext = context.applicationContext
    private val running = AtomicBoolean(false)
    private val playbackQueue = ArrayBlockingQueue<AudioPacket.Frame>(12)
    private val uplinkQueue = ArrayBlockingQueue<ByteArray>(4)
    private val packetSequence = java.util.concurrent.atomic.AtomicInteger(0)
    private val playbackSequenceLock = Any()
    private var lastPlaybackSequence: Int? = null
    private val downlinkReassembler = AudioPacket.Reassembler()
    @Volatile private var audioTrack: AudioTrack? = null
    @Volatile private var audioRecord: AudioRecord? = null
    @Volatile private var echoCanceler: AcousticEchoCanceler? = null
    @Volatile private var noiseSuppressor: NoiseSuppressor? = null
    @Volatile private var playbackThread: Thread? = null
    @Volatile private var captureThread: Thread? = null
    @Volatile private var uplinkThread: Thread? = null

    fun start() {
        if (!running.compareAndSet(false, true)) return
        try {
            startPlayback()
            // Register the UDP return path even when microphone permission is denied.
            // The single silent frame lets the Host learn this socket address so playback
            // can still work in receive-only mode.
            runCatching {
                enqueueUplink(AudioPacket.encode(AudioPacket.Frame(
                    direction = AudioPacket.DIRECTION_PHONE_TO_HOST,
                    channels = 1,
                    sequence = packetSequence.getAndIncrement(),
                    timestampNs = System.nanoTime(),
                    samplesPerChannel = AudioPacket.SAMPLES_PER_CHANNEL,
                    pcm = ByteArray(AudioPacket.SAMPLES_PER_CHANNEL * 2),
                )))
            }.onFailure { Log.w(TAG, "Audio return-path registration failed: ${it.message}") }
            uplinkThread = Thread({ uplinkLoop() }, "PhoneVR-Audio-Uplink").apply {
                isDaemon = true
                start()
            }
            if (appContext.checkSelfPermission(Manifest.permission.RECORD_AUDIO) == PackageManager.PERMISSION_GRANTED) {
                runCatching { startCapture() }
                    .onFailure { Log.w(TAG, "Phone microphone capture unavailable; playback remains active: ${it.message}") }
            } else {
                Log.w(TAG, "Microphone permission not granted; PC-to-phone audio remains enabled")
            }
        } catch (error: Throwable) {
            close()
            throw error
        }
    }

    /** Called by the UDP or AOA receive worker. This method never blocks. */
    fun acceptDownlinkPacket(wire: ByteArray) {
        if (!running.get()) return
        val completeFrame = downlinkReassembler.add(wire) ?: return
        val frame = AudioPacket.decode(completeFrame) ?: return
        if (frame.direction != AudioPacket.DIRECTION_HOST_TO_PHONE || frame.channels != 2) return
        synchronized(playbackSequenceLock) {
            val previous = lastPlaybackSequence
            if (previous != null) {
                // Sequence is uint32 on the wire. Compare modulo 2^32 so the
                // signed Kotlin Int representation remains valid across wrap.
                val delta = (frame.sequence.toLong() - previous.toLong()) and 0xffff_ffffL
                if (delta == 0L || delta >= 0x8000_0000L) return
            }
            lastPlaybackSequence = frame.sequence
        }
        if (!playbackQueue.offer(frame)) {
            // Drop the oldest audio access unit to keep latency bounded.
            playbackQueue.poll()
            playbackQueue.offer(frame)
        }
    }

    private fun startPlayback() {
        val frameBytes = AudioPacket.SAMPLES_PER_CHANNEL * 2 * 2
        val minimum = AudioTrack.getMinBufferSize(
            AudioPacket.SAMPLE_RATE,
            AudioFormat.CHANNEL_OUT_STEREO,
            AudioFormat.ENCODING_PCM_16BIT,
        ).coerceAtLeast(frameBytes)
        val track = AudioTrack.Builder()
            .setAudioAttributes(
                AudioAttributes.Builder()
                    .setUsage(AudioAttributes.USAGE_GAME)
                    .setContentType(AudioAttributes.CONTENT_TYPE_MUSIC)
                    .build(),
            )
            .setAudioFormat(
                AudioFormat.Builder()
                    .setSampleRate(AudioPacket.SAMPLE_RATE)
                    .setEncoding(AudioFormat.ENCODING_PCM_16BIT)
                    .setChannelMask(AudioFormat.CHANNEL_OUT_STEREO)
                    .build(),
            )
            .setBufferSizeInBytes(maxOf(minimum * 2, frameBytes * 4))
            .setTransferMode(AudioTrack.MODE_STREAM)
            .setPerformanceMode(AudioTrack.PERFORMANCE_MODE_LOW_LATENCY)
            .build()
        audioTrack = track
        check(track.state == AudioTrack.STATE_INITIALIZED) { "AudioTrack initialization failed" }
        track.play()
        playbackThread = Thread({ playbackLoop(track, frameBytes) }, "PhoneVR-Audio-Playback").apply {
            isDaemon = true
            start()
        }
    }

    private fun playbackLoop(track: AudioTrack, frameBytes: Int) {
        try {
            Process.setThreadPriority(Process.THREAD_PRIORITY_AUDIO)
        } catch (_: RuntimeException) {
            // The audio thread still runs with the platform default priority.
        }
        var prebuffered = false
        val silence = ByteArray(frameBytes)
        while (running.get() && track.playState == AudioTrack.PLAYSTATE_PLAYING) {
            if (!prebuffered) {
                if (playbackQueue.size < INITIAL_JITTER_FRAMES) {
                    try {
                        Thread.sleep(2)
                    } catch (_: InterruptedException) {
                        if (!running.get()) break
                    }
                    continue
                }
                prebuffered = true
            }
            val frame = try {
                playbackQueue.poll(AudioPacket.FRAME_DURATION_MS.toLong(), TimeUnit.MILLISECONDS)
            } catch (_: InterruptedException) {
                if (!running.get()) break else continue
            }
            val pcm = frame?.pcm ?: silence
            var offset = 0
            while (running.get() && offset < pcm.size) {
                val count = track.write(pcm, offset, pcm.size - offset, AudioTrack.WRITE_BLOCKING)
                if (count < 0) {
                    Log.w(TAG, "AudioTrack write failed: $count")
                    return
                }
                if (count == 0) break
                offset += count
            }
        }
    }

    private fun startCapture() {
        val frameBytes = AudioPacket.SAMPLES_PER_CHANNEL * 2
        val minimum = AudioRecord.getMinBufferSize(
            AudioPacket.SAMPLE_RATE,
            AudioFormat.CHANNEL_IN_MONO,
            AudioFormat.ENCODING_PCM_16BIT,
        ).coerceAtLeast(frameBytes)
        val record = AudioRecord.Builder()
            .setAudioSource(MediaRecorder.AudioSource.MIC)
            .setAudioFormat(
                AudioFormat.Builder()
                    .setSampleRate(AudioPacket.SAMPLE_RATE)
                    .setEncoding(AudioFormat.ENCODING_PCM_16BIT)
                    .setChannelMask(AudioFormat.CHANNEL_IN_MONO)
                    .build(),
            )
            .setBufferSizeInBytes(maxOf(minimum * 2, frameBytes * 4))
            .build()
        audioRecord = record
        check(record.state == AudioRecord.STATE_INITIALIZED) { "AudioRecord initialization failed" }
        runCatching {
            if (AcousticEchoCanceler.isAvailable()) {
                echoCanceler = AcousticEchoCanceler.create(record.audioSessionId)?.apply { enabled = true }
            }
            if (NoiseSuppressor.isAvailable()) {
                noiseSuppressor = NoiseSuppressor.create(record.audioSessionId)?.apply { enabled = true }
            }
        }.onFailure { Log.i(TAG, "Optional microphone DSP unavailable: ${it.message}") }
        record.startRecording()
        captureThread = Thread({ captureLoop(record, frameBytes) }, "PhoneVR-Audio-Capture").apply {
            isDaemon = true
            start()
        }
    }

    private fun captureLoop(record: AudioRecord, frameBytes: Int) {
        try {
            Process.setThreadPriority(Process.THREAD_PRIORITY_AUDIO)
        } catch (_: RuntimeException) {
            // Continue with the default priority.
        }
        val pcm = ByteArray(frameBytes)
        while (running.get() && record.recordingState == AudioRecord.RECORDSTATE_RECORDING) {
            var offset = 0
            while (running.get() && offset < pcm.size) {
                val count = record.read(pcm, offset, pcm.size - offset, AudioRecord.READ_BLOCKING)
                if (count < 0) {
                    Log.w(TAG, "AudioRecord read failed: $count")
                    return
                }
                if (count == 0) continue
                offset += count
            }
            if (!running.get()) break
            val frame = AudioPacket.Frame(
                direction = AudioPacket.DIRECTION_PHONE_TO_HOST,
                channels = 1,
                sequence = packetSequence.getAndIncrement(),
                timestampNs = System.nanoTime(),
                samplesPerChannel = AudioPacket.SAMPLES_PER_CHANNEL,
                pcm = pcm.copyOf(),
            )
            enqueueUplink(AudioPacket.encode(frame))
        }
    }

    private fun enqueueUplink(packet: ByteArray) {
        if (!uplinkQueue.offer(packet)) {
            // Keep the capture side independent from a temporarily blocked USB writer.
            uplinkQueue.poll()
            uplinkQueue.offer(packet)
        }
    }

    private fun uplinkLoop() {
        try {
            Process.setThreadPriority(Process.THREAD_PRIORITY_AUDIO)
        } catch (_: RuntimeException) {
            // Continue with the default priority.
        }
        while (running.get() || uplinkQueue.isNotEmpty()) {
            val packet = try {
                uplinkQueue.poll(20, TimeUnit.MILLISECONDS)
            } catch (_: InterruptedException) {
                if (!running.get()) break else continue
            } ?: continue
            try {
                sendPacket(packet)
            } catch (error: Exception) {
                if (running.get()) Log.w(TAG, "Phone microphone packet send failed: ${error.message}")
            }
        }
    }

    override fun close() {
        if (!running.getAndSet(false)) return
        playbackThread?.interrupt()
        captureThread?.interrupt()
        uplinkThread?.interrupt()
        uplinkQueue.clear()
        runCatching { audioRecord?.stop() }
        runCatching { audioTrack?.pause() }
        runCatching { audioTrack?.flush() }
        runCatching { audioTrack?.stop() }
        runCatching { echoCanceler?.release() }
        runCatching { noiseSuppressor?.release() }
        runCatching { audioRecord?.release() }
        runCatching { audioTrack?.release() }
        echoCanceler = null
        noiseSuppressor = null
        audioRecord = null
        audioTrack = null
        playbackQueue.clear()
        synchronized(playbackSequenceLock) { lastPlaybackSequence = null }
        playbackThread = null
        captureThread = null
        uplinkThread = null
    }

    companion object {
        private const val TAG = "PhoneVRAudio"
        private const val INITIAL_JITTER_FRAMES = 2 // 40 ms target prebuffer
    }
}
