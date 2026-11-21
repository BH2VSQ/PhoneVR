package encoder

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

const (
	h264StreamMaxAccessUnit = 16 * 1024 * 1024
	h264PendingFrameLimit   = 24 // Bound asynchronous input/output backlog to about 400 ms at 60 FPS.
	H264OutputStallTimeout  = 2 * time.Second
)

// ErrH264Backpressure means FFmpeg has accepted the maximum number of input
// frames whose encoded access units have not yet arrived. This is a frame-drop
// signal, not a broken encoder session: callers should discard the current raw
// frame and keep the same FFmpeg process alive.
var ErrH264Backpressure = errors.New("H.264 encoder is backpressured; raw frame should be dropped")

// H264AccessUnitParser splits an Annex-B byte stream at H.264 AUD NAL units.
// QSV inserts AUDs in the encoder itself; other backends use FFmpeg's
// h264_metadata bitstream filter. One stdout Read never implies one frame.
type H264AccessUnitParser struct{ pending []byte }

func (p *H264AccessUnitParser) Feed(chunk []byte, emit func([]byte, bool) error) error {
	if len(chunk) > 0 {
		p.pending = append(p.pending, chunk...)
	}
	for {
		auds := findAUDStartCodes(p.pending)
		if len(auds) < 2 {
			break
		}
		end := auds[1]
		accessUnit := append([]byte(nil), p.pending[:end]...)
		if len(accessUnit) == 0 {
			return errors.New("empty H.264 access unit")
		}
		if err := emit(accessUnit, h264ContainsIDR(accessUnit)); err != nil {
			return err
		}
		p.pending = append(p.pending[:0], p.pending[end:]...)
	}
	if len(p.pending) > h264StreamMaxAccessUnit {
		return errors.New("H.264 stream did not produce an AUD boundary within 16 MiB")
	}
	return nil
}

func (p *H264AccessUnitParser) Flush(emit func([]byte, bool) error) error {
	if len(p.pending) == 0 {
		return nil
	}
	if len(p.pending) > h264StreamMaxAccessUnit {
		return errors.New("final H.264 access unit is oversized")
	}
	frame := append([]byte(nil), p.pending...)
	p.pending = nil
	return emit(frame, h264ContainsIDR(frame))
}

func findNALStart(data []byte, from int) (int, int) {
	for i := from; i+3 < len(data); i++ {
		if data[i] != 0 || data[i+1] != 0 {
			continue
		}
		if data[i+2] == 1 {
			return i, 3
		}
		if i+4 <= len(data) && data[i+2] == 0 && data[i+3] == 1 {
			return i, 4
		}
	}
	return -1, 0
}

func findAUDStartCodes(data []byte) []int {
	var result []int
	for from := 0; ; {
		start, prefix := findNALStart(data, from)
		if start < 0 {
			break
		}
		header := start + prefix
		if header < len(data) && data[header]&0x1f == 9 {
			result = append(result, start)
		}
		from = header + 1
	}
	return result
}

func h264ContainsIDR(data []byte) bool {
	for from := 0; ; {
		start, prefix := findNALStart(data, from)
		if start < 0 {
			return false
		}
		header := start + prefix
		if header < len(data) && data[header]&0x1f == 5 {
			return true
		}
		from = header + 1
	}
}

type AccessUnitCallback func(frame []byte, frameID uint32, timestampNs uint64, keyFrame bool) error

type H264EncoderSession struct {
	cmd              *exec.Cmd
	stdin            io.WriteCloser
	timestamps       chan uint64
	callback         AccessUnitCallback
	logf             func(string)
	writeMu          sync.Mutex
	closed           atomic.Bool
	frameID          atomic.Uint32
	done             chan struct{}
	selected         Capability
	inputFrames      atomic.Uint64
	outputBytes      atomic.Uint64
	outputUnits      atomic.Uint64
	lastOutputNs     atomic.Int64
	lastAccessUnitNs atomic.Int64
}

// StartH264Encoder validates the configured resolution, frame rate and BGRA
// input conversion with a one-second test stream before starting the long-lived
// raw-BGRA-to-H.264 process.
func StartH264Encoder(ctx context.Context, ffmpegPath string, width, height, fps int, callback AccessUnitCallback, logf func(string)) (*H264EncoderSession, error) {
	return startH264Encoder(ctx, ffmpegPath, width, height, fps, callback, logf, true)
}

