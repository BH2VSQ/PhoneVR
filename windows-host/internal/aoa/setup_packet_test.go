package aoa

import (
	"encoding/binary"
	"testing"
)

func TestPackSetupPacketMatchesUSBWireLayout(t *testing.T) {
	got := packSetupPacket(0xC0, 51, 0x1234, 0x5678, 0x9ABC)
	var actual [8]byte
	binary.LittleEndian.PutUint64(actual[:], got)
	want := [8]byte{0xC0, 51, 0x34, 0x12, 0x78, 0x56, 0xBC, 0x9A}
	if actual != want {
		t.Fatalf("setup packet bytes = % X, want % X", actual, want)
	}
}
