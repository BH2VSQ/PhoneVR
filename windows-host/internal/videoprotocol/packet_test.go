package videoprotocol

import "testing"

func TestPacketRoundTrip(t *testing.T) {
	input := Packet{Flags: FlagFrameStart | FlagKeyFrame, FrameID: 9, TimestampNs: 123456789, FrameSize: 10, Payload: []byte("0123456789")}
	wire, err := input.Encode()
	if err != nil {
		t.Fatal(err)
	}
	got, err := Decode(wire)
	if err != nil {
		t.Fatal(err)
	}
	if got.FrameID != input.FrameID || got.TimestampNs != input.TimestampNs || got.FrameSize != input.FrameSize || string(got.Payload) != string(input.Payload) {
		t.Fatalf("roundtrip mismatch: %#v", got)
	}
}

func TestPacketRejectsInvalidFragmentAndFlags(t *testing.T) {
	if _, err := (Packet{FrameSize: 2, FragmentOffset: 1, Payload: []byte("xx")}).Encode(); err == nil {
		t.Fatal("expected out-of-range fragment rejection")
	}
	if _, err := (Packet{Flags: 0x80, FrameSize: 1, Payload: []byte("x")}).Encode(); err == nil {
		t.Fatal("expected unknown flags rejection")
	}
	wire, err := (Packet{Flags: FlagFrameEnd, FrameSize: 3, FragmentOffset: 0, Payload: []byte("x")}).Encode()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Decode(wire); err == nil {
		t.Fatal("expected invalid end marker rejection")
	}
}

func TestPacketRejectsNonzeroReservedField(t *testing.T) {
	wire, err := (Packet{FrameSize: 1, Payload: []byte("x")}).Encode()
	if err != nil {
		t.Fatal(err)
	}
	wire[31] = 1
	if _, err := Decode(wire); err == nil {
		t.Fatal("expected nonzero reserved field rejection")
	}
}

func TestPacketRejectsBadHeader(t *testing.T) {
	wire, err := (Packet{FrameSize: 1, Payload: []byte("x")}).Encode()
	if err != nil {
		t.Fatal(err)
	}
	wire[4] = 7
	if _, err := Decode(wire); err == nil {
		t.Fatal("expected unsupported version rejection")
	}
}

func TestFragmentFrameProducesBoundedCompleteSequence(t *testing.T) {
	frame := make([]byte, MaxPayloadSize*2+17)
	for i := range frame {
		frame[i] = byte(i)
	}
	fragments, err := FragmentFrame(frame, 77, 123456, true)
	if err != nil {
		t.Fatal(err)
	}
	if len(fragments) != 3 {
		t.Fatalf("got %d fragments, want 3", len(fragments))
	}
	var rebuilt []byte
	for i, wire := range fragments {
		if len(wire) > MaxDatagramSize {
			t.Fatalf("fragment %d exceeds datagram limit", i)
		}
		packet, decodeErr := Decode(wire)
		if decodeErr != nil {
			t.Fatalf("fragment %d: %v", i, decodeErr)
		}
		if packet.FrameID != 77 || packet.TimestampNs != 123456 || packet.FrameSize != uint32(len(frame)) {
			t.Fatalf("fragment %d has inconsistent frame metadata", i)
		}
		if i == 0 && packet.Flags&FlagFrameStart == 0 {
			t.Fatal("first fragment lacks start flag")
		}
		if i == len(fragments)-1 && packet.Flags&FlagFrameEnd == 0 {
			t.Fatal("last fragment lacks end flag")
		}
		if packet.Flags&FlagKeyFrame == 0 {
			t.Fatalf("fragment %d lacks keyframe flag", i)
		}
		rebuilt = append(rebuilt, packet.Payload...)
	}
	if string(rebuilt) != string(frame) {
		t.Fatal("fragment payloads do not reconstruct original frame")
	}
}

func TestFragmentFrameRejectsEmptyAndOversizedFrames(t *testing.T) {
	if _, err := FragmentFrame(nil, 1, 1, false); err == nil {
		t.Fatal("expected empty frame rejection")
	}
	if _, err := FragmentFrame(make([]byte, MaxEncodedFrame+1), 1, 1, false); err == nil {
		t.Fatal("expected oversized frame rejection")
	}
}
