package main

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/vsq-studio/phonevr-host/internal/usbmux"
	"github.com/vsq-studio/phonevr-host/internal/videoprotocol"
)

type videoMuxTestStream struct {
	mu     sync.Mutex
	writes bytes.Buffer
	closed chan struct{}
	once   sync.Once
}

func newVideoMuxTestStream() *videoMuxTestStream {
	return &videoMuxTestStream{closed: make(chan struct{})}
}

func (s *videoMuxTestStream) Read([]byte) (int, error) {
	<-s.closed
	return 0, io.EOF
}

func (s *videoMuxTestStream) Write(data []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.writes.Write(data)
}

func (s *videoMuxTestStream) Close() error {
	s.once.Do(func() { close(s.closed) })
	return nil
}

func (s *videoMuxTestStream) Bytes() []byte {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]byte(nil), s.writes.Bytes()...)
}

type videoTransportTestConn struct {
	net.Conn
	remote net.Addr
}

func (c videoTransportTestConn) RemoteAddr() net.Addr { return c.remote }

func makeVideoTransportTestConn(t *testing.T, remote net.Addr) net.Conn {
	t.Helper()
	left, right := net.Pipe()
	t.Cleanup(func() {
		_ = left.Close()
		_ = right.Close()
	})
	return videoTransportTestConn{Conn: left, remote: remote}
}

func addVideoTransportTestDevice(t *testing.T, s *HostService, deviceID, key, transport string) {
	device := activeDevice{
		Snapshot: DeviceSnapshot{
			Name:        "PhoneVR test device",
			DeviceID:    deviceID,
			ConnectedAt: time.Now().Format(time.RFC3339),
			Transport:   transport,
		},
		Ready:        true,
		VideoEnabled: true,
		LastPingAt:   time.Now(),
		WriteMu:      &sync.Mutex{},
	}
	if transport == "USB AOA Bulk" {
		device.Conn = makeVideoTransportTestConn(t, testTransportAddr("USB-AOA:test"))
		device.USBLink = &usbmux.Mux{}
	} else {
		device.Conn = makeVideoTransportTestConn(t, &net.TCPAddr{IP: net.ParseIP("192.0.2.10"), Port: 48101})
	}
	s.devices[key] = device
}

type testTransportAddr string

func (a testTransportAddr) Network() string { return "usb" }
func (a testTransportAddr) String() string  { return string(a) }

func TestAutoVideoTransportPrefersUSBAndFallsBackToNetwork(t *testing.T) {
	service := NewHostService(false)
	addVideoTransportTestDevice(t, service, "phone-1", "USB-AOA:test", "USB AOA Bulk")
	addVideoTransportTestDevice(t, service, "phone-1", "192.0.2.10:50000", "Wi-Fi / LAN")
	if err := service.SetVideoTransport(string(VideoTransportAuto), string(VideoPreferUSB)); err != nil {
		t.Fatal(err)
	}
	service.updateVideoTransportFeedback("phone-1", "usb", -1, -1, true)

	target, ok := service.currentVideoTarget()
	if !ok || target.Transport != "usb" || target.USB == nil {
		t.Fatalf("initial auto target = %+v, enabled=%t; want USB AOA", target, ok)
	}

	service.updateVideoTransportFeedback("phone-1", "network", -1, -1, true)
	service.markVideoTransportFailure(target, errors.New("simulated USB write failure"))
	target, ok = service.currentVideoTarget()
	if !ok || target.Transport != "network" || target.UDP == nil {
		t.Fatalf("fallback target = %+v, enabled=%t; want network UDP", target, ok)
	}
	if target.UDP.IP.String() != "192.0.2.10" || target.UDP.Port != androidVideoPort {
		t.Fatalf("network target address = %v; want 192.0.2.10:%d", target.UDP, androidVideoPort)
	}
}

func TestEmptyVideoTransportConfigDefaultsToNetworkOnly(t *testing.T) {
	mode, preferred := loadVideoTransportConfigFrom(filepath.Join(t.TempDir(), "missing.json"))
	if mode != VideoTransportNetwork || preferred != VideoPreferNetwork {
		t.Fatalf("empty transport config = %q/%q; want network/network", mode, preferred)
	}
	if normalizeVideoTransportMode("") != VideoTransportNetwork {
		t.Fatal("empty transport mode should default to network-only")
	}
}

