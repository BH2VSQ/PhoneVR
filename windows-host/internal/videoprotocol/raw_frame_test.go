package videoprotocol

import "testing"

func TestRawFrameHeaderRoundTrip(t *testing.T) {
	input := RawFrameHeader{FrameID: 42, TimestampNs: 123456789, Width: 1600, Height: 720, Stride: 6400, PayloadSize: 6400 * 720}
	wire, err := input.Encode()
	if err != nil {
		t.Fatal(err)
	}
	got, err := DecodeRawFrameHeader(wire)
	if err != nil {
		t.Fatal(err)
	}
	if got != input {
		t.Fatalf("round trip mismatch: got %+v want %+v", got, input)
	}
}

func TestRawFrameHeaderRejectsInvalidDimensionsAndStride(t *testing.T) {
	cases := []RawFrameHeader{
		{Width: 0, Height: 720, Stride: 6400, PayloadSize: 6400 * 720},
		{Width: 1600, Height: 720, Stride: 6399, PayloadSize: 6399 * 720},
		{Width: 5000, Height: 720, Stride: 20000, PayloadSize: 20000 * 720},
	}
	for _, item := range cases {
		if _, err := item.Encode(); err == nil {
			t.Fatalf("expected invalid header rejection: %+v", item)
		}
	}
}
