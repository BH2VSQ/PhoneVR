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
    fun daydreamView2016ProfilePreservesItsPublishedDistortionCoefficients() {
        val value = DisplaySettings().withDaydreamView2016Profile()

        assertEquals(DisplaySettings.PROFILE_DAYDREAM_VIEW_2016, value.viewerProfileId)
        assertEquals(0.01f, value.distortionK1, 0.0001f)
        assertEquals(0.67f, value.distortionK2, 0.0001f)
        assertEquals(DisplaySettings().lensSeparationMm, value.lensSeparationMm, 0.0001f)
        assertEquals(DisplaySettings().screenToLensDistanceMm, value.screenToLensDistanceMm, 0.0001f)
        assertEquals(DisplaySettings().fovDegrees, value.fovDegrees, 0.0001f)
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
    fun stereoMappingAndPerEyeOffsetsArePersistableAndBounded() {
        val original = DisplaySettings(
            swapEyes = true,
            leftEyeOffsetX = -0.025f,
            leftEyeOffsetY = 0.012f,
            rightEyeOffsetX = 0.04f,
            rightEyeOffsetY = -0.018f,
            verticalTiltDegrees = 7.5f,
        )
        val restored = DisplaySettings.fromJson(original.toJson())
        assertEquals(original, restored)

        val bounded = original.copy(
            leftEyeOffsetX = -1f,
            leftEyeOffsetY = 1f,
            rightEyeOffsetX = 0.5f,
            rightEyeOffsetY = -0.5f,
            verticalTiltDegrees = 99f,
        ).normalized()
        assertEquals(-DisplaySettings.MAX_EYE_OFFSET, bounded.leftEyeOffsetX)
        assertEquals(DisplaySettings.MAX_EYE_OFFSET, bounded.leftEyeOffsetY)
        assertEquals(DisplaySettings.MAX_EYE_OFFSET, bounded.rightEyeOffsetX)
        assertEquals(-DisplaySettings.MAX_EYE_OFFSET, bounded.rightEyeOffsetY)
        assertEquals(DisplaySettings.MAX_VERTICAL_TILT_DEGREES, bounded.verticalTiltDegrees)
    }

    @Test
    fun olderRemoteSettingsPreserveFallbackStereoMapping() {
        val fallback = DisplaySettings(swapEyes = true, leftEyeOffsetX = 0.02f, verticalTiltDegrees = -4.5f)
        val restored = DisplaySettings.fromJson(org.json.JSONObject().put("ipdMm", 65.0), fallback)
        assertEquals(true, restored.swapEyes)
        assertEquals(0.02f, restored.leftEyeOffsetX, 0.0001f)
        assertEquals(-4.5f, restored.verticalTiltDegrees, 0.0001f)
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
