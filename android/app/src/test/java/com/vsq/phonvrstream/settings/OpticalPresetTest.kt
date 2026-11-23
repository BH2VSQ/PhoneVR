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
