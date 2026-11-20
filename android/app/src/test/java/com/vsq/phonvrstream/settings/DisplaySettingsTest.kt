package com.vsq.phonvrstream.settings

import org.junit.Assert.assertEquals
import org.junit.Test

class DisplaySettingsTest {
    @Test
    fun normalizedClampsIpdAndVisualParameters() {
        val value = DisplaySettings(
            ipdMm = 120f,
            distortionK1 = 4f,
            blur = -1f,
            sharpness = 2f,
            fovDegrees = 20f,
        ).normalized()

        assertEquals(75f, value.ipdMm)
        assertEquals(0.60f, value.distortionK1)
        assertEquals(0f, value.blur)
        assertEquals(1f, value.sharpness)
        assertEquals(60f, value.fovDegrees)
    }

    @Test
    fun daydreamView2017ProfileUsesPublishedDistortionCoefficients() {
        val value = DisplaySettings(ipdMm = 66f, lensCenterX = 0.52f)
            .withDaydreamView2017Profile()

        assertEquals(DisplaySettings.PROFILE_DAYDREAM_VIEW_2017, value.viewerProfileId)
        assertEquals(0.4331f, value.distortionK1, 0.0001f)
        assertEquals(-0.085f, value.distortionK2, 0.0001f)
        assertEquals(64f, value.lensSeparationMm, 0.0001f)
        assertEquals(66f, value.ipdMm)
        assertEquals(0.52f, value.lensCenterX)
    }

    @Test
    fun physicalViewerGeometryIsPersistableAndNormalized() {
        val value = DisplaySettings(
            screenToLensDistanceMm = 12f,
            lensSeparationMm = 95f,
            screenVerticalAlignment = "unknown",
            trayToLensCenterMm = 90f,
        ).normalized()

        assertEquals(DisplaySettings.MIN_SCREEN_TO_LENS_MM, value.screenToLensDistanceMm)
        assertEquals(DisplaySettings.MAX_LENS_SEPARATION_MM, value.lensSeparationMm)
        assertEquals(DisplaySettings.ALIGNMENT_CENTER, value.screenVerticalAlignment)
        assertEquals(DisplaySettings.MAX_TRAY_TO_LENS_CENTER_MM, value.trayToLensCenterMm)
    }

    @Test
    fun physicalLensSpacingProducesMirroredPerEyeCenters() {
        val settings = DisplaySettings(lensSeparationMm = 64f).normalized()
        assertEquals(0.6f, settings.lensCenterXForEye(true, 160f), 0.0001f)
        assertEquals(0.4f, settings.lensCenterXForEye(false, 160f), 0.0001f)

        val adjusted = settings.copy(lensCenterX = 0.53f)
        assertEquals(0.63f, adjusted.lensCenterXForEye(true, 160f), 0.0001f)
        assertEquals(0.43f, adjusted.lensCenterXForEye(false, 160f), 0.0001f)
    }

    @Test
    fun defaultsAreWithinSupportedRanges() {
        val value = DisplaySettings().normalized()
        assertEquals(63f, value.ipdMm)
        assertEquals(63.9f, value.lensSeparationMm, 0.0001f)
        assertEquals(40f, value.screenToLensDistanceMm)
        assertEquals(DisplaySettings.ALIGNMENT_CENTER, value.screenVerticalAlignment)
        assertEquals(90f, value.fovDegrees)
    }
}
