//go:build windows

package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

const phoneVRDriverName = "phonevr"
const phoneVRDriverDisabledFile = "steamvr-driver.disabled"

type steamVRDriverStatus struct {
	Installed        bool
	Path             string
	Runtime          string
	Disabled         bool
	BundledAvailable bool
}

func steamVRInstallRoot() (string, error) {
	keys := []string{
		`HKLM\SOFTWARE\Microsoft\Windows\CurrentVersion\Uninstall\Steam App 250820`,
		`HKLM\SOFTWARE\WOW6432Node\Microsoft\Windows\CurrentVersion\Uninstall\Steam App 250820`,
	}
	for _, key := range keys {
		out, err := exec.Command("reg.exe", "query", key, "/v", "InstallLocation").CombinedOutput()
		if err != nil {
			continue
		}
		lines := strings.Split(string(out), "\n")
		for _, line := range lines {
			fields := strings.Fields(strings.TrimSpace(line))
			for i := 0; i+1 < len(fields); i++ {
				if strings.EqualFold(fields[i], "REG_SZ") {
					candidate := strings.TrimSpace(strings.Join(fields[i+1:], " "))
					if candidate != "" && fileExists(filepath.Join(candidate, "bin", "win64", "vrpathreg.exe")) {
						return filepath.Clean(candidate), nil
					}
				}
			}
		}
	}
	return "", errors.New("未找到 SteamVR 安装目录。请先通过 Steam 安装并启动一次 SteamVR")
}

func vrPathRegistry(runtime string) string {
	return filepath.Join(runtime, "bin", "win64", "vrpathreg.exe")
}

func queryPhoneVRDriver(runtime string) (string, error) {
	reg := vrPathRegistry(runtime)
	out, err := exec.Command(reg, "finddriver", phoneVRDriverName).CombinedOutput()
	path := strings.TrimSpace(string(out))
	if err == nil {
		if path == "" {
			return "", errors.New("SteamVR 返回了空的驱动路径")
		}
		return filepath.Clean(path), nil
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) && exitErr.ExitCode() == 1 {
		return "", nil
	}
	if errors.As(err, &exitErr) && exitErr.ExitCode() == 2 {
		return "", errors.New("检测到多个 PhoneVR 驱动注册项")
	}
	return "", fmt.Errorf("查询 SteamVR 驱动注册状态失败：%s (%w)", strings.TrimSpace(string(out)), err)
}

func runVRPathRegistry(runtime string, args ...string) error {
	cmd := exec.Command(vrPathRegistry(runtime), args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("SteamVR 驱动注册工具执行失败：%s (%w)", strings.TrimSpace(string(out)), err)
	}
	return nil
}

func phoneVRAppDataDirectory() (string, error) {
	base := strings.TrimSpace(os.Getenv("LOCALAPPDATA"))
	if base == "" {
		var err error
		base, err = os.UserConfigDir()
		if err != nil {
			return "", err
		}
	}
	path := filepath.Join(base, "PhoneVR")
	if err := os.MkdirAll(path, 0o700); err != nil {
		return "", err
	}
	return path, nil
}

func phoneVRDataDirectory() (string, error) {
	base, err := phoneVRAppDataDirectory()
	if err != nil {
		return "", err
	}
	path := filepath.Join(base, "SteamVRDriver")
	if err := os.MkdirAll(path, 0o700); err != nil {
		return "", err
	}
	return path, nil
}

func phoneVRDriverDisabledPath() (string, error) {
	base, err := phoneVRAppDataDirectory()
	if err != nil {
		return "", err
	}
	return filepath.Join(base, phoneVRDriverDisabledFile), nil
}

func phoneVRDriverAutoInstallDisabled() (bool, error) {
	path, err := phoneVRDriverDisabledPath()
	if err != nil {
		return false, err
	}
	_, err = os.Stat(path)
	if err == nil {
		return true, nil
	}
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	return false, err
}

