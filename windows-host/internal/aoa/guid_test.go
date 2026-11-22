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
