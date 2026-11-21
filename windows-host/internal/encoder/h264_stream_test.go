package encoder

import (
	"bytes"
	"context"
	"errors"
	"os/exec"
	"strings"
	"testing"
	"time"
)

func TestStartH264EncoderWithCapabilityRejectsSoftwareFallback(t *testing.T) {
	_, err := StartH264EncoderWithCapability(
		context.Background(), "ffmpeg-does-not-need-to-exist", 1600, 720, 60,
		Capability{Name: "libx264", Codec: CodecH264, Backend: BackendSoftware, Hardware: false},
		func([]byte, uint32, uint64, bool) error { return nil }, nil,
	)
	if err == nil || !strings.Contains(err.Error(), "prevalidated hardware H.264") {
		t.Fatalf("software capability should be rejected before process launch, got %v", err)
	}
}

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

func TestRawH264EncoderArgsUseHardwareFormatsAndBoundedQSVQueue(t *testing.T) {
	nvenc := strings.Join(rawH264EncoderArgs(Capability{Name: "h264_nvenc", Codec: CodecH264, Backend: BackendNVENC, Hardware: true}, 1600, 720, 60), " ")
	if !strings.Contains(nvenc, "-pix_fmt nv12") || !strings.Contains(nvenc, "-flush_packets 1") {
		t.Fatalf("expected NV12 hardware input and low-latency mux flushing, args: %s", nvenc)
	}
	qsv := strings.Join(rawH264EncoderArgs(Capability{Name: "h264_qsv", Codec: CodecH264, Backend: BackendQSV, Hardware: true}, 1600, 720, 60), " ")
	for _, option := range []string{"-async_depth 4", "-low_delay_brc 1", "-pix_fmt nv12", "-bf 0", "-aud 1"} {
		if !strings.Contains(qsv, option) {
			t.Fatalf("QSV args missing %q: %s", option, qsv)
		}
	}
	if strings.Contains(qsv, "-pix_fmt yuv420p") || strings.Contains(qsv, "-fflags nobuffer") || strings.Contains(qsv, "h264_metadata") {
		t.Fatalf("QSV args still request an incompatible output format or discard startup buffering: %s", qsv)
	}
	if !strings.Contains(qsv, "-async_depth 4") {
		t.Fatalf("QSV queue depth must allow hardware parallelism: %s", qsv)
	}
	if h264PendingFrameLimit < 8 || h264PendingFrameLimit > 32 {
		t.Fatalf("unexpected pending frame backlog limit: %d", h264PendingFrameLimit)
	}
}

type testWriteCloser struct{}

func (testWriteCloser) Write(p []byte) (int, error) { return len(p), nil }
func (testWriteCloser) Close() error                { return nil }

func TestH264TimestampBackpressureIsRecoverable(t *testing.T) {
	pending := make(chan uint64, 1)
	pending <- 123
	session := &H264EncoderSession{stdin: testWriteCloser{}, timestamps: pending}
	err := session.WriteFrame([]byte{0, 0, 0, 255}, 456)
	if !errors.Is(err, ErrH264Backpressure) {
		t.Fatalf("expected recoverable backpressure, got %v", err)
	}
	if session.closed.Load() {
		t.Fatal("backpressure must not mark the persistent FFmpeg session closed")
	}
	if got := session.PendingFrames(); got != 1 {
		t.Fatalf("pending timestamp count changed after dropped frame: got %d, want 1", got)
	}
}

func TestH264OutputStallRequiresBackedUpQueueAndNoRecentAccessUnits(t *testing.T) {
	stalled := &H264EncoderSession{timestamps: make(chan uint64, h264PendingFrameLimit)}
	for i := 0; i < h264PendingFrameLimit/2; i++ {
		stalled.timestamps <- uint64(i)
	}
	stalled.inputFrames.Store(uint64(h264PendingFrameLimit / 2))
	stalled.lastAccessUnitNs.Store(time.Now().Add(-3 * time.Second).UnixNano())
	if !stalled.OutputStalled(2 * time.Second) {
		t.Fatal("expected a backed-up encoder with no recent access unit to be stalled")
	}

	healthy := &H264EncoderSession{timestamps: make(chan uint64, h264PendingFrameLimit)}
	for i := 0; i < h264PendingFrameLimit/2; i++ {
		healthy.timestamps <- uint64(i)
	}
	healthy.inputFrames.Store(uint64(h264PendingFrameLimit / 2))
	healthy.lastAccessUnitNs.Store(time.Now().UnixNano())
	if healthy.OutputStalled(2 * time.Second) {
		t.Fatal("recently emitted access units must prevent false stall detection")
	}
}
