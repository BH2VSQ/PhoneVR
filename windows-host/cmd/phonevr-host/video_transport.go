package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type VideoTransportMode string

const (
	VideoTransportAuto     VideoTransportMode = "auto"
	VideoTransportUSB      VideoTransportMode = "usb"
	VideoTransportNetwork  VideoTransportMode = "network"
	VideoTransportParallel VideoTransportMode = "parallel"
)

type VideoTransportPreference string

const (
	VideoPreferUSB     VideoTransportPreference = "usb"
	VideoPreferNetwork VideoTransportPreference = "network"
)

type VideoTransportSnapshot struct {
	Mode                    VideoTransportMode       `json:"mode"`
	Preferred               VideoTransportPreference `json:"preferred"`
	Active                  string                   `json:"active"`
	USBAvailable            bool                     `json:"usbAvailable"`
	NetworkAvailable        bool                     `json:"networkAvailable"`
	NetworkRXBytesPerSecond uint64                   `json:"networkRxBytesPerSecond"`
	USBRXBytesPerSecond     uint64                   `json:"usbRxBytesPerSecond"`
	NetworkSharePercent     int                      `json:"networkSharePercent"`
	USBSharePercent         int                      `json:"usbSharePercent"`
	LastSwitchAt            string                   `json:"lastSwitchAt,omitempty"`
	LastSwitchReason        string                   `json:"lastSwitchReason,omitempty"`
}

const videoPathProbePayload = "PVRP\x01"

type videoTransportConfig struct {
	Mode      VideoTransportMode       `json:"mode"`
	Preferred VideoTransportPreference `json:"preferred"`
}

func videoTransportConfigPath() (string, error) {
	base, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(base, "PhoneVRStream", "video-transport.json"), nil
}

func loadVideoTransportConfigFrom(path string) (VideoTransportMode, VideoTransportPreference) {
	mode, preferred := VideoTransportNetwork, VideoPreferNetwork
	if strings.TrimSpace(path) == "" {
		return mode, preferred
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return mode, preferred
	}
	var config videoTransportConfig
	if json.Unmarshal(data, &config) != nil {
		return mode, preferred
	}
	return normalizeVideoTransportMode(string(config.Mode)), normalizeVideoTransportPreference(string(config.Preferred))
}

func saveVideoTransportConfigTo(path string, mode VideoTransportMode, preferred VideoTransportPreference) error {
	if strings.TrimSpace(path) == "" {
		return errors.New("video transport settings path is empty")
	}
	config := videoTransportConfig{
		Mode:      normalizeVideoTransportMode(string(mode)),
		Preferred: normalizeVideoTransportPreference(string(preferred)),
	}
	data, err := json.MarshalIndent(config, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o600)
}

type transportHealth struct {
	ProbeAt                         time.Time
	FeedbackAt                      time.Time
	LastPacketAgeMS                 int64
	LastDecodedAgeMS                int64
	UnhealthyUntil                  time.Time
	LastError                       string
	ReceiveBytesPerSecond           uint64
	AssignedSharePermille           int
	EstimatedCapacityBytesPerSecond float64
}

type transportCandidateGroup struct {
	deviceID    string
	connectedAt time.Time
	usb         *activeDevice
	network     *activeDevice
}

func videoTransportUsesAOA(mode VideoTransportMode) bool {
	return mode != VideoTransportNetwork
}

func normalizeVideoTransportMode(mode string) VideoTransportMode {
	switch VideoTransportMode(strings.ToLower(strings.TrimSpace(mode))) {
	case VideoTransportAuto:
		return VideoTransportAuto
	case VideoTransportUSB:
		return VideoTransportUSB
	case VideoTransportNetwork:
		return VideoTransportNetwork
	case VideoTransportParallel:
		return VideoTransportParallel
	default:
		return VideoTransportNetwork
	}
}

func normalizeVideoTransportPreference(preference string) VideoTransportPreference {
	if strings.EqualFold(strings.TrimSpace(preference), string(VideoPreferUSB)) {
		return VideoPreferUSB
	}
	return VideoPreferNetwork
}

