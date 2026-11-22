package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net"
	"strings"
	"time"
)

// PhoneDisplaySettings mirrors Android DisplaySettings. The struct is kept in
// the shared TCP control protocol so the Windows UI can edit real phone state.
type PhoneDisplaySettings struct {
	ViewerProfileID         string  `json:"viewerProfileId"`
	IPDMm                   float32 `json:"ipdMm"`
	LensSeparationMm        float32 `json:"lensSeparationMm"`
	ScreenToLensDistanceMm  float32 `json:"screenToLensDistanceMm"`
	ScreenVerticalAlignment string  `json:"screenVerticalAlignment"`
	TrayToLensCenterMm      float32 `json:"trayToLensCenterMm"`
	LensCenterX             float32 `json:"lensCenterX"`
	LensCenterY             float32 `json:"lensCenterY"`
	DistortionK1            float32 `json:"distortionK1"`
	DistortionK2            float32 `json:"distortionK2"`
	Blur                    float32 `json:"blur"`
	Sharpness               float32 `json:"sharpness"`
	FOVDegrees              float32 `json:"fovDegrees"`
}

func DefaultPhoneDisplaySettings() PhoneDisplaySettings {
	return PhoneDisplaySettings{
		ViewerProfileID: "custom", IPDMm: 63, LensSeparationMm: 63.9,
		ScreenToLensDistanceMm: 40, ScreenVerticalAlignment: "center",
		TrayToLensCenterMm: 35, LensCenterX: .5, LensCenterY: .5,
		DistortionK1: .18, DistortionK2: .04, Blur: 0, Sharpness: .35, FOVDegrees: 90,
	}
}

func (s PhoneDisplaySettings) Normalized() PhoneDisplaySettings {
	defaults := DefaultPhoneDisplaySettings()
	if s.ViewerProfileID != "custom" && s.ViewerProfileID != "daydream_view_2017" {
		s.ViewerProfileID = defaults.ViewerProfileID
	}
	if s.ScreenVerticalAlignment != "center" && s.ScreenVerticalAlignment != "top" && s.ScreenVerticalAlignment != "bottom" {
		s.ScreenVerticalAlignment = defaults.ScreenVerticalAlignment
	}
	s.IPDMm = clampFloat32(s.IPDMm, 50, 75)
	s.LensSeparationMm = clampFloat32(s.LensSeparationMm, 50, 75)
	s.ScreenToLensDistanceMm = clampFloat32(s.ScreenToLensDistanceMm, 25, 70)
	s.TrayToLensCenterMm = clampFloat32(s.TrayToLensCenterMm, 10, 60)
	s.LensCenterX = clampFloat32(s.LensCenterX, .35, .65)
	s.LensCenterY = clampFloat32(s.LensCenterY, .35, .65)
	s.DistortionK1 = clampFloat32(s.DistortionK1, -.30, .60)
	s.DistortionK2 = clampFloat32(s.DistortionK2, -.20, .30)
	s.Blur = clampFloat32(s.Blur, 0, 1)
	s.Sharpness = clampFloat32(s.Sharpness, 0, 1)
	s.FOVDegrees = clampFloat32(s.FOVDegrees, 60, 115)
	// The Daydream preset button seeds the published starting values in the UI.
	// Keep subsequent manual calibration edits when the profile remains selected.
	return s
}

func clampFloat32(value, minimum, maximum float32) float32 {
	if math.IsNaN(float64(value)) || math.IsInf(float64(value), 0) {
		return minimum
	}
	return float32(math.Max(float64(minimum), math.Min(float64(maximum), float64(value))))
}

func (s *HostService) SendPhoneDisplaySettings(settings PhoneDisplaySettings) error {
	settings = settings.Normalized()
	s.mu.RLock()
	var target activeDevice
	found := false
	latest := time.Time{}
	for _, device := range s.devices {
		if !device.Ready || device.VideoOnly {
			continue
		}
		connectedAt, _ := time.Parse(time.RFC3339, device.Snapshot.ConnectedAt)
		if !found || connectedAt.After(latest) {
			target = device
			latest = connectedAt
			found = true
		}
	}
	s.mu.RUnlock()
	if !found || target.Conn == nil || target.WriteMu == nil {
		return errors.New("no connected Android HMD")
	}
	message := map[string]any{
		"type":      "set_display_settings",
		"requestId": newSessionID(),
		"settings":  settings,
	}
	if err := writeControlJSON(target, message); err != nil {
		return fmt.Errorf("send display settings to phone: %w", err)
	}
	s.addLog("Display settings sent to " + target.Snapshot.Name)
	return nil
}

