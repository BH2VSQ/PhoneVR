# Repository build scripts

The PowerShell scripts use UTF-8 with BOM for Windows PowerShell 5.1 compatibility. They contain English-only diagnostic strings to avoid console code-page issues. The scripts resolve the repository path from their own location. The examples below assume the current directory is the repository root; they can also be invoked by absolute path from another directory.

## Build everything on Windows

Requirements:

- Windows 10/11
- Android Studio with JDK 17+ (the script tries Android Studio's bundled JBR)
- Android SDK Platform 36 and Build-Tools 36.0.0
- Go 1.22+ on `PATH`
- CMake 3.24+, Git and Visual Studio C++ x64 build tools for the bundled SteamVR driver

PowerShell:

```powershell
Set-ExecutionPolicy -Scope Process Bypass
.\scripts\build-all.ps1
```

Or double-click / run the CMD shim:

```cmd
scripts\build-all.cmd
```

The default full build runs Android unit tests, builds Debug and unsigned Release APKs, runs Go tests and `go vet`, builds the PhoneVR SteamVR OpenVR driver, embeds it into both Windows Host executables, and builds the Windows x64 Host. The scripts continue to the second target if the first target fails, then return a non-zero exit code and print all failed targets.

## Select build targets

```powershell
# Build Android Debug and Windows Host, skip tests
.\scripts\build-all.ps1 -AndroidConfiguration Debug -SkipTests

# Build only Android APKs
.\scripts\build-all.ps1 -SkipWindows

# Build only Windows Host
.\scripts\build-all.ps1 -SkipAndroid

# Build only unsigned Release APK
.\scripts\build-all.ps1 -AndroidConfiguration Release -SkipWindows
```

## Individual builds

```powershell
# Android tests + APKs on Windows
.\scripts\build-android.ps1

# Windows Host + embedded SteamVR driver tests and EXEs
.\scripts\build-windows-host.ps1

# Diagnostic Host build without the embedded SteamVR driver
.\scripts\build-windows-host.ps1 -SkipBundledSteamVRDriver
```

Android-only build on Linux/macOS or in CI:

```bash
bash ./scripts/build-android.sh both
bash ./scripts/build-android.sh debug --skip-tests
```

`build-android.ps1` uses `android/gradlew.bat`. It resolves the SDK from `android/local.properties`, `ANDROID_HOME`, `ANDROID_SDK_ROOT`, or the standard `%LOCALAPPDATA%\Android\Sdk` location. If needed, it writes the resolved local SDK path to the ignored `android/local.properties` file. It does not install SDK packages automatically.

## Output layout

```text
dist/
├── android/
│   ├── PhoneVRStream-Android-debug.apk
│   └── PhoneVRStream-Android-release-unsigned.apk
└── windows/
    ├── PhoneVRHost.exe         # SteamVR driver embedded
    ├── PhoneVRHost-console.exe # SteamVR driver embedded
    └── SHA256SUMS.txt
```

The Release APK is unsigned. The Windows Host is built for Windows x64 with CGO disabled. `PhoneVRHost.exe` uses the Windows GUI subsystem and opens the native Win32 desktop UI with an OpenGL HMD viewport; `PhoneVRHost-console.exe` is retained for logging and headless diagnostics.

## Clean generated files

```powershell
.\scripts\clean.ps1
```

This removes generated build outputs while preserving Android SDK, Gradle user caches, and `android/local.properties`.


Java version detection captures standard output and standard error separately. This is intentional: `java -version` normally prints to standard error, which must not be confused with a PowerShell error when `$ErrorActionPreference` is set to `Stop`. JDK 17 or newer is accepted.


## USB tethering firewall

When Windows marks the Android USB/RNDIS adapter as a Public network, use an elevated PowerShell to allow PhoneVR only from the directly connected local subnet:

```powershell
.\tools\enable-usb-tethering-firewall.ps1
```

Remove the three rules with `-Remove`. The helper opens only inbound discovery UDP 48100, pose UDP 48102 and control TCP 48101. The video stream UDP 48104 is sent outbound to the Android peer; SteamVR bridge ports remain loopback-only.
