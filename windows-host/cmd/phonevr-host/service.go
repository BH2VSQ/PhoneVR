package main

import (
	"bufio"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/vsq-studio/phonevr-host/internal/encoder"
	"github.com/vsq-studio/phonevr-host/internal/protocol"
)

const (
	protocolVersion      = 1
	controlIdleTimeout   = 15 * time.Second
	controlWriteTimeout  = 5 * time.Second
	discoveryPortDefault = 48100
	controlPortDefault   = 48101
	posePortDefault      = 48102
	steamVRStatusPort    = 48105
	discoveryQuery       = "PHONEVR_DISCOVER_V1"
	discoveryPrefix      = "PHONEVR_HOST_V1 "
	maxLogEntries        = 250
)

type HostConfig struct {
	ListenAddress string `json:"listenAddress"`
	DiscoveryPort int    `json:"discoveryPort"`
	ControlPort   int    `json:"controlPort"`
	PosePort      int    `json:"posePort"`
}

func DefaultHostConfig() HostConfig {
	return HostConfig{ListenAddress: "0.0.0.0", DiscoveryPort: discoveryPortDefault, ControlPort: controlPortDefault, PosePort: posePortDefault}
}

type DeviceSnapshot struct {
	Name              string                `json:"name"`
	DeviceID          string                `json:"deviceId,omitempty"`
	Client            string                `json:"client,omitempty"`
	Address           string                `json:"address"`
	ConnectedAt       string                `json:"connectedAt"`
	DisplaySettings   *PhoneDisplaySettings `json:"displaySettings,omitempty"`
	OpticalPresets    []OpticalPreset       `json:"opticalPresets,omitempty"`
	MediaCapabilities json.RawMessage       `json:"mediaCapabilities,omitempty"`
	BatteryLevel      *int                  `json:"batteryLevel,omitempty"`
	Charging          bool                  `json:"charging,omitempty"`
}

type PoseSnapshot struct {
	Sequence    uint32  `json:"sequence"`
	TimestampNs int64   `json:"timestampNs"`
	X           float32 `json:"x"`
	Y           float32 `json:"y"`
	Z           float32 `json:"z"`
	W           float32 `json:"w"`
	Yaw         float64 `json:"yaw"`
	Pitch       float64 `json:"pitch"`
	Roll        float64 `json:"roll"`
	Source      string  `json:"source"`
	ReceivedAt  string  `json:"receivedAt"`
}

type HostSnapshot struct {
	Running                  bool             `json:"running"`
	ServerName               string           `json:"serverName"`
	ListenAddress            string           `json:"listenAddress"`
	LocalIPv4                []string         `json:"localIPv4"`
	DiscoveryPort            int              `json:"discoveryPort"`
	ControlPort              int              `json:"controlPort"`
	PosePort                 int              `json:"posePort"`
	ConnectedDevices         []DeviceSnapshot `json:"connectedDevices"`
	LatestPose               *PoseSnapshot    `json:"latestPose,omitempty"`
	PacketsReceived          uint64           `json:"packetsReceived"`
	LastPacketAt             string           `json:"lastPacketAt,omitempty"`
	Uptime                   string           `json:"uptime,omitempty"`
	LastError                string           `json:"lastError,omitempty"`
	SteamVRConnected         bool             `json:"steamVRConnected"`
	SteamVRDriverLoaded      bool             `json:"steamVRDriverLoaded"`
	SteamVRHMDActive         bool             `json:"steamVRHmdActive"`
	SteamVRPoseValid         bool             `json:"steamVRPoseValid"`
	SteamVRDisplayActive     bool             `json:"steamVRDisplayActive"`
	SteamVRHasPresentedFrame bool             `json:"steamVRHasPresentedFrame"`
	SteamVRFrameCounter      uint64           `json:"steamVRFrameCounter"`
	SteamVRHeartbeatAgeMS    int64            `json:"steamVRHeartbeatAgeMs,omitempty"`
	Logs                     []string         `json:"logs"`
}

type PoseFrameSnapshot struct {
	LatestPose      *PoseSnapshot `json:"latestPose,omitempty"`
	PacketsReceived uint64        `json:"packetsReceived"`
}

type helloMessage struct {
	Type              string          `json:"type"`
	Protocol          int             `json:"protocol"`
	DeviceName        string          `json:"deviceName"`
	DeviceID          string          `json:"deviceId"`
	Client            string          `json:"client"`
	MediaCapabilities json.RawMessage `json:"mediaCapabilities,omitempty"`
}

