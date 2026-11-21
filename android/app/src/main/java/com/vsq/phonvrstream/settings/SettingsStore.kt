package com.vsq.phonvrstream.settings

import android.content.Context
import org.json.JSONArray

class SettingsStore(context: Context) {
    private val prefs = context.getSharedPreferences("phone_vr_display", Context.MODE_PRIVATE)

    fun load(): DisplaySettings = DisplaySettings(
        viewerProfileId = prefs.getString(KEY_PROFILE, DisplaySettings.PROFILE_CUSTOM) ?: DisplaySettings.PROFILE_CUSTOM,
        ipdMm = prefs.getFloat(KEY_IPD, 63.0f),
        lensSeparationMm = prefs.getFloat(KEY_LENS_SEPARATION, 63.9f),
        screenToLensDistanceMm = prefs.getFloat(KEY_SCREEN_TO_LENS, 40.0f),
        screenVerticalAlignment = prefs.getString(KEY_VERTICAL_ALIGNMENT, DisplaySettings.ALIGNMENT_CENTER)
            ?: DisplaySettings.ALIGNMENT_CENTER,
        trayToLensCenterMm = prefs.getFloat(KEY_TRAY_TO_LENS_CENTER, 35.0f),
        lensCenterX = prefs.getFloat(KEY_CENTER_X, 0.5f),
        lensCenterY = prefs.getFloat(KEY_CENTER_Y, 0.5f),
        distortionK1 = prefs.getFloat(KEY_K1, 0.18f),
        distortionK2 = prefs.getFloat(KEY_K2, 0.04f),
        blur = prefs.getFloat(KEY_BLUR, 0.0f),
        sharpness = prefs.getFloat(KEY_SHARPNESS, 0.35f),
        fovDegrees = prefs.getFloat(KEY_FOV, 90.0f),
    ).normalized()

    fun loadOpticalPresets(legacySettings: DisplaySettings = load()): List<OpticalPreset> {
        val raw = prefs.getString(KEY_OPTICAL_PRESETS, null)
        if (raw == null) {
            // Upgrade existing installations by turning their previous manual
            // calibration into a local preset before hiding the old controls.
            val defaults = OpticalPreset.defaults().toMutableList()
            if (legacySettings.normalized() != DisplaySettings().normalized()) {
                defaults.add(
                    0,
                    OpticalPreset(
                        id = "migrated-existing-settings",
                        name = "原有显示配置",
                        builtin = false,
                        settings = legacySettings.normalized(),
                    ),
                )
                saveActiveOpticalPresetId("migrated-existing-settings")
            }
            saveOpticalPresets(defaults)
            return defaults
        }
        return try {
            val parsed = OpticalPreset.listFromJson(JSONArray(raw))
            parsed.ifEmpty { OpticalPreset.defaults().also(::saveOpticalPresets) }
        } catch (_: Exception) {
            OpticalPreset.defaults().also(::saveOpticalPresets)
        }
    }

    fun saveOpticalPresets(presets: List<OpticalPreset>) {
        val normalized = presets
            .filter { it.id.isNotBlank() && it.name.isNotBlank() }
            .distinctBy { it.id }
        prefs.edit().putString(KEY_OPTICAL_PRESETS, OpticalPreset.listToJson(normalized).toString()).apply()
    }

    fun loadActiveOpticalPresetId(): String =
        prefs.getString(KEY_ACTIVE_PRESET, "builtin-cardboard") ?: "builtin-cardboard"

    fun saveActiveOpticalPresetId(id: String) {
        prefs.edit().putString(KEY_ACTIVE_PRESET, id).apply()
    }

    fun save(settings: DisplaySettings) {
        val value = settings.normalized()
        prefs.edit()
            .putString(KEY_PROFILE, value.viewerProfileId)
            .putFloat(KEY_IPD, value.ipdMm)
            .putFloat(KEY_LENS_SEPARATION, value.lensSeparationMm)
            .putFloat(KEY_SCREEN_TO_LENS, value.screenToLensDistanceMm)
            .putString(KEY_VERTICAL_ALIGNMENT, value.screenVerticalAlignment)
            .putFloat(KEY_TRAY_TO_LENS_CENTER, value.trayToLensCenterMm)
            .putFloat(KEY_CENTER_X, value.lensCenterX)
            .putFloat(KEY_CENTER_Y, value.lensCenterY)
            .putFloat(KEY_K1, value.distortionK1)
            .putFloat(KEY_K2, value.distortionK2)
            .putFloat(KEY_BLUR, value.blur)
            .putFloat(KEY_SHARPNESS, value.sharpness)
            .putFloat(KEY_FOV, value.fovDegrees)
            .apply()
    }

    companion object {
        private const val KEY_OPTICAL_PRESETS = "optical_presets_json"
        private const val KEY_ACTIVE_PRESET = "active_optical_preset_id"
        private const val KEY_PROFILE = "viewer_profile_id"
        private const val KEY_IPD = "ipd_mm"
        private const val KEY_LENS_SEPARATION = "lens_separation_mm"
        private const val KEY_SCREEN_TO_LENS = "screen_to_lens_mm"
        private const val KEY_VERTICAL_ALIGNMENT = "screen_vertical_alignment"
        private const val KEY_TRAY_TO_LENS_CENTER = "tray_to_lens_center_mm"
        private const val KEY_CENTER_X = "lens_center_x"
        private const val KEY_CENTER_Y = "lens_center_y"
        private const val KEY_K1 = "distortion_k1"
        private const val KEY_K2 = "distortion_k2"
        private const val KEY_BLUR = "blur"
        private const val KEY_SHARPNESS = "sharpness"
        private const val KEY_FOV = "fov_degrees"
    }
}
