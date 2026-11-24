//go:build windows

package main

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"
	"unicode/utf16"

	"github.com/vsq-studio/phonevr-host/internal/childprocess"
	"unsafe"
)

const (
	vbCablePackageURL = "https://download.vb-audio.com/Download_CABLE/VBCABLE_Driver_Pack46.zip"
	vbCablePackage    = "VBCABLE_Driver_Pack46.zip"
)

var vbCableInstallMu sync.Mutex

var (
	vbCableShell32         = syscall.NewLazyDLL("shell32.dll")
	vbCableShellExecuteExW = vbCableShell32.NewProc("ShellExecuteExW")
	vbCableKernel32        = syscall.NewLazyDLL("kernel32.dll")
	vbCableWaitForSingle   = vbCableKernel32.NewProc("WaitForSingleObject")
	vbCableGetExitCode     = vbCableKernel32.NewProc("GetExitCodeProcess")
	vbCableCloseHandle     = vbCableKernel32.NewProc("CloseHandle")
)

type vbCableShellExecuteInfo struct {
	CBSize        uint32
	FMask         uint32
	HWnd          uintptr
	Verb          *uint16
	File          *uint16
	Parameters    *uint16
	Directory     *uint16
	Show          int32
	HInstApp      uintptr
	IDList        uintptr
	Class         *uint16
	HKeyClass     uintptr
	HotKey        uint32
	IconOrMonitor uintptr
	Process       uintptr
}

func vbCableCacheDirectory() (string, error) {
	base := strings.TrimSpace(os.Getenv("LOCALAPPDATA"))
	if base == "" {
		var err error
		base, err = os.UserConfigDir()
		if err != nil {
			return "", err
		}
	}
	return filepath.Join(base, "PhoneVR", "Drivers", "VB-CABLE", "Pack46"), nil
}

