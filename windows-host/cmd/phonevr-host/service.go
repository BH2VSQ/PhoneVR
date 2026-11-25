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

	"github.com/vsq-studio/phonevr-host/internal/aoa"
	"github.com/vsq-studio/phonevr-host/internal/encoder"
	"github.com/vsq-studio/phonevr-host/internal/protocol"
	"github.com/vsq-studio/phonevr-host/internal/usbmux"
	"github.com/vsq-studio/phonevr-host/internal/videoprotocol"
)

const (
	protocolVersion             = 1
	controlHandshakeTimeout     = 8 * time.Second
	usbControlHandshakeTimeout  = 30 * time.Second
	controlIdleTimeout          = 15 * time.Second
	controlWriteTimeout         = 5 * time.Second
	discoveryPortDefault        = 48100
	controlPortDefault          = 48101
	posePortDefault             = 48102
	steamVRStatusPort           = 48105
	steamVRVideoCapturePort     = 48106
	steamVRControllerBridgePort = 48108
	androidVideoPort            = 48104
	videoInputQueueDepth        = 2 // one frame may be written to FFmpeg while two newer frames wait
	videoOutputQueueDepth       = 8 // isolate FFmpeg stdout from potentially blocking USB Bulk writes
	videoUSBQueueDepth          = 4 // bounded independent queue for striped AOA writes
	videoRetainedFrameLimit     = 16
	videoRetainedFrameMaxBytes  = 16 * 1024 * 1024
	videoRetainedFrameTTL       = 700 * time.Millisecond
	videoBridgeReadTimeout      = 10 * time.Second
	discoveryQuery              = "PHONEVR_DISCOVER_V1"
	discoveryPrefix             = "PHONEVR_HOST_V1 "
	maxLogEntries               = 250
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
	Transport         string                `json:"transport,omitempty"`
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
	Running                     bool                   `json:"running"`
	ServerName                  string                 `json:"serverName"`
	ListenAddress               string                 `json:"listenAddress"`
	LocalIPv4                   []string               `json:"localIPv4"`
	DiscoveryPort               int                    `json:"discoveryPort"`
	ControlPort                 int                    `json:"controlPort"`
	PosePort                    int                    `json:"posePort"`
	ConnectedDevices            []DeviceSnapshot       `json:"connectedDevices"`
	LatestPose                  *PoseSnapshot          `json:"latestPose,omitempty"`
	PacketsReceived             uint64                 `json:"packetsReceived"`
	LastPacketAt                string                 `json:"lastPacketAt,omitempty"`
	Uptime                      string                 `json:"uptime,omitempty"`
	LastError                   string                 `json:"lastError,omitempty"`
	SteamVRConnected            bool                   `json:"steamVRConnected"`
	SteamVRDriverLoaded         bool                   `json:"steamVRDriverLoaded"`
	SteamVRHMDActive            bool                   `json:"steamVRHmdActive"`
	SteamVRPoseValid            bool                   `json:"steamVRPoseValid"`
	SteamVRDisplayActive        bool                   `json:"steamVRDisplayActive"`
	SteamVRHasPresentedFrame    bool                   `json:"steamVRHasPresentedFrame"`
	SteamVRFrameCounter         uint64                 `json:"steamVRFrameCounter"`
	SteamVRHeartbeatAgeMS       int64                  `json:"steamVRHeartbeatAgeMs,omitempty"`
	VideoCaptureBridgeActive    bool                   `json:"videoCaptureBridgeActive"`
	VideoRawFrames              uint64                 `json:"videoRawFrames"`
	VideoEncodedFrames          uint64                 `json:"videoEncodedFrames"`
	VideoUDPFragments           uint64                 `json:"videoUdpFragments"`
	VideoRetransmittedFragments uint64                 `json:"videoRetransmittedFragments"`
	VideoDroppedFrames          uint64                 `json:"videoDroppedFrames"`
	VideoTransport              VideoTransportSnapshot `json:"videoTransport"`
	Logs                        []string               `json:"logs"`
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
	VideoOnly         bool            `json:"videoOnly,omitempty"`
	MediaCapabilities json.RawMessage `json:"mediaCapabilities,omitempty"`
}

type discoveryMessage struct {
	Name        string `json:"name"`
	ControlPort int    `json:"controlPort"`
	Protocol    int    `json:"protocol"`
}

type welcomeMessage struct {
	Type                string   `json:"type"`
	Protocol            int      `json:"protocol"`
	SessionID           string   `json:"sessionId"`
	ServerName          string   `json:"serverName"`
	PosePort            int      `json:"posePort"`
	Capabilities        []string `json:"capabilities"`
	VideoEnabled        bool     `json:"videoEnabled"`
	VideoPort           int      `json:"videoPort,omitempty"`
	VideoCodec          string   `json:"videoCodec,omitempty"`
	VideoMimeType       string   `json:"videoMimeType,omitempty"`
	VideoTransport      string   `json:"videoTransport,omitempty"`
	VideoWidth          int      `json:"videoWidth,omitempty"`
	VideoHeight         int      `json:"videoHeight,omitempty"`
	VideoFPS            int      `json:"videoFps,omitempty"`
	VideoReorderGraceMS int      `json:"videoReorderGraceMs,omitempty"`
	VideoDecoderName    string   `json:"videoDecoderName,omitempty"`
	VideoReason         string   `json:"videoReason,omitempty"`
}

type controlMessage struct {
	Type                  string                    `json:"type"`
	Controllers           *protocol.ControllerFrame `json:"controllers,omitempty"`
	RequestID             string                    `json:"requestId,omitempty"`
	Settings              *PhoneDisplaySettings     `json:"settings,omitempty"`
	Presets               []OpticalPreset           `json:"presets,omitempty"`
	PresetID              string                    `json:"presetId,omitempty"`
	Action                string                    `json:"action,omitempty"`
	Status                string                    `json:"status,omitempty"`
	Message               string                    `json:"message,omitempty"`
	BatteryLevel          *int                      `json:"batteryLevel,omitempty"`
	Charging              bool                      `json:"charging,omitempty"`
	VideoTransport        string                    `json:"videoTransport,omitempty"`
	LastPacketAgeMS       int64                     `json:"lastPacketAgeMs,omitempty"`
	LastDecodedAgeMS      int64                     `json:"lastDecodedAgeMs,omitempty"`
	VideoProbe            bool                      `json:"videoProbe,omitempty"`
	VideoRXBytesPerSecond uint64                    `json:"videoRxBytesPerSecond,omitempty"`
	VideoFrameID          uint32                    `json:"frameId,omitempty"`
	MissingFragments      []int                     `json:"missingFragments,omitempty"`
}

type activeDevice struct {
	Snapshot     DeviceSnapshot
	Conn         net.Conn
	USBLink      *usbmux.Mux
	WriteMu      *sync.Mutex
	Ready        bool
	VideoEnabled bool
	VideoOnly    bool
	LastPingAt   time.Time
}

type videoTarget struct {
	UDP       *net.UDPAddr
	USB       *usbmux.Mux
	Transport string
	DeviceID  string
	Switched  bool
}

type retainedVideoFrameKey struct {
	deviceID string
	frameID  uint32
}

type retainedVideoFrame struct {
	fragments  [][]byte
	paths      []string // "network" or "usb" for each original fragment
	target     videoTarget
	createdAt  time.Time
	totalBytes int
}

type usbVideoWork struct {
	link          *usbmux.Mux
	deviceID      string
	fragments     [][]byte
	fallbackUDP   *net.UDPAddr
	fallbackParts [][]byte
	hasOtherPath  bool
	isProbe       bool
}

type taggedNetConn struct {
	net.Conn
	remote net.Addr
}

func (c *taggedNetConn) RemoteAddr() net.Addr { return c.remote }

type taggedAddr string

func (a taggedAddr) Network() string { return "usb" }
func (a taggedAddr) String() string  { return string(a) }

type rawVideoFrame struct {
	data        []byte
	timestampNs uint64
}

type encodedVideoFrame struct {
	data        []byte
	frameID     uint32
	timestampNs uint64
	keyFrame    bool
}

var errNoVideoTarget = errors.New("no paired Android video target")

type HostService struct {
	lifecycleMu                 sync.Mutex
	videoEncoderProbeMu         sync.Mutex
	mu                          sync.RWMutex
	config                      HostConfig
	serverName                  string
	localIPv4                   []string
	startedAt                   time.Time
	listener                    net.Listener
	discovery                   *net.UDPConn
	pose                        *net.UDPConn
	steamVRStatus               *net.UDPConn
	videoCaptureListener        net.Listener
	videoUDP                    *net.UDPConn
	videoFFmpegPath             string
	videoEncoderProbeDone       chan struct{}
	videoEncoderProbeProfileID  string
	videoEncoderReady           bool
	videoEncoderSelected        encoder.Capability
	videoEncoderCandidates      []encoder.Capability
	videoEncoderError           string
	videoBridgeActive           atomic.Bool
	videoProfileMismatchLogged  atomic.Bool
	videoFrameQueue             chan *rawVideoFrame
	videoOutputQueue            chan encodedVideoFrame
	videoUSBQueue               chan usbVideoWork
	videoStripeMu               sync.Mutex
	videoStripeCredit           map[string]int
	retainedVideoMu             sync.Mutex
	retainedVideoFrames         map[retainedVideoFrameKey]retainedVideoFrame
	retainedVideoBytes          int
	videoNackMu                 sync.Mutex
	videoNackSeen               map[string]time.Time
	videoOutputMu               sync.Mutex
	videoOutputNeedsKeyFrame    bool
	videoFramePool              sync.Pool
	videoInputQueueDrops        atomic.Uint64
	videoOutputQueueDrops       atomic.Uint64
	videoTransportSendMaxNs     atomic.Int64
	videoRawFrames              atomic.Uint64
	videoEncodedFrames          atomic.Uint64
	videoUDPFragments           atomic.Uint64
	videoRetransmittedFragments atomic.Uint64
	videoDroppedFrames          atomic.Uint64
	steamVRHeartbeatAt          time.Time
	steamVRFlags                uint8
	steamVRFrameCounter         uint64
	cancel                      context.CancelFunc
	wg                          sync.WaitGroup
	devices                     map[string]activeDevice
	connections                 map[net.Conn]struct{}
	usbLinks                    map[string]*usbmux.Mux
	transportMode               VideoTransportMode
	transportPreference         VideoTransportPreference
	transportConfigPath         string
	transportConfigMu           sync.Mutex
	videoProfile                VideoStreamProfile
	videoProfilePath            string
	usbScanWake                 chan struct{}
	activeVideoTransport        string
	activeVideoDeviceID         string
	activeVideoSince            time.Time
	lastTransportSwitchAt       time.Time
	lastTransportSwitchReason   string
	lastVideoPathProbeAt        time.Time
	transportHealth             map[string]transportHealth
	// The pose snapshot is immutable after publication. Atomic publication avoids
	// reader/writer lock stalls when the desktop UI is busy rendering the model.
	latestPose       atomic.Pointer[PoseSnapshot]
	posePackets      atomic.Uint64
	poseLastPacketNs atomic.Int64
	lastError        string
	logs             []string
	consoleLogs      bool
}

