package main

import (
	"bufio"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"math"
	"net"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestHostSnapshotIncludesVideoPipelineCounters(t *testing.T) {
	service := NewHostService(false)
	service.videoBridgeActive.Store(true)
	service.videoRawFrames.Store(120)
	service.videoEncodedFrames.Store(118)
	service.videoUDPFragments.Store(420)
	service.videoDroppedFrames.Store(2)

	snapshot := service.Snapshot()
	if !snapshot.VideoCaptureBridgeActive {
		t.Fatal("video capture bridge state was not exposed in Snapshot")
	}
	if snapshot.VideoRawFrames != 120 || snapshot.VideoEncodedFrames != 118 ||
		snapshot.VideoUDPFragments != 420 || snapshot.VideoDroppedFrames != 2 {
		t.Fatalf("unexpected video counters: %+v", snapshot)
	}
}

func TestRawVideoQueueKeepsNewestFramesWhenEncoderFallsBehind(t *testing.T) {
	service := NewHostService(false)
	service.videoFrameQueue = make(chan *rawVideoFrame, videoInputQueueDepth)
	service.videoFramePool = sync.Pool{}

	for value := byte(1); value <= 3; value++ {
		service.enqueueRawVideoFrame(&rawVideoFrame{data: []byte{value}, timestampNs: uint64(value)})
	}

	if got := len(service.videoFrameQueue); got != videoInputQueueDepth {
		t.Fatalf("queue depth = %d, want bounded depth %d", got, videoInputQueueDepth)
	}
	if got := service.videoInputQueueDrops.Load(); got != 1 {
		t.Fatalf("dropped stale frames = %d, want 1", got)
	}
	first := <-service.videoFrameQueue
	second := <-service.videoFrameQueue
	if first.data[0] != 2 || second.data[0] != 3 {
		t.Fatalf("queue did not preserve newest frames: first=%d second=%d", first.data[0], second.data[0])
	}
}

func TestHostSnapshotUsesCachedNetworkAddresses(t *testing.T) {
	service := NewHostService(false)
	service.localIPv4 = []string{"192.168.10.20", "10.0.0.5"}

	snapshot := service.Snapshot()
	if len(snapshot.LocalIPv4) != 2 || snapshot.LocalIPv4[0] != "192.168.10.20" {
		t.Fatalf("unexpected cached addresses: %#v", snapshot.LocalIPv4)
	}
	snapshot.LocalIPv4[0] = "mutated"
	if service.localIPv4[0] != "192.168.10.20" {
		t.Fatal("Snapshot() exposed the service's cached address backing array")
	}
}

func TestHostServiceLifecycleAndPairing(t *testing.T) {
	config := HostConfig{
		ListenAddress: "127.0.0.1",
		DiscoveryPort: reservePort(t, "udp4"),
		ControlPort:   reservePort(t, "tcp4"),
		PosePort:      reservePort(t, "udp4"),
	}
	service := NewHostService(false)
	if err := service.Start(config); err != nil {
		t.Fatalf("Start() error = %v", err)
	}

	snapshot := service.Snapshot()
	if !snapshot.Running {
		t.Fatal("service should be running after Start")
	}
	if snapshot.ControlPort == 0 || snapshot.DiscoveryPort == 0 || snapshot.PosePort == 0 {
		t.Fatalf("ports should be assigned: %+v", snapshot)
	}

	conn, err := net.DialTimeout("tcp4", net.JoinHostPort("127.0.0.1", fmt.Sprint(snapshot.ControlPort)), time.Second)
	if err != nil {
		t.Fatalf("dial control listener: %v", err)
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(2 * time.Second))
	encoder := json.NewEncoder(conn)
	if err := encoder.Encode(helloMessage{Type: "hello", Protocol: protocolVersion, DeviceName: "Test Android", Client: "PhoneVRStream-Test", MediaCapabilities: json.RawMessage(`[ {"mimeType":"video/avc","hardwareAccelerated":true} ]`)}); err != nil {
		t.Fatalf("send hello: %v", err)
	}
	reader := bufio.NewReader(conn)
	decoder := json.NewDecoder(reader)
	var welcome welcomeMessage
	if err := decoder.Decode(&welcome); err != nil {
		t.Fatalf("read welcome: %v", err)
	}
	if welcome.Type != "welcome" || welcome.PosePort != snapshot.PosePort {
		t.Fatalf("unexpected welcome response: %+v", welcome)
	}
	if service.Snapshot().VideoCaptureBridgeActive {
		t.Fatal("control-channel pairing must not be reported as an active raw-video capture bridge")
	}
	completeOpticalPresetPull(t, service, decoder, encoder)
	if err := encoder.Encode(controlMessage{Type: "ping"}); err != nil {
		t.Fatalf("send ping: %v", err)
	}
	var pong map[string]any
	if err := decoder.Decode(&pong); err != nil {
		t.Fatalf("read pong: %v", err)
	}
	if pong["type"] != "pong" {
		t.Fatalf("expected pong, got %v", pong)
	}

	waitUntil(t, 2*time.Second, func() bool { return len(service.Snapshot().ConnectedDevices) == 1 })
	deviceSnapshot := service.Snapshot().ConnectedDevices[0]
	if len(deviceSnapshot.MediaCapabilities) == 0 || !json.Valid(deviceSnapshot.MediaCapabilities) {
		t.Fatalf("phone media capabilities were not retained: %s", deviceSnapshot.MediaCapabilities)
	}
	if err := service.SendPhoneAction("center_view"); err != nil {
		t.Fatalf("send center-view action: %v", err)
	}
	var action map[string]any
	if err := decoder.Decode(&action); err != nil {
		t.Fatalf("read device action: %v", err)
	}
	if action["type"] != "device_action" || action["action"] != "center_view" {
		t.Fatalf("unexpected device action: %#v", action)
	}
	if err := service.SendPhoneAction("unsupported"); err == nil {
		t.Fatal("unsupported device action should be rejected")
	}

	poseSocket, err := net.DialUDP("udp4", nil, &net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: snapshot.PosePort})
	if err != nil {
		t.Fatalf("dial pose receiver: %v", err)
	}
	defer poseSocket.Close()
	if _, err := poseSocket.Write(testPosePacket()); err != nil {
		t.Fatalf("write pose packet: %v", err)
	}
	waitUntil(t, 2*time.Second, func() bool { return service.Snapshot().PacketsReceived == 1 })
	pose := service.Snapshot().LatestPose
	if pose == nil || pose.Sequence != 42 {
		t.Fatalf("unexpected latest pose: %+v", pose)
	}

	service.Stop()
	if service.Snapshot().Running {
		t.Fatal("service should be stopped after Stop")
	}
}

