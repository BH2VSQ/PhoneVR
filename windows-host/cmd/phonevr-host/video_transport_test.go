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

func TestParallelVideoTransportStripesDistinctFragmentsAcrossUDPAndUSB(t *testing.T) {
	udpReceiver, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatal(err)
	}
	defer udpReceiver.Close()
	if err := udpReceiver.SetReadDeadline(time.Now().Add(180 * time.Millisecond)); err != nil {
		t.Fatal(err)
	}

	udpSender, err := net.ListenUDP("udp", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer udpSender.Close()
	service := NewHostService(false)
	service.videoUDP = udpSender
	service.videoUSBQueue = make(chan usbVideoWork, videoUSBQueueDepth)
	workerCtx, stopWorker := context.WithCancel(context.Background())
	service.wg.Add(1)
	go service.videoUSBWorkSendLoop(workerCtx, service.videoUSBQueue)
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
		t.Fatalf("stripe video fragments: %v", err)
	}

	udpPackets := make([][]byte, 0, len(fragments))
	for {
		buffer := make([]byte, videoprotocol.MaxDatagramSize)
		n, _, err := udpReceiver.ReadFromUDP(buffer)
		if err != nil {
			if netErr, ok := err.(net.Error); ok && netErr.Timeout() {
				break
			}
			t.Fatalf("read striped UDP fragment: %v", err)
		}
		udpPackets = append(udpPackets, append([]byte(nil), buffer[:n]...))
	}
	if len(udpPackets) == 0 || len(udpPackets) >= len(fragments) {
		t.Fatalf("UDP fragment count = %d of %d; expected a split across both paths", len(udpPackets), len(fragments))
	}

	udpOffsets := make(map[uint32][]byte, len(udpPackets))
	for _, packet := range udpPackets {
		decoded, err := videoprotocol.Decode(packet)
		if err != nil {
			t.Fatalf("decode UDP PVRV fragment: %v", err)
		}
		udpOffsets[decoded.FragmentOffset] = packet
	}
	usbExpectedBytes := 0
	for _, fragment := range fragments {
		decoded, _ := videoprotocol.Decode(fragment)
		if _, sentOnUDP := udpOffsets[decoded.FragmentOffset]; !sentOnUDP {
			usbExpectedBytes += usbmux.HeaderSize + len(fragment)
		}
	}
	deadline := time.Now().Add(2 * time.Second)
	for len(stream.Bytes()) < usbExpectedBytes && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	usbData := stream.Bytes()
	if len(usbData) != usbExpectedBytes {
		t.Fatalf("USB striped wire bytes = %d, want %d", len(usbData), usbExpectedBytes)
	}
	usbReader := bytes.NewReader(usbData)
	seen := make(map[uint32]string, len(fragments))
	for offset := range udpOffsets {
		seen[offset] = "network"
	}
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
		decoded, err := videoprotocol.Decode(packet.Payload)
		if err != nil {
			t.Fatalf("decode USB PVRV fragment: %v", err)
		}
		if _, duplicate := seen[decoded.FragmentOffset]; duplicate {
			t.Fatalf("fragment offset %d was duplicated across transport paths", decoded.FragmentOffset)
		}
		seen[decoded.FragmentOffset] = "usb"
	}
	if len(seen) != len(fragments) {
		t.Fatalf("combined paths delivered %d unique fragments, want %d", len(seen), len(fragments))
	}
	for _, fragment := range fragments {
		decoded, _ := videoprotocol.Decode(fragment)
		if _, ok := seen[decoded.FragmentOffset]; !ok {
			t.Fatalf("fragment at offset %d is missing from both paths", decoded.FragmentOffset)
		}
	}

}

