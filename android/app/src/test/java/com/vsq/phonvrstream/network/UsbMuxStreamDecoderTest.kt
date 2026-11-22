package com.vsq.phonvrstream.network

import org.junit.Assert.assertArrayEquals
import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Test

class UsbMuxStreamDecoderTest {
    @Test
    fun aoaRawReadBufferIsLargeEnoughForMaximumDocumentedPacket() {
        assertEquals(16 * 1024, AOA_READ_BUFFER_SIZE)
    }

    @Test
    fun reconstructsFrameSplitAcrossUsbPacketSizedReads() {
        val payload = ByteArray(521) { (it and 0xFF).toByte() }
        val wire = frame(UsbMuxProtocol.CHANNEL_CONTROL, 1, payload)
        val decoder = UsbMuxStreamDecoder()

        // The Android AOA endpoint uses 512-byte USB packets. Deliberately
        // split the PVRU frame at the same boundary as the failing capture.
        assertTrue(decoder.append(wire, 0, 512).isEmpty())
        val frames = decoder.append(wire, 512, wire.size - 512)

        assertEquals(1, frames.size)
        assertEquals(UsbMuxProtocol.CHANNEL_CONTROL, frames[0].channel)
        assertEquals(1, frames[0].sequence)
        assertArrayEquals(payload, frames[0].payload)
        assertEquals(0, decoder.pendingBytes)
    }

    @Test
    fun reconstructsCoalescedFramesFromOneBulkRead() {
        val first = byteArrayOf(1, 2, 3)
        val second = ByteArray(70) { (it + 10).toByte() }
        val wire = frame(UsbMuxProtocol.CHANNEL_CONTROL, 1, first) +
            frame(UsbMuxProtocol.CHANNEL_CONTROL, 2, second)
        val frames = UsbMuxStreamDecoder().append(wire)

        assertEquals(2, frames.size)
        assertEquals(1, frames[0].sequence)
        assertEquals(2, frames[1].sequence)
        assertArrayEquals(first, frames[0].payload)
        assertArrayEquals(second, frames[1].payload)
    }

    @Test
    fun retainsPartialHeaderAndPayloadAcrossReads() {
        val payload = ByteArray(900) { (it * 3).toByte() }
        val wire = frame(UsbMuxProtocol.CHANNEL_VIDEO, 9, payload)
        val decoder = UsbMuxStreamDecoder()
        val output = ArrayList<UsbMuxStreamDecoder.Frame>()

        var offset = 0
        val chunks = intArrayOf(5, 11, 16, 127, 512, 300, 64)
        var chunkIndex = 0
        while (offset < wire.size) {
            val count = minOf(chunks[chunkIndex++ % chunks.size], wire.size - offset)
            output += decoder.append(wire, offset, count)
            offset += count
        }

        assertEquals(1, output.size)
        assertEquals(UsbMuxProtocol.CHANNEL_VIDEO, output[0].channel)
        assertEquals(9, output[0].sequence)
        assertArrayEquals(payload, output[0].payload)
        assertEquals(0, decoder.pendingBytes)
    }

    @Test
    fun rejectsInvalidHeaderWithRawHeaderHex() {
        val decoder = UsbMuxStreamDecoder()
        val error = runCatching { decoder.append(ByteArray(16)) }.exceptionOrNull()
        assertTrue(error is IllegalArgumentException)
        assertTrue(error?.message?.contains("headerHex=") == true)
    }

    private fun frame(channel: Int, sequence: Int, payload: ByteArray): ByteArray =
        UsbMuxProtocol.encodeHeader(channel, payload.size, sequence) + payload
}