func TestSteamVRDriverHeartbeatUpdatesConnectionState(t *testing.T) {
	config := HostConfig{
		ListenAddress: "127.0.0.1",
		DiscoveryPort: reservePort(t, "udp4"),
		ControlPort:   reservePort(t, "tcp4"),
		PosePort:      reservePort(t, "udp4"),
	}
	service := NewHostService(false)
	if err := service.Start(config); err != nil {
		t.Skipf("Host listeners unavailable for heartbeat integration test: %v", err)
	}
	defer service.Stop()

	sender, err := net.DialUDP("udp4", nil, &net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: steamVRStatusPort})
	if err != nil {
		t.Fatalf("open SteamVR heartbeat sender: %v", err)
	}
	defer sender.Close()

	packet := make([]byte, steamVRHeartbeatPacketSize)
	copy(packet[:4], "PVST")
	packet[4] = 1
	packet[5] = steamVRFlagDriverLoaded | steamVRFlagHMDActive | steamVRFlagPoseValid | steamVRFlagDisplayActive | steamVRFlagPresentedFrame
	binary.BigEndian.PutUint64(packet[8:], 123456)
	if _, err := sender.Write(packet); err != nil {
		t.Fatalf("send SteamVR heartbeat: %v", err)
	}
	waitUntil(t, time.Second, func() bool { return service.Snapshot().SteamVRConnected })
	snapshot := service.Snapshot()
	if !snapshot.SteamVRDriverLoaded || !snapshot.SteamVRHMDActive || !snapshot.SteamVRPoseValid ||
		!snapshot.SteamVRDisplayActive || !snapshot.SteamVRHasPresentedFrame || snapshot.SteamVRFrameCounter != 123456 {
		t.Fatalf("SteamVR status was not decoded correctly: %+v", snapshot)
	}

	service.mu.Lock()
	service.steamVRHeartbeatAt = time.Now().Add(-3 * time.Second)
	service.mu.Unlock()
	if stale := service.Snapshot(); stale.SteamVRConnected || stale.SteamVRPoseValid || stale.SteamVRDisplayActive {
		t.Fatalf("stale driver heartbeat should not remain connected: %+v", stale)
	}
}

