package aoa

import "strings"

// packSetupPacket returns WINUSB_SETUP_PACKET's eight bytes packed into a
// little-endian uint64. WinUsb_ControlTransfer receives this structure by
// value, not as a pointer.
func packSetupPacket(requestType, request byte, value, index, length uint16) uint64 {
	return uint64(requestType) |
		uint64(request)<<8 |
		uint64(value)<<16 |
		uint64(index)<<32 |
		uint64(length)<<48
}

// isSupportedAOAProtocol accepts the protocol versions defined by the current
// Android Open Accessory specification. AOA v2 is backward-compatible with v1.
func isSupportedAOAProtocol(version uint16) bool {
	return version == 1 || version == 2
}

// includeWinUSBService keeps the scanner generic while excluding interfaces
// that Windows explicitly assigns to a different function driver. An empty
// interfaces without an explicit WinUSB service are not opened or probed.
func includeWinUSBService(service string) bool {
	return strings.EqualFold(strings.TrimSpace(service), "WinUSB")
}
