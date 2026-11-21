//go:build windows

package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

const bundledPhoneVRDriverDirectoryName = "PhoneVRSteamVRDriver"
const phoneVRDriverAutoInstallDisabledFile = ".auto-install-disabled"

func phoneVRDriverAutoInstallDisabledPath() (string, error) {
	dataDirectory, err := phoneVRDataDirectory()
	if err != nil {
		return "", err
	}
	return filepath.Join(dataDirectory, phoneVRDriverAutoInstallDisabledFile), nil
}

func phoneVRDriverAutoInstallDisabled() (bool, error) {
	path, err := phoneVRDriverAutoInstallDisabledPath()
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
	path, err := phoneVRDriverAutoInstallDisabledPath()
	if err != nil {
		return err
	}
	if disabled {
		return os.WriteFile(path, []byte("disabled\n"), 0o600)
	}
	err = os.Remove(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return err
}

func uninstallAndDisablePhoneVRDriver() (string, error) {
	message, err := uninstallPhoneVRDriver()
	if err != nil {
		return "", err
	}
	if err := setPhoneVRDriverAutoInstallDisabled(true); err != nil {
		return "", fmt.Errorf("驱动已从 SteamVR 卸载，但无法保存自动部署停用状态：%w", err)
	}
	return message + "\n下次启动 Host 时不会自动重新安装。", nil
}

// bundledPhoneVRDriverDirectory locates the driver shipped beside PhoneVRHost.
// The dist/steamvr-driver fallback keeps local source-tree development usable.
func bundledPhoneVRDriverDirectory() (string, error) {
	candidates := make([]string, 0, 4)
	if executable, err := os.Executable(); err == nil {
		candidates = append(candidates, filepath.Join(filepath.Dir(executable), bundledPhoneVRDriverDirectoryName))
	}
	if workingDirectory, err := os.Getwd(); err == nil {
		candidates = append(candidates,
			filepath.Join(workingDirectory, bundledPhoneVRDriverDirectoryName),
			filepath.Join(workingDirectory, "dist", "steamvr-driver"),
		)
	}
	for _, candidate := range candidates {
		if err := validatePhoneVRDriverDirectory(candidate); err == nil {
			absolute, absErr := filepath.Abs(candidate)
			if absErr == nil {
				return absolute, nil
			}
			return filepath.Clean(candidate), nil
		}
	}
	return "", fmt.Errorf("未找到随 PhoneVR Host 发布的内置驱动目录 %q；请解压完整 Host 压缩包，不要只复制 EXE", bundledPhoneVRDriverDirectoryName)
}

func validatePhoneVRDriverDirectory(root string) error {
	manifestPath := filepath.Join(root, "driver.vrdrivermanifest")
	dllPath := filepath.Join(root, "bin", "win64", "driver_phonevr.dll")
	manifestData, err := os.ReadFile(manifestPath)
	if err != nil {
		return fmt.Errorf("读取 PhoneVR 驱动 manifest 失败：%w", err)
	}
	var manifest struct {
		Name string `json:"name"`
	}
	if err := json.Unmarshal(manifestData, &manifest); err != nil {
		return fmt.Errorf("PhoneVR 驱动 manifest JSON 无效：%w", err)
	}
	if !strings.EqualFold(strings.TrimSpace(manifest.Name), phoneVRDriverName) {
		return fmt.Errorf("驱动 manifest 名称不是 %q：%q", phoneVRDriverName, manifest.Name)
	}
	dllInfo, err := os.Stat(dllPath)
	if err != nil {
		return fmt.Errorf("缺少 PhoneVR Windows x64 驱动 DLL：%w", err)
	}
	if !dllInfo.Mode().IsRegular() || dllInfo.Size() == 0 {
		return errors.New("PhoneVR 驱动 DLL 不是有效的普通文件或文件为空")
	}
	return nil
}

// phoneVRDriverPackageDigest returns a deterministic digest of every regular
// file in the package. Relative names are included to detect layout changes.
func phoneVRDriverPackageDigest(root string) (string, error) {
	var files []string
	rootInfo, err := os.Lstat(root)
	if err != nil {
		return "", err
	}
	if !rootInfo.IsDir() || rootInfo.Mode()&os.ModeSymlink != 0 {
		return "", errors.New("PhoneVR 驱动包根路径不是普通目录")
	}
	if err := filepath.WalkDir(root, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.Type()&os.ModeSymlink != 0 {
			return fmt.Errorf("PhoneVR 驱动包不允许符号链接：%s", path)
		}
		if entry.IsDir() {
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if !info.Mode().IsRegular() {
			return fmt.Errorf("PhoneVR 驱动包包含非普通文件：%s", path)
		}
		relative, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		files = append(files, filepath.ToSlash(relative))
		return nil
	}); err != nil {
		return "", err
	}
	sort.Strings(files)
	hasher := sha256.New()
	for _, relative := range files {
		if _, err := io.WriteString(hasher, relative+"\x00"); err != nil {
			return "", err
		}
		file, err := os.Open(filepath.Join(root, filepath.FromSlash(relative)))
		if err != nil {
			return "", err
		}
		_, copyErr := io.Copy(hasher, file)
		closeErr := file.Close()
		if copyErr != nil {
			return "", copyErr
		}
		if closeErr != nil {
			return "", closeErr
		}
		_, _ = hasher.Write([]byte{0})
	}
	return hex.EncodeToString(hasher.Sum(nil)), nil
}

func copyPhoneVRDriverDirectory(source, destination string) error {
	if err := os.MkdirAll(destination, 0o700); err != nil {
		return err
	}
	return filepath.WalkDir(source, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.Type()&os.ModeSymlink != 0 {
			return fmt.Errorf("PhoneVR 驱动包不允许符号链接：%s", path)
		}
		relative, err := filepath.Rel(source, path)
		if err != nil {
			return err
		}
		if relative == "." {
			return nil
		}
		target := filepath.Join(destination, relative)
		if entry.IsDir() {
			return os.MkdirAll(target, 0o700)
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if !info.Mode().IsRegular() {
			return fmt.Errorf("PhoneVR 驱动包包含非普通文件：%s", path)
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o700); err != nil {
			return err
		}
		input, err := os.Open(path)
		if err != nil {
			return err
		}
		mode := os.FileMode(0o600)
		if strings.EqualFold(filepath.Ext(target), ".dll") {
			mode = 0o700
		}
		output, err := os.OpenFile(target, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, mode)
		if err != nil {
			_ = input.Close()
			return err
		}
		_, copyErr := io.Copy(output, input)
		inputCloseErr := input.Close()
		outputCloseErr := output.Close()
		if copyErr != nil {
			return copyErr
		}
		if inputCloseErr != nil {
			return inputCloseErr
		}
		return outputCloseErr
	})
}

func samePhoneVRPath(left, right string) bool {
	if strings.TrimSpace(left) == "" || strings.TrimSpace(right) == "" {
		return false
	}
	leftAbs, leftErr := filepath.Abs(left)
	rightAbs, rightErr := filepath.Abs(right)
	if leftErr == nil && rightErr == nil {
		left, right = leftAbs, rightAbs
	}
	return strings.EqualFold(filepath.Clean(left), filepath.Clean(right))
}

// autoInstallBundledPhoneVRDriver silently skips systems without SteamVR and
// skips the file copy when the currently registered managed package is exact.
func autoInstallBundledPhoneVRDriver() (string, error) {
	runtimeRoot, err := steamVRInstallRoot()
	if err != nil {
		return "", nil
	}
	disabled, err := phoneVRDriverAutoInstallDisabled()
	if err != nil {
		return "", err
	}
	if disabled {
		return "", nil
	}
	source, err := bundledPhoneVRDriverDirectory()
	if err != nil {
		return "", err
	}
	digest, err := phoneVRDriverPackageDigest(source)
	if err != nil {
		return "", err
	}
	dataDirectory, err := phoneVRDataDirectory()
	if err != nil {
		return "", err
	}
	expectedPath := filepath.Join(dataDirectory, digest[:20])
	registeredPath, queryErr := queryPhoneVRDriver(runtimeRoot)
	if queryErr == nil && samePhoneVRPath(registeredPath, expectedPath) && validatePhoneVRDriverDirectory(expectedPath) == nil {
		installedDigest, digestErr := phoneVRDriverPackageDigest(expectedPath)
		if digestErr == nil && installedDigest == digest {
			return "", nil
		}
	}
	return installBundledPhoneVRDriver()
}

func installBundledPhoneVRDriver() (string, error) {
	runtimeRoot, err := steamVRInstallRoot()
	if err != nil {
		return "", err
	}
	source, err := bundledPhoneVRDriverDirectory()
	if err != nil {
		return "", err
	}
	if err := validatePhoneVRDriverDirectory(source); err != nil {
		return "", err
	}
	digest, err := phoneVRDriverPackageDigest(source)
	if err != nil {
		return "", err
	}
	dataDirectory, err := phoneVRDataDirectory()
	if err != nil {
		return "", err
	}
	installDirectory := filepath.Join(dataDirectory, digest[:20])

	packageIsCurrent := false
	if validateErr := validatePhoneVRDriverDirectory(installDirectory); validateErr == nil {
		installedDigest, digestErr := phoneVRDriverPackageDigest(installDirectory)
		packageIsCurrent = digestErr == nil && installedDigest == digest
	}
	if !packageIsCurrent {
		if _, statErr := os.Stat(installDirectory); statErr == nil {
			if removeErr := os.RemoveAll(installDirectory); removeErr != nil {
				return "", fmt.Errorf("无法替换损坏的 PhoneVR 驱动目录（请先关闭 SteamVR）：%w", removeErr)
			}
		}
		stagingDirectory, err := os.MkdirTemp(dataDirectory, ".phonevr-driver-stage-")
		if err != nil {
			return "", err
		}
		defer os.RemoveAll(stagingDirectory)
		if err := copyPhoneVRDriverDirectory(source, stagingDirectory); err != nil {
			return "", fmt.Errorf("复制内置 PhoneVR 驱动失败：%w", err)
		}
		if err := validatePhoneVRDriverDirectory(stagingDirectory); err != nil {
			return "", err
		}
		stagedDigest, err := phoneVRDriverPackageDigest(stagingDirectory)
		if err != nil {
			return "", err
		}
		if stagedDigest != digest {
			return "", errors.New("内置 PhoneVR 驱动复制后校验失败，源文件与部署文件不一致")
		}
		if err := os.Rename(stagingDirectory, installDirectory); err != nil {
			return "", fmt.Errorf("将 PhoneVR 驱动部署到用户目录失败：%w", err)
		}
	}

	oldPath, queryErr := queryPhoneVRDriver(runtimeRoot)
	if queryErr != nil && strings.Contains(queryErr.Error(), "多个 PhoneVR 驱动") {
		if err := runVRPathRegistry(runtimeRoot, "removedriverswithname", phoneVRDriverName); err != nil {
			return "", err
		}
		oldPath = ""
		queryErr = nil
	}
	if queryErr != nil {
		return "", queryErr
	}
	if samePhoneVRPath(oldPath, installDirectory) {
		if err := setPhoneVRDriverAutoInstallDisabled(false); err != nil {
			return "", err
		}
		cleanupManagedDriverVersions(dataDirectory, installDirectory)
		return "PhoneVR 内置驱动已是最新状态。", nil
	}
	if oldPath != "" {
		if err := runVRPathRegistry(runtimeRoot, "removedriver", oldPath); err != nil {
			return "", fmt.Errorf("移除旧 PhoneVR 驱动注册项失败：%w", err)
		}
	}
	if err := runVRPathRegistry(runtimeRoot, "adddriver", installDirectory); err != nil {
		if oldPath != "" {
			_ = runVRPathRegistry(runtimeRoot, "adddriver", oldPath)
		}
		return "", fmt.Errorf("向 SteamVR 注册内置 PhoneVR 驱动失败：%w", err)
	}
	if err := setPhoneVRDriverAutoInstallDisabled(false); err != nil {
		return "", err
	}
	cleanupManagedDriverVersions(dataDirectory, installDirectory)
	return "PhoneVR 内置驱动已自动部署并注册。驱动目录：\n" + installDirectory + "\n如果 SteamVR 已经运行，请重启 SteamVR 以确保加载新版本。", nil
}
