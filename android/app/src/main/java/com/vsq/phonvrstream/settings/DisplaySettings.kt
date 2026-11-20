package com.vsq.phonvrstream.settings

import org.json.JSONObject

/** User-adjustable display calibration values. Distances are stored in millimetres. */
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
        distortionK2 = distortionK2.coerceIn(-0.20f, 0.30f),
        blur = blur.coerceIn(0.0f, 1.0f),
        sharpness = sharpness.coerceIn(0.0f, 1.0f),
        fovDegrees = fovDegrees.coerceIn(60.0f, 115.0f),
    )

    fun withDaydreamView2017Profile(): DisplaySettings = copy(
        viewerProfileId = PROFILE_DAYDREAM_VIEW_2017,
        // Published measurements for Google VR's Daydream View v2.0 (2017) default profile.
        lensSeparationMm = 64.0f,
        distortionK1 = 0.4331f,
        distortionK2 = -0.085f,
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

    /** Parses remote settings while preserving local defaults for omitted fields. */
    companion object {
        const val PROFILE_CUSTOM = "custom"
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
