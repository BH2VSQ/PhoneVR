//go:build windows

package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"time"
	"unsafe"

	"github.com/vsq-studio/phonevr-host/internal/drivermanager"
)

const phoneVRDriverName = "phonevr"

type steamVRDriverStatus struct {
	Installed bool
	Path      string
	Runtime   string
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
		return "", errors.New("检测到多个 PhoneVR 驱动注册项，请先卸载重复的 PhoneVR 驱动")
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

func getPhoneVRDriverStatus() (steamVRDriverStatus, error) {
	runtime, err := steamVRInstallRoot()
	if err != nil {
		return steamVRDriverStatus{}, err
	}
	path, err := queryPhoneVRDriver(runtime)
	if err != nil {
		return steamVRDriverStatus{Runtime: runtime}, err
	}
	return steamVRDriverStatus{Installed: path != "", Path: path, Runtime: runtime}, nil
}

func phoneVRDataDirectory() (string, error) {
	base := os.Getenv("LOCALAPPDATA")
	if strings.TrimSpace(base) == "" {
		var err error
		base, err = os.UserConfigDir()
		if err != nil {
			return "", err
		}
	}
	path := filepath.Join(base, "PhoneVR", "SteamVRDriver")
	if err := os.MkdirAll(path, 0o700); err != nil {
		return "", err
	}
	return path, nil
}

func installPhoneVRDriverArchive(archivePath string) (string, error) {
	runtime, err := steamVRInstallRoot()
	if err != nil {
		return "", err
	}
	dataDir, err := phoneVRDataDirectory()
	if err != nil {
		return "", err
	}
	archiveFile, err := os.Open(archivePath)
	if err != nil {
		return "", err
	}
	hasher := sha256.New()
	if _, err := io.Copy(hasher, archiveFile); err != nil {
		archiveFile.Close()
		return "", err
	}
	if err := archiveFile.Close(); err != nil {
		return "", err
	}
	sum := hasher.Sum(nil)
	folder := hex.EncodeToString(sum)[:16]
	installDir := filepath.Join(dataDir, folder)
	if err := os.RemoveAll(installDir); err != nil {
		return "", err
	}
	if err := drivermanager.ValidateAndExtractDriverZip(archivePath, installDir); err != nil {
		_ = os.RemoveAll(installDir)
		return "", err
	}
	if !fileExists(filepath.Join(installDir, "bin", "win64", "driver_phonevr.dll")) {
		_ = os.RemoveAll(installDir)
		return "", errors.New("驱动包缺少 Windows x64 DLL")
	}
	oldPath, queryErr := queryPhoneVRDriver(runtime)
	if queryErr != nil && strings.Contains(queryErr.Error(), "多个 PhoneVR 驱动") {
		if err := runVRPathRegistry(runtime, "removedriverswithname", phoneVRDriverName); err != nil {
			_ = os.RemoveAll(installDir)
			return "", err
		}
		oldPath = ""
		queryErr = nil
	}
	if queryErr != nil {
		_ = os.RemoveAll(installDir)
		return "", queryErr
	}
	if oldPath != "" {
		if err := runVRPathRegistry(runtime, "removedriver", oldPath); err != nil {
			_ = os.RemoveAll(installDir)
			return "", err
		}
	}
	if err := runVRPathRegistry(runtime, "adddriver", installDir); err != nil {
		if oldPath != "" {
			_ = runVRPathRegistry(runtime, "adddriver", oldPath)
		}
		_ = os.RemoveAll(installDir)
		return "", err
	}
	cleanupManagedDriverVersions(dataDir, installDir)
	return installDir, nil
}

func installLatestPhoneVRDriver() (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancel()
	tag, asset, err := drivermanager.FetchLatestDriverRelease(ctx, nil)
	if err != nil {
		return "", err
	}
	dataDir, err := phoneVRDataDirectory()
	if err != nil {
		return "", err
	}
	downloadDir := filepath.Join(dataDir, "downloads")
	if err := os.MkdirAll(downloadDir, 0o700); err != nil {
		return "", err
	}
	archive := filepath.Join(downloadDir, "PhoneVR-SteamVR-Driver-Windows-x64.zip")
	if err := drivermanager.DownloadAsset(ctx, nil, asset, archive); err != nil {
		return "", err
	}
	path, err := installPhoneVRDriverArchive(archive)
	if err != nil {
		return "", fmt.Errorf("Release %s 驱动安装失败：%w", tag, err)
	}
	return fmt.Sprintf("已安装 Release %s\n%s", tag, path), nil
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
		managed, _ := phoneVRDataDirectory()
		cleanupManagedDriverVersions(managed, "")
		return "已移除所有重复的 PhoneVR 驱动注册项。自定义目录中的旧驱动文件未删除，请重启 SteamVR。", nil
	}
	if err != nil {
		return "", err
	}
	if path == "" {
		return "PhoneVR SteamVR 驱动当前未注册", nil
	}
	manifest := filepath.Join(path, "driver.vrdrivermanifest")
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
	managed, _ := phoneVRDataDirectory()
	managedPath := isManagedDriverPath(managed, path)
	if managedPath {
		_ = os.RemoveAll(path)
	}
	cleanupManagedDriverVersions(managed, "")
	if managedPath {
		return "PhoneVR 驱动已从 SteamVR 注销并删除本地文件。请重启 SteamVR。", nil
	}
	return "PhoneVR 驱动已从 SteamVR 注销。驱动文件位于用户自定义目录，未自动删除。请重启 SteamVR。", nil
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
		if !entry.IsDir() || strings.EqualFold(entry.Name(), "downloads") {
			continue
		}
		path := filepath.Join(managed, entry.Name())
		canonical, _ := filepath.Abs(path)
		if canonicalKeep != "" && strings.EqualFold(canonical, canonicalKeep) {
			continue
		}
		_ = os.RemoveAll(path)
	}
}

