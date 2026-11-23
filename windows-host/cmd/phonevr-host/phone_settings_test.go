package main

import (
	"encoding/json"
	"testing"
)

func TestNormalizeOpticalPresetsDropsInvalidAndDuplicateIDs(t *testing.T) {
	presets := []OpticalPreset{
		{ID: " preset-a ", Name: " First ", Settings: DefaultPhoneDisplaySettings()},
		{ID: "preset-a", Name: "Duplicate", Settings: DefaultPhoneDisplaySettings()},
		{ID: "", Name: "Missing ID", Settings: DefaultPhoneDisplaySettings()},
		{ID: "preset-b", Name: "   ", Settings: DefaultPhoneDisplaySettings()},
		{ID: "preset-c", Name: "Third", Settings: PhoneDisplaySettings{IPDMm: 200}},
	}
	got := normalizeOpticalPresets(presets)
	if len(got) != 2 {
		t.Fatalf("normalized preset count = %d, want 2: %+v", len(got), got)
	}
	if got[0].ID != "preset-a" || got[0].Name != "First" {
		t.Fatalf("first preset was not trimmed: %+v", got[0])
	}
	if got[1].Settings.IPDMm != 75 {
		t.Fatalf("out-of-range IPD was not normalized: %v", got[1].Settings.IPDMm)
	}
}

func TestPhoneDisplaySettingsNormalizeStereoEyeMappingAndOffsets(t *testing.T) {
	settings := PhoneDisplaySettings{
		ViewerProfileID: "daydream_view_2016",
		SwapEyes:        true,
		LeftEyeOffsetX:  -0.12,
		LeftEyeOffsetY:  0.025,
		RightEyeOffsetX: 0.06,
		RightEyeOffsetY: 0.15,
		DistortionK2:    0.67,
	}.Normalized()

	if settings.ViewerProfileID != "daydream_view_2016" {
		t.Fatalf("2016 viewer profile was rejected: %q", settings.ViewerProfileID)
	}
	if !settings.SwapEyes {
		t.Fatal("eye swap flag was lost")
	}
	if settings.LeftEyeOffsetX != -0.12 || settings.LeftEyeOffsetY != 0.025 ||
		settings.RightEyeOffsetX != 0.06 || settings.RightEyeOffsetY != 0.15 {
		t.Fatalf("unexpected normalized per-eye offsets: %+v", settings)
	}
	if settings.DistortionK2 != 0.67 {
		t.Fatalf("Daydream 2016 K2 was clipped: %v", settings.DistortionK2)
	}
}

func TestPhoneDisplaySettingsStereoFieldsRoundTripJSON(t *testing.T) {
	original := DefaultPhoneDisplaySettings()
	original.SwapEyes = true
	original.LeftEyeOffsetX = -0.025
	original.LeftEyeOffsetY = 0.012
	original.RightEyeOffsetX = 0.04
	original.RightEyeOffsetY = -0.018
	original.VerticalTiltDegrees = -6.5

	encoded, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("marshal settings: %v", err)
	}
	var decoded PhoneDisplaySettings
	if err := json.Unmarshal(encoded, &decoded); err != nil {
		t.Fatalf("unmarshal settings: %v", err)
	}
	if decoded != original {
		t.Fatalf("stereo settings changed during JSON round trip: got %+v, want %+v", decoded, original)
	}
}
