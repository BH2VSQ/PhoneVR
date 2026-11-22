//go:build windows

package encoder

import (
	"os/exec"

	"github.com/vsq-studio/phonevr-host/internal/childprocess"
)

// configureFFmpegProcess applies the Host-wide silent child-process policy.
func configureFFmpegProcess(cmd *exec.Cmd) {
	childprocess.HideConsoleWindow(cmd)
}