type discoveryMessage struct {
	Name        string `json:"name"`
	ControlPort int    `json:"controlPort"`
	Protocol    int    `json:"protocol"`
}

type welcomeMessage struct {
	Type         string   `json:"type"`
	Protocol     int      `json:"protocol"`
	SessionID    string   `json:"sessionId"`
	ServerName   string   `json:"serverName"`
	PosePort     int      `json:"posePort"`
	Capabilities []string `json:"capabilities"`
}

type controlMessage struct {
	Type         string                `json:"type"`
	RequestID    string                `json:"requestId,omitempty"`
	Settings     *PhoneDisplaySettings `json:"settings,omitempty"`
	Presets      []OpticalPreset       `json:"presets,omitempty"`
	PresetID     string                `json:"presetId,omitempty"`
	Action       string                `json:"action,omitempty"`
	Status       string                `json:"status,omitempty"`
	Message      string                `json:"message,omitempty"`
	BatteryLevel *int                  `json:"batteryLevel,omitempty"`
	Charging     bool                  `json:"charging,omitempty"`
}

type activeDevice struct {
	Snapshot DeviceSnapshot
	Conn     net.Conn
	WriteMu  *sync.Mutex
	Ready    bool
}

type HostService struct {
	lifecycleMu         sync.Mutex
	mu                  sync.RWMutex
	config              HostConfig
	serverName          string
	startedAt           time.Time
	listener            net.Listener
	discovery           *net.UDPConn
	pose                *net.UDPConn
	steamVRStatus       *net.UDPConn
	steamVRHeartbeatAt  time.Time
	steamVRFlags        uint8
	steamVRFrameCounter uint64
	cancel              context.CancelFunc
	wg                  sync.WaitGroup
	devices             map[string]activeDevice
	connections         map[net.Conn]struct{}
	latestPose          *PoseSnapshot
	packets             uint64
	lastPacket          time.Time
	lastError           string
	logs                []string
	consoleLogs         bool
}

func NewHostService(consoleLogs bool) *HostService {
	return &HostService{
		devices: make(map[string]activeDevice), connections: make(map[net.Conn]struct{}),
		consoleLogs: consoleLogs,
	}
}

func (s *HostService) Start(config HostConfig) error {
	s.lifecycleMu.Lock()
	defer s.lifecycleMu.Unlock()

	config = normalizeHostConfig(config)
	s.mu.Lock()
	if s.listener != nil {
		s.mu.Unlock()
		return errors.New("PhoneVR Host is already running")
	}
	serverName, hostErr := localHostName()
	if hostErr != nil || strings.TrimSpace(serverName) == "" {
		serverName = "PhoneVR Host"
	}
	s.config = config
	s.serverName = serverName

	tcpListener, err := net.Listen("tcp4", net.JoinHostPort(config.ListenAddress, fmt.Sprint(config.ControlPort)))
	if err != nil {
		s.mu.Unlock()
		return fmt.Errorf("start TCP control listener: %w", err)
	}
	udpDiscovery, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.ParseIP(config.ListenAddress), Port: config.DiscoveryPort})
	if err != nil {
		_ = tcpListener.Close()
		s.mu.Unlock()
		return fmt.Errorf("start UDP discovery listener: %w", err)
	}
	udpPose, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.ParseIP(config.ListenAddress), Port: config.PosePort})
	if err != nil {
		_ = tcpListener.Close()
		_ = udpDiscovery.Close()
		s.mu.Unlock()
		return fmt.Errorf("start UDP pose listener: %w", err)
	}
	// A larger receive buffer absorbs short Wi-Fi scheduling bursts. UDP pose
	// packets are still latest-state data, so this is a jitter cushion rather
	// than a queue that the client must drain in sequence.
	_ = udpPose.SetReadBuffer(256 * 1024)

	// The SteamVR server driver runs in vrserver.exe, not in this Host process.
	// A loopback-only heartbeat is the authoritative signal that the driver is
	// loaded and its RunFrame callback is still being serviced by the runtime.
	udpSteamVRStatus, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: steamVRStatusPort})
	if err != nil {
		_ = tcpListener.Close()
		_ = udpDiscovery.Close()
		_ = udpPose.Close()
		s.mu.Unlock()
		return fmt.Errorf("start SteamVR status listener on 127.0.0.1:%d: %w", steamVRStatusPort, err)
	}
	_ = udpSteamVRStatus.SetReadBuffer(32 * 1024)

	ctx, cancel := context.WithCancel(context.Background())
	config.DiscoveryPort = udpDiscovery.LocalAddr().(*net.UDPAddr).Port
	config.ControlPort = tcpListener.Addr().(*net.TCPAddr).Port
	config.PosePort = udpPose.LocalAddr().(*net.UDPAddr).Port
	s.config = config
	s.serverName = serverName
	s.startedAt = time.Now()
	s.listener = tcpListener
	s.discovery = udpDiscovery
	s.pose = udpPose
	s.steamVRStatus = udpSteamVRStatus
	s.steamVRHeartbeatAt = time.Time{}
	s.steamVRFlags = 0
	s.steamVRFrameCounter = 0
	s.cancel = cancel
	s.devices = make(map[string]activeDevice)
	s.connections = make(map[net.Conn]struct{})
	s.latestPose = nil
	s.packets = 0
	s.lastPacket = time.Time{}
	s.lastError = ""
	s.mu.Unlock()

	s.addLog(fmt.Sprintf("Host started on %s | TCP %d | UDP discovery %d | UDP pose %d", config.ListenAddress, config.ControlPort, config.DiscoveryPort, config.PosePort))
	for _, ip := range localIPv4Addresses() {
		s.addLog("LAN address: " + ip)
	}

	s.wg.Add(4)
	go s.acceptLoop(ctx, tcpListener)
	go s.discoveryLoop(ctx, udpDiscovery, serverName, config.ControlPort)
	go s.poseLoop(ctx, udpPose)
	go s.steamVRStatusLoop(ctx, udpSteamVRStatus)
	return nil
}

