package main

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	phonevraudio "github.com/vsq-studio/phonevr-host/internal/audio"
)

var errAudioBridgeUnsupported = errors.New("Windows WASAPI bridge unsupported on this platform")

const (
	audioBridgeStereoFrameBytes = phonevraudio.SamplesPerFrame * 2 * 2
	audioBridgeMonoFrameBytes   = phonevraudio.SamplesPerFrame * 2
	audioBridgeMaxStatusBytes   = 16 * 1024
)

type audioBridgeRuntime struct {
	stdin  io.WriteCloser
	stdout io.ReadCloser
	done   <-chan error
	cancel context.CancelFunc
}

type audioEndpointStatus struct {
	Version                      int    `json:"version"`
	SampleRate                   int    `json:"sampleRate"`
	RenderEndpointID             string `json:"renderEndpointId"`
	RenderFriendlyName           string `json:"renderFriendlyName"`
	MicrophoneEndpointID         string `json:"microphoneEndpointId"`
	MicrophoneFriendlyName       string `json:"microphoneFriendlyName"`
	MicrophoneRenderEndpointID   string `json:"microphoneRenderEndpointId"`
	MicrophoneRenderFriendlyName string `json:"microphoneRenderFriendlyName"`
	MicrophoneRouteProvider      string `json:"microphoneRouteProvider"`
	MicrophoneRouteActive        bool   `json:"microphoneRouteActive"`
}

// decodeAudioEndpointStatusRecord decodes the helper's standalone PVRS record.
// It is also used by the one-shot endpoint probe when HostService is stopped.
func decodeAudioEndpointStatusRecord(record []byte) (audioEndpointStatus, error) {
	if len(record) < 8 || string(record[:4]) != "PVRS" {
		return audioEndpointStatus{}, errors.New("invalid audio endpoint status record header")
	}
	size := binary.LittleEndian.Uint32(record[4:8])
	if size == 0 || size > audioBridgeMaxStatusBytes || int(size) != len(record)-8 {
		return audioEndpointStatus{}, fmt.Errorf("invalid audio endpoint status record size %d", size)
	}
	var status audioEndpointStatus
	if err := json.Unmarshal(record[8:], &status); err != nil {
		return audioEndpointStatus{}, fmt.Errorf("decode audio endpoint status record: %w", err)
	}
	if status.Version != 1 || status.SampleRate != phonevraudio.SampleRate {
		return audioEndpointStatus{}, fmt.Errorf("unsupported audio endpoint status version=%d sampleRate=%d", status.Version, status.SampleRate)
	}
	return status, nil
}

// isCompatibleVirtualCableStatus confirms that Host has both sides of a
// virtual playback/recording pair. It is platform-independent so endpoint
// pairing policy can be unit-tested without Windows MMDevice APIs.
func isCompatibleVirtualCableStatus(status audioEndpointStatus) bool {
	if status.MicrophoneEndpointID == "" || status.MicrophoneRenderEndpointID == "" {
		return false
	}
	provider := strings.ToLower(strings.TrimSpace(status.MicrophoneRouteProvider))
	if provider == "vb-cable" || provider == "vac" {
		return true
	}
	return (strings.Contains(strings.ToLower(status.MicrophoneFriendlyName), "cable output") &&
		strings.Contains(strings.ToLower(status.MicrophoneRenderFriendlyName), "cable input")) ||
		isVACFriendlyName(status.MicrophoneFriendlyName, status.MicrophoneRenderFriendlyName)
}

func isVACFriendlyName(captureName, renderName string) bool {
	capture := strings.ToLower(strings.TrimSpace(captureName))
	render := strings.ToLower(strings.TrimSpace(renderName))
	return capture != "" && capture == render && strings.Contains(capture, "virtual audio cable")
}

