package main

import (
	"testing"
)

func TestNativeDaydreamModelLoadsAsSolidMesh(t *testing.T) {
	model, err := loadNativeHeadsetModel()
	if err != nil {
		t.Fatalf("load native HMD mesh: %v", err)
	}
	if len(model.Triangles) < 10000 {
		t.Fatalf("mesh triangles = %d, want a detailed solid mesh", len(model.Triangles))
	}
	for index := 0; index < len(model.Triangles); index += 997 {
		triangle := model.Triangles[index]
		if triangle.C == [3]float32{} {
			t.Fatalf("triangle %d has no material colour", index)
		}
	}
}

func TestNativePresetDefaultsIncludeDaydreamAndCustom(t *testing.T) {
	presets := nativePresetDefaults()
	if len(presets) < 2 {
		t.Fatalf("default preset count = %d", len(presets))
	}
	if presets[0].Settings.ViewerProfileID != "daydream_view_2017" {
		t.Fatalf("first preset profile = %q", presets[0].Settings.ViewerProfileID)
	}
	if presets[0].Settings.DistortionK1 < 0.43 {
		t.Fatalf("Daydream K1 = %f", presets[0].Settings.DistortionK1)
	}
}

func TestNativePresetPersistenceKeepsOnlyUserPresets(t *testing.T) {
	path := t.TempDir() + "/optical-presets.json"
	presets := append(nativePresetDefaults(), OpticalPreset{ID: "test-user", Name: "My preset", Settings: DefaultPhoneDisplaySettings()})
	if err := saveNativePresetsTo(path, presets); err != nil {
		t.Fatal(err)
	}
	loaded := loadNativePresetsFrom(path)
	if len(loaded) != 3 {
		t.Fatalf("loaded presets = %d, want 3", len(loaded))
	}
	if loaded[2].ID != "test-user" {
		t.Fatalf("loaded user preset = %q", loaded[2].ID)
	}
}
