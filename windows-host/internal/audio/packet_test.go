package audio

import (
	"bytes"
	"testing"
)

func TestPacketRoundTripBothDirections(t *testing.T) {
	for _, test := range []struct {
		direction Direction
		channels  uint8
	}{
		{DirectionHostToPhone, 2},
		{DirectionPhoneToHost, 1},
	} {
		payload := make([]byte, SamplesPerFrame*int(test.channels)*2)
		for i := range payload {
			payload[i] = byte(i * 31)
		}
		wire, err := Encode(Packet{
			Direction: test.direction, Channels: test.channels,
			Sequence: 0x10203040, TimestampNs: 0x0102030405060708,
			SamplesPerCh: SamplesPerFrame, Payload: payload,
		})
		if err != nil {
			t.Fatal(err)
		}
		got, err := Decode(wire)
		if err != nil {
			t.Fatal(err)
		}
		if got.Direction != test.direction || got.Channels != test.channels ||
			got.Sequence != 0x10203040 || got.TimestampNs != 0x0102030405060708 ||
			!bytes.Equal(got.Payload, payload) {
			t.Fatalf("round-trip mismatch: %#v", got)
		}
	}
}

func TestPacketHeaderMatchesAndroidWireLayout(t *testing.T) {
	wire, err := Encode(Packet{
		Direction: DirectionHostToPhone, Channels: 2, Sequence: 42,
		TimestampNs: 0x0102030405060708, SamplesPerCh: SamplesPerFrame,
		Payload: make([]byte, SamplesPerFrame*4),
	})
	if err != nil {
		t.Fatal(err)
	}
	want := []byte{
		0x50, 0x56, 0x52, 0x41,
		0x01, 0x01, 0x02, 0x01,
		0x2a, 0x00, 0x00, 0x00,
		0x08, 0x07, 0x06, 0x05, 0x04, 0x03, 0x02, 0x01,
		0xc0, 0x03, 0x00, 0x0f,
	}
	if !bytes.Equal(wire[:HeaderSize], want) {
		t.Fatalf("audio header mismatch: got % x, want % x", wire[:HeaderSize], want)
	}
}

func TestDecodeRejectsMalformedPackets(t *testing.T) {
	payload := make([]byte, SamplesPerFrame*2*2)
	wire, err := Encode(Packet{Direction: DirectionHostToPhone, Channels: 2,
		SamplesPerCh: SamplesPerFrame, Payload: payload})
	if err != nil {
		t.Fatal(err)
	}
	cases := map[string][]byte{
		"short":               wire[:HeaderSize-1],
		"bad signature":       append([]byte("NOPE"), wire[4:]...),
		"unsupported version": append([]byte(nil), wire...),
		"bad direction":       append([]byte(nil), wire...),
		"bad payload length":  append([]byte(nil), wire...),
	}
	cases["unsupported version"][4] = 99
	cases["bad direction"][5] = 99
	cases["bad payload length"][23]--
	for name, data := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := Decode(data); err == nil {
				t.Fatal("expected malformed packet to be rejected")
			}
		})
	}
}

func TestEncodeRejectsUnexpectedFrameSize(t *testing.T) {
	_, err := Encode(Packet{Direction: DirectionPhoneToHost, Channels: 1,
		SamplesPerCh: SamplesPerFrame, Payload: make([]byte, 2)})
	if err == nil {
		t.Fatal("expected invalid PCM frame size to be rejected")
	}
}
