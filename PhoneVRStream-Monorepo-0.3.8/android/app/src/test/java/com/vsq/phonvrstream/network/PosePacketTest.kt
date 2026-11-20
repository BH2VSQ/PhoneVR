package com.vsq.phonvrstream.network

import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Test
import java.nio.ByteBuffer
import java.nio.ByteOrder

class PosePacketTest {
    @Test
    fun encodesVersionOnePacketInNetworkByteOrder() {
        val pose = HeadPose(123456789L, 0f, 0f, 0f, 1f)
        val packet = PosePacket.encode(42, pose)
        val buffer = ByteBuffer.wrap(packet).order(ByteOrder.BIG_ENDIAN)

        assertEquals(PosePacket.SIZE_BYTES, packet.size)
        assertEquals("PVSP", String(packet, 0, 4, Charsets.US_ASCII))
        buffer.position(4)
        assertEquals(PosePacket.VERSION, buffer.get().toInt())
        buffer.position(8)
        assertEquals(42, buffer.int)
        assertEquals(123456789L, buffer.long)
        assertEquals(0f, buffer.float)
        assertEquals(0f, buffer.float)
        assertEquals(0f, buffer.float)
        assertEquals(1f, buffer.float)
    }

    @Test
    fun rejectsNonFiniteOrNonNormalizedQuaternion() {
        val nonFinite = runCatching {
            PosePacket.encode(1, HeadPose(1L, Float.NaN, 0f, 0f, 1f))
        }
        val nonNormalized = runCatching {
            PosePacket.encode(1, HeadPose(1L, 0f, 0f, 0f, 0.1f))
        }
        assertTrue(nonFinite.isFailure)
        assertTrue(nonNormalized.isFailure)
    }
}