// InstallVirtualCable is the fallback installer. An existing VAC or VB-CABLE
// route is reused first; the official VB-CABLE package is installed only when
// no compatible virtual cable pair is installed. Disabled pairs are reported
// to the user and never cause a duplicate driver installation.
func (s *HostService) InstallVirtualCable() (string, error) {
	vbCableInstallMu.Lock()
	defer vbCableInstallMu.Unlock()

	// A manually configured third-party route may not use VAC/VB-CABLE naming.
	// Respect it and avoid installing a second virtual driver simply because
	// automatic vendor-name detection cannot identify the pair.
	routing := loadAudioRoutingConfig().normalized()
	if hasCustomPhoneMicrophoneRoute(routing) {
		s.addLog("Custom phone-microphone audio endpoints are configured; skipping the optional VB-CABLE fallback installer")
		return "检测到已经配置自定义手机麦克风声卡路由。PhoneVR 会继续使用用户选择的播放端和录音端，不会额外安装 VB-CABLE。若需要更换设备，请在音频设备路由页面修改选择。", nil
	}

	if status, ok := currentVirtualCableStatus(); ok && isCompatibleVirtualCableStatus(status) {
		if !status.MicrophoneRouteActive {
			s.addLog("An existing VAC/VB-CABLE pair is installed but one or both endpoints are disabled; skipping installation")
			return "检测到已经安装的虚拟音频线缆，但播放端或录音端当前处于禁用状态。请在 Windows 声音设置中启用这两个配对端点，然后重新打开 PhoneVR 的音频与麦克风页面。PhoneVR 不会重复安装第二套驱动。", nil
		}
		provider := strings.TrimSpace(status.MicrophoneRouteProvider)
		if strings.EqualFold(provider, "VAC") || isVACFriendlyName(status.MicrophoneFriendlyName, status.MicrophoneRenderFriendlyName) {
			s.addLog("Existing Eugene Muzychenko Virtual Audio Cable detected; skipping VB-CABLE installation and reusing the installed VAC route")
			return "检测到已经安装的 Virtual Audio Cable（VAC）。PhoneVR 将直接复用现有 Line 播放/录音端点，不会再次安装 VB-CABLE，也不会修改或卸载现有 VAC。Windows 应用请选择配对的 Line 1 (Virtual Audio Cable) 作为麦克风。", nil
		}
		s.addLog("Existing VB-CABLE route detected; skipping redundant installation")
		return "检测到已经安装的 VB-CABLE，PhoneVR 将复用现有 CABLE Input / CABLE Output，不会重复安装。", nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	root, err := vbCableCacheDirectory()
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(root, 0o700); err != nil {
		return "", fmt.Errorf("create VB-CABLE cache: %w", err)
	}
	archivePath := filepath.Join(root, vbCablePackage)
	installerPath := ""
	if info, statErr := os.Stat(archivePath); statErr == nil && info.Size() > 0 && info.Size() <= vbCableArchiveMaxBytes {
		installerPath, err = extractCachedVBCable(archivePath, filepath.Join(root, "extracted"))
		if err != nil {
			s.addLog("Cached VB-CABLE package is invalid; downloading a clean copy: " + err.Error())
		}
	}
	if installerPath == "" {
		s.addLog("Downloading the official VB-Audio VB-CABLE package (Pack 46) from vb-audio.com")
		if err := downloadVBCablePackage(ctx, vbCablePackageURL, archivePath); err != nil {
			return "", err
		}
		installerPath, err = extractCachedVBCable(archivePath, filepath.Join(root, "extracted"))
		if err != nil {
			return "", err
		}
	}

	s.addLog("Verifying VB-CABLE installer Authenticode signature and VB-Audio publisher")
	signer, err := verifyVBCableInstallerSignature(ctx, installerPath)
	if err != nil {
		return "", err
	}
	s.addLog("Verified VB-CABLE installer signature: " + signer)
	s.addLog("Launching the original VB-Audio installer with Windows UAC; install the driver in its window")
	if err := runVBCableInstallerElevated(ctx, installerPath); err != nil {
		return "", err
	}

	// The VB-Audio installer can request a reboot. The bridge keeps enumerating
	// both cable endpoints while Host is running, so give PnP a short window to
	// publish them before reporting that a restart may be required.
	deadline := time.Now().Add(18 * time.Second)
	for time.Now().Before(deadline) {
		if status, err := readAudioEndpointStatus(); err == nil && isCompatibleVirtualCableStatus(status) {
			s.addLog("VB-CABLE endpoints detected; phone microphone route is ready")
			return "VB-CABLE 安装完成，手机麦克风路由已经就绪。Windows 应用可选择 CABLE Output 作为麦克风。", nil
		}
		time.Sleep(750 * time.Millisecond)
	}
	s.addLog("VB-CABLE installer completed; endpoints are not active yet. Windows may require a restart before CABLE Input / CABLE Output appear")
	return "VB-CABLE 原厂安装程序已完成。Host 尚未检测到 CABLE Input / CABLE Output；请重启 Windows 后重新启动 PhoneVR Host。Windows 输入设备会显示为 CABLE Output (VB-Audio Virtual Cable)。", nil
}

// currentVirtualCableStatus probes Windows MMDevice directly so an already
// installed VAC is still detected when HostService is stopped and its runtime
// endpoint cache has been cleared. The saved status is a fallback for systems
// whose bridge binary was built before one-shot probing was introduced.
func currentVirtualCableStatus() (audioEndpointStatus, bool) {
	if status, err := probeVirtualCableEndpoints(); err == nil && isCompatibleVirtualCableStatus(status) {
		return status, true
	}
	status, err := readAudioEndpointStatus()
	if err == nil && isCompatibleVirtualCableStatus(status) {
		return status, true
	}
	return audioEndpointStatus{}, false
}

func probeVirtualCableEndpoints() (audioEndpointStatus, error) {
	path, err := embeddedAudioBridgeExecutable()
	if err != nil {
		return audioEndpointStatus{}, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, path, "--probe")
	childprocess.HideConsoleWindow(command)
	record, err := command.Output()
	if err != nil {
		return audioEndpointStatus{}, fmt.Errorf("probe installed virtual audio cable endpoints: %w", err)
	}
	return decodeAudioEndpointStatusRecord(record)
}

func readAudioEndpointStatus() (audioEndpointStatus, error) {
	path, err := audioEndpointStatusPath()
	if err != nil {
		return audioEndpointStatus{}, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return audioEndpointStatus{}, err
	}
	var status audioEndpointStatus
	if err := json.Unmarshal(data, &status); err != nil {
		return audioEndpointStatus{}, err
	}
	return status, nil
}

func extractCachedVBCable(archivePath, destination string) (string, error) {
	info, err := os.Stat(archivePath)
	if err != nil {
		return "", err
	}
	file, err := os.Open(archivePath)
	if err != nil {
		return "", err
	}
	defer file.Close()
	return extractVBCablePackage(file, info.Size(), destination)
}

func downloadVBCablePackage(ctx context.Context, source, destination string) error {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, source, nil)
	if err != nil {
		return err
	}
	client := &http.Client{Timeout: 2 * time.Minute}
	response, err := client.Do(request)
	if err != nil {
		return fmt.Errorf("download official VB-CABLE package: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return fmt.Errorf("VB-Audio package server returned HTTP %s", response.Status)
	}
	if response.ContentLength > vbCableArchiveMaxBytes {
		return fmt.Errorf("VB-CABLE package is too large: %d bytes", response.ContentLength)
	}
	temp, err := os.CreateTemp(filepath.Dir(destination), "VBCABLE-*.download")
	if err != nil {
		return err
	}
	tempPath := temp.Name()
	defer os.Remove(tempPath)
	written, copyErr := io.Copy(temp, io.LimitReader(response.Body, vbCableArchiveMaxBytes+1))
	closeErr := temp.Close()
	if copyErr != nil || closeErr != nil {
		return errors.Join(copyErr, closeErr)
	}
	if written <= 0 || written > vbCableArchiveMaxBytes {
		return fmt.Errorf("downloaded VB-CABLE package size %d is invalid", written)
	}
	if err := os.Rename(tempPath, destination); err != nil {
		// Windows rename does not replace a previous file. Remove the stale
		// cached package only after the new download has completed successfully.
		if removeErr := os.Remove(destination); removeErr != nil && !errors.Is(removeErr, os.ErrNotExist) {
			return errors.Join(err, removeErr)
		}
		if retryErr := os.Rename(tempPath, destination); retryErr != nil {
			return retryErr
		}
	}
	return nil
}

func verifyVBCableInstallerSignature(ctx context.Context, installerPath string) (string, error) {
	powershell, err := exec.LookPath("powershell.exe")
	if err != nil {
		return "", errors.New("PowerShell is required to validate the VB-Audio installer signature")
	}
	// Do not trust a filename or the download origin alone. Check the PE
	// Authenticode signature and require a VB-Audio / Vincent Burel signer.
	command := `$ErrorActionPreference='Stop'; $s=Get-AuthenticodeSignature -LiteralPath $args[0]; if($s.Status -ne 'Valid'){Write-Error ("Authenticode status: " + $s.Status); exit 31}; $subject=$s.SignerCertificate.Subject; if($subject -notmatch '(?i)VB-AUDIO|Vincent Burel|V\. Burel'){Write-Error ("Unexpected signer: " + $subject); exit 32}; Write-Output $subject`
	encoded := base64.StdEncoding.EncodeToString(utf16LE(command))
	cmd := exec.CommandContext(ctx, powershell, "-NoProfile", "-NonInteractive", "-ExecutionPolicy", "Bypass", "-EncodedCommand", encoded, installerPath)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("VB-CABLE installer signature verification failed: %w: %s", err, strings.TrimSpace(string(output)))
	}
	return strings.TrimSpace(string(output)), nil
}