func TestDecodeSteamVRDriverHeartbeatRejectsInvalidPackets(t *testing.T) {
	valid := make([]byte, steamVRHeartbeatPacketSize)
	copy(valid[:4], "PVST")
	valid[4] = 1
	valid[5] = steamVRFlagDriverLoaded
	binary.BigEndian.PutUint64(valid[8:], 99)
	if heartbeat, ok := decodeSteamVRDriverHeartbeat(valid); !ok || heartbeat.Flags != steamVRFlagDriverLoaded || heartbeat.FrameCounter != 99 {
		t.Fatalf("valid heartbeat decode = %+v, %v", heartbeat, ok)
	}
	for _, invalid := range [][]byte{nil, valid[:15], append(append([]byte(nil), valid...), 0)} {
		if _, ok := decodeSteamVRDriverHeartbeat(invalid); ok {
			t.Fatalf("invalid heartbeat unexpectedly accepted: %x", invalid)
		}
	}
	invalidVersion := append([]byte(nil), valid...)
	invalidVersion[4] = 2
	if _, ok := decodeSteamVRDriverHeartbeat(invalidVersion); ok {
		t.Fatal("unsupported heartbeat version was accepted")
	}
}

func TestSteamVRPoseBridgeForwardsValidatedPhonePose(t *testing.T) {
	bridge, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: steamVRPoseBridgePort})
	if err != nil {
		t.Skipf("SteamVR bridge port is already in use: %v", err)
	}
	defer bridge.Close()
	_ = bridge.SetReadDeadline(time.Now().Add(2 * time.Second))

	config := HostConfig{
		ListenAddress: "127.0.0.1",
		DiscoveryPort: reservePort(t, "udp4"),
		ControlPort:   reservePort(t, "tcp4"),
		PosePort:      reservePort(t, "udp4"),
	}
	service := NewHostService(false)
	if err := service.Start(config); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	defer service.Stop()

	sender, err := net.DialUDP("udp4", nil, &net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: service.Snapshot().PosePort})
	if err != nil {
		t.Fatalf("dial phone pose endpoint: %v", err)
	}
	defer sender.Close()
	packet := testPosePacket()
	if _, err := sender.Write(packet); err != nil {
		t.Fatalf("send pose packet: %v", err)
	}

	received := make([]byte, 64)
	n, _, err := bridge.ReadFromUDP(received)
	if err != nil {
		t.Fatalf("read SteamVR bridge packet: %v", err)
	}
	if n != len(packet) || string(received[:n]) != string(packet) {
		t.Fatalf("SteamVR bridge packet mismatch: got %d bytes", n)
	}
}

func TestHighRatePoseTrafficDoesNotBreakControlHeartbeat(t *testing.T) {
	config := HostConfig{
		ListenAddress: "127.0.0.1",
		DiscoveryPort: reservePort(t, "udp4"),
		ControlPort:   reservePort(t, "tcp4"),
		PosePort:      reservePort(t, "udp4"),
	}
	service := NewHostService(false)
	if err := service.Start(config); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	defer service.Stop()

	snapshot := service.Snapshot()
	conn, err := net.DialTimeout("tcp4", net.JoinHostPort("127.0.0.1", fmt.Sprint(snapshot.ControlPort)), time.Second)
	if err != nil {
		t.Fatalf("dial control listener: %v", err)
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(8 * time.Second))
	encoder := json.NewEncoder(conn)
	decoder := json.NewDecoder(conn)
	if err := encoder.Encode(helloMessage{Type: "hello", Protocol: protocolVersion, DeviceName: "Fast Motion Test", Client: "PhoneVRStressTest"}); err != nil {
		t.Fatalf("send hello: %v", err)
	}
	var welcome welcomeMessage
	if err := decoder.Decode(&welcome); err != nil {
		t.Fatalf("read welcome: %v", err)
	}
	if welcome.Type != "welcome" {
		t.Fatalf("unexpected welcome: %+v", welcome)
	}
	completeOpticalPresetPull(t, service, decoder, encoder)

	udp, err := net.DialUDP("udp4", nil, &net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: snapshot.PosePort})
	if err != nil {
		t.Fatalf("open pose sender: %v", err)
	}
	defer udp.Close()
	_ = udp.SetWriteBuffer(256 * 1024)

	floodDone := make(chan error, 1)
	go func() {
		packet := testPosePacket()
		for i := 0; i < 3000; i++ {
			binary.BigEndian.PutUint32(packet[8:12], uint32(i))
			if _, writeErr := udp.Write(packet); writeErr != nil {
				floodDone <- writeErr
				return
			}
		}
		floodDone <- nil
	}()

	// Keep the independent TCP heartbeat healthy while UDP pose samples arrive
	// in a burst that is much faster than normal 60 Hz tracking.
	for i := 0; i < 20; i++ {
		_ = service.Snapshot() // mirrors the native UI's periodic status poll
		if err := encoder.Encode(controlMessage{Type: "ping"}); err != nil {
			t.Fatalf("send heartbeat %d during UDP burst: %v", i, err)
		}
		var pong map[string]any
		if err := decoder.Decode(&pong); err != nil {
			t.Fatalf("read heartbeat %d during UDP burst: %v", i, err)
		}
		if pong["type"] != "pong" {
			t.Fatalf("heartbeat %d returned %v, want pong", i, pong["type"])
		}
		time.Sleep(10 * time.Millisecond)
	}
	if err := <-floodDone; err != nil {
		t.Fatalf("UDP pose flood failed: %v", err)
	}
	waitUntil(t, 2*time.Second, func() bool {
		return len(service.Snapshot().ConnectedDevices) == 1 && service.Snapshot().PacketsReceived > 0
	})
}