// SetVideoTransport changes the requested video route without restarting the
// capture pipeline or H.264 encoder. Auto mode keeps a healthy active route and
// uses the preferred route when a new session becomes available.
func (s *HostService) SetVideoTransport(mode, preferred string) error {
	normalizedMode := normalizeVideoTransportMode(mode)
	normalizedPreference := normalizeVideoTransportPreference(preferred)
	s.transportConfigMu.Lock()
	defer s.transportConfigMu.Unlock()
	s.mu.RLock()
	changed := s.transportMode != normalizedMode || s.transportPreference != normalizedPreference
	configPath := s.transportConfigPath
	s.mu.RUnlock()
	if changed && configPath != "" {
		if err := saveVideoTransportConfigTo(configPath, normalizedMode, normalizedPreference); err != nil {
			return fmt.Errorf("save video transport settings: %w", err)
		}
	}
	s.mu.Lock()
	s.transportMode = normalizedMode
	s.transportPreference = normalizedPreference
	if changed {
		s.activeVideoTransport = ""
		s.activeVideoDeviceID = ""
		s.activeVideoSince = time.Time{}
		s.lastTransportSwitchReason = "用户更改了传输模式或优先级"
		s.lastTransportSwitchAt = time.Now()
	}
	s.mu.Unlock()
	if changed {
		// Wake the AOA discovery loop immediately. Network-only mode must not
		// enumerate WinUSB interfaces in the background; USB/auto/parallel
		// modes resume scanning without requiring a Host restart.
		select {
		case s.usbScanWake <- struct{}{}:
		default:
		}
		s.addLog(fmt.Sprintf("Video transport mode: %s, preferred: %s", normalizedMode, normalizedPreference))
	}
	return nil
}

func (s *HostService) videoTransportSnapshotLocked() VideoTransportSnapshot {
	mode := s.transportMode
	if mode == "" {
		mode = VideoTransportNetwork
	}
	preferred := s.transportPreference
	if preferred == "" {
		preferred = VideoPreferNetwork
	}
	groups := s.transportCandidatesLocked()
	var usbAvailable, networkAvailable bool
	for _, group := range groups {
		usbAvailable = usbAvailable || s.transportCandidateHealthyLocked(group.deviceID, "usb", group.usb)
		networkAvailable = networkAvailable || s.transportCandidateHealthyLocked(group.deviceID, "network", group.network)
	}
	result := VideoTransportSnapshot{
		Mode: mode, Preferred: preferred, Active: s.activeVideoTransport,
		USBAvailable: usbAvailable, NetworkAvailable: networkAvailable,
		LastSwitchReason: s.lastTransportSwitchReason,
	}
	if group := s.transportCandidatesLocked()[s.activeVideoDeviceID]; group != nil {
		network := s.transportHealth[s.activeVideoDeviceID+"|network"]
		usb := s.transportHealth[s.activeVideoDeviceID+"|usb"]
		result.NetworkRXBytesPerSecond = network.ReceiveBytesPerSecond
		result.USBRXBytesPerSecond = usb.ReceiveBytesPerSecond
		if s.activeVideoTransport == "parallel" {
			usbShare := s.videoStripeUSBSharePermilleLocked(s.activeVideoDeviceID)
			result.USBSharePercent = usbShare / 10
			result.NetworkSharePercent = 100 - result.USBSharePercent
		}
	}
	if !s.lastTransportSwitchAt.IsZero() {
		result.LastSwitchAt = s.lastTransportSwitchAt.Format(time.RFC3339Nano)
	}
	return result
}

func (s *HostService) transportCandidatesLocked() map[string]*transportCandidateGroup {
	groups := make(map[string]*transportCandidateGroup)
	for key, deviceValue := range s.devices {
		device := deviceValue
		if !device.Ready || !device.VideoEnabled || device.Conn == nil {
			continue
		}
		deviceID := strings.TrimSpace(device.Snapshot.DeviceID)
		if deviceID == "" {
			// Older clients do not send a persistent ID. Keep their paths isolated
			// instead of accidentally pairing two phones with the same model name.
			deviceID = "legacy:" + key
		}
		group := groups[deviceID]
		if group == nil {
			group = &transportCandidateGroup{deviceID: deviceID}
			groups[deviceID] = group
		}
		connectedAt, _ := time.Parse(time.RFC3339, device.Snapshot.ConnectedAt)
		if connectedAt.After(group.connectedAt) {
			group.connectedAt = connectedAt
		}
		if device.USBLink != nil {
			group.usb = &device
			continue
		}
		if tcp, ok := device.Conn.RemoteAddr().(*net.TCPAddr); ok && tcp.IP != nil {
			group.network = &device
		}
	}
	return groups
}