func utf16LE(value string) []byte {
	encoded := utf16.Encode([]rune(value))
	data := make([]byte, len(encoded)*2)
	for index, unit := range encoded {
		data[index*2] = byte(unit)
		data[index*2+1] = byte(unit >> 8)
	}
	return data
}

func runVBCableInstallerElevated(ctx context.Context, installerPath string) error {
	verb, _ := syscall.UTF16PtrFromString("runas")
	file, _ := syscall.UTF16PtrFromString(installerPath)
	directory, _ := syscall.UTF16PtrFromString(filepath.Dir(installerPath))
	info := vbCableShellExecuteInfo{
		CBSize:    uint32(unsafe.Sizeof(vbCableShellExecuteInfo{})),
		FMask:     0x00000040, // SEE_MASK_NOCLOSEPROCESS
		Verb:      verb,
		File:      file,
		Directory: directory,
		Show:      1, // SW_SHOWNORMAL
	}
	result, _, callErr := vbCableShellExecuteExW.Call(uintptr(unsafe.Pointer(&info)))
	if result == 0 {
		if errno, ok := callErr.(syscall.Errno); ok && errno == 1223 {
			return errors.New("VB-CABLE 安装已被用户取消 UAC 授权")
		}
		return fmt.Errorf("launch VB-Audio installer with UAC: %v", callErr)
	}
	if info.Process == 0 {
		return errors.New("VB-Audio installer started without a waitable process handle")
	}
	defer vbCableCloseHandle.Call(info.Process)
	waitHandle := make(chan error, 1)
	go func() {
		for {
			wait, _, err := vbCableWaitForSingle.Call(info.Process, 500)
			switch uint32(wait) {
			case 0: // WAIT_OBJECT_0
				var exitCode uint32
				ok, _, getErr := vbCableGetExitCode.Call(info.Process, uintptr(unsafe.Pointer(&exitCode)))
				if ok == 0 {
					waitHandle <- fmt.Errorf("read VB-Audio installer exit code: %v", getErr)
					return
				}
				if exitCode != 0 {
					waitHandle <- fmt.Errorf("VB-Audio installer exited with code %d", exitCode)
					return
				}
				waitHandle <- nil
				return
			case 0x00000102: // WAIT_TIMEOUT
				continue
			default:
				waitHandle <- fmt.Errorf("wait for VB-Audio installer: %v", err)
				return
			}
		}
	}()
	select {
	case err := <-waitHandle:
		return err
	case <-ctx.Done():
		return ctx.Err()
	}
}