// audioEndpointWorker owns the Windows bridge process for exactly the same
// lifetime as HostService.Start/Stop. It is isolated from video, pose and USB
// accessory workers, and restarts the audio helper after endpoint invalidation.
func (s *HostService) audioEndpointWorker(ctx context.Context) {
	defer s.wg.Done()
	defer func() {
		if err := clearAudioEndpointStatus(); err != nil {
			s.addLog("Unable to clear Windows audio endpoint IDs after Host stopped: " + err.Error())
		}
	}()
	backoff := time.Second
	loggedUnavailable := false
	for ctx.Err() == nil {
		// Do not leave stale endpoint IDs published while a helper is restarting
		// or WASAPI cannot open the current default device. SteamVR will unset its
		// audio properties until the replacement helper reports fresh endpoints.
		if err := clearAudioEndpointStatus(); err != nil {
			s.addLog("Unable to clear stale Windows audio endpoint IDs: " + err.Error())
		}
		childCtx, cancel := context.WithCancel(ctx)
		runtime, err := startAudioBridgeProcess(childCtx, s)
		if err != nil {
			cancel()
			if errors.Is(err, errAudioBridgeUnsupported) {
				return
			}
			if !loggedUnavailable {
				s.addLog("Windows audio bridge unavailable: " + err.Error())
				loggedUnavailable = true
			}
			if !waitAudioRetry(ctx, backoff) {
				return
			}
			backoff = nextAudioBackoff(backoff)
			continue
		}
		loggedUnavailable = false
		startedAt := time.Now()
		s.addLog("Windows audio bridge process started; WASAPI loopback and existing virtual-cable microphone route supervision are active")

		readErr := make(chan error, 1)
		writeErr := make(chan error, 1)
		go func() { readErr <- s.readAudioBridgeOutput(childCtx, runtime.stdout) }()
		go func() { writeErr <- s.writeAudioBridgeInput(childCtx, runtime.stdin) }()

		var reason error
		processFinished, readerFinished, writerFinished := false, false, false
		select {
		case <-ctx.Done():
		case reason = <-runtime.done:
			processFinished = true
		case reason = <-readErr:
			readerFinished = true
		case reason = <-writeErr:
			writerFinished = true
		}
		cancel()
		if runtime.cancel != nil {
			runtime.cancel()
		}
		_ = runtime.stdin.Close()
		_ = runtime.stdout.Close()
		// Host.Stop waits for this supervisor. Reap the child and both pipe
		// workers before returning so no audio process/goroutine outlives Host.
		if !processFinished {
			<-runtime.done
		}
		if !readerFinished {
			<-readErr
		}
		if !writerFinished {
			<-writeErr
		}
		if reason != nil && ctx.Err() == nil {
			s.addLog("Windows audio bridge stopped: " + reason.Error())
		}
		if ctx.Err() != nil {
			return
		}
		// Reset the retry delay only after a sustained healthy session. A helper
		// that exits a few seconds after launch must not pin the retry loop at 1s.
		if time.Since(startedAt) >= 30*time.Second {
			backoff = time.Second
		}
		if !waitAudioRetry(ctx, backoff) {
			return
		}
		backoff = nextAudioBackoff(backoff)
	}
}

func nextAudioBackoff(current time.Duration) time.Duration {
	current *= 2
	if current > 15*time.Second {
		return 15 * time.Second
	}
	return current
}

func waitAudioRetry(ctx context.Context, delay time.Duration) bool {
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}

func (s *HostService) readAudioBridgeOutput(ctx context.Context, output io.Reader) error {
	var tag [4]byte
	for ctx.Err() == nil {
		if _, err := io.ReadFull(output, tag[:]); err != nil {
			if ctx.Err() != nil || errors.Is(err, io.EOF) || errors.Is(err, io.ErrClosedPipe) {
				return err
			}
			return fmt.Errorf("read audio bridge record tag: %w", err)
		}
		switch string(tag[:]) {
		case "PVRA":
			frame := make([]byte, audioBridgeStereoFrameBytes)
			if _, err := io.ReadFull(output, frame); err != nil {
				return fmt.Errorf("read WASAPI PCM frame: %w", err)
			}
			if !s.hasAudioTarget() {
				continue
			}
			if err := s.PublishAudioPCM(frame); err != nil {
				// A target can disconnect between hasAudioTarget and the send.
				// Keep the audio worker quiet during pairing and reconnect windows.
				if !strings.Contains(err.Error(), "no active PhoneVR audio session") &&
					!strings.Contains(err.Error(), "return UDP address") {
					s.addLog("Audio downlink send failed: " + err.Error())
				}
			}
		case "PVRS":
			var sizeBytes [4]byte
			if _, err := io.ReadFull(output, sizeBytes[:]); err != nil {
				return fmt.Errorf("read audio endpoint status length: %w", err)
			}
			size := binary.LittleEndian.Uint32(sizeBytes[:])
			if size == 0 || size > audioBridgeMaxStatusBytes {
				return fmt.Errorf("invalid audio endpoint status size %d", size)
			}
			payload := make([]byte, size)
			if _, err := io.ReadFull(output, payload); err != nil {
				return fmt.Errorf("read audio endpoint status: %w", err)
			}
			var status audioEndpointStatus
			if err := json.Unmarshal(payload, &status); err != nil {
				return fmt.Errorf("decode audio endpoint status: %w", err)
			}
			if status.Version != 1 || status.SampleRate != phonevraudio.SampleRate || status.RenderEndpointID == "" {
				return errors.New("audio bridge returned incomplete endpoint status")
			}
			if err := saveAudioEndpointStatus(status); err != nil {
				s.addLog("Unable to publish Windows audio endpoint IDs to SteamVR: " + err.Error())
			}
			s.addLog(fmt.Sprintf("WASAPI loopback endpoint: %s", status.RenderFriendlyName))
			if status.MicrophoneEndpointID != "" && status.MicrophoneRenderEndpointID != "" {
				provider := strings.TrimSpace(status.MicrophoneRouteProvider)
				if provider == "" {
					provider = "existing virtual cable"
				}
				s.addLog(fmt.Sprintf("Phone microphone route active via %s: %s -> %s", provider, status.MicrophoneRenderFriendlyName, status.MicrophoneFriendlyName))
			} else {
				s.addLog("No supported virtual microphone cable detected. Existing VAC/VB-CABLE drivers are left untouched; install VB-CABLE only if you do not already have a compatible virtual cable.")
			}
		default:
			return fmt.Errorf("unknown Windows audio bridge record %q", string(tag[:]))
		}
	}
	return ctx.Err()
}