// StartHardwareH264Encoder refuses the CPU software fallback. PhoneVR's Host
// can already be CPU-bound by the SteamVR compositor; running x264 at 1600x720
// would compete with VR rendering and worsen dropped frames.
func StartHardwareH264Encoder(ctx context.Context, ffmpegPath string, width, height, fps int, callback AccessUnitCallback, logf func(string)) (*H264EncoderSession, error) {
	return startH264Encoder(ctx, ffmpegPath, width, height, fps, callback, logf, false)
}

func startH264Encoder(ctx context.Context, ffmpegPath string, width, height, fps int, callback AccessUnitCallback, logf func(string), allowSoftware bool) (*H264EncoderSession, error) {
	if width < 16 || height < 16 || fps < 1 || fps > 120 {
		return nil, errors.New("invalid H.264 encoder profile")
	}
	if callback == nil {
		return nil, errors.New("H.264 access-unit callback is required")
	}
	if ffmpegPath == "" {
		resolved, err := exec.LookPath("ffmpeg")
		if err != nil {
			return nil, fmt.Errorf("find FFmpeg: %w", err)
		}
		ffmpegPath = resolved
	}
	caps, err := ProbeFFmpeg(ctx, ffmpegPath)
	if err != nil {
		return nil, err
	}
	var selected Capability
	var testErrors []string
	for _, candidate := range caps {
		if candidate.Codec != CodecH264 {
			continue
		}
		if !allowSoftware && !candidate.Hardware {
			testErrors = append(testErrors, candidate.Name+": CPU software encoding is disabled for live VR streaming")
			continue
		}
		if err := testH264Encoder(ctx, ffmpegPath, candidate, width, height, fps); err != nil {
			testErrors = append(testErrors, candidate.Name+": "+err.Error())
			continue
		}
		selected = candidate
		break
	}
	if selected.Name == "" {
		return nil, fmt.Errorf("no usable H.264 encoder session: %s", strings.Join(testErrors, "; "))
	}
	return startH264EncoderWithCapability(ctx, ffmpegPath, width, height, fps, selected, callback, logf)
}

// StartH264EncoderWithCapability starts a persistent encoder session using a
// hardware capability already tested during Host startup. It deliberately does
// not spawn additional FFmpeg discovery/probe processes for every video bridge
// reconnect. The Host owns this session for its lifetime, independently of the
// loopback TCP capture connection.
func StartH264EncoderWithCapability(ctx context.Context, ffmpegPath string, width, height, fps int, selected Capability, callback AccessUnitCallback, logf func(string)) (*H264EncoderSession, error) {
	if !selected.Hardware || selected.Codec != CodecH264 || selected.Name == "" {
		return nil, errors.New("a prevalidated hardware H.264 encoder capability is required")
	}
	if width < 16 || height < 16 || fps < 1 || fps > 120 {
		return nil, errors.New("invalid H.264 encoder profile")
	}
	if callback == nil {
		return nil, errors.New("H.264 access-unit callback is required")
	}
	if ffmpegPath == "" {
		resolved, err := exec.LookPath("ffmpeg")
		if err != nil {
			return nil, fmt.Errorf("find FFmpeg: %w", err)
		}
		ffmpegPath = resolved
	}
	return startH264EncoderWithCapability(ctx, ffmpegPath, width, height, fps, selected, callback, logf)
}

func startH264EncoderWithCapability(ctx context.Context, ffmpegPath string, width, height, fps int, selected Capability, callback AccessUnitCallback, logf func(string)) (*H264EncoderSession, error) {
	args := rawH264EncoderArgs(selected, width, height, fps)
	cmd := exec.CommandContext(ctx, ffmpegPath, args...)
	configureFFmpegProcess(cmd)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, fmt.Errorf("open FFmpeg stdin: %w", err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		_ = stdin.Close()
		return nil, fmt.Errorf("open FFmpeg stdout: %w", err)
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		_ = stdin.Close()
		return nil, fmt.Errorf("open FFmpeg stderr: %w", err)
	}
	if err := cmd.Start(); err != nil {
		_ = stdin.Close()
		return nil, fmt.Errorf("start FFmpeg H.264 encoder: %w", err)
	}
	session := &H264EncoderSession{cmd: cmd, stdin: stdin, timestamps: make(chan uint64, h264PendingFrameLimit), callback: callback, logf: logf, done: make(chan struct{}), selected: selected}
	nowNs := time.Now().UnixNano()
	session.lastOutputNs.Store(nowNs)
	session.lastAccessUnitNs.Store(nowNs)
	if logf != nil {
		logf(fmt.Sprintf("Video encoder session initialized: %s (%s), %dx%d @ %d FPS", selected.Name, selected.Backend, width, height, fps))
	}
	go session.readStderr(stderr)
	go session.readAccessUnits(stdout)
	return session, nil
}