func normalizeHostConfig(config HostConfig) HostConfig {
	defaults := DefaultHostConfig()
	if strings.TrimSpace(config.ListenAddress) == "" {
		config.ListenAddress = defaults.ListenAddress
	}
	if net.ParseIP(config.ListenAddress) == nil {
		config.ListenAddress = defaults.ListenAddress
	}
	if config.DiscoveryPort < 0 || config.DiscoveryPort > 65535 {
		config.DiscoveryPort = defaults.DiscoveryPort
	}
	if config.ControlPort < 0 || config.ControlPort > 65535 {
		config.ControlPort = defaults.ControlPort
	}
	if config.PosePort < 0 || config.PosePort > 65535 {
		config.PosePort = defaults.PosePort
	}
	if config.DiscoveryPort == 0 && config.ControlPort == 0 && config.PosePort == 0 {
		return defaults
	}
	return config
}

// ProbeVideoEncoders runs a bounded, best-effort FFmpeg encoder inventory.
// Results are candidates only; actual adapter/session initialization is a
// required later validation before a streaming profile can be selected.
func (s *HostService) ProbeVideoEncoders(ffmpegPath string) {
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		candidates, err := encoder.ProbeFFmpeg(ctx, ffmpegPath)
		if err != nil {
			s.addLog("Video encoder discovery unavailable: " + err.Error())
			return
		}
		for _, candidate := range candidates {
			s.addLog(fmt.Sprintf("Video encoder candidate (adapter/session unverified): %s | %s | %s", candidate.Name, candidate.Codec, candidate.Backend))
		}
	}()
}

func (s *HostService) Stop() {
	s.lifecycleMu.Lock()
	defer s.lifecycleMu.Unlock()

	s.mu.Lock()
	if s.listener == nil {
		s.mu.Unlock()
		return
	}
	cancel := s.cancel
	listener := s.listener
	discovery := s.discovery
	pose := s.pose
	steamVRStatus := s.steamVRStatus
	connections := make([]net.Conn, 0, len(s.connections))
	for conn := range s.connections {
		connections = append(connections, conn)
	}
	s.listener = nil
	s.discovery = nil
	s.pose = nil
	s.steamVRStatus = nil
	s.steamVRHeartbeatAt = time.Time{}
	s.steamVRFlags = 0
	s.steamVRFrameCounter = 0
	s.cancel = nil
	s.devices = make(map[string]activeDevice)
	s.connections = make(map[net.Conn]struct{})
	s.latestPose = nil
	s.lastPacket = time.Time{}
	s.mu.Unlock()

	if cancel != nil {
		cancel()
	}
	_ = listener.Close()
	_ = discovery.Close()
	_ = pose.Close()
	if steamVRStatus != nil {
		_ = steamVRStatus.Close()
	}
	for _, conn := range connections {
		_ = conn.Close()
	}
	s.wg.Wait()
	s.addLog("Host stopped; TCP and UDP listeners released")
}

