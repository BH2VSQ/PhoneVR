package com.vsq.phonvrstream.controller

import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNotNull
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test

class DaydreamControllerProtocolTest {
    @Test
    fun rejectsShortNotifications() {
        assertNull(DaydreamControllerProtocol.decode(ByteArray(18), 1L))
    }

    @Test
    fun decodesButtonBitsAndIdentityOrientation() {
        val packet = ByteArray(19)
        packet[18] = (0x01 or 0x04 or 0x02 or 0x10 or 0x08).toByte()

        val decoded = DaydreamControllerProtocol.decode(packet, 123L)
        assertNotNull(decoded)
        val state = decoded!!.state
        assertTrue(state.connected)
        assertTrue(state.click)
        assertTrue(state.app)
        assertTrue(state.home)
        assertTrue(state.volumeUp)
        assertTrue(state.volumeDown)
        assertFalse(state.isTouching)
        assertTrue(state.hasOrientation)
        assertEquals(0f, state.orientationX, 1e-6f)
        assertEquals(0f, state.orientationY, 1e-6f)
        assertEquals(0f, state.orientationZ, 1e-6f)
        assertEquals(1f, state.orientationW, 1e-6f)
    }

    @Test
    fun decodesSequenceAndPackedTouchCoordinates() {
        val packet = ByteArray(19)
        packet[0] = 0x2a
        packet[1] = 0x7c
        packet[16] = 0x1f
        packet[17] = 0xff.toByte()
        packet[18] = 0xff.toByte()

        val state = DaydreamControllerProtocol.decode(packet, 456L)!!.state
        assertEquals((0x7c ushr 2), state.packetSequence)
        assertEquals(0x2a shl 1, state.deviceTimeTicks)
        assertEquals(1f, state.touchX, 0f)
        assertEquals(1f, state.touchY, 0f)
        assertTrue(state.isTouching)
    }

    @Test
    fun accelerationAndGyroscopeAreFinite() {
        val state = DaydreamControllerProtocol.decode(ByteArray(19), 789L)!!.state
        listOf(
            state.accelerometerX, state.accelerometerY, state.accelerometerZ,
            state.gyroscopeX, state.gyroscopeY, state.gyroscopeZ,
        ).forEach { assertTrue(it.isFinite()) }
    }
}
