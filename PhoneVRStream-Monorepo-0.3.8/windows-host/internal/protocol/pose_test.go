package protocol

import (
	"encoding/binary"
	"math"
	"testing"
)

func TestDecodePosePacket(t *testing.T) {
	packet := make([]byte, PosePacketSize)
	copy(packet[:4], "PVSP")
	packet[4] = PoseVersion
	binary.BigEndian.PutUint32(packet[8:12], 42)
	binary.BigEndian.PutUint64(packet[12:20], 123456789)
	binary.BigEndian.PutUint32(packet[20:24], math.Float32bits(0))
	binary.BigEndian.PutUint32(packet[24:28], math.Float32bits(0))
	binary.BigEndian.PutUint32(packet[28:32], math.Float32bits(0))
	binary.BigEndian.PutUint32(packet[32:36], math.Float32bits(1))

	pose, err := DecodePosePacket(packet)
	if err != nil {
		t.Fatalf("DecodePosePacket() error = %v", err)
	}
	if pose.Sequence != 42 || pose.TimestampNs != 123456789 || pose.W != 1 {
		t.Fatalf("unexpected decoded pose: %+v", pose)
	}
}

func TestDecodePosePacketRejectsInvalidData(t *testing.T) {
	if _, err := DecodePosePacket(make([]byte, 10)); err == nil {
		t.Fatal("expected invalid size error")
	}
	packet := make([]byte, PosePacketSize)
	if _, err := DecodePosePacket(packet); err == nil {
		t.Fatal("expected invalid magic error")
	}
}