func (s *HostService) writeAudioBridgeInput(ctx context.Context, input io.Writer) error {
	queue := s.AudioUplinkFrames()
	for ctx.Err() == nil {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case packet, ok := <-queue:
			if !ok {
				return errors.New("phone microphone queue closed")
			}
			if packet.Direction != phonevraudio.DirectionPhoneToHost || packet.Channels != 1 || len(packet.Payload) != audioBridgeMonoFrameBytes {
				continue
			}
			if err := writeAll(input, packet.Payload); err != nil {
				return fmt.Errorf("forward phone microphone PCM to Windows endpoint: %w", err)
			}
		}
	}
	return ctx.Err()
}

func writeAll(writer io.Writer, data []byte) error {
	for len(data) > 0 {
		written, err := writer.Write(data)
		if err != nil {
			return err
		}
		if written <= 0 {
			return io.ErrShortWrite
		}
		data = data[written:]
	}
	return nil
}

func (s *HostService) hasAudioTarget() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, device := range s.devices {
		if device.Ready && !device.VideoOnly {
			return true
		}
	}
	return false
}

func audioEndpointStatusPath() (string, error) {
	base := strings.TrimSpace(os.Getenv("LOCALAPPDATA"))
	if base == "" {
		var err error
		base, err = os.UserConfigDir()
		if err != nil {
			return "", err
		}
	}
	return filepath.Join(base, "PhoneVR", "audio-endpoints.json"), nil
}

func clearAudioEndpointStatus() error {
	path, err := audioEndpointStatusPath()
	if err != nil {
		return err
	}
	base := filepath.Dir(path)
	var failures []error
	for _, name := range []string{
		"audio-endpoints.json",
		"audio-render-endpoint.txt",
		"audio-record-endpoint.txt",
		"audio-microphone-render-endpoint.txt",
	} {
		if removeErr := os.Remove(filepath.Join(base, name)); removeErr != nil && !errors.Is(removeErr, os.ErrNotExist) {
			failures = append(failures, fmt.Errorf("remove %s: %w", name, removeErr))
		}
	}
	return errors.Join(failures...)
}

func saveAudioEndpointStatus(status audioEndpointStatus) error {
	path, err := audioEndpointStatusPath()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(status, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	// This is diagnostic metadata, not a transactional state file. Write it
	// directly because os.Rename does not replace an existing destination on
	// Windows, where endpoint status can be emitted repeatedly by the helper.
	if err := os.WriteFile(path, data, 0o600); err != nil {
		return err
	}
	// OpenVR properties consume endpoint IDs as plain UTF-8 strings. Keep
	// these tiny files separate so the native driver does not need a JSON
	// parser and can refresh them while SteamVR is already running.
	base := filepath.Dir(path)
	for name, value := range map[string]string{
		"audio-render-endpoint.txt":            status.RenderEndpointID,
		"audio-record-endpoint.txt":            status.MicrophoneEndpointID,
		"audio-microphone-render-endpoint.txt": status.MicrophoneRenderEndpointID,
	} {
		endpointPath := filepath.Join(base, name)
		if value == "" {
			if err := os.Remove(endpointPath); err != nil && !errors.Is(err, os.ErrNotExist) {
				return fmt.Errorf("remove %s: %w", name, err)
			}
			continue
		}
		if err := os.WriteFile(endpointPath, []byte(value), 0o600); err != nil {
			return fmt.Errorf("write %s: %w", name, err)
		}
	}
	return nil
}