// ProbeHardwareH264Encoder verifies that a hardware H.264 encoder can process
// a one-second BGRA stream at the actual target profile and emit an Annex-B
// access-unit stream. Listing an encoder in `ffmpeg -encoders` is not enough:
// driver/runtime libraries or full-profile throughput may still be unavailable.
func ProbeHardwareH264Encoder(ctx context.Context, ffmpegPath string, width, height, fps int) (Capability, error) {
	if width < 16 || height < 16 || fps < 1 || fps > 120 {
		return Capability{}, errors.New("invalid H.264 encoder probe profile")
	}
	if ffmpegPath == "" {
		resolved, err := exec.LookPath("ffmpeg")
		if err != nil {
			return Capability{}, fmt.Errorf("find FFmpeg: %w", err)
		}
		ffmpegPath = resolved
	}
	capabilities, err := ProbeFFmpeg(ctx, ffmpegPath)
	if err != nil {
		return Capability{}, err
	}
	var failures []string
	for _, candidate := range capabilities {
		if candidate.Codec != CodecH264 || !candidate.Hardware {
			continue
		}
		if err := testH264Encoder(ctx, ffmpegPath, candidate, width, height, fps); err != nil {
			failures = append(failures, candidate.Name+": "+err.Error())
			continue
		}
		return candidate, nil
	}
	if len(failures) == 0 {
		return Capability{}, errors.New("FFmpeg exposes no hardware H.264 encoder (NVENC/AMF/QSV); CPU encoding is disabled for live VR streaming")
	}
	return Capability{}, fmt.Errorf("no hardware H.264 encoder could initialize: %s; CPU encoding is disabled for live VR streaming", strings.Join(failures, "; "))
}

func testH264Encoder(ctx context.Context, path string, capability Capability, width, height, fps int) error {
	probeFile, err := os.CreateTemp("", "phonevr-h264-probe-*.h264")
	if err != nil {
		return fmt.Errorf("create encoder probe output: %w", err)
	}
	probePath := probeFile.Name()
	_ = probeFile.Close()
	defer os.Remove(probePath)

	// Exercise the same frame size and BGRA-to-NV12 conversion used by the
	// live path. A tiny 160x90 two-frame test only proves that the encoder can
	// initialize; it does not detect a QSV path that stalls at the real profile.
	probeFrames := fps
	input := fmt.Sprintf("testsrc=size=%dx%d:rate=%d,format=bgra", width, height, fps)
	args := []string{"-hide_banner", "-loglevel", "error", "-f", "lavfi", "-i", input, "-frames:v", fmt.Sprint(probeFrames), "-an"}
	args = append(args, h264CodecArgs(capability, fps)...)
	args = append(args, "-pix_fmt", encoderOutputPixelFormat(capability))
	args = append(args, h264AccessUnitArgs(capability)...)
	args = append(args, "-f", "h264", "-y", probePath)
	testCtx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	started := time.Now()
	probeCmd := exec.CommandContext(testCtx, path, args...)
	configureFFmpegProcess(probeCmd)
	output, err := probeCmd.CombinedOutput()
	elapsed := time.Since(started)
	if err != nil {
		return fmt.Errorf("full-profile encoder test failed after %s: %w: %s", elapsed.Round(time.Millisecond), err, strings.TrimSpace(string(output)))
	}
	if elapsed > 3*time.Second {
		return fmt.Errorf("encoder is too slow for live VR: encoded %d frames at %dx%d in %s (limit 3s)", probeFrames, width, height, elapsed.Round(time.Millisecond))
	}
	encoded, readErr := os.ReadFile(probePath)
	if readErr != nil {
		return fmt.Errorf("read encoder probe output: %w", readErr)
	}
	if len(encoded) < 16 || len(findAUDStartCodes(encoded)) < 2 || !h264ContainsIDR(encoded) {
		return errors.New("full-profile encoder test produced no usable H.264 access-unit stream")
	}
	return nil
}