func writeControlJSON(device activeDevice, value any) error {
	if device.Conn == nil {
		return net.ErrClosed
	}
	if device.WriteMu != nil {
		device.WriteMu.Lock()
		defer device.WriteMu.Unlock()
	}
	_ = device.Conn.SetWriteDeadline(time.Now().Add(controlWriteTimeout))
	err := json.NewEncoder(device.Conn).Encode(value)
	_ = device.Conn.SetWriteDeadline(time.Time{})
	return err
}

// SendPhoneAction forwards a supported device-side operation to the active HMD.
func (s *HostService) SendPhoneAction(action string) error {
	if action != "center_view" && action != "open_display_calibration" && action != "open_optical_presets" {
		return fmt.Errorf("unsupported phone action: %s", action)
	}
	s.mu.RLock()
	var target activeDevice
	found := false
	latest := time.Time{}
	for _, device := range s.devices {
		if !device.Ready || device.VideoOnly {
			continue
		}
		connectedAt, _ := time.Parse(time.RFC3339, device.Snapshot.ConnectedAt)
		if !found || connectedAt.After(latest) {
			target, latest, found = device, connectedAt, true
		}
	}
	s.mu.RUnlock()
	if !found || target.Conn == nil || target.WriteMu == nil {
		return errors.New("no connected Android HMD")
	}
	message := map[string]any{"type": "device_action", "requestId": newSessionID(), "action": action}
	if err := writeControlJSON(target, message); err != nil {
		return fmt.Errorf("send phone action: %w", err)
	}
	s.addLog("Phone action sent: " + action)
	return nil
}

// SendPhoneOpticalPresets synchronizes the host-edited list back to Android,
// where the persistent copy remains the source of truth.
func (s *HostService) SendPhoneOpticalPresets(presets []OpticalPreset) error {
	presets = normalizeOpticalPresets(presets)
	device, err := s.latestReadyDevice()
	if err != nil {
		return err
	}
	message := map[string]any{
		"type": "set_optical_presets", "requestId": newSessionID(), "presets": presets,
	}
	if err := writeControlJSON(device, message); err != nil {
		return fmt.Errorf("send optical presets to phone: %w", err)
	}
	s.addLog(fmt.Sprintf("Synchronized %d optical presets to %s", len(presets), device.Snapshot.Name))
	return nil
}

// ApplyPhoneOpticalPreset asks Android to load the preset by ID from its own
// local persistent store and apply those display parameters.
func (s *HostService) ApplyPhoneOpticalPreset(presetID string) error {
	if strings.TrimSpace(presetID) == "" {
		return errors.New("optical preset ID is empty")
	}
	device, err := s.latestReadyDevice()
	if err != nil {
		return err
	}
	message := map[string]any{
		"type": "apply_optical_preset", "requestId": newSessionID(), "presetId": presetID,
	}
	if err := writeControlJSON(device, message); err != nil {
		return fmt.Errorf("apply optical preset on phone: %w", err)
	}
	s.addLog("Requested optical preset " + presetID + " on " + device.Snapshot.Name)
	return nil
}

func (s *HostService) latestReadyDevice() (activeDevice, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var target activeDevice
	found := false
	latest := time.Time{}
	for _, device := range s.devices {
		if !device.Ready || device.VideoOnly || device.Conn == nil || device.WriteMu == nil {
			continue
		}
		connectedAt, _ := time.Parse(time.RFC3339, device.Snapshot.ConnectedAt)
		if !found || connectedAt.After(latest) {
			target, latest, found = device, connectedAt, true
		}
	}
	if !found {
		return activeDevice{}, errors.New("no connected Android HMD")
	}
	return target, nil
}

func normalizeOpticalPresets(presets []OpticalPreset) []OpticalPreset {
	result := make([]OpticalPreset, 0, len(presets))
	seen := make(map[string]struct{}, len(presets))
	for _, preset := range presets {
		preset.ID = strings.TrimSpace(preset.ID)
		preset.Name = strings.TrimSpace(preset.Name)
		if preset.ID == "" || preset.Name == "" {
			continue
		}
		if _, exists := seen[preset.ID]; exists {
			continue
		}
		seen[preset.ID] = struct{}{}
		preset.Settings = preset.Settings.Normalized()
		result = append(result, preset)
	}
	return result
}
