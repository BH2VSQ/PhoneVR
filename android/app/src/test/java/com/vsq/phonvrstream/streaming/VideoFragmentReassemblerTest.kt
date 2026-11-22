package com.vsq.phonvrstream.streaming

import org.junit.Assert.assertArrayEquals
import org.junit.Assert.assertEquals
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test
import java.nio.ByteBuffer
import java.nio.ByteOrder

class VideoFragmentReassemblerTest {
    @Test fun rebuildsOutOfOrderFragmentsOnlyWhenFrameIsComplete() {
        val data = ByteArray(VideoFragmentReassembler.MAX_PAYLOAD_SIZE * 2 + 17) { (it % 251).toByte() }
        val packets = fragment(data, 7, key = true)
        val receiver = VideoFragmentReassembler()
        assertNull(receiver.accept(packets[2], nowNs = 1))
        assertNull(receiver.accept(packets[0], nowNs = 2))
        val frame = receiver.accept(packets[1], nowNs = 3)
        assertTrue(frame?.keyFrame == true)
        assertEquals(7L, frame?.frameId)
        assertArrayEquals(data, frame?.bytes)
    }

    @Test fun ignoresDuplicatesAndExpiredIncompleteFrames() {
        val data = ByteArray(VideoFragmentReassembler.MAX_PAYLOAD_SIZE + 10) { 5 }
        val packets = fragment(data, 8, key = false)
        val receiver = VideoFragmentReassembler(frameTimeoutNs = 10)
        assertNull(receiver.accept(packets[0], nowNs = 1))
        assertNull(receiver.accept(packets[0], nowNs = 2))
        receiver.expire(nowNs = 20)
        assertEquals(0, receiver.inFlightCount())
        assertEquals(1L, receiver.takeExpiredAssemblyCount())
        assertEquals(0L, receiver.takeExpiredAssemblyCount())
        assertNull(receiver.accept(packets[1], nowNs = 21))
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
