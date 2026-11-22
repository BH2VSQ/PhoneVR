//go:build windows

package childprocess

import (
	"os/exec"
	"syscall"
)

const createNoWindow = 0x08000000 // CREATE_NO_WINDOW

// HideConsoleWindow prevents console-subsystem child processes from flashing
// a console window when launched by the GUI Host. Stdout/stderr redirection
// remains controlled by the caller, so CombinedOutput and pipes keep working.
func HideConsoleWindow(cmd *exec.Cmd) {
	if cmd == nil {
		return
	}
	attrs := cmd.SysProcAttr
	if attrs == nil {
		attrs = &syscall.SysProcAttr{}
	}
	attrs.HideWindow = true
	attrs.CreationFlags |= createNoWindow
	cmd.SysProcAttr = attrs
}
