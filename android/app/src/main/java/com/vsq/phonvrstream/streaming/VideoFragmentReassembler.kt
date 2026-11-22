package com.vsq.phonvrstream.streaming

import java.nio.ByteBuffer
import java.nio.ByteOrder
import java.util.LinkedHashMap
import kotlin.math.min

/** One complete Annex-B H.264 access unit reconstructed from PVRV UDP fragments. */
data class CompletedVideoFrame(
    val frameId: Long,
    val timestampNs: Long,
    val keyFrame: Boolean,
    val bytes: ByteArray,
)

/**
 * Bounded, out-of-order tolerant fragment reassembly.
 *
 * It deliberately returns only complete frames. A late or incomplete frame is
 * discarded rather than queued behind newer video, because stale VR imagery is
 * worse than a dropped frame.
 */
class VideoFragmentReassembler(
    private val maxInFlightFrames: Int = 3,
    private val maxFrameBytes: Int = 4 * 1024 * 1024,
    private val frameTimeoutNs: Long = 250_000_000L,
) {
    private data class Assembly(
        val frameId: Long,
        val timestampNs: Long,
        val frameSize: Int,
        val keyFrame: Boolean,
        val data: ByteArray,
        val received: BooleanArray,
        var receivedBytes: Int = 0,
        var sawStart: Boolean = false,
        var sawEnd: Boolean = false,
        var lastUpdateNs: Long,
    )

    private val pending = LinkedHashMap<Long, Assembly>()
    private var latestCompletedFrameId: Long? = null
    private var expiredAssemblyCount = 0L

    @Synchronized
    fun inFlightCount(): Int = pending.size

    @Synchronized
    fun takeExpiredAssemblyCount(): Long {
        val count = expiredAssemblyCount
        expiredAssemblyCount = 0L
        return count
    }

    @Synchronized
    fun accept(datagram: ByteArray, length: Int = datagram.size, nowNs: Long = System.nanoTime()): CompletedVideoFrame? {
        expire(nowNs)
        if (length < HEADER_SIZE || length > MAX_DATAGRAM_SIZE || length > datagram.size) return null
        if (datagram[0] != 'P'.code.toByte() || datagram[1] != 'V'.code.toByte() ||
            datagram[2] != 'R'.code.toByte() || datagram[3] != 'V'.code.toByte()) return null
        if (datagram[4].toInt() and 0xff != 1) return null
        val flags = datagram[5].toInt() and 0xff
        if (flags and KNOWN_FLAGS.inv() != 0) return null

        val header = ByteBuffer.wrap(datagram, 6, HEADER_SIZE - 6).order(ByteOrder.BIG_ENDIAN)
        if (header.short.toInt() and 0xffff != HEADER_SIZE) return null
        val frameId = header.int.toLong() and 0xffff_ffffL
        val timestampNs = header.long
        val frameSize = header.int
        val offset = header.int
        val payloadLength = header.short.toInt() and 0xffff
        val reserved = header.short.toInt() and 0xffff
        if (reserved != 0 || payloadLength <= 0 || payloadLength != length - HEADER_SIZE) return null
        if (frameSize <= 0 || frameSize > maxFrameBytes || offset < 0 || offset >= frameSize) return null
        if (offset % MAX_PAYLOAD_SIZE != 0) return null
        val fragmentIndex = offset / MAX_PAYLOAD_SIZE
        val fragmentCount = (frameSize + MAX_PAYLOAD_SIZE - 1) / MAX_PAYLOAD_SIZE
        if (fragmentIndex !in 0 until fragmentCount) return null
        val expectedPayload = min(MAX_PAYLOAD_SIZE, frameSize - offset)
        if (payloadLength != expectedPayload) return null
        val start = flags and FLAG_FRAME_START != 0
        val end = flags and FLAG_FRAME_END != 0
        val keyFrame = flags and FLAG_KEY_FRAME != 0
        if (start != (offset == 0) || end != (offset + payloadLength == frameSize)) return null

        val latest = latestCompletedFrameId
        if (latest != null && !isNewer(frameId, latest)) return null

        var assembly = pending[frameId]
        if (assembly == null) {
            if (pending.size >= maxInFlightFrames) {
                val oldest = pending.keys.firstOrNull() ?: return null
                pending.remove(oldest)
            }
            assembly = Assembly(
                frameId = frameId,
                timestampNs = timestampNs,
                frameSize = frameSize,
                keyFrame = keyFrame,
                data = ByteArray(frameSize),
                received = BooleanArray(fragmentCount),
                lastUpdateNs = nowNs,
            )
            pending[frameId] = assembly
        } else if (assembly.timestampNs != timestampNs || assembly.frameSize != frameSize || assembly.keyFrame != keyFrame) {
            pending.remove(frameId)
            return null
        }

        if (assembly.received[fragmentIndex]) return null
        System.arraycopy(datagram, HEADER_SIZE, assembly.data, offset, payloadLength)
        assembly.received[fragmentIndex] = true
        assembly.receivedBytes += payloadLength
        assembly.sawStart = assembly.sawStart || start
        assembly.sawEnd = assembly.sawEnd || end
        assembly.lastUpdateNs = nowNs

        if (!assembly.sawStart || !assembly.sawEnd || assembly.receivedBytes != assembly.frameSize) return null
        if (assembly.received.any { !it }) return null

        pending.remove(frameId)
        val previous = latestCompletedFrameId
        if (previous != null && !isNewer(frameId, previous)) return null
        latestCompletedFrameId = frameId
        // Once a newer frame completes, all older in-flight assemblies are stale.
        pending.keys.toList().filterNot { isNewer(it, frameId) }.forEach(pending::remove)
        return CompletedVideoFrame(frameId, timestampNs, keyFrame, assembly.data)
    }

    @Synchronized
    fun expire(nowNs: Long = System.nanoTime()) {
        val iterator = pending.entries.iterator()
        while (iterator.hasNext()) {
            if (nowNs - iterator.next().value.lastUpdateNs > frameTimeoutNs) {
                iterator.remove()
                expiredAssemblyCount++
            }
        }
    }

    @Synchronized
    fun clear() {
        pending.clear()
        latestCompletedFrameId = null
        expiredAssemblyCount = 0L
    }

    private fun isNewer(candidate: Long, reference: Long): Boolean {
        val delta = (candidate - reference) and 0xffff_ffffL
        return delta != 0L && delta < 0x8000_0000L
    }

    companion object {
        const val HEADER_SIZE = 32
        const val MAX_DATAGRAM_SIZE = 1232
        const val MAX_PAYLOAD_SIZE = MAX_DATAGRAM_SIZE - HEADER_SIZE
        const val FLAG_FRAME_START = 1
        const val FLAG_FRAME_END = 2
        const val FLAG_KEY_FRAME = 4
        private const val KNOWN_FLAGS = FLAG_FRAME_START or FLAG_FRAME_END or FLAG_KEY_FRAME
    }
}
