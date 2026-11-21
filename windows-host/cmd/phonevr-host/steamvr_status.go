package main

import "encoding/binary"

const (
	steamVRFlagDriverLoaded uint8 = 1 << iota
	steamVRFlagHMDActive
	steamVRFlagPoseValid
	steamVRFlagDisplayActive
	steamVRFlagPresentedFrame
)

const steamVRHeartbeatPacketSize = 16

type steamVRDriverHeartbeat struct {
	Flags        uint8
	FrameCounter uint64
}

// decodeSteamVRDriverHeartbeat validates the small, loopback-only status packet
// sent by the native OpenVR driver. Layout: magic[4], version, flags, reserved[2],
// frameCounter[8] (big endian).
func decodeSteamVRDriverHeartbeat(packet []byte) (steamVRDriverHeartbeat, bool) {
	if len(packet) != steamVRHeartbeatPacketSize ||
		string(packet[:4]) != "PVST" || packet[4] != 1 {
		return steamVRDriverHeartbeat{}, false
	}
	return steamVRDriverHeartbeat{
		Flags:        packet[5],
		FrameCounter: binary.BigEndian.Uint64(packet[8:16]),
	}, true
}
