//go:build !windows

package encoder

import "os/exec"

func configureFFmpegProcess(_ *exec.Cmd) {}
