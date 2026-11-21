package videoprotocol

import (
	"encoding/binary"
	"errors"
	"fmt"
)

const (
	RawFrameHeaderSize = 32
	RawFrameVersion    = 1
	RawFrameMagic      = "PVRC"
	RawFrameMaxWidth   = 4096
	RawFrameMaxHeight  = 2160
	RawFrameMaxBytes   = RawFrameMaxWidth * RawFrameMaxHeight * 4
)

// RawFrameHeader describes a BGRA frame captured by the SteamVR driver and
// streamed over the loopback-only TCP capture bridge to the Windows Host.
type RawFrameHeader struct {
	FrameID     uint32
	TimestampNs uint64
	Width       uint16
	Height      uint16
	Stride      uint32
	PayloadSize uint32
}

func (h RawFrameHeader) Validate() error {
	if h.Width == 0 || h.Height == 0 || h.Width > RawFrameMaxWidth || h.Height > RawFrameMaxHeight {
		return errors.New("raw video dimensions are outside supported bounds")
	}
	minimumStride := uint64(h.Width) * 4
	if uint64(h.Stride) < minimumStride {
		return errors.New("raw video stride is smaller than BGRA row size")
	}
	want := uint64(h.Stride) * uint64(h.Height)
	if want == 0 || want > RawFrameMaxBytes || uint64(h.PayloadSize) != want {
		return fmt.Errorf("invalid raw video payload size: got %d, want %d", h.PayloadSize, want)
	}
	return nil
}

func (h RawFrameHeader) Encode() ([]byte, error) {
	if err := h.Validate(); err != nil {
		return nil, err
	}
	out := make([]byte, RawFrameHeaderSize)
	copy(out[:4], RawFrameMagic)
	out[4] = RawFrameVersion
	out[5] = 1 // BGRA8 pixel format.
	binary.BigEndian.PutUint16(out[6:8], RawFrameHeaderSize)
	binary.BigEndian.PutUint32(out[8:12], h.FrameID)
	binary.BigEndian.PutUint64(out[12:20], h.TimestampNs)
	binary.BigEndian.PutUint16(out[20:22], h.Width)
	binary.BigEndian.PutUint16(out[22:24], h.Height)
	binary.BigEndian.PutUint32(out[24:28], h.Stride)
	binary.BigEndian.PutUint32(out[28:32], h.PayloadSize)
	return out, nil
}

func DecodeRawFrameHeader(data []byte) (RawFrameHeader, error) {
	if len(data) != RawFrameHeaderSize {
		return RawFrameHeader{}, errors.New("invalid raw video header size")
	}
	if string(data[:4]) != RawFrameMagic || data[4] != RawFrameVersion || data[5] != 1 {
		return RawFrameHeader{}, errors.New("unsupported raw video header or pixel format")
	}
	if binary.BigEndian.Uint16(data[6:8]) != RawFrameHeaderSize {
		return RawFrameHeader{}, errors.New("unsupported raw video header version")
	}
	h := RawFrameHeader{
		FrameID:     binary.BigEndian.Uint32(data[8:12]),
		TimestampNs: binary.BigEndian.Uint64(data[12:20]),
		Width:       binary.BigEndian.Uint16(data[20:22]),
		Height:      binary.BigEndian.Uint16(data[22:24]),
		Stride:      binary.BigEndian.Uint32(data[24:28]),
		PayloadSize: binary.BigEndian.Uint32(data[28:32]),
	}
	return h, h.Validate()
}
