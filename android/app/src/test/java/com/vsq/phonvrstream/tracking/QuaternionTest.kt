package com.vsq.phonvrstream.tracking

import org.junit.Assert.assertTrue
import org.junit.Test

class QuaternionTest {
    @Test
    fun alignedToRemovesAntipodalSignFlip() {
        val previous = Quaternion(0.0f, 0.70710677f, 0.0f, 0.70710677f)
        val rawEquivalent = Quaternion(0.0f, -0.70710677f, 0.0f, -0.70710677f)
        val aligned = rawEquivalent.alignedTo(previous)
        val dot = aligned.x * previous.x + aligned.y * previous.y +
            aligned.z * previous.z + aligned.w * previous.w
        assertTrue("quaternion must stay in the same hemisphere", dot > 0.999f)
    }

    @Test
    fun alignedToAlwaysReturnsNormalizedQuaternion() {
        val aligned = Quaternion(0.2f, -0.3f, 0.4f, -0.8f)
            .alignedTo(Quaternion.IDENTITY)
        val norm = kotlin.math.sqrt(
            aligned.x * aligned.x + aligned.y * aligned.y +
                aligned.z * aligned.z + aligned.w * aligned.w,
        )
        assertTrue(kotlin.math.abs(norm - 1.0f) < 1e-5f)
        assertTrue(aligned.w >= 0f)
    }
}
