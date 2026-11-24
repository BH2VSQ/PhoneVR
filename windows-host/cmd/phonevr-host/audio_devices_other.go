//go:build !windows

package main

import "errors"

func listWindowsAudioEndpoints() (AudioEndpointInventory, error) {
	return AudioEndpointInventory{}, errors.New("Windows audio endpoint enumeration is supported on Windows only")
}
