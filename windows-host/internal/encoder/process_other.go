//go:build !windows

package encoder

import "os/exec"

func configureFFmpegProcess(cmd *exec.Cmd) {}
