package com.vsq.phonvrstream.network

import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test

class PoseSendThrottleTest {
    @Test
    fun limitsPoseDatagramsToConfiguredRate() {
        val throttle = PoseSendThrottle(60)
        val start = 1_000_000_000L

        assertTrue(throttle.tryAcquire(1L, start))
        assertFalse(throttle.tryAcquire(2L, start + 5_000_000L))
        assertFalse(throttle.tryAcquire(3L, start + 10_000_000L))
        assertTrue(throttle.tryAcquire(3L, start + 16_666_667L))
        assertFalse(throttle.tryAcquire(3L, start + 40_000_000L))
        assertTrue(throttle.tryAcquire(4L, start + 33_333_334L))
    }

    @Test
    fun coalescesIntermediateSamplesAndReportsRemainingWait() {
        val throttle = PoseSendThrottle(60)
        val start = 2_000_000_000L

        assertTrue(throttle.tryAcquire(10L, start))
        assertFalse(throttle.tryAcquire(11L, start + 2_000_000L))
        assertFalse(throttle.tryAcquire(12L, start + 4_000_000L))
        assertTrue(throttle.nanosUntilNextAllowed(start + 4_000_000L) in 12_666_000L..12_667_000L)
        // The caller provides the newest sample at the next slot. Sample 11 is never queued.
        assertTrue(throttle.tryAcquire(12L, start + 16_666_667L))
    }

    @Test(expected = IllegalArgumentException::class)
    fun rejectsInvalidRate() {
        PoseSendThrottle(0)
    }

    @Test
    fun supportsTwoHundredHertzSensorRate() {
        val throttle = PoseSendThrottle(200)
        assertTrue(throttle.tryAcquire(1L, 1_000_000_000L))
        assertFalse(throttle.tryAcquire(2L, 1_004_999_999L))
        assertTrue(throttle.tryAcquire(2L, 1_005_000_000L))
    }
}