func TestParallelVideoTransportKeepsUDPWhenUSBQueueIsFull(t *testing.T) {
	udpReceiver, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatal(err)
	}
	defer udpReceiver.Close()
	if err := udpReceiver.SetReadDeadline(time.Now().Add(200 * time.Millisecond)); err != nil {
		t.Fatal(err)
	}
	udpSender, err := net.ListenUDP("udp", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer udpSender.Close()

	service := NewHostService(false)
	service.videoUDP = udpSender
	service.videoUSBQueue = make(chan usbVideoWork, 1)
	service.videoUSBQueue <- usbVideoWork{} // occupy the only queue slot
	frame, err := videoprotocol.FragmentFrame(bytes.Repeat([]byte{0x41}, 2600), 55, 1234, false)
	if err != nil {
		t.Fatal(err)
	}
	target := videoTarget{
		USB: &usbmux.Mux{}, UDP: udpReceiver.LocalAddr().(*net.UDPAddr),
		Transport: "parallel", DeviceID: "phone-queue-full-test",
	}
	if err := service.sendVideoFragmentsParallel(frame, target); err != nil {
		t.Fatalf("USB queue congestion should not fail a successful UDP delivery: %v", err)
	}
	seen := make(map[uint32]struct{}, len(frame))
	for {
		buffer := make([]byte, videoprotocol.MaxDatagramSize)
		n, _, err := udpReceiver.ReadFromUDP(buffer)
		if err != nil {
			if netErr, ok := err.(net.Error); ok && netErr.Timeout() {
				break
			}
			t.Fatalf("read surviving UDP fallback: %v", err)
		}
		packet, err := videoprotocol.Decode(buffer[:n])
		if err != nil {
			t.Fatalf("UDP fallback packet is invalid: %v", err)
		}
		seen[packet.FragmentOffset] = struct{}{}
	}
	if len(seen) != len(frame) {
		t.Fatalf("UDP fallback recovered %d unique fragments, want %d", len(seen), len(frame))
	}
	service.mu.RLock()
	health := service.transportHealth["phone-queue-full-test|usb"]
	service.mu.RUnlock()
	if health.UnhealthyUntil.IsZero() {
		t.Fatal("USB route should be marked unhealthy when its video queue is full")
	}
}

func TestVideoNackRetransmitsMissingNetworkFragmentOverUSB(t *testing.T) {
	udpSender, err := net.ListenUDP("udp", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer udpSender.Close()
	service := NewHostService(false)
	service.videoUDP = udpSender
	service.videoUSBQueue = make(chan usbVideoWork, videoUSBQueueDepth)
	workerCtx, stopWorker := context.WithCancel(context.Background())
	service.wg.Add(1)
	go service.videoUSBWorkSendLoop(workerCtx, service.videoUSBQueue)
	t.Cleanup(func() {
		stopWorker()
		service.wg.Wait()
	})

	stream := newVideoMuxTestStream()
	usb := usbmux.New(stream)
	defer usb.Close()
	frameBytes := bytes.Repeat([]byte{0x65, 0x11, 0x22, 0x33}, 900)
	fragments, err := videoprotocol.FragmentFrame(frameBytes, 771, 99887766, true)
	if err != nil {
		t.Fatal(err)
	}
	target := videoTarget{
		USB: usb, UDP: &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 48104},
		Transport: "parallel", DeviceID: "phone-nack-test",
	}
	service.retainStripedVideoFrame(fragments, repeatVideoPath("network", len(fragments)), target)
	service.retransmitMissingVideoFragments("phone-nack-test", 771, []int{1, 1, -1, len(fragments) + 1})

	expectedBytes := usbmux.HeaderSize + len(fragments[1])
	deadline := time.Now().Add(time.Second)
	for len(stream.Bytes()) < expectedBytes && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	reader := bytes.NewReader(stream.Bytes())
	packet, err := usbmux.ReadFrame(reader)
	if err != nil {
		t.Fatalf("read repaired USB mux packet: %v", err)
	}
	if packet.Channel != usbmux.ChannelVideo || !bytes.Equal(packet.Payload, fragments[1]) {
		t.Fatalf("repaired USB packet differs from missing fragment: channel=%d bytes=%d", packet.Channel, len(packet.Payload))
	}
	if service.videoRetransmittedFragments.Load() != 1 {
		t.Fatalf("retransmitted fragment count = %d, want 1", service.videoRetransmittedFragments.Load())
	}
}

func TestParallelStripeWeightsFollowMeasuredPathCapacity(t *testing.T) {
	service := NewHostService(false)
	service.setVideoTransportAssignedShares("phone-rate", 1000, 1000)
	service.updateVideoTransportFeedbackWithRate("phone-rate", "network", -1, -1, false, 20_000_000)
	service.updateVideoTransportFeedbackWithRate("phone-rate", "usb", -1, -1, false, 1_000_000)
	if share := service.videoStripeUSBSharePermille("phone-rate"); share != 50 {
		t.Fatalf("USB share = %d permille, want a 5%% floor for a 1:20 capacity ratio", share)
	}
	fragments := make([][]byte, 40)
	for index := range fragments {
		fragments[index] = []byte{byte(index)}
	}
	network, usb, assignments := service.stripeVideoFragments("phone-rate-test", fragments, 250)
	if len(network) != 30 || len(usb) != 10 {
		t.Fatalf("weighted stripe counts network=%d USB=%d; want 30/10", len(network), len(usb))
	}
	if len(assignments) != len(fragments) {
		t.Fatalf("assignment count = %d, want %d", len(assignments), len(fragments))
	}
}