func TestReadLatestPoseBatchCoalescesQueuedDatagrams(t *testing.T) {
	listener, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: 0})
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()

	sender, err := net.DialUDP("udp4", nil, listener.LocalAddr().(*net.UDPAddr))
	if err != nil {
		t.Fatal(err)
	}
	defer sender.Close()

	for sequence := uint32(10); sequence <= 12; sequence++ {
		packet := testPosePacket()
		binary.BigEndian.PutUint32(packet[8:12], sequence)
		if _, err := sender.Write(packet); err != nil {
			t.Fatalf("send pose sequence %d: %v", sequence, err)
		}
	}

	batch, invalid, err := readLatestPoseBatch(listener, make([]byte, 1500), time.Second)
	if err != nil {
		t.Fatalf("readLatestPoseBatch() error = %v", err)
	}
	if invalid != 0 {
		t.Fatalf("unexpected invalid datagrams: %d", invalid)
	}
	if len(batch) != 3 {
		t.Fatalf("expected 3 queued poses in one batch, got %d", len(batch))
	}
	if got := batch[len(batch)-1].pose.Sequence; got != 12 {
		t.Fatalf("latest pose sequence = %d, want 12", got)
	}
	if got := binary.BigEndian.Uint32(batch[len(batch)-1].payload[8:12]); got != 12 {
		t.Fatalf("forward payload sequence = %d, want 12", got)
	}
}

func TestPoseBatchForwardPolicyPreservesShortBurstsAndBoundsLongBacklogs(t *testing.T) {
	makeBatch := func(count int) []receivedPoseDatagram {
		batch := make([]receivedPoseDatagram, count)
		for i := range batch {
			batch[i].pose.Sequence = uint32(i + 1)
		}
		return batch
	}

	short := makeBatch(8)
	if got := poseBatchToForward(short); len(got) != len(short) {
		t.Fatalf("short burst forwards %d samples, want all %d", len(got), len(short))
	}

	long := makeBatch(maxPoseBatchForward + 20)
	got := poseBatchToForward(long)
	if len(got) != 1 {
		t.Fatalf("long stale backlog forwards %d samples, want latest only", len(got))
	}
	if got[0].pose.Sequence != uint32(len(long)) {
		t.Fatalf("long backlog forwarded sequence %d, want newest %d", got[0].pose.Sequence, len(long))
	}
}

func TestNormalizeHostConfigKeepsEphemeralPorts(t *testing.T) {
	config := normalizeHostConfig(HostConfig{ListenAddress: "127.0.0.1", DiscoveryPort: 0, ControlPort: 48101, PosePort: 48102})
	if config.DiscoveryPort != 0 {
		t.Fatalf("expected ephemeral discovery port 0, got %d", config.DiscoveryPort)
	}
	if normalizeHostConfig(HostConfig{}).ControlPort != controlPortDefault {
		t.Fatal("zero-value config should resolve to defaults")
	}
}

func reservePort(t *testing.T, network string) int {
	t.Helper()
	if network == "tcp4" {
		listener, err := net.Listen("tcp4", "127.0.0.1:0")
		if err != nil {
			t.Fatal(err)
		}
		port := listener.Addr().(*net.TCPAddr).Port
		_ = listener.Close()
		return port
	}
	conn, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: 0})
	if err != nil {
		t.Fatal(err)
	}
	port := conn.LocalAddr().(*net.UDPAddr).Port
	_ = conn.Close()
	return port
}

