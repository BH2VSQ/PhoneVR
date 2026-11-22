package usbmux

import (
	"io"
	"net"
	"testing"
	"time"
)

func TestMuxRoutesFramesAndSerializesWrites(t *testing.T) {
	phone, host := net.Pipe()
	mux := New(host)
	defer mux.Close()
	defer phone.Close()

	writeDone := make(chan error, 1)
	go func() {
		writeDone <- WriteFrame(phone, Frame{Channel: ChannelPose, Sequence: 9, Payload: []byte{1, 2, 3}})
	}()
	frame, err := mux.ReadFrame()
	if err != nil {
		t.Fatal(err)
	}
	if frame.Channel != ChannelPose || frame.Sequence != 9 || string(frame.Payload) != string([]byte{1, 2, 3}) {
		t.Fatalf("unexpected frame: %#v", frame)
	}
	if err := <-writeDone; err != nil {
		t.Fatal(err)
	}

	writeDone = make(chan error, 1)
	go func() { writeDone <- mux.WriteFrame(ChannelVideo, []byte{4, 5, 6, 7}) }()
	_ = phone.SetReadDeadline(time.Now().Add(time.Second))
	got, err := ReadFrame(phone)
	if err != nil {
		t.Fatal(err)
	}
	if got.Channel != ChannelVideo || string(got.Payload) != string([]byte{4, 5, 6, 7}) {
		t.Fatalf("unexpected outbound frame: %#v", got)
	}
	if err := <-writeDone; err != nil {
		t.Fatal(err)
	}
}

func TestMuxCloseUnblocksReaderAndWriter(t *testing.T) {
	phone, host := net.Pipe()
	mux := New(host)
	if err := mux.Close(); err != nil {
		t.Fatal(err)
	}
	_ = phone.Close()
	select {
	case <-mux.done:
	case <-time.After(time.Second):
		t.Fatal("mux read loop did not stop after Close")
	}
	if _, err := mux.ReadFrame(); err == nil {
		t.Fatal("expected closed read")
	}
	if err := mux.WriteFrame(ChannelControl, []byte("test")); err == nil {
		t.Fatal("expected closed write")
	}
	if err := mux.Close(); err != nil && err != io.ErrClosedPipe {
		t.Fatalf("unexpected second close result: %v", err)
	}
}
