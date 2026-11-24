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
        assertEquals("00000002-1000-1000-8000-00805f9b34fb", DaydreamControllerProtocol.CONTROL_CHARACTERISTIC_UUID)
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
    fun decodesOrientationAsAxisAngleRotationVector() {
        // XKF3 X rotation: 1024 / 4095 turns, approximately a quarter turn.
        val packet = ByteArray(19)
        packet[2] = 0x80.toByte()

        val state = DaydreamControllerProtocol.decode(packet, 321L)!!.state
        val expectedHalfAngle = (1024.0 * 2.0 * Math.PI / 4095.0) * 0.5
        assertEquals(kotlin.math.sin(expectedHalfAngle).toFloat(), state.orientationX, 1e-4f)
        assertEquals(0f, state.orientationY, 1e-6f)
        assertEquals(0f, state.orientationZ, 1e-6f)
        assertEquals(kotlin.math.cos(expectedHalfAngle).toFloat(), state.orientationW, 1e-4f)
    }

    @Test
    fun decodesOrientationYAndZUsingWireAxisOrderXZy() {
        val quarterTurn = 1024
        val packetY = ByteArray(19).apply {
            // The third 13-bit field in XKF3 is Y, carried by bytes 5 and 6.
            this[5] = (quarterTurn ushr 5).toByte()
            this[6] = ((quarterTurn and 0x1f) shl 3).toByte()
        }
        val stateY = DaydreamControllerProtocol.decode(packetY, 322L)!!.state
        val halfAngle = (quarterTurn * 2.0 * Math.PI / 4095.0) * 0.5
        assertEquals(0f, stateY.orientationX, 1e-6f)
        assertEquals(kotlin.math.sin(halfAngle).toFloat(), stateY.orientationY, 1e-4f)
        assertEquals(0f, stateY.orientationZ, 1e-6f)

        val packetZ = ByteArray(19).apply {
            // The second 13-bit field in XKF3 is Z, carried by byte 3 low bits and byte 4.
            this[3] = (quarterTurn ushr 8).toByte()
            this[4] = quarterTurn.toByte()
        }
        val stateZ = DaydreamControllerProtocol.decode(packetZ, 323L)!!.state
        assertEquals(0f, stateZ.orientationX, 1e-6f)
        assertEquals(0f, stateZ.orientationY, 1e-6f)
        assertEquals(kotlin.math.sin(halfAngle).toFloat(), stateZ.orientationZ, 1e-4f)
    }

    @Test
    fun mapsAccelerometerAndGyroscopeWireAxesIntoXyzOrder() {
        val packet = ByteArray(19).apply {
            // accel wire order X, Z, Y: encode 1024 only in Y and Z.
            this[8] = 0x08
            this[10] = 0x40
            // gyro wire order X, Z, Y: encode 1024 only in Y and Z.
            this[13] = 0x10
            this[15] = 0x80.toByte()
        }
        val state = DaydreamControllerProtocol.decode(packet, 324L)!!.state
        val expectedAcceleration = (1024.0 * 8.0 * 9.8 / 4095.0).toFloat()
        val expectedGyroscope = (1024.0 * (2048.0 / 180.0 * Math.PI / 4095.0)).toFloat()
        assertEquals(0f, state.accelerometerX, 1e-6f)
        assertEquals(expectedAcceleration, state.accelerometerY, 1e-4f)
        assertEquals(expectedAcceleration, state.accelerometerZ, 1e-4f)
        assertEquals(0f, state.gyroscopeX, 1e-6f)
        assertEquals(expectedGyroscope, state.gyroscopeY, 1e-4f)
        assertEquals(expectedGyroscope, state.gyroscopeZ, 1e-4f)
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
    fun detectsTouchContactAndDecodesBothAxesFromThePackedWireBytes() {
        val rawX = 128
        val rawY = 192 // Upper half of the Daydream pad: protocol Y increases upward.
        val packet = ByteArray(19).apply {
            this[16] = (rawX ushr 3).toByte()
            this[17] = (((rawX and 0x07) shl 5) or (rawY ushr 3)).toByte()
            this[18] = (((rawY and 0x07) shl 5) or 0x01).toByte()
        }
        val state = DaydreamControllerProtocol.decode(packet, 457L)!!.state
        assertTrue(state.isTouching)
        assertEquals(rawX / 255f, state.touchX, 1e-6f)
        assertEquals(rawY / 255f, state.touchY, 1e-6f)
        assertTrue(state.click)

        val untouched = DaydreamControllerProtocol.decode(ByteArray(19), 458L)!!.state
        assertFalse(untouched.isTouching)
        assertEquals(0f, untouched.touchX, 0f)
        assertEquals(0f, untouched.touchY, 0f)
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
