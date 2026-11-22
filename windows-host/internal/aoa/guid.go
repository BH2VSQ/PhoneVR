package aoa

import (
	"encoding/hex"
	"fmt"
	"strconv"
	"strings"
)

// guid matches the Windows GUID memory layout.
type guid struct {
	Data1 uint32
	Data2 uint16
	Data3 uint16
	Data4 [8]byte
}

func parseGUID(value string) (guid, error) {
	var result guid
	value = strings.TrimSpace(strings.Trim(value, "{}"))
	parts := strings.Split(value, "-")
	if len(parts) != 5 {
		return result, fmt.Errorf("invalid interface GUID %q", value)
	}
	data1, err := strconv.ParseUint(parts[0], 16, 32)
	if err != nil {
		return result, err
	}
	data2, err := strconv.ParseUint(parts[1], 16, 16)
	if err != nil {
		return result, err
	}
	data3, err := strconv.ParseUint(parts[2], 16, 16)
	if err != nil {
		return result, err
	}
	data4, err := hex.DecodeString(parts[3] + parts[4])
	if err != nil || len(data4) != 8 {
		return result, fmt.Errorf("invalid interface GUID data4 %q", value)
	}
	result.Data1 = uint32(data1)
	result.Data2 = uint16(data2)
	result.Data3 = uint16(data3)
	copy(result.Data4[:], data4)
	return result, nil
}

// parseUniqueGUIDs converts registry values into unique interface GUIDs.
// It intentionally does not add GUID_DEVINTERFACE_USB_DEVICE as a fallback:
// only GUIDs explicitly registered by a device function driver are valid
// WinUSB interface enumeration classes.
func parseUniqueGUIDs(values ...[]string) []guid {
	seen := make(map[guid]struct{})
	result := make([]guid, 0)
	for _, group := range values {
		for _, value := range group {
			parsed, err := parseGUID(value)
			if err != nil {
				continue
			}
			if _, exists := seen[parsed]; exists {
				continue
			}
			seen[parsed] = struct{}{}
			result = append(result, parsed)
		}
	}
	return result
}
