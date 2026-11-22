package usbmux

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
)

const (
	ProtocolVersion byte = 1
	HeaderSize           = 16
	MaxPayloadSize       = 1024 * 1024

	ChannelControl byte = 1
	ChannelPose    byte = 2
	ChannelVideo   byte = 3
)

var frameMagic = [4]byte{'P', 'V', 'R', 'U'}

// Frame carries one logical message over the AOA bulk byte stream. USB bulk
// packet boundaries are deliberately ignored; the header provides framing.
type Frame struct {
	Channel  byte
	Sequence uint32
	Payload  []byte
}

func WriteFrame(w io.Writer, frame Frame) error {
	if frame.Channel < ChannelControl || frame.Channel > ChannelVideo {
		return fmt.Errorf("invalid USB multiplex channel %d", frame.Channel)
	}
	if len(frame.Payload) > MaxPayloadSize {
		return fmt.Errorf("USB multiplex payload too large: %d", len(frame.Payload))
	}
	var header [HeaderSize]byte
	copy(header[:4], frameMagic[:])
	header[4] = ProtocolVersion
	header[5] = frame.Channel
	binary.LittleEndian.PutUint32(header[8:12], uint32(len(frame.Payload)))
	binary.LittleEndian.PutUint32(header[12:16], frame.Sequence)
	wire := make([]byte, HeaderSize+len(frame.Payload))
	copy(wire[:HeaderSize], header[:])
	copy(wire[HeaderSize:], frame.Payload)
	return writeAll(w, wire)
}

func ReadFrame(r io.Reader) (Frame, error) {
	var frame Frame
	var header [HeaderSize]byte
	if _, err := io.ReadFull(r, header[:]); err != nil {
		return frame, err
	}
	if string(header[:4]) != string(frameMagic[:]) {
		return frame, errors.New("invalid USB multiplex frame magic")
	}
	if header[4] != ProtocolVersion {
		return frame, fmt.Errorf("unsupported USB multiplex version %d", header[4])
	}
	frame.Channel = header[5]
	if header[6] != 0 || header[7] != 0 {
		return frame, errors.New("unsupported USB multiplex flags")
	}
	if frame.Channel < ChannelControl || frame.Channel > ChannelVideo {
		return frame, fmt.Errorf("invalid USB multiplex channel %d", frame.Channel)
	}
	length := binary.LittleEndian.Uint32(header[8:12])
	if length > MaxPayloadSize {
		return frame, fmt.Errorf("USB multiplex frame length %d exceeds limit", length)
	}
	frame.Sequence = binary.LittleEndian.Uint32(header[12:16])
	frame.Payload = make([]byte, int(length))
	if _, err := io.ReadFull(r, frame.Payload); err != nil {
		return Frame{}, err
	}
	return frame, nil
}

func writeAll(w io.Writer, data []byte) error {
	for len(data) > 0 {
		n, err := w.Write(data)
		if err != nil {
			return err
		}
		if n <= 0 {
			return io.ErrShortWrite
		}
		data = data[n:]
	}
	return nil
}
