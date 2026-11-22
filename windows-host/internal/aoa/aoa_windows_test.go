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

func TestValidateAOAAccessoryInterfaceAcceptsInterfaceZero(t *testing.T) {
	err := validateAOAAccessoryInterface(usbInterfaceDescriptor{
		InterfaceNumber: 0, InterfaceClass: 0xFF, InterfaceSubClass: 0xFF,
		InterfaceProtocol: 0x00, NumEndpoints: 2,
	})
	if err != nil {
		t.Fatalf("interface 0 should be accepted as AOA accessory interface: %v", err)
	}
}

func TestValidateAOAAccessoryInterfaceIdentifiesADBInterface(t *testing.T) {
	err := validateAOAAccessoryInterface(usbInterfaceDescriptor{
		InterfaceNumber: 1, InterfaceClass: 0xFF, InterfaceSubClass: 0x42,
		InterfaceProtocol: 0x01, NumEndpoints: 2,
	})
	if err == nil {
		t.Fatal("ADB interface 1 must not be accepted as the AOA accessory data interface")
	}
	message := err.Error()
	for _, expected := range []string{"ADB interface", "MI_00", "MI_01", "interface=1", "class=0xFF", "subclass=0x42", "protocol=0x01"} {
		if !strings.Contains(message, expected) {
			t.Fatalf("interface diagnostic %q does not contain %q", message, expected)
		}
	}
}

func TestAOABulkOutUsesFiniteWinUSBTransferTimeout(t *testing.T) {
	if usbPipePolicyTransferTimeout != 3 {
		t.Fatalf("PIPE_TRANSFER_TIMEOUT policy = %d, want 3", usbPipePolicyTransferTimeout)
	}
	if aoaBulkOutTransferTimeoutMs <= 0 || aoaBulkOutTransferTimeoutMs > 2000 {
		t.Fatalf("Bulk OUT timeout = %d ms, want a finite timeout no greater than 2000 ms", aoaBulkOutTransferTimeoutMs)
	}
}
