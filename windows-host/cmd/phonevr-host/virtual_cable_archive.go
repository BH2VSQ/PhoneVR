package main

import (
	"archive/zip"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

const (
	vbCableArchiveMaxBytes       = 24 << 20
	vbCableArchiveMaxExtractSize = 64 << 20
)

// extractVBCablePackage extracts the unmodified official VB-CABLE package.
// Archive entries are validated before writing so a downloaded ZIP cannot
// escape the Host-owned driver cache via absolute or parent-relative paths.
func extractVBCablePackage(reader io.ReaderAt, size int64, destination string) (string, error) {
	if size <= 0 || size > vbCableArchiveMaxBytes {
		return "", fmt.Errorf("VB-CABLE archive size %d is outside the permitted range", size)
	}
	archive, err := zip.NewReader(reader, size)
	if err != nil {
		return "", fmt.Errorf("open VB-CABLE ZIP: %w", err)
	}
	root, err := filepath.Abs(destination)
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(root, 0o700); err != nil {
		return "", err
	}
	var extracted int64
	installer := ""
	for _, entry := range archive.File {
		name := strings.ReplaceAll(entry.Name, "\\", "/")
		if name == "" || strings.HasPrefix(name, "/") || filepath.IsAbs(name) {
			return "", fmt.Errorf("unsafe VB-CABLE archive path %q", entry.Name)
		}
		clean := filepath.Clean(filepath.FromSlash(name))
		if clean == "." || clean == ".." || strings.HasPrefix(clean, ".."+string(os.PathSeparator)) {
			return "", fmt.Errorf("unsafe VB-CABLE archive path %q", entry.Name)
		}
		if entry.Mode()&os.ModeSymlink != 0 {
			return "", fmt.Errorf("VB-CABLE archive contains unsupported symlink %q", entry.Name)
		}
		target := filepath.Join(root, clean)
		rel, err := filepath.Rel(root, target)
		if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(os.PathSeparator)) {
			return "", fmt.Errorf("VB-CABLE archive path escapes destination: %q", entry.Name)
		}
		if entry.FileInfo().IsDir() {
			if err := os.MkdirAll(target, 0o700); err != nil {
				return "", err
			}
			continue
		}
		extracted += int64(entry.UncompressedSize64)
		if extracted > vbCableArchiveMaxExtractSize {
			return "", errors.New("VB-CABLE archive expands beyond the permitted size")
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o700); err != nil {
			return "", err
		}
		input, err := entry.Open()
		if err != nil {
			return "", fmt.Errorf("open VB-CABLE archive entry %q: %w", entry.Name, err)
		}
		mode := os.FileMode(0o600)
		if strings.EqualFold(filepath.Ext(target), ".exe") {
			mode = 0o700
		}
		output, err := os.OpenFile(target, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, mode)
		if err != nil {
			_ = input.Close()
			return "", err
		}
		written, copyErr := io.CopyN(output, input, int64(entry.UncompressedSize64))
		closeInputErr := input.Close()
		closeOutputErr := output.Close()
		if copyErr != nil || closeInputErr != nil || closeOutputErr != nil || written != int64(entry.UncompressedSize64) {
			return "", fmt.Errorf("extract VB-CABLE entry %q failed: copy=%v inputClose=%v outputClose=%v", entry.Name, copyErr, closeInputErr, closeOutputErr)
		}
		base := strings.ToLower(filepath.Base(target))
		if base == "vbcable_setup_x64.exe" {
			installer = target
		}
	}
	if installer == "" {
		for _, entry := range archive.File {
			if strings.EqualFold(filepath.Base(entry.Name), "VBCABLE_Setup.exe") {
				candidate := filepath.Join(root, filepath.Clean(filepath.FromSlash(strings.ReplaceAll(entry.Name, "\\", "/"))))
				installer = candidate
				break
			}
		}
	}
	if installer == "" {
		return "", errors.New("official VB-CABLE package does not contain VBCABLE_Setup_x64.exe or VBCABLE_Setup.exe")
	}
	return installer, nil
}