func (s *HostService) Snapshot() HostSnapshot {
	s.mu.RLock()
	running := s.listener != nil
	snapshot := HostSnapshot{
		Running: running, ServerName: s.serverName, ListenAddress: s.config.ListenAddress,
		LocalIPv4: localIPv4Addresses(), DiscoveryPort: s.config.DiscoveryPort,
		ControlPort: s.config.ControlPort, PosePort: s.config.PosePort,
		LatestPose: s.latestPose, PacketsReceived: s.packets,
		LastError: s.lastError,
	}
	if !s.steamVRHeartbeatAt.IsZero() {
		age := time.Since(s.steamVRHeartbeatAt)
		snapshot.SteamVRHeartbeatAgeMS = age.Milliseconds()
		if age <= 2500*time.Millisecond {
			snapshot.SteamVRConnected = s.steamVRFlags&steamVRFlagDriverLoaded != 0
			snapshot.SteamVRDriverLoaded = s.steamVRFlags&steamVRFlagDriverLoaded != 0
			snapshot.SteamVRHMDActive = s.steamVRFlags&steamVRFlagHMDActive != 0
			snapshot.SteamVRPoseValid = s.steamVRFlags&steamVRFlagPoseValid != 0
			snapshot.SteamVRDisplayActive = s.steamVRFlags&steamVRFlagDisplayActive != 0
			snapshot.SteamVRHasPresentedFrame = s.steamVRFlags&steamVRFlagPresentedFrame != 0
			snapshot.SteamVRFrameCounter = s.steamVRFrameCounter
		}
	}
	for _, device := range s.devices {
		snapshot.ConnectedDevices = append(snapshot.ConnectedDevices, device.Snapshot)
	}
	if running {
		snapshot.Uptime = time.Since(s.startedAt).Round(time.Second).String()
	}
	if !s.lastPacket.IsZero() {
		snapshot.LastPacketAt = s.lastPacket.Format(time.RFC3339)
	}
	snapshot.Logs = append([]string(nil), s.logs...)
	s.mu.RUnlock()
	sort.Slice(snapshot.ConnectedDevices, func(i, j int) bool {
		return snapshot.ConnectedDevices[i].ConnectedAt < snapshot.ConnectedDevices[j].ConnectedAt
	})
	return snapshot
}

// PoseFrame returns only the high-frequency head-pose state so the native desktop
// OpenGL viewer can refresh the HMD model without polling device and log
// metadata on every animation update.
func (s *HostService) PoseFrame() PoseFrameSnapshot {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var latest *PoseSnapshot
	if s.latestPose != nil {
		copyPose := *s.latestPose
		latest = &copyPose
	}
	return PoseFrameSnapshot{LatestPose: latest, PacketsReceived: s.packets}
}

func (s *HostService) acceptLoop(ctx context.Context, listener net.Listener) {
	defer s.wg.Done()
	for {
		conn, err := listener.Accept()
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			s.setError(fmt.Sprintf("TCP accept failed: %v", err))
			continue
		}
		s.mu.Lock()
		if s.listener == nil {
			s.mu.Unlock()
			_ = conn.Close()
			return
		}
		s.connections[conn] = struct{}{}
		s.mu.Unlock()
		s.wg.Add(1)
		go func() {
			defer s.wg.Done()
			s.handleControlConnection(ctx, conn)
		}()
	}
}

func (s *HostService) discoveryLoop(ctx context.Context, conn *net.UDPConn, serverName string, tcpPort int) {
	defer s.wg.Done()
	data, _ := json.Marshal(discoveryMessage{Name: serverName, ControlPort: tcpPort, Protocol: protocolVersion})
	response := []byte(discoveryPrefix + string(data))
	buffer := make([]byte, 512)
	for {
		_ = conn.SetReadDeadline(time.Now().Add(time.Second))
		n, remote, err := conn.ReadFromUDP(buffer)
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			if netErr, ok := err.(net.Error); ok && netErr.Timeout() {
				continue
			}
			s.setError(fmt.Sprintf("UDP discovery receive failed: %v", err))
			continue
		}
		if string(buffer[:n]) == discoveryQuery {
			if _, err := conn.WriteToUDP(response, remote); err != nil && ctx.Err() == nil {
				s.addLog("Discovery response failed: " + err.Error())
			}
		}
	}
}