func h264CodecArgs(capability Capability, fps int) []string {
	gop := fmt.Sprint(fps)
	switch capability.Backend {
	case BackendNVENC:
		return []string{"-c:v", capability.Name, "-preset", "p1", "-tune", "ll", "-rc", "cbr", "-b:v", "8M", "-maxrate", "8M", "-bufsize", "2M", "-g", gop, "-bf", "0", "-zerolatency", "1"}
	case BackendAMF:
		return []string{"-c:v", capability.Name, "-usage", "lowlatency", "-quality", "speed", "-rc", "cbr", "-b:v", "8M", "-maxrate", "8M", "-bufsize", "2M", "-g", gop, "-bf", "0"}
	case BackendQSV:
		// QSV is asynchronous. Keep its hardware queue shallow for live VR;
		// otherwise raw BGRA frames can pile up while the compositor keeps
		// producing newer frames. LowDelayBRC also reduces per-frame bitrate
		// variance without enabling look-ahead or B-frames.
		return []string{"-c:v", capability.Name, "-preset", "veryfast", "-async_depth", "4", "-look_ahead", "0", "-low_delay_brc", "1", "-b:v", "8M", "-maxrate", "8M", "-bufsize", "2M", "-g", gop, "-bf", "0"}
	default:
		return []string{"-c:v", capability.Name, "-preset", "ultrafast", "-tune", "zerolatency", "-x264-params", "repeat-headers=1:scenecut=0", "-b:v", "8M", "-g", gop, "-bf", "0"}
	}
}

func h264AccessUnitArgs(capability Capability) []string {
	// The QSV encoder implements AUD insertion natively. Prefer that option
	// over a post-encode bitstream filter so the access-unit parser receives
	// a reliable boundary from the hardware encoder itself.
	if capability.Backend == BackendQSV {
		return []string{"-aud", "1"}
	}
	return []string{"-bsf:v", "h264_metadata=aud=insert"}
}

func encoderOutputPixelFormat(capability Capability) string {
	// Intel QSV's 8-bit H.264 path uses NV12. Asking it for yuv420p makes
	// FFmpeg print an incompatibility warning and silently negotiate NV12;
	// request the supported hardware format explicitly instead. The Annex-B
	// H.264 bitstream remains ordinary 4:2:0 and is decoder-compatible.
	if capability.Hardware {
		return "nv12"
	}
	return "yuv420p"
}

func rawH264EncoderArgs(capability Capability, width, height, fps int) []string {
	// The rawvideo demuxer already supplies frames as they are written. The
	// nobuffer demuxer flag is unnecessary here and can discard useful startup
	// buffering when QSV has not yet emitted its first access unit.
	args := []string{"-hide_banner", "-loglevel", "warning", "-f", "rawvideo", "-pixel_format", "bgra", "-video_size", fmt.Sprintf("%dx%d", width, height), "-framerate", fmt.Sprint(fps), "-i", "pipe:0", "-an"}
	args = append(args, h264CodecArgs(capability, fps)...)
	args = append(args, "-pix_fmt", encoderOutputPixelFormat(capability))
	args = append(args, h264AccessUnitArgs(capability)...)
	args = append(args, "-flush_packets", "1", "-f", "h264", "pipe:1")
	return args
}

func (s *H264EncoderSession) HardwareAccelerated() bool { return s != nil && s.selected.Hardware }
func (s *H264EncoderSession) PendingFrames() int {
	if s == nil {
		return 0
	}
	return len(s.timestamps)
}
func (s *H264EncoderSession) InputFrames() uint64 {
	if s == nil {
		return 0
	}
	return s.inputFrames.Load()
}
func (s *H264EncoderSession) OutputBytes() uint64 {
	if s == nil {
		return 0
	}
	return s.outputBytes.Load()
}
func (s *H264EncoderSession) OutputAccessUnits() uint64 {
	if s == nil {
		return 0
	}
	return s.outputUnits.Load()
}

