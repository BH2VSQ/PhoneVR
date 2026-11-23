package protocol

import (
	"encoding/binary"
	"math"
	"testing"
)

func TestEncodeControllerPacketLayout(t *testing.T) {
	state := ControllerState{
		Connected: true, TimestampNs: 0x0102030405060708, PacketSequence: 0x11223344,
		Touching: true, TouchX: 0.25, TouchY: 0.75,
		Click: true, App: true, Home: true, VolumeUp: true, VolumeDown: true,
		OrientationY: 0.70710677, OrientationW: 0.70710677, HasOrientation: true,
	}
	packet, err := EncodeControllerPacket(ControllerHandRight, state)
	if err != nil {
		t.Fatal(err)
	}
	if len(packet) != ControllerPacketSize || string(packet[:4]) != "PVSC" || packet[4] != 1 || packet[5] != 1 {
		t.Fatalf("unexpected header: %v", packet[:6])
	}
	if packet[6] != 0x07 || packet[7] != 0x1f {
		t.Fatalf("unexpected flags/buttons: %02x/%02x", packet[6], packet[7])
	}
	if got := binary.BigEndian.Uint32(packet[8:12]); got != state.PacketSequence {
		t.Fatalf("sequence=%08x", got)
	}
	if got := binary.BigEndian.Uint64(packet[12:20]); got != state.TimestampNs {
		t.Fatalf("timestamp=%016x", got)
	}
	if got := math.Float32frombits(binary.BigEndian.Uint32(packet[40:44])); got != 0.75 {
		t.Fatalf("touchY=%v", got)
	}
}

func TestEncodeControllerPacketRejectsInvalidHandAndQuaternion(t *testing.T) {
	if _, err := EncodeControllerPacket(2, ControllerState{}); err == nil {
		t.Fatal("expected invalid hand error")
	}
	if _, err := EncodeControllerPacket(ControllerHandLeft, ControllerState{Connected: true, HasOrientation: true, OrientationW: 0.1}); err == nil {
		t.Fatal("expected quaternion norm error")
	}
}

func TestEncodeControllerPacketRejectsNonFiniteTouch(t *testing.T) {
	if _, err := EncodeControllerPacket(ControllerHandLeft, ControllerState{TouchX: float32(math.NaN())}); err == nil {
		t.Fatal("expected non-finite input error")
	}
}
