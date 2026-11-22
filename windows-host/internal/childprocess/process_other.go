//go:build !windows

package childprocess

import "os/exec"

// HideConsoleWindow is a no-op outside Windows. It keeps call sites portable.
func HideConsoleWindow(_ *exec.Cmd) {}