func (s *HostService) transportCandidateHealthyLocked(deviceID, transport string, device *activeDevice) bool {
	if device == nil || !device.Ready || !device.VideoEnabled || device.Conn == nil {
		return false
	}
	key := deviceID + "|" + transport
	health := s.transportHealth[key]
	now := time.Now()
	if !health.UnhealthyUntil.IsZero() && now.Before(health.UnhealthyUntil) {
		return false
	}
	if !device.LastPingAt.IsZero() && now.Sub(device.LastPingAt) > 7*time.Second {
		return false
	}
	// Route reachability is proven by a receiver acknowledgement to a tiny
	// packet sent over this exact path. Do not use last-media-packet age as a
	// route-health gate: an encoder stall can stop media packets even while Wi-Fi
	// remains healthy, and demoting the route would also tear down the SteamVR
	// capture bridge that is needed when encoding resumes.
	return !health.ProbeAt.IsZero() && now.Sub(health.ProbeAt) <= 4*time.Second
}

func (s *HostService) expireSilentActiveTransportLocked(now time.Time) {
	if s.activeVideoTransport == "parallel" {
		// Striped mode validates each path independently through the one-second
		// PVRP probes. The receiver's latest-source field can only name one of
		// the two interleaved copies, so it is not a suitable aggregate watchdog.
		return
	}
	if s.activeVideoDeviceID == "" || s.activeVideoTransport == "" || s.activeVideoSince.IsZero() ||
		now.Sub(s.activeVideoSince) <= 4*time.Second {
		return
	}
	key := s.activeVideoDeviceID + "|" + s.activeVideoTransport
	health := s.transportHealth[key]
	stale := health.ProbeAt.IsZero() || now.Sub(health.ProbeAt) > 4*time.Second
	if !stale || (!health.UnhealthyUntil.IsZero() && now.Before(health.UnhealthyUntil)) {
		return
	}
	health.UnhealthyUntil = now.Add(5 * time.Second)
	health.LastError = s.activeVideoTransport + " 视频路径探测应答超时"
	s.transportHealth[key] = health
	s.lastTransportSwitchReason = health.LastError
}

// videoReorderGraceMilliseconds keeps the single-path receiver responsive while
// allowing extra latency for fragments intentionally split over UDP and AOA.
func (s *HostService) videoReorderGraceMilliseconds() int {
	s.mu.RLock()
	mode := s.transportMode
	s.mu.RUnlock()
	if mode == VideoTransportParallel {
		return 120
	}
	return 60
}

func (s *HostService) currentVideoTarget() (videoTarget, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.expireSilentActiveTransportLocked(time.Now())
	groups := s.transportCandidatesLocked()
	if len(groups) == 0 {
		return videoTarget{}, false
	}
	ordered := make([]*transportCandidateGroup, 0, len(groups))
	for _, group := range groups {
		ordered = append(ordered, group)
	}
	// A newer active Android session wins when more than one headset is paired.
	for i := 0; i < len(ordered); i++ {
		for j := i + 1; j < len(ordered); j++ {
			if ordered[j].connectedAt.After(ordered[i].connectedAt) {
				ordered[i], ordered[j] = ordered[j], ordered[i]
			}
		}
	}
	mode := s.transportMode
	if mode == "" {
		mode = VideoTransportNetwork
	}
	preferred := s.transportPreference
	if preferred == "" {
		preferred = VideoPreferNetwork
	}
	for _, group := range ordered {
		usbOK := s.transportCandidateHealthyLocked(group.deviceID, "usb", group.usb)
		networkOK := s.transportCandidateHealthyLocked(group.deviceID, "network", group.network)
		selected := ""
		switch mode {
		case VideoTransportParallel:
			if usbOK || networkOK {
				selected = "parallel"
			}
		case VideoTransportUSB:
			if usbOK {
				selected = "usb"
			}
		case VideoTransportNetwork:
			if networkOK {
				selected = "network"
			}
		default:
			// Preserve the currently selected route while it remains healthy.
			if s.activeVideoDeviceID == group.deviceID {
				if s.activeVideoTransport == "usb" && usbOK {
					selected = "usb"
				} else if s.activeVideoTransport == "network" && networkOK {
					selected = "network"
				}
			}
			if selected == "" {
				if preferred == VideoPreferUSB {
					if usbOK {
						selected = "usb"
					} else if networkOK {
						selected = "network"
					}
				} else {
					if networkOK {
						selected = "network"
					} else if usbOK {
						selected = "usb"
					}
				}
			}
		}
		if selected == "" {
			continue
		}
		var target videoTarget
		target.DeviceID = group.deviceID
		target.Transport = selected
		target.Switched = s.activeVideoTransport != selected || s.activeVideoDeviceID != group.deviceID
		if selected == "parallel" {
			if usbOK {
				target.USB = group.usb.USBLink
			}
			if networkOK {
				tcp := group.network.Conn.RemoteAddr().(*net.TCPAddr)
				target.UDP = &net.UDPAddr{IP: append(net.IP(nil), tcp.IP...), Port: androidVideoPort}
			}
		} else if selected == "usb" {
			target.USB = group.usb.USBLink
		} else {
			tcp := group.network.Conn.RemoteAddr().(*net.TCPAddr)
			target.UDP = &net.UDPAddr{IP: append(net.IP(nil), tcp.IP...), Port: androidVideoPort}
		}
		if s.activeVideoTransport != selected || s.activeVideoDeviceID != group.deviceID {
			previous := s.activeVideoTransport
			if previous == "" {
				previous = "none"
			}
			s.activeVideoTransport = selected
			s.activeVideoDeviceID = group.deviceID
			s.activeVideoSince = time.Now()
			s.lastTransportSwitchAt = s.activeVideoSince
			reason := "备用链路可用"
			if health := s.transportHealth[group.deviceID+"|"+previous]; previous != "none" && health.LastError != "" {
				reason = health.LastError
			} else if previous == "none" && s.lastTransportSwitchReason == "" {
				reason = "首次选择可用链路"
			} else if previous == "none" {
				reason = s.lastTransportSwitchReason
			}
			s.lastTransportSwitchReason = reason
			// Avoid producing logs while the service mutex is held. The snapshot
			// is sufficient for UI visibility; send failures are logged separately.
		}
		return target, true
	}
	s.activeVideoTransport = ""
	s.activeVideoDeviceID = ""
	s.activeVideoSince = time.Time{}
	return videoTarget{}, false
}

