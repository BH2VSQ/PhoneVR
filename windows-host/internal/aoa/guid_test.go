package aoa

import "testing"

func TestParseGUID(t *testing.T) {
	got, err := parseGUID("{A5DCBF10-6530-11D2-901F-00C04FB951ED}")
	if err != nil {
		t.Fatal(err)
	}
	want := guid{0xA5DCBF10, 0x6530, 0x11D2, [8]byte{0x90, 0x1F, 0x00, 0xC0, 0x4F, 0xB9, 0x51, 0xED}}
	if got != want {
		t.Fatalf("parsed GUID = %#v, want %#v", got, want)
	}
}

func TestParseGUIDRejectsMalformedValues(t *testing.T) {
	for _, value := range []string{"", "not-a-guid", "{1234}", "A5DCBF10-6530-11D2-901F-XXC04FB951ED"} {
		if _, err := parseGUID(value); err == nil {
			t.Errorf("parseGUID(%q) unexpectedly succeeded", value)
		}
	}
}

func TestParseUniqueGUIDsUsesOnlyRegisteredInterfaces(t *testing.T) {
	wantFirst := guid{0xF72FE0D4, 0xCBCB, 0x407D, [8]byte{0x88, 0x14, 0x9E, 0xD6, 0x73, 0xD0, 0xDD, 0x6B}}
	wantSecond := guid{0xD696BFEB, 0x1734, 0x417D, [8]byte{0x8A, 0x04, 0x86, 0xD0, 0x10, 0x71, 0xC5, 0x12}}
	got := parseUniqueGUIDs(
		[]string{"{F72FE0D4-CBCB-407D-8814-9ED673D0DD6B}", "not-a-guid"},
		[]string{"{F72FE0D4-CBCB-407D-8814-9ED673D0DD6B}", "{D696BFEB-1734-417D-8A04-86D01071C512}"},
	)
	if len(got) != 2 || got[0] != wantFirst || got[1] != wantSecond {
		t.Fatalf("parseUniqueGUIDs() = %#v, want both registered GUIDs %#v and %#v", got, wantFirst, wantSecond)
	}
}

func TestParseUniqueGUIDsDoesNotInjectGenericUSBInterface(t *testing.T) {
	if got := parseUniqueGUIDs(); len(got) != 0 {
		t.Fatalf("parseUniqueGUIDs() without registered values = %#v, want empty", got)
	}
}

func TestIsSupportedAOAProtocol(t *testing.T) {
	for _, version := range []uint16{1, 2} {
		if !isSupportedAOAProtocol(version) {
			t.Errorf("AOA protocol version %d should be supported", version)
		}
	}
	for _, version := range []uint16{0, 3, 0xFFFF} {
		if isSupportedAOAProtocol(version) {
			t.Errorf("AOA protocol version %d should be rejected", version)
		}
	}
}

func TestIncludeWinUSBService(t *testing.T) {
	for _, service := range []string{"WinUSB", "winusb", " WinUSB "} {
		if !includeWinUSBService(service) {
			t.Errorf("service %q should be included", service)
		}
	}
	for _, service := range []string{"", "USBSTOR", "WUDFRd", "usbccgp", "BthUsb"} {
		if includeWinUSBService(service) {
			t.Errorf("service %q should be excluded", service)
		}
	}
}
