package com.vsq.phonvrstream.settings

import org.json.JSONObject

/** Optical display parameters stored in a locally persisted optical preset. Distances are millimetres. */
data class DisplaySettings(
    val viewerProfileId: String = PROFILE_CUSTOM,
    /** User eye separation used by the virtual stereo cameras. */
    val ipdMm: Float = 63.0f,
    /** Physical center-to-center spacing between the viewer's two lenses. */
    val lensSeparationMm: Float = 63.9f,
    /** Measured distance from the active display surface to the lens optical center. */
    val screenToLensDistanceMm: Float = 40.0f,
    /** CENTER, BOTTOM, or TOP: how the phone screen is seated in the viewer. */
    val screenVerticalAlignment: String = ALIGNMENT_CENTER,
    /** Distance from the selected tray edge to the lens center, used for TOP/BOTTOM. */
    val trayToLensCenterMm: Float = 35.0f,
    val lensCenterX: Float = 0.5f,
    val lensCenterY: Float = 0.5f,
    val distortionK1: Float = 0.18f,
    val distortionK2: Float = 0.04f,
    val blur: Float = 0.0f,
    val sharpness: Float = 0.35f,
    val fovDegrees: Float = 90.0f,
    /** Select the opposite SBS source half for each physical display eye. */
    val swapEyes: Boolean = false,
    /** Per-physical-eye image translation in normalized single-eye UV units. */
    val leftEyeOffsetX: Float = 0.0f,
    val leftEyeOffsetY: Float = 0.0f,
    val rightEyeOffsetX: Float = 0.0f,
    val rightEyeOffsetY: Float = 0.0f,
    /** Vertical perspective tilt of the displayed image plane, in degrees. */
    val verticalTiltDegrees: Float = 0.0f,
) {
    fun normalized(): DisplaySettings = copy(
        ipdMm = ipdMm.coerceIn(MIN_IPD_MM, MAX_IPD_MM),
        lensSeparationMm = lensSeparationMm.coerceIn(MIN_LENS_SEPARATION_MM, MAX_LENS_SEPARATION_MM),
        screenToLensDistanceMm = screenToLensDistanceMm.coerceIn(MIN_SCREEN_TO_LENS_MM, MAX_SCREEN_TO_LENS_MM),
        screenVerticalAlignment = screenVerticalAlignment.takeIf {
            it == ALIGNMENT_CENTER || it == ALIGNMENT_BOTTOM || it == ALIGNMENT_TOP
        } ?: ALIGNMENT_CENTER,
        trayToLensCenterMm = trayToLensCenterMm.coerceIn(MIN_TRAY_TO_LENS_CENTER_MM, MAX_TRAY_TO_LENS_CENTER_MM),
        lensCenterX = lensCenterX.coerceIn(0.35f, 0.65f),
        lensCenterY = lensCenterY.coerceIn(0.35f, 0.65f),
        distortionK1 = distortionK1.coerceIn(-0.30f, 0.60f),
        // Daydream View v1.0 (2016) uses a much larger positive K2 value.
        // The previous 0.30 ceiling silently clipped its published 0.67 value.
        distortionK2 = distortionK2.coerceIn(MIN_DISTORTION_K2, MAX_DISTORTION_K2),
        blur = blur.coerceIn(0.0f, 1.0f),
        sharpness = sharpness.coerceIn(0.0f, 1.0f),
        fovDegrees = fovDegrees.coerceIn(60.0f, 115.0f),
        leftEyeOffsetX = leftEyeOffsetX.coerceIn(-MAX_EYE_OFFSET, MAX_EYE_OFFSET),
        leftEyeOffsetY = leftEyeOffsetY.coerceIn(-MAX_EYE_OFFSET, MAX_EYE_OFFSET),
        rightEyeOffsetX = rightEyeOffsetX.coerceIn(-MAX_EYE_OFFSET, MAX_EYE_OFFSET),
        rightEyeOffsetY = rightEyeOffsetY.coerceIn(-MAX_EYE_OFFSET, MAX_EYE_OFFSET),
        verticalTiltDegrees = verticalTiltDegrees.coerceIn(-MAX_VERTICAL_TILT_DEGREES, MAX_VERTICAL_TILT_DEGREES),
    )

    fun withDaydreamView2017Profile(): DisplaySettings = copy(
        viewerProfileId = PROFILE_DAYDREAM_VIEW_2017,
        // Published measurements for Google VR's Daydream View v2.0 (2017) default profile.
        lensSeparationMm = 64.0f,
        distortionK1 = 0.4331f,
        distortionK2 = -0.085f,
    ).normalized()

    fun withDaydreamView2016Profile(): DisplaySettings = copy(
        viewerProfileId = PROFILE_DAYDREAM_VIEW_2016,
        // The published 2016 profile coefficients are k1=0.01 and k2=0.67.
        // Keep physical geometry and FOV at the user's generic baseline because
        // the available 64 mm / 100 degree measurements refer to the 2017 model.
        distortionK1 = 0.01f,
        distortionK2 = 0.67f,
    ).normalized()

    fun toJson(): JSONObject = JSONObject()
        .put("viewerProfileId", viewerProfileId)
        .put("ipdMm", ipdMm.toDouble())
        .put("lensSeparationMm", lensSeparationMm.toDouble())
        .put("screenToLensDistanceMm", screenToLensDistanceMm.toDouble())
        .put("screenVerticalAlignment", screenVerticalAlignment)
        .put("trayToLensCenterMm", trayToLensCenterMm.toDouble())
        .put("lensCenterX", lensCenterX.toDouble())
        .put("lensCenterY", lensCenterY.toDouble())
        .put("distortionK1", distortionK1.toDouble())
        .put("distortionK2", distortionK2.toDouble())
        .put("blur", blur.toDouble())
        .put("sharpness", sharpness.toDouble())
        .put("fovDegrees", fovDegrees.toDouble())
        .put("swapEyes", swapEyes)
        .put("leftEyeOffsetX", leftEyeOffsetX.toDouble())
        .put("leftEyeOffsetY", leftEyeOffsetY.toDouble())
        .put("rightEyeOffsetX", rightEyeOffsetX.toDouble())
        .put("rightEyeOffsetY", rightEyeOffsetY.toDouble())
        .put("verticalTiltDegrees", verticalTiltDegrees.toDouble())

    /** Parses remote settings while preserving local defaults for omitted fields. */
    companion object {
        const val PROFILE_CUSTOM = "custom"
        const val PROFILE_DAYDREAM_VIEW_2016 = "daydream_view_2016"
        const val PROFILE_DAYDREAM_VIEW_2017 = "daydream_view_2017"
        const val ALIGNMENT_CENTER = "center"
        const val ALIGNMENT_BOTTOM = "bottom"
        const val ALIGNMENT_TOP = "top"
        const val MIN_IPD_MM = 50.0f
        const val MAX_IPD_MM = 75.0f
        const val MIN_LENS_SEPARATION_MM = 50.0f
        const val MAX_LENS_SEPARATION_MM = 75.0f
        const val MIN_SCREEN_TO_LENS_MM = 25.0f
        const val MAX_SCREEN_TO_LENS_MM = 70.0f
        const val MIN_TRAY_TO_LENS_CENTER_MM = 10.0f
        const val MAX_TRAY_TO_LENS_CENTER_MM = 60.0f
        const val MIN_DISTORTION_K2 = -0.20f
        const val MAX_DISTORTION_K2 = 0.80f
        const val MAX_EYE_OFFSET = 0.20f
        const val MAX_VERTICAL_TILT_DEGREES = 20.0f

        fun fromJson(json: JSONObject, fallback: DisplaySettings = DisplaySettings()): DisplaySettings = DisplaySettings(
            viewerProfileId = json.optString("viewerProfileId", fallback.viewerProfileId),
            ipdMm = json.optDouble("ipdMm", fallback.ipdMm.toDouble()).toFloat(),
            lensSeparationMm = json.optDouble("lensSeparationMm", fallback.lensSeparationMm.toDouble()).toFloat(),
            screenToLensDistanceMm = json.optDouble("screenToLensDistanceMm", fallback.screenToLensDistanceMm.toDouble()).toFloat(),
            screenVerticalAlignment = json.optString("screenVerticalAlignment", fallback.screenVerticalAlignment),
            trayToLensCenterMm = json.optDouble("trayToLensCenterMm", fallback.trayToLensCenterMm.toDouble()).toFloat(),
            lensCenterX = json.optDouble("lensCenterX", fallback.lensCenterX.toDouble()).toFloat(),
            lensCenterY = json.optDouble("lensCenterY", fallback.lensCenterY.toDouble()).toFloat(),
            distortionK1 = json.optDouble("distortionK1", fallback.distortionK1.toDouble()).toFloat(),
            distortionK2 = json.optDouble("distortionK2", fallback.distortionK2.toDouble()).toFloat(),
            blur = json.optDouble("blur", fallback.blur.toDouble()).toFloat(),
            sharpness = json.optDouble("sharpness", fallback.sharpness.toDouble()).toFloat(),
            fovDegrees = json.optDouble("fovDegrees", fallback.fovDegrees.toDouble()).toFloat(),
            swapEyes = json.optBoolean("swapEyes", fallback.swapEyes),
            leftEyeOffsetX = json.optDouble("leftEyeOffsetX", fallback.leftEyeOffsetX.toDouble()).toFloat(),
            leftEyeOffsetY = json.optDouble("leftEyeOffsetY", fallback.leftEyeOffsetY.toDouble()).toFloat(),
            rightEyeOffsetX = json.optDouble("rightEyeOffsetX", fallback.rightEyeOffsetX.toDouble()).toFloat(),
            rightEyeOffsetY = json.optDouble("rightEyeOffsetY", fallback.rightEyeOffsetY.toDouble()).toFloat(),
            verticalTiltDegrees = json.optDouble("verticalTiltDegrees", fallback.verticalTiltDegrees.toDouble()).toFloat(),
        ).normalized()
    }

    /** Returns the optical center inside one half-screen eye viewport. */
    fun lensCenterXForEye(isLeftEye: Boolean, physicalScreenWidthMm: Float): Float {
        val screenWidth = physicalScreenWidthMm.takeIf { it > 1f } ?: (lensSeparationMm * 2f)
        val spacingRatio = lensSeparationMm / screenWidth
        val physicalCenter = if (isLeftEye) 1f - spacingRatio else spacingRatio
        return (physicalCenter + lensCenterX - 0.5f).coerceIn(0.15f, 0.85f)
    }

}
