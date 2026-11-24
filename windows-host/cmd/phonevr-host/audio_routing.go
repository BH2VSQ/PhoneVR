package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// AudioRoutingConfig keeps the two signal directions independent. WASAPI
// loopback reads from a Windows playback endpoint; phone microphone PCM is
// rendered into a playback endpoint (normally a virtual cable's render side),
// while Windows applications read from its paired recording endpoint.
type AudioRoutingConfig struct {
	Version                    int    `json:"version"`
	PCToPhoneRenderEndpointID  string `json:"pcToPhoneRenderEndpointId,omitempty"`
	PhoneToPCRenderEndpointID  string `json:"phoneToPcRenderEndpointId,omitempty"`
	PhoneToPCCaptureEndpointID string `json:"phoneToPcCaptureEndpointId,omitempty"`
}

func (c AudioRoutingConfig) normalized() AudioRoutingConfig {
	c.Version = 1
	c.PCToPhoneRenderEndpointID = strings.TrimSpace(c.PCToPhoneRenderEndpointID)
	c.PhoneToPCRenderEndpointID = strings.TrimSpace(c.PhoneToPCRenderEndpointID)
	c.PhoneToPCCaptureEndpointID = strings.TrimSpace(c.PhoneToPCCaptureEndpointID)
	return c
}

// hasCustomPhoneMicrophoneRoute reports whether the user explicitly selected
// either side of the phone-microphone route. In that case the optional
// VB-CABLE installer must not add another virtual driver just because the
// selected third-party endpoint names are unknown to automatic VAC/VB-CABLE
// detection.
func hasCustomPhoneMicrophoneRoute(config AudioRoutingConfig) bool {
	config = config.normalized()
	return config.PhoneToPCRenderEndpointID != "" || config.PhoneToPCCaptureEndpointID != ""
}

// AudioEndpointInfo is a Windows MMDevice endpoint surfaced to the native
// audio-routing dialog. Flow is "render" or "capture"; State is the raw
// DEVICE_STATE_* mask returned by MMDevice.
type AudioEndpointInfo struct {
	ID           string `json:"id"`
	FriendlyName string `json:"friendlyName"`
	Flow         string `json:"flow"`
	State        uint32 `json:"state"`
	IsDefault    bool   `json:"isDefault"`
}

func (e AudioEndpointInfo) Active() bool { return e.State&1 != 0 }

// AudioEndpointInventory describes all currently installed endpoint
// instances, including disabled endpoints so the UI can distinguish a missing
// device from one that merely needs to be enabled in Windows Sound settings.
type AudioEndpointInventory struct {
	Version                  int                 `json:"version"`
	DefaultRenderEndpointID  string              `json:"defaultRenderEndpointId,omitempty"`
	DefaultCaptureEndpointID string              `json:"defaultCaptureEndpointId,omitempty"`
	RenderEndpoints          []AudioEndpointInfo `json:"renderEndpoints"`
	CaptureEndpoints         []AudioEndpointInfo `json:"captureEndpoints"`
}

func (i AudioEndpointInventory) normalized() AudioEndpointInventory {
	if i.Version == 0 {
		i.Version = 1
	}
	if i.RenderEndpoints == nil {
		i.RenderEndpoints = []AudioEndpointInfo{}
	}
	if i.CaptureEndpoints == nil {
		i.CaptureEndpoints = []AudioEndpointInfo{}
	}
	return i
}

func audioRoutingConfigPath() (string, error) {
	base := strings.TrimSpace(os.Getenv("LOCALAPPDATA"))
	if base == "" {
		var err error
		base, err = os.UserConfigDir()
		if err != nil {
			return "", err
		}
	}
	return filepath.Join(base, "PhoneVR", "audio-routing.json"), nil
}

func loadAudioRoutingConfig() AudioRoutingConfig {
	path, err := audioRoutingConfigPath()
	if err != nil {
		return AudioRoutingConfig{Version: 1}
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return AudioRoutingConfig{Version: 1}
	}
	var config AudioRoutingConfig
	if err := json.Unmarshal(data, &config); err != nil {
		return AudioRoutingConfig{Version: 1}
	}
	return config.normalized()
}

func saveAudioRoutingConfig(config AudioRoutingConfig) error {
	config = config.normalized()
	path, err := audioRoutingConfigPath()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("create PhoneVR audio settings directory: %w", err)
	}
	data, err := json.MarshalIndent(config, "", "  ")
	if err != nil {
		return fmt.Errorf("encode audio routing settings: %w", err)
	}
	data = append(data, '\n')
	// Windows os.Rename does not replace an existing destination. The settings
	// file is small and contains no secrets, so write it directly to avoid a
	// remove/rename gap while the running supervisor reloads the configuration.
	if err := os.WriteFile(path, data, 0o600); err != nil {
		return fmt.Errorf("save audio routing settings: %w", err)
	}
	return nil
}

func validateAudioEndpointInventory(inventory AudioEndpointInventory) error {
	if inventory.Version != 1 {
		return fmt.Errorf("unsupported audio endpoint inventory version %d", inventory.Version)
	}
	seen := make(map[string]string, len(inventory.RenderEndpoints)+len(inventory.CaptureEndpoints))
	for _, group := range []struct {
		flow      string
		endpoints []AudioEndpointInfo
	}{{"render", inventory.RenderEndpoints}, {"capture", inventory.CaptureEndpoints}} {
		for _, endpoint := range group.endpoints {
			if strings.TrimSpace(endpoint.ID) == "" || strings.TrimSpace(endpoint.FriendlyName) == "" {
				return errors.New("audio endpoint inventory contains an endpoint without ID or friendly name")
			}
			if !strings.EqualFold(endpoint.Flow, group.flow) {
				return fmt.Errorf("audio endpoint %q has flow %q in %s list", endpoint.ID, endpoint.Flow, group.flow)
			}
			if previous, exists := seen[endpoint.ID]; exists {
				return fmt.Errorf("audio endpoint %q appears in both %s and %s lists", endpoint.ID, previous, group.flow)
			}
			seen[endpoint.ID] = group.flow
		}
	}
	return nil
}

func selectedEndpointID(configured string, endpoints []AudioEndpointInfo) string {
	configured = strings.TrimSpace(configured)
	if configured == "" {
		return ""
	}
	for _, endpoint := range endpoints {
		if endpoint.ID == configured && endpoint.Active() {
			return configured
		}
	}
	return ""
}
