//go:build windows

package main

import (
	"context"
	"crypto/sha256"
	"embed"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/vsq-studio/phonevr-host/internal/childprocess"
)

//go:embed embedded-audio
var embeddedPhoneVRAudio embed.FS

func embeddedAudioBridgeExecutable() (string, error) {
	data, err := embeddedPhoneVRAudio.ReadFile("embedded-audio/PhoneVRAudioBridge.exe")
	if err != nil || len(data) < 1024 {
		return "", errors.New("PhoneVRAudioBridge.exe is not embedded; rebuild with scripts/build-windows-host.ps1")
	}
	base := strings.TrimSpace(os.Getenv("LOCALAPPDATA"))
	if base == "" {
		base, err = os.UserConfigDir()
		if err != nil {
			return "", err
		}
	}
	directory := filepath.Join(base, "PhoneVR", "AudioBridge")
	if err := os.MkdirAll(directory, 0o700); err != nil {
		return "", fmt.Errorf("create audio bridge directory: %w", err)
	}
	digest := sha256.Sum256(data)
	// A content-addressed executable path avoids replacing an image that may
	// still be mapped by a previous Host process. It also avoids Windows
	// os.Rename semantics, which do not replace an existing destination.
	path := filepath.Join(directory, fmt.Sprintf("PhoneVRAudioBridge-%x.exe", digest[:8]))
	needsWrite := true
	if existing, readErr := os.ReadFile(path); readErr == nil && sha256.Sum256(existing) == digest {
		needsWrite = false
	}
	if needsWrite {
		// A corrupt file at the canonical content-addressed path cannot be
		// replaced by os.Rename on Windows. Publish a uniquely named recovery
		// image instead of deleting a file another Host instance may be using.
		if _, statErr := os.Stat(path); statErr == nil {
			path = filepath.Join(directory, fmt.Sprintf("PhoneVRAudioBridge-%x-%d-%d.exe", digest[:8], os.Getpid(), time.Now().UnixNano()))
		}
		staged, err := os.CreateTemp(directory, "PhoneVRAudioBridge-*.tmp")
		if err != nil {
			return "", fmt.Errorf("stage Windows audio bridge: %w", err)
		}
		temp := staged.Name()
		if err := staged.Chmod(0o700); err != nil {
			_ = staged.Close()
			_ = os.Remove(temp)
			return "", fmt.Errorf("set Windows audio bridge permissions: %w", err)
		}
		if _, err := staged.Write(data); err != nil {
			_ = staged.Close()
			_ = os.Remove(temp)
			return "", fmt.Errorf("write staged Windows audio bridge: %w", err)
		}
		if err := staged.Close(); err != nil {
			_ = os.Remove(temp)
			return "", fmt.Errorf("close staged Windows audio bridge: %w", err)
		}
		if err := os.Rename(temp, path); err != nil {
			_ = os.Remove(temp)
			// Another Host instance may have published the same content-addressed
			// image concurrently. Accept it only after verifying the exact digest.
			if existing, readErr := os.ReadFile(path); readErr != nil || sha256.Sum256(existing) != digest {
				return "", fmt.Errorf("publish Windows audio bridge: %w", err)
			}
		}
	}

	return path, nil
}

func startAudioBridgeProcess(parent context.Context, service *HostService) (*audioBridgeRuntime, error) {
	path, err := embeddedAudioBridgeExecutable()
	if err != nil {
		return nil, err
	}
	// Use explicit OS pipes instead of StdoutPipe/StdinPipe. Cmd.Wait closes
	// the latter as soon as the child exits, which can race the Host's reader
	// while it drains the final complete PVRA/PVRS record.
	hostStdin, childStdin, err := os.Pipe()
	if err != nil {
		return nil, fmt.Errorf("create audio bridge stdin pipe: %w", err)
	}
	hostStdout, childStdout, err := os.Pipe()
	if err != nil {
		_ = hostStdin.Close()
		_ = childStdin.Close()
		return nil, fmt.Errorf("create audio bridge stdout pipe: %w", err)
	}
	ctx, cancel := context.WithCancel(parent)
	cmd := exec.CommandContext(ctx, path)
	cmd.Stdin = childStdin
	cmd.Stdout = childStdout
	cmd.Stderr = &audioBridgeLogWriter{service: service}
	childprocess.HideConsoleWindow(cmd)
	if err := cmd.Start(); err != nil {
		cancel()
		_ = hostStdin.Close()
		_ = childStdin.Close()
		_ = hostStdout.Close()
		_ = childStdout.Close()
		return nil, fmt.Errorf("start PhoneVRAudioBridge.exe: %w", err)
	}
	// The child inherited duplicated standard handles during Start. Retain
	// only the Host ends in this process so EOF and cancellation propagate.
	_ = childStdin.Close()
	_ = childStdout.Close()
	done := make(chan error, 1)
	go func() { done <- cmd.Wait(); close(done) }()
	return &audioBridgeRuntime{stdin: hostStdin, stdout: hostStdout, done: done, cancel: cancel}, nil
}

type audioBridgeLogWriter struct {
	service *HostService
	mu      sync.Mutex
	pending string
}

func (w *audioBridgeLogWriter) Write(data []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.pending += string(data)
	for {
		index := strings.IndexByte(w.pending, '\n')
		if index < 0 {
			break
		}
		line := strings.TrimSpace(w.pending[:index])
		w.pending = w.pending[index+1:]
		if line != "" {
			w.service.addLog(line)
		}
	}
	return len(data), nil
}

var _ io.Writer = (*audioBridgeLogWriter)(nil)
