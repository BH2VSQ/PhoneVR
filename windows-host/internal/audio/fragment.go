package audio

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"hash/crc32"
	"sync"
	"time"
)

const (
	FragmentHeaderSize   = 20
	MaxUDPDatagramSize   = 1200
	FragmentPayloadSize  = MaxUDPDatagramSize - FragmentHeaderSize
	MaxWireSize          = HeaderSize + MaxPayloadSize
	fragmentVersion      = 1
	maxFragmentCount     = (MaxWireSize + FragmentPayloadSize - 1) / FragmentPayloadSize
	maxPendingAssemblies = 16
	fragmentLifetime     = 180 * time.Millisecond
)

var fragmentMagic = [4]byte{'P', 'V', 'A', 'F'}

// Fragment splits one complete PVRA PCM frame into UDP-safe PVAF datagrams.
// USB AOA continues to carry the original unfragmented PVRA frame.
func Fragment(wire []byte) ([][]byte, error) {
	if _, err := Decode(wire); err != nil {
		return nil, fmt.Errorf("fragment invalid audio frame: %w", err)
	}
	count := (len(wire) + FragmentPayloadSize - 1) / FragmentPayloadSize
	if count < 1 || count > maxFragmentCount {
		return nil, fmt.Errorf("audio frame requires unsupported fragment count %d", count)
	}
	fragments := make([][]byte, 0, count)
	for index := 0; index < count; index++ {
		start := index * FragmentPayloadSize
		end := start + FragmentPayloadSize
		if end > len(wire) {
			end = len(wire)
		}
		payload := wire[start:end]
		fragment := make([]byte, FragmentHeaderSize+len(payload))
		copy(fragment[:4], fragmentMagic[:])
		fragment[4] = fragmentVersion
		fragment[5] = wire[5] // Preserve the inner PVRA direction.
		fragment[6] = byte(index)
		fragment[7] = byte(count)
		copy(fragment[8:12], wire[8:12]) // Inner PVRA sequence.
		binary.LittleEndian.PutUint16(fragment[12:14], uint16(len(wire)))
		binary.LittleEndian.PutUint16(fragment[14:16], uint16(len(payload)))
		binary.LittleEndian.PutUint32(fragment[16:20], crc32.ChecksumIEEE(payload))
		copy(fragment[FragmentHeaderSize:], payload)
		fragments = append(fragments, fragment)
	}
	return fragments, nil
}

type fragmentKey struct {
	direction Direction
	sequence  uint32
}

type fragmentAssembly struct {
	totalSize int
	count     int
	parts     [][]byte
	received  int
	bytes     int
	createdAt time.Time
}

// Reassembler accepts out-of-order and duplicate PVAF UDP datagrams. It keeps
// only a small, short-lived window so a missing fragment never blocks new speech.
type Reassembler struct {
	mu      sync.Mutex
	pending map[fragmentKey]*fragmentAssembly
}

func NewReassembler() *Reassembler {
	return &Reassembler{pending: make(map[fragmentKey]*fragmentAssembly)}
}

// Add returns a complete PVRA frame once all fragments arrive. A raw PVRA frame
// is accepted for compatibility with older PhoneVR clients.
func (r *Reassembler) Add(datagram []byte, now time.Time) ([]byte, bool, error) {
	if len(datagram) >= 4 && bytes.Equal(datagram[:4], magic[:]) {
		if _, err := Decode(datagram); err != nil {
			return nil, false, err
		}
		return append([]byte(nil), datagram...), true, nil
	}
	if len(datagram) < FragmentHeaderSize || !bytes.Equal(datagram[:4], fragmentMagic[:]) {
		return nil, false, errors.New("invalid audio UDP fragment signature")
	}
	if datagram[4] != fragmentVersion {
		return nil, false, fmt.Errorf("unsupported audio fragment version %d", datagram[4])
	}
	direction := Direction(datagram[5])
	if direction != DirectionHostToPhone && direction != DirectionPhoneToHost {
		return nil, false, fmt.Errorf("invalid audio fragment direction %d", direction)
	}
	index, count := int(datagram[6]), int(datagram[7])
	sequence := binary.LittleEndian.Uint32(datagram[8:12])
	totalSize := int(binary.LittleEndian.Uint16(datagram[12:14]))
	chunkSize := int(binary.LittleEndian.Uint16(datagram[14:16]))
	checksum := binary.LittleEndian.Uint32(datagram[16:20])
	payload := datagram[FragmentHeaderSize:]
	if totalSize < HeaderSize || totalSize > MaxWireSize || count < 1 || count > maxFragmentCount ||
		index < 0 || index >= count || chunkSize != len(payload) || chunkSize == 0 ||
		chunkSize > FragmentPayloadSize || len(datagram) > MaxUDPDatagramSize {
		return nil, false, errors.New("invalid audio UDP fragment dimensions")
	}
	expectedCount := (totalSize + FragmentPayloadSize - 1) / FragmentPayloadSize
	expectedChunk := FragmentPayloadSize
	if index == count-1 {
		expectedChunk = totalSize - index*FragmentPayloadSize
	}
	if count != expectedCount || chunkSize != expectedChunk {
		return nil, false, errors.New("inconsistent audio UDP fragment layout")
	}
	if crc32.ChecksumIEEE(payload) != checksum {
		return nil, false, errors.New("audio UDP fragment checksum mismatch")
	}

	key := fragmentKey{direction: direction, sequence: sequence}
	r.mu.Lock()
	defer r.mu.Unlock()
	for existingKey, assembly := range r.pending {
		if now.Sub(assembly.createdAt) > fragmentLifetime {
			delete(r.pending, existingKey)
		}
	}
	assembly := r.pending[key]
	if assembly == nil {
		if len(r.pending) >= maxPendingAssemblies {
			var oldestKey fragmentKey
			var oldest time.Time
			for candidate, pending := range r.pending {
				if oldest.IsZero() || pending.createdAt.Before(oldest) {
					oldestKey, oldest = candidate, pending.createdAt
				}
			}
			delete(r.pending, oldestKey)
		}
		assembly = &fragmentAssembly{
			totalSize: totalSize,
			count:     count,
			parts:     make([][]byte, count),
			createdAt: now,
		}
		r.pending[key] = assembly
	} else if assembly.totalSize != totalSize || assembly.count != count {
		delete(r.pending, key)
		return nil, false, errors.New("conflicting audio UDP fragment metadata")
	}
	if previous := assembly.parts[index]; previous != nil {
		if !bytes.Equal(previous, payload) {
			delete(r.pending, key)
			return nil, false, errors.New("conflicting duplicate audio UDP fragment")
		}
		return nil, false, nil
	}
	assembly.parts[index] = append([]byte(nil), payload...)
	assembly.received++
	assembly.bytes += len(payload)
	if assembly.received != assembly.count {
		return nil, false, nil
	}
	delete(r.pending, key)
	if assembly.bytes != assembly.totalSize {
		return nil, false, errors.New("reassembled audio UDP frame size mismatch")
	}
	frame := make([]byte, 0, assembly.totalSize)
	for _, part := range assembly.parts {
		frame = append(frame, part...)
	}
	if _, err := Decode(frame); err != nil {
		return nil, false, fmt.Errorf("reassembled invalid audio frame: %w", err)
	}
	return frame, true, nil
}
