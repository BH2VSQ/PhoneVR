//go:build !windows

package main

import "errors"

func runNativeDesktopApp(_ *HostService, _ HostConfig) error {
	return errors.New("the native PhoneVR Host desktop UI is currently available on Windows; use -headless on other platforms")
}