func setPhoneVRDriverAutoInstallDisabled(disabled bool) error {
	path, err := phoneVRDriverDisabledPath()
	if err != nil {
		return err
	}
	if disabled {
		return os.WriteFile(path, []byte("PhoneVR SteamVR driver auto-install disabled by user.\n"), 0o600)
	}
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}

func getPhoneVRDriverStatus() (steamVRDriverStatus, error) {
	disabled, disabledErr := phoneVRDriverAutoInstallDisabled()
	if disabledErr != nil {
		return steamVRDriverStatus{}, disabledErr
	}
	_, _, bundleErr := readEmbeddedPhoneVRDriver()
	status := steamVRDriverStatus{Disabled: disabled, BundledAvailable: bundleErr == nil}
	runtime, err := steamVRInstallRoot()
	if err != nil {
		return status, err
	}
	status.Runtime = runtime
	path, err := queryPhoneVRDriver(runtime)
	if err != nil {
		if strings.Contains(err.Error(), "多个 PhoneVR 驱动") {
			return status, err
		}
		return status, err
	}
	status.Installed = path != ""
	status.Path = path
	return status, nil
}

func stageBundledPhoneVRDriver(dataDir string, files map[string][]byte, digest string) (string, error) {
	if err := validateBundledDriverFiles(files); err != nil {
		return "", err
	}
	if len(digest) != 64 {
		return "", errors.New("内置驱动 SHA-256 摘要无效")
	}
	installDir := filepath.Join(dataDir, digest)
	if _, err := os.Stat(installDir); err == nil {
		if bundledDriverDirectoryMatches(installDir, files) {
			return installDir, nil
		}
		// Repair a corrupted or partial staging from a previous build. If SteamVR
		// still has a DLL loaded from this directory, Windows will refuse removal
		// and the user receives a clear error instead of overwriting a live DLL.
		if removeErr := os.RemoveAll(installDir); removeErr != nil {
			return "", fmt.Errorf("内置驱动版本目录校验失败且无法修复，请先完全退出 SteamVR 后重试：%w", removeErr)
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return "", err
	}
	stagingDir, err := os.MkdirTemp(dataDir, ".phonevr-driver-staging-")
	if err != nil {
		return "", err
	}
	defer os.RemoveAll(stagingDir)
	if err := writeBundledDriverFiles(stagingDir, files); err != nil {
		return "", err
	}
	if !bundledDriverDirectoryMatches(stagingDir, files) {
		return "", errors.New("内置驱动复制后的完整性校验失败")
	}
	if err := os.Rename(stagingDir, installDir); err != nil {
		// A second Host instance may have installed the exact same digest.
		if bundledDriverDirectoryMatches(installDir, files) {
			return installDir, nil
		}
		return "", fmt.Errorf("无法提交内置驱动目录：%w", err)
	}
	return installDir, nil
}

func installBundledPhoneVRDriver() (string, error) {
	return installBundledPhoneVRDriverInternal(true)
}

func installBundledPhoneVRDriverInternal(clearDisabledMarker bool) (string, error) {
	runtime, err := steamVRInstallRoot()
	if err != nil {
		return "", err
	}
	files, digest, err := readEmbeddedPhoneVRDriver()
	if err != nil {
		return "", fmt.Errorf("当前 Host 未包含可用的 PhoneVR SteamVR 驱动：%w。请重新运行完整构建脚本", err)
	}
	dataDir, err := phoneVRDataDirectory()
	if err != nil {
		return "", err
	}
	installDir, err := stageBundledPhoneVRDriver(dataDir, files, digest)
	if err != nil {
		return "", err
	}

	oldPath, queryErr := queryPhoneVRDriver(runtime)
	if queryErr != nil && strings.Contains(queryErr.Error(), "多个 PhoneVR 驱动") {
		if err := runVRPathRegistry(runtime, "removedriverswithname", phoneVRDriverName); err != nil {
			return "", err
		}
		oldPath = ""
		queryErr = nil
	}
	if queryErr != nil {
		return "", queryErr
	}
	if oldPath == installDir {
		if !bundledDriverDirectoryMatches(installDir, files) {
			return "", errors.New("已注册的 PhoneVR 驱动文件与内置版本不一致")
		}
		if clearDisabledMarker {
			if err := setPhoneVRDriverAutoInstallDisabled(false); err != nil {
				return "", err
			}
		}
		return "PhoneVR 内置驱动已经是最新版本\n" + installDir, nil
	}
	if oldPath != "" {
		if err := runVRPathRegistry(runtime, "removedriver", oldPath); err != nil {
			return "", err
		}
	}
	if err := runVRPathRegistry(runtime, "adddriver", installDir); err != nil {
		if oldPath != "" {
			_ = runVRPathRegistry(runtime, "adddriver", oldPath)
		}
		return "", err
	}
	if clearDisabledMarker {
		if err := setPhoneVRDriverAutoInstallDisabled(false); err != nil {
			return "", fmt.Errorf("驱动已注册，但无法清除自动安装停用标记：%w", err)
		}
	}
	cleanupManagedDriverVersions(dataDir, installDir)
	return "PhoneVR 内置驱动已部署并注册\n" + installDir, nil
}

func ensureBundledPhoneVRDriverAtStartup() (string, error) {
	disabled, err := phoneVRDriverAutoInstallDisabled()
	if err != nil {
		return "", err
	}
	if disabled {
		return "用户已停用 PhoneVR 驱动自动部署，跳过启动时安装", nil
	}
	_, err = steamVRInstallRoot()
	if err != nil {
		// SteamVR is optional while the Host is being configured; do not make
		// the network service fail just because SteamVR is not installed yet.
		return "", err
	}
	return installBundledPhoneVRDriverInternal(false)
}

func uninstallPhoneVRDriver() (string, error) {
	runtime, err := steamVRInstallRoot()
	if err != nil {
		return "", err
	}
	path, err := queryPhoneVRDriver(runtime)
	if err != nil && strings.Contains(err.Error(), "多个 PhoneVR 驱动") {
		if removeErr := runVRPathRegistry(runtime, "removedriverswithname", phoneVRDriverName); removeErr != nil {
			return "", removeErr
		}
		if err := setPhoneVRDriverAutoInstallDisabled(true); err != nil {
			return "", err
		}
		managed, _ := phoneVRDataDirectory()
		cleanupManagedDriverVersions(managed, "")
		return "已移除重复的 PhoneVR 驱动注册项，并停用自动部署。请重启 SteamVR。", nil
	}
	if err != nil {
		return "", err
	}
	if path != "" {
		manifest := filepath.Join(path, phoneVRDriverManifestName)
		manifestData, err := os.ReadFile(manifest)
		if err != nil {
			return "", fmt.Errorf("无法验证已注册驱动 manifest：%w", err)
		}
		var manifestInfo struct {
			Name string `json:"name"`
		}
		if err := json.Unmarshal(manifestData, &manifestInfo); err != nil || !strings.EqualFold(manifestInfo.Name, phoneVRDriverName) {
			return "", errors.New("已注册路径的 manifest 不属于 PhoneVR，已取消卸载")
		}
		if err := runVRPathRegistry(runtime, "removedriver", path); err != nil {
			return "", err
		}
	}
	if err := setPhoneVRDriverAutoInstallDisabled(true); err != nil {
		return "", err
	}
	managed, err := phoneVRDataDirectory()
	if err != nil {
		return "", err
	}
	managedPath := path != "" && isManagedDriverPath(managed, path)
	cleanupManagedDriverVersions(managed, "")
	if managedPath {
		return "PhoneVR 驱动已从 SteamVR 注销并删除本地文件，自动部署已停用。请重启 SteamVR。", nil
	}
	if path == "" {
		return "PhoneVR 驱动当前未注册，已停用自动部署。", nil
	}
	return "PhoneVR 驱动已从 SteamVR 注销。用户自定义目录中的驱动文件未删除，自动部署已停用。请重启 SteamVR。", nil
}

func isManagedDriverPath(managed, candidate string) bool {
	canonicalManaged, _ := filepath.Abs(managed)
	canonicalPath, _ := filepath.Abs(candidate)
	return canonicalManaged != "" && canonicalPath != "" && strings.HasPrefix(strings.ToLower(canonicalPath), strings.ToLower(canonicalManaged+string(os.PathSeparator)))
}

func cleanupManagedDriverVersions(managed, keep string) {
	entries, err := os.ReadDir(managed)
	if err != nil {
		return
	}
	canonicalKeep := ""
	if keep != "" {
		canonicalKeep, _ = filepath.Abs(keep)
	}
	for _, entry := range entries {
		path := filepath.Join(managed, entry.Name())
		canonical, _ := filepath.Abs(path)
		if canonicalKeep != "" && strings.EqualFold(canonical, canonicalKeep) {
			continue
		}
		_ = os.RemoveAll(path)
	}
}

func fileExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}