func TestNetworkOnlyFrameCanBeRetransmittedAfterNACK(t *testing.T) {
	listener, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 0})
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	if err := listener.SetReadDeadline(time.Now().Add(time.Second)); err != nil {
		t.Fatal(err)
	}

	sender, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4zero, Port: 0})
	if err != nil {
		t.Fatal(err)
	}
	defer sender.Close()
	service := NewHostService(false)
	service.videoUDP = sender
	frameData := bytes.Repeat([]byte{0x41, 0x42, 0x43, 0x44}, 900)
	fragments, err := videoprotocol.FragmentFrame(frameData, 912, 445566, true)
	if err != nil {
		t.Fatal(err)
	}
	target := videoTarget{
		Transport: "network",
		DeviceID:  "phone-network-only",
		UDP:       listener.LocalAddr().(*net.UDPAddr),
	}
	service.retainVideoFrameForSinglePath(fragments, "network", target)
	service.retransmitMissingVideoFragments(target.DeviceID, 912, []int{-2})

	for index, expected := range fragments {
		buffer := make([]byte, videoprotocol.MaxDatagramSize)
		n, _, readErr := listener.ReadFromUDP(buffer)
		if readErr != nil {
			t.Fatalf("read whole-frame repair fragment %d/%d: %v", index+1, len(fragments), readErr)
		}
		if !bytes.Equal(buffer[:n], expected) {
			t.Fatalf("repaired fragment %d differs from retained PVRV packet", index)
		}
	}
	if got := service.videoRetransmittedFragments.Load(); got != uint64(len(fragments)) {
		t.Fatalf("retransmitted fragment counter = %d, want %d", got, len(fragments))
	}
}

func TestVideoReorderGraceMatchesSelectedTransportMode(t *testing.T) {
	service := NewHostService(false)
	service.transportMode = VideoTransportNetwork
	if got := service.videoReorderGraceMilliseconds(); got != 60 {
		t.Fatalf("network reorder grace = %dms, want 60ms", got)
	}
	service.transportMode = VideoTransportParallel
	if got := service.videoReorderGraceMilliseconds(); got != 120 {
		t.Fatalf("parallel reorder grace = %dms, want 120ms", got)
	}
}

func TestStripeCreditIsPerDeviceAndSingleFragmentUsesNetwork(t *testing.T) {
	service := NewHostService(false)
	one := [][]byte{{0x01}}
	network, usb, assignments := service.stripeVideoFragments("phone-a", one, 750)
	if len(network) != 1 || len(usb) != 0 || assignments[0] != "network" {
		t.Fatalf("single fragment assignment network=%d USB=%d paths=%v; want network only", len(network), len(usb), assignments)
	}
	service.stripeVideoFragments("phone-b", [][]byte{{1}, {2}}, 750)
	if got := service.videoStripeCredit["phone-a"]; got != 0 {
		t.Fatalf("phone-a credit changed when phone-b streamed: %d", got)
	}
	if got := service.videoStripeCredit["phone-b"]; got == 0 {
		t.Fatal("phone-b should have independent stripe credit")
	}
}

func TestStripeKeepsNetworkMarkerAndBoundsCreditAtHighUSBShare(t *testing.T) {
	service := NewHostService(false)
	for frame := 0; frame < 100; frame++ {
		network, usb, _ := service.stripeVideoFragments("phone-high-usb", [][]byte{{1}, {2}}, 950)
		if len(network) != 1 || len(usb) != 1 {
			t.Fatalf("frame %d got network=%d USB=%d; every two-fragment frame needs one packet on each path", frame, len(network), len(usb))
		}
		if credit := service.videoStripeCredit["phone-high-usb"]; credit < 0 || credit >= 1000 {
			t.Fatalf("stripe credit escaped [0,999] after frame %d: %d", frame, credit)
		}
	}
}

func TestRetainedVideoFramesAreScopedByDeviceAndFrameID(t *testing.T) {
	service := NewHostService(false)
	fragments, err := videoprotocol.FragmentFrame(bytes.Repeat([]byte{0x41}, 2600), 91, 1234, false)
	if err != nil {
		t.Fatal(err)
	}
	service.retainStripedVideoFrame(fragments, repeatVideoPath("network", len(fragments)), videoTarget{DeviceID: "phone-a"})
	service.retainStripedVideoFrame(fragments, repeatVideoPath("usb", len(fragments)), videoTarget{DeviceID: "phone-b"})
	if got := len(service.retainedVideoFrames); got != 2 {
		t.Fatalf("retained frame entries = %d, want 2 for two devices with the same frame ID", got)
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
