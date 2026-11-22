package usbmux

import (
	"errors"
	"io"
	"net"
	"strings"
	"sync"
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

type failingWriteStream struct {
	closed chan struct{}
	once   sync.Once
}

func newFailingWriteStream() *failingWriteStream {
	return &failingWriteStream{closed: make(chan struct{})}
}

func (s *failingWriteStream) Read([]byte) (int, error) {
	<-s.closed
	return 0, io.EOF
}

func (s *failingWriteStream) Write([]byte) (int, error) {
	return 0, errors.New("synthetic USB transfer timeout")
}

func (s *failingWriteStream) Close() error {
	s.once.Do(func() { close(s.closed) })
	return nil
}

func TestMuxClosesByteStreamAfterFailedFrameWrite(t *testing.T) {
	stream := newFailingWriteStream()
	mux := New(stream)
	err := mux.WriteFrame(ChannelVideo, []byte("partial frame"))
	if err == nil || !strings.Contains(err.Error(), "synthetic USB transfer timeout") {
		t.Fatalf("WriteFrame error = %v, want synthetic transfer timeout", err)
	}
	select {
	case <-mux.done:
	case <-time.After(time.Second):
		t.Fatal("failed frame write did not close the multiplex read loop")
	}
	if mux.Err() == nil || !strings.Contains(mux.Err().Error(), "synthetic USB transfer timeout") {
		t.Fatalf("mux error = %v, want original write failure", mux.Err())
	}
	if err := mux.WriteFrame(ChannelControl, []byte("after failure")); !errors.Is(err, ErrClosed) {
		t.Fatalf("write after a partial frame should be closed, got %v", err)
	}
}
