package protocol

import (
	"encoding/binary"
	"errors"
	"math"
)

const (
	ControllerPacketSize = 44
	ControllerHandLeft   = 0
	ControllerHandRight  = 1
)

// ControllerState is one normalized Daydream View remote snapshot.
// TimestampNs uses Android's monotonic clock domain and is diagnostic only on Host.
type ControllerState struct {
	Connected      bool    `json:"connected"`
	TimestampNs    uint64  `json:"timestampNs"`
	PacketSequence uint32  `json:"packetSequence"`
	Touching       bool    `json:"touching"`
	TouchX         float32 `json:"touchX"`
	TouchY         float32 `json:"touchY"`
	Click          bool    `json:"click"`
	App            bool    `json:"app"`
	Home           bool    `json:"home"`
	VolumeUp       bool    `json:"volumeUp"`
	VolumeDown     bool    `json:"volumeDown"`
	OrientationX   float32 `json:"orientationX"`
	OrientationY   float32 `json:"orientationY"`
	OrientationZ   float32 `json:"orientationZ"`
	OrientationW   float32 `json:"orientationW"`
	HasOrientation bool    `json:"hasOrientation"`
}

type ControllerFrame struct {
	Left  ControllerState `json:"left"`
	Right ControllerState `json:"right"`
}

// EncodeControllerPacket serializes one hand into the fixed-size loopback PVSC protocol.
// Layout: magic/version/hand/flags/buttons, sequence, timestamp, quaternion, touch X/Y.
func EncodeControllerPacket(hand byte, state ControllerState) ([]byte, error) {
	if hand != ControllerHandLeft && hand != ControllerHandRight {
		return nil, errors.New("invalid controller hand")
	}
	values := []float32{state.TouchX, state.TouchY, state.OrientationX, state.OrientationY, state.OrientationZ, state.OrientationW}
	for _, value := range values {
		if math.IsNaN(float64(value)) || math.IsInf(float64(value), 0) {
			return nil, errors.New("controller state contains a non-finite value")
		}
	}
	touchX := clampUnit(state.TouchX)
	touchY := clampUnit(state.TouchY)
	qx, qy, qz, qw := state.OrientationX, state.OrientationY, state.OrientationZ, state.OrientationW
	if state.HasOrientation {
		norm := math.Sqrt(float64(qx*qx + qy*qy + qz*qz + qw*qw))
		if norm < 0.5 || norm > 1.5 {
			return nil, errors.New("controller quaternion norm is outside the accepted range")
		}
		qx, qy, qz, qw = float32(float64(qx)/norm), float32(float64(qy)/norm), float32(float64(qz)/norm), float32(float64(qw)/norm)
	} else {
		qx, qy, qz, qw = 0, 0, 0, 1
	}

	packet := make([]byte, ControllerPacketSize)
	copy(packet[0:4], "PVSC")
	packet[4] = 1
	packet[5] = hand
	if state.Connected {
		packet[6] |= 1 << 0
	}
	if state.Touching {
		packet[6] |= 1 << 1
	}
	if state.HasOrientation {
		packet[6] |= 1 << 2
	}
	if state.Click {
		packet[7] |= 1 << 0
	}
	if state.App {
		packet[7] |= 1 << 1
	}
	if state.Home {
		packet[7] |= 1 << 2
	}
	if state.VolumeUp {
		packet[7] |= 1 << 3
	}
	if state.VolumeDown {
		packet[7] |= 1 << 4
	}
	binary.BigEndian.PutUint32(packet[8:12], state.PacketSequence)
	binary.BigEndian.PutUint64(packet[12:20], state.TimestampNs)
	putFloat32(packet[20:24], qx)
	putFloat32(packet[24:28], qy)
	putFloat32(packet[28:32], qz)
	putFloat32(packet[32:36], qw)
	putFloat32(packet[36:40], touchX)
	putFloat32(packet[40:44], touchY)
	return packet, nil
}

func clampUnit(value float32) float32 {
	if value < 0 {
		return 0
	}
	if value > 1 {
		return 1
	}
	return value
}

func putFloat32(dst []byte, value float32) {
	binary.BigEndian.PutUint32(dst, math.Float32bits(value))
}
