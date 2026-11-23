package com.vsq.phonvrstream.streaming

import org.junit.Assert.assertArrayEquals
import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Test
import java.nio.ByteBuffer
import java.nio.ByteOrder

class VideoFragmentReassemblerTest {
    @Test fun rebuildsOutOfOrderFragmentsOnlyWhenFrameIsComplete() {
        val data = ByteArray(VideoFragmentReassembler.MAX_PAYLOAD_SIZE * 2 + 17) { (it % 251).toByte() }
        val packets = fragment(data, 7, key = true)
        val receiver = VideoFragmentReassembler()
        assertTrue(receiver.accept(packets[2], nowNs = 1).isEmpty())
        assertTrue(receiver.accept(packets[0], nowNs = 2).isEmpty())
        val frame = receiver.accept(packets[1], nowNs = 3).single()
        assertTrue(frame.keyFrame)
        assertEquals(7L, frame.frameId)
        assertArrayEquals(data, frame.bytes)
    }

    @Test fun requestsOnlyMissingFragmentIndexesForIncompleteFrame() {
        val data = ByteArray(VideoFragmentReassembler.MAX_PAYLOAD_SIZE * 3 + 5) { 5 }
        val packets = fragment(data, 8, key = false)
        val receiver = VideoFragmentReassembler(frameTimeoutNs = 500_000_000L, reorderGraceNs = 50_000_000L)
        receiver.accept(packets[0], nowNs = 1_000_000L)
        receiver.accept(packets[2], nowNs = 2_000_000L)

        val first = receiver.takeMissingFragmentRequests(nowNs = 80_000_000L).single()
        assertEquals(8L, first.frameId)
        assertEquals(listOf(1, 3), first.fragmentIndexes)
        assertTrue(receiver.takeMissingFragmentRequests(nowNs = 89_000_000L).isEmpty())
        val second = receiver.takeMissingFragmentRequests(nowNs = 100_000_000L).single()
        assertEquals(listOf(1, 3), second.fragmentIndexes)
        assertTrue(receiver.takeMissingFragmentRequests(nowNs = 120_000_000L).isEmpty())
    }

    @Test fun completesAndDeliversFramesInFrameIdOrderAcrossDifferentPathLatency() {
        val oldData = ByteArray(VideoFragmentReassembler.MAX_PAYLOAD_SIZE * 2 + 7) { 1 }
        val newData = ByteArray(VideoFragmentReassembler.MAX_PAYLOAD_SIZE * 2 + 9) { 2 }
        val oldPackets = fragment(oldData, 100, key = false)
        val newPackets = fragment(newData, 101, key = false)
        val receiver = VideoFragmentReassembler(frameTimeoutNs = 500_000_000L, reorderGraceNs = 90_000_000L)

        assertTrue(receiver.accept(oldPackets[0], nowNs = 1_000_000L).isEmpty())
        assertTrue(receiver.accept(newPackets[0], nowNs = 2_000_000L).isEmpty())
        assertTrue(receiver.accept(newPackets[1], nowNs = 3_000_000L).isEmpty())
        assertTrue(receiver.accept(newPackets[2], nowNs = 4_000_000L).isEmpty())
        assertTrue(receiver.drainCompleted(nowNs = 50_000_000L).isEmpty())

        val ordered = receiver.accept(oldPackets[1], nowNs = 70_000_000L)
        assertTrue(ordered.isEmpty())
        val tail = receiver.accept(oldPackets[2], nowNs = 71_000_000L)
        assertEquals(listOf(100L, 101L), tail.map { it.frameId })
        assertArrayEquals(oldData, tail[0].bytes)
        assertArrayEquals(newData, tail[1].bytes)
    }

    @Test fun holdsACompleteFrameBrieflyWhenAnEntirePreviousFrameHasNotArrivedYet() {
        val previousData = ByteArray(100) { 3 }
        val currentData = ByteArray(120) { 4 }
        val baseline = fragment(ByteArray(80) { 2 }, 299, key = false).single()
        val previous = fragment(previousData, 300, key = false).single()
        val current = fragment(currentData, 301, key = false).single()
        val receiver = VideoFragmentReassembler(reorderGraceNs = 120_000_000L)
        assertEquals(listOf(299L), receiver.accept(baseline, nowNs = 500_000L).map { it.frameId })

        // Frame 300 is wholly delayed on the slower path. The receiver has no
        // partial assembly for it when frame 301 arrives over the fast path.
        assertTrue(receiver.accept(current, nowNs = 1_000_000L).isEmpty())
        assertTrue(receiver.drainCompleted(nowNs = 80_000_000L).isEmpty())
        assertEquals(listOf(301L), receiver.drainCompleted(nowNs = 130_000_000L).map { it.frameId })
        assertTrue(receiver.accept(previous, nowNs = 140_000_000L).isEmpty())
    }

