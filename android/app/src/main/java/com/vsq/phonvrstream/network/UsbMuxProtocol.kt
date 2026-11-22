package com.vsq.phonvrstream.network

import java.nio.ByteBuffer
import java.nio.ByteOrder

/** Wire format shared with windows-host/internal/usbmux. */
object UsbMuxProtocol {
    const val HEADER_SIZE = 16
    const val MAX_PAYLOAD_SIZE = 1024 * 1024
    const val CHANNEL_CONTROL = 1
    const val CHANNEL_POSE = 2
    const val CHANNEL_VIDEO = 3
    private const val VERSION = 1
    private val MAGIC = byteArrayOf(0x50, 0x56, 0x52, 0x55)

    data class Header(val channel: Int, val payloadLength: Int, val sequence: Int)

    fun encodeHeader(channel: Int, payloadLength: Int, sequence: Int): ByteArray {
        require(channel in CHANNEL_CONTROL..CHANNEL_VIDEO) { "Invalid USB multiplex channel" }
        require(payloadLength in 0..MAX_PAYLOAD_SIZE) { "Invalid USB multiplex payload length" }
        return ByteBuffer.allocate(HEADER_SIZE).order(ByteOrder.LITTLE_ENDIAN)
            .put(MAGIC)
            .put(VERSION.toByte())
            .put(channel.toByte())
            .putShort(0)
            .putInt(payloadLength)
            .putInt(sequence)
            .array()
    }

    fun decodeHeader(data: ByteArray): Header {
        require(data.size == HEADER_SIZE) { "USB multiplex header must be $HEADER_SIZE bytes" }
        require(data.copyOfRange(0, 4).contentEquals(MAGIC)) { "Invalid USB multiplex frame signature" }
        val view = ByteBuffer.wrap(data).order(ByteOrder.LITTLE_ENDIAN)
        view.position(4)
        val version = view.get().toInt() and 0xFF
        require(version == VERSION) { "Unsupported USB multiplex version $version" }
        val channel = view.get().toInt() and 0xFF
        require(channel in CHANNEL_CONTROL..CHANNEL_VIDEO) { "Unknown USB multiplex channel $channel" }
        require(view.short.toInt() == 0) { "Unsupported USB multiplex flags" }
        val length = view.int
        require(length in 0..MAX_PAYLOAD_SIZE) { "Invalid USB multiplex payload length $length" }
        return Header(channel, length, view.int)
    }
}
