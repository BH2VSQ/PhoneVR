package main

import (
	"archive/zip"
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

func makeVBCableZip(t *testing.T, entries map[string]string) []byte {
	t.Helper()
	var buffer bytes.Buffer
	writer := zip.NewWriter(&buffer)
	for name, contents := range entries {
		entry, err := writer.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := entry.Write([]byte(contents)); err != nil {
			t.Fatal(err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	return buffer.Bytes()
}

func TestExtractVBCablePackage(t *testing.T) {
	data := makeVBCableZip(t, map[string]string{
		"VBCABLE_Setup_x64.exe": "signed installer placeholder",
		"driver/VBCABLE.inf":   "driver package placeholder",
	})
	root := t.TempDir()
	installer, err := extractVBCablePackage(bytes.NewReader(data), int64(len(data)), root)
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Base(installer) != "VBCABLE_Setup_x64.exe" {
		t.Fatalf("unexpected installer path %q", installer)
	}
	if contents, err := os.ReadFile(installer); err != nil || string(contents) != "signed installer placeholder" {
		t.Fatalf("installer extraction failed: content=%q err=%v", contents, err)
	}
}

func TestExtractVBCablePackageRejectsTraversal(t *testing.T) {
	data := makeVBCableZip(t, map[string]string{
		"../outside.txt":         "no",
		"VBCABLE_Setup_x64.exe": "installer",
	})
	if _, err := extractVBCablePackage(bytes.NewReader(data), int64(len(data)), t.TempDir()); err == nil {
		t.Fatal("expected archive traversal to be rejected")
	}
}

func TestExtractVBCablePackageRejectsMissingInstaller(t *testing.T) {
	data := makeVBCableZip(t, map[string]string{"readme.txt": "no installer"})
	if _, err := extractVBCablePackage(bytes.NewReader(data), int64(len(data)), t.TempDir()); err == nil {
		t.Fatal("expected missing installer to be rejected")
	}
}
