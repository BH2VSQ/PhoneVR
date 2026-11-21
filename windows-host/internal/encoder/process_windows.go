//go:build windows

package encoder

import (
	"os/exec"
	"syscall"
)

// configureFFmpegProcess prevents each FFmpeg preflight or streaming process
// from creating a visible console window beside the native PhoneVR Host UI.
func configureFFmpegProcess(cmd *exec.Cmd) {
	if cmd == nil {
		return
	}
	cmd.SysProcAttr = &syscall.SysProcAttr{
		HideWindow:    true,
		CreationFlags: 0x08000000, // CREATE_NO_WINDOW
	}
}