func testPosePacket() []byte {
	packet := make([]byte, 36)
	copy(packet[:4], []byte("PVSP"))
	packet[4] = 1
	binary.BigEndian.PutUint32(packet[8:12], 42)
	binary.BigEndian.PutUint64(packet[12:20], uint64(time.Now().UnixNano()))
	binary.BigEndian.PutUint32(packet[20:24], math.Float32bits(0))
	binary.BigEndian.PutUint32(packet[24:28], math.Float32bits(0))
	binary.BigEndian.PutUint32(packet[28:32], math.Float32bits(0))
	binary.BigEndian.PutUint32(packet[32:36], math.Float32bits(1))
	return packet
}

func waitUntil(t *testing.T, timeout time.Duration, predicate func() bool) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if predicate() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("condition was not met before timeout")
}

func TestHostServiceDiscoveryResponse(t *testing.T) {
	config := HostConfig{
		ListenAddress: "127.0.0.1",
		DiscoveryPort: reservePort(t, "udp4"),
		ControlPort:   reservePort(t, "tcp4"),
		PosePort:      reservePort(t, "udp4"),
	}
	service := NewHostService(false)
	if err := service.Start(config); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	defer service.Stop()

	client, err := net.DialUDP("udp4", nil, &net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: service.Snapshot().DiscoveryPort})
	if err != nil {
		t.Fatalf("dial discovery service: %v", err)
	}
	defer client.Close()
	_ = client.SetDeadline(time.Now().Add(time.Second))
	if _, err := client.Write([]byte(discoveryQuery)); err != nil {
		t.Fatalf("send discovery request: %v", err)
	}
	buffer := make([]byte, 512)
	n, _, err := client.ReadFromUDP(buffer)
	if err != nil {
		t.Fatalf("read discovery response: %v", err)
	}
	response := string(buffer[:n])
	if !strings.HasPrefix(response, discoveryPrefix) {
		t.Fatalf("unexpected discovery response: %s", response)
	}
	var message discoveryMessage
	if err := json.Unmarshal([]byte(strings.TrimPrefix(response, discoveryPrefix)), &message); err != nil {
		t.Fatalf("decode discovery response: %v", err)
	}
	if message.Protocol != protocolVersion || message.ControlPort != service.Snapshot().ControlPort {
		t.Fatalf("unexpected discovery payload: %+v", message)
	}
}

func TestStopReleasesAllListeningPortsForImmediateRestart(t *testing.T) {
	discoveryPort := reservePort(t, "udp4")
	posePort := reservePort(t, "udp4")
	for posePort == discoveryPort {
		posePort = reservePort(t, "udp4")
	}
	config := HostConfig{
		ListenAddress: "127.0.0.1",
		DiscoveryPort: discoveryPort,
		ControlPort:   reservePort(t, "tcp4"),
		PosePort:      posePort,
	}

	service := NewHostService(false)
	if err := service.Start(config); err != nil {
		t.Fatalf("initial Start() error = %v", err)
	}
	started := service.Snapshot()
	service.Stop()
	if service.Snapshot().Running {
		t.Fatal("service still reports running after Stop")
	}

	// Bind every endpoint immediately after Stop. This proves that the TCP
	// listener and both UDP sockets have been closed, not merely hidden in UI.
	tcp, err := net.Listen("tcp4", net.JoinHostPort("127.0.0.1", fmt.Sprint(started.ControlPort)))
	if err != nil {
		t.Fatalf("TCP control port %d was not released: %v", started.ControlPort, err)
	}
	_ = tcp.Close()

	for _, port := range []int{started.DiscoveryPort, started.PosePort} {
		udp, listenErr := net.ListenUDP("udp4", &net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: port})
		if listenErr != nil {
			t.Fatalf("UDP port %d was not released: %v", port, listenErr)
		}
		_ = udp.Close()
	}

	// The same service instance must also be restartable on the same ports.
	if err := service.Start(config); err != nil {
		t.Fatalf("Start() after Stop() could not rebind released ports: %v", err)
	}
	service.Stop()
}

func TestStopClosesActiveControlConnections(t *testing.T) {
	config := HostConfig{
		ListenAddress: "127.0.0.1",
		DiscoveryPort: reservePort(t, "udp4"),
		ControlPort:   reservePort(t, "tcp4"),
		PosePort:      reservePort(t, "udp4"),
	}
	service := NewHostService(false)
	if err := service.Start(config); err != nil {
		t.Fatalf("Start() error = %v", err)
	}

	snapshot := service.Snapshot()
	conn, err := net.DialTimeout("tcp4", net.JoinHostPort("127.0.0.1", fmt.Sprint(snapshot.ControlPort)), time.Second)
	if err != nil {
		t.Fatalf("dial active control session: %v", err)
	}
	defer conn.Close()
	waitUntil(t, time.Second, func() bool {
		service.mu.RLock()
		defer service.mu.RUnlock()
		return len(service.connections) == 1
	})

	service.Stop()
	_ = conn.SetReadDeadline(time.Now().Add(time.Second))
	if _, readErr := conn.Read(make([]byte, 1)); readErr == nil {
		t.Fatal("active control connection remained open after Host Stop")
	}
}

