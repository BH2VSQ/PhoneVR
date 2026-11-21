package main

import (
	"bufio"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/vsq-studio/phonevr-host/internal/encoder"
	"github.com/vsq-studio/phonevr-host/internal/protocol"
	"github.com/vsq-studio/phonevr-host/internal/videoprotocol"
)

const (
	protocolVersion         = 1
	controlIdleTimeout      = 15 * time.Second
	controlWriteTimeout     = 5 * time.Second
	discoveryPortDefault    = 48100
	controlPortDefault      = 48101
	posePortDefault         = 48102
	steamVRStatusPort       = 48105
	steamVRVideoCapturePort = 48106
	androidVideoPort        = 48104
	videoWidth              = 1600
	videoHeight             = 720
	videoFrameRate          = 60
	videoInputQueueDepth    = 2 // one frame may be written to FFmpeg while two newer frames wait
	videoBridgeReadTimeout  = 10 * time.Second
	discoveryQuery          = "PHONEVR_DISCOVER_V1"
	discoveryPrefix         = "PHONEVR_HOST_V1 "
	maxLogEntries           = 250
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
	VideoCaptureBridgeActive bool             `json:"videoCaptureBridgeActive"`
	VideoRawFrames           uint64           `json:"videoRawFrames"`
	VideoEncodedFrames       uint64           `json:"videoEncodedFrames"`
	VideoUDPFragments        uint64           `json:"videoUdpFragments"`
	VideoDroppedFrames       uint64           `json:"videoDroppedFrames"`
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
	Type             string   `json:"type"`
	Protocol         int      `json:"protocol"`
	SessionID        string   `json:"sessionId"`
	ServerName       string   `json:"serverName"`
	PosePort         int      `json:"posePort"`
	Capabilities     []string `json:"capabilities"`
	VideoEnabled     bool     `json:"videoEnabled"`
	VideoPort        int      `json:"videoPort,omitempty"`
	VideoCodec       string   `json:"videoCodec,omitempty"`
	VideoMimeType    string   `json:"videoMimeType,omitempty"`
	VideoWidth       int      `json:"videoWidth,omitempty"`
	VideoHeight      int      `json:"videoHeight,omitempty"`
	VideoFPS         int      `json:"videoFps,omitempty"`
	VideoDecoderName string   `json:"videoDecoderName,omitempty"`
	VideoReason      string   `json:"videoReason,omitempty"`
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
	Snapshot     DeviceSnapshot
	Conn         net.Conn
	WriteMu      *sync.Mutex
	Ready        bool
	VideoEnabled bool
}

type rawVideoFrame struct {
	data        []byte
	timestampNs uint64
}

