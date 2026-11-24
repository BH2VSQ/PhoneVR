package com.vsq.phonvrstream.settings

import org.json.JSONArray
import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Test

class OpticalPresetTest {
    @Test
    fun defaultsContainBothDaydreamGenerationsAndCardboardPresets() {
        val presets = OpticalPreset.defaults()
        assertEquals(
            listOf("builtin-daydream-2016", "builtin-daydream-2017", "builtin-cardboard"),
            presets.map { it.id },
        )
        assertTrue(presets.all { it.builtin })
        assertEquals(0.01f, presets.first().settings.distortionK1, 0.0001f)
        assertEquals(0.67f, presets.first().settings.distortionK2, 0.0001f)
        presets.forEach { preset ->
            assertEquals(0.09f, preset.settings.leftEyeOffsetX, 0.0001f)
            assertEquals(0.09f, preset.settings.leftEyeOffsetY, 0.0001f)
            assertEquals(-0.09f, preset.settings.rightEyeOffsetX, 0.0001f)
            assertEquals(0.09f, preset.settings.rightEyeOffsetY, 0.0001f)
        }
    }

    @Test
    fun legacyZeroOffsetsAreMigratedOnlyForUntouchedBuiltIns() {
        val legacyBuiltIns = OpticalPreset.defaults().map { preset ->
            preset.copy(settings = preset.settings.copy(
                leftEyeOffsetX = 0f,
                leftEyeOffsetY = 0f,
                rightEyeOffsetX = 0f,
                rightEyeOffsetY = 0f,
            ))
        }
        val manuallyCalibrated = legacyBuiltIns.first().copy(
            settings = legacyBuiltIns.first().settings.copy(leftEyeOffsetX = 0.025f),
        )
        val custom = OpticalPreset(
            id = "user-custom",
            name = "My Viewer",
            settings = DisplaySettings(),
        )

        val migrated = OpticalPreset.migrateLegacyZeroEyeOffsets(
            legacyBuiltIns.drop(1) + manuallyCalibrated + custom,
        ).associateBy { it.id }

        assertEquals(0.09f, migrated.getValue("builtin-daydream-2017").settings.leftEyeOffsetX, 0.0001f)
        assertEquals(0.09f, migrated.getValue("builtin-cardboard").settings.leftEyeOffsetY, 0.0001f)
        assertEquals(0.025f, migrated.getValue("builtin-daydream-2016").settings.leftEyeOffsetX, 0.0001f)
        assertEquals(0f, migrated.getValue("builtin-daydream-2016").settings.leftEyeOffsetY, 0.0001f)
        assertEquals(custom, migrated.getValue("user-custom"))
    }

    @Test
    fun presetListRoundTripsThroughJson() {
        val expected = OpticalPreset.defaults() + OpticalPreset(
            id = "user-custom",
            name = "My Viewer",
            settings = DisplaySettings(ipdMm = 66f).normalized(),
        )
        val actual = OpticalPreset.listFromJson(OpticalPreset.listToJson(expected))
        assertEquals(expected, actual)
    }

    @Test
    fun malformedAndDuplicateEntriesAreIgnored() {
        val valid = OpticalPreset.defaults().first().toJson()
        val values = JSONArray().put(valid).put(valid).put(org.json.JSONObject().put("id", "broken"))
        val actual = OpticalPreset.listFromJson(values)
        assertEquals(1, actual.size)
        assertEquals("builtin-daydream-2016", actual.single().id)
    }
}