func TestSendPhoneDisplaySettingsAndReceiveAppliedAck(t *testing.T) {
	config := HostConfig{
		ListenAddress: "127.0.0.1",
		DiscoveryPort: reservePort(t, "udp4"),
		ControlPort:   reservePort(t, "tcp4"),
		PosePort:      reservePort(t, "udp4"),
	}
	service := NewHostService(false)
	if err := service.Start(config); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	defer service.Stop()

	snapshot := service.Snapshot()
	conn, err := net.DialTimeout("tcp4", net.JoinHostPort("127.0.0.1", fmt.Sprint(snapshot.ControlPort)), time.Second)
	if err != nil {
		t.Fatalf("dial control listener: %v", err)
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(3 * time.Second))
	encoder := json.NewEncoder(conn)
	decoder := json.NewDecoder(bufio.NewReader(conn))
	if err := encoder.Encode(helloMessage{Type: "hello", Protocol: protocolVersion, DeviceName: "Settings Test Phone", Client: "PhoneVR-Test"}); err != nil {
		t.Fatalf("send hello: %v", err)
	}
	var welcome welcomeMessage
	if err := decoder.Decode(&welcome); err != nil {
		t.Fatalf("read welcome: %v", err)
	}
	if welcome.Type != "welcome" {
		t.Fatalf("unexpected welcome: %+v", welcome)
	}
	completeOpticalPresetPull(t, service, decoder, encoder)

	current := DefaultPhoneDisplaySettings()
	current.IPDMm = 62.5
	if err := encoder.Encode(map[string]any{"type": "display_settings", "settings": current}); err != nil {
		t.Fatalf("send initial settings: %v", err)
	}
	waitUntil(t, time.Second, func() bool {
		devices := service.Snapshot().ConnectedDevices
		return len(devices) == 1 && devices[0].DisplaySettings != nil
	})

	requested := DefaultPhoneDisplaySettings()
	requested.ViewerProfileID = "daydream_view_2017"
	requested.IPDMm = 68.5
	requested.LensSeparationMm = 65.2
	requested.DistortionK1 = .455
	requested.DistortionK2 = -.071
	if err := service.SendPhoneDisplaySettings(requested); err != nil {
		t.Fatalf("SendPhoneDisplaySettings() error = %v", err)
	}
	var command controlMessage
	if err := decoder.Decode(&command); err != nil {
		t.Fatalf("read settings command: %v", err)
	}
	if command.Type != "set_display_settings" || command.Settings == nil || command.RequestID == "" {
		t.Fatalf("unexpected settings command: %+v", command)
	}
	if command.Settings.IPDMm != 68.5 || command.Settings.LensSeparationMm != 65.2 ||
		math.Abs(float64(command.Settings.DistortionK1-.455)) > 0.0001 ||
		math.Abs(float64(command.Settings.DistortionK2-(-.071))) > 0.0001 {
		t.Fatalf("manual Daydream optical tuning was not preserved: %+v", command.Settings)
	}

	if err := encoder.Encode(map[string]any{
		"type": "display_settings_ack", "requestId": command.RequestID,
		"status": "applied", "settings": command.Settings,
	}); err != nil {
		t.Fatalf("send applied acknowledgement: %v", err)
	}
	waitUntil(t, time.Second, func() bool {
		devices := service.Snapshot().ConnectedDevices
		return len(devices) == 1 && devices[0].DisplaySettings != nil && devices[0].DisplaySettings.IPDMm == 68.5
	})
}

