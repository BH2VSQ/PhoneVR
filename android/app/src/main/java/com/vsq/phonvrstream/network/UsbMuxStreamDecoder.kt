package com.vsq.phonvrstream.network

/**
 * Reassembles PVRU frames from arbitrary AOA bulk reads.
 *
 * Android's AOA file descriptor represents USB bulk packets. Callers must read
 * with a buffer large enough for a complete USB packet before parsing the
 * byte stream. This decoder then handles frames split across reads or several
 * frames coalesced into one read.
 */
internal const val AOA_READ_BUFFER_SIZE = 16 * 1024

internal class UsbMuxStreamDecoder {
    data class Frame(
        val channel: Int,
        val sequence: Int,
        val payload: ByteArray,
    )

    private var buffer = ByteArray(INITIAL_CAPACITY)
    private var start = 0
    private var end = 0

    val pendingBytes: Int
        get() = end - start

    fun append(source: ByteArray, offset: Int = 0, length: Int = source.size - offset): List<Frame> {
        require(offset >= 0 && length >= 0 && offset + length <= source.size) {
            "Invalid USB multiplex input range"
        }
        if (length == 0) return emptyList()

        ensureWritable(length)
        System.arraycopy(source, offset, buffer, end, length)
        end += length

        val frames = ArrayList<Frame>()
        while (end - start >= UsbMuxProtocol.HEADER_SIZE) {
            val headerBytes = buffer.copyOfRange(start, start + UsbMuxProtocol.HEADER_SIZE)
            val header = try {
                UsbMuxProtocol.decodeHeader(headerBytes)
            } catch (error: IllegalArgumentException) {
                val hex = headerBytes.joinToString("") { "%02X".format(it.toInt() and 0xFF) }
                throw IllegalArgumentException(
                    "${error.message ?: "Invalid USB multiplex header"}; headerHex=$hex; pendingBytes=${end - start}",
                    error,
                )
            }

            val frameSize = UsbMuxProtocol.HEADER_SIZE + header.payloadLength
            if (end - start < frameSize) break

            val payloadStart = start + UsbMuxProtocol.HEADER_SIZE
            val payload = buffer.copyOfRange(payloadStart, payloadStart + header.payloadLength)
            frames += Frame(header.channel, header.sequence, payload)
            start += frameSize
        }

        compactIfUseful()
        return frames
    }

    private fun ensureWritable(additionalBytes: Int) {
        if (buffer.size - end >= additionalBytes) return
        compact()
        if (buffer.size - end >= additionalBytes) return

        val required = (end - start) + additionalBytes
        val maximum = UsbMuxProtocol.HEADER_SIZE + UsbMuxProtocol.MAX_PAYLOAD_SIZE + AOA_READ_BUFFER_SIZE
        require(required <= maximum) {
            "USB multiplex receive buffer exceeded limit: pending=${end - start}, incoming=$additionalBytes, limit=$maximum"
        }

        var capacity = buffer.size
        while (capacity < required) {
            capacity = minOf(capacity * 2, maximum)
        }
        buffer = buffer.copyOf(capacity)
    }

    private fun compactIfUseful() {
        if (start == end) {
            start = 0
            end = 0
        } else if (start >= buffer.size / 2) {
            compact()
        }
    }

    private fun compact() {
        if (start == 0) return
        val remaining = end - start
        if (remaining > 0) System.arraycopy(buffer, start, buffer, 0, remaining)
        start = 0
        end = remaining
    }

    companion object {
        private const val INITIAL_CAPACITY = AOA_READ_BUFFER_SIZE * 2
    }
}
