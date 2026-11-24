package main

import (
	"bytes"
	"net"
	"testing"
	"time"

	phonevraudio "github.com/vsq-studio/phonevr-host/internal/audio"
)

func TestPublishAudioPCMUsesIndependentUDPPort(t *testing.T) {
	receiver, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 0})
	if err != nil {
		t.Fatal(err)
	}
	defer receiver.Close()
	sender, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 0})
	if err != nil {
		t.Fatal(err)
	}
	defer sender.Close()

	service := &HostService{
		audioUDP:            sender,
		audioClockStartedAt: time.Now(),
		devices: map[string]activeDevice{
			"phone": {
				Snapshot: DeviceSnapshot{Address: "127.0.0.1:49123"},
				Ready:    true,
				AudioUDP: receiver.LocalAddr().(*net.UDPAddr),
			},
		},
	}
	pcm := bytes.Repeat([]byte{0x2a}, phonevraudio.MaxPayloadSize)
	if err := service.PublishAudioPCM(pcm); err != nil {
		t.Fatal(err)
	}
	_ = receiver.SetReadDeadline(time.Now().Add(time.Second))
	buffer := make([]byte, phonevraudio.MaxUDPDatagramSize)
	reassembler := phonevraudio.NewReassembler()
	var frame []byte
	for frame == nil {
		n, _, readErr := receiver.ReadFromUDP(buffer)
		if readErr != nil {
			t.Fatal(readErr)
		}
		completed, complete, addErr := reassembler.Add(buffer[:n], time.Now())
		if addErr != nil {
			t.Fatal(addErr)
		}
		if complete {
			frame = completed
		}
	}
	packet, err := phonevraudio.Decode(frame)
	if err != nil {
		t.Fatal(err)
	}
	if packet.Direction != phonevraudio.DirectionHostToPhone || packet.Channels != 2 || !bytes.Equal(packet.Payload, pcm) {
		t.Fatal("published audio fragments do not reconstruct the PCM source")
	}
}

func TestAcceptAudioUplinkPublishesValidatedFrames(t *testing.T) {
	service := &HostService{
		audioUplink: make(chan phonevraudio.Packet, 2),
		devices:     make(map[string]activeDevice),
	}
	pcm := bytes.Repeat([]byte{0x17}, phonevraudio.SamplesPerFrame*2)
	wire, err := phonevraudio.Encode(phonevraudio.Packet{
		Direction:    phonevraudio.DirectionPhoneToHost,
		Channels:     1,
		Sequence:     12,
		SamplesPerCh: phonevraudio.SamplesPerFrame,
		Payload:      pcm,
	})
	if err != nil {
		t.Fatal(err)
	}
	service.acceptAudioUplink("usb-phone", nil, wire)
	select {
	case got := <-service.AudioUplinkFrames():
		if got.Direction != phonevraudio.DirectionPhoneToHost || got.Sequence != 12 || !bytes.Equal(got.Payload, pcm) {
			t.Fatal("uplink frame was not preserved")
		}
	default:
		t.Fatal("uplink frame was not queued")
	}
	service.acceptAudioUplink("usb-phone", nil, []byte("invalid"))
	if got := service.audioUplinkPackets.Load(); got != 1 {
		t.Fatalf("expected one accepted audio frame, got %d", got)
	}
}

func TestAudioUDPReassemblySeparatesPeersWithMatchingSequences(t *testing.T) {
	makeFragments := func(value byte) [][]byte {
		frame, err := phonevraudio.Encode(phonevraudio.Packet{
			Direction:    phonevraudio.DirectionPhoneToHost,
			Channels:     1,
			Sequence:     55,
			SamplesPerCh: phonevraudio.SamplesPerFrame,
			Payload:      bytes.Repeat([]byte{value}, phonevraudio.SamplesPerFrame*2),
		})
		if err != nil {
			t.Fatal(err)
		}
		fragments, err := phonevraudio.Fragment(frame)
		if err != nil {
			t.Fatal(err)
		}
		return fragments
	}
	first := makeFragments(0x11)
	second := makeFragments(0x22)
	var peers audioUDPReassemblySet
	now := time.Now()
	for peer, fragments := range map[string][][]byte{"192.0.2.1:40001": first, "192.0.2.1:40002": second} {
		for _, fragment := range fragments[:1] {
			if _, complete, err := peers.Add(peer, fragment, now); err != nil || complete {
				t.Fatalf("first fragment for %s: complete=%v err=%v", peer, complete, err)
			}
		}
	}
	for peer, expected := range map[string]struct {
		fragments [][]byte
		value     byte
	}{
		"192.0.2.1:40001": {fragments: first, value: 0x11},
		"192.0.2.1:40002": {fragments: second, value: 0x22},
	} {
		frame, complete, err := peers.Add(peer, expected.fragments[1], now.Add(time.Millisecond))
		if err != nil || !complete {
			t.Fatalf("complete frame for %s: complete=%v err=%v", peer, complete, err)
		}
		packet, err := phonevraudio.Decode(frame)
		if err != nil {
			t.Fatal(err)
		}
		if packet.Payload[0] != expected.value {
			t.Fatalf("peer %s received payload marker 0x%02x, want 0x%02x", peer, packet.Payload[0], expected.value)
		}
	}
}
