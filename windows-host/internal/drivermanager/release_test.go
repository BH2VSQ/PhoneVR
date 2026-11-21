package drivermanager

import (
	"archive/zip"
	"os"
	"path/filepath"
	"testing"
)

func makeDriverZip(t *testing.T, path string, entries map[string]string) {
	t.Helper()
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	zw := zip.NewWriter(f)
	for name, body := range entries {
		w, err := zw.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write([]byte(body)); err != nil {
			t.Fatal(err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestValidateAndExtractDriverZip(t *testing.T) {
	temp := t.TempDir()
	archive := filepath.Join(temp, "driver.zip")
	makeDriverZip(t, archive, map[string]string{
		"driver.vrdrivermanifest":      `{"name":"phonevr","directory":""}`,
		"bin/win64/driver_phonevr.dll": "test dll bytes",
		"resources/input/phonevr.json": "{}",
	})
	out := filepath.Join(temp, "installed")
	if err := ValidateAndExtractDriverZip(archive, out); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"driver.vrdrivermanifest", "bin/win64/driver_phonevr.dll", "resources/input/phonevr.json"} {
		if _, err := os.Stat(filepath.Join(out, filepath.FromSlash(path))); err != nil {
			t.Fatalf("missing %s: %v", path, err)
		}
	}
}

func TestValidateAndExtractRejectsWrongManifestAndMissingDLL(t *testing.T) {
	for name, entries := range map[string]map[string]string{
		"wrong-name":  {"driver.vrdrivermanifest": `{"name":"other"}`, "bin/win64/driver_phonevr.dll": "dll"},
		"missing-dll": {"driver.vrdrivermanifest": `{"name":"phonevr"}`},
	} {
		t.Run(name, func(t *testing.T) {
			temp := t.TempDir()
			archive := filepath.Join(temp, "driver.zip")
			makeDriverZip(t, archive, entries)
			if err := ValidateAndExtractDriverZip(archive, filepath.Join(temp, "out")); err == nil {
				t.Fatal("expected invalid driver package to be rejected")
			}
		})
	}
}

func TestValidateAndExtractRejectsTraversal(t *testing.T) {
	temp := t.TempDir()
	archive := filepath.Join(temp, "driver.zip")
	makeDriverZip(t, archive, map[string]string{
		"driver.vrdrivermanifest":      `{"name":"phonevr"}`,
		"bin/win64/driver_phonevr.dll": "dll",
		"../../outside.txt":            "bad",
	})
	if err := ValidateAndExtractDriverZip(archive, filepath.Join(temp, "out")); err == nil {
		t.Fatal("expected path traversal to be rejected")
	}
	if _, err := os.Stat(filepath.Join(temp, "outside.txt")); !os.IsNotExist(err) {
		t.Fatal("archive wrote outside destination")
	}
}
