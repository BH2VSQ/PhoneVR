//go:build windows

package main

import "testing"

func TestDefaultNativeThemeConfigUsesDayMode(t *testing.T) {
	if got := defaultNativeThemeConfig().Mode; got != "light" {
		t.Fatalf("default theme mode = %q, want light", got)
	}
}

func TestNormalizeNativeThemeConfigDefaultsUnknownModeToDay(t *testing.T) {
	got := normalizeNativeThemeConfig(NativeThemeConfig{Mode: "unknown"})
	if got.Mode != "light" {
		t.Fatalf("normalized unknown theme mode = %q, want light", got.Mode)
	}
}

func TestNormalizeNativeThemeConfigPreservesExplicitNightMode(t *testing.T) {
	got := normalizeNativeThemeConfig(NativeThemeConfig{Mode: "dark"})
	if got.Mode != "dark" {
		t.Fatalf("normalized explicit theme mode = %q, want dark", got.Mode)
	}
}
