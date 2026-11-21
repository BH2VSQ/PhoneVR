//go:build !windows

package main

func ensureBundledPhoneVRDriverAtStartup() (string, error) {
	return "", nil
}
