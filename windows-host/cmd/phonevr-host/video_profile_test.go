package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestVideoStreamProfilesAreSBSAndEven(t *testing.T) {
	want := map[string][2]int{
		"performance": {1280, 576},
		"balanced":    {1600, 720},
		"high":        {1920, 864},
		"maximum":     {2400, 1080},
	}
	for id, dimensions := range want {
		profile, ok := videoStreamProfileByID(id)
		if !ok {
			t.Fatalf("profile %q not found", id)
		}
		if profile.Width != dimensions[0] || profile.Height != dimensions[1] || profile.Width%2 != 0 || profile.FPS != 60 || profile.BitrateMbps < 1 {
			t.Fatalf("profile %q has unexpected dimensions or rate: %+v", id, profile)
		}
	}
}

func TestVideoStreamProfileConfigRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "PhoneVRStream", "video-profile.json")
	if err := saveVideoStreamProfile(path, "high"); err != nil {
		t.Fatalf("save profile: %v", err)
	}
	got := loadVideoStreamProfile(path)
	if got.ID != "high" || got.Width != 1920 || got.Height != 864 {
		t.Fatalf("loaded profile = %+v", got)
	}
	if err := saveVideoStreamProfile(path, "unknown"); err == nil {
		t.Fatal("expected unknown profile to be rejected")
	}
	if err := os.Remove(path); err != nil {
		t.Fatalf("remove profile: %v", err)
	}
	if got := loadVideoStreamProfile(path); got.ID != "balanced" {
		t.Fatalf("missing config should use balanced profile, got %+v", got)
	}
}

func TestPendingAndActiveVideoProfilesAreSeparated(t *testing.T) {
	pendingPath := filepath.Join(t.TempDir(), "PhoneVRStream", "video-profile.json")
	activePath := filepath.Join(t.TempDir(), "PhoneVRStream", "video-profile-active.json")
	if err := saveVideoStreamProfile(pendingPath, "high"); err != nil {
		t.Fatalf("save pending profile: %v", err)
	}
	if err := saveVideoStreamProfile(activePath, "balanced"); err != nil {
		t.Fatalf("save active profile: %v", err)
	}
	if got := loadVideoStreamProfile(pendingPath); got.ID != "high" {
		t.Fatalf("pending profile = %+v, want high", got)
	}
	if got := loadVideoStreamProfile(activePath); got.ID != "balanced" {
		t.Fatalf("active profile = %+v, want balanced until Host restarts", got)
	}
}