type HostService struct {
	lifecycleMu sync.Mutex
	mu          sync.RWMutex
	// poseStateMu isolates the 180 Hz pose path from UI snapshots, device
	// metadata and log updates guarded by mu.
	poseStateMu           sync.RWMutex
	config                HostConfig
	serverName            string
	localIPv4             []string
	startedAt             time.Time
	listener              net.Listener
	discovery             *net.UDPConn
	pose                  *net.UDPConn
	steamVRStatus         *net.UDPConn
	videoCaptureListener  net.Listener
	videoUDP              *net.UDPConn
	videoFFmpegPath       string
	videoEncoderProbeDone chan struct{}
	videoEncoderReady     bool
	videoEncoderSelected  encoder.Capability
	videoEncoderError     string
	videoBridgeActive     atomic.Bool
	videoFrameQueue       chan *rawVideoFrame
	videoFramePool        sync.Pool
	videoInputQueueDrops  atomic.Uint64
	videoRawFrames        atomic.Uint64
	videoEncodedFrames    atomic.Uint64
	videoUDPFragments     atomic.Uint64
	videoDroppedFrames    atomic.Uint64
	steamVRHeartbeatAt    time.Time
	steamVRFlags          uint8
	steamVRFrameCounter   uint64
	cancel                context.CancelFunc
	wg                    sync.WaitGroup
	devices               map[string]activeDevice
	connections           map[net.Conn]struct{}
	latestPose            *PoseSnapshot // guarded by poseStateMu
	posePackets           uint64        // guarded by poseStateMu
	poseLastPacket        time.Time     // guarded by poseStateMu
	lastError             string
	logs                  []string
	consoleLogs           bool
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
	s.mu.RLock()
	alreadyRunning := s.listener != nil
	s.mu.RUnlock()
	if alreadyRunning {
		return errors.New("PhoneVR Host is already running")
	}
	// Network-interface enumeration can block on virtual adapters and VPN
	// providers. Resolve addresses once at service start, never in Snapshot(),
	// and do it before starting real-time workers so interface queries never run
	// in the hot pose path or while holding the service state lock.
	networkAddresses := localIPv4NetworkAddresses()
	localIPs := make([]string, 0, len(networkAddresses))
	localIPSet := make(map[string]struct{}, len(networkAddresses))
	for _, address := range networkAddresses {
		if _, exists := localIPSet[address.IP]; exists {
			continue
		}
		localIPSet[address.IP] = struct{}{}
		localIPs = append(localIPs, address.IP)
	}
	sort.Strings(localIPs)
	serverName, hostErr := localHostName()
	if hostErr != nil || strings.TrimSpace(serverName) == "" {
		serverName = "PhoneVR Host"
	}
	s.mu.Lock()
	if s.listener != nil {
		s.mu.Unlock()
		return errors.New("PhoneVR Host is already running")
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
	// Keep pose buffering bounded. The pose loop preserves small receive bursts
	// for accurate sensor-rate velocity estimation and collapses only unusually
	// large stale backlogs before they reach the local SteamVR driver socket.
	_ = udpPose.SetReadBuffer(16 * 1024)

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

	videoCaptureListener, err := net.Listen("tcp4", net.JoinHostPort("127.0.0.1", strconv.Itoa(steamVRVideoCapturePort)))
	if err != nil {
		_ = tcpListener.Close()
		_ = udpDiscovery.Close()
		_ = udpPose.Close()
		_ = udpSteamVRStatus.Close()
		s.mu.Unlock()
		return fmt.Errorf("start loopback video capture bridge on 127.0.0.1:%d: %w", steamVRVideoCapturePort, err)
	}
	videoUDP, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4zero, Port: 0})
	if err != nil {
		_ = tcpListener.Close()
		_ = udpDiscovery.Close()
		_ = udpPose.Close()
		_ = udpSteamVRStatus.Close()
		_ = videoCaptureListener.Close()
		s.mu.Unlock()
		return fmt.Errorf("open video UDP sender: %w", err)
	}
	_ = videoUDP.SetWriteBuffer(4 * 1024 * 1024)

	ctx, cancel := context.WithCancel(context.Background())
	config.DiscoveryPort = udpDiscovery.LocalAddr().(*net.UDPAddr).Port
	config.ControlPort = tcpListener.Addr().(*net.TCPAddr).Port
	config.PosePort = udpPose.LocalAddr().(*net.UDPAddr).Port
	s.config = config
	s.serverName = serverName
	s.localIPv4 = append([]string(nil), localIPs...)
	s.startedAt = time.Now()
	s.listener = tcpListener
	s.discovery = udpDiscovery
	s.pose = udpPose
	s.steamVRStatus = udpSteamVRStatus
	s.videoCaptureListener = videoCaptureListener
	s.videoUDP = videoUDP
	s.videoFrameQueue = make(chan *rawVideoFrame, videoInputQueueDepth)
	s.videoFramePool = sync.Pool{New: func() any {
		return &rawVideoFrame{data: make([]byte, videoWidth*videoHeight*4)}
	}}
	s.videoBridgeActive.Store(false)
	s.videoInputQueueDrops.Store(0)
	s.steamVRHeartbeatAt = time.Time{}
	s.steamVRFlags = 0
	s.steamVRFrameCounter = 0
	s.cancel = cancel
	s.devices = make(map[string]activeDevice)
	s.connections = make(map[net.Conn]struct{})
	s.lastError = ""
	s.mu.Unlock()
	s.poseStateMu.Lock()
	s.latestPose = nil
	s.posePackets = 0
	s.poseLastPacket = time.Time{}
	s.poseStateMu.Unlock()

	s.addLog(fmt.Sprintf("Host started on %s | TCP %d | UDP discovery %d | UDP pose %d", config.ListenAddress, config.ControlPort, config.DiscoveryPort, config.PosePort))
	for _, address := range networkAddresses {
		s.addLog(fmt.Sprintf("Host interface [%s] %s: %s", address.Kind, address.Name, address.IP))
	}

	s.wg.Add(6)
	go s.acceptLoop(ctx, tcpListener)
	go s.discoveryLoop(ctx, udpDiscovery, serverName, config.ControlPort)
	go s.poseLoop(ctx, udpPose)
	go s.steamVRStatusLoop(ctx, udpSteamVRStatus)
	go s.videoCaptureLoop(ctx, videoCaptureListener)
	go s.videoEncoderLoop(ctx)
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
	s.mu.Lock()
	s.videoFFmpegPath = ffmpegPath
	if s.videoEncoderProbeDone == nil {
		s.videoEncoderProbeDone = make(chan struct{})
	}
	done := s.videoEncoderProbeDone
	s.mu.Unlock()
	defer close(done)

	// This probe is deliberately synchronous. main() invokes it before Start(),
	// so the TCP pairing listener cannot accept a phone before the H.264
	// hardware-encoder result has been committed to service state.
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	candidates, err := encoder.ProbeFFmpeg(ctx, ffmpegPath)
	if err != nil {
		s.mu.Lock()
		s.videoEncoderError = err.Error()
		s.videoEncoderReady = false
		s.mu.Unlock()
		s.addLog("Video encoder discovery unavailable: " + err.Error())
		return
	}
	for _, candidate := range candidates {
		s.addLog(fmt.Sprintf("Video encoder candidate (adapter/session unverified): %s | %s | %s", candidate.Name, candidate.Codec, candidate.Backend))
	}
	selected, probeErr := encoder.ProbeHardwareH264Encoder(ctx, ffmpegPath, videoFrameRate)
	s.mu.Lock()
	s.videoEncoderReady = probeErr == nil
	if probeErr != nil {
		s.videoEncoderError = probeErr.Error()
		s.videoEncoderSelected = encoder.Capability{}
	} else {
		s.videoEncoderError = ""
		s.videoEncoderSelected = selected
	}
	s.mu.Unlock()
	if probeErr != nil {
		s.addLog("Live VR video disabled: " + probeErr.Error())
		return
	}
	s.addLog("Live VR video hardware encoder validated: " + selected.Name + "/" + string(selected.Backend))
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
	videoCaptureListener := s.videoCaptureListener
	videoUDP := s.videoUDP
	connections := make([]net.Conn, 0, len(s.connections))
	for conn := range s.connections {
		connections = append(connections, conn)
	}
	s.listener = nil
	s.discovery = nil
	s.pose = nil
	s.steamVRStatus = nil
	s.videoCaptureListener = nil
	s.videoUDP = nil
	s.steamVRHeartbeatAt = time.Time{}
	s.steamVRFlags = 0
	s.steamVRFrameCounter = 0
	s.cancel = nil
	s.devices = make(map[string]activeDevice)
	s.connections = make(map[net.Conn]struct{})
	s.mu.Unlock()
	s.poseStateMu.Lock()
	s.latestPose = nil
	s.poseLastPacket = time.Time{}
	s.poseStateMu.Unlock()

	if cancel != nil {
		cancel()
	}
	_ = listener.Close()
	_ = discovery.Close()
	_ = pose.Close()
	if steamVRStatus != nil {
		_ = steamVRStatus.Close()
	}
	if videoCaptureListener != nil {
		_ = videoCaptureListener.Close()
	}
	if videoUDP != nil {
		_ = videoUDP.Close()
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
		LocalIPv4: append([]string(nil), s.localIPv4...), DiscoveryPort: s.config.DiscoveryPort,
		ControlPort: s.config.ControlPort, PosePort: s.config.PosePort,
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
	snapshot.VideoCaptureBridgeActive = s.videoBridgeActive.Load()
	snapshot.VideoRawFrames = s.videoRawFrames.Load()
	snapshot.VideoEncodedFrames = s.videoEncodedFrames.Load()
	snapshot.VideoUDPFragments = s.videoUDPFragments.Load()
	snapshot.VideoDroppedFrames = s.videoDroppedFrames.Load()
	for _, device := range s.devices {
		snapshot.ConnectedDevices = append(snapshot.ConnectedDevices, device.Snapshot)
	}
	if running {
		snapshot.Uptime = time.Since(s.startedAt).Round(time.Second).String()
	}
	snapshot.Logs = append([]string(nil), s.logs...)
	s.mu.RUnlock()

	// Do not nest poseStateMu under mu. The real-time pose writer never needs
	// the general service mutex, and status/log rendering cannot stall it.
	s.poseStateMu.RLock()
	if s.latestPose != nil {
		copyPose := *s.latestPose
		snapshot.LatestPose = &copyPose
	}
	snapshot.PacketsReceived = s.posePackets
	if !s.poseLastPacket.IsZero() {
		snapshot.LastPacketAt = s.poseLastPacket.Format(time.RFC3339)
	}
	s.poseStateMu.RUnlock()

	sort.Slice(snapshot.ConnectedDevices, func(i, j int) bool {
		return snapshot.ConnectedDevices[i].ConnectedAt < snapshot.ConnectedDevices[j].ConnectedAt
	})
	return snapshot
}