func TestHostSynchronizesAndroidOpticalPresetsAndAppliesByID(t *testing.T) {
	config := HostConfig{
		ListenAddress: "127.0.0.1",
		DiscoveryPort: reservePort(t, "udp4"),
		ControlPort:   reservePort(t, "tcp4"),
		PosePort:      reservePort(t, "udp4"),
	}
	service := NewHostService(false)
	if err := service.Start(config); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	defer service.Stop()

	snapshot := service.Snapshot()
	conn, err := net.DialTimeout("tcp4", net.JoinHostPort("127.0.0.1", fmt.Sprint(snapshot.ControlPort)), time.Second)
	if err != nil {
		t.Fatalf("dial control listener: %v", err)
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(3 * time.Second))
	encoder := json.NewEncoder(conn)
	decoder := json.NewDecoder(bufio.NewReader(conn))
	if err := encoder.Encode(helloMessage{Type: "hello", Protocol: protocolVersion, DeviceName: "Optical Preset Test", Client: "PhoneVR-Test"}); err != nil {
		t.Fatalf("send hello: %v", err)
	}
	var welcome welcomeMessage
	if err := decoder.Decode(&welcome); err != nil {
		t.Fatalf("read welcome: %v", err)
	}
	if welcome.Type != "welcome" {
		t.Fatalf("unexpected welcome: %+v", welcome)
	}
	foundCapability := false
	for _, capability := range welcome.Capabilities {
		if capability == "android-optical-presets-v1" {
			foundCapability = true
		}
	}
	if !foundCapability {
		t.Fatalf("welcome missing android optical preset capability: %+v", welcome.Capabilities)
	}
	completeOpticalPresetPull(t, service, decoder, encoder)

	presets := nativePresetDefaults()
	presets = append(presets, OpticalPreset{
		ID: "test-custom", Name: "Custom Viewer", Settings: DefaultPhoneDisplaySettings(),
	})
	if err := service.SendPhoneOpticalPresets(presets); err != nil {
		t.Fatalf("SendPhoneOpticalPresets() error = %v", err)
	}
	var update controlMessage
	if err := decoder.Decode(&update); err != nil {
		t.Fatalf("read preset update: %v", err)
	}
	if update.Type != "set_optical_presets" || update.RequestID == "" || len(update.Presets) != 3 {
		t.Fatalf("unexpected preset update: %+v", update)
	}
	if err := encoder.Encode(controlMessage{Type: "optical_presets_ack", RequestID: update.RequestID, Status: "applied", Presets: update.Presets}); err != nil {
		t.Fatalf("send preset acknowledgement: %v", err)
	}
	waitUntil(t, time.Second, func() bool {
		devices := service.Snapshot().ConnectedDevices
		return len(devices) == 1 && len(devices[0].OpticalPresets) == 3
	})

	if err := service.ApplyPhoneOpticalPreset("test-custom"); err != nil {
		t.Fatalf("ApplyPhoneOpticalPreset() error = %v", err)
	}
	var apply controlMessage
	if err := decoder.Decode(&apply); err != nil {
		t.Fatalf("read apply command: %v", err)
	}
	if apply.Type != "apply_optical_preset" || apply.PresetID != "test-custom" || apply.RequestID == "" {
		t.Fatalf("unexpected apply command: %+v", apply)
	}
}

func TestHostRetriesOpticalPresetRequestWithoutDroppingControlSession(t *testing.T) {
	config := HostConfig{
		ListenAddress: "127.0.0.1",
		DiscoveryPort: reservePort(t, "udp4"),
		ControlPort:   reservePort(t, "tcp4"),
		PosePort:      reservePort(t, "udp4"),
	}
	service := NewHostService(false)
	if err := service.Start(config); err != nil {
		t.Skipf("Host listeners unavailable for preset retry test: %v", err)
	}
	defer service.Stop()

	conn, err := net.DialTimeout("tcp4", net.JoinHostPort("127.0.0.1", fmt.Sprint(service.Snapshot().ControlPort)), time.Second)
	if err != nil {
		t.Fatalf("dial control listener: %v", err)
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(7 * time.Second))
	encoder := json.NewEncoder(conn)
	decoder := json.NewDecoder(bufio.NewReader(conn))
	if err := encoder.Encode(helloMessage{Type: "hello", Protocol: protocolVersion, DeviceName: "Preset Retry Test"}); err != nil {
		t.Fatalf("send hello: %v", err)
	}
	var welcome welcomeMessage
	if err := decoder.Decode(&welcome); err != nil {
		t.Fatalf("read welcome: %v", err)
	}
	var first controlMessage
	if err := decoder.Decode(&first); err != nil {
		t.Fatalf("read initial preset request: %v", err)
	}
	if first.Type != "get_optical_presets" {
		t.Fatalf("expected initial preset request, got %+v", first)
	}

	// Simulate an ordinary control heartbeat after the retry deadline. The
	// server must keep the session alive and send another catalogue request.
	time.Sleep(2100 * time.Millisecond)
	if err := encoder.Encode(controlMessage{Type: "ping"}); err != nil {
		t.Fatalf("send heartbeat before retry: %v", err)
	}
	var pong map[string]any
	if err := decoder.Decode(&pong); err != nil || pong["type"] != "pong" {
		t.Fatalf("control heartbeat failed while waiting for presets: %#v, %v", pong, err)
	}
	var retry controlMessage
	if err := decoder.Decode(&retry); err != nil {
		t.Fatalf("read retried preset request: %v", err)
	}
	if retry.Type != "get_optical_presets" || retry.RequestID == "" || retry.RequestID == first.RequestID {
		t.Fatalf("unexpected retried preset request: %+v (first %+v)", retry, first)
	}

	presets := nativePresetDefaults()
	if err := encoder.Encode(controlMessage{Type: "optical_presets", RequestID: retry.RequestID, Presets: presets}); err != nil {
		t.Fatalf("send preset response: %v", err)
	}
	waitUntil(t, time.Second, func() bool {
		devices := service.Snapshot().ConnectedDevices
		return len(devices) == 1 && len(devices[0].OpticalPresets) == len(presets)
	})
}

