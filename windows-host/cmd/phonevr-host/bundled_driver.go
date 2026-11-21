package main

import (
	"crypto/sha256"
	"embed"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

const phoneVRDriverManifestName = "driver.vrdrivermanifest"
const phoneVRDriverDLLName = "bin/win64/driver_phonevr.dll"

// embeddedPhoneVRDriver is populated at build time by build-windows-host.ps1.
// A README placeholder keeps ordinary source checkouts and `go test` buildable.
//
//go:embed embedded-driver
var embeddedPhoneVRDriver embed.FS

func readEmbeddedPhoneVRDriver() (map[string][]byte, string, error) {
	root, err := fs.Sub(embeddedPhoneVRDriver, "embedded-driver")
	if err != nil {
		return nil, "", fmt.Errorf("open embedded PhoneVR driver: %w", err)
	}
	files := make(map[string][]byte)
	err = fs.WalkDir(root, ".", func(name string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			return nil
		}
		if name == "README.txt" {
			return nil
		}
		if !safeDriverRelativePath(name) {
			return fmt.Errorf("embedded driver contains an unsafe path: %q", name)
		}
		data, readErr := fs.ReadFile(root, name)
		if readErr != nil {
			return readErr
		}
		files[filepath.ToSlash(name)] = data
		return nil
	})
	if err != nil {
		return nil, "", err
	}
	if err := validateBundledDriverFiles(files); err != nil {
		return nil, "", err
	}
	digest := bundledDriverDigest(files)
	return files, digest, nil
}

func safeDriverRelativePath(name string) bool {
	if strings.TrimSpace(name) == "" || strings.Contains(name, "\\") || strings.Contains(name, ":") || strings.HasPrefix(name, "/") {
		return false
	}
	clean := filepath.ToSlash(filepath.Clean(filepath.FromSlash(name)))
	return clean == name && clean != "." && clean != ".." && !strings.HasPrefix(clean, "../")
}

func validateBundledDriverFiles(files map[string][]byte) error {
	manifestData, ok := files[phoneVRDriverManifestName]
	if !ok || len(manifestData) == 0 {
		return errors.New("embedded PhoneVR driver is missing driver.vrdrivermanifest")
	}
	var manifest struct {
		Name string `json:"name"`
	}
	if err := json.Unmarshal(manifestData, &manifest); err != nil {
		return fmt.Errorf("embedded PhoneVR driver manifest is invalid: %w", err)
	}
	if !strings.EqualFold(manifest.Name, "phonevr") {
		return fmt.Errorf("embedded driver manifest name is %q, expected phonevr", manifest.Name)
	}
	dll, ok := files[phoneVRDriverDLLName]
	if !ok || !isWindowsAMD64PE(dll) {
		return errors.New("embedded PhoneVR driver is missing a valid Windows x64 DLL at bin/win64/driver_phonevr.dll")
	}
	for name := range files {
		if !safeDriverRelativePath(name) {
			return fmt.Errorf("embedded driver contains an unsafe path: %q", name)
		}
	}
	return nil
}

func isWindowsAMD64PE(data []byte) bool {
	if len(data) < 0x40 || data[0] != 'M' || data[1] != 'Z' {
		return false
	}
	peOffset := uint64(binary.LittleEndian.Uint32(data[0x3c:0x40]))
	if peOffset < 0x40 || peOffset+6 > uint64(len(data)) {
		return false
	}
	start := int(peOffset)
	if string(data[start:start+4]) != "PE\x00\x00" {
		return false
	}
	return binary.LittleEndian.Uint16(data[start+4:start+6]) == 0x8664
}

func bundledDriverDigest(files map[string][]byte) string {
	names := make([]string, 0, len(files))
	for name := range files {
		names = append(names, filepath.ToSlash(name))
	}
	sort.Strings(names)
	hash := sha256.New()
	for _, name := range names {
		data := files[name]
		_, _ = fmt.Fprintf(hash, "%d:%s:%d:", len(name), name, len(data))
		_, _ = hash.Write(data)
	}
	return hex.EncodeToString(hash.Sum(nil))
}

func writeBundledDriverFiles(root string, files map[string][]byte) error {
	if err := validateBundledDriverFiles(files); err != nil {
		return err
	}
	if err := os.MkdirAll(root, 0o700); err != nil {
		return err
	}
	for name, data := range files {
		if !safeDriverRelativePath(name) {
			return fmt.Errorf("refusing unsafe PhoneVR driver path %q", name)
		}
		fullPath := filepath.Join(root, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(fullPath), 0o700); err != nil {
			return err
		}
		mode := os.FileMode(0o600)
		if strings.EqualFold(filepath.Ext(fullPath), ".dll") {
			mode = 0o700
		}
		if err := os.WriteFile(fullPath, data, mode); err != nil {
			return err
		}
	}
	return nil
}

func bundledDriverDirectoryMatches(root string, files map[string][]byte) bool {
	for name, expected := range files {
		actual, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(name)))
		if err != nil || !equalBytes(actual, expected) {
			return false
		}
	}
	return true
}

func equalBytes(a, b []byte) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
