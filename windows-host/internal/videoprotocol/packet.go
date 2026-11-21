// Package videoprotocol defines the first PhoneVR low-latency video datagram
// envelope. Encoded H.264/HEVC/AV1 access units are fragmented into bounded
// UDP payloads; a future receiver reassembles only complete, current frames.
package videoprotocol

import (
	"encoding/binary"
	"errors"
)

const (
	HeaderSize      = 32
	ProtocolVersion = 1
	MaxDatagramSize = 1232 // conservative IPv6-safe UDP datagram size
	MaxPayloadSize  = MaxDatagramSize - HeaderSize
	MaxEncodedFrame = 64 * 1024 * 1024
)

const (
	FlagFrameStart uint8 = 1 << iota
	FlagFrameEnd
	FlagKeyFrame
)

var magic = [4]byte{'P', 'V', 'R', 'V'}

// Packet describes one fragment of one encoded side-by-side stereo frame.
// Payload is not copied by Decode; it references the supplied datagram.
type Packet struct {
	Flags          uint8
	FrameID        uint32
	TimestampNs    uint64
	FrameSize      uint32
	FragmentOffset uint32
	Payload        []byte
}

func (p Packet) Encode() ([]byte, error) {
	payloadLen := len(p.Payload)
	if payloadLen == 0 || payloadLen > MaxPayloadSize {
		return nil, errors.New("invalid video fragment payload size")
	}
	if p.FrameSize == 0 || p.FrameSize > MaxEncodedFrame {
		return nil, errors.New("invalid encoded frame size")
	}
	if uint64(p.FragmentOffset)+uint64(payloadLen) > uint64(p.FrameSize) {
		return nil, errors.New("video fragment exceeds frame boundary")
	}
	if p.Flags & ^uint8(FlagFrameStart|FlagFrameEnd|FlagKeyFrame) != 0 {
		return nil, errors.New("unknown video packet flags")
	}
	out := make([]byte, HeaderSize+payloadLen)
	copy(out[:4], magic[:])
	out[4] = ProtocolVersion
	out[5] = p.Flags
	binary.BigEndian.PutUint16(out[6:8], HeaderSize)
	binary.BigEndian.PutUint32(out[8:12], p.FrameID)
	binary.BigEndian.PutUint64(out[12:20], p.TimestampNs)
	binary.BigEndian.PutUint32(out[20:24], p.FrameSize)
	binary.BigEndian.PutUint32(out[24:28], p.FragmentOffset)
	binary.BigEndian.PutUint16(out[28:30], uint16(payloadLen))
	copy(out[HeaderSize:], p.Payload)
	return out, nil
}

// FragmentFrame splits one encoded access unit into MTU-safe UDP packets. The
// key-frame bit is repeated on every fragment so a receiver can classify a
// frame even when its first fragment was lost. The caller should drop the
// entire access unit if any fragment cannot be sent.
func FragmentFrame(frame []byte, frameID uint32, timestampNs uint64, keyFrame bool) ([][]byte, error) {
	if len(frame) == 0 || len(frame) > MaxEncodedFrame {
		return nil, errors.New("invalid encoded frame size")
	}
	fragments := make([][]byte, 0, (len(frame)+MaxPayloadSize-1)/MaxPayloadSize)
	for offset := 0; offset < len(frame); {
		end := offset + MaxPayloadSize
		if end > len(frame) {
			end = len(frame)
		}
		flags := uint8(0)
		if offset == 0 {
			flags |= FlagFrameStart
		}
		if end == len(frame) {
			flags |= FlagFrameEnd
		}
		if keyFrame {
			flags |= FlagKeyFrame
		}
		packet := Packet{
			Flags: flags, FrameID: frameID, TimestampNs: timestampNs,
			FrameSize: uint32(len(frame)), FragmentOffset: uint32(offset),
			Payload: frame[offset:end],
		}
		wire, err := packet.Encode()
		if err != nil {
			return nil, err
		}
		fragments = append(fragments, wire)
		offset = end
	}
	return fragments, nil
}

func Decode(data []byte) (Packet, error) {
	if len(data) < HeaderSize || len(data) > MaxDatagramSize {
		return Packet{}, errors.New("invalid video datagram size")
	}
	if string(data[:4]) != string(magic[:]) {
		return Packet{}, errors.New("invalid video packet magic")
	}
	if data[4] != ProtocolVersion {
		return Packet{}, errors.New("unsupported video packet version")
	}
	if binary.BigEndian.Uint16(data[6:8]) != HeaderSize {
		return Packet{}, errors.New("unsupported video header size")
	}
	if binary.BigEndian.Uint16(data[30:32]) != 0 {
		return Packet{}, errors.New("reserved video header field must be zero")
	}
	payloadLen := int(binary.BigEndian.Uint16(data[28:30]))
	if payloadLen == 0 || payloadLen != len(data)-HeaderSize {
		return Packet{}, errors.New("video payload length mismatch")
	}
	p := Packet{
		Flags: data[5], FrameID: binary.BigEndian.Uint32(data[8:12]),
		TimestampNs: binary.BigEndian.Uint64(data[12:20]), FrameSize: binary.BigEndian.Uint32(data[20:24]),
		FragmentOffset: binary.BigEndian.Uint32(data[24:28]), Payload: data[HeaderSize:],
	}
	if p.Flags & ^uint8(FlagFrameStart|FlagFrameEnd|FlagKeyFrame) != 0 {
		return Packet{}, errors.New("unknown video packet flags")
	}
	if p.FrameSize == 0 || p.FrameSize > MaxEncodedFrame {
		return Packet{}, errors.New("invalid encoded frame size")
	}
	if uint64(p.FragmentOffset)+uint64(payloadLen) > uint64(p.FrameSize) {
		return Packet{}, errors.New("video fragment exceeds frame boundary")
	}
	if p.Flags&FlagFrameStart != 0 && p.FragmentOffset != 0 {
		return Packet{}, errors.New("start fragment has nonzero offset")
	}
	if p.Flags&FlagFrameEnd != 0 && p.FragmentOffset+uint32(payloadLen) != p.FrameSize {
		return Packet{}, errors.New("end fragment does not terminate frame")
	}
	return p, nil
}
