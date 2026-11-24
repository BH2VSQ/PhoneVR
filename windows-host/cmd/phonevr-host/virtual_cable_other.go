//go:build !windows

package main

import "errors"

func (s *HostService) InstallVirtualCable() (string, error) {
	return "", errors.New("VB-CABLE installation is supported on Windows only")
}
