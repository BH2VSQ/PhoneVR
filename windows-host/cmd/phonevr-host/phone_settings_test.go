package main

import "testing"

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
