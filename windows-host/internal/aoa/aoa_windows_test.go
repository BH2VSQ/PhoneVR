//go:build windows && amd64

package aoa

import (
	"strings"
	"syscall"
	"testing"
)

func TestWinUSBCreateFileFlagsEnableOverlappedIO(t *testing.T) {
	flags := fileAttributeNormal | fileFlagOverlapped
	if flags&fileFlagOverlapped == 0 {
		t.Fatal("WinUSB CreateFile flags must include FILE_FLAG_OVERLAPPED")
	}
	if flags&fileAttributeNormal == 0 {
		t.Fatal("WinUSB CreateFile flags must include FILE_ATTRIBUTE_NORMAL")
	}
}

func TestWindowsCallErrorCode(t *testing.T) {
	got, ok := windowsCallErrorCode(syscall.Errno(5))
	if !ok || got != 5 {
		t.Fatalf("windowsCallErrorCode(ERROR_ACCESS_DENIED) = %d, %v; want 5, true", got, ok)
	}
}

func TestFormatWindowsCallErrorIncludesStageAndCode(t *testing.T) {
	err := formatWindowsCallError("WinUsb_Initialize", syscall.Errno(6))
	message := err.Error()
	if !strings.Contains(message, "WinUsb_Initialize") || !strings.Contains(message, "Win32 error 6") {
		t.Fatalf("formatted error = %q; want stage and Win32 error code", message)
	}
}
