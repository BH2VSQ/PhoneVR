package com.vsq.phonvrstream.network

import org.junit.Assert.assertArrayEquals
import org.junit.Assert.assertEquals
import org.junit.Test

class UsbMuxProtocolTest {
    @Test
    fun headerMatchesWindowsHostWireLayout() {
        val header = UsbMuxProtocol.encodeHeader(
            channel = UsbMuxProtocol.CHANNEL_VIDEO,
            payloadLength = 3,
            sequence = 1,
        )
        assertArrayEquals(
            byteArrayOf(
                0x50, 0x56, 0x52, 0x55,
                0x01, 0x03, 0x00, 0x00,
                0x03, 0x00, 0x00, 0x00,
                0x01, 0x00, 0x00, 0x00,
            ),
            header,
        )
        val decoded = UsbMuxProtocol.decodeHeader(header)
        assertEquals(UsbMuxProtocol.CHANNEL_VIDEO, decoded.channel)
        assertEquals(3, decoded.payloadLength)
        assertEquals(1, decoded.sequence)
    }

    @Test
    fun audioChannelsMatchWindowsHost() {
        listOf(UsbMuxProtocol.CHANNEL_AUDIO_DOWNLINK, UsbMuxProtocol.CHANNEL_AUDIO_UPLINK).forEach { channel ->
            val header = UsbMuxProtocol.encodeHeader(channel, 1944, 7)
            val decoded = UsbMuxProtocol.decodeHeader(header)
            assertEquals(channel, decoded.channel)
            assertEquals(1944, decoded.payloadLength)
            assertEquals(7, decoded.sequence)
        }
    }

    @Test(expected = IllegalArgumentException::class)
    fun rejectsInvalidChannel() {
        UsbMuxProtocol.encodeHeader(9, 1, 0)
    }

    @Test(expected = IllegalArgumentException::class)
    fun rejectsOversizedPayload() {
        UsbMuxProtocol.encodeHeader(UsbMuxProtocol.CHANNEL_CONTROL, UsbMuxProtocol.MAX_PAYLOAD_SIZE + 1, 0)
    }

    @Test(expected = IllegalArgumentException::class)
    fun rejectsReservedFlags() {
        val header = UsbMuxProtocol.encodeHeader(UsbMuxProtocol.CHANNEL_CONTROL, 0, 1)
        header[6] = 1
        UsbMuxProtocol.decodeHeader(header)
    }
}
