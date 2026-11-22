package usbmux

import (
	"errors"
	"fmt"
	"hash/crc32"
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
	loggerMu sync.RWMutex
	logger   func(string)
	txLogs   atomic.Uint32
	rxLogs   atomic.Uint32
}

func New(stream io.ReadWriteCloser) *Mux {
	m := &Mux{
		stream: stream, incoming: make(chan Frame, 256),
		stop: make(chan struct{}), done: make(chan struct{}),
	}
	go m.readLoop()
	return m
}

func (m *Mux) SetLogger(logger func(string)) {
	m.loggerMu.Lock()
	m.logger = logger
	m.loggerMu.Unlock()
}

func (m *Mux) logf(format string, args ...any) {
	m.loggerMu.RLock()
	logger := m.logger
	m.loggerMu.RUnlock()
	if logger != nil {
		logger(fmt.Sprintf(format, args...))
	}
}

func (m *Mux) readLoop() {
	defer close(m.done)
	for {
		frame, err := ReadFrame(m.stream)
		if err != nil {
			m.setError(err)
			return
		}
		if m.rxLogs.Add(1) <= 24 {
			m.logf("RX frame channel=%d seq=%d payloadBytes=%d payloadCrc32=%08X",
				frame.Channel, frame.Sequence, len(frame.Payload), crc32.ChecksumIEEE(frame.Payload))
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
	frame := Frame{Channel: channel, Sequence: m.sequence.Add(1), Payload: payload}
	wire, err := encodeFrame(frame)
	if err != nil {
		return err
	}
	if m.txLogs.Add(1) <= 24 {
		m.logf("TX frame channel=%d seq=%d payloadBytes=%d wireBytes=%d payloadCrc32=%08X wireCrc32=%08X headerHex=% X",
			channel, frame.Sequence, len(payload), len(wire), crc32.ChecksumIEEE(payload), crc32.ChecksumIEEE(wire), wire[:HeaderSize])
	}
	written, err := writeAllCount(m.stream, wire)
	if err != nil {
		m.logf("TX frame failed channel=%d seq=%d requestedWireBytes=%d writtenWireBytes=%d error=%v",
			channel, frame.Sequence, len(wire), written, err)
		// A failed write may have sent only part of a multiplex frame. The peer
		// cannot recover framing on this byte stream, so tear down the session.
		m.setError(err)
		_ = m.Close()
		return err
	}
	if written != len(wire) {
		err = io.ErrShortWrite
		m.logf("TX frame short write channel=%d seq=%d requestedWireBytes=%d writtenWireBytes=%d",
			channel, frame.Sequence, len(wire), written)
		m.setError(err)
		_ = m.Close()
		return err
	}
	return nil
}

func (m *Mux) Close() error {
	var err error
	m.closeMu.Do(func() {
		close(m.stop)
		err = m.stream.Close()
	})
	return err
}