const steamVRPoseBridgePort = 48103

func (s *HostService) steamVRStatusLoop(ctx context.Context, conn *net.UDPConn) {
	defer s.wg.Done()
	buffer := make([]byte, 64)
	for {
		_ = conn.SetReadDeadline(time.Now().Add(time.Second))
		n, remote, err := conn.ReadFromUDP(buffer)
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			if netErr, ok := err.(net.Error); ok && netErr.Timeout() {
				continue
			}
			s.addLog(fmt.Sprintf("SteamVR status receive failed: %v", err))
			continue
		}
		if !remote.IP.IsLoopback() {
			continue
		}
		heartbeat, ok := decodeSteamVRDriverHeartbeat(buffer[:n])
		if !ok {
			continue
		}
		s.mu.Lock()
		s.steamVRHeartbeatAt = time.Now()
		s.steamVRFlags = heartbeat.Flags
		s.steamVRFrameCounter = heartbeat.FrameCounter
		s.mu.Unlock()
	}
}

func (s *HostService) poseLoop(ctx context.Context, conn *net.UDPConn) {
	defer s.wg.Done()
	// SteamVR loads the native driver inside vrserver.exe, separate from this
	// Go Host process. Forward the validated PVSP packet over loopback so the
	// driver can consume the same HMD pose without changing the phone protocol.
	bridge, bridgeErr := net.DialUDP("udp4", nil, &net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: steamVRPoseBridgePort})
	if bridgeErr != nil {
		s.addLog("SteamVR local pose bridge unavailable: " + bridgeErr.Error())
	} else {
		defer bridge.Close()
	}
	buffer := make([]byte, 1500)
	for {
		_ = conn.SetReadDeadline(time.Now().Add(time.Second))
		n, remote, err := conn.ReadFromUDP(buffer)
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			if netErr, ok := err.(net.Error); ok && netErr.Timeout() {
				continue
			}
			s.setError(fmt.Sprintf("UDP pose receive failed: %v", err))
			continue
		}
		pose, err := protocol.DecodePosePacket(buffer[:n])
		if err != nil {
			s.addLog(fmt.Sprintf("Ignored invalid pose packet from %s: %v", remote, err))
			continue
		}
		if bridge != nil {
			// This is best-effort latest-pose IPC. Never block the phone receiver
			// or queue stale tracking samples behind the SteamVR driver.
			_, _ = bridge.Write(buffer[:n])
		}
		yaw, pitch, roll := quaternionEuler(pose.X, pose.Y, pose.Z, pose.W)
		s.mu.Lock()
		s.packets++
		s.lastPacket = time.Now()
		s.latestPose = &PoseSnapshot{
			Sequence: pose.Sequence, TimestampNs: pose.TimestampNs,
			X: pose.X, Y: pose.Y, Z: pose.Z, W: pose.W,
			Yaw: yaw, Pitch: pitch, Roll: roll,
			Source: remote.IP.String(), ReceivedAt: s.lastPacket.Format(time.RFC3339Nano),
		}
		s.mu.Unlock()
	}
}