// OutputStalled reports a real encoder stall only after a meaningful number
// of frames has been accepted and the pending timestamp queue is backed up.
// It avoids restarting QSV for ordinary transient backpressure.
func (s *H264EncoderSession) OutputStalled(timeout time.Duration) bool {
	if s == nil || s.InputFrames() < uint64(h264PendingFrameLimit/2) || s.PendingFrames() < h264PendingFrameLimit/2 {
		return false
	}
	last := s.lastAccessUnitNs.Load()
	if last <= 0 {
		return false
	}
	return time.Since(time.Unix(0, last)) >= timeout
}
func (s *H264EncoderSession) EncoderName() string {
	if s == nil {
		return "unknown"
	}
	return s.selected.Name + "/" + string(s.selected.Backend)
}

func (s *H264EncoderSession) WriteFrame(frame []byte, timestampNs uint64) error {
	if s == nil || s.closed.Load() {
		return errors.New("H.264 encoder session is closed")
	}
	select {
	case <-s.done:
		return errors.New("FFmpeg H.264 encoder process has exited")
	default:
	}
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	select {
	case s.timestamps <- timestampNs:
	default:
		return ErrH264Backpressure
	}
	for len(frame) > 0 {
		n, err := s.stdin.Write(frame)
		if err != nil {
			return fmt.Errorf("write raw frame to FFmpeg: %w", err)
		}
		if n == 0 {
			return io.ErrShortWrite
		}
		frame = frame[n:]
	}
	s.inputFrames.Add(1)
	return nil
}

func (s *H264EncoderSession) readStderr(reader io.Reader) {
	buf := make([]byte, 4096)
	for {
		n, err := reader.Read(buf)
		if n > 0 && s.logf != nil {
			text := strings.TrimSpace(string(buf[:n]))
			if text != "" {
				s.logf("FFmpeg: " + text)
			}
		}
		if err != nil {
			return
		}
	}
}

func (s *H264EncoderSession) readAccessUnits(reader io.Reader) {
	defer close(s.done)
	parser := H264AccessUnitParser{}
	buffer := make([]byte, 64*1024)
	emit := func(frame []byte, keyFrame bool) error {
		var timestamp uint64
		select {
		case timestamp = <-s.timestamps:
		case <-time.After(500 * time.Millisecond):
			return errors.New("timed out matching encoded H.264 frame to capture timestamp")
		}
		id := s.frameID.Add(1)
		unitCount := s.outputUnits.Add(1)
		s.lastAccessUnitNs.Store(time.Now().UnixNano())
		if unitCount == 1 && s.logf != nil {
			s.logf(fmt.Sprintf("FFmpeg emitted first H.264 access unit: bytes=%d IDR=%t", len(frame), keyFrame))
		}
		return s.callback(frame, id, timestamp, keyFrame)
	}
	for {
		n, err := reader.Read(buffer)
		if n > 0 {
			if s.outputBytes.Add(uint64(n)) == uint64(n) && s.logf != nil {
				s.logf(fmt.Sprintf("FFmpeg H.264 stdout became active: first chunk %d bytes", n))
			}
			s.lastOutputNs.Store(time.Now().UnixNano())
			if parseErr := parser.Feed(buffer[:n], emit); parseErr != nil {
				if s.logf != nil {
					s.logf("H.264 stream parser stopped: " + parseErr.Error())
				}
				_ = s.cmd.Process.Kill()
				return
			}
		}
		if err != nil {
			if !errors.Is(err, io.EOF) && s.logf != nil {
				s.logf("FFmpeg output read failed: " + err.Error())
			}
			_ = parser.Flush(emit)
			waitErr := s.cmd.Wait()
			if waitErr != nil && s.logf != nil {
				s.logf("FFmpeg encoder exited: " + waitErr.Error())
			}
			return
		}
	}
}

func (s *H264EncoderSession) Close() {
	if s == nil || !s.closed.CompareAndSwap(false, true) {
		return
	}
	_ = s.stdin.Close()
	select {
	case <-s.done:
	case <-time.After(2 * time.Second):
		if s.cmd.Process != nil {
			_ = s.cmd.Process.Kill()
		}
		<-s.done
	}
}

// Keep bytes imported for test helpers and parser comparisons without exposing
// mutable FFmpeg output buffers to callbacks.
