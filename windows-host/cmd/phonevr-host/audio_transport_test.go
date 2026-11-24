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
	buffer := make([]byte, phonevraudio.HeaderSize+phonevraudio.MaxPayloadSize)
	n, _, err := receiver.ReadFromUDP(buffer)
	if err != nil {
		t.Fatal(err)
	}
	packet, err := phonevraudio.Decode(buffer[:n])
	if err != nil {
		t.Fatal(err)
	}
	if packet.Direction != phonevraudio.DirectionHostToPhone || packet.Channels != 2 || !bytes.Equal(packet.Payload, pcm) {
		t.Fatal("published audio packet does not match the PCM source")
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