func (s *HostService) handleControlConnection(ctx context.Context, conn net.Conn) {
	defer conn.Close()
	defer s.removeConnection(conn)
	_ = conn.SetReadDeadline(time.Now().Add(8 * time.Second))
	_ = conn.SetWriteDeadline(time.Now().Add(5 * time.Second))
	reader := bufio.NewScanner(conn)
	reader.Buffer(make([]byte, 1024), 64*1024)
	if !reader.Scan() {
		return
	}
	var hello helloMessage
	if err := json.Unmarshal(reader.Bytes(), &hello); err != nil {
		_ = writeJSONLine(conn, map[string]any{"type": "error", "message": "invalid hello JSON"})
		return
	}
	if hello.Type != "hello" || hello.Protocol != protocolVersion {
		_ = writeJSONLine(conn, map[string]any{"type": "error", "message": "protocol mismatch"})
		return
	}
	if strings.TrimSpace(hello.DeviceName) == "" {
		hello.DeviceName = "Android device"
	}
	remote := conn.RemoteAddr().String()
	mediaCapabilities := hello.MediaCapabilities
	if len(mediaCapabilities) > 32*1024 || (len(mediaCapabilities) > 0 && !json.Valid(mediaCapabilities)) {
		mediaCapabilities = nil
	}
	device := activeDevice{Snapshot: DeviceSnapshot{
		Name: hello.DeviceName, DeviceID: hello.DeviceID, Client: hello.Client,
		Address: remote, ConnectedAt: time.Now().Format(time.RFC3339),
		MediaCapabilities: append(json.RawMessage(nil), mediaCapabilities...),
	}, Conn: conn, WriteMu: &sync.Mutex{}}
	s.mu.Lock()
	s.devices[remote] = device
	s.mu.Unlock()
	s.addLog(fmt.Sprintf("Device paired: %s (%s)", hello.DeviceName, remote))
	defer func() {
		s.mu.Lock()
		delete(s.devices, remote)
		s.mu.Unlock()
		s.addLog("Device disconnected: " + hello.DeviceName)
	}()

	welcome := welcomeMessage{
		Type: "welcome", Protocol: protocolVersion, SessionID: newSessionID(),
		ServerName: s.serverName, PosePort: s.config.PosePort,
		Capabilities: []string{"head-pose-3dof-udp-v1", "control-heartbeat-v1", "display-settings-control-v1", "android-optical-presets-v1", "android-media-capabilities-v1"},
	}
	if err := writeJSONLine(conn, welcome); err != nil {
		s.addLog("Pair response failed: " + err.Error())
		return
	}
	s.mu.Lock()
	if current, ok := s.devices[remote]; ok && current.Conn == conn {
		current.Ready = true
		s.devices[remote] = current
	}
	s.mu.Unlock()

	// Ask the Android device for its persistent optical preset catalogue.
	// The phone remains the source of truth; the host only caches and edits it.
	if err := writeControlJSON(device, map[string]any{
		"type": "get_optical_presets", "requestId": newSessionID(),
	}); err != nil {
		s.addLog("Optical preset request failed: " + err.Error())
		return
	}

	for {
		// Apply the idle deadline before Scan. Setting it after Scan allowed
		// the initial handshake deadline to remain active for the first idle
		// heartbeat interval, which could close a delayed client unnecessarily.
		_ = conn.SetReadDeadline(time.Now().Add(controlIdleTimeout))
		if !reader.Scan() {
			break
		}
		var message controlMessage
		if err := json.Unmarshal(reader.Bytes(), &message); err != nil {
			_ = conn.SetWriteDeadline(time.Now().Add(controlWriteTimeout))
			_ = writeJSONLine(conn, map[string]any{"type": "error", "message": "invalid control JSON"})
			continue
		}
		switch message.Type {
		case "ping":
			if message.BatteryLevel != nil {
				level := *message.BatteryLevel
				if level < 0 {
					level = 0
				}
				if level > 100 {
					level = 100
				}
				s.mu.Lock()
				if current, ok := s.devices[remote]; ok && current.Conn == conn {
					current.Snapshot.BatteryLevel = &level
					current.Snapshot.Charging = message.Charging
					s.devices[remote] = current
				}
				s.mu.Unlock()
			}
			if err := writeControlJSON(device, map[string]any{"type": "pong", "timeMs": time.Now().UnixMilli()}); err != nil {
				return
			}
		case "optical_presets":
			presets := normalizeOpticalPresets(message.Presets)
			s.mu.Lock()
			if current, ok := s.devices[remote]; ok && current.Conn == conn {
				current.Snapshot.OpticalPresets = presets
				s.devices[remote] = current
			}
			s.mu.Unlock()
			s.addLog(fmt.Sprintf("Received %d optical presets from %s", len(presets), hello.DeviceName))
		case "optical_presets_ack":
			if message.Status == "applied" {
				s.addLog(fmt.Sprintf("Android saved %d optical presets", len(message.Presets)))
				s.mu.Lock()
				if current, ok := s.devices[remote]; ok && current.Conn == conn {
					current.Snapshot.OpticalPresets = normalizeOpticalPresets(message.Presets)
					s.devices[remote] = current
				}
				s.mu.Unlock()
			} else if message.Status == "rejected" {
				s.addLog("Android rejected optical presets: " + message.Message)
			}
		case "display_settings":
			if message.Settings == nil {
				_ = writeControlJSON(device, map[string]any{"type": "error", "message": "missing display settings"})
				continue
			}
			settings := message.Settings.Normalized()
			s.mu.Lock()
			if current, ok := s.devices[remote]; ok && current.Conn == conn {
				current.Snapshot.DisplaySettings = &settings
				s.devices[remote] = current
			}
			s.mu.Unlock()
		case "device_action_ack":
			if message.Status == "applied" {
				s.addLog("Phone action applied: " + message.RequestID)
			} else if message.Status == "rejected" {
				s.addLog("Phone action rejected: " + message.Message)
			}
		case "display_settings_ack":
			if message.Settings != nil && message.Status == "applied" {
				settings := message.Settings.Normalized()
				s.mu.Lock()
				if current, ok := s.devices[remote]; ok && current.Conn == conn {
					current.Snapshot.DisplaySettings = &settings
					s.devices[remote] = current
				}
				s.mu.Unlock()
				s.addLog("Phone display settings applied: " + hello.DeviceName)
			} else if message.Status == "rejected" {
				s.addLog("Phone rejected display settings: " + message.Message)
			}
		default:
			_ = writeControlJSON(device, map[string]any{"type": "error", "message": "unsupported control message"})
		}
		select {
		case <-ctx.Done():
			return
		default:
		}
	}
	if err := reader.Err(); err != nil && ctx.Err() == nil {
		s.addLog(fmt.Sprintf("Control channel closed for %s: %v", hello.DeviceName, err))
	}
}

