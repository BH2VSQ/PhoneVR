package com.vsq.phonvrstream.network

import org.junit.Assert.assertArrayEquals
import org.junit.Assert.assertEquals
import org.junit.Assert.assertNull
import org.junit.Test

class AudioPacketTest {
    @Test
    fun headerMatchesWindowsHostWireLayout() {
        val frame = AudioPacket.Frame(
            AudioPacket.DIRECTION_HOST_TO_PHONE, 2, 42, 0x0102030405060708L,
            AudioPacket.SAMPLES_PER_CHANNEL,
            ByteArray(AudioPacket.SAMPLES_PER_CHANNEL * 4),
        )
        val header = AudioPacket.encode(frame).copyOfRange(0, AudioPacket.HEADER_SIZE)
        assertArrayEquals(
            byteArrayOf(
                0x50, 0x56, 0x52, 0x41,
                0x01, 0x01, 0x02, 0x01,
                0x2a, 0x00, 0x00, 0x00,
                0x08, 0x07, 0x06, 0x05, 0x04, 0x03, 0x02, 0x01,
                0xc0.toByte(), 0x03, 0x00, 0x0f,
            ),
            header,
        )
    }

    @Test
    fun roundTripsBothDirections() {
        listOf(
            AudioPacket.Frame(
                AudioPacket.DIRECTION_HOST_TO_PHONE, 2, 42, 123456789L,
                AudioPacket.SAMPLES_PER_CHANNEL,
                ByteArray(AudioPacket.SAMPLES_PER_CHANNEL * 4) { (it * 31).toByte() },
            ),
            AudioPacket.Frame(
                AudioPacket.DIRECTION_PHONE_TO_HOST, 1, 43, 987654321L,
                AudioPacket.SAMPLES_PER_CHANNEL,
                ByteArray(AudioPacket.SAMPLES_PER_CHANNEL * 2) { (it * 17).toByte() },
            ),
        ).forEach { expected ->
            val actual = AudioPacket.decode(AudioPacket.encode(expected))
            requireNotNull(actual)
            assertEquals(expected.direction, actual.direction)
            assertEquals(expected.channels, actual.channels)
            assertEquals(expected.sequence, actual.sequence)
            assertEquals(expected.timestampNs, actual.timestampNs)
            assertArrayEquals(expected.pcm, actual.pcm)
        }
    }

    @Test
    fun rejectsMalformedPacket() {
        val packet = AudioPacket.encode(
            AudioPacket.Frame(
                AudioPacket.DIRECTION_HOST_TO_PHONE, 2, 1, 1L,
                AudioPacket.SAMPLES_PER_CHANNEL,
                ByteArray(AudioPacket.SAMPLES_PER_CHANNEL * 4),
            ),
        )
        assertNull(AudioPacket.decode(packet.copyOf(packet.size - 1)))
        val wrongVersion = packet.copyOf().also { it[4] = 2 }
        assertNull(AudioPacket.decode(wrongVersion))
    }
}
