package usbmux

import (
	"errors"
	"io"
	"sync"
	"sync/atomic"
)

var ErrClosed = errors.New("USB multiplex connection closed")

// Mux owns the physical full-duplex bulk stream. Incoming frames are delivered
// in order; writes from pose, control and video workers are serialized.
type Mux struct {
	stream   io.ReadWriteCloser
	incoming chan Frame
	stop     chan struct{}
	done     chan struct{}
	writeMu  sync.Mutex
	closeMu  sync.Once
	sequence atomic.Uint32
	errMu    sync.RWMutex
	err      error
}

func New(stream io.ReadWriteCloser) *Mux {
	m := &Mux{
		stream: stream, incoming: make(chan Frame, 256),
		stop: make(chan struct{}), done: make(chan struct{}),
	}
	go m.readLoop()
	return m
}

func (m *Mux) readLoop() {
	defer close(m.done)
	for {
		frame, err := ReadFrame(m.stream)
		if err != nil {
			m.setError(err)
			return
		}
		select {
		case m.incoming <- frame:
		case <-m.stop:
			return
		}
	}
}

func (m *Mux) setError(err error) {
	m.errMu.Lock()
	if m.err == nil {
		m.err = err
	}
	m.errMu.Unlock()
}

func (m *Mux) Err() error {
	m.errMu.RLock()
	defer m.errMu.RUnlock()
	return m.err
}

func (m *Mux) ReadFrame() (Frame, error) {
	select {
	case frame := <-m.incoming:
		return frame, nil
	case <-m.done:
		if err := m.Err(); err != nil {
			return Frame{}, err
		}
		return Frame{}, ErrClosed
	case <-m.stop:
		return Frame{}, ErrClosed
	}
}

func (m *Mux) WriteFrame(channel byte, payload []byte) error {
	select {
	case <-m.stop:
		return ErrClosed
	case <-m.done:
		return ErrClosed
	default:
	}
	m.writeMu.Lock()
	defer m.writeMu.Unlock()
	select {
	case <-m.stop:
		return ErrClosed
	case <-m.done:
		return ErrClosed
	default:
	}
	return WriteFrame(m.stream, Frame{Channel: channel, Sequence: m.sequence.Add(1), Payload: payload})
}

func (m *Mux) Close() error {
	var err error
	m.closeMu.Do(func() {
		close(m.stop)
		err = m.stream.Close()
	})
	return err
}