func TestVideoTransportConfigPersistsAndReloads(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings", "video-transport.json")
	service := NewHostService(false)
	service.transportConfigPath = path
	if err := service.SetVideoTransport(string(VideoTransportParallel), string(VideoPreferUSB)); err != nil {
		t.Fatalf("save video transport settings: %v", err)
	}
	mode, preferred := loadVideoTransportConfigFrom(path)
	if mode != VideoTransportParallel || preferred != VideoPreferUSB {
		t.Fatalf("reloaded transport settings = %q/%q; want parallel/usb", mode, preferred)
	}
}

func TestParallelVideoTransportMirrorsIdenticalFragmentsToUDPAndUSB(t *testing.T) {
	udpReceiver, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatal(err)
	}
	defer udpReceiver.Close()
	if err := udpReceiver.SetReadDeadline(time.Now().Add(2 * time.Second)); err != nil {
		t.Fatal(err)
	}

	udpSender, err := net.ListenUDP("udp", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer udpSender.Close()
	service := NewHostService(false)
	service.videoUDP = udpSender
	service.videoUSBQueue = make(chan usbMirrorWork, videoUSBQueueDepth)
	workerCtx, stopWorker := context.WithCancel(context.Background())
	service.wg.Add(1)
	go service.videoUSBMirrorSendLoop(workerCtx, service.videoUSBQueue)
	t.Cleanup(func() {
		stopWorker()
		service.wg.Wait()
	})

	stream := newVideoMuxTestStream()
	usb := usbmux.New(stream)
	defer usb.Close()
	frame := bytes.Repeat([]byte{0x65, 0x88, 0x84, 0x21}, 900)
	fragments, err := videoprotocol.FragmentFrame(frame, 1234, 987654321, true)
	if err != nil {
		t.Fatal(err)
	}
	target := videoTarget{
		USB: usb, UDP: udpReceiver.LocalAddr().(*net.UDPAddr),
		Transport: "parallel", DeviceID: "phone-parallel-test",
	}
	if err := service.sendVideoFragmentsParallel(fragments, target); err != nil {
		t.Fatalf("mirror video fragments: %v", err)
	}

	udpPackets := make([][]byte, 0, len(fragments))
	for range fragments {
		buffer := make([]byte, videoprotocol.MaxDatagramSize)
		n, _, err := udpReceiver.ReadFromUDP(buffer)
		if err != nil {
			t.Fatalf("read mirrored UDP fragment: %v", err)
		}
		udpPackets = append(udpPackets, append([]byte(nil), buffer[:n]...))
	}
	expectedUSBBytes := 0
	for _, fragment := range fragments {
		expectedUSBBytes += usbmux.HeaderSize + len(fragment)
	}
	deadline := time.Now().Add(2 * time.Second)
	for len(stream.Bytes()) < expectedUSBBytes && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	usbData := stream.Bytes()
	if len(usbData) != expectedUSBBytes {
		t.Fatalf("USB mirrored wire bytes = %d, want %d", len(usbData), expectedUSBBytes)
	}
	usbReader := bytes.NewReader(usbData)
	usbPackets := make([][]byte, 0, len(fragments))
	for {
		packet, err := usbmux.ReadFrame(usbReader)
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			t.Fatalf("decode USB mux frame: %v", err)
		}
		if packet.Channel != usbmux.ChannelVideo {
			t.Fatalf("USB channel = %d, want video channel", packet.Channel)
		}
		usbPackets = append(usbPackets, packet.Payload)
	}
	if len(udpPackets) != len(fragments) || len(usbPackets) != len(fragments) {
		t.Fatalf("fragment counts: expected=%d UDP=%d USB=%d", len(fragments), len(udpPackets), len(usbPackets))
	}
	for i, fragment := range fragments {
		if !bytes.Equal(udpPackets[i], fragment) || !bytes.Equal(usbPackets[i], fragment) {
			t.Fatalf("fragment %d differs between source, UDP and USB copies", i)
		}
	}
}

