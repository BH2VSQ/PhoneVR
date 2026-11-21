package encoder

import (
	"bytes"
	"context"
	"os/exec"
	"strings"
	"testing"
	"time"
)

func TestH264AccessUnitParserSplitsAtAUD(t *testing.T) {
	// SPS/PPS prefix, AUD + IDR frame, then AUD + inter frame. Synthetic NAL
	// payloads are sufficient to test Annex-B boundary handling.
	stream := []byte{
		0, 0, 0, 1, 0x67, 0x01,
		0, 0, 0, 1, 0x68, 0x02,
		0, 0, 0, 1, 0x09, 0xf0,
		0, 0, 0, 1, 0x65, 0xaa,
		0, 0, 0, 1, 0x09, 0xf0,
		0, 0, 0, 1, 0x41, 0xbb,
	}
	parser := H264AccessUnitParser{}
	var frames [][]byte
	var keys []bool
	emit := func(frame []byte, key bool) error {
		frames = append(frames, frame)
		keys = append(keys, key)
		return nil
	}
	for _, chunk := range [][]byte{stream[:7], stream[7:19], stream[19:31], stream[31:]} {
		if err := parser.Feed(chunk, emit); err != nil {
			t.Fatal(err)
		}
	}
	if err := parser.Flush(emit); err != nil {
		t.Fatal(err)
	}
	if len(frames) != 2 {
		t.Fatalf("got %d access units, want 2", len(frames))
	}
	if !keys[0] || keys[1] {
		t.Fatalf("unexpected key-frame flags: %v", keys)
	}
	if !bytes.Contains(frames[0], []byte{0x67, 0x01}) {
		t.Fatal("first access unit lost SPS prefix")
	}
	if !bytes.Contains(frames[1], []byte{0x41, 0xbb}) {
		t.Fatal("second access unit lost slice NAL")
	}
}

func TestH264AccessUnitParserRejectsMissingAUDGrowth(t *testing.T) {
	parser := H264AccessUnitParser{}
	parser.pending = make([]byte, h264StreamMaxAccessUnit)
	if err := parser.Feed([]byte{1}, func([]byte, bool) error { return nil }); err == nil {
		t.Fatal("expected oversized no-AUD stream rejection")
	}
}

func TestH264EncoderSessionEmitsAnnexBAccessUnitsWhenFFmpegAvailable(t *testing.T) {
	path, err := exec.LookPath("ffmpeg")
	if err != nil {
		t.Skip("FFmpeg is not installed in this test environment")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	type received struct {
		data []byte
		key  bool
	}
	frames := make(chan received, 8)
	session, err := StartH264Encoder(ctx, path, 160, 90, 30, func(data []byte, _ uint32, _ uint64, key bool) error {
		t.Logf("callback: bytes=%d key=%v", len(data), key)
		frames <- received{data: append([]byte(nil), data...), key: key}
		return nil
	}, func(message string) { t.Log(message) })
	if err != nil {
		t.Skipf("No locally usable H.264 encoder session: %v", err)
	}
	for i := 0; i < 5; i++ {
		frame := make([]byte, 160*90*4)
		for j := 0; j < len(frame); j += 4 {
			frame[j+0] = byte(i * 23)
			frame[j+1] = byte(j / 4)
			frame[j+2] = byte(255 - i*23)
			frame[j+3] = 255
		}
		if err := session.WriteFrame(frame, uint64(i+1)*33_333_333); err != nil {
			session.Close()
			t.Fatal(err)
		}
	}
	session.Close()
	close(frames)
	count := 0
	keyFound := false
	for frame := range frames {
		if len(frame.data) < 8 || !bytes.Contains(frame.data, []byte{0, 0, 1}) && !bytes.Contains(frame.data, []byte{0, 0, 0, 1}) {
			t.Fatal("encoder output is not an Annex-B H.264 access unit")
		}
		count++
		keyFound = keyFound || frame.key
	}
	if count < 3 {
		t.Fatalf("encoder emitted only %d access units, want at least 3", count)
	}
	if !keyFound {
		t.Fatal("encoder did not mark an IDR access unit")
	}
}

func TestRawH264EncoderArgsFlushPacketsAndBoundTimestampBacklog(t *testing.T) {
	args := rawH264EncoderArgs(Capability{Name: "h264_nvenc", Codec: CodecH264, Backend: BackendNVENC, Hardware: true}, 1600, 720, 60)
	joined := strings.Join(args, " ")
	if !strings.Contains(joined, "-flush_packets 1") {
		t.Fatalf("expected low-latency mux flushing, args: %s", joined)
	}
	if h264PendingFrameLimit > 8 || h264PendingFrameLimit < 2 {
		t.Fatalf("unexpected pending frame backlog limit: %d", h264PendingFrameLimit)
	}
}
