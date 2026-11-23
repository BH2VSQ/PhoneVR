package main

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
)

// VideoStreamProfile describes the side-by-side encoded frame dimensions.
// Width is the complete SBS frame width, not the per-eye width.
type VideoStreamProfile struct {
	ID          string `json:"profileId"`
	Width       int    `json:"width"`
	Height      int    `json:"height"`
	FPS         int    `json:"fps"`
	BitrateMbps int    `json:"-"`
	DisplayName string `json:"-"`
	EnglishName string `json:"-"`
}

var videoStreamProfiles = []VideoStreamProfile{
	{ID: "performance", Width: 1280, Height: 576, FPS: 60, BitrateMbps: 5, DisplayName: "性能优先 1280×576 SBS（每眼 640×576，5 Mbps）", EnglishName: "Performance 1280×576 SBS (640×576 per eye, 5 Mbps)"},
	{ID: "balanced", Width: 1600, Height: 720, FPS: 60, BitrateMbps: 8, DisplayName: "均衡 1600×720 SBS（每眼 800×720，8 Mbps）", EnglishName: "Balanced 1600×720 SBS (800×720 per eye, 8 Mbps)"},
	{ID: "high", Width: 1920, Height: 864, FPS: 60, BitrateMbps: 13, DisplayName: "高清 1920×864 SBS（每眼 960×864，13 Mbps）", EnglishName: "High 1920×864 SBS (960×864 per eye, 13 Mbps)"},
	{ID: "maximum", Width: 2400, Height: 1080, FPS: 60, BitrateMbps: 18, DisplayName: "最高 2400×1080 SBS（每眼 1200×1080，18 Mbps）", EnglishName: "Maximum 2400×1080 SBS (1200×1080 per eye, 18 Mbps)"},
}

func defaultVideoStreamProfile() VideoStreamProfile {
	return videoStreamProfiles[1]
}

func videoStreamProfileByID(id string) (VideoStreamProfile, bool) {
	id = strings.TrimSpace(strings.ToLower(id))
	for _, profile := range videoStreamProfiles {
		if profile.ID == id {
			return profile, true
		}
	}
	return VideoStreamProfile{}, false
}

func videoStreamProfileConfigPath() (string, error) {
	configDir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(configDir, "PhoneVRStream", "video-profile.json"), nil
}

func loadVideoStreamProfile(path string) VideoStreamProfile {
	fallback := defaultVideoStreamProfile()
	if strings.TrimSpace(path) == "" {
		return fallback
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return fallback
	}
	var saved struct {
		ID string `json:"profileId"`
	}
	if json.Unmarshal(data, &saved) != nil {
		return fallback
	}
	if profile, ok := videoStreamProfileByID(saved.ID); ok {
		return profile
	}
	return fallback
}

func saveVideoStreamProfile(path, profileID string) error {
	profile, ok := videoStreamProfileByID(profileID)
	if !ok {
		return errors.New("unknown video stream profile")
	}
	if strings.TrimSpace(path) == "" {
		return errors.New("video profile config path is empty")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(struct {
		ID string `json:"profileId"`
	}{ID: profile.ID}, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	return os.WriteFile(path, data, 0o600)
}
