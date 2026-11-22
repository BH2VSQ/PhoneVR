package main

import (
	"errors"
	"net"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/vsq-studio/phonevr-host/internal/usbmux"
)

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

func TestManualVideoTransportModesDoNotUseTheOtherPath(t *testing.T) {
	service := NewHostService(false)
	addVideoTransportTestDevice(t, service, "phone-1", "USB-AOA:test", "USB AOA Bulk")
	addVideoTransportTestDevice(t, service, "phone-1", "192.0.2.10:50000", "Wi-Fi / LAN")

	if err := service.SetVideoTransport(string(VideoTransportNetwork), string(VideoPreferUSB)); err != nil {
		t.Fatal(err)
	}
	target, ok := service.currentVideoTarget()
	if !ok || target.Transport != "network" {
		t.Fatalf("network-only target = %+v, enabled=%t", target, ok)
	}

	if err := service.SetVideoTransport(string(VideoTransportUSB), string(VideoPreferNetwork)); err != nil {
		t.Fatal(err)
	}
	target, ok = service.currentVideoTarget()
	if !ok || target.Transport != "usb" {
		t.Fatalf("USB-only target = %+v, enabled=%t", target, ok)
	}
}

func TestActiveVideoTransportRequiresRecentEndToEndFeedback(t *testing.T) {
	service := NewHostService(false)
	addVideoTransportTestDevice(t, service, "phone-1", "USB-AOA:test", "USB AOA Bulk")
	addVideoTransportTestDevice(t, service, "phone-1", "192.0.2.10:50000", "Wi-Fi / LAN")
	service.transportPreference = VideoPreferNetwork

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
	addVideoTransportTestDevice(t, service, "phone-1", "USB-AOA:test", "USB AOA Bulk")
	addVideoTransportTestDevice(t, service, "phone-1", "192.0.2.10:50000", "Wi-Fi / LAN")
	service.transportPreference = VideoPreferNetwork
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
