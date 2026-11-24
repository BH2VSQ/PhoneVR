package com.vsq.phonvrstream.controller

import com.vsq.phonvrstream.tracking.Quaternion
import kotlin.math.PI
import kotlin.math.cos
import kotlin.math.sin
import kotlin.math.sqrt

/** Decoder for the 19-byte BLE notification used by the original Daydream remote. */
object DaydreamControllerProtocol {
    const val MIN_PACKET_SIZE = 19
    const val SERVICE_UUID = "0000fe55-0000-1000-8000-00805f9b34fb"
    const val REPORT_CHARACTERISTIC_UUID = "00000001-1000-1000-8000-00805f9b34fb"
    const val CONTROL_CHARACTERISTIC_UUID = "00000002-1000-1000-8000-00805f9b34fb"
    const val CLIENT_CONFIGURATION_UUID = "00002902-0000-1000-8000-00805f9b34fb"

    data class Decoded(
        val state: ControllerInputState,
        val accelerometerX: Float,
        val accelerometerY: Float,
        val accelerometerZ: Float,
        val gyroscopeX: Float,
        val gyroscopeY: Float,
        val gyroscopeZ: Float,
    )

    fun decode(packet: ByteArray, timestampNs: Long): Decoded? {
        if (packet.size < MIN_PACKET_SIZE) return null
        val timeTicks = ((u8(packet, 0) shl 1) or ((u8(packet, 1) and 0x80) ushr 7))
        val sequence = (u8(packet, 1) and 0x7c) ushr 2

        val xOrientation = signed13(
            ((u8(packet, 1) and 0x03) shl 11) or
                (u8(packet, 2) shl 3) or
                ((u8(packet, 3) and 0x80) ushr 5),
        ) * (2.0 * PI / 4095.0)
        // XKF3 packs the rotation vector in X, Z, Y order.
        val zOrientation = signed13(
            ((u8(packet, 3) and 0x1f) shl 8) or u8(packet, 4),
        ) * (2.0 * PI / 4095.0)
        val yOrientation = signed13(
            (u8(packet, 5) shl 5) or ((u8(packet, 6) and 0xf8) ushr 3),
        ) * (2.0 * PI / 4095.0)

        val xAcceleration = signed13(
            ((u8(packet, 6) and 0x07) shl 10) or
                (u8(packet, 7) shl 2) or
                ((u8(packet, 8) and 0xc0) ushr 6),
        ) * (8.0 * 9.8 / 4095.0)
        val zAcceleration = signed13(
            ((u8(packet, 8) and 0x3f) shl 7) or ((u8(packet, 9) and 0xfe) ushr 1),
        ) * (8.0 * 9.8 / 4095.0)
        val yAcceleration = signed13(
            ((u8(packet, 9) and 0x01) shl 12) or
                (u8(packet, 10) shl 4) or
                ((u8(packet, 11) and 0xf0) ushr 4),
        ) * (8.0 * 9.8 / 4095.0)

        val gyroScale = 2048.0 / 180.0 * PI / 4095.0
        val xGyroscope = signed13(
            ((u8(packet, 11) and 0x0f) shl 9) or
                (u8(packet, 12) shl 1) or
                ((u8(packet, 13) and 0x80) ushr 7),
        ) * gyroScale
        val zGyroscope = signed13(
            ((u8(packet, 13) and 0x7f) shl 6) or ((u8(packet, 14) and 0xfc) ushr 2),
        ) * gyroScale
        val yGyroscope = signed13(
            ((u8(packet, 14) and 0x03) shl 11) or
                (u8(packet, 15) shl 3) or
                ((u8(packet, 16) and 0xe0) ushr 5),
        ) * gyroScale

        val touchX = (((u8(packet, 16) and 0x1f) shl 3) or ((u8(packet, 17) and 0xe0) ushr 5)) / 255f
        val touchY = (((u8(packet, 17) and 0x1f) shl 3) or ((u8(packet, 18) and 0xe0) ushr 5)) / 255f
        val buttons = u8(packet, 18)
        // The BLE firmware sends the XKF3 orientation as an axis-angle rotation vector.
        // The vector direction is the rotation axis and its magnitude is the angle in radians.
        val orientation = fromRotationVector(xOrientation, yOrientation, zOrientation)
        val state = ControllerInputState(
            connected = true,
            timestampNs = timestampNs,
            packetSequence = sequence,
            deviceTimeTicks = timeTicks,
            // The BLE report has no dedicated contact bit. In the observed
            // protocol, both coordinate bytes are zero while untouched and
            // both become non-zero whenever a finger contacts the pad.
            isTouching = touchX > 0f && touchY > 0f,
            touchX = touchX,
            touchY = touchY,
            click = (buttons and 0x01) != 0,
            app = (buttons and 0x04) != 0,
            home = (buttons and 0x02) != 0,
            volumeUp = (buttons and 0x10) != 0,
            volumeDown = (buttons and 0x08) != 0,
            orientationX = orientation.x,
            orientationY = orientation.y,
            orientationZ = orientation.z,
            orientationW = orientation.w,
            hasOrientation = true,
            accelerometerX = xAcceleration.toFloat(),
            accelerometerY = yAcceleration.toFloat(),
            accelerometerZ = zAcceleration.toFloat(),
            gyroscopeX = xGyroscope.toFloat(),
            gyroscopeY = yGyroscope.toFloat(),
            gyroscopeZ = zGyroscope.toFloat(),
        ).normalized()
        return Decoded(
            state = state,
            accelerometerX = xAcceleration.toFloat(),
            accelerometerY = yAcceleration.toFloat(),
            accelerometerZ = zAcceleration.toFloat(),
            gyroscopeX = xGyroscope.toFloat(),
            gyroscopeY = yGyroscope.toFloat(),
            gyroscopeZ = zGyroscope.toFloat(),
        )
    }

    private fun u8(data: ByteArray, index: Int): Int = data[index].toInt() and 0xff

    private fun signed13(value: Int): Int {
        val masked = value and 0x1fff
        return if ((masked and 0x1000) != 0) masked - 0x2000 else masked
    }

    private fun fromRotationVector(x: Double, y: Double, z: Double): Quaternion {
        val angle = sqrt(x * x + y * y + z * z)
        if (!angle.isFinite() || angle < 1.0e-8) return Quaternion(0f, 0f, 0f, 1f)
        val halfAngle = angle * 0.5
        val axisScale = sin(halfAngle) / angle
        return Quaternion(
            (x * axisScale).toFloat(),
            (y * axisScale).toFloat(),
            (z * axisScale).toFloat(),
            cos(halfAngle).toFloat(),
        ).normalized()
    }
}
