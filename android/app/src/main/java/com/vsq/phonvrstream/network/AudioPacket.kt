package com.vsq.phonvrstream.network

import java.nio.ByteBuffer
import java.nio.ByteOrder
import java.util.zip.CRC32

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
    const val UDP_MAX_DATAGRAM_BYTES = 1_200
    const val FRAGMENT_HEADER_SIZE = 20
    const val FRAGMENT_PAYLOAD_BYTES = UDP_MAX_DATAGRAM_BYTES - FRAGMENT_HEADER_SIZE
    private const val FRAGMENT_VERSION = 1
    private const val MAX_PENDING_FRAMES = 16
    private const val FRAGMENT_LIFETIME_NS = 180_000_000L

    data class Frame(
        val direction: Int,
        val channels: Int,
        val sequence: Int,
        val timestampNs: Long,
        val samplesPerChannel: Int,
        val pcm: ByteArray,
    )

    private val magic = byteArrayOf(0x50, 0x56, 0x52, 0x41) // PVRA
    private val fragmentMagic = byteArrayOf(0x50, 0x56, 0x41, 0x46) // PVAF

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


    /** Split a complete PCM frame into UDP datagrams no larger than 1200 bytes. */
    fun fragment(wire: ByteArray): List<ByteArray> {
        val frame = requireNotNull(decode(wire)) { "Invalid PVRA frame for UDP fragmentation" }
        val count = (wire.size + FRAGMENT_PAYLOAD_BYTES - 1) / FRAGMENT_PAYLOAD_BYTES
        require(count in 1..4) { "Unsupported audio fragment count: $count" }
        return (0 until count).map { index ->
            val start = index * FRAGMENT_PAYLOAD_BYTES
            val end = minOf(start + FRAGMENT_PAYLOAD_BYTES, wire.size)
            val payload = wire.copyOfRange(start, end)
            val checksum = CRC32().apply { update(payload) }.value.toInt()
            ByteBuffer.allocate(FRAGMENT_HEADER_SIZE + payload.size)
                .order(ByteOrder.LITTLE_ENDIAN)
                .put(fragmentMagic)
                .put(FRAGMENT_VERSION.toByte())
                .put(frame.direction.toByte())
                .put(index.toByte())
                .put(count.toByte())
                .putInt(frame.sequence)
                .putShort(wire.size.toShort())
                .putShort(payload.size.toShort())
                .putInt(checksum)
                .put(payload)
                .array()
        }
    }

    /** Reassembles bounded, out-of-order UDP fragments. AOA carries raw PVRA frames. */
    class Reassembler {
        private data class Key(val direction: Int, val sequence: Int)
        private data class Assembly(
            val totalSize: Int,
            val parts: Array<ByteArray?>,
            val createdAtNs: Long,
            var received: Int = 0,
            var receivedBytes: Int = 0,
        )

        private val pending = LinkedHashMap<Key, Assembly>()

        @Synchronized
        fun add(datagram: ByteArray, nowNs: Long = System.nanoTime()): ByteArray? {
            if (datagram.size >= HEADER_SIZE && datagram.copyOfRange(0, 4).contentEquals(magic)) {
                return datagram.takeIf { decode(it) != null }
            }
            if (datagram.size < FRAGMENT_HEADER_SIZE ||
                !datagram.copyOfRange(0, 4).contentEquals(fragmentMagic)) return null
            val view = ByteBuffer.wrap(datagram).order(ByteOrder.LITTLE_ENDIAN)
            view.position(4)
            if ((view.get().toInt() and 0xff) != FRAGMENT_VERSION) return null
            val direction = view.get().toInt() and 0xff
            if (direction != DIRECTION_HOST_TO_PHONE && direction != DIRECTION_PHONE_TO_HOST) return null
            val index = view.get().toInt() and 0xff
            val count = view.get().toInt() and 0xff
            val sequence = view.int
            val totalSize = view.short.toInt() and 0xffff
            val payloadSize = view.short.toInt() and 0xffff
            val checksum = view.int
            val payload = ByteArray(view.remaining()).also { view.get(it) }
            if (datagram.size > UDP_MAX_DATAGRAM_BYTES || totalSize !in (HEADER_SIZE + 1)..(HEADER_SIZE + MAX_PAYLOAD_BYTES) ||
                count !in 1..4 || index !in 0 until count || payloadSize != payload.size ||
                payloadSize !in 1..FRAGMENT_PAYLOAD_BYTES) return null
            val expectedCount = (totalSize + FRAGMENT_PAYLOAD_BYTES - 1) / FRAGMENT_PAYLOAD_BYTES
            val expectedPayload = if (index == count - 1) totalSize - index * FRAGMENT_PAYLOAD_BYTES else FRAGMENT_PAYLOAD_BYTES
            if (count != expectedCount || payloadSize != expectedPayload) return null
            if (CRC32().apply { update(payload) }.value.toInt() != checksum) return null

            val iterator = pending.entries.iterator()
            while (iterator.hasNext()) {
                if (nowNs - iterator.next().value.createdAtNs > FRAGMENT_LIFETIME_NS) iterator.remove()
            }
            val key = Key(direction, sequence)
            var assembly = pending[key]
            if (assembly == null) {
                while (pending.size >= MAX_PENDING_FRAMES) {
                    val oldest = pending.keys.firstOrNull() ?: break
                    pending.remove(oldest)
                }
                assembly = Assembly(totalSize, arrayOfNulls(count), nowNs)
                pending[key] = assembly
            } else if (assembly.totalSize != totalSize || assembly.parts.size != count) {
                pending.remove(key)
                return null
            }
            val previous = assembly.parts[index]
            if (previous != null) {
                if (!previous.contentEquals(payload)) pending.remove(key)
                return null
            }
            assembly.parts[index] = payload
            assembly.received++
            assembly.receivedBytes += payload.size
            if (assembly.received != count) return null
            pending.remove(key)
            if (assembly.receivedBytes != totalSize) return null
            val complete = ByteArray(totalSize)
            var offset = 0
            for (part in assembly.parts) {
                val bytes = part ?: return null
                bytes.copyInto(complete, offset)
                offset += bytes.size
            }
            return complete.takeIf { decode(it) != null }
        }
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
