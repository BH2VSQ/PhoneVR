package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
)

type OpticalPreset struct {
	ID       string               `json:"id"`
	Name     string               `json:"name"`
	Builtin  bool                 `json:"builtin,omitempty"`
	Settings PhoneDisplaySettings `json:"settings"`
}

func nativePresetDefaults() []OpticalPreset {
	return []OpticalPreset{
		{ID: "builtin-daydream-2017", Name: "Daydream View 2017", Builtin: true, Settings: PhoneDisplaySettings{
			ViewerProfileID: "daydream_view_2017", IPDMm: 63, LensSeparationMm: 64,
			ScreenToLensDistanceMm: 42, ScreenVerticalAlignment: "center", TrayToLensCenterMm: 30,
			LensCenterX: .5, LensCenterY: .5, DistortionK1: .4331, DistortionK2: -.085,
			Blur: 0, Sharpness: .15, FOVDegrees: 90,
			LeftEyeOffsetX: .09, LeftEyeOffsetY: .09,
			RightEyeOffsetX: -.09, RightEyeOffsetY: .09,
		}},
		{ID: "builtin-cardboard", Name: "通用 Cardboard", Builtin: true, Settings: DefaultPhoneDisplaySettings()},
	}
}

func nativePresetPath() (string, error) {
	base, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	dir := filepath.Join(base, "PhoneVRStream")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	return filepath.Join(dir, "optical-presets.json"), nil
}

func loadNativePresets() []OpticalPreset {
	path, err := nativePresetPath()
	if err != nil {
		return nativePresetDefaults()
	}
	return loadNativePresetsFrom(path)
}

func loadNativePresetsFrom(path string) []OpticalPreset {
	presets := nativePresetDefaults()
	data, err := os.ReadFile(path)
	if err != nil {
		return presets
	}
	var custom []OpticalPreset
	if json.Unmarshal(data, &custom) != nil {
		return presets
	}
	for _, preset := range custom {
		if !preset.Builtin && !strings.HasPrefix(preset.ID, "builtin-") {
			preset.Builtin = false
			preset.Settings = preset.Settings.Normalized()
			presets = append(presets, preset)
		}
	}
	return presets
}

func saveNativePresets(presets []OpticalPreset) error {
	path, err := nativePresetPath()
	if err != nil {
		return err
	}
	return saveNativePresetsTo(path, presets)
}

func saveNativePresetsTo(path string, presets []OpticalPreset) error {
	custom := make([]OpticalPreset, 0, len(presets))
	for _, preset := range presets {
		if !preset.Builtin && !strings.HasPrefix(preset.ID, "builtin-") {
			preset.Settings = preset.Settings.Normalized()
			custom = append(custom, preset)
		}
	}
	data, err := json.MarshalIndent(custom, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o600)
}

func nativeLanguagePath() (string, error) {
	base, err := nativePresetPath()
	if err != nil {
		return "", err
	}
	return filepath.Join(filepath.Dir(base), "language.txt"), nil
}

func loadNativeLanguage() string {
	path, err := nativeLanguagePath()
	if err != nil {
		return "zh"
	}
	value, err := os.ReadFile(path)
	if err == nil && strings.TrimSpace(string(value)) == "en" {
		return "en"
	}
	return "zh"
}

func saveNativeLanguage(language string) {
	path, err := nativeLanguagePath()
	if err != nil {
		return
	}
	if language != "en" {
		language = "zh"
	}
	_ = os.WriteFile(path, []byte(language), 0o600)
}