func (s *HostService) markVideoTransportFailure(target videoTarget, err error) {
	if target.Transport == "" || target.DeviceID == "" {
		return
	}
	s.mu.Lock()
	key := target.DeviceID + "|" + target.Transport
	health := s.transportHealth[key]
	health.UnhealthyUntil = time.Now().Add(3 * time.Second)
	if err != nil {
		health.LastError = fmt.Sprintf("%s 发送失败：%v", target.Transport, err)
		s.lastTransportSwitchReason = health.LastError
	}
	s.transportHealth[key] = health
	// Keep the failed route as the previous active path until the next target
	// selection so the switch reason and route transition remain observable.
	s.lastTransportSwitchAt = time.Now()
	s.mu.Unlock()
}

func (s *HostService) updateVideoTransportFeedback(deviceID, transport string, packetAgeMS, decodedAgeMS int64, probe bool) {
	s.updateVideoTransportFeedbackWithRate(deviceID, transport, packetAgeMS, decodedAgeMS, probe, 0)
}

func (s *HostService) updateVideoTransportFeedbackWithRate(deviceID, transport string, packetAgeMS, decodedAgeMS int64, probe bool, receiveBytesPerSecond uint64) {
	transport = strings.ToLower(strings.TrimSpace(transport))
	if transport != "usb" && transport != "network" {
		return
	}
	if packetAgeMS < -1 {
		packetAgeMS = -1
	}
	if decodedAgeMS < -1 {
		decodedAgeMS = -1
	}
	var probeLog string
	s.mu.Lock()
	key := strings.TrimSpace(deviceID) + "|" + transport
	if strings.TrimSpace(deviceID) != "" {
		health := s.transportHealth[key]
		now := time.Now()
		probeWasFresh := !health.ProbeAt.IsZero() && now.Sub(health.ProbeAt) <= 2*time.Second
		health.FeedbackAt = now
		if probe {
			if !probeWasFresh {
				probeLog = fmt.Sprintf("Video path probe acknowledged: device=%s transport=%s", strings.TrimSpace(deviceID), transport)
			}
			health.ProbeAt = now
			health.UnhealthyUntil = time.Time{}
		} else {
			health.LastPacketAgeMS = packetAgeMS
			health.LastDecodedAgeMS = decodedAgeMS
			if packetAgeMS >= 0 && packetAgeMS <= 1800 {
				health.ProbeAt = now
				health.UnhealthyUntil = time.Time{}
			}
		}
		health.ReceiveBytesPerSecond = receiveBytesPerSecond
		if health.AssignedSharePermille > 0 {
			capacitySample := float64(receiveBytesPerSecond) * 1000 / float64(health.AssignedSharePermille)
			if health.EstimatedCapacityBytesPerSecond <= 0 {
				health.EstimatedCapacityBytesPerSecond = capacitySample
			} else {
				health.EstimatedCapacityBytesPerSecond = health.EstimatedCapacityBytesPerSecond*0.65 + capacitySample*0.35
			}
		}
		s.transportHealth[key] = health
	}
	s.mu.Unlock()
	if probeLog != "" {
		s.addLog(probeLog)
	}
}

