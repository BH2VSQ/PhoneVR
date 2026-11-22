//go:build windows

package childprocess

import (
	"os/exec"
	"syscall"
	"testing"
)

func TestHideConsoleWindowConfiguresWindowsChildProcess(t *testing.T) {
	cmd := exec.Command("cmd.exe", "/c", "exit", "0")
	HideConsoleWindow(cmd)
	if cmd.SysProcAttr == nil {
		t.Fatal("SysProcAttr was not initialized")
	}
	if !cmd.SysProcAttr.HideWindow {
		t.Fatal("HideWindow is false")
	}
	if cmd.SysProcAttr.CreationFlags&createNoWindow == 0 {
		t.Fatalf("CreationFlags %#x does not include CREATE_NO_WINDOW", cmd.SysProcAttr.CreationFlags)
	}

	// Preserve pre-existing process attributes when applying the helper.
	cmd = exec.Command("cmd.exe", "/c", "exit", "0")
	cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: 0x00000200}
	HideConsoleWindow(cmd)
	if cmd.SysProcAttr.CreationFlags&0x00000200 == 0 {
		t.Fatal("existing creation flags were overwritten")
	}
}
