package drivermanager

import (
	"archive/zip"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const (
	GitHubLatestReleaseURL = "https://api.github.com/repos/BH2VSQ/PhoneVR/releases/latest"
	DriverAssetName        = "PhoneVR-SteamVR-Driver-Windows-x64.zip"
	MaxDriverArchiveBytes  = int64(200 << 20)
	MaxDriverExpandedBytes = uint64(300 << 20)
)

type ReleaseAsset struct {
	Name               string `json:"name"`
	BrowserDownloadURL string `json:"browser_download_url"`
	Size               int64  `json:"size"`
}

type GitHubRelease struct {
	TagName string         `json:"tag_name"`
	HTMLURL string         `json:"html_url"`
	Assets  []ReleaseAsset `json:"assets"`
}

// FetchLatestDriverRelease resolves the driver ZIP published with a PhoneVR GitHub Release.
func FetchLatestDriverRelease(ctx context.Context, client *http.Client) (string, ReleaseAsset, error) {
	if client == nil {
		client = &http.Client{Timeout: 45 * time.Second}
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, GitHubLatestReleaseURL, nil)
	if err != nil {
		return "", ReleaseAsset{}, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	req.Header.Set("User-Agent", "PhoneVR-Host-DriverManager")
	resp, err := client.Do(req)
	if err != nil {
		return "", ReleaseAsset{}, fmt.Errorf("查询 PhoneVR Release 失败：%w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return "", ReleaseAsset{}, fmt.Errorf("GitHub Release API 返回 %s：%s", resp.Status, strings.TrimSpace(string(body)))
	}
	var release GitHubRelease
	if err := json.NewDecoder(io.LimitReader(resp.Body, 2<<20)).Decode(&release); err != nil {
		return "", ReleaseAsset{}, fmt.Errorf("解析 Release 信息失败：%w", err)
	}
	for _, asset := range release.Assets {
		if asset.Name == DriverAssetName && strings.HasPrefix(asset.BrowserDownloadURL, "https://github.com/") && asset.Size > 0 && asset.Size <= MaxDriverArchiveBytes {
			return release.TagName, asset, nil
		}
	}
	return "", ReleaseAsset{}, fmt.Errorf("Release %q 未包含 %s", release.TagName, DriverAssetName)
}

// DownloadAsset downloads a release asset to a new file and enforces both declared and actual size limits.
func DownloadAsset(ctx context.Context, client *http.Client, asset ReleaseAsset, destination string) error {
	if asset.Size <= 0 || asset.Size > MaxDriverArchiveBytes || !strings.HasPrefix(asset.BrowserDownloadURL, "https://github.com/") {
		return errors.New("驱动 Release 下载地址或文件大小无效")
	}
	if client == nil {
		client = &http.Client{Timeout: 3 * time.Minute}
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, asset.BrowserDownloadURL, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", "PhoneVR-Host-DriverManager")
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("下载驱动失败：%w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("下载驱动失败：HTTP %s", resp.Status)
	}
	if resp.ContentLength > MaxDriverArchiveBytes {
		return errors.New("驱动压缩包超过大小限制")
	}
	if err := os.MkdirAll(filepath.Dir(destination), 0o700); err != nil {
		return err
	}
	file, err := os.OpenFile(destination, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	written, copyErr := io.Copy(file, io.LimitReader(resp.Body, MaxDriverArchiveBytes+1))
	closeErr := file.Close()
	if copyErr != nil {
		_ = os.Remove(destination)
		return copyErr
	}
	if closeErr != nil {
		_ = os.Remove(destination)
		return closeErr
	}
	if written <= 0 || written > MaxDriverArchiveBytes {
		_ = os.Remove(destination)
		return errors.New("下载的驱动文件大小无效")
	}
	return nil
}

// ValidateAndExtractDriverZip validates the manifest and DLL before writing files.
// The archive must contain driver.vrdrivermanifest at its root and bin/win64/driver_phonevr.dll.
func ValidateAndExtractDriverZip(archivePath, destination string) error {
	archiveInfo, err := os.Stat(archivePath)
	if err != nil {
		return err
	}
	if archiveInfo.Size() <= 0 || archiveInfo.Size() > MaxDriverArchiveBytes {
		return errors.New("驱动 ZIP 压缩文件大小无效")
	}
	zr, err := zip.OpenReader(archivePath)
	if err != nil {
		return fmt.Errorf("无法打开驱动 ZIP：%w", err)
	}
	defer zr.Close()
	if len(zr.File) == 0 || len(zr.File) > 4096 {
		return errors.New("驱动 ZIP 文件数量无效")
	}
	var expanded uint64
	foundManifest, foundDLL := false, false
	for _, f := range zr.File {
		name := filepath.ToSlash(f.Name)
		clean := filepath.Clean(filepath.FromSlash(name))
		if name == "" || strings.HasPrefix(name, "/") || strings.Contains(name, ":") || filepath.IsAbs(clean) || clean == "." || clean == ".." || strings.HasPrefix(clean, ".."+string(os.PathSeparator)) || strings.HasPrefix(clean, "../") {
			return fmt.Errorf("驱动 ZIP 包含非法路径：%q", f.Name)
		}
		if f.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("驱动 ZIP 不允许符号链接：%q", f.Name)
		}
		expanded += f.UncompressedSize64
		if expanded > MaxDriverExpandedBytes {
			return errors.New("驱动 ZIP 解压后超过大小限制")
		}
		if name == "driver.vrdrivermanifest" {
			foundManifest = true
		}
		if name == "bin/win64/driver_phonevr.dll" {
			foundDLL = true
		}
	}
	if !foundManifest || !foundDLL {
		return errors.New("驱动 ZIP 缺少 driver.vrdrivermanifest 或 bin/win64/driver_phonevr.dll")
	}
	if err := os.MkdirAll(destination, 0o700); err != nil {
		return err
	}
	root, err := filepath.Abs(destination)
	if err != nil {
		return err
	}
	for _, f := range zr.File {
		rel := filepath.Clean(filepath.FromSlash(f.Name))
		out := filepath.Join(root, rel)
		if !strings.HasPrefix(out, root+string(os.PathSeparator)) && out != root {
			return fmt.Errorf("驱动 ZIP 路径越界：%q", f.Name)
		}
		if f.FileInfo().IsDir() {
			if err := os.MkdirAll(out, 0o700); err != nil {
				return err
			}
			continue
		}
		if err := os.MkdirAll(filepath.Dir(out), 0o700); err != nil {
			return err
		}
		rc, err := f.Open()
		if err != nil {
			return err
		}
		mode := os.FileMode(0o600)
		if strings.EqualFold(filepath.Ext(out), ".dll") {
			mode = 0o700
		}
		wf, err := os.OpenFile(out, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, mode)
		if err != nil {
			rc.Close()
			return err
		}
		written, copyErr := io.Copy(wf, io.LimitReader(rc, int64(f.UncompressedSize64)+1))
		closeZipErr, closeFileErr := rc.Close(), wf.Close()
		if copyErr != nil {
			return copyErr
		}
		if uint64(written) != f.UncompressedSize64 {
			return fmt.Errorf("驱动 ZIP 文件长度不匹配：%q", f.Name)
		}
		if closeZipErr != nil {
			return closeZipErr
		}
		if closeFileErr != nil {
			return closeFileErr
		}
	}
	manifestPath := filepath.Join(root, "driver.vrdrivermanifest")
	data, err := os.ReadFile(manifestPath)
	if err != nil {
		return err
	}
	var manifest struct {
		Name      string `json:"name"`
		Directory string `json:"directory"`
	}
	if err := json.Unmarshal(data, &manifest); err != nil {
		return fmt.Errorf("驱动 manifest 无效：%w", err)
	}
	if !strings.EqualFold(manifest.Name, "phonevr") {
		return fmt.Errorf("驱动 manifest 名称不匹配：%q", manifest.Name)
	}
	return nil
}