func NewHostService(consoleLogs bool) *HostService {
	transportConfigPath, _ := videoTransportConfigPath()
	videoProfilePath, _ := videoStreamProfileConfigPath()
	transportMode, transportPreference := loadVideoTransportConfigFrom(transportConfigPath)
	return &HostService{
		devices: make(map[string]activeDevice), connections: make(map[net.Conn]struct{}), usbLinks: make(map[string]*usbmux.Mux),
		transportMode: transportMode, transportPreference: transportPreference, transportConfigPath: transportConfigPath,
		videoProfile: loadVideoStreamProfile(videoProfilePath), videoProfilePath: videoProfilePath,
		transportHealth: make(map[string]transportHealth), consoleLogs: consoleLogs,
		usbScanWake:         make(chan struct{}, 1),
		retainedVideoFrames: make(map[retainedVideoFrameKey]retainedVideoFrame),
		videoNackSeen:       make(map[string]time.Time),
	}
}

// SelectedVideoStreamProfile returns the profile currently selected on disk.
// The active pipeline keeps the profile loaded when the Host service started.
func (s *HostService) SelectedVideoStreamProfile() VideoStreamProfile {
	return loadVideoStreamProfile(s.videoProfilePath)
}

// SaveVideoStreamProfile changes the next-start profile. The active Host and
// SteamVR driver intentionally keep their existing dimensions until restarted.
func (s *HostService) SaveVideoStreamProfile(profileID string) error {
	return saveVideoStreamProfile(s.videoProfilePath, profileID)
}

func (s *HostService) activeVideoProfile() VideoStreamProfile {
	profile := s.videoProfile
	if profile.Width <= 0 || profile.Height <= 0 || profile.FPS <= 0 || profile.BitrateMbps <= 0 {
		return defaultVideoStreamProfile()
	}
	return profile
}