func TestParallelVideoTransportKeepsUDPWhenUSBQueueIsFull(t *testing.T) {
	udpReceiver, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatal(err)
	}
	defer udpReceiver.Close()
	if err := udpReceiver.SetReadDeadline(time.Now().Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	udpSender, err := net.ListenUDP("udp", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer udpSender.Close()

	service := NewHostService(false)
	service.videoUDP = udpSender
	service.videoUSBQueue = make(chan usbMirrorWork, 1)
	service.videoUSBQueue <- usbMirrorWork{} // occupy the only queue slot
	fragment := []byte("PVRV-test-fragment")
	target := videoTarget{
		USB: &usbmux.Mux{}, UDP: udpReceiver.LocalAddr().(*net.UDPAddr),
		Transport: "parallel", DeviceID: "phone-queue-full-test",
	}
	if err := service.sendVideoFragmentsParallel([][]byte{fragment}, target); err != nil {
		t.Fatalf("USB queue congestion should not fail a successful UDP delivery: %v", err)
	}
	buffer := make([]byte, 128)
	n, _, err := udpReceiver.ReadFromUDP(buffer)
	if err != nil {
		t.Fatalf("read surviving UDP copy: %v", err)
	}
	if !bytes.Equal(buffer[:n], fragment) {
		t.Fatalf("UDP packet = %q, want %q", buffer[:n], fragment)
	}
	service.mu.RLock()
	health := service.transportHealth["phone-queue-full-test|usb"]
	service.mu.RUnlock()
	if health.UnhealthyUntil.IsZero() {
		t.Fatal("USB route should be marked unhealthy when its mirror queue is full")
	}
}

func TestParallelVideoTransportSelectsBothRoutes(t *testing.T) {
	service := NewHostService(false)
	service.transportConfigPath = ""
	addVideoTransportTestDevice(t, service, "phone-1", "USB-AOA:test", "USB AOA Bulk")
	addVideoTransportTestDevice(t, service, "phone-1", "192.0.2.10:50000", "Wi-Fi / LAN")
	if err := service.SetVideoTransport(string(VideoTransportParallel), string(VideoPreferNetwork)); err != nil {
		t.Fatal(err)
	}
	if target, ok := service.currentVideoTarget(); ok {
		t.Fatalf("parallel target became active before video-path probes: %+v", target)
	}
	service.updateVideoTransportFeedback("phone-1", "network", -1, -1, true)
	target, ok := service.currentVideoTarget()
	if !ok || target.Transport != "parallel" || target.UDP == nil || target.USB != nil {
		t.Fatalf("parallel target before USB probe = %+v, enabled=%t; want network only", target, ok)
	}
	service.updateVideoTransportFeedback("phone-1", "usb", -1, -1, true)
	target, ok = service.currentVideoTarget()
	if !ok || target.Transport != "parallel" || target.USB == nil || target.UDP == nil {
		t.Fatalf("parallel target = %+v, enabled=%t; want both paths after independent probe ACKs", target, ok)
	}
	service.updateVideoTransportFeedback("phone-1", "usb", -1, -1, true)
	service.updateVideoTransportFeedback("phone-1", "network", -1, -1, true)
	target, ok = service.currentVideoTarget()
	if !ok || target.USB == nil || target.UDP == nil {
		t.Fatalf("parallel target after probe ACKs = %+v, enabled=%t; want both paths", target, ok)
	}
}

func TestManualVideoTransportModesDoNotUseTheOtherPath(t *testing.T) {
	service := NewHostService(false)
	addVideoTransportTestDevice(t, service, "phone-1", "USB-AOA:test", "USB AOA Bulk")
	addVideoTransportTestDevice(t, service, "phone-1", "192.0.2.10:50000", "Wi-Fi / LAN")

	if err := service.SetVideoTransport(string(VideoTransportNetwork), string(VideoPreferUSB)); err != nil {
		t.Fatal(err)
	}
	service.updateVideoTransportFeedback("phone-1", "network", -1, -1, true)
	target, ok := service.currentVideoTarget()
	if !ok || target.Transport != "network" {
		t.Fatalf("network-only target = %+v, enabled=%t", target, ok)
	}

	if err := service.SetVideoTransport(string(VideoTransportUSB), string(VideoPreferNetwork)); err != nil {
		t.Fatal(err)
	}
	service.updateVideoTransportFeedback("phone-1", "usb", -1, -1, true)
	target, ok = service.currentVideoTarget()
	if !ok || target.Transport != "usb" {
		t.Fatalf("USB-only target = %+v, enabled=%t", target, ok)
	}
}

func TestActiveVideoTransportRequiresRecentEndToEndFeedback(t *testing.T) {
	service := NewHostService(false)
	if err := service.SetVideoTransport(string(VideoTransportAuto), string(VideoPreferNetwork)); err != nil {
		t.Fatal(err)
	}
	addVideoTransportTestDevice(t, service, "phone-1", "USB-AOA:test", "USB AOA Bulk")
	addVideoTransportTestDevice(t, service, "phone-1", "192.0.2.10:50000", "Wi-Fi / LAN")
	service.transportPreference = VideoPreferNetwork
	service.updateVideoTransportFeedback("phone-1", "network", -1, -1, true)

	target, ok := service.currentVideoTarget()
	if !ok || target.Transport != "network" {
		t.Fatalf("initial target = %+v, enabled=%t; want network", target, ok)
	}
	service.mu.Lock()
	service.transportHealth["phone-1|usb"] = transportHealth{ProbeAt: time.Now()}
	service.activeVideoSince = time.Now().Add(-5 * time.Second)
	service.transportHealth["phone-1|network"] = transportHealth{
		FeedbackAt: time.Now().Add(-5 * time.Second), LastPacketAgeMS: 5000,
	}
	service.mu.Unlock()

	target, ok = service.currentVideoTarget()
	if !ok || target.Transport != "usb" {
		t.Fatalf("silent network-loss fallback = %+v, enabled=%t; want USB AOA", target, ok)
	}
}

func TestStandbyVideoPathRequiresRecentProbeAcknowledgement(t *testing.T) {
	service := NewHostService(false)
	if err := service.SetVideoTransport(string(VideoTransportAuto), string(VideoPreferNetwork)); err != nil {
		t.Fatal(err)
	}
	addVideoTransportTestDevice(t, service, "phone-1", "USB-AOA:test", "USB AOA Bulk")
	addVideoTransportTestDevice(t, service, "phone-1", "192.0.2.10:50000", "Wi-Fi / LAN")
	service.transportPreference = VideoPreferNetwork
	service.updateVideoTransportFeedback("phone-1", "network", -1, -1, true)
	target, ok := service.currentVideoTarget()
	if !ok || target.Transport != "network" {
		t.Fatalf("initial target = %+v, enabled=%t; want network", target, ok)
	}
	service.mu.Lock()
	service.activeVideoSince = time.Now().Add(-5 * time.Second)
	service.transportHealth["phone-1|network"] = transportHealth{FeedbackAt: time.Now().Add(-5 * time.Second), LastPacketAgeMS: 5000}
	service.mu.Unlock()

	if usb, found := service.transportCandidateGroupForTest("phone-1", "usb"); !found {
		t.Fatal("USB standby candidate missing")
	} else {
		service.mu.Lock()
		healthy := service.transportCandidateHealthyLocked("phone-1", "usb", usb)
		service.mu.Unlock()
		if healthy {
			t.Fatal("USB standby path accepted without a video probe acknowledgement")
		}
	}
	service.updateVideoTransportFeedback("phone-1", "usb", -1, -1, true)
	target, ok = service.currentVideoTarget()
	if !ok || target.Transport != "usb" {
		t.Fatalf("probe-verified fallback = %+v, enabled=%t; want USB", target, ok)
	}
}

func (s *HostService) transportCandidateGroupForTest(deviceID, transport string) (*activeDevice, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for key, device := range s.devices {
		identity := device.Snapshot.DeviceID
		if identity == "" {
			identity = "legacy:" + key
		}
		if identity != deviceID {
			continue
		}
		if transport == "usb" && device.USBLink != nil {
			copy := device
			return &copy, true
		}
		if transport == "network" && device.USBLink == nil {
			copy := device
			return &copy, true
		}
	}
	return nil, false
}

func TestSnapshotCombinesNetworkAndAOAAsOnePhone(t *testing.T) {
	service := NewHostService(false)
	addVideoTransportTestDevice(t, service, "phone-1", "USB-AOA:test", "USB AOA Bulk")
	addVideoTransportTestDevice(t, service, "phone-1", "192.0.2.10:50000", "Wi-Fi / LAN")
	snapshot := service.Snapshot()
	if len(snapshot.ConnectedDevices) != 1 {
		t.Fatalf("connected device count = %d; want one paired phone", len(snapshot.ConnectedDevices))
	}
	transport := snapshot.ConnectedDevices[0].Transport
	if !strings.Contains(transport, "USB AOA Bulk") || !strings.Contains(transport, "Wi-Fi / LAN") {
		t.Fatalf("combined transport = %q; want both AOA and LAN", transport)
	}
}