func (s *HostService) removeConnection(conn net.Conn) {
	s.mu.Lock()
	delete(s.connections, conn)
	for key, device := range s.devices {
		if device.Conn == conn {
			delete(s.devices, key)
		}
	}
	s.mu.Unlock()
}

func (s *HostService) addLog(message string) {
	entry := time.Now().Format("15:04:05") + "  " + message
	s.mu.Lock()
	s.logs = append(s.logs, entry)
	if len(s.logs) > maxLogEntries {
		s.logs = append([]string(nil), s.logs[len(s.logs)-maxLogEntries:]...)
	}
	s.mu.Unlock()
	if s.consoleLogs {
		fmt.Println(entry)
	}
}

func (s *HostService) setError(message string) {
	s.mu.Lock()
	s.lastError = message
	s.mu.Unlock()
	s.addLog("ERROR: " + message)
}

func localIPv4Addresses() []string {
	interfaces, err := net.Interfaces()
	if err != nil {
		return nil
	}
	var result []string
	for _, item := range interfaces {
		if item.Flags&net.FlagUp == 0 || item.Flags&net.FlagLoopback != 0 {
			continue
		}
		addresses, _ := item.Addrs()
		for _, address := range addresses {
			ipNet, ok := address.(*net.IPNet)
			if ok && ipNet.IP.To4() != nil {
				result = append(result, ipNet.IP.String())
			}
		}
	}
	sort.Strings(result)
	return result
}

func localHostName() (string, error) { return netHostName() }

func quaternionEuler(x, y, z, w float32) (yaw, pitch, roll float64) {
	sinr := 2 * (float64(w*x) + float64(y*z))
	cosr := 1 - 2*(float64(x*x)+float64(y*y))
	roll = radiansToDegrees(math.Atan2(sinr, cosr))
	sinp := 2 * (float64(w*y) - float64(z*x))
	if math.Abs(sinp) >= 1 {
		if sinp < 0 {
			pitch = -90
		} else {
			pitch = 90
		}
	} else {
		pitch = radiansToDegrees(math.Asin(sinp))
	}
	siny := 2 * (float64(w*z) + float64(x*y))
	cosy := 1 - 2*(float64(y*y)+float64(z*z))
	yaw = radiansToDegrees(math.Atan2(siny, cosy))
	return
}

func radiansToDegrees(value float64) float64 { return value * 180 / math.Pi }

func writeJSONLine(conn net.Conn, value any) error { return json.NewEncoder(conn).Encode(value) }

func newSessionID() string {
	bytes := make([]byte, 12)
	if _, err := rand.Read(bytes); err != nil {
		return fmt.Sprintf("%d", time.Now().UnixNano())
	}
	return hex.EncodeToString(bytes)
}
