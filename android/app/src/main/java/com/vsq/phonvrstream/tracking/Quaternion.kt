package com.vsq.phonvrstream.tracking

import kotlin.math.asin
import kotlin.math.atan2

/** Quaternion helpers shared by sensor processing and tracking regression tests. */
internal data class Quaternion(val x: Float, val y: Float, val z: Float, val w: Float) {
    fun normalized(): Quaternion {
        val length = kotlin.math.sqrt(x * x + y * y + z * z + w * w).coerceAtLeast(1e-8f)
        return Quaternion(x / length, y / length, z / length, w / length)
    }

    /**
     * Keep the quaternion in the same hemisphere as the previous sample.
     * q and -q encode the same rotation, but interpolating between antipodal
     * representations can create a spurious full-turn jump in downstream VR runtimes.
     */
    fun alignedTo(reference: Quaternion): Quaternion {
        val candidate = normalized()
        val previous = reference.normalized()
        val dot = candidate.x * previous.x + candidate.y * previous.y +
            candidate.z * previous.z + candidate.w * previous.w
        return if (dot < 0f) {
            Quaternion(-candidate.x, -candidate.y, -candidate.z, -candidate.w)
        } else {
            candidate
        }
    }

    fun toEulerDegrees(): Triple<Float, Float, Float> {
        val q = normalized()
        val sinr = 2f * (q.w * q.x + q.y * q.z)
        val cosr = 1f - 2f * (q.x * q.x + q.y * q.y)
        val roll = atan2(sinr, cosr)
        val sinp = 2f * (q.w * q.y - q.z * q.x)
        val pitch = if (kotlin.math.abs(sinp) >= 1f) {
            Math.copySign((Math.PI / 2.0).toFloat(), sinp)
        } else asin(sinp)
        val siny = 2f * (q.w * q.z + q.x * q.y)
        val cosy = 1f - 2f * (q.y * q.y + q.z * q.z)
        val yaw = atan2(siny, cosy)
        val factor = (180.0 / Math.PI).toFloat()
        return Triple(yaw * factor, pitch * factor, roll * factor)
    }

    companion object {
        val IDENTITY = Quaternion(0f, 0f, 0f, 1f)

        fun inverse(value: Quaternion): Quaternion {
            val q = value.normalized()
            return Quaternion(-q.x, -q.y, -q.z, q.w)
        }

        fun multiply(a: Quaternion, b: Quaternion): Quaternion = Quaternion(
            a.w * b.x + a.x * b.w + a.y * b.z - a.z * b.y,
            a.w * b.y - a.x * b.z + a.y * b.w + a.z * b.x,
            a.w * b.z + a.x * b.y - a.y * b.x + a.z * b.w,
            a.w * b.w - a.x * b.x - a.y * b.y - a.z * b.z,
        ).normalized()

        fun fromRotationMatrix(m: FloatArray): Quaternion {
            require(m.size >= 9) { "A 3x3 rotation matrix is required" }
            val m00 = m[0]; val m01 = m[1]; val m02 = m[2]
            val m10 = m[3]; val m11 = m[4]; val m12 = m[5]
            val m20 = m[6]; val m21 = m[7]; val m22 = m[8]
            val trace = m00 + m11 + m22
            return if (trace > 0f) {
                val s = kotlin.math.sqrt(trace + 1f) * 2f
                Quaternion((m21 - m12) / s, (m02 - m20) / s, (m10 - m01) / s, 0.25f * s)
            } else if (m00 > m11 && m00 > m22) {
                val s = kotlin.math.sqrt(1f + m00 - m11 - m22) * 2f
                Quaternion(0.25f * s, (m01 + m10) / s, (m02 + m20) / s, (m21 - m12) / s)
            } else if (m11 > m22) {
                val s = kotlin.math.sqrt(1f + m11 - m00 - m22) * 2f
                Quaternion((m01 + m10) / s, 0.25f * s, (m12 + m21) / s, (m02 - m20) / s)
            } else {
                val s = kotlin.math.sqrt(1f + m22 - m00 - m11) * 2f
                Quaternion((m02 + m20) / s, (m12 + m21) / s, 0.25f * s, (m10 - m01) / s)
            }.normalized()
        }
    }
}
