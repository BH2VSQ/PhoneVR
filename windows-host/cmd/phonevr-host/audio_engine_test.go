package main

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"
)

type shortWriter struct {
	buffer bytes.Buffer
	limit  int
}

func (w *shortWriter) Write(p []byte) (int, error) {
	if len(p) > w.limit {
		p = p[:w.limit]
	}
	return w.buffer.Write(p)
}

func TestWriteAllHandlesShortWrites(t *testing.T) {
	writer := &shortWriter{limit: 3}
	payload := []byte("PhoneVR microphone PCM stream")
	if err := writeAll(writer, payload); err != nil {
		t.Fatalf("writeAll returned error: %v", err)
	}
	if !bytes.Equal(writer.buffer.Bytes(), payload) {
		t.Fatalf("writeAll mismatch: got %q want %q", writer.buffer.Bytes(), payload)
	}
}

func TestSaveAudioEndpointStatusPublishesOpenVRIDs(t *testing.T) {
	root := t.TempDir()
	t.Setenv("LOCALAPPDATA", root)
	status := audioEndpointStatus{
		Version: 1, SampleRate: 48000,
		RenderEndpointID: "{render-device-id}", RenderFriendlyName: "Speakers",
		MicrophoneEndpointID: "{phonevr-mic-id}", MicrophoneFriendlyName: "PhoneVR Microphone",
	}
	if err := saveAudioEndpointStatus(status); err != nil {
		t.Fatalf("saveAudioEndpointStatus: %v", err)
	}
	base := filepath.Join(root, "PhoneVR")
	for name, want := range map[string]string{
		"audio-render-endpoint.txt": "{render-device-id}",
		"audio-record-endpoint.txt": "{phonevr-mic-id}",
	} {
		got, err := os.ReadFile(filepath.Join(base, name))
		if err != nil {
			t.Fatalf("read %s: %v", name, err)
		}
		if string(got) != want {
			t.Fatalf("%s = %q, want %q", name, got, want)
		}
	}
	statusData, err := os.ReadFile(filepath.Join(base, "audio-endpoints.json"))
	if err != nil {
		t.Fatalf("read audio-endpoints.json: %v", err)
	}
	var decoded audioEndpointStatus
	if err := json.Unmarshal(statusData, &decoded); err != nil {
		t.Fatalf("decode status json: %v", err)
	}
	if decoded.MicrophoneEndpointID != status.MicrophoneEndpointID {
		t.Fatalf("recording endpoint was not persisted: %#v", decoded)
	}

	status.MicrophoneEndpointID = ""
	status.MicrophoneFriendlyName = ""
	if err := saveAudioEndpointStatus(status); err != nil {
		t.Fatalf("save status without microphone endpoint: %v", err)
	}
	if _, err := os.Stat(filepath.Join(base, "audio-record-endpoint.txt")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("stale recording endpoint file remains: %v", err)
	}
}

func TestClearAudioEndpointStatusRemovesStaleOpenVRIDs(t *testing.T) {
	root := t.TempDir()
	t.Setenv("LOCALAPPDATA", root)
	base := filepath.Join(root, "PhoneVR")
	if err := os.MkdirAll(base, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"audio-endpoints.json", "audio-render-endpoint.txt", "audio-record-endpoint.txt"} {
		if err := os.WriteFile(filepath.Join(base, name), []byte("stale"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if err := clearAudioEndpointStatus(); err != nil {
		t.Fatalf("clearAudioEndpointStatus: %v", err)
	}
	for _, name := range []string{"audio-endpoints.json", "audio-render-endpoint.txt", "audio-record-endpoint.txt"} {
		if _, err := os.Stat(filepath.Join(base, name)); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("stale file %s remains: %v", name, err)
		}
	}
}

func TestReadAudioBridgeOutputParsesEndpointStatusAndFrames(t *testing.T) {
	root := t.TempDir()
	t.Setenv("LOCALAPPDATA", root)
	status := audioEndpointStatus{
		Version: 1, SampleRate: 48000,
		RenderEndpointID: "render-1", RenderFriendlyName: "Default Speakers",
		MicrophoneEndpointID: "mic-1", MicrophoneFriendlyName: "PhoneVR Microphone",
	}
	encoded, err := json.Marshal(status)
	if err != nil {
		t.Fatal(err)
	}
	var stream bytes.Buffer
	stream.WriteString("PVRS")
	var size [4]byte
	binary.LittleEndian.PutUint32(size[:], uint32(len(encoded)))
	stream.Write(size[:])
	stream.Write(encoded)
	stream.WriteString("PVRA")
	stream.Write(make([]byte, audioBridgeStereoFrameBytes))

	service := NewHostService(true)
	err = service.readAudioBridgeOutput(context.Background(), &stream)
	if !errors.Is(err, io.EOF) {
		t.Fatalf("expected EOF after complete fixture stream, got %v", err)
	}
	for name, want := range map[string]string{
		"audio-render-endpoint.txt": "render-1",
		"audio-record-endpoint.txt": "mic-1",
	} {
		got, readErr := os.ReadFile(filepath.Join(root, "PhoneVR", name))
		if readErr != nil {
			t.Fatalf("read published endpoint %s: %v", name, readErr)
		}
		if string(got) != want {
			t.Fatalf("endpoint %s = %q, want %q", name, got, want)
		}
	}
}

func TestNextAudioBackoffCapsAt15Seconds(t *testing.T) {
	delay := time.Second
	want := []time.Duration{2 * time.Second, 4 * time.Second, 8 * time.Second, 15 * time.Second, 15 * time.Second}
	for index, expected := range want {
		delay = nextAudioBackoff(delay)
		if delay != expected {
			t.Fatalf("backoff step %d = %s, want %s", index, delay, expected)
		}
	}
}

func TestWaitAudioRetryStopsOnContextCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if waitAudioRetry(ctx, time.Hour) {
		t.Fatal("waitAudioRetry returned true after cancellation")
	}
}