func fileExists(path string) bool { info, err := os.Stat(path); return err == nil && !info.IsDir() }

// OPENFILENAMEW is used only for the optional local ZIP selection path.
type nativeOpenFileName struct {
	StructSize       uint32
	Owner            uintptr
	Instance         uintptr
	Filter           *uint16
	CustomFilter     *uint16
	MaxCustFilter    uint32
	FilterIndex      uint32
	File             *uint16
	MaxFile          uint32
	FileTitle        *uint16
	MaxFileTitle     uint32
	InitialDir       *uint16
	Title            *uint16
	Flags            uint32
	FileOffset       uint16
	FileExtension    uint16
	DefaultExtension *uint16
	CustomData       uintptr
	Hook             uintptr
	TemplateName     *uint16
	// These two fields are present in the current OPENFILENAMEW ABI.
	// Omitting them shifts pvReserved/dwReserved/FlagsEx and corrupts memory
	// when the common dialog writes the structure on return.
	EditInfo  uintptr // LPEDITMENU
	Prompt    uintptr // LPCSTR
	Reserved  uintptr // pvReserved
	Reserved2 uint32  // dwReserved
	FlagsEx   uint32
}

func choosePhoneVRDriverZip(owner uintptr) (string, error) {
	dll := syscall.NewLazyDLL("comdlg32.dll")
	proc := dll.NewProc("GetOpenFileNameW")
	if err := proc.Find(); err != nil {
		return "", fmt.Errorf("无法加载 Windows 文件选择器：%w", err)
	}
	buffer := make([]uint16, 32768)
	filterUnits := syscall.StringToUTF16("PhoneVR SteamVR Driver (*.zip)\x00*.zip\x00")
	titleUnits := syscall.StringToUTF16("选择 PhoneVR SteamVR 驱动 ZIP")
	info := nativeOpenFileName{
		StructSize: uint32(unsafe.Sizeof(nativeOpenFileName{})), Owner: owner,
		Filter: &filterUnits[0], FilterIndex: 1, File: &buffer[0], MaxFile: uint32(len(buffer)),
		Title: &titleUnits[0],
		Flags: 0x00001000 | 0x00000800 | 0x00080000 | 0x00000008, // FILEMUSTEXIST | PATHMUSTEXIST | EXPLORER | NOCHANGEDIR
	}
	ok, _, callErr := proc.Call(uintptr(unsafe.Pointer(&info)))
	runtime.KeepAlive(&info)
	runtime.KeepAlive(buffer)
	runtime.KeepAlive(filterUnits)
	runtime.KeepAlive(titleUnits)
	if ok == 0 {
		errorProc := dll.NewProc("CommDlgExtendedError")
		if findErr := errorProc.Find(); findErr == nil {
			code, _, _ := errorProc.Call()
			if code != 0 {
				return "", fmt.Errorf("Windows 文件选择器失败（CommDlgExtendedError=0x%X）：%v", code, callErr)
			}
		}
		return "", nil // User cancelled.
	}
	selected := syscall.UTF16ToString(buffer)
	if selected == "" {
		return "", errors.New("文件选择器返回了空路径")
	}
	return selected, nil
}

func (app *nativeUI) refreshDriverStatus(d *nativeDialog) {
	if d == nil || d.status == 0 {
		return
	}
	setNativeText(d.status, app.text("正在检查 SteamVR 驱动...", "Checking SteamVR driver..."))
	go func(hwnd, status uintptr) {
		state, err := getPhoneVRDriverStatus()
		ok, _, _ := pIsWindow.Call(hwnd)
		if ok == 0 {
			return
		}
		if err != nil {
			setNativeText(status, "SteamVR: "+err.Error())
			return
		}
		if state.Installed {
			setNativeText(status, "PhoneVR 驱动已注册\n"+state.Path+"\nSteamVR: "+state.Runtime)
		} else {
			setNativeText(status, "PhoneVR 驱动未安装\nSteamVR: "+state.Runtime)
		}
	}(d.hwnd, d.status)
}

func (app *nativeUI) runDriverTask(d *nativeDialog, startText string, task func() (string, error)) {
	if d == nil {
		return
	}
	setNativeText(d.status, startText)
	buttons := []uintptr{d.controls[idDriverRefresh], d.controls[idDriverRelease], d.controls[idDriverLocal], d.controls[idDriverRemove]}
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
			setNativeText(status, "操作失败："+err.Error())
		} else {
			setNativeText(status, message)
		}
		pInvalidateRect.Call(hwnd, 0, 1)
		for _, button := range buttons {
			if button != 0 {
				pEnableWindow.Call(button, 1)
			}
		}
	}()
}
