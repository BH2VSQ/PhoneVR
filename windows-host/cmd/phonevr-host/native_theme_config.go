//go:build windows

package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

type NativeThemeConfig struct {
	Mode       string    `json:"mode"`
	AccentHex  string    `json:"accentHex"`
	PaletteHex [5]string `json:"paletteHex,omitempty"`
}

type nativePalette struct {
	Background     uintptr
	Panel          uintptr
	Surface        uintptr
	Input          uintptr
	Border         uintptr
	Text           uintptr
	Muted          uintptr
	Accent         uintptr
	AccentText     uintptr
	DangerBg       uintptr
	DangerLine     uintptr
	DangerText     uintptr
	SteamBg        uintptr
	SteamLine      uintptr
	SteamText      uintptr
	Good           uintptr
	Warning        uintptr
	PoseBackground uintptr
	PoseGrid       uintptr
	PoseAxisX      uintptr
	PoseAxisY      uintptr
	PoseAxisZ      uintptr
	PoseGaze       uintptr
}

var defaultNativePaletteHex = [5]string{
	"#5BCEFA", "#F5A9B8", "#B39DDB", "#80CBC4", "#FFCC80",
}

func defaultNativeThemeConfig() NativeThemeConfig {
	return NativeThemeConfig{Mode: "dark", AccentHex: "#5BCEFA", PaletteHex: defaultNativePaletteHex}
}

func normalizeNativeThemeConfig(config NativeThemeConfig) NativeThemeConfig {
	if config.Mode != "light" {
		config.Mode = "dark"
	}
	defaults := defaultNativeThemeConfig()
	for i := range config.PaletteHex {
		if _, err := parseNativeHexColor(config.PaletteHex[i]); err != nil {
			config.PaletteHex[i] = defaults.PaletteHex[i]
		}
		config.PaletteHex[i] = strings.ToUpper(config.PaletteHex[i])
	}
	if _, err := parseNativeHexColor(config.AccentHex); err != nil {
		config.AccentHex = defaults.AccentHex
	}
	config.AccentHex = strings.ToUpper(config.AccentHex)
	return config
}

func nativeThemeConfigPath() (string, error) {
	base, err := nativePresetPath()
	if err != nil {
		return "", err
	}
	return filepath.Join(filepath.Dir(base), "theme.json"), nil
}

func loadNativeThemeConfig() NativeThemeConfig {
	config := defaultNativeThemeConfig()
	path, err := nativeThemeConfigPath()
	if err != nil {
		return config
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return config
	}
	if err := json.Unmarshal(data, &config); err != nil {
		return defaultNativeThemeConfig()
	}
	return normalizeNativeThemeConfig(config)
}

func saveNativeThemeConfig(config NativeThemeConfig) error {
	config = normalizeNativeThemeConfig(config)
	if _, err := parseNativeHexColor(config.AccentHex); err != nil {
		return fmt.Errorf("invalid accent color: %w", err)
	}
	path, err := nativeThemeConfigPath()
	if err != nil {
		return err
	}
	data, err := json.MarshalIndent(config, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o600)
}

func parseNativeHexColor(value string) (uintptr, error) {
	value = strings.TrimSpace(strings.TrimPrefix(value, "#"))
	if len(value) != 6 {
		return 0, fmt.Errorf("expected #RRGGBB")
	}
	r, errR := strconv.ParseUint(value[0:2], 16, 8)
	g, errG := strconv.ParseUint(value[2:4], 16, 8)
	b, errB := strconv.ParseUint(value[4:6], 16, 8)
	if errR != nil || errG != nil || errB != nil {
		return 0, fmt.Errorf("expected #RRGGBB")
	}
	return rgb(uint8(r), uint8(g), uint8(b)), nil
}

func (app *nativeUI) palette() nativePalette {
	accent, err := parseNativeHexColor(app.themeConfig.AccentHex)
	if err != nil {
		accent = rgb(91, 206, 250)
	}
	accentText := nativeAccentForeground(accent)
	if app.themeConfig.Mode == "light" {
		return nativePalette{
			Background: rgb(239, 245, 250), Panel: rgb(250, 252, 255), Surface: rgb(231, 239, 247),
			Input: rgb(255, 255, 255), Border: rgb(190, 207, 222), Text: rgb(27, 43, 58),
			Muted: rgb(91, 112, 132), Accent: accent, AccentText: accentText,
			DangerBg: rgb(255, 233, 236), DangerLine: rgb(230, 170, 180), DangerText: rgb(142, 45, 61),
			SteamBg: rgb(237, 237, 249), SteamLine: rgb(193, 190, 228), SteamText: rgb(74, 69, 131),
			Good: rgb(32, 151, 111), Warning: rgb(173, 116, 35),
			PoseBackground: rgb(245, 249, 253), PoseGrid: rgb(194, 208, 221),
			PoseAxisX: rgb(194, 54, 54), PoseAxisY: rgb(25, 137, 82), PoseAxisZ: rgb(43, 98, 190), PoseGaze: rgb(0, 133, 163),
		}
	}
	return nativePalette{
		Background: rgb(9, 17, 27), Panel: rgb(16, 29, 42), Surface: rgb(20, 38, 56),
		Input: rgb(9, 21, 33), Border: rgb(38, 59, 80), Text: rgb(228, 239, 250),
		Muted: rgb(125, 153, 178), Accent: accent, AccentText: accentText,
		DangerBg: rgb(64, 31, 39), DangerLine: rgb(111, 55, 67), DangerText: rgb(255, 190, 197),
		SteamBg: rgb(21, 21, 42), SteamLine: rgb(60, 58, 98), SteamText: rgb(184, 182, 239),
		Good: rgb(85, 217, 160), Warning: rgb(231, 183, 110),
		PoseBackground: rgb(11, 19, 28), PoseGrid: rgb(27, 43, 59),
		PoseAxisX: rgb(242, 89, 82), PoseAxisY: rgb(72, 220, 147), PoseAxisZ: rgb(82, 151, 255), PoseGaze: rgb(47, 220, 245),
	}
}

func nativeAccentForeground(color uintptr) uintptr {
	r := float64(color & 0xff)
	g := float64((color >> 8) & 0xff)
	b := float64((color >> 16) & 0xff)
	luminance := 0.299*r + 0.587*g + 0.114*b
	if luminance < 150 {
		return rgb(248, 251, 255)
	}
	return rgb(8, 24, 36)
}
