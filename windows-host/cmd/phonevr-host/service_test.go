package main

import (
	"bufio"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"math"
	"net"
	"strings"
	"testing"
	"time"
)

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
	if err := encoder.Encode(helloMessage{Type: "hello", Protocol: protocolVersion, DeviceName: "Test Android", Client: "PhoneVRStream-Test"}); err != nil {
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