// setDriverStatusText converts LF-only strings to CRLF, which is the native
// line-break format expected by a standard Win32 multiline EDIT control.
func setDriverStatusText(hwnd uintptr, text string) {
	text = strings.ReplaceAll(text, "\r\n", "\n")
	text = strings.ReplaceAll(text, "\n", "\r\n")
	setNativeText(hwnd, text)
}

func (app *nativeUI) refreshDriverStatus(d *nativeDialog) {
	if d == nil || d.status == 0 {
		return
	}
	setDriverStatusText(d.status, app.text("正在检查 SteamVR 驱动...", "Checking SteamVR driver..."))
	go func(hwnd, status uintptr) {
		state, err := getPhoneVRDriverStatus()
		ok, _, _ := pIsWindow.Call(hwnd)
		if ok == 0 {
			return
		}
		if err != nil {
			setDriverStatusText(status, "SteamVR: "+err.Error())
			return
		}
		lines := []string{}
		if state.Installed {
			lines = append(lines, "PhoneVR 驱动已注册", state.Path)
		} else {
			lines = append(lines, "PhoneVR 驱动未注册")
		}
		if state.Disabled {
			lines = append(lines, "自动部署：已停用（手动安装/修复可重新启用）")
		} else {
			lines = append(lines, "自动部署：已启用")
		}
		if !state.BundledAvailable {
			lines = append(lines, "内置驱动：当前 Host 未包含，请重新构建完整发布包")
		} else {
			lines = append(lines, "内置驱动：可用")
		}
		lines = append(lines, "SteamVR: "+state.Runtime)
		setDriverStatusText(status, strings.Join(lines, "\n"))
	}(d.hwnd, d.status)
}

func (app *nativeUI) runDriverTask(d *nativeDialog, startText string, task func() (string, error)) {
	if d == nil {
		return
	}
	setDriverStatusText(d.status, startText)
	buttons := []uintptr{d.controls[idDriverRefresh], d.controls[idDriverInstall], d.controls[idDriverRemove]}
	for _, button := range buttons {
		if button != 0 {
			pEnableWindow.Call(button, 0)
		}
	}
	hwnd, status := d.hwnd, d.status
	go func() {
		message, err := task()
		valid, _, _ := pIsWindow.Call(hwnd)
		if valid == 0 {
			return
		}
		if err != nil {
			setDriverStatusText(status, "操作失败："+err.Error())
		} else {
			setDriverStatusText(status, message)
		}
		pInvalidateRect.Call(hwnd, 0, 1)
		for _, button := range buttons {
			if button != 0 {
				pEnableWindow.Call(button, 1)
			}
		}
	}()
}
