package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestAudioRoutingConfigKeepsBothSignalDirectionsIndependent(t *testing.T) {
	config := AudioRoutingConfig{
		PCToPhoneRenderEndpointID:  "  {speaker-render}  ",
		PhoneToPCRenderEndpointID:  "  {third-party-cable-render}  ",
		PhoneToPCCaptureEndpointID: "  {third-party-cable-capture}  ",
	}.normalized()

	if config.Version != 1 {
		t.Fatalf("audio routing version = %d, want 1", config.Version)
	}
	if config.PCToPhoneRenderEndpointID != "{speaker-render}" {
		t.Fatalf("PC-to-phone endpoint was not trimmed: %#v", config)
	}
	if config.PhoneToPCRenderEndpointID != "{third-party-cable-render}" || config.PhoneToPCCaptureEndpointID != "{third-party-cable-capture}" {
		t.Fatalf("custom virtual audio endpoints were changed or coupled: %#v", config)
	}
	encoded, err := json.Marshal(config)
	if err != nil {
		t.Fatal(err)
	}
	var decoded AudioRoutingConfig
	if err := json.Unmarshal(encoded, &decoded); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(decoded.normalized(), config) {
		t.Fatalf("routing config round trip mismatch: got %#v want %#v", decoded, config)
	}
}

func TestAudioRoutingConfigSaveAndLoadSupportsArbitraryEndpointIDs(t *testing.T) {
	root := t.TempDir()
	t.Setenv("LOCALAPPDATA", root)
	want := AudioRoutingConfig{
		Version:                    1,
		PCToPhoneRenderEndpointID:  "{HDMI-Display-Audio}",
		PhoneToPCRenderEndpointID:  "{Virtual-Cable-Render-Endpoint}",
		PhoneToPCCaptureEndpointID: "{Virtual-Cable-Capture-Endpoint}",
	}
	if err := saveAudioRoutingConfig(want); err != nil {
		t.Fatalf("saveAudioRoutingConfig: %v", err)
	}
	got := loadAudioRoutingConfig()
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("loaded routing config = %#v, want %#v", got, want)
	}
	path := filepath.Join(root, "PhoneVR", "audio-routing.json")
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("audio routing config was not persisted at %s: %v", path, err)
	}
}

func TestValidateAudioEndpointInventoryDoesNotRestrictVendorNames(t *testing.T) {
	inventory := AudioEndpointInventory{
		Version: 1,
		RenderEndpoints: []AudioEndpointInfo{
			{ID: "render-speaker", FriendlyName: "Speakers (Realtek USB2.0 Audio)", Flow: "render", State: 1, IsDefault: true},
			{ID: "render-third-party", FriendlyName: "Studio Route A (Example Audio Driver)", Flow: "render", State: 1},
		},
		CaptureEndpoints: []AudioEndpointInfo{
			{ID: "capture-third-party", FriendlyName: "Studio Route A Return (Example Audio Driver)", Flow: "capture", State: 1},
			{ID: "capture-mic", FriendlyName: "Microphone Array (USB Interface)", Flow: "capture", State: 1, IsDefault: true},
		},
	}
	if err := validateAudioEndpointInventory(inventory); err != nil {
		t.Fatalf("generic third-party audio endpoints were rejected: %v", err)
	}
}

func TestValidateAudioEndpointInventoryRejectsWrongFlowAndDuplicateIDs(t *testing.T) {
	cases := []AudioEndpointInventory{
		{
			Version:         1,
			RenderEndpoints: []AudioEndpointInfo{{ID: "same", FriendlyName: "Speaker", Flow: "capture", State: 1}},
		},
		{
			Version:          1,
			RenderEndpoints:  []AudioEndpointInfo{{ID: "same", FriendlyName: "Speaker", Flow: "render", State: 1}},
			CaptureEndpoints: []AudioEndpointInfo{{ID: "same", FriendlyName: "Mic", Flow: "capture", State: 1}},
		},
	}
	for index, inventory := range cases {
		if err := validateAudioEndpointInventory(inventory); err == nil {
			t.Fatalf("case %d should be rejected", index)
		}
	}
}

func TestSelectedEndpointIDRequiresActiveEndpointButAllowsCustomVendor(t *testing.T) {
	endpoints := []AudioEndpointInfo{
		{ID: "custom-active", FriendlyName: "Virtual Audio Render (Other Vendor)", Flow: "render", State: 1},
		{ID: "custom-disabled", FriendlyName: "Disabled Cable", Flow: "render", State: 2},
	}
	if got := selectedEndpointID("custom-active", endpoints); got != "custom-active" {
		t.Fatalf("selectedEndpointID returned %q for custom active endpoint", got)
	}
	if got := selectedEndpointID("custom-disabled", endpoints); got != "" {
		t.Fatalf("disabled endpoint unexpectedly selected: %q", got)
	}
}

func TestHasCustomPhoneMicrophoneRouteSuppressesFallbackInstaller(t *testing.T) {
	cases := []struct {
		name   string
		config AudioRoutingConfig
		want   bool
	}{
		{name: "no custom microphone route", config: AudioRoutingConfig{PCToPhoneRenderEndpointID: "speaker"}, want: false},
		{name: "custom render selected", config: AudioRoutingConfig{PhoneToPCRenderEndpointID: "custom-virtual-render"}, want: true},
		{name: "custom capture selected", config: AudioRoutingConfig{PhoneToPCCaptureEndpointID: "custom-virtual-capture"}, want: true},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			if got := hasCustomPhoneMicrophoneRoute(test.config); got != test.want {
				t.Fatalf("hasCustomPhoneMicrophoneRoute = %t, want %t", got, test.want)
			}
		})
	}
}
