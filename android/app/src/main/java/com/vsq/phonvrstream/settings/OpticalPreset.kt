package com.vsq.phonvrstream.settings

import org.json.JSONArray
import org.json.JSONObject

/** Optical preset stored on the Android device and synchronized with PhoneVR Host. */
data class OpticalPreset(
    val id: String,
    val name: String,
    val builtin: Boolean = false,
    val settings: DisplaySettings,
) {
    fun toJson(): JSONObject = JSONObject()
        .put("id", id)
        .put("name", name)
        .put("builtin", builtin)
        .put("settings", settings.normalized().toJson())

    companion object {
        fun fromJson(json: JSONObject): OpticalPreset? {
            val id = json.optString("id").trim()
            val name = json.optString("name").trim()
            val settings = json.optJSONObject("settings") ?: return null
            if (id.isBlank() || name.isBlank()) return null
            return OpticalPreset(
                id = id,
                name = name,
                builtin = json.optBoolean("builtin", false),
                settings = DisplaySettings.fromJson(settings),
            )
        }

        fun listFromJson(array: JSONArray): List<OpticalPreset> {
            val result = mutableListOf<OpticalPreset>()
            val ids = mutableSetOf<String>()
            for (index in 0 until array.length()) {
                val value = array.optJSONObject(index)?.let(::fromJson) ?: continue
                if (ids.add(value.id)) result += value
            }
            return result
        }

        fun listToJson(presets: List<OpticalPreset>): JSONArray = JSONArray().apply {
            presets.forEach { put(it.toJson()) }
        }

        private fun withDefaultEyeOffsets(settings: DisplaySettings): DisplaySettings = settings.copy(
            leftEyeOffsetX = 0.09f,
            leftEyeOffsetY = 0.09f,
            rightEyeOffsetX = -0.09f,
            rightEyeOffsetY = 0.09f,
        ).normalized()

        /**
         * Upgrades untouched built-in presets created before the default eye
         * alignment values were introduced. Any preset with a manually changed
         * eye offset is retained exactly as stored.
         */
        fun migrateLegacyZeroEyeOffsets(presets: List<OpticalPreset>): List<OpticalPreset> {
            val builtInIds = defaults().filter { it.builtin }.mapTo(mutableSetOf()) { it.id }
            return presets.map { preset ->
                val value = preset.settings
                val offsetsAreZero = value.leftEyeOffsetX == 0f && value.leftEyeOffsetY == 0f &&
                    value.rightEyeOffsetX == 0f && value.rightEyeOffsetY == 0f
                if (preset.builtin && preset.id in builtInIds && offsetsAreZero) {
                    preset.copy(settings = withDefaultEyeOffsets(value))
                } else {
                    preset
                }
            }
        }

        fun defaults(): List<OpticalPreset> = listOf(
            OpticalPreset(
                id = "builtin-daydream-2016",
                name = "Daydream View 2016",
                builtin = true,
                settings = withDefaultEyeOffsets(DisplaySettings(
                    ipdMm = 63f,
                    lensCenterX = .5f,
                    lensCenterY = .5f,
                    blur = 0f,
                    sharpness = .15f,
                ).withDaydreamView2016Profile()),
            ),
            OpticalPreset(
                id = "builtin-daydream-2017",
                name = "Daydream View 2017",
                builtin = true,
                settings = DisplaySettings(
                    viewerProfileId = DisplaySettings.PROFILE_DAYDREAM_VIEW_2017,
                    ipdMm = 63f,
                    lensSeparationMm = 64f,
                    screenToLensDistanceMm = 42f,
                    screenVerticalAlignment = DisplaySettings.ALIGNMENT_CENTER,
                    trayToLensCenterMm = 30f,
                    lensCenterX = .5f,
                    lensCenterY = .5f,
                    distortionK1 = .4331f,
                    distortionK2 = -.085f,
                    blur = 0f,
                    sharpness = .15f,
                    fovDegrees = 90f,
                ).let(::withDefaultEyeOffsets),
            ),
            OpticalPreset(
                id = "builtin-cardboard",
                name = "通用 Cardboard",
                builtin = true,
                settings = withDefaultEyeOffsets(DisplaySettings()),
            ),
        )
    }
}
