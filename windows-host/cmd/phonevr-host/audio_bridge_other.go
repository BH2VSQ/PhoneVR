//go:build !windows

package main

import (
	"context"
)

func startAudioBridgeProcess(context.Context, *HostService) (*audioBridgeRuntime, error) {
	return nil, errAudioBridgeUnsupported
}