func (s *HostService) videoStripeUSBSharePermilleLocked(deviceID string) int {
	usb := s.transportHealth[deviceID+"|usb"].EstimatedCapacityBytesPerSecond
	network := s.transportHealth[deviceID+"|network"].EstimatedCapacityBytesPerSecond
	if usb <= 0 || network <= 0 {
		return 500
	}
	share := int(usb / (usb + network) * 1000)
	// Keep a small minimum share for both healthy paths, while allowing a
	// much slower route to stay below 25% instead of filling its USB queue.
	if share < 50 {
		share = 50
	}
	if share > 950 {
		share = 950
	}
	return share
}

func (s *HostService) videoStripeUSBSharePermille(deviceID string) int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.videoStripeUSBSharePermilleLocked(deviceID)
}

func (s *HostService) setVideoTransportAssignedShares(deviceID string, networkBytes, usbBytes int) {
	total := networkBytes + usbBytes
	if total <= 0 {
		return
	}
	s.mu.Lock()
	networkKey := deviceID + "|network"
	usbKey := deviceID + "|usb"
	network := s.transportHealth[networkKey]
	usb := s.transportHealth[usbKey]
	networkShare := networkBytes * 1000 / total
	usbShare := usbBytes * 1000 / total
	if network.AssignedSharePermille == 0 {
		network.AssignedSharePermille = networkShare
	} else {
		network.AssignedSharePermille = (network.AssignedSharePermille*85 + networkShare*15) / 100
	}
	if usb.AssignedSharePermille == 0 {
		usb.AssignedSharePermille = usbShare
	} else {
		usb.AssignedSharePermille = (usb.AssignedSharePermille*85 + usbShare*15) / 100
	}
	s.transportHealth[networkKey] = network
	s.transportHealth[usbKey] = usb
	s.mu.Unlock()
}

func (s *HostService) videoTransportStatus() VideoTransportSnapshot {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.videoTransportSnapshotLocked()
}

var errVideoTransportUnavailable = errors.New("selected video transport is unavailable")

// probeStandbyVideoPaths sends a tiny datagram over every currently connected
// video path, including the active route. Android acknowledges only after the
// packet reaches the shared video receiver, proving that the route is reachable
// even when no encoded media frame is currently being produced.
func (s *HostService) probeStandbyVideoPaths() {
	now := time.Now()
	s.mu.Lock()
	if !s.lastVideoPathProbeAt.IsZero() && now.Sub(s.lastVideoPathProbeAt) < time.Second {
		s.mu.Unlock()
		return
	}
	s.lastVideoPathProbeAt = now
	groups := s.transportCandidatesLocked()
	udp := s.videoUDP
	type probeTarget struct {
		target videoTarget
	}
	var targets []probeTarget
	for deviceID, group := range groups {
		// Probe active and standby paths alike. The acknowledgement keeps the
		// socket/bulk receiver health independent from whether the encoder is
		// currently producing media frames.
		if group.usb != nil {
			targets = append(targets, probeTarget{target: videoTarget{USB: group.usb.USBLink, Transport: "usb", DeviceID: deviceID}})
		}
		if group.network != nil {
			if tcp, ok := group.network.Conn.RemoteAddr().(*net.TCPAddr); ok && tcp.IP != nil {
				targets = append(targets, probeTarget{target: videoTarget{
					UDP:       &net.UDPAddr{IP: append(net.IP(nil), tcp.IP...), Port: androidVideoPort},
					Transport: "network", DeviceID: deviceID,
				}})
			}
		}
	}
	s.mu.Unlock()
	for _, item := range targets {
		target := item.target
		if target.USB != nil {
			// Keep even standby-path probes off the video sender goroutine. A
			// stalled WinUSB transfer must never pause the active UDP route.
			work := usbVideoWork{
				link: target.USB, deviceID: target.DeviceID,
				fragments: [][]byte{[]byte(videoPathProbePayload)}, isProbe: true,
			}
			if err := s.enqueueUSBVideoWork(work); err != nil {
				s.markVideoTransportFailure(target, err)
			}
			continue
		}
		if udp != nil && target.UDP != nil {
			if _, err := udp.WriteToUDP([]byte(videoPathProbePayload), target.UDP); err != nil {
				s.markVideoTransportFailure(target, err)
			}
		}
	}
}