func TestPhoneDisplaySettingsNormalizationClampsValues(t *testing.T) {
	settings := PhoneDisplaySettings{IPDMm: 100, Blur: -1, Sharpness: 4, FOVDegrees: 10}.Normalized()
	if settings.IPDMm != 75 || settings.Blur != 0 || settings.Sharpness != 1 || settings.FOVDegrees != 60 {
		t.Fatalf("settings normalization did not clamp values: %+v", settings)
	}
}

func TestDaydreamProfileAllowsManualOpticalCalibration(t *testing.T) {
	settings := PhoneDisplaySettings{
		ViewerProfileID: "daydream_view_2017", IPDMm: 66,
		LensSeparationMm: 65, DistortionK1: .44, DistortionK2: -.07,
	}.Normalized()
	if settings.ViewerProfileID != "daydream_view_2017" || settings.LensSeparationMm != 65 ||
		settings.DistortionK1 != .44 || settings.DistortionK2 != -.07 {
		t.Fatalf("manual Daydream calibration was overwritten: %+v", settings)
	}
}

func completeOpticalPresetPull(t *testing.T, service *HostService, decoder *json.Decoder, encoder *json.Encoder) {
	t.Helper()
	var request controlMessage
	if err := decoder.Decode(&request); err != nil {
		t.Fatalf("read Android optical preset request: %v", err)
	}
	if request.Type != "get_optical_presets" || request.RequestID == "" {
		t.Fatalf("unexpected initial control message, want get_optical_presets: %+v", request)
	}
	presets := nativePresetDefaults()
	if err := encoder.Encode(map[string]any{"type": "optical_presets", "requestId": request.RequestID, "presets": presets}); err != nil {
		t.Fatalf("reply with local Android optical presets: %v", err)
	}
	waitUntil(t, time.Second, func() bool {
		devices := service.Snapshot().ConnectedDevices
		return len(devices) == 1 && len(devices[0].OpticalPresets) == len(presets)
	})
}

func TestPoseUpdateDoesNotWaitForGeneralServiceMutex(t *testing.T) {
	config := HostConfig{
		ListenAddress: "127.0.0.1",
		DiscoveryPort: reservePort(t, "udp4"),
		ControlPort:   reservePort(t, "tcp4"),
		PosePort:      reservePort(t, "udp4"),
	}
	service := NewHostService(false)
	if err := service.Start(config); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	defer service.Stop()
	posePort := service.Snapshot().PosePort
	sender, err := net.DialUDP("udp4", nil, &net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: posePort})
	if err != nil {
		t.Fatalf("DialUDP() error = %v", err)
	}
	defer sender.Close()

	service.mu.Lock()
	unlocked := false
	defer func() {
		if !unlocked {
			service.mu.Unlock()
		}
	}()
	if _, err := sender.Write(testPosePacket()); err != nil {
		t.Fatalf("send pose packet: %v", err)
	}
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if pose := service.PoseFrame(); pose.PacketsReceived == 1 && pose.LatestPose != nil {
			service.mu.Unlock()
			unlocked = true
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("pose receiver waited for the general Host service mutex")
}

func TestClassifyHostInterface(t *testing.T) {
	cases := map[string]string{
		"Wi-Fi":                      "Wi-Fi",
		"wlan0":                      "Wi-Fi",
		"Ethernet 3":                 "Ethernet",
		"eth0":                       "Ethernet",
		"vEthernet (Default Switch)": "Network",
		"Other Adapter":              "Network",
	}
	for name, expected := range cases {
		if actual := classifyHostInterface(name); actual != expected {
			t.Errorf("classifyHostInterface(%q) = %q, want %q", name, actual, expected)
		}
	}
}
