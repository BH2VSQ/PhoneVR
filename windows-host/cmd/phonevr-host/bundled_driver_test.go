package main

import (
	"encoding/binary"
	"os"
	"path/filepath"
	"testing"
)

func fakeWindowsAMD64PE() []byte {
	data := make([]byte, 0x40+6)
	data[0], data[1] = 'M', 'Z'
	binary.LittleEndian.PutUint32(data[0x3c:0x40], 0x40)
	copy(data[0x40:0x44], []byte("PE\x00\x00"))
	binary.LittleEndian.PutUint16(data[0x44:0x46], 0x8664)
	return data
}

func testBundledDriverFiles() map[string][]byte {
	return map[string][]byte{
		phoneVRDriverManifestName: []byte(`{"name":"phonevr","directory":""}`),
		phoneVRDriverDLLName:      fakeWindowsAMD64PE(),
		"SHA256SUMS.txt":          []byte("test checksum file\n"),
	}
}

func TestValidateBundledDriverFiles(t *testing.T) {
	if err := validateBundledDriverFiles(testBundledDriverFiles()); err != nil {
		t.Fatalf("valid driver package rejected: %v", err)
	}
}

func TestValidateBundledDriverFilesRejectsMissingDLL(t *testing.T) {
	files := testBundledDriverFiles()
	delete(files, phoneVRDriverDLLName)
	if err := validateBundledDriverFiles(files); err == nil {
		t.Fatal("expected missing DLL to be rejected")
	}
}

func TestValidateBundledDriverFilesRejectsWrongManifest(t *testing.T) {
	files := testBundledDriverFiles()
	files[phoneVRDriverManifestName] = []byte(`{"name":"not-phonevr"}`)
	if err := validateBundledDriverFiles(files); err == nil {
		t.Fatal("expected mismatched manifest name to be rejected")
	}
}

func TestValidateBundledDriverFilesRejectsNonPEPayload(t *testing.T) {
	files := testBundledDriverFiles()
	files[phoneVRDriverDLLName] = []byte("not a Windows DLL")
	if err := validateBundledDriverFiles(files); err == nil {
		t.Fatal("expected non-PE DLL payload to be rejected")
	}
}

func TestValidateBundledDriverFilesRejectsX86PE(t *testing.T) {
	files := testBundledDriverFiles()
	dll := fakeWindowsAMD64PE()
	binary.LittleEndian.PutUint16(dll[0x44:0x46], 0x014c)
	files[phoneVRDriverDLLName] = dll
	if err := validateBundledDriverFiles(files); err == nil {
		t.Fatal("expected 32-bit Windows PE payload to be rejected")
	}
}

func TestSafeDriverRelativePath(t *testing.T) {
	good := []string{"driver.vrdrivermanifest", "bin/win64/driver_phonevr.dll", "resources/settings/default.vrsettings"}
	for _, name := range good {
		if !safeDriverRelativePath(name) {
			t.Errorf("expected safe path %q", name)
		}
	}
	bad := []string{"../outside.dll", "bin/../../outside.dll", "/absolute/path", `bin\\win64\\driver.dll`, "C:/driver.dll", "a/../b"}
	for _, name := range bad {
		if safeDriverRelativePath(name) {
			t.Errorf("expected unsafe path %q to be rejected", name)
		}
	}
}

func TestBundledDriverDigestIsDeterministicAndContentSensitive(t *testing.T) {
	files := testBundledDriverFiles()
	first := bundledDriverDigest(files)
	second := bundledDriverDigest(testBundledDriverFiles())
	if first != second {
		t.Fatalf("digest changed for identical driver files: %s != %s", first, second)
	}
	files["SHA256SUMS.txt"] = []byte("updated checksum file\n")
	if changed := bundledDriverDigest(files); changed == first {
		t.Fatal("digest did not change after driver content changed")
	}
}

func TestWriteAndVerifyBundledDriverFiles(t *testing.T) {
	root := filepath.Join(t.TempDir(), "driver")
	files := testBundledDriverFiles()
	if err := writeBundledDriverFiles(root, files); err != nil {
		t.Fatalf("write bundled driver: %v", err)
	}
	if !bundledDriverDirectoryMatches(root, files) {
		t.Fatal("installed driver files did not match embedded content")
	}
	if err := os.WriteFile(filepath.Join(root, "SHA256SUMS.txt"), []byte("tampered"), 0o600); err != nil {
		t.Fatal(err)
	}
	if bundledDriverDirectoryMatches(root, files) {
		t.Fatal("tampered managed driver was accepted")
	}
}
