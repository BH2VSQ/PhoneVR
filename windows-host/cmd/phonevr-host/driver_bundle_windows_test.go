//go:build windows

package main

import (
	"os"
	"path/filepath"
	"testing"
)

func writeTestPhoneVRDriverPackage(t *testing.T, root string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(root, "bin", "win64"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "driver.vrdrivermanifest"), []byte(`{"name":"phonevr","directory":"","resourceOnly":false}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "bin", "win64", "driver_phonevr.dll"), []byte("test-dll"), 0o700); err != nil {
		t.Fatal(err)
	}
}

func TestValidatePhoneVRDriverDirectory(t *testing.T) {
	root := t.TempDir()
	writeTestPhoneVRDriverPackage(t, root)
	if err := validatePhoneVRDriverDirectory(root); err != nil {
		t.Fatalf("valid package rejected: %v", err)
	}
	if err := os.Remove(filepath.Join(root, "bin", "win64", "driver_phonevr.dll")); err != nil {
		t.Fatal(err)
	}
	if err := validatePhoneVRDriverDirectory(root); err == nil {
		t.Fatal("package without driver DLL was accepted")
	}
}

func TestPhoneVRDriverPackageDigestIsStableAndSensitiveToContent(t *testing.T) {
	root := t.TempDir()
	writeTestPhoneVRDriverPackage(t, root)
	first, err := phoneVRDriverPackageDigest(root)
	if err != nil {
		t.Fatal(err)
	}
	second, err := phoneVRDriverPackageDigest(root)
	if err != nil {
		t.Fatal(err)
	}
	if first != second {
		t.Fatalf("digest changed without package changes: %s != %s", first, second)
	}
	if err := os.WriteFile(filepath.Join(root, "resources.txt"), []byte("resource-v2"), 0o600); err != nil {
		t.Fatal(err)
	}
	third, err := phoneVRDriverPackageDigest(root)
	if err != nil {
		t.Fatal(err)
	}
	if first == third {
		t.Fatal("digest did not change after a package file was added")
	}
}

func TestCopyPhoneVRDriverDirectory(t *testing.T) {
	source := filepath.Join(t.TempDir(), "source")
	destination := filepath.Join(t.TempDir(), "destination")
	writeTestPhoneVRDriverPackage(t, source)
	if err := copyPhoneVRDriverDirectory(source, destination); err != nil {
		t.Fatal(err)
	}
	if err := validatePhoneVRDriverDirectory(destination); err != nil {
		t.Fatalf("copied package is invalid: %v", err)
	}
	sourceDigest, err := phoneVRDriverPackageDigest(source)
	if err != nil {
		t.Fatal(err)
	}
	destinationDigest, err := phoneVRDriverPackageDigest(destination)
	if err != nil {
		t.Fatal(err)
	}
	if sourceDigest != destinationDigest {
		t.Fatalf("copied package digest mismatch: %s != %s", sourceDigest, destinationDigest)
	}
}

func TestPhoneVRDriverAutoInstallOptOutPersistsUntilReenabled(t *testing.T) {
	t.Setenv("LOCALAPPDATA", t.TempDir())
	if disabled, err := phoneVRDriverAutoInstallDisabled(); err != nil {
		t.Fatal(err)
	} else if disabled {
		t.Fatal("auto-install must be enabled by default")
	}
	if err := setPhoneVRDriverAutoInstallDisabled(true); err != nil {
		t.Fatal(err)
	}
	if disabled, err := phoneVRDriverAutoInstallDisabled(); err != nil {
		t.Fatal(err)
	} else if !disabled {
		t.Fatal("uninstall opt-out marker was not detected")
	}
	if err := setPhoneVRDriverAutoInstallDisabled(false); err != nil {
		t.Fatal(err)
	}
	if disabled, err := phoneVRDriverAutoInstallDisabled(); err != nil {
		t.Fatal(err)
	} else if disabled {
		t.Fatal("install/repair must clear the auto-install opt-out marker")
	}
}

func TestAutoInstallBundledPhoneVRDriverHonorsUserOptOut(t *testing.T) {
	t.Setenv("LOCALAPPDATA", t.TempDir())
	if err := setPhoneVRDriverAutoInstallDisabled(true); err != nil {
		t.Fatal(err)
	}
	message, err := autoInstallBundledPhoneVRDriver()
	if err != nil {
		t.Fatalf("auto-install should honor opt-out without resolving the bundle: %v", err)
	}
	if message != "" {
		t.Fatalf("auto-install returned an unexpected message while disabled: %q", message)
	}
}
