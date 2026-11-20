package com.vsq.phonvrstream.controller

import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test

class ControllerInputStateTest {
    @Test
    fun normalizedClampsTouchpadCoordinates() {
        val normalized = ControllerInputState(
            connected = true,
            timestampNs = 1L,
            isTouching = true,
            touchX = -0.2f,
            touchY = 1.4f,
        ).normalized()

        assertEquals(0f, normalized.touchX, 0f)
        assertEquals(1f, normalized.touchY, 0f)
        assertTrue(normalized.connected)
    }

    @Test
    fun controllerOrientationIsOptionalAndSeparateFromHmd() {
        val disconnected = ControllerInputState(connected = false, timestampNs = 2L)
        assertFalse(disconnected.hasOrientation)
        assertEquals(1f, disconnected.orientationW, 0f)

        val tracked = disconnected.copy(
            connected = true,
            hasOrientation = true,
            orientationY = 0.70710677f,
            orientationW = 0.70710677f,
            click = true,
        )
        assertTrue(tracked.hasOrientation)
        assertTrue(tracked.click)
        assertEquals(0.70710677f, tracked.orientationY, 0f)
    }

    @Test
    fun elbowModelPositionIsExplicitlyOptional() {
        val state = ControllerInputState(connected = true, timestampNs = 3L)
        assertFalse(state.hasPosition)
        assertEquals(0f, state.positionX, 0f)
    }
}
