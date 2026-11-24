package com.vsq.phonvrstream.network

import java.nio.ByteBuffer
import java.nio.ByteOrder

/** Wire format mirrored by windows-host/internal/audio/packet.go. */
object AudioPacket {
    const val HEADER_SIZE = 24
    const val SAMPLE_RATE = 48_000
    const val FRAME_DURATION_MS = 20
    const val SAMPLES_PER_CHANNEL = SAMPLE_RATE * FRAME_DURATION_MS / 1_000
    const val DIRECTION_HOST_TO_PHONE = 1
    const val DIRECTION_PHONE_TO_HOST = 2
    const val FORMAT_PCM_S16LE = 1
    const val MAX_PAYLOAD_BYTES = SAMPLES_PER_CHANNEL * 2 * 2

    data class Frame(
        val direction: Int,
        val channels: Int,
        val sequence: Int,
        val timestampNs: Long,
        val samplesPerChannel: Int,
        val pcm: ByteArray,
    )

    private val magic = byteArrayOf(0x50, 0x56, 0x52, 0x41) // PVRA

    fun encode(frame: Frame): ByteArray {
        require(frame.direction == DIRECTION_HOST_TO_PHONE || frame.direction == DIRECTION_PHONE_TO_HOST)
        require(frame.channels == 1 || frame.channels == 2)
        require(frame.samplesPerChannel == SAMPLES_PER_CHANNEL)
        require(frame.pcm.size == frame.samplesPerChannel * frame.channels * 2)
        require(frame.pcm.size <= MAX_PAYLOAD_BYTES)
        return ByteBuffer.allocate(HEADER_SIZE + frame.pcm.size)
            .order(ByteOrder.LITTLE_ENDIAN)
            .put(magic)
            .put(1.toByte()) // protocol version
            .put(frame.direction.toByte())
            .put(frame.channels.toByte())
            .put(FORMAT_PCM_S16LE.toByte())
            .putInt(frame.sequence)
            .putLong(frame.timestampNs)
            .putShort(frame.samplesPerChannel.toShort())
            .putShort(frame.pcm.size.toShort())
            .put(frame.pcm)
            .array()
    }

    fun decode(wire: ByteArray): Frame? {
        if (wire.size < HEADER_SIZE) return null
        val view = ByteBuffer.wrap(wire).order(ByteOrder.LITTLE_ENDIAN)
        val signature = ByteArray(4)
        view.get(signature)
        if (!signature.contentEquals(magic)) return null
        if ((view.get().toInt() and 0xff) != 1) return null
        val direction = view.get().toInt() and 0xff
        if (direction != DIRECTION_HOST_TO_PHONE && direction != DIRECTION_PHONE_TO_HOST) return null
        val channels = view.get().toInt() and 0xff
        if (channels != 1 && channels != 2) return null
        if ((view.get().toInt() and 0xff) != FORMAT_PCM_S16LE) return null
        val sequence = view.int
        val timestampNs = view.long
        val samples = view.short.toInt() and 0xffff
        val payloadSize = view.short.toInt() and 0xffff
        if (samples != SAMPLES_PER_CHANNEL || payloadSize != samples * channels * 2 ||
            payloadSize > MAX_PAYLOAD_BYTES || wire.size != HEADER_SIZE + payloadSize) return null
        val pcm = ByteArray(payloadSize)
        view.get(pcm)
        return Frame(direction, channels, sequence, timestampNs, samples, pcm)
    }
}
