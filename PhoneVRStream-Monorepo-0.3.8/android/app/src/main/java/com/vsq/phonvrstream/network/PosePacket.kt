package com.vsq.phonvrstream.network

import java.nio.ByteBuffer
import java.nio.ByteOrder

/**
 * Binary UDP pose packet. All multi-byte fields use network byte order (big endian).
 * Layout: PVSP | version:u8 | flags:u8 | reserved:u16 | sequence:u32 |
 *         sensorTimestampNs:i64 | quaternion x/y/z/w:f32
 */
object PosePacket {
    const val VERSION = 1
    const val SIZE_BYTES = 36
    private val MAGIC = byteArrayOf('P'.code.toByte(), 'V'.code.toByte(), 'S'.code.toByte(), 'P'.code.toByte())

    fun encode(sequence: Int, pose: HeadPose): ByteArray {
        val values = floatArrayOf(pose.x, pose.y, pose.z, pose.w)
        require(values.all(Float::isFinite)) { "Pose quaternion contains non-finite values" }
        val norm = kotlin.math.sqrt(values.sumOf { (it * it).toDouble() }).toFloat()
        require(norm in 0.5f..1.5f) { "Pose quaternion is not normalized (norm=$norm)" }

        return ByteBuffer.allocate(SIZE_BYTES).order(ByteOrder.BIG_ENDIAN).apply {
            put(MAGIC)
            put(VERSION.toByte())
            put(0) // flags
            putShort(0) // reserved
            putInt(sequence)
            putLong(pose.timestampNs)
            values.forEach(::putFloat)
        }.array()
    }
}
