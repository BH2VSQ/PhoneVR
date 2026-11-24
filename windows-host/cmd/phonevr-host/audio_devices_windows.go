//go:build windows

package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"time"

	"github.com/vsq-studio/phonevr-host/internal/childprocess"
)

func listWindowsAudioEndpoints() (AudioEndpointInventory, error) {
	path, err := embeddedAudioBridgeExecutable()
	if err != nil {
		return AudioEndpointInventory{}, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, path, "--list-endpoints")
	childprocess.HideConsoleWindow(command)
	output, err := command.Output()
	if err != nil {
		return AudioEndpointInventory{}, fmt.Errorf("enumerate Windows audio endpoints: %w", err)
	}
	var inventory AudioEndpointInventory
	if err := json.Unmarshal(output, &inventory); err != nil {
		return AudioEndpointInventory{}, fmt.Errorf("decode Windows audio endpoint inventory: %w", err)
	}
	inventory = inventory.normalized()
	if err := validateAudioEndpointInventory(inventory); err != nil {
		return AudioEndpointInventory{}, err
	}
	return inventory, nil
}
