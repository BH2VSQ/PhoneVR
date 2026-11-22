package aoa

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
