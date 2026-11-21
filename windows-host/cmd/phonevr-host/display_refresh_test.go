package main

import (
	"encoding/json"
	"math"
	"os"
	"path/filepath"
	"testing"
)

func TestNormalizeDisplayRefreshPresets(t *testing.T) {
	tests := []struct {
		mode string
		want float64
	}{{"60", 60}, {"90", 90}, {"120", 120}}
	for _, tt := range tests {
		got, err := NormalizeDisplayRefreshConfig(DisplayRefreshConfig{Mode: tt.mode, RefreshRateHz: 1})
		if err != nil || got.RefreshRateHz != tt.want || got.Mode != tt.mode {
			t.Fatalf("mode %s: got %+v, err=%v", tt.mode, got, err)
		}
	}
}

func TestNormalizeDisplayRefreshCustom(t *testing.T) {
	got, err := NormalizeDisplayRefreshConfig(DisplayRefreshConfig{Mode: "custom", RefreshRateHz: 87.26})
	if err != nil || got.RefreshRateHz != 87.3 {
		t.Fatalf("got %+v, err=%v", got, err)
	}
	for _, rate := range []float64{math.NaN(), math.Inf(1), 59.9, 120.1} {
		if _, err := NormalizeDisplayRefreshConfig(DisplayRefreshConfig{Mode: "custom", RefreshRateHz: rate}); err == nil {
			t.Fatalf("expected custom rate %v to fail", rate)
		}
	}
}

func TestDisplayRefreshConfigJSON(t *testing.T) {
	data, err := json.Marshal(DisplayRefreshConfig{Mode: "custom", RefreshRateHz: 85.5})
	if err != nil {
		t.Fatal(err)
	}
	var got DisplayRefreshConfig
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatal(err)
	}
	if normalized, err := NormalizeDisplayRefreshConfig(got); err != nil || normalized.RefreshRateHz != 85.5 {
		t.Fatalf("got %+v, err=%v", normalized, err)
	}
}

func TestDefaultDisplayRefreshPreservesPreviousTiming(t *testing.T) {
	got := DefaultDisplayRefreshConfig()
	if got.Mode != displayRefreshModeCustom || got.RefreshRateHz != 72 {
		t.Fatalf("default refresh config = %+v, want Custom 72 Hz", got)
	}
}

func TestSaveAndLoadDisplayRefreshConfig(t *testing.T) {
	localAppData := t.TempDir()
	t.Setenv("LOCALAPPDATA", localAppData)

	want := DisplayRefreshConfig{Mode: displayRefreshModeCustom, RefreshRateHz: 87.3}
	if err := saveDisplayRefreshConfig(want); err != nil {
		t.Fatalf("saveDisplayRefreshConfig() error = %v", err)
	}
	got := loadDisplayRefreshConfig()
	if got != want {
		t.Fatalf("loadDisplayRefreshConfig() = %+v, want %+v", got, want)
	}

	path := filepath.Join(localAppData, "PhoneVR", "display-refresh.json")
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("display refresh config was not written at %s: %v", path, err)
	}
}
