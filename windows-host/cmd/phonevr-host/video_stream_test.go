package main

import (
	"encoding/json"
	"testing"
)

func TestSupportsH264Profile(t *testing.T) {
	raw := json.RawMessage(`[{"name":"c2.qti.avc.decoder","mimeType":"video/avc","maxWidth":1920,"maxHeight":1080,"maxFrameRate":60,"hardwareAccelerated":true,"hardwareClassificationReliable":true,"supportsSbs1600x720At60":true}]`)
	if !supportsH264Profile(raw, 1600, 720, 60) {
		t.Fatal("expected H.264 SBS profile to be accepted")
	}
	if got := selectH264DecoderProfile(raw, 1600, 720, 60); got != "c2.qti.avc.decoder" {
		t.Fatalf("selected decoder = %q", got)
	}
	newProfiles := json.RawMessage(`[{"name":"c2.qti.avc.decoder","mimeType":"video/avc","maxWidth":2400,"maxHeight":1080,"maxFrameRate":60,"hardwareAccelerated":true,"supportedSbsProfiles":["1600x720","1920x864","2400x1080"]}]`)
	if got := selectH264DecoderProfile(newProfiles, 1920, 864, 60); got != "c2.qti.avc.decoder" {
		t.Fatalf("high-resolution profile decoder = %q", got)
	}
	if got := selectH264DecoderProfile(newProfiles, 1280, 576, 60); got != "" {
		t.Fatalf("decoder must explicitly advertise each profile, got %q", got)
	}
	uncertainClassification := json.RawMessage(`[{"name":"omx.vendor.avc.decoder","mimeType":"video/avc","maxWidth":1920,"maxHeight":1080,"maxFrameRate":60,"hardwareAccelerated":true,"hardwareClassificationReliable":false,"supportsSbs1600x720At60":true}]`)
	if !supportsH264Profile(uncertainClassification, 1600, 720, 60) {
		t.Fatal("legacy Android codec classification is advisory; a name-classified hardware decoder should be accepted")
	}
	if got := selectH264DecoderProfile(uncertainClassification, 1600, 720, 60); got != "omx.vendor.avc.decoder" {
		t.Fatalf("legacy Android decoder selected = %q", got)
	}
	if supportsH264Profile(json.RawMessage(`[ {"name":"c2.android.avc.decoder","mimeType":"video/avc","maxWidth":1920,"maxHeight":1080,"maxFrameRate":120,"hardwareAccelerated":false,"hardwareClassificationReliable":true,"supportsSbs1600x720At60":false} ]`), 1600, 720, 60) {
		t.Fatal("must reject software-only Android decoder for live VR")
	}
	if supportsH264Profile(raw, 1920, 1080, 90) {
		t.Fatal("must reject unsupported frame rate")
	}
	if supportsH264Profile(json.RawMessage(`[{"name":"c2.qti.hevc.decoder","mimeType":"video/hevc","maxWidth":3840,"maxHeight":2160,"maxFrameRate":120}]`), 1600, 720, 60) {
		t.Fatal("must not accept HEVC for H.264 negotiation")
	}
}
