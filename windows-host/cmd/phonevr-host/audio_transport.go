package main

import (
	"context"
	"errors"
	"fmt"
	"net"
	"strings"
	"time"

	phonevraudio "github.com/vsq-studio/phonevr-host/internal/audio"
	"github.com/vsq-studio/phonevr-host/internal/usbmux"
)

// AudioUplinkFrames exposes phone microphone access units to the Windows audio
// endpoint worker. The channel is bounded; stale speech frames are discarded
// rather than allowing the audio path to accumulate latency.
func (s *HostService) AudioUplinkFrames() <-chan phonevraudio.Packet {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.audioUplink
}

// PublishAudioPCM sends one 20 ms, 48 kHz, stereo signed-16-bit PCM frame to
// every active non-auxiliary Android session. A Windows WASAPI loopback worker
// can feed this API without touching the video encoder or pose transport.
func (s *HostService) PublishAudioPCM(pcm []byte) error {
	s.mu.RLock()
	clockStartedAt := s.audioClockStartedAt
	audioUDP := s.audioUDP
	devices := make([]activeDevice, 0, len(s.devices))
	for _, device := range s.devices {
		if device.Ready && !device.VideoOnly {
			devices = append(devices, device)
		}
	}
	s.mu.RUnlock()

	packet, err := phonevraudio.Encode(phonevraudio.Packet{
		Direction:    phonevraudio.DirectionHostToPhone,
		Channels:     2,
		Sequence:     s.audioSequence.Add(1) - 1,
		TimestampNs:  uint64(time.Since(clockStartedAt).Nanoseconds()),
		SamplesPerCh: phonevraudio.SamplesPerFrame,
		Payload:      pcm,
	})
	if err != nil {
		return err
	}
	if len(devices) == 0 {
		return errors.New("no active PhoneVR audio session")
	}

	var sendErrors []string
	sent := 0
	for _, device := range devices {
		if device.USBLink != nil {
			if err := device.USBLink.WriteFrame(usbmux.ChannelAudioDownlink, packet); err != nil {
				sendErrors = append(sendErrors, err.Error())
				continue
			}
			sent++
			continue
		}
		if audioUDP == nil || device.AudioUDP == nil {
			continue // Wait for the phone's first microphone packet to learn its UDP port.
		}
		if _, err := audioUDP.WriteToUDP(packet, device.AudioUDP); err != nil {
			sendErrors = append(sendErrors, err.Error())
			continue
		}
		sent++
	}
	if sent == 0 {
		if len(sendErrors) > 0 {
			return fmt.Errorf("audio frame send failed: %s", strings.Join(sendErrors, "; "))
		}
		return errors.New("audio session has not established a return UDP address")
	}
	s.audioDownlinkPackets.Add(1)
	return nil
}

func (s *HostService) audioLoop(ctx context.Context, conn *net.UDPConn) {
	defer s.wg.Done()
	buffer := make([]byte, phonevraudio.HeaderSize+phonevraudio.MaxPayloadSize+1)
	for ctx.Err() == nil {
		_ = conn.SetReadDeadline(time.Now().Add(500 * time.Millisecond))
		n, remote, err := conn.ReadFromUDP(buffer)
		if err != nil {
			if ctx.Err() != nil || errors.Is(err, net.ErrClosed) {
				return
			}
			if timeout, ok := err.(net.Error); ok && timeout.Timeout() {
				continue
			}
			s.addLog("Audio UDP read failed: " + err.Error())
			continue
		}
		packet, decodeErr := phonevraudio.Decode(buffer[:n])
		if decodeErr != nil || packet.Direction != phonevraudio.DirectionPhoneToHost || packet.Channels != 1 {
			continue
		}
		s.acceptAudioUplink("", remote, buffer[:n])
	}
}

func (s *HostService) acceptAudioUplink(deviceKey string, remote *net.UDPAddr, wire []byte) {
	packet, err := phonevraudio.Decode(wire)
	if err != nil || packet.Direction != phonevraudio.DirectionPhoneToHost || packet.Channels != 1 {
		return
	}
	s.mu.Lock()
	if deviceKey != "" {
		if current, ok := s.devices[deviceKey]; ok {
			current.AudioUDP = nil
			s.devices[deviceKey] = current
		}
	} else if remote != nil {
		for key, current := range s.devices {
			if current.VideoOnly {
				continue
			}
			remoteAddress, _, splitErr := net.SplitHostPort(current.Snapshot.Address)
			if splitErr == nil && remoteAddress == remote.IP.String() {
				copyAddress := *remote
				current.AudioUDP = &copyAddress
				s.devices[key] = current
			}
		}
	}
	queue := s.audioUplink
	s.mu.Unlock()
	if queue == nil {
		return
	}
	select {
	case queue <- packet:
	default:
		// Drop one stale frame before retrying, keeping the return path bounded.
		select {
		case <-queue:
		default:
		}
		select {
		case queue <- packet:
		default:
		}
	}
	s.audioUplinkPackets.Add(1)
}
