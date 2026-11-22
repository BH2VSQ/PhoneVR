package usbmux

import (
	"bytes"
	"io"
	"testing"
)

type chunkWriter struct {
	bytes.Buffer
	max int
}

func (w *chunkWriter) Write(p []byte) (int, error) {
	if len(p) > w.max {
		p = p[:w.max]
	}
	return w.Buffer.Write(p)
}

func TestFrameRoundTripAcrossShortWrites(t *testing.T) {
	want := Frame{Channel: ChannelVideo, Sequence: 42, Payload: bytes.Repeat([]byte{0x5a}, 8191)}
	var stream chunkWriter
	stream.max = 7
	if err := WriteFrame(&stream, want); err != nil {
		t.Fatal(err)
	}
	got, err := ReadFrame(bytes.NewReader(stream.Bytes()))
	if err != nil {
		t.Fatal(err)
	}
	if got.Channel != want.Channel || got.Sequence != want.Sequence || !bytes.Equal(got.Payload, want.Payload) {
		t.Fatalf("round trip mismatch: got channel=%d sequence=%d payload=%d", got.Channel, got.Sequence, len(got.Payload))
	}
}

func TestReadFrameRejectsOversize(t *testing.T) {
	var frame bytes.Buffer
	frame.Write([]byte{'P', 'V', 'R', 'U', ProtocolVersion, ChannelControl, 0, 0})
	frame.Write([]byte{0xff, 0xff, 0xff, 0x7f, 0, 0, 0, 0})
	if _, err := ReadFrame(&frame); err == nil {
		t.Fatal("expected oversized frame rejection")
	}
}

func TestReadFrameDetectsTruncation(t *testing.T) {
	var frame bytes.Buffer
	if err := WriteFrame(&frame, Frame{Channel: ChannelPose, Payload: []byte{1, 2, 3}}); err != nil {
		t.Fatal(err)
	}
	data := frame.Bytes()[:HeaderSize+1]
	if _, err := ReadFrame(bytes.NewReader(data)); err == nil || err == io.EOF {
		t.Fatalf("expected truncation error, got %v", err)
	}
}

func TestReadFrameRejectsReservedFlags(t *testing.T) {
	wire := make([]byte, HeaderSize)
	copy(wire[:4], []byte{'P', 'V', 'R', 'U'})
	wire[4] = ProtocolVersion
	wire[5] = ChannelControl
	wire[6] = 1
	if _, err := ReadFrame(bytes.NewReader(wire)); err == nil {
		t.Fatal("expected nonzero flags to be rejected")
	}
}
