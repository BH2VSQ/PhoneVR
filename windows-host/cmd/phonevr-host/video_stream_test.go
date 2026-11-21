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
	uncertainClassification := json.RawMessage(`[{"name":"omx.vendor.avc.decoder","mimeType":"video/avc","maxWidth":1920,"maxHeight":1080,"maxFrameRate":60,"hardwareAccelerated":true,"hardwareClassificationReliable":false,"supportsSbs1600x720At60":true}]`)
	if supportsH264Profile(uncertainClassification, 1600, 720, 60) {
		t.Fatal("must reject a decoder whose hardware classification is not reliable")
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