    @Test fun networkOnlyReorderWindowCanReleaseAtSixtyMilliseconds() {
        val receiver = VideoFragmentReassembler(reorderGraceNs = 60_000_000L)
        val baseline = fragment(ByteArray(80) { 2 }, 398, key = false).single()
        val current = fragment(ByteArray(120) { 4 }, 400, key = false).single()
        assertEquals(listOf(398L), receiver.accept(baseline, nowNs = 1_000_000L).map { it.frameId })
        assertTrue(receiver.accept(current, nowNs = 2_000_000L).isEmpty())
        assertTrue(receiver.drainCompleted(nowNs = 61_000_000L).isEmpty())
        assertEquals(listOf(400L), receiver.drainCompleted(nowNs = 63_000_000L).map { it.frameId })
    }

    @Test fun requestsAndRepairsACompletelyMissingFrame() {
        val receiver = VideoFragmentReassembler(reorderGraceNs = 120_000_000L)
        val baseline = fragment(ByteArray(80) { 2 }, 299, key = false).single()
        val missing = fragment(ByteArray(100) { 3 }, 300, key = false).single()
        val current = fragment(ByteArray(120) { 4 }, 301, key = false).single()

        assertEquals(listOf(299L), receiver.accept(baseline, nowNs = 1_000_000L).map { it.frameId })
        assertTrue(receiver.accept(current, nowNs = 2_000_000L).isEmpty())

        val firstNack = receiver.takeMissingFragmentRequests(nowNs = 40_000_000L).single()
        assertEquals(300L, firstNack.frameId)
        assertEquals(listOf(VideoFragmentReassembler.WHOLE_FRAME_FRAGMENT_INDEX), firstNack.fragmentIndexes)
        val secondNack = receiver.takeMissingFragmentRequests(nowNs = 80_000_000L).single()
        assertEquals(firstNack, secondNack)
        assertTrue(receiver.takeMissingFragmentRequests(nowNs = 120_000_000L).isEmpty())

        // The Host's full-frame repair arrives before the reorder deadline.
        val repaired = receiver.accept(missing, nowNs = 90_000_000L)
        assertEquals(listOf(300L, 301L), repaired.map { it.frameId })
    }

    @Test fun expiresOldIncompleteFrameAfterNackGraceAndReleasesNewerCompleteFrame() {
        val oldData = ByteArray(VideoFragmentReassembler.MAX_PAYLOAD_SIZE * 2 + 3) { 9 }
        val newData = ByteArray(VideoFragmentReassembler.MAX_PAYLOAD_SIZE + 3) { 7 }
        val oldPackets = fragment(oldData, 200, key = false)
        val newPackets = fragment(newData, 201, key = false)
        val receiver = VideoFragmentReassembler(frameTimeoutNs = 500_000_000L, reorderGraceNs = 75_000_000L)
        receiver.accept(oldPackets[0], nowNs = 1_000_000L)
        receiver.accept(newPackets[0], nowNs = 2_000_000L)
        receiver.accept(newPackets[1], nowNs = 3_000_000L)

        assertTrue(receiver.drainCompleted(nowNs = 50_000_000L).isEmpty())
        receiver.expire(nowNs = 600_000_000L)
        val released = receiver.drainCompleted(nowNs = 600_000_000L)
        assertEquals(listOf(201L), released.map { it.frameId })
        assertEquals(1L, receiver.takeExpiredAssemblyCount())
    }

    private fun fragment(data: ByteArray, frameId: Int, key: Boolean): List<ByteArray> {
        val size = VideoFragmentReassembler.MAX_PAYLOAD_SIZE
        val count = (data.size + size - 1) / size
        return (0 until count).map { index ->
            val offset = index * size
            val length = minOf(size, data.size - offset)
            val flags = (if (index == 0) 1 else 0) or
                (if (index == count - 1) 2 else 0) or (if (key) 4 else 0)
            val packet = ByteBuffer.allocate(32 + length).order(ByteOrder.BIG_ENDIAN)
            packet.put(byteArrayOf('P'.code.toByte(), 'V'.code.toByte(), 'R'.code.toByte(), 'V'.code.toByte()))
            packet.put(1.toByte()).put(flags.toByte()).putShort(32.toShort())
            packet.putInt(frameId).putLong(1234L).putInt(data.size).putInt(offset)
            packet.putShort(length.toShort()).putShort(0.toShort())
            packet.put(data, offset, length)
            packet.array()
        }
    }
}
