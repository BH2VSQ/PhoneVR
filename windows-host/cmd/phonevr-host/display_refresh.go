package main

import (
	"encoding/json"
	"errors"
	"math"
	"os"
	"path/filepath"
	"strings"
)

const (
	displayRefreshMode60      = "60"
	displayRefreshMode90      = "90"
	displayRefreshMode120     = "120"
	displayRefreshModeCustom  = "custom"
	minCustomDisplayRefreshHz = 60.0
	maxCustomDisplayRefreshHz = 120.0
)

type DisplayRefreshConfig struct {
	Mode          string  `json:"mode"`
	RefreshRateHz float64 `json:"refreshRateHz"`
}

func DefaultDisplayRefreshConfig() DisplayRefreshConfig {
	return DisplayRefreshConfig{Mode: displayRefreshModeCustom, RefreshRateHz: 72}
}

func NormalizeDisplayRefreshConfig(config DisplayRefreshConfig) (DisplayRefreshConfig, error) {
	switch strings.ToLower(strings.TrimSpace(config.Mode)) {
	case displayRefreshMode60:
		return DisplayRefreshConfig{Mode: displayRefreshMode60, RefreshRateHz: 60}, nil
	case displayRefreshMode90:
		return DisplayRefreshConfig{Mode: displayRefreshMode90, RefreshRateHz: 90}, nil
	case displayRefreshMode120:
		return DisplayRefreshConfig{Mode: displayRefreshMode120, RefreshRateHz: 120}, nil
	case displayRefreshModeCustom:
		if math.IsNaN(config.RefreshRateHz) || math.IsInf(config.RefreshRateHz, 0) ||
			config.RefreshRateHz < minCustomDisplayRefreshHz || config.RefreshRateHz > maxCustomDisplayRefreshHz {
			return DisplayRefreshConfig{}, errors.New("custom refresh rate must be between 60 and 120 Hz")
		}
		rate := math.Round(config.RefreshRateHz*10) / 10
		return DisplayRefreshConfig{Mode: displayRefreshModeCustom, RefreshRateHz: rate}, nil
	default:
		return DisplayRefreshConfig{}, errors.New("unsupported display refresh mode")
	}
}

func displayRefreshConfigPath() (string, error) {
	base := strings.TrimSpace(os.Getenv("LOCALAPPDATA"))
	if base == "" {
		var err error
		base, err = os.UserConfigDir()
		if err != nil {
			return "", err
		}
	}
	dir := filepath.Join(base, "PhoneVR")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	return filepath.Join(dir, "display-refresh.json"), nil
}

func loadDisplayRefreshConfig() DisplayRefreshConfig {
	config := DefaultDisplayRefreshConfig()
	path, err := displayRefreshConfigPath()
	if err != nil {
		return config
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return config
	}
	var parsed DisplayRefreshConfig
	if json.Unmarshal(data, &parsed) != nil {
		return config
	}
	normalized, err := NormalizeDisplayRefreshConfig(parsed)
	if err != nil {
		return config
	}
	return normalized
}

func saveDisplayRefreshConfig(config DisplayRefreshConfig) error {
	normalized, err := NormalizeDisplayRefreshConfig(config)
	if err != nil {
		return err
	}
	path, err := displayRefreshConfigPath()
	if err != nil {
		return err
	}
	data, err := json.MarshalIndent(normalized, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o600)
}