// PoseFrame returns only the high-frequency head-pose state so the native desktop
// OpenGL viewer can refresh the HMD model without polling device and log
// metadata on every animation update.
func (s *HostService) PoseFrame() PoseFrameSnapshot {
	s.poseStateMu.RLock()
	defer s.poseStateMu.RUnlock()
	var latest *PoseSnapshot
	if s.latestPose != nil {
		copyPose := *s.latestPose
		latest = &copyPose
	}
	return PoseFrameSnapshot{LatestPose: latest, PacketsReceived: s.posePackets}
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

type receivedPoseDatagram struct {
	pose       protocol.Pose
	payload    []byte
	remote     *net.UDPAddr
	receivedAt time.Time
}

// readLatestPoseBatch blocks for one datagram, then drains packets already
// queued by the OS. Small batches preserve every sample so the driver can
// estimate angular velocity from the phone's native sensor cadence. Only a
// very large backlog is collapsed to its newest sample.
func readLatestPoseBatch(conn *net.UDPConn, buffer []byte, timeout time.Duration) ([]receivedPoseDatagram, uint64, error) {
	if err := conn.SetReadDeadline(time.Now().Add(timeout)); err != nil {
		return nil, 0, err
	}

	readOne := func() (receivedPoseDatagram, bool, error) {
		n, remote, err := conn.ReadFromUDP(buffer)
		if err != nil {
			return receivedPoseDatagram{}, false, err
		}
		pose, decodeErr := protocol.DecodePosePacket(buffer[:n])
		if decodeErr != nil {
			return receivedPoseDatagram{}, false, nil
		}
		remoteCopy := &net.UDPAddr{IP: append(net.IP(nil), remote.IP...), Port: remote.Port, Zone: remote.Zone}
		return receivedPoseDatagram{
			pose: pose, payload: append([]byte(nil), buffer[:n]...),
			remote: remoteCopy, receivedAt: time.Now(),
		}, true, nil
	}

	batch := make([]receivedPoseDatagram, 0, 4)
	var invalidPackets uint64
	first, valid, err := readOne()
	if err != nil {
		return nil, 0, err
	}
	if valid {
		batch = append(batch, first)
	} else {
		invalidPackets++
	}

	// A very short drain window reads datagrams that were already queued while
	// allowing packets arriving a fraction later to join this latest-state
	// batch. It avoids replaying a long backlog without adding a frame of delay.
	if err := conn.SetReadDeadline(time.Now().Add(250 * time.Microsecond)); err != nil {
		return batch, invalidPackets, err
	}
	for {
		datagram, valid, readErr := readOne()
		if readErr != nil {
			if netErr, ok := readErr.(net.Error); ok && netErr.Timeout() {
				break
			}
			return batch, invalidPackets, readErr
		}
		if valid {
			batch = append(batch, datagram)
		} else {
			invalidPackets++
		}
	}
	return batch, invalidPackets, nil
}

const maxPoseBatchForward = 32

// poseBatchToForward preserves short bursts (normally one or two datagrams)
// and collapses only a long stale backlog. The driver consumes all forwarded
// samples for angular-velocity estimation and publishes the newest orientation.
func poseBatchToForward(batch []receivedPoseDatagram) []receivedPoseDatagram {
	if len(batch) > maxPoseBatchForward {
		return batch[len(batch)-1:]
	}
	return batch
}

func (s *HostService) poseLoop(ctx context.Context, conn *net.UDPConn) {
	defer s.wg.Done()
	// Keep the socket buffer bounded. Short receive bursts are forwarded in
	// order so the driver can retain the sensor cadence; only very large stale
	// backlogs are collapsed to the newest valid pose.
	_ = conn.SetReadBuffer(16 * 1024)
	bridge, bridgeErr := net.DialUDP("udp4", nil, &net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: steamVRPoseBridgePort})
	if bridgeErr != nil {
		s.addLog("SteamVR local pose bridge unavailable: " + bridgeErr.Error())
	} else {
		_ = bridge.SetWriteBuffer(16 * 1024)
		defer bridge.Close()
	}

	buffer := make([]byte, 1500)
	statsStarted := time.Now()
	var statsReceived uint64
	var statsForwarded uint64
	var statsCoalesced uint64
	var statsSequenceGaps uint64
	var statsInvalid uint64
	var statsBridgeErrors uint64
	var maxHostReadGap time.Duration
	var maxSensorInterval time.Duration
	var maxBatchSize int
	var maxPoseStateLockWait time.Duration
	var previousArrival time.Time
	var previousSensorTimestamp int64
	var previousSequence uint32
	havePreviousSequence := false

	for {
		batch, invalidPackets, err := readLatestPoseBatch(conn, buffer, time.Second)
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
		statsInvalid += invalidPackets
		if len(batch) == 0 {
			continue
		}

		if len(batch) > maxBatchSize {
			maxBatchSize = len(batch)
		}
		for _, datagram := range batch {
			statsReceived++
			if !previousArrival.IsZero() {
				if gap := datagram.receivedAt.Sub(previousArrival); gap > maxHostReadGap {
					maxHostReadGap = gap
				}
			}
			previousArrival = datagram.receivedAt
			if previousSensorTimestamp > 0 && datagram.pose.TimestampNs > previousSensorTimestamp {
				if gap := time.Duration(datagram.pose.TimestampNs - previousSensorTimestamp); gap > maxSensorInterval {
					maxSensorInterval = gap
				}
			}
			if datagram.pose.TimestampNs > 0 {
				previousSensorTimestamp = datagram.pose.TimestampNs
			}
			if havePreviousSequence {
				sequenceDelta := datagram.pose.Sequence - previousSequence
				if sequenceDelta > 1 && sequenceDelta < 0x80000000 {
					statsSequenceGaps += uint64(sequenceDelta - 1)
				}
			}
			previousSequence = datagram.pose.Sequence
			havePreviousSequence = true
		}

		latest := batch[len(batch)-1]
		forwardBatch := poseBatchToForward(batch)
		statsCoalesced += uint64(len(batch) - len(forwardBatch))
		if bridge != nil {
			// Preserve every sample in a short burst. The native receiver drains
			// queued samples itself, estimates angular velocity across all of them,
			// and publishes only the latest orientation to SteamVR.
			for _, datagram := range forwardBatch {
				if _, writeErr := bridge.Write(datagram.payload); writeErr != nil {
					statsBridgeErrors++
				} else {
					statsForwarded++
				}
			}
		}

		yaw, pitch, roll := quaternionEuler(latest.pose.X, latest.pose.Y, latest.pose.Z, latest.pose.W)
		lockWaitStarted := time.Now()
		s.poseStateMu.Lock()
		if wait := time.Since(lockWaitStarted); wait > maxPoseStateLockWait {
			maxPoseStateLockWait = wait
		}
		s.posePackets += uint64(len(batch))
		s.poseLastPacket = latest.receivedAt
		s.latestPose = &PoseSnapshot{
			Sequence: latest.pose.Sequence, TimestampNs: latest.pose.TimestampNs,
			X: latest.pose.X, Y: latest.pose.Y, Z: latest.pose.Z, W: latest.pose.W,
			Yaw: yaw, Pitch: pitch, Roll: roll,
			Source: latest.remote.IP.String(), ReceivedAt: latest.receivedAt.Format(time.RFC3339Nano),
		}
		s.poseStateMu.Unlock()

		if time.Since(statsStarted) >= 5*time.Second {
			seconds := time.Since(statsStarted).Seconds()
			rxRate := float64(statsReceived) / seconds
			forwardRate := float64(statsForwarded) / seconds
			s.addLog(fmt.Sprintf("Pose pipeline: Host RX %.1f Hz, forwarded %.1f Hz, coalesced %d, sequence gaps %d, invalid %d, max Host read gap %.1f ms, max Android sensor interval %.1f ms, max batch %d, max pose-state lock wait %.2f ms, loopback write errors %d",
				rxRate, forwardRate, statsCoalesced, statsSequenceGaps, statsInvalid,
				float64(maxHostReadGap)/float64(time.Millisecond),
				float64(maxSensorInterval)/float64(time.Millisecond), maxBatchSize,
				float64(maxPoseStateLockWait)/float64(time.Millisecond), statsBridgeErrors))
			statsStarted = time.Now()
			statsReceived = 0
			statsForwarded = 0
			statsCoalesced = 0
			statsSequenceGaps = 0
			statsInvalid = 0
			statsBridgeErrors = 0
			maxHostReadGap = 0
			maxSensorInterval = 0
			maxBatchSize = 0
			maxPoseStateLockWait = 0
		}
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
	// Wait briefly for the actual hardware encoder preflight. Do not advertise
	// a video stream that would later fall back to CPU encoding on an already
	// CPU-bound SteamVR PC.
	s.mu.RLock()
	probeDone := s.videoEncoderProbeDone
	s.mu.RUnlock()
	if probeDone != nil {
		select {
		case <-probeDone:
		case <-time.After(8 * time.Second):
		}
	}
	s.mu.RLock()
	hardwareEncoderReady := s.videoEncoderReady
	encoderError := s.videoEncoderError
	s.mu.RUnlock()
	selectedAndroidDecoder := selectH264DecoderProfile(mediaCapabilities, videoWidth, videoHeight, videoFrameRate)
	if selectedAndroidDecoder == "" {
		s.addLog(fmt.Sprintf("Android H.264 decoder negotiation details for %s: %s",
			hello.DeviceName, describeH264DecoderCapabilities(mediaCapabilities)))
	}
	videoEnabled := hardwareEncoderReady && selectedAndroidDecoder != ""
	if videoEnabled && !h264DecoderClassificationReliable(mediaCapabilities, selectedAndroidDecoder) {
		s.addLog(fmt.Sprintf("Android decoder %s is on a pre-Android-10 platform; accepting the vendor-codec heuristic after profile capability checks", selectedAndroidDecoder))
	}
	device := activeDevice{Snapshot: DeviceSnapshot{
		Name: hello.DeviceName, DeviceID: hello.DeviceID, Client: hello.Client,
		Address: remote, ConnectedAt: time.Now().Format(time.RFC3339),
		MediaCapabilities: append(json.RawMessage(nil), mediaCapabilities...),
	}, Conn: conn, WriteMu: &sync.Mutex{}, VideoEnabled: videoEnabled}
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
		VideoEnabled: videoEnabled,
	}
	if videoEnabled {
		welcome.VideoPort = androidVideoPort
		welcome.VideoCodec = "h264"
		welcome.VideoMimeType = "video/avc"
		welcome.VideoWidth = videoWidth
		welcome.VideoHeight = videoHeight
		welcome.VideoFPS = videoFrameRate
		welcome.VideoDecoderName = selectedAndroidDecoder
		welcome.Capabilities = append(welcome.Capabilities, "sbs-h264-udp-v1")
	} else if !hardwareEncoderReady {
		if encoderError == "" {
			encoderError = "hardware encoder preflight did not complete before the pairing deadline"
		}
		welcome.VideoReason = encoderError
		s.addLog("Video streaming disabled for " + hello.DeviceName + ": " + encoderError)
	} else {
		welcome.VideoReason = fmt.Sprintf("Android needs a hardware H.264 decoder supporting %dx%d@%d", videoWidth, videoHeight, videoFrameRate)
		s.addLog(fmt.Sprintf("Video streaming disabled for %s: Android did not advertise hardware H.264 %dx%d@%d support", hello.DeviceName, videoWidth, videoHeight, videoFrameRate))
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

	// Ask Android for its persistent optical preset catalogue. The response is
	// independent from pose transport and must not be treated as proof that the
	// TCP session itself is unhealthy. Retry a few times if the initial request
	// was missed while the Android activity/UI was starting.
	presetRequestCount := 1
	presetResponseReceived := false
	nextPresetRetry := time.Now().Add(2 * time.Second)
	if err := writeControlJSON(device, map[string]any{
		"type": "get_optical_presets", "requestId": newSessionID(),
	}); err != nil {
		s.addLog("Optical preset request failed: " + err.Error())
	} else {
		s.addLog("Requested Android optical preset catalogue")
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
			presetResponseReceived = true
			s.mu.Lock()
			if current, ok := s.devices[remote]; ok && current.Conn == conn {
				current.Snapshot.OpticalPresets = presets
				s.devices[remote] = current
			}
			s.mu.Unlock()
			if len(presets) == 0 {
				s.addLog(fmt.Sprintf("Android %s replied to optical preset request with an empty catalogue", hello.DeviceName))
			} else {
				s.addLog(fmt.Sprintf("Received %d optical presets from %s (request %s)", len(presets), hello.DeviceName, message.RequestID))
			}
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

		if !presetResponseReceived && time.Now().After(nextPresetRetry) {
			if presetRequestCount < 4 {
				presetRequestCount++
				requestID := newSessionID()
				if err := writeControlJSON(device, map[string]any{
					"type": "get_optical_presets", "requestId": requestID,
				}); err != nil {
					s.addLog("Optical preset retry failed: " + err.Error())
					return
				}
				s.addLog(fmt.Sprintf("Retrying Android optical preset request (%d/3; request %s)", presetRequestCount-1, requestID))
				nextPresetRetry = time.Now().Add(2 * time.Second)
			} else {
				s.addLog("Android optical preset catalogue was not received after 3 retries; control and pose links remain active")
				presetResponseReceived = true // Stop retrying; keep the session alive.
			}
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

// supportsH264Profile checks the exact decoder profile advertised by Android.
// We do not start video when the phone cannot decode the negotiated profile.
func selectH264DecoderProfile(raw json.RawMessage, width, height, fps int) string {
	var entries []struct {
		Name                           string  `json:"name"`
		MimeType                       string  `json:"mimeType"`
		MaxWidth                       int     `json:"maxWidth"`
		MaxHeight                      int     `json:"maxHeight"`
		MaxFrameRate                   float64 `json:"maxFrameRate"`
		HardwareAccelerated            bool    `json:"hardwareAccelerated"`
		HardwareClassificationReliable bool    `json:"hardwareClassificationReliable"`
		SupportsTargetProfile          bool    `json:"supportsSbs1600x720At60"`
	}
	if len(raw) == 0 || json.Unmarshal(raw, &entries) != nil {
		return ""
	}
	for _, item := range entries {
		if strings.TrimSpace(item.Name) != "" && strings.EqualFold(item.MimeType, "video/avc") &&
			item.HardwareAccelerated && item.SupportsTargetProfile &&
			item.MaxWidth >= width && item.MaxHeight >= height && item.MaxFrameRate >= float64(fps) {
			return item.Name
		}
	}
	return ""
}

func describeH264DecoderCapabilities(raw json.RawMessage) string {
	var entries []struct {
		Name                           string  `json:"name"`
		MimeType                       string  `json:"mimeType"`
		MaxWidth                       int     `json:"maxWidth"`
		MaxHeight                      int     `json:"maxHeight"`
		MaxFrameRate                   float64 `json:"maxFrameRate"`
		HardwareAccelerated            bool    `json:"hardwareAccelerated"`
		HardwareClassificationReliable bool    `json:"hardwareClassificationReliable"`
		SupportsTargetProfile          bool    `json:"supportsSbs1600x720At60"`
	}
	if len(raw) == 0 {
		return "MediaCodec capability array is empty"
	}
	if err := json.Unmarshal(raw, &entries); err != nil {
		return "MediaCodec capability JSON is invalid: " + err.Error()
	}
	var reports []string
	for _, item := range entries {
		if !strings.EqualFold(item.MimeType, "video/avc") {
			continue
		}
		reports = append(reports, fmt.Sprintf("%s max=%dx%d@%.1f hw=%t hw-classification-reliable=%t supports-1600x720@60=%t",
			item.Name, item.MaxWidth, item.MaxHeight, item.MaxFrameRate,
			item.HardwareAccelerated, item.HardwareClassificationReliable, item.SupportsTargetProfile))
	}
	if len(reports) == 0 {
		return "no video/avc decoder advertised"
	}
	return strings.Join(reports, "; ")
}

func supportsH264Profile(raw json.RawMessage, width, height, fps int) bool {
	return selectH264DecoderProfile(raw, width, height, fps) != ""
}

func h264DecoderClassificationReliable(raw json.RawMessage, decoderName string) bool {
	var entries []struct {
		Name                           string `json:"name"`
		HardwareClassificationReliable bool   `json:"hardwareClassificationReliable"`
	}
	if len(raw) == 0 || json.Unmarshal(raw, &entries) != nil {
		return false
	}
	for _, entry := range entries {
		if entry.Name == decoderName {
			return entry.HardwareClassificationReliable
		}
	}
	return false
}

func maxDurationSeconds(value time.Duration) float64 {
	seconds := value.Seconds()
	if seconds < 0.001 {
		return 0.001
	}
	return seconds
}

func (s *HostService) currentVideoTarget() (*net.UDPAddr, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, device := range s.devices {
		if !device.Ready || !device.VideoEnabled || device.Conn == nil {
			continue
		}
		tcp, ok := device.Conn.RemoteAddr().(*net.TCPAddr)
		if !ok || tcp.IP == nil {
			continue
		}
		return &net.UDPAddr{IP: append(net.IP(nil), tcp.IP...), Port: androidVideoPort}, true
	}
	return nil, false
}

func (s *HostService) sendEncodedVideoFrame(frame []byte, frameID uint32, timestampNs uint64, keyFrame bool) error {
	target, enabled := s.currentVideoTarget()
	if !enabled {
		return nil
	}
	s.mu.RLock()
	udp := s.videoUDP
	s.mu.RUnlock()
	if udp == nil {
		return errors.New("video UDP sender is unavailable")
	}
	fragments, err := videoprotocol.FragmentFrame(frame, frameID, timestampNs, keyFrame)
	if err != nil {
		return err
	}
	for _, packet := range fragments {
		if _, err := udp.WriteToUDP(packet, target); err != nil {
			s.videoDroppedFrames.Add(1)
			return err
		}
		s.videoUDPFragments.Add(1)
	}
	return nil
}

func (s *HostService) videoCaptureLoop(ctx context.Context, listener net.Listener) {
	defer s.wg.Done()
	var lastNoVideoTargetLog time.Time
	for {
		conn, err := listener.Accept()
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			s.addLog("Video capture bridge accept failed: " + err.Error())
			continue
		}
		if _, enabled := s.currentVideoTarget(); !enabled {
			_ = conn.Close()
			if time.Since(lastNoVideoTargetLog) >= 5*time.Second {
				s.addLog("SteamVR video bridge connection rejected: no paired Android client has negotiated a supported H.264 decoder")
				lastNoVideoTargetLog = time.Now()
			}
			continue
		}
		// There is exactly one driver-side producer. Reject overlapping stale
		// sockets instead of running two readers and two FFmpeg pipelines.
		if !s.videoBridgeActive.CompareAndSwap(false, true) {
			_ = conn.Close()
			s.addLog("Rejected overlapping SteamVR video bridge connection while the previous bridge is still active")
			continue
		}
		_ = conn.SetWriteDeadline(time.Now().Add(time.Second))
		if _, err := conn.Write([]byte{1}); err != nil {
			s.videoBridgeActive.Store(false)
			_ = conn.Close()
			continue
		}
		_ = conn.SetWriteDeadline(time.Time{})
		s.mu.Lock()
		if s.listener == nil {
			s.mu.Unlock()
			s.videoBridgeActive.Store(false)
			_ = conn.Close()
			return
		}
		s.connections[conn] = struct{}{}
		s.mu.Unlock()
		s.wg.Add(1)
		go func() {
			defer s.wg.Done()
			defer s.videoBridgeActive.Store(false)
			defer s.removeConnection(conn)
			defer conn.Close()
			s.handleVideoCaptureConnection(ctx, conn)
		}()
	}
}

func (s *HostService) acquireRawVideoFrame() *rawVideoFrame {
	if frame, ok := s.videoFramePool.Get().(*rawVideoFrame); ok && frame != nil && len(frame.data) == videoWidth*videoHeight*4 {
		return frame
	}
	return &rawVideoFrame{data: make([]byte, videoWidth*videoHeight*4)}
}

func (s *HostService) releaseRawVideoFrame(frame *rawVideoFrame) {
	if frame == nil {
		return
	}
	frame.timestampNs = 0
	s.videoFramePool.Put(frame)
}

// enqueueRawVideoFrame is latest-frame-wins. The TCP reader must keep draining
// the driver's 4.6 MiB BGRA frames even when QSV is briefly slower than capture;
// stale frames are dropped from this tiny queue instead of back-pressuring the
// loopback socket until the driver times out and reconnects.
func (s *HostService) enqueueRawVideoFrame(frame *rawVideoFrame) {
	if frame == nil || s.videoFrameQueue == nil {
		s.releaseRawVideoFrame(frame)
		return
	}
	select {
	case s.videoFrameQueue <- frame:
		return
	default:
	}
	select {
	case stale := <-s.videoFrameQueue:
		s.releaseRawVideoFrame(stale)
		s.videoInputQueueDrops.Add(1)
	default:
	}
	select {
	case s.videoFrameQueue <- frame:
	default:
		s.releaseRawVideoFrame(frame)
		s.videoInputQueueDrops.Add(1)
	}
}

func (s *HostService) handleVideoCaptureConnection(ctx context.Context, conn net.Conn) {
	if tcp, ok := conn.(*net.TCPConn); ok {
		_ = tcp.SetReadBuffer(16 * 1024 * 1024)
	}
	headerBytes := make([]byte, videoprotocol.RawFrameHeaderSize)
	for ctx.Err() == nil {
		_ = conn.SetReadDeadline(time.Now().Add(videoBridgeReadTimeout))
		if _, err := io.ReadFull(conn, headerBytes); err != nil {
			if ctx.Err() != nil {
				return
			}
			if nerr, ok := err.(net.Error); ok && nerr.Timeout() {
				s.addLog("Video capture bridge idle for 10 seconds; reconnecting without restarting the persistent H.264 encoder")
				return
			}
			s.addLog("Video capture bridge closed: " + err.Error())
			return
		}
		header, err := videoprotocol.DecodeRawFrameHeader(headerBytes)
		if err != nil {
			s.addLog("Rejected SteamVR raw video frame: " + err.Error())
			return
		}
		if int(header.Width) != videoWidth || int(header.Height) != videoHeight || header.Stride != uint32(videoWidth*4) {
			s.addLog(fmt.Sprintf("Unexpected SteamVR video profile %dx%d stride=%d; expected %dx%d BGRA", header.Width, header.Height, header.Stride, videoWidth, videoHeight))
			return
		}
		if int(header.PayloadSize) != videoWidth*videoHeight*4 {
			s.addLog(fmt.Sprintf("SteamVR raw frame payload %d does not match negotiated buffer %d", header.PayloadSize, videoWidth*videoHeight*4))
			return
		}
		frame := s.acquireRawVideoFrame()
		_ = conn.SetReadDeadline(time.Now().Add(videoBridgeReadTimeout))
		if _, err := io.ReadFull(conn, frame.data); err != nil {
			s.releaseRawVideoFrame(frame)
			s.addLog("Incomplete SteamVR raw video frame; reconnecting: " + err.Error())
			return
		}
		if _, enabled := s.currentVideoTarget(); !enabled {
			s.releaseRawVideoFrame(frame)
			return
		}
		frame.timestampNs = header.TimestampNs
		s.videoRawFrames.Add(1)
		s.enqueueRawVideoFrame(frame)
	}
}

// videoEncoderLoop owns exactly one FFmpeg process for the Host service
// lifetime. It is intentionally independent of the driver's TCP connection:
// brief loopback reconnects can no longer spawn and destroy FFmpeg repeatedly.
func (s *HostService) videoEncoderLoop(ctx context.Context) {
	defer s.wg.Done()
	var session *encoder.H264EncoderSession
	var retryAfter time.Time
	var framesInWindow uint64
	statsStart := time.Now()
	encodedAtWindowStart := s.videoEncodedFrames.Load()
	udpAtWindowStart := s.videoUDPFragments.Load()
	droppedAtWindowStart := s.videoDroppedFrames.Load()
	inputDropsAtWindowStart := s.videoInputQueueDrops.Load()
	defer func() {
		if session != nil {
			session.Close()
		}
		for {
			select {
			case frame := <-s.videoFrameQueue:
				s.releaseRawVideoFrame(frame)
			default:
				return
			}
		}
	}()

	for {
		select {
		case <-ctx.Done():
			return
		case frame := <-s.videoFrameQueue:
			if frame == nil {
				continue
			}
			if session == nil {
				if time.Now().Before(retryAfter) {
					s.releaseRawVideoFrame(frame)
					continue
				}
				s.mu.RLock()
				ffmpegPath := s.videoFFmpegPath
				selected := s.videoEncoderSelected
				ready := s.videoEncoderReady
				s.mu.RUnlock()
				if !ready || selected.Name == "" {
					s.releaseRawVideoFrame(frame)
					retryAfter = time.Now().Add(5 * time.Second)
					continue
				}
				created, createErr := encoder.StartH264EncoderWithCapability(ctx, ffmpegPath, videoWidth, videoHeight, videoFrameRate, selected,
					func(accessUnit []byte, id uint32, timestamp uint64, key bool) error {
						s.videoEncodedFrames.Add(1)
						if sendErr := s.sendEncodedVideoFrame(accessUnit, id, timestamp, key); sendErr != nil {
							s.addLog("Dropped one H.264 access unit after UDP send failure; persistent encoder remains active: " + sendErr.Error())
						}
						return nil
					}, s.addLog)
				if createErr != nil {
					s.addLog("Persistent H.264 encoder startup failed; retrying after 5 seconds: " + createErr.Error())
					retryAfter = time.Now().Add(5 * time.Second)
					s.releaseRawVideoFrame(frame)
					continue
				}
				session = created
				s.addLog("Persistent live VR hardware H.264 encoder started: " + session.EncoderName())
			}

			if err := session.WriteFrame(frame.data, frame.timestampNs); err != nil {
				s.videoDroppedFrames.Add(1)
				s.addLog("Persistent H.264 encoder input failed; stopping the failed session and retrying after 5 seconds: " + err.Error())
				session.Close()
				session = nil
				retryAfter = time.Now().Add(5 * time.Second)
				s.releaseRawVideoFrame(frame)
				continue
			}
			s.releaseRawVideoFrame(frame)
			framesInWindow++
			if elapsed := time.Since(statsStart); elapsed >= 5*time.Second {
				seconds := maxDurationSeconds(elapsed)
				encodedNow := s.videoEncodedFrames.Load()
				udpNow := s.videoUDPFragments.Load()
				droppedNow := s.videoDroppedFrames.Load()
				inputDropsNow := s.videoInputQueueDrops.Load()
				s.addLog(fmt.Sprintf("Video pipeline: queued-to-FFmpeg %.1f FPS | H.264 %.1f FPS | UDP fragments %d | input queue drops %d | dropped access units %d | backend %s",
					float64(framesInWindow)/seconds,
					float64(encodedNow-encodedAtWindowStart)/seconds,
					udpNow-udpAtWindowStart, inputDropsNow-inputDropsAtWindowStart, droppedNow-droppedAtWindowStart, session.EncoderName()))
				framesInWindow = 0
				encodedAtWindowStart = encodedNow
				udpAtWindowStart = udpNow
				droppedAtWindowStart = droppedNow
				inputDropsAtWindowStart = inputDropsNow
				statsStart = time.Now()
			}
		}
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

type HostNetworkAddress struct {
	Name string
	IP   string
	Kind string
}

func classifyHostInterface(name string) string {
	normalized := strings.ToLower(name)
	switch {
	case strings.Contains(normalized, "wi-fi"), strings.Contains(normalized, "wifi"), strings.Contains(normalized, "wlan"):
		return "Wi-Fi"
	case strings.Contains(normalized, "vethernet"):
		return "Network"
	case strings.Contains(normalized, "ethernet"), strings.HasPrefix(normalized, "eth"):
		return "Ethernet"
	default:
		return "Network"
	}
}

func localIPv4NetworkAddresses() []HostNetworkAddress {
	interfaces, err := net.Interfaces()
	if err != nil {
		return nil
	}
	var result []HostNetworkAddress
	for _, item := range interfaces {
		if item.Flags&net.FlagUp == 0 || item.Flags&net.FlagLoopback != 0 {
			continue
		}
		addresses, _ := item.Addrs()
		for _, address := range addresses {
			ipNet, ok := address.(*net.IPNet)
			if ok && ipNet.IP.To4() != nil {
				result = append(result, HostNetworkAddress{Name: item.Name, IP: ipNet.IP.String(), Kind: classifyHostInterface(item.Name)})
			}
		}
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].Kind != result[j].Kind {
			return result[i].Kind < result[j].Kind
		}
		if result[i].Name != result[j].Name {
			return result[i].Name < result[j].Name
		}
		return result[i].IP < result[j].IP
	})
	return result
}

func localIPv4Addresses() []string {
	addresses := localIPv4NetworkAddresses()
	result := make([]string, 0, len(addresses))
	seen := make(map[string]struct{}, len(addresses))
	for _, address := range addresses {
		if _, exists := seen[address.IP]; exists {
			continue
		}
		seen[address.IP] = struct{}{}
		result = append(result, address.IP)
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