func (s *HostService) activeVideoDimensions() (int, int, int) {
	profile := s.activeVideoProfile()
	return profile.Width, profile.Height, profile.FPS
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

	// Reload the pending profile on every service start. This also supports a
	// Stop/Start cycle from the native UI without requiring the entire Host
	// process to exit.
	s.videoProfile = loadVideoStreamProfile(s.videoProfilePath)
	activeProfile := s.activeVideoProfile()
	s.mu.RLock()
	probedProfileID := s.videoEncoderProbeProfileID
	ffmpegPath := s.videoFFmpegPath
	s.mu.RUnlock()
	if probedProfileID != activeProfile.ID && (probedProfileID != "" || strings.TrimSpace(ffmpegPath) != "") {
		s.addLog(fmt.Sprintf("Video profile changed to %s; rerunning hardware H.264 preflight at %dx%d@%d", activeProfile.ID, activeProfile.Width, activeProfile.Height, activeProfile.FPS))
		s.ProbeVideoEncoders(ffmpegPath)
	}
	activeProfilePath, err := videoStreamProfileActiveConfigPath()
	if err != nil {
		return fmt.Errorf("resolve active video profile path: %w", err)
	}
	if err := saveVideoStreamProfile(activeProfilePath, activeProfile.ID); err != nil {
		return fmt.Errorf("publish active video profile for SteamVR driver: %w", err)
	}
	s.addLog(fmt.Sprintf("SteamVR active video profile synchronized: %s | %dx%d@%d", activeProfile.ID, activeProfile.Width, activeProfile.Height, activeProfile.FPS))
	s.videoProfileMismatchLogged.Store(false)

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
	s.videoOutputQueue = make(chan encodedVideoFrame, videoOutputQueueDepth)
	s.videoUSBQueue = make(chan usbVideoWork, videoUSBQueueDepth)
	s.videoStripeMu.Lock()
	s.videoStripeCredit = make(map[string]int)
	s.videoStripeMu.Unlock()
	s.retainedVideoMu.Lock()
	s.retainedVideoFrames = make(map[retainedVideoFrameKey]retainedVideoFrame)
	s.retainedVideoBytes = 0
	s.retainedVideoMu.Unlock()
	s.videoNackMu.Lock()
	s.videoNackSeen = make(map[string]time.Time)
	s.videoNackMu.Unlock()
	s.videoOutputMu.Lock()
	s.videoOutputNeedsKeyFrame = false
	s.videoOutputMu.Unlock()
	s.videoInputQueueDrops.Store(0)
	s.videoOutputQueueDrops.Store(0)
	s.videoTransportSendMaxNs.Store(0)
	videoWidth, videoHeight, _ := s.activeVideoDimensions()
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
	s.usbLinks = make(map[string]*usbmux.Mux)
	s.lastError = ""
	s.mu.Unlock()
	s.latestPose.Store(nil)
	s.posePackets.Store(0)
	s.poseLastPacketNs.Store(0)

	scannerState := "enabled"
	if !s.usbAOAScanningEnabled() {
		scannerState = "paused (network-only video transport)"
	}
	s.addLog(fmt.Sprintf("Host started on %s | TCP %d | UDP discovery %d | pose %d | USB AOA scanner %s", config.ListenAddress, config.ControlPort, config.DiscoveryPort, config.PosePort, scannerState))
	s.addLog(fmt.Sprintf("Active SBS stream profile: %s | %dx%d@%d | H.264 target bitrate %d Mbps", activeProfile.ID, activeProfile.Width, activeProfile.Height, activeProfile.FPS, activeProfile.BitrateMbps))
	for _, address := range networkAddresses {
		s.addLog(fmt.Sprintf("Host interface [%s] %s: %s", address.Kind, address.Name, address.IP))
	}

	s.wg.Add(9)
	go s.acceptLoop(ctx, tcpListener)
	go s.usbAccessoryLoop(ctx)
	go s.discoveryLoop(ctx, udpDiscovery, serverName, config.ControlPort)
	go s.poseLoop(ctx, udpPose)
	go s.steamVRStatusLoop(ctx, udpSteamVRStatus)
	go s.videoCaptureLoop(ctx, videoCaptureListener)
	go s.videoEncoderLoop(ctx)
	go s.videoTransportSendLoop(ctx)
	go s.videoUSBWorkSendLoop(ctx, s.videoUSBQueue)
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

// ProbeVideoEncoders runs a bounded FFmpeg inventory and validates every available hardware H.264 backend.
// Runtime session health is monitored separately because an encoder may pass
// a synthetic preflight and still fail when fed live SteamVR frames.
func (s *HostService) ProbeVideoEncoders(ffmpegPath string) {
	s.videoEncoderProbeMu.Lock()
	defer s.videoEncoderProbeMu.Unlock()

	profile := s.activeVideoProfile()
	done := make(chan struct{})
	s.mu.Lock()
	s.videoFFmpegPath = ffmpegPath
	s.videoEncoderProbeDone = done
	s.videoEncoderReady = false
	s.videoEncoderCandidates = nil
	s.videoEncoderSelected = encoder.Capability{}
	s.videoEncoderError = "H.264 hardware encoder preflight is running"
	s.mu.Unlock()
	defer func() {
		s.mu.Lock()
		s.videoEncoderProbeProfileID = profile.ID
		s.mu.Unlock()
		close(done)
	}()

	// This probe is deliberately synchronous. main() invokes it before Start(),
	// so the TCP pairing listener cannot accept a phone before the H.264
	// hardware-encoder result has been committed to service state.
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	ffmpegCandidates, err := encoder.ProbeFFmpeg(ctx, ffmpegPath)
	if err != nil {
		s.mu.Lock()
		s.videoEncoderError = err.Error()
		s.videoEncoderReady = false
		s.mu.Unlock()
		s.addLog("Video encoder discovery unavailable: " + err.Error())
		return
	}
	for _, candidate := range ffmpegCandidates {
		s.addLog(fmt.Sprintf("Video encoder candidate (adapter/session unverified): %s | %s | %s", candidate.Name, candidate.Codec, candidate.Backend))
	}
	videoWidth, videoHeight, videoFPS := s.activeVideoDimensions()
	validated, rejected, probeErr := encoder.ProbeHardwareH264EncodersDetailed(ctx, ffmpegPath, videoWidth, videoHeight, videoFPS)
	for _, reason := range rejected {
		s.addLog("Live VR hardware H.264 candidate rejected: " + reason)
	}
	s.mu.Lock()
	s.videoEncoderReady = probeErr == nil && len(validated) > 0
	s.videoEncoderCandidates = append([]encoder.Capability(nil), validated...)
	if probeErr != nil {
		s.videoEncoderError = probeErr.Error()
		s.videoEncoderSelected = encoder.Capability{}
	} else {
		s.videoEncoderError = ""
		s.videoEncoderSelected = validated[0]
	}
	s.mu.Unlock()
	if probeErr != nil {
		s.addLog("Live VR video disabled: " + probeErr.Error())
		return
	}
	for index, candidate := range validated {
		s.addLog(fmt.Sprintf("Live VR hardware H.264 candidate %d validated: %s/%s", index+1, candidate.Name, candidate.Backend))
	}
	s.addLog(fmt.Sprintf("Live VR video hardware encoder preflight passed: %d usable H.264 backend(s); primary=%s/%s", len(validated), validated[0].Name, validated[0].Backend))
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
	usbLinks := make([]*usbmux.Mux, 0, len(s.usbLinks))
	for _, mux := range s.usbLinks {
		if mux != nil {
			usbLinks = append(usbLinks, mux)
		}
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
	s.usbLinks = make(map[string]*usbmux.Mux)
	s.mu.Unlock()
	s.latestPose.Store(nil)
	s.poseLastPacketNs.Store(0)

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
	// Abort outstanding WinUSB transfers before waiting for real-time workers.
	for _, mux := range usbLinks {
		_ = mux.Close()
	}
	s.wg.Wait()
	s.mu.Lock()
	s.videoUSBQueue = nil
	s.mu.Unlock()
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
	snapshot.VideoRetransmittedFragments = s.videoRetransmittedFragments.Load()
	snapshot.VideoDroppedFrames = s.videoDroppedFrames.Load()
	snapshot.VideoTransport = s.videoTransportSnapshotLocked()
	deviceByID := make(map[string]int)
	for key, device := range s.devices {
		item := device.Snapshot
		identity := strings.TrimSpace(item.DeviceID)
		if identity == "" {
			identity = "legacy:" + key
		}
		if index, exists := deviceByID[identity]; exists {
			current := &snapshot.ConnectedDevices[index]
			if !strings.Contains(current.Transport, item.Transport) {
				if current.Transport == "" {
					current.Transport = item.Transport
				} else if item.Transport != "" {
					current.Transport += " + " + item.Transport
				}
			}
			if item.BatteryLevel != nil {
				current.BatteryLevel = item.BatteryLevel
				current.Charging = item.Charging
			}
			if item.DisplaySettings != nil {
				current.DisplaySettings = item.DisplaySettings
			}
			if len(item.OpticalPresets) > 0 {
				current.OpticalPresets = item.OpticalPresets
			}
			if item.ConnectedAt < current.ConnectedAt {
				current.ConnectedAt = item.ConnectedAt
			}
			continue
		}
		deviceByID[identity] = len(snapshot.ConnectedDevices)
		snapshot.ConnectedDevices = append(snapshot.ConnectedDevices, item)
	}
	if running {
		snapshot.Uptime = time.Since(s.startedAt).Round(time.Second).String()
	}
	snapshot.Logs = append([]string(nil), s.logs...)
	s.mu.RUnlock()

	// Read the immutable pose snapshot atomically. UI snapshots must never hold
	// a lock that can delay the 180 Hz pose receiver.
	if latest := s.latestPose.Load(); latest != nil {
		copyPose := *latest
		snapshot.LatestPose = &copyPose
	}
	snapshot.PacketsReceived = s.posePackets.Load()
	if receivedNs := s.poseLastPacketNs.Load(); receivedNs > 0 {
		snapshot.LastPacketAt = time.Unix(0, receivedNs).Format(time.RFC3339)
	}

	sort.Slice(snapshot.ConnectedDevices, func(i, j int) bool {
		return snapshot.ConnectedDevices[i].ConnectedAt < snapshot.ConnectedDevices[j].ConnectedAt
	})
	return snapshot
}

// PoseFrame returns only the high-frequency head-pose state so the native desktop
// OpenGL viewer can refresh the HMD model without polling device and log
// metadata on every animation update.
func (s *HostService) PoseFrame() PoseFrameSnapshot {
	var latest *PoseSnapshot
	if current := s.latestPose.Load(); current != nil {
		copyPose := *current
		latest = &copyPose
	}
	return PoseFrameSnapshot{LatestPose: latest, PacketsReceived: s.posePackets.Load()}
}

func (s *HostService) usbAOAScanningEnabled() bool {
	s.mu.RLock()
	mode := s.transportMode
	s.mu.RUnlock()
	if mode == "" {
		mode = VideoTransportNetwork
	}
	return videoTransportUsesAOA(mode)
}

func (s *HostService) usbAccessoryLoop(ctx context.Context) {
	defer s.wg.Done()
	for ctx.Err() == nil {
		if !s.usbAOAScanningEnabled() {
			select {
			case <-ctx.Done():
				return
			case <-s.usbScanWake:
				continue
			}
		}

		// The AOA package intentionally scans until it finds a device. Give
		// each scan a child context so changing to network-only mode can cancel
		// enumeration promptly instead of leaving SetupAPI polling active.
		scanCtx, cancelScan := context.WithCancel(ctx)
		scanFinished := make(chan struct{})
		watcherFinished := make(chan struct{})
		go func() {
			defer close(watcherFinished)
			select {
			case <-ctx.Done():
				cancelScan()
			case <-s.usbScanWake:
				cancelScan()
			case <-scanFinished:
			}
		}()
		device, err := aoa.NextAccessoryWithStatus(scanCtx, s.addLog)
		close(scanFinished)
		cancelScan()
		<-watcherFinished
		if ctx.Err() != nil {
			return
		}
		if err != nil {
			if errors.Is(err, context.Canceled) {
				continue
			}
			s.addLog("USB AOA scanner error: " + err.Error())
			time.Sleep(time.Second)
			continue
		}
		if device == nil {
			continue
		}
		if !s.usbAOAScanningEnabled() {
			_ = device.Close()
			continue
		}
		s.addLog("Android AOA accessory detected: " + device.ID())
		s.serveUSBAccessory(ctx, device)
	}
}

func (s *HostService) serveUSBAccessory(ctx context.Context, device *aoa.Device) {
	device.SetLogger(func(message string) { s.addLog("USB AOA " + message) })
	mux := usbmux.New(device)
	mux.SetLogger(func(message string) { s.addLog("USB AOA mux " + message) })
	deviceKey := "USB-AOA:" + device.ID()
	serverPipe, accessoryPipe := net.Pipe()
	conn := &taggedNetConn{Conn: serverPipe, remote: taggedAddr(deviceKey)}
	s.mu.Lock()
	if s.listener == nil || ctx.Err() != nil {
		s.mu.Unlock()
		_ = conn.Close()
		_ = accessoryPipe.Close()
		_ = mux.Close()
		return
	}
	s.connections[conn] = struct{}{}
	s.usbLinks[deviceKey] = mux
	s.mu.Unlock()

	poseForward, poseErr := net.DialUDP("udp4", nil, &net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: s.config.PosePort})
	if poseErr != nil {
		s.addLog("USB AOA pose bridge unavailable: " + poseErr.Error())
	}
	var bridgeWG sync.WaitGroup
	bridgeWG.Add(2)
	go func() {
		defer bridgeWG.Done()
		defer accessoryPipe.Close()
		loggedControlFrames := 0
		for {
			frame, err := mux.ReadFrame()
			if err != nil {
				if ctx.Err() == nil {
					s.addLog("USB AOA Bulk IN stopped: " + err.Error())
				}
				_ = accessoryPipe.Close()
				return
			}
			switch frame.Channel {
			case usbmux.ChannelControl:
				if loggedControlFrames < 6 {
					preview := frame.Payload
					if len(preview) > 120 {
						preview = preview[:120]
					}
					s.addLog(fmt.Sprintf("USB AOA control RX frame #%d: bytes=%d preview=%q", loggedControlFrames+1, len(frame.Payload), string(preview)))
					loggedControlFrames++
				}
				if _, err := accessoryPipe.Write(frame.Payload); err != nil {
					s.addLog("USB AOA control bridge to Host failed: " + err.Error())
					return
				}
			case usbmux.ChannelPose:
				if poseForward != nil && len(frame.Payload) > 0 {
					if _, err := poseForward.Write(frame.Payload); err != nil {
						s.addLog("USB AOA pose forward failed: " + err.Error())
					}
				}
			}
		}
	}()
	go func() {
		defer bridgeWG.Done()
		buffer := make([]byte, 32*1024)
		loggedControlChunks := 0
		for {
			n, err := accessoryPipe.Read(buffer)
			if n > 0 {
				payload := append([]byte(nil), buffer[:n]...)
				if loggedControlChunks < 6 {
					preview := payload
					if len(preview) > 120 {
						preview = preview[:120]
					}
					s.addLog(fmt.Sprintf("USB AOA control TX chunk #%d: bytes=%d preview=%q", loggedControlChunks+1, len(payload), string(preview)))
					loggedControlChunks++
				}
				if writeErr := mux.WriteFrame(usbmux.ChannelControl, payload); writeErr != nil {
					s.addLog("USB AOA control Bulk OUT failed: " + writeErr.Error())
					return
				}
			}
			if err != nil {
				return
			}
		}
	}()

	s.handleControlConnection(ctx, conn)
	_ = accessoryPipe.Close()
	_ = conn.Close()
	_ = mux.Close()
	if poseForward != nil {
		_ = poseForward.Close()
	}
	bridgeWG.Wait()
	s.mu.Lock()
	delete(s.usbLinks, deviceKey)
	s.mu.Unlock()
	s.addLog("Android AOA accessory disconnected")
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
const poseBatchDrainWindow = time.Millisecond

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

	// Drain a short window so datagrams that the Windows UDP/IOCP receive path
	// has not surfaced yet can join this batch. One millisecond remains well
	// below the 5.5ms interval of a 180Hz sensor and avoids flaky single-packet
	// batches caused by sub-millisecond Windows receive scheduling.
	if err := conn.SetReadDeadline(time.Now().Add(poseBatchDrainWindow)); err != nil {
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
	var maxPosePublishDuration time.Duration
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
		publishStarted := time.Now()
		snapshot := &PoseSnapshot{
			Sequence: latest.pose.Sequence, TimestampNs: latest.pose.TimestampNs,
			X: latest.pose.X, Y: latest.pose.Y, Z: latest.pose.Z, W: latest.pose.W,
			Yaw: yaw, Pitch: pitch, Roll: roll,
			Source: latest.remote.IP.String(), ReceivedAt: latest.receivedAt.Format(time.RFC3339Nano),
		}
		s.posePackets.Add(uint64(len(batch)))
		s.poseLastPacketNs.Store(latest.receivedAt.UnixNano())
		s.latestPose.Store(snapshot)
		if elapsed := time.Since(publishStarted); elapsed > maxPosePublishDuration {
			maxPosePublishDuration = elapsed
		}

		if time.Since(statsStarted) >= 5*time.Second {
			seconds := time.Since(statsStarted).Seconds()
			rxRate := float64(statsReceived) / seconds
			forwardRate := float64(statsForwarded) / seconds
			s.addLog(fmt.Sprintf("Pose pipeline: Host RX %.1f Hz, forwarded %.1f Hz, coalesced %d, sequence gaps %d, invalid %d, max Host read gap %.1f ms, max Android sensor interval %.1f ms, max batch %d, max pose publish duration %.3f ms, loopback write errors %d",
				rxRate, forwardRate, statsCoalesced, statsSequenceGaps, statsInvalid,
				float64(maxHostReadGap)/float64(time.Millisecond),
				float64(maxSensorInterval)/float64(time.Millisecond), maxBatchSize,
				float64(maxPosePublishDuration)/float64(time.Millisecond), statsBridgeErrors))
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
			maxPosePublishDuration = 0
		}
	}
}

func (s *HostService) handleControlConnection(ctx context.Context, conn net.Conn) {
	defer conn.Close()
	defer s.removeConnection(conn)
	handshakeTimeout := controlHandshakeTimeoutFor(conn)
	_ = conn.SetReadDeadline(time.Now().Add(handshakeTimeout))
	if conn.RemoteAddr() != nil && conn.RemoteAddr().Network() == "usb" {
		s.addLog(fmt.Sprintf("USB AOA control handshake: allowing %s for Android accessory permission and hello", handshakeTimeout))
	}
	reader := bufio.NewScanner(conn)
	reader.Buffer(make([]byte, 1024), 64*1024)
	if !reader.Scan() {
		return
	}
	var hello helloMessage
	if err := json.Unmarshal(reader.Bytes(), &hello); err != nil {
		_ = writeJSONLineWithDeadline(conn, map[string]any{"type": "error", "message": "invalid hello JSON"})
		return
	}
	if hello.Type != "hello" || hello.Protocol != protocolVersion {
		_ = writeJSONLineWithDeadline(conn, map[string]any{"type": "error", "message": "protocol mismatch"})
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
	videoWidth, videoHeight, videoFPS := s.activeVideoDimensions()
	selectedAndroidDecoder := selectH264DecoderProfile(mediaCapabilities, videoWidth, videoHeight, videoFPS)
	if selectedAndroidDecoder == "" {
		s.addLog(fmt.Sprintf("Android H.264 decoder negotiation details for %s: %s",
			hello.DeviceName, describeH264DecoderCapabilities(mediaCapabilities)))
	}
	videoEnabled := hardwareEncoderReady && selectedAndroidDecoder != ""
	if videoEnabled && !h264DecoderClassificationReliable(mediaCapabilities, selectedAndroidDecoder) {
		s.addLog(fmt.Sprintf("Android decoder %s is on a pre-Android-10 platform; accepting the vendor-codec heuristic after profile capability checks", selectedAndroidDecoder))
	}
	s.mu.RLock()
	usbLink := s.usbLinks[remote]
	s.mu.RUnlock()
	transportName := "Wi-Fi / LAN"
	if usbLink != nil {
		transportName = "USB AOA Bulk"
	}
	device := activeDevice{Snapshot: DeviceSnapshot{
		Name: hello.DeviceName, DeviceID: hello.DeviceID, Client: hello.Client, Transport: transportName,
		Address: remote, ConnectedAt: time.Now().Format(time.RFC3339),
		MediaCapabilities: append(json.RawMessage(nil), mediaCapabilities...),
	}, Conn: conn, USBLink: usbLink, WriteMu: &sync.Mutex{}, VideoEnabled: videoEnabled, VideoOnly: hello.VideoOnly, LastPingAt: time.Now()}
	s.mu.Lock()
	s.devices[remote] = device
	s.mu.Unlock()
	if hello.VideoOnly {
		s.addLog(fmt.Sprintf("Additional video path paired: %s via %s (%s)", hello.DeviceName, transportName, remote))
	} else {
		s.addLog(fmt.Sprintf("Device paired: %s via %s (%s)", hello.DeviceName, transportName, remote))
	}
	defer func() {
		s.mu.Lock()
		delete(s.devices, remote)
		s.mu.Unlock()
		s.addLog("Device disconnected: " + hello.DeviceName)
	}()

	welcome := welcomeMessage{
		Type: "welcome", Protocol: protocolVersion, SessionID: newSessionID(),
		ServerName: s.serverName, PosePort: s.config.PosePort,
		Capabilities: []string{"head-pose-3dof-udp-v1", "control-heartbeat-v1", "display-settings-control-v1", "android-optical-presets-v1", "android-media-capabilities-v1", "daydream-controller-ble-v1", "steamvr-dual-controller-v1"},
		VideoEnabled: videoEnabled,
	}
	if usbLink != nil {
		welcome.Capabilities = append(welcome.Capabilities, "usb-aoa-bulk-v1", "usb-multiplex-v1")
	}
	if videoEnabled {
		if usbLink != nil {
			welcome.VideoTransport = "usb"
		} else {
			welcome.VideoTransport = "udp"
		}
		welcome.VideoPort = androidVideoPort
		welcome.VideoCodec = "h264"
		welcome.VideoMimeType = "video/avc"
		welcome.VideoWidth = videoWidth
		welcome.VideoHeight = videoHeight
		welcome.VideoFPS = videoFPS
		welcome.VideoReorderGraceMS = s.videoReorderGraceMilliseconds()
		welcome.VideoDecoderName = selectedAndroidDecoder
		if usbLink != nil {
			welcome.Capabilities = append(welcome.Capabilities, "sbs-h264-usb-v1")
		} else {
			welcome.Capabilities = append(welcome.Capabilities, "sbs-h264-udp-v1")
		}
	} else if !hardwareEncoderReady {
		if encoderError == "" {
			encoderError = "hardware encoder preflight did not complete before the pairing deadline"
		}
		welcome.VideoReason = encoderError
		s.addLog("Video streaming disabled for " + hello.DeviceName + ": " + encoderError)
	} else {
		welcome.VideoReason = fmt.Sprintf("Android needs a hardware H.264 decoder supporting %dx%d@%d", videoWidth, videoHeight, videoFPS)
		s.addLog(fmt.Sprintf("Video streaming disabled for %s: Android did not advertise hardware H.264 %dx%d@%d support", hello.DeviceName, videoWidth, videoHeight, videoFPS))
	}
	if usbLink != nil {
		if payload, marshalErr := json.Marshal(welcome); marshalErr == nil {
			preview := payload
			if len(preview) > 160 {
				preview = preview[:160]
			}
			s.addLog(fmt.Sprintf("USB AOA Welcome prepared: jsonBytes=%d wireBytes=%d preview=%q", len(payload), len(payload)+1, string(preview)))
		}
	}
	if err := writeJSONLineWithDeadline(conn, welcome); err != nil {
		s.addLog("Pair response failed: " + err.Error())
		return
	}
	var controllerBridge *net.UDPConn
	if !hello.VideoOnly {
		bridgeAddress := &net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: steamVRControllerBridgePort}
		bridge, bridgeErr := net.DialUDP("udp4", nil, bridgeAddress)
		if bridgeErr != nil {
			s.addLog("Daydream controller bridge unavailable: " + bridgeErr.Error())
		} else {
			controllerBridge = bridge
			defer controllerBridge.Close()
		}
	}
	s.mu.Lock()
	if current, ok := s.devices[remote]; ok && current.Conn == conn {
		current.Ready = true
		s.devices[remote] = current
	}
	s.mu.Unlock()

	// The auxiliary AOA video session deliberately carries no device-management
	// traffic. The network control session remains authoritative for settings,
	// presets, and user actions.
	presetRequestCount := 1
	presetResponseReceived := hello.VideoOnly
	nextPresetRetry := time.Now().Add(2 * time.Second)
	if !hello.VideoOnly {
		if err := writeControlJSON(device, map[string]any{
			"type": "get_optical_presets", "requestId": newSessionID(),
		}); err != nil {
			s.addLog("Optical preset request failed: " + err.Error())
		} else {
			s.addLog("Requested Android optical preset catalogue")
		}
	}

	lastControllerBridgeLog := time.Time{}
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
			_ = writeJSONLineWithDeadline(conn, map[string]any{"type": "error", "message": "invalid control JSON"})
			continue
		}
		switch message.Type {
		case "ping":
			s.mu.Lock()
			if current, ok := s.devices[remote]; ok && current.Conn == conn {
				current.LastPingAt = time.Now()
				s.devices[remote] = current
			}
			s.mu.Unlock()
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
		case "video_feedback":
			s.updateVideoTransportFeedbackWithRate(hello.DeviceID, message.VideoTransport, message.LastPacketAgeMS, message.LastDecodedAgeMS, message.VideoProbe, message.VideoRXBytesPerSecond)
		case "video_nack":
			s.retransmitMissingVideoFragments(hello.DeviceID, message.VideoFrameID, message.MissingFragments)
		case "controller_state":
			if hello.VideoOnly || message.Controllers == nil || controllerBridge == nil {
				continue
			}
			if time.Since(lastControllerBridgeLog) >= 2*time.Second {
				left, right := message.Controllers.Left, message.Controllers.Right
				s.addLog(fmt.Sprintf(
					"Daydream input bridge: left connected=%t touch=%t xy=%.3f,%.3f; right connected=%t touch=%t xy=%.3f,%.3f",
					left.Connected, left.Touching, left.TouchX, left.TouchY,
					right.Connected, right.Touching, right.TouchX, right.TouchY,
				))
				lastControllerBridgeLog = time.Now()
			}
			leftPacket, leftErr := protocol.EncodeControllerPacket(protocol.ControllerHandLeft, message.Controllers.Left)
			rightPacket, rightErr := protocol.EncodeControllerPacket(protocol.ControllerHandRight, message.Controllers.Right)
			if leftErr != nil || rightErr != nil {
				if leftErr != nil {
					s.addLog("Rejected invalid left Daydream controller state: " + leftErr.Error())
				} else {
					s.addLog("Rejected invalid right Daydream controller state: " + rightErr.Error())
				}
				continue
			}
			if _, err := controllerBridge.Write(leftPacket); err != nil {
				s.addLog("Left Daydream controller bridge write failed: " + err.Error())
			}
			if _, err := controllerBridge.Write(rightPacket); err != nil {
				s.addLog("Right Daydream controller bridge write failed: " + err.Error())
			}
		case "optical_presets":
			if hello.VideoOnly {
				continue
			}
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
			if hello.VideoOnly {
				continue
			}
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
			if hello.VideoOnly {
				continue
			}
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
			if hello.VideoOnly {
				continue
			}
			if message.Status == "applied" {
				s.addLog("Phone action applied: " + message.RequestID)
			} else if message.Status == "rejected" {
				s.addLog("Phone action rejected: " + message.Message)
			}
		case "display_settings_ack":
			if hello.VideoOnly {
				continue
			}
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

		if !hello.VideoOnly && !presetResponseReceived && time.Now().After(nextPresetRetry) {
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
		Name                           string   `json:"name"`
		MimeType                       string   `json:"mimeType"`
		MaxWidth                       int      `json:"maxWidth"`
		MaxHeight                      int      `json:"maxHeight"`
		MaxFrameRate                   float64  `json:"maxFrameRate"`
		HardwareAccelerated            bool     `json:"hardwareAccelerated"`
		HardwareClassificationReliable bool     `json:"hardwareClassificationReliable"`
		SupportsTargetProfile          bool     `json:"supportsSbs1600x720At60"`
		SupportedSBSProfiles           []string `json:"supportedSbsProfiles"`
	}
	if len(raw) == 0 || json.Unmarshal(raw, &entries) != nil {
		return ""
	}
	targetProfile := fmt.Sprintf("%dx%d", width, height)
	for _, item := range entries {
		profileSupported := false
		for _, advertised := range item.SupportedSBSProfiles {
			if strings.TrimSpace(advertised) == targetProfile {
				profileSupported = true
				break
			}
		}
		// Older clients only advertise the original 1600x720 capability bit.
		if width == 1600 && height == 720 && item.SupportsTargetProfile {
			profileSupported = true
		}
		if strings.TrimSpace(item.Name) != "" && strings.EqualFold(item.MimeType, "video/avc") &&
			item.HardwareAccelerated && profileSupported &&
			item.MaxWidth >= width && item.MaxHeight >= height && item.MaxFrameRate >= float64(fps) {
			return item.Name
		}
	}
	return ""
}

func describeH264DecoderCapabilities(raw json.RawMessage) string {
	var entries []struct {
		Name                           string   `json:"name"`
		MimeType                       string   `json:"mimeType"`
		MaxWidth                       int      `json:"maxWidth"`
		MaxHeight                      int      `json:"maxHeight"`
		MaxFrameRate                   float64  `json:"maxFrameRate"`
		HardwareAccelerated            bool     `json:"hardwareAccelerated"`
		HardwareClassificationReliable bool     `json:"hardwareClassificationReliable"`
		SupportsTargetProfile          bool     `json:"supportsSbs1600x720At60"`
		SupportedSBSProfiles           []string `json:"supportedSbsProfiles"`
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
		reports = append(reports, fmt.Sprintf("%s max=%dx%d@%.1f hw=%t hw-classification-reliable=%t supports-1600x720@60=%t profiles=%s",
			item.Name, item.MaxWidth, item.MaxHeight, item.MaxFrameRate,
			item.HardwareAccelerated, item.HardwareClassificationReliable, item.SupportsTargetProfile, strings.Join(item.SupportedSBSProfiles, ",")))
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

var errVideoTransportAwaitingIDR = errors.New("video transport changed; waiting for next IDR")

func (s *HostService) sendEncodedVideoFrame(frame []byte, frameID uint32, timestampNs uint64, keyFrame bool) error {
	target, enabled := s.currentVideoTarget()
	if !enabled {
		return errNoVideoTarget
	}
	if target.Switched && !keyFrame {
		return errVideoTransportAwaitingIDR
	}
	fragments, err := videoprotocol.FragmentFrame(frame, frameID, timestampNs, keyFrame)
	if err != nil {
		return err
	}
	if target.Transport == "parallel" {
		return s.sendVideoFragmentsParallel(fragments, target)
	}
	if target.USB != nil {
		if err := s.sendVideoFragmentsUSB(fragments, target.USB); err != nil {
			s.markVideoTransportFailure(target, err)
			return fmt.Errorf("USB AOA Bulk OUT: %w", err)
		}
		// Keep the access unit for NACK repair in every transport mode. Until
		// 0.5.17 only striped mode retained frames, so a single-path USB client
		// could request missing fragments that the Host no longer had available.
		s.retainVideoFrameForSinglePath(fragments, "usb", target)
		return nil
	}
	if err := s.sendVideoFragmentsUDP(fragments, target.UDP); err != nil {
		s.markVideoTransportFailure(target, err)
		return fmt.Errorf("Android UDP video send: %w", err)
	}
	// Network-only mode must support the same receiver-driven fragment repair
	// as aggregation mode. Cache only successfully transmitted access units.
	s.retainVideoFrameForSinglePath(fragments, "network", target)
	return nil
}

// sendVideoFragmentsParallel stripes distinct fragments of each encoded access
// unit over both active routes. The Android receiver reassembles fragments by
// frame ID/offset and requests missing fragments over the control channel.
func (s *HostService) sendVideoFragmentsParallel(fragments [][]byte, target videoTarget) error {
	if len(fragments) == 0 {
		return nil
	}
	if target.UDP == nil && target.USB == nil {
		return errVideoTransportUnavailable
	}
	if target.UDP == nil {
		if err := s.enqueueUSBFragments(target, fragments, nil, nil); err != nil {
			return err
		}
		s.retainStripedVideoFrame(fragments, repeatVideoPath("usb", len(fragments)), target)
		return nil
	}
	if target.USB == nil {
		if err := s.sendVideoFragmentsUDP(fragments, target.UDP); err != nil {
			route := target
			route.Transport = "network"
			s.markVideoTransportFailure(route, err)
			return fmt.Errorf("network UDP: %w", err)
		}
		s.retainStripedVideoFrame(fragments, repeatVideoPath("network", len(fragments)), target)
		return nil
	}

	usbShare := s.videoStripeUSBSharePermille(target.DeviceID)
	udpFragments, usbFragments, assignments := s.stripeVideoFragments(target.DeviceID, fragments, usbShare)
	udpBytes := videoFragmentBytes(udpFragments)
	usbBytes := videoFragmentBytes(usbFragments)
	s.setVideoTransportAssignedShares(target.DeviceID, udpBytes, usbBytes)
	s.retainStripedVideoFrame(fragments, assignments, target)

	var routeErrors []error
	udpSucceeded := false
	if len(udpFragments) > 0 {
		if err := s.sendVideoFragmentsUDP(udpFragments, target.UDP); err != nil {
			route := target
			route.Transport = "network"
			s.markVideoTransportFailure(route, err)
			routeErrors = append(routeErrors, fmt.Errorf("network UDP: %w", err))
			// The other route receives the complete frame on a route failure.
			// Duplicate PVRV fragments are harmless at the receiver.
			usbFragments = append(usbFragments, udpFragments...)
		} else {
			udpSucceeded = true
		}
	}

	usbQueued := false
	if len(usbFragments) > 0 {
		fallbackParts := append([][]byte(nil), usbFragments...)
		if err := s.enqueueUSBFragments(target, usbFragments, target.UDP, fallbackParts); err != nil {
			route := target
			route.Transport = "usb"
			s.markVideoTransportFailure(route, err)
			routeErrors = append(routeErrors, fmt.Errorf("USB AOA stripe queue: %w", err))
			// If AOA cannot accept this batch, send the exact same assigned
			// fragments through UDP to complete the access unit.
			if err := s.sendVideoFragmentsUDP(usbFragments, target.UDP); err == nil {
				udpSucceeded = true
				s.videoRetransmittedFragments.Add(uint64(len(usbFragments)))
			} else {
				route := target
				route.Transport = "network"
				s.markVideoTransportFailure(route, err)
				routeErrors = append(routeErrors, fmt.Errorf("network fallback: %w", err))
			}
		} else {
			usbQueued = true
		}
	}
	if udpSucceeded || usbQueued {
		return nil
	}
	if len(routeErrors) == 0 {
		return errVideoTransportUnavailable
	}
	return errors.Join(routeErrors...)
}

func (s *HostService) stripeVideoFragments(deviceID string, fragments [][]byte, usbSharePermille int) (network, usb [][]byte, assignments []string) {
	if len(fragments) == 0 {
		return nil, nil, nil
	}
	if usbSharePermille < 1 {
		usbSharePermille = 1
	}
	if usbSharePermille > 999 {
		usbSharePermille = 999
	}
	network = make([][]byte, 0, len(fragments))
	usb = make([][]byte, 0, len(fragments))
	assignments = make([]string, len(fragments))
	// A one-datagram access unit cannot be split. Keep it on the network
	// route so a slower AOA queue cannot hide an entire frame from the receiver.
	if len(fragments) == 1 {
		return append(network, fragments[0]), nil, []string{"network"}
	}
	s.videoStripeMu.Lock()
	if s.videoStripeCredit == nil {
		s.videoStripeCredit = make(map[string]int)
	}
	credit := s.videoStripeCredit[deviceID]
	for index, fragment := range fragments {
		credit += usbSharePermille
		if credit >= 1000 {
			credit -= 1000
			usb = append(usb, fragment)
			assignments[index] = "usb"
		} else {
			network = append(network, fragment)
			assignments[index] = "network"
		}
	}
	// A multi-fragment access unit must retain at least one network fragment.
	// This gives the receiver an early frame marker even when AOA is slower.
	if len(network) == 0 {
		last := usb[len(usb)-1]
		usb = usb[:len(usb)-1]
		network = append(network, last)
		assignments[len(assignments)-1] = "network"
		// The network marker is a per-access-unit safety constraint. Keep the
		// residual credit bounded; small frames may therefore realize a lower
		// USB share than the long-run target when only two fragments are present.
	}
	s.videoStripeCredit[deviceID] = credit
	s.videoStripeMu.Unlock()
	return network, usb, assignments
}

func videoFragmentBytes(fragments [][]byte) int {
	bytes := 0
	for _, fragment := range fragments {
		bytes += len(fragment)
	}
	return bytes
}

func repeatVideoPath(path string, count int) []string {
	paths := make([]string, count)
	for index := range paths {
		paths[index] = path
	}
	return paths
}

func (s *HostService) retainVideoFrameForSinglePath(fragments [][]byte, path string, target videoTarget) {
	if len(fragments) == 0 || (path != "network" && path != "usb") {
		return
	}
	s.retainStripedVideoFrame(fragments, repeatVideoPath(path, len(fragments)), target)
}

func (s *HostService) retainStripedVideoFrame(fragments [][]byte, paths []string, target videoTarget) {
	if len(fragments) == 0 || len(fragments) != len(paths) {
		return
	}
	packet, err := videoprotocol.Decode(fragments[0])
	if err != nil {
		return
	}
	totalBytes := videoFragmentBytes(fragments)
	if totalBytes > videoRetainedFrameMaxBytes {
		return
	}
	entry := retainedVideoFrame{
		fragments: append([][]byte(nil), fragments...), paths: append([]string(nil), paths...),
		target: target, createdAt: time.Now(), totalBytes: totalBytes,
	}
	s.retainedVideoMu.Lock()
	defer s.retainedVideoMu.Unlock()
	s.pruneRetainedVideoFramesLocked(time.Now())
	key := retainedVideoFrameKey{deviceID: target.DeviceID, frameID: packet.FrameID}
	if previous, ok := s.retainedVideoFrames[key]; ok {
		s.retainedVideoBytes -= previous.totalBytes
	}
	s.retainedVideoFrames[key] = entry
	s.retainedVideoBytes += totalBytes
	for len(s.retainedVideoFrames) > videoRetainedFrameLimit || s.retainedVideoBytes > videoRetainedFrameMaxBytes {
		var oldestID retainedVideoFrameKey
		var oldest time.Time
		found := false
		for id, candidate := range s.retainedVideoFrames {
			if !found || candidate.createdAt.Before(oldest) {
				oldestID, oldest, found = id, candidate.createdAt, true
			}
		}
		if !found {
			break
		}
		removed := s.retainedVideoFrames[oldestID]
		delete(s.retainedVideoFrames, oldestID)
		s.retainedVideoBytes -= removed.totalBytes
	}
}

func (s *HostService) pruneRetainedVideoFramesLocked(now time.Time) {
	for key, frame := range s.retainedVideoFrames {
		if now.Sub(frame.createdAt) > videoRetainedFrameTTL {
			delete(s.retainedVideoFrames, key)
			s.retainedVideoBytes -= frame.totalBytes
		}
	}
}

func (s *HostService) retransmitMissingVideoFragments(deviceID string, frameID uint32, indexes []int) {
	if strings.TrimSpace(deviceID) == "" || len(indexes) == 0 || len(indexes) > 256 {
		return
	}
	if !s.acceptVideoNack(deviceID, frameID, indexes) {
		return
	}
	s.retainedVideoMu.Lock()
	s.pruneRetainedVideoFramesLocked(time.Now())
	frame, ok := s.retainedVideoFrames[retainedVideoFrameKey{deviceID: deviceID, frameID: frameID}]
	s.retainedVideoMu.Unlock()
	if !ok {
		return
	}
	target, available := s.currentVideoTarget()
	if !available || target.DeviceID != deviceID {
		target = frame.target
	}
	// A -2 fragment index means the receiver observed a frame-ID gap but no
	// datagram for that frame arrived. Resend the complete retained access unit
	// over the currently faster route so a wholly lost UDP frame can be repaired
	// before the receiver abandons its reorder window and waits for the next IDR.
	requestWholeFrame := false
	for _, index := range indexes {
		if index == -2 {
			requestWholeFrame = true
			break
		}
	}
	if requestWholeFrame {
		parts := append([][]byte(nil), frame.fragments...)
		preferUSB := target.USB != nil && (target.UDP == nil || s.videoStripeUSBSharePermille(deviceID) >= 500)
		if preferUSB {
			if err := s.enqueueUSBFragments(target, parts, target.UDP, parts); err == nil {
				s.videoRetransmittedFragments.Add(uint64(len(parts)))
				return
			} else if target.UDP == nil {
				return
			}
		}
		if target.UDP != nil {
			if err := s.sendVideoFragmentsUDP(parts, target.UDP); err == nil {
				s.videoRetransmittedFragments.Add(uint64(len(parts)))
				return
			} else {
				route := target
				route.Transport = "network"
				s.markVideoTransportFailure(route, err)
			}
		}
		if target.USB != nil {
			if err := s.enqueueUSBFragments(target, parts, nil, nil); err == nil {
				s.videoRetransmittedFragments.Add(uint64(len(parts)))
			}
		}
		return
	}
	var networkParts, usbParts [][]byte
	seen := make(map[int]struct{}, len(indexes))
	for _, index := range indexes {
		if index < 0 || index >= len(frame.fragments) {
			continue
		}
		if _, duplicate := seen[index]; duplicate {
			continue
		}
		seen[index] = struct{}{}
		part := frame.fragments[index]
		original := frame.paths[index]
		if original == "network" && target.USB != nil {
			usbParts = append(usbParts, part)
		} else if original == "usb" && target.UDP != nil {
			networkParts = append(networkParts, part)
		} else if target.UDP != nil {
			networkParts = append(networkParts, part)
		} else if target.USB != nil {
			usbParts = append(usbParts, part)
		}
	}
	if len(networkParts) > 0 && target.UDP != nil {
		if err := s.sendVideoFragmentsUDP(networkParts, target.UDP); err != nil {
			route := target
			route.Transport = "network"
			s.markVideoTransportFailure(route, err)
			if target.USB != nil {
				usbParts = append(usbParts, networkParts...)
			}
		}
	}
	if len(usbParts) > 0 && target.USB != nil {
		if err := s.enqueueUSBFragments(target, usbParts, target.UDP, usbParts); err != nil && target.UDP != nil {
			_ = s.sendVideoFragmentsUDP(usbParts, target.UDP)
		}
	}
	if len(networkParts)+len(usbParts) > 0 {
		s.videoRetransmittedFragments.Add(uint64(len(networkParts) + len(usbParts)))
	}
}

func (s *HostService) acceptVideoNack(deviceID string, frameID uint32, indexes []int) bool {
	key := fmt.Sprintf("%s|%d|%v", deviceID, frameID, indexes)
	now := time.Now()
	s.videoNackMu.Lock()
	defer s.videoNackMu.Unlock()
	if s.videoNackSeen == nil {
		s.videoNackSeen = make(map[string]time.Time)
	}
	for previousKey, seenAt := range s.videoNackSeen {
		if now.Sub(seenAt) > 500*time.Millisecond {
			delete(s.videoNackSeen, previousKey)
		}
	}
	if previous, ok := s.videoNackSeen[key]; ok && now.Sub(previous) < 15*time.Millisecond {
		return false
	}
	s.videoNackSeen[key] = now
	return true
}

func (s *HostService) enqueueUSBFragments(target videoTarget, fragments [][]byte, fallbackUDP *net.UDPAddr, fallbackParts [][]byte) error {
	if target.USB == nil || len(fragments) == 0 {
		return errors.New("USB AOA stripe route is unavailable")
	}
	work := usbVideoWork{
		link: target.USB, deviceID: target.DeviceID,
		fragments: fragments, fallbackUDP: fallbackUDP,
		fallbackParts: fallbackParts, hasOtherPath: fallbackUDP != nil,
	}
	return s.enqueueUSBVideoWork(work)
}

func (s *HostService) enqueueUSBVideoWork(work usbVideoWork) error {
	s.mu.RLock()
	queue := s.videoUSBQueue
	s.mu.RUnlock()
	if queue == nil {
		return errors.New("USB AOA video worker is not running")
	}
	select {
	case queue <- work:
		return nil
	default:
		return errors.New("USB AOA video queue is full; network path remains preferred")
	}
}

func (s *HostService) videoUSBWorkSendLoop(ctx context.Context, queue <-chan usbVideoWork) {
	defer s.wg.Done()
	for {
		select {
		case <-ctx.Done():
			return
		case work := <-queue:
			if work.link == nil {
				continue
			}
			var err error
			if work.isProbe {
				if len(work.fragments) != 1 {
					err = errors.New("invalid USB video-path probe work item")
				} else {
					err = work.link.WriteFrame(usbmux.ChannelVideo, work.fragments[0])
				}
			} else {
				err = s.sendVideoFragmentsUSB(work.fragments, work.link)
			}
			if err == nil {
				continue
			}
			route := videoTarget{Transport: "usb", DeviceID: work.deviceID}
			s.markVideoTransportFailure(route, err)
			fallbackSent := false
			if !work.isProbe && work.fallbackUDP != nil && len(work.fallbackParts) > 0 {
				if fallbackErr := s.sendVideoFragmentsUDP(work.fallbackParts, work.fallbackUDP); fallbackErr == nil {
					fallbackSent = true
					s.videoRetransmittedFragments.Add(uint64(len(work.fallbackParts)))
				} else {
					s.addLog("USB AOA stripe fallback over UDP failed: " + fallbackErr.Error())
				}
			}
			if !work.isProbe && !fallbackSent && !work.hasOtherPath {
				s.videoDroppedFrames.Add(1)
				dropped := s.requireVideoKeyFrame()
				s.addLog(fmt.Sprintf("USB AOA video send failed without an alternate route: %v; dropped %d queued access units and waiting for next IDR", err, dropped))
			} else {
				s.addLog(fmt.Sprintf("USB AOA striped path send failed; alternate path or fragment repair will recover it: %v", err))
			}
		}
	}
}

func (s *HostService) sendVideoFragmentsUSB(fragments [][]byte, link *usbmux.Mux) error {
	if link == nil {
		return errors.New("USB AOA Bulk link is unavailable")
	}
	for _, packet := range fragments {
		if err := link.WriteFrame(usbmux.ChannelVideo, packet); err != nil {
			// A failed/partial multiplex write invalidates byte-stream framing.
			_ = link.Close()
			return err
		}
		s.videoUDPFragments.Add(1)
	}
	return nil
}

func (s *HostService) sendVideoFragmentsUDP(fragments [][]byte, target *net.UDPAddr) error {
	if target == nil {
		return errors.New("network UDP target is unavailable")
	}
	s.mu.RLock()
	udp := s.videoUDP
	s.mu.RUnlock()
	if udp == nil {
		return errors.New("video UDP sender is unavailable")
	}
	for _, packet := range fragments {
		if _, err := udp.WriteToUDP(packet, target); err != nil {
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
				s.addLog("SteamVR video bridge connection deferred: no paired Android client currently has a healthy negotiated H.264 transport route")
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
	width, height, _ := s.activeVideoDimensions()
	if frame, ok := s.videoFramePool.Get().(*rawVideoFrame); ok && frame != nil && len(frame.data) == width*height*4 {
		return frame
	}
	return &rawVideoFrame{data: make([]byte, width*height*4)}
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
	videoWidth, videoHeight, _ := s.activeVideoDimensions()
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
			if !s.videoProfileMismatchLogged.Swap(true) {
				s.addLog(fmt.Sprintf("SteamVR video profile mismatch: driver sends %dx%d stride=%d, Host is active at %dx%d BGRA. Mismatched frames are being discarded. Exit SteamVR, restart the Host service so it publishes the selected active profile, then launch SteamVR again.", header.Width, header.Height, header.Stride, videoWidth, videoHeight))
			}
			// Keep draining complete mismatched frames. Closing immediately made
			// the driver reconnect for every compositor frame and flooded logs.
			if _, err := io.CopyN(io.Discard, conn, int64(header.PayloadSize)); err != nil {
				if !s.videoProfileMismatchLogged.Swap(true) {
					s.addLog("Could not drain mismatched SteamVR raw frame: " + err.Error())
				}
				return
			}
			continue
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
		rawCount := s.videoRawFrames.Add(1)
		if rawCount <= 3 {
			s.addLog(describeRawBGRAFrame(frame.data, rawCount))
		}
		s.enqueueRawVideoFrame(frame)
	}
}

// describeRawBGRAFrame samples a small subset of the first raw frames. It
// confirms whether the driver is delivering varied pixels before FFmpeg; it
// deliberately avoids scanning the entire 4.6 MiB frame on every capture.
func describeRawBGRAFrame(data []byte, frameNumber uint64) string {
	if len(data) == 0 {
		return fmt.Sprintf("SteamVR raw frame %d is empty", frameNumber)
	}
	var minR, minG, minB = uint8(255), uint8(255), uint8(255)
	var maxR, maxG, maxB uint8
	var sumR, sumG, sumB uint64
	var samples uint64
	const sampleStride = 64 * 1024
	for offset := 0; offset+3 < len(data); offset += sampleStride {
		b, g, r := data[offset], data[offset+1], data[offset+2]
		if r < minR {
			minR = r
		}
		if r > maxR {
			maxR = r
		}
		if g < minG {
			minG = g
		}
		if g > maxG {
			maxG = g
		}
		if b < minB {
			minB = b
		}
		if b > maxB {
			maxB = b
		}
		sumR += uint64(r)
		sumG += uint64(g)
		sumB += uint64(b)
		samples++
	}
	if samples == 0 {
		return fmt.Sprintf("SteamVR raw frame %d has no sampleable pixels", frameNumber)
	}
	return fmt.Sprintf("SteamVR raw frame %d BGRA sample: R=%d..%d avg=%.1f G=%d..%d avg=%.1f B=%d..%d avg=%.1f samples=%d",
		frameNumber, minR, maxR, float64(sumR)/float64(samples), minG, maxG, float64(sumG)/float64(samples),
		minB, maxB, float64(sumB)/float64(samples), samples)
}

func minDuration(a, b time.Duration) time.Duration {
	if a < b {
		return a
	}
	return b
}

// videoEncoderLoop owns exactly one FFmpeg process for the Host service
// lifetime. It is intentionally independent of the driver's TCP connection:
// brief loopback reconnects can no longer spawn and destroy FFmpeg repeatedly.
func (s *HostService) videoEncoderLoop(ctx context.Context) {
	defer s.wg.Done()
	videoWidth, videoHeight, videoFPS := s.activeVideoDimensions()
	var session *encoder.H264EncoderSession
	var retryAfter time.Time
	retryDelay := 2 * time.Second
	var sessionStartedAt time.Time
	var lastBackpressureLog time.Time
	candidateIndex := 0
	advanceEncoderCandidate := func(failedName, reason string) {
		s.mu.RLock()
		candidateCount := len(s.videoEncoderCandidates)
		var next encoder.Capability
		if candidateIndex+1 < candidateCount {
			next = s.videoEncoderCandidates[candidateIndex+1]
		}
		s.mu.RUnlock()
		if next.Name != "" {
			candidateIndex++
			s.addLog(fmt.Sprintf("H.264 backend %s failed (%s); switching to next prevalidated hardware encoder %s/%s", failedName, reason, next.Name, next.Backend))
			retryAfter = time.Time{}
			return
		}
		candidateIndex = 0
		s.addLog(fmt.Sprintf("H.264 backend %s failed (%s); no alternate prevalidated hardware encoder is available, retrying primary after %s", failedName, reason, retryDelay))
		retryAfter = time.Now().Add(retryDelay)
		retryDelay = minDuration(retryDelay*2, 8*time.Second)
	}
	var framesInWindow uint64
	statsStart := time.Now()
	rawAtWindowStart := s.videoRawFrames.Load()
	encodedAtWindowStart := s.videoEncodedFrames.Load()
	udpAtWindowStart := s.videoUDPFragments.Load()
	droppedAtWindowStart := s.videoDroppedFrames.Load()
	inputDropsAtWindowStart := s.videoInputQueueDrops.Load()
	outputDropsAtWindowStart := s.videoOutputQueueDrops.Load()
	logPipelineStats := func(active *encoder.H264EncoderSession) {
		elapsed := time.Since(statsStart)
		if elapsed < 5*time.Second {
			return
		}
		seconds := maxDurationSeconds(elapsed)
		encodedNow := s.videoEncodedFrames.Load()
		udpNow := s.videoUDPFragments.Load()
		droppedNow := s.videoDroppedFrames.Load()
		inputDropsNow := s.videoInputQueueDrops.Load()
		outputDropsNow := s.videoOutputQueueDrops.Load()
		rawNow := s.videoRawFrames.Load()
		sendMaxMs := float64(s.videoTransportSendMaxNs.Swap(0)) / float64(time.Millisecond)
		name, pending, outputBytes, outputUnits := "stopped", 0, uint64(0), uint64(0)
		if active != nil {
			name, pending, outputBytes, outputUnits = active.EncoderName(), active.PendingFrames(), active.OutputBytes(), active.OutputAccessUnits()
		}
		s.addLog(fmt.Sprintf("Video pipeline: raw RX %.1f FPS | submitted %.1f FPS | H.264 %.1f FPS | transport fragments %d | capture-queue drops %d | output-queue drops %d | encoder/transport drops %d | send max %.1f ms | FFmpeg stdout %d bytes | H.264 access units %d | pending=%d | backend %s",
			float64(rawNow-rawAtWindowStart)/seconds,
			float64(framesInWindow)/seconds,
			float64(encodedNow-encodedAtWindowStart)/seconds,
			udpNow-udpAtWindowStart, inputDropsNow-inputDropsAtWindowStart, outputDropsNow-outputDropsAtWindowStart,
			droppedNow-droppedAtWindowStart, sendMaxMs, outputBytes, outputUnits, pending, name))
		framesInWindow = 0
		rawAtWindowStart = rawNow
		encodedAtWindowStart = encodedNow
		udpAtWindowStart = udpNow
		droppedAtWindowStart = droppedNow
		inputDropsAtWindowStart = inputDropsNow
		outputDropsAtWindowStart = outputDropsNow
		statsStart = time.Now()
	}
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
				candidates := append([]encoder.Capability(nil), s.videoEncoderCandidates...)
				selectedFallback := s.videoEncoderSelected
				ready := s.videoEncoderReady
				s.mu.RUnlock()
				if len(candidates) == 0 && selectedFallback.Name != "" {
					candidates = []encoder.Capability{selectedFallback}
				}
				if !ready || len(candidates) == 0 {
					s.releaseRawVideoFrame(frame)
					retryAfter = time.Now().Add(5 * time.Second)
					continue
				}
				if candidateIndex >= len(candidates) {
					candidateIndex = 0
				}
				selected := candidates[candidateIndex]
				created, createErr := encoder.StartH264EncoderWithCapabilityAndBitrate(ctx, ffmpegPath, videoWidth, videoHeight, videoFPS, s.activeVideoProfile().BitrateMbps, selected,
					func(accessUnit []byte, id uint32, timestamp uint64, key bool) error {
						s.videoEncodedFrames.Add(1)
						// Never perform transport I/O on FFmpeg's stdout reader goroutine.
						// A blocked WinUSB write would stop stdout draining and look like
						// an encoder stall. The bounded queue drops stale access units.
						s.enqueueEncodedVideoFrame(encodedVideoFrame{
							data: accessUnit, frameID: id, timestampNs: timestamp, keyFrame: key,
						})
						return nil
					}, s.addLog)
				if createErr != nil {
					s.addLog(fmt.Sprintf("Persistent H.264 encoder startup failed for %s/%s: %s", selected.Name, selected.Backend, createErr.Error()))
					advanceEncoderCandidate(selected.Name+"/"+string(selected.Backend), "startup failed")
					s.releaseRawVideoFrame(frame)
					continue
				}
				session = created
				sessionStartedAt = time.Now()
				s.addLog("Persistent live VR hardware H.264 encoder started: " + session.EncoderName() + fmt.Sprintf(" at %dx%d@%d, %d Mbps", videoWidth, videoHeight, videoFPS, s.activeVideoProfile().BitrateMbps))
			}

			if err := session.WriteFrame(frame.data, frame.timestampNs); err != nil {
				s.videoDroppedFrames.Add(1)
				s.releaseRawVideoFrame(frame)
				if errors.Is(err, encoder.ErrH264Backpressure) {
					// Drop input while the encoder catches up. Recycle only when the
					// pending queue is backed up and both stdout and completed access
					// units have been idle beyond the startup/runtime grace period.
					stallTimeout := encoder.H264OutputStallTimeout
					if session.OutputAccessUnits() == 0 {
						stallTimeout = encoder.H264StartupOutputStallTimeout
					}
					if session.OutputStalled(stallTimeout) {
						stderr := session.StderrTail()
						stallErr := fmt.Sprintf("H.264 encoder stalled (timeout=%s, last-output-age=%s, last-access-unit-age=%s, pending=%d, input=%d, stdout=%d bytes, output access units=%d, stderr=%q)",
							stallTimeout, session.LastOutputAge().Round(time.Millisecond), session.LastAccessUnitAge().Round(time.Millisecond),
							session.PendingFrames(), session.InputFrames(), session.OutputBytes(), session.OutputAccessUnits(), stderr)
						failedName := session.EncoderName()
						// Once the encoder has delivered a meaningful run of frames, use
						// a short recovery delay instead of carrying startup backoff into
						// later failures. This prevents 60-second blackouts after one QSV stall.
						if session.OutputAccessUnits() >= 30 || time.Since(sessionStartedAt) >= 10*time.Second {
							retryDelay = 2 * time.Second
						}
						s.addLog(stallErr + "; closing failed session")
						session.Close()
						session = nil
						advanceEncoderCandidate(failedName, "no encoded access units")
					} else if time.Since(lastBackpressureLog) >= 5*time.Second {
						lastBackpressureLog = time.Now()
						s.addLog(fmt.Sprintf("H.264 encoder backpressure: keeping %s alive and dropping raw frames (pending=%d, input=%d, stdout=%d bytes, output access units=%d); no encoder restart",
							session.EncoderName(), session.PendingFrames(), session.InputFrames(), session.OutputBytes(), session.OutputAccessUnits()))
					}
					logPipelineStats(session)
					continue
				}
				failedName := session.EncoderName()
				stderr := session.StderrTail()
				s.addLog(fmt.Sprintf("Persistent H.264 encoder process %s failed: %v; stderr=%q", failedName, err, stderr))
				session.Close()
				session = nil
				if time.Since(sessionStartedAt) > 10*time.Second {
					retryDelay = 2 * time.Second
				}
				advanceEncoderCandidate(failedName, err.Error())
				continue
			}
			s.releaseRawVideoFrame(frame)
			framesInWindow++
			if session.OutputAccessUnits() >= 30 && time.Since(sessionStartedAt) >= 5*time.Second {
				retryDelay = 2 * time.Second
			}
			logPipelineStats(session)
		}
	}
}

// enqueueEncodedVideoFrame is non-blocking because it runs on FFmpeg's stdout
// reader goroutine. Transport I/O is performed by videoTransportSendLoop.
// If the sender falls behind, stale access units are purged and delta frames
// are dropped until a fresh IDR can re-establish decoder state.
func (s *HostService) enqueueEncodedVideoFrame(frame encodedVideoFrame) {
	s.videoOutputMu.Lock()
	defer s.videoOutputMu.Unlock()
	queue := s.videoOutputQueue
	if queue == nil {
		s.videoOutputQueueDrops.Add(1)
		return
	}
	if s.videoOutputNeedsKeyFrame && !frame.keyFrame {
		s.videoOutputQueueDrops.Add(1)
		return
	}
	select {
	case queue <- frame:
		if frame.keyFrame {
			s.videoOutputNeedsKeyFrame = false
		}
		return
	default:
	}

	// Queue full: discard old queued frames. The currently transmitted frame,
	// if any, completes independently and the next queued frame must be IDR.
	dropped := 0
	for {
		select {
		case <-queue:
			dropped++
		default:
			droppedFrames := dropped
			if !frame.keyFrame {
				droppedFrames++
			}
			s.videoOutputQueueDrops.Add(uint64(droppedFrames))
			s.videoOutputNeedsKeyFrame = true
			if frame.keyFrame {
				queue <- frame
				s.videoOutputNeedsKeyFrame = false
			}
			return
		}
	}
}

// requireVideoKeyFrame drops queued delta frames after a transport write error.
func (s *HostService) requireVideoKeyFrame() int {
	s.videoOutputMu.Lock()
	defer s.videoOutputMu.Unlock()
	s.videoOutputNeedsKeyFrame = true
	dropped := 0
	for s.videoOutputQueue != nil {
		select {
		case <-s.videoOutputQueue:
			dropped++
		default:
			s.videoOutputQueueDrops.Add(uint64(dropped))
			return dropped
		}
	}
	s.videoOutputQueueDrops.Add(uint64(dropped))
	return dropped
}

func (s *HostService) videoTransportSendLoop(ctx context.Context) {
	defer s.wg.Done()
	probeTicker := time.NewTicker(time.Second)
	defer probeTicker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-probeTicker.C:
			s.probeStandbyVideoPaths()
		case frame := <-s.videoOutputQueue:
			started := time.Now()
			err := s.sendEncodedVideoFrame(frame.data, frame.frameID, frame.timestampNs, frame.keyFrame)
			elapsed := time.Since(started).Nanoseconds()
			for previous := s.videoTransportSendMaxNs.Load(); elapsed > previous; previous = s.videoTransportSendMaxNs.Load() {
				if s.videoTransportSendMaxNs.CompareAndSwap(previous, elapsed) {
					break
				}
			}
			if err != nil {
				s.videoDroppedFrames.Add(1)
				dropped := s.requireVideoKeyFrame()
				if !errors.Is(err, errNoVideoTarget) && !errors.Is(err, errVideoTransportAwaitingIDR) {
					s.addLog(fmt.Sprintf("Video transport send failed after %.1f ms: %v; dropped %d queued access units and waiting for next IDR", float64(elapsed)/float64(time.Millisecond), err, dropped))
				} else if errors.Is(err, errVideoTransportAwaitingIDR) {
					s.addLog("Video transport changed; dropped delta frames and waiting for the next IDR")
				}
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

func controlHandshakeTimeoutFor(conn net.Conn) time.Duration {
	if conn != nil {
		if remote := conn.RemoteAddr(); remote != nil && remote.Network() == "usb" {
			// Android may display an accessory permission prompt after AOA
			// re-enumeration. Give the user enough time to approve it; the
			// LAN handshake remains short to reject stale network clients.
			return usbControlHandshakeTimeout
		}
	}
	return controlHandshakeTimeout
}

func writeJSONLineWithDeadline(conn net.Conn, value any) error {
	if conn == nil {
		return net.ErrClosed
	}
	_ = conn.SetWriteDeadline(time.Now().Add(controlWriteTimeout))
	err := writeJSONLine(conn, value)
	// Do not leave an absolute write deadline behind. The next control write
	// establishes its own deadline, independent of the handshake wait time.
	_ = conn.SetWriteDeadline(time.Time{})
	return err
}

func writeJSONLine(conn net.Conn, value any) error { return json.NewEncoder(conn).Encode(value) }

func newSessionID() string {
	bytes := make([]byte, 12)
	if _, err := rand.Read(bytes); err != nil {
		return fmt.Sprintf("%d", time.Now().UnixNano())
	}
	return hex.EncodeToString(bytes)
}
