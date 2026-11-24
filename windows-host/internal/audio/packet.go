// Package audio defines the low-latency PhoneVR PCM transport frame.
// Audio uses an independent protocol and never enters the H.264 video path.
package audio

import (
	"encoding/binary"
	"errors"
	"fmt"
)

const (
	ProtocolVersion = 1
	HeaderSize      = 24
	SampleRate      = 48000
	FrameDurationMs = 20
	SamplesPerFrame = SampleRate * FrameDurationMs / 1000
	MaxPayloadSize  = SamplesPerFrame * 2 * 2 // stereo signed 16-bit PCM
	FormatPCM16LE   = 1
)

type Direction byte

const (
	DirectionHostToPhone Direction = 1
	DirectionPhoneToHost Direction = 2
)

var magic = [4]byte{'P', 'V', 'R', 'A'}

// Packet is one 20 ms PCM audio access unit. TimestampNs is a sender-local
// monotonic timestamp and must not be compared directly with the peer's clock.
type Packet struct {
	Direction    Direction
	Channels     uint8
	Sequence     uint32
	TimestampNs  uint64
	SamplesPerCh uint16
	Payload      []byte
}

func Encode(packet Packet) ([]byte, error) {
	if packet.Direction != DirectionHostToPhone && packet.Direction != DirectionPhoneToHost {
		return nil, fmt.Errorf("invalid audio direction %d", packet.Direction)
	}
	if packet.Channels != 1 && packet.Channels != 2 {
		return nil, fmt.Errorf("unsupported audio channel count %d", packet.Channels)
	}
	if packet.SamplesPerCh != SamplesPerFrame {
		return nil, fmt.Errorf("audio frame must contain %d samples per channel", SamplesPerFrame)
	}
	wantPayload := int(packet.SamplesPerCh) * int(packet.Channels) * 2
	if len(packet.Payload) != wantPayload || len(packet.Payload) > MaxPayloadSize {
		return nil, fmt.Errorf("invalid PCM payload size %d, expected %d", len(packet.Payload), wantPayload)
	}
	wire := make([]byte, HeaderSize+len(packet.Payload))
	copy(wire[:4], magic[:])
	wire[4] = ProtocolVersion
	wire[5] = byte(packet.Direction)
	wire[6] = packet.Channels
	wire[7] = FormatPCM16LE
	binary.LittleEndian.PutUint32(wire[8:12], packet.Sequence)
	binary.LittleEndian.PutUint64(wire[12:20], packet.TimestampNs)
	binary.LittleEndian.PutUint16(wire[20:22], packet.SamplesPerCh)
	binary.LittleEndian.PutUint16(wire[22:24], uint16(len(packet.Payload)))
	copy(wire[HeaderSize:], packet.Payload)
	return wire, nil
}

func Decode(wire []byte) (Packet, error) {
	var packet Packet
	if len(wire) < HeaderSize {
		return packet, errors.New("audio packet is shorter than its header")
	}
	if string(wire[:4]) != string(magic[:]) {
		return packet, errors.New("invalid audio packet signature")
	}
	if wire[4] != ProtocolVersion {
		return packet, fmt.Errorf("unsupported audio protocol version %d", wire[4])
	}
	packet.Direction = Direction(wire[5])
	if packet.Direction != DirectionHostToPhone && packet.Direction != DirectionPhoneToHost {
		return Packet{}, fmt.Errorf("invalid audio direction %d", packet.Direction)
	}
	packet.Channels = wire[6]
	if packet.Channels != 1 && packet.Channels != 2 {
		return Packet{}, fmt.Errorf("unsupported audio channel count %d", packet.Channels)
	}
	if wire[7] != FormatPCM16LE {
		return Packet{}, fmt.Errorf("unsupported audio sample format %d", wire[7])
	}
	packet.Sequence = binary.LittleEndian.Uint32(wire[8:12])
	packet.TimestampNs = binary.LittleEndian.Uint64(wire[12:20])
	packet.SamplesPerCh = binary.LittleEndian.Uint16(wire[20:22])
	payloadSize := int(binary.LittleEndian.Uint16(wire[22:24]))
	if packet.SamplesPerCh != SamplesPerFrame {
		return Packet{}, fmt.Errorf("invalid samples per channel %d", packet.SamplesPerCh)
	}
	wantPayload := int(packet.SamplesPerCh) * int(packet.Channels) * 2
	if payloadSize != wantPayload || len(wire) != HeaderSize+payloadSize {
		return Packet{}, fmt.Errorf("invalid audio payload length %d", payloadSize)
	}
	packet.Payload = append([]byte(nil), wire[HeaderSize:]...)
	return packet, nil
}
