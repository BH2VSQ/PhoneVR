package com.vsq.phonvrstream.streaming

import java.nio.ByteBuffer
import java.nio.ByteOrder
import java.util.LinkedHashMap
import kotlin.math.min

/** One complete Annex-B H.264 access unit reconstructed from PVRV fragments. */
data class CompletedVideoFrame(
    val frameId: Long,
    val timestampNs: Long,
    val keyFrame: Boolean,
    val bytes: ByteArray,
)

data class MissingVideoFragments(
    val frameId: Long,
    val fragmentIndexes: List<Int>,
)

/**
 * Bounded, out-of-order tolerant fragment reassembly for the aggregated UDP/AOA
 * video paths. Frames may complete out of order because the two transports have
 * different latency. A short reorder window keeps H.264 access units in encoder
 * order while NACK feedback gives the slower path time to deliver missing parts.
 */
class VideoFragmentReassembler(
    private val maxInFlightFrames: Int = 8,
    private val maxFrameBytes: Int = 8 * 1024 * 1024,
    private val frameTimeoutNs: Long = 350_000_000L,
    private val reorderGraceNs: Long = 120_000_000L,
) {
    private data class Assembly(
        val frameId: Long,
        val timestampNs: Long,
        val frameSize: Int,
        val keyFrame: Boolean,
        val data: ByteArray,
        val received: BooleanArray,
        val createdNs: Long,
        var receivedBytes: Int = 0,
        var lastUpdateNs: Long,
        var lastNackNs: Long = 0L,
        var nackCount: Int = 0,
    )

    private data class CompletedEntry(
        val frame: CompletedVideoFrame,
        val completedNs: Long,
    )

    private data class WholeFrameNackState(
        var lastSentNs: Long = 0L,
        var rounds: Int = 0,
    )

    private val pending = LinkedHashMap<Long, Assembly>()
    private val completed = LinkedHashMap<Long, CompletedEntry>()
    private val wholeFrameNacks = LinkedHashMap<Long, WholeFrameNackState>()
    private var latestDeliveredFrameId: Long? = null
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
    fun accept(
        datagram: ByteArray,
        length: Int = datagram.size,
        nowNs: Long = System.nanoTime(),
    ): List<CompletedVideoFrame> {
        expire(nowNs)
        if (length < HEADER_SIZE || length > MAX_DATAGRAM_SIZE || length > datagram.size) return emptyList()
        if (datagram[0] != 'P'.code.toByte() || datagram[1] != 'V'.code.toByte() ||
            datagram[2] != 'R'.code.toByte() || datagram[3] != 'V'.code.toByte()) return emptyList()
        if (datagram[4].toInt() and 0xff != 1) return emptyList()
        val flags = datagram[5].toInt() and 0xff
        if (flags and KNOWN_FLAGS.inv() != 0) return emptyList()

        val header = ByteBuffer.wrap(datagram, 6, HEADER_SIZE - 6).order(ByteOrder.BIG_ENDIAN)
        if (header.short.toInt() and 0xffff != HEADER_SIZE) return emptyList()
        val frameId = header.int.toLong() and 0xffff_ffffL
        val timestampNs = header.long
        val frameSize = header.int
        val offset = header.int
        val payloadLength = header.short.toInt() and 0xffff
        val reserved = header.short.toInt() and 0xffff
        if (reserved != 0 || payloadLength <= 0 || payloadLength != length - HEADER_SIZE) return emptyList()
        if (frameSize <= 0 || frameSize > maxFrameBytes || offset < 0 || offset >= frameSize) return emptyList()
        if (offset % MAX_PAYLOAD_SIZE != 0) return emptyList()
        val fragmentIndex = offset / MAX_PAYLOAD_SIZE
        val fragmentCount = (frameSize + MAX_PAYLOAD_SIZE - 1) / MAX_PAYLOAD_SIZE
        if (fragmentIndex !in 0 until fragmentCount) return emptyList()
        val expectedPayload = min(MAX_PAYLOAD_SIZE, frameSize - offset)
        if (payloadLength != expectedPayload) return emptyList()
        val start = flags and FLAG_FRAME_START != 0
        val end = flags and FLAG_FRAME_END != 0
        val keyFrame = flags and FLAG_KEY_FRAME != 0
        if (start != (offset == 0) || end != (offset + payloadLength == frameSize)) return emptyList()

        val latest = latestDeliveredFrameId
        if (latest != null && !isNewer(frameId, latest)) return emptyList()
        if (completed.containsKey(frameId)) return emptyList()
        // A previously absent frame can arrive as a full-frame NACK repair.
        // Once any packet arrives, partial-fragment NACKs take over.
        wholeFrameNacks.remove(frameId)

        var assembly = pending[frameId]
        if (assembly == null) {
            if (pending.size >= maxInFlightFrames) {
                val oldest = pending.values.minByOrNull { it.createdNs } ?: return emptyList()
                pending.remove(oldest.frameId)
                expiredAssemblyCount++
            }
            assembly = Assembly(
                frameId = frameId,
                timestampNs = timestampNs,
                frameSize = frameSize,
                keyFrame = keyFrame,
                data = ByteArray(frameSize),
                received = BooleanArray(fragmentCount),
                createdNs = nowNs,
                lastUpdateNs = nowNs,
            )
            pending[frameId] = assembly
        } else if (assembly.timestampNs != timestampNs || assembly.frameSize != frameSize || assembly.keyFrame != keyFrame) {
            pending.remove(frameId)
            expiredAssemblyCount++
            return emptyList()
        }

        if (assembly.received[fragmentIndex]) return emptyList()
        System.arraycopy(datagram, HEADER_SIZE, assembly.data, offset, payloadLength)
        assembly.received[fragmentIndex] = true
        assembly.receivedBytes += payloadLength
        assembly.lastUpdateNs = nowNs

        if (assembly.receivedBytes != assembly.frameSize || assembly.received.any { !it }) return emptyList()

        pending.remove(frameId)
        completed[frameId] = CompletedEntry(
            CompletedVideoFrame(frameId, timestampNs, keyFrame, assembly.data), nowNs,
        )
        return drainCompleted(nowNs)
    }

    /** Return complete frames in frame-ID order when any older frame has arrived or timed out. */
    @Synchronized
    fun drainCompleted(
        nowNs: Long = System.nanoTime(),
        maxFrames: Int = MAX_COMPLETED_FRAMES_PER_DRAIN,
    ): List<CompletedVideoFrame> {
        val ready = ArrayList<CompletedVideoFrame>()
        val limit = maxFrames.coerceAtLeast(1)
        while (completed.isNotEmpty() && ready.size < limit) {
            val latest = latestDeliveredFrameId
            val candidate = if (latest == null) {
                val oldestCompleteID = oldestFrameId(completed.keys)
                oldestCompleteID?.let(completed::get)
            } else {
                completed.values
                    .filter { isNewer(it.frame.frameId, latest) }
                    .minByOrNull { frameDistance(it.frame.frameId, latest) }
            } ?: break

            val expectedFrameId = latest?.let { (it + 1L) and 0xffff_ffffL }
            val sequenceGap = expectedFrameId != null && candidate.frame.frameId != expectedFrameId
            val olderInFlight = pending.values.filter { isNewer(candidate.frame.frameId, it.frameId) }
            // A whole access unit can be assigned to a slower route only when
            // it has one datagram. Hold later frames briefly even if no packet
            // for that older frame has reached this receiver yet.
            if ((olderInFlight.isNotEmpty() || sequenceGap) &&
                nowNs - candidate.completedNs < reorderGraceNs) break
            if (olderInFlight.isNotEmpty()) {
                olderInFlight.forEach {
                    pending.remove(it.frameId)
                    expiredAssemblyCount++
                }
            }

            completed.remove(candidate.frame.frameId)
            val previous = latestDeliveredFrameId
            if (previous != null && !isNewer(candidate.frame.frameId, previous)) continue
            latestDeliveredFrameId = candidate.frame.frameId
            ready += candidate.frame
            wholeFrameNacks.keys.toList()
                .filterNot { isNewer(it, candidate.frame.frameId) }
                .forEach(wholeFrameNacks::remove)

            // Completed pictures older than the frame just emitted are stale.
            val delivered = candidate.frame.frameId
            completed.keys.toList().filterNot { isNewer(it, delivered) }.forEach(completed::remove)
            pending.values.filter { !isNewer(it.frameId, delivered) }.forEach {
                pending.remove(it.frameId)
                expiredAssemblyCount++
            }
        }
        return ready
    }

    /** Missing fragment and wholly missing frame reports are rate-limited. */
    @Synchronized
    fun takeMissingFragmentRequests(nowNs: Long = System.nanoTime()): List<MissingVideoFragments> {
        val requests = ArrayList<MissingVideoFragments>()
        for (assembly in pending.values) {
            if (assembly.nackCount >= MAX_NACK_ROUNDS) continue
            val reference = maxOf(assembly.lastUpdateNs, assembly.lastNackNs)
            if (nowNs - reference < NACK_DELAY_NS) continue
            val missing = assembly.received.indices.filter { !assembly.received[it] }.take(MAX_NACK_FRAGMENTS)
            if (missing.isEmpty()) continue
            assembly.lastNackNs = nowNs
            assembly.nackCount++
            requests += MissingVideoFragments(assembly.frameId, missing)
        }

        // A complete access unit can be entirely absent from UDP, leaving no
        // partial Assembly from which to derive missing fragment indexes. If a
        // later frame is complete, request the next absent frame ID. Fragment
        // index -2 is the protocol sentinel for retransmitting the entire AU.
        val latest = latestDeliveredFrameId
        if (latest != null && completed.isNotEmpty()) {
            val candidate = completed.values.minByOrNull {
                frameDistance(it.frame.frameId, latest)
            }
            if (candidate != null) {
                val targetFrameId = candidate.frame.frameId
                var missingFrameId = (latest + 1L) and 0xffff_ffffL
                var scanned = 0
                while (missingFrameId != targetFrameId && scanned < MAX_WHOLE_FRAME_NACKS_PER_DRAIN) {
                    if (!pending.containsKey(missingFrameId) && !completed.containsKey(missingFrameId)) {
                        val state = wholeFrameNacks.getOrPut(missingFrameId) { WholeFrameNackState() }
                        val firstRequestReady = nowNs - candidate.completedNs >= NACK_DELAY_NS
                        val retryReady = state.lastSentNs == 0L || nowNs - state.lastSentNs >= NACK_DELAY_NS
                        if (state.rounds < MAX_NACK_ROUNDS && firstRequestReady && retryReady) {
                            state.lastSentNs = nowNs
                            state.rounds++
                            requests += MissingVideoFragments(missingFrameId, listOf(WHOLE_FRAME_FRAGMENT_INDEX))
                        }
                    }
                    missingFrameId = (missingFrameId + 1L) and 0xffff_ffffL
                    scanned++
                }
            }
        }
        return requests
    }

    @Synchronized
    fun expire(nowNs: Long = System.nanoTime()) {
        val expired = pending.values.filter { nowNs - it.createdNs > frameTimeoutNs }
        expired.forEach {
            pending.remove(it.frameId)
            expiredAssemblyCount++
        }
    }

    @Synchronized
    fun clear() {
        pending.clear()
        completed.clear()
        wholeFrameNacks.clear()
        latestDeliveredFrameId = null
        expiredAssemblyCount = 0L
    }

    private fun oldestFrameId(frameIds: Collection<Long>): Long? {
        var oldest: Long? = null
        for (candidate in frameIds) {
            val current = oldest
            if (current == null || isNewer(current, candidate)) oldest = candidate
        }
        return oldest
    }

    private fun frameDistance(candidate: Long, reference: Long): Long =
        (candidate - reference) and 0xffff_ffffL

    private fun isNewer(candidate: Long, reference: Long): Boolean {
        val delta = frameDistance(candidate, reference)
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
        private const val NACK_DELAY_NS = 10_000_000L
        private const val MAX_NACK_ROUNDS = 2
        private const val MAX_NACK_FRAGMENTS = 256
        private const val MAX_WHOLE_FRAME_NACKS_PER_DRAIN = 4
        private const val MAX_COMPLETED_FRAMES_PER_DRAIN = 2
        const val WHOLE_FRAME_FRAGMENT_INDEX = -2
    }
}
