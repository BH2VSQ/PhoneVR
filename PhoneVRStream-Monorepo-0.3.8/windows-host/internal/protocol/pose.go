package protocol

import (
	"encoding/binary"
	"errors"
	"math"
)

const (
	PosePacketSize = 36
	PoseVersion    = 1
)

var magic = [4]byte{'P', 'V', 'S', 'P'}

type Pose struct {
	Sequence    uint32
	TimestampNs int64
	X           float32
	Y           float32
	Z           float32
	W           float32
}

func DecodePosePacket(data []byte) (Pose, error) {
	if len(data) != PosePacketSize {
		return Pose{}, errors.New("invalid pose packet size")
	}
	if string(data[:4]) != string(magic[:]) {
		return Pose{}, errors.New("invalid pose packet magic")
	}
	if data[4] != PoseVersion {
		return Pose{}, errors.New("unsupported pose packet version")
	}
	pose := Pose{
		Sequence:    binary.BigEndian.Uint32(data[8:12]),
		TimestampNs: int64(binary.BigEndian.Uint64(data[12:20])),
		X:           math.Float32frombits(binary.BigEndian.Uint32(data[20:24])),
		Y:           math.Float32frombits(binary.BigEndian.Uint32(data[24:28])),
		Z:           math.Float32frombits(binary.BigEndian.Uint32(data[28:32])),
		W:           math.Float32frombits(binary.BigEndian.Uint32(data[32:36])),
	}
	values := []float32{pose.X, pose.Y, pose.Z, pose.W}
	var normSquared float32
	for _, value := range values {
		if math.IsNaN(float64(value)) || math.IsInf(float64(value), 0) {
			return Pose{}, errors.New("pose quaternion contains non-finite value")
		}
		normSquared += value * value
	}
	norm := float32(math.Sqrt(float64(normSquared)))
	if norm < 0.5 || norm > 1.5 {
		return Pose{}, errors.New("pose quaternion is not normalized")
	}
	return pose, nil
}
