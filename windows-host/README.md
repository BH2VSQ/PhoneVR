# PhoneVR Host 0.5.5 bundled SteamVR driver and live SBS video

Executable version is injected from the repository root `VERSION` file during builds.

Native Windows desktop application and Go networking service for the PhoneVRStream Android HMD client.

## Video encoder discovery

The Host runs a bounded FFmpeg encoder-candidate probe at startup. Use `PhoneVRHost.exe -ffmpeg <path>` when FFmpeg is not on PATH. NVIDIA NVENC, AMD AMF and Intel QSV names are candidates only; the video pipeline must still initialize the selected encoder against a concrete DXGI adapter and test a frame before marking it usable.

## Native desktop UI

`PhoneVRHost.exe` creates a native Win32 top-level application window. The UI is drawn with Win32/GDI+ and the HMD pose preview uses WGL/OpenGL. There is no WebView, browser runtime or local HTTP dashboard.

The dashboard shows Host/SteamVR/HMD status, phone battery, the 3D HMD model and Yaw/Pitch/Roll. The SteamVR state pill adapts to English and Chinese labels. The preview keeps a reserved gap above the telemetry cards.

The main menu provides:

- Optical Presets
- Network Settings
- Appearance & Theme
- Runtime Logs
- SteamVR Driver management
- Center View and language switching

### Optical presets

The Android device stores the authoritative optical preset catalogue in local app storage. On connection the Host requests the list; it does not maintain a second persistent catalogue. The Windows Optical Presets window supports apply, edit (double-click or Edit), create, clone, rename and delete. The editor contains the display parameters previously found in the separate phone display settings window. Changes are synchronized back to Android over the TCP control channel. Applying a preset sends its ID; Android resolves and loads the corresponding locally stored entry and updates the stereo renderer.

The Android app's optical preset panel is a selector only. It no longer exposes manual calibration sliders. Existing calibration values are migrated into a local preset when upgrading an installation that has no preset catalogue yet.

### Appearance and child windows

The appearance panel supports light/dark modes with independent OpenGL preview backgrounds, grid/axis colors and headset material palettes. Five color-only swatches apply on left-click; right-click opens the Windows color picker to customize and save that swatch. The theme and custom swatches are saved in the user's application settings. Optical-editor and network-settings viewports follow the active palette. Child windows use a client-drawn header, without a separate Windows caption close button; the in-app Close button remains available.

Network Settings provides the listener address and discovery/control/pose ports, including service start/stop/restart. Runtime Logs provides the Host event log. Dialog content supports vertical scrolling and mouse-wheel navigation, with action buttons in a fixed footer.

## Run

Run `PhoneVRHost.exe` normally. It starts the networking service and opens the native UI.

For command-line diagnostics or a server-only session:

```powershell
.\PhoneVRHost-console.exe -headless
```

Press Ctrl+C to stop the headless process. Closing the native main window stops the Host service, closes TCP/UDP listeners and active connections, and releases all listening ports.

## Requirements

- Windows 10/11 x64
- OpenGL 1.1 support provided by the Windows system `opengl32.dll`
- No Microsoft Edge, Google Chrome, WebView2 runtime, Node.js or third-party Go GUI package required to run the application
- Go 1.22 or newer to build the Host
- CMake 3.24+, Git and Visual Studio C++ x64 tools to build and embed the SteamVR driver

## Build

From the repository root:

```powershell
.\scripts\build-windows-host.ps1
```

Outputs:

- `dist/windows/PhoneVRHost.exe`: native Win32 + OpenGL application, with the SteamVR driver embedded
- `dist/windows/PhoneVRHost-console.exe`: console build for diagnostics and headless operation, also with the driver embedded
- `dist/windows/SHA256SUMS.txt`: executable hashes
- `dist/steamvr-driver/`: intermediate standalone driver package created before embedding

## Test

```powershell
Push-Location .\windows-host
go test ./...
go test -race ./...
go vet ./...
Pop-Location
```

The native UI and model renderer are Windows-specific. CI cross-builds the GUI and console binaries on Windows, while platform-independent tests cover mesh parsing, optical preset persistence and the Host network service.

## Firewall and ports

Allow `PhoneVRHost.exe` through Windows Defender Firewall on Private networks when prompted.

| Port | Transport | Purpose |
|---|---|---|
| 48100 | UDP | LAN discovery |
| 48101 | TCP | Pairing, heartbeat and remote settings/actions |
| 48102 | UDP | HMD head pose |
| 48103 | UDP loopback | Host-to-SteamVR pose bridge |
| 48105 | UDP loopback | SteamVR driver health / display-frame heartbeat |
| 48104 | UDP | Host → Android H.264 video fragments |
| 48106 | TCP loopback | SteamVR driver → Host raw BGRA capture bridge |


## SteamVR local pose bridge and runtime status

Validated Android `PVSP` pose packets received on the existing UDP pose port are forwarded unchanged to `127.0.0.1:48103`. This loopback-only bridge is consumed by the separate SteamVR OpenVR driver inside `vrserver.exe`; it is not exposed to the LAN.

The OpenVR driver sends a versioned `PVST` heartbeat to `127.0.0.1:48105` every 250 ms. Host derives the top-bar SteamVR state from the driver's live heartbeat rather than a hard-coded label or the presence of `vrserver.exe`. The snapshot distinguishes driver loaded, HMD activated, fresh pose, display redirect active and at least one completed virtual-display frame. A heartbeat older than 2.5 seconds is treated as disconnected.

## Video encoder capability discovery

`internal/encoder` parses the installed FFmpeg encoder list and recognizes NVENC, AMF, QSV and software H.264/HEVC/AV1 candidates. Live VR video performs a real hardware H.264 IDR preflight and revalidates the selected encoder when the capture session starts. CPU software encoding is deliberately refused for live VR to avoid competing with SteamVR rendering.

## SteamVR Driver Manager

The PhoneVR OpenVR driver is embedded inside `PhoneVRHost.exe` and `PhoneVRHost-console.exe`. The standard Windows Host build compiles `steamvr-driver/`, stages the resulting package into the Go embed directory, and embeds the complete driver payload into both executables. No separate ZIP or sibling driver folder is needed to run the released Host.

At startup, Host checks SteamVR installation and registration. Unless the user has disabled automatic deployment, it copies the embedded driver into a content-versioned directory under `%LOCALAPPDATA%\PhoneVR\SteamVRDriver`, verifies the copied files, and registers the directory through SteamVR's own `bin\win64\vrpathreg.exe`.

Open **SteamVR Driver** from the Host main menu to refresh status, install/repair the embedded driver, or uninstall and disable future auto-deployment. The opt-out marker is stored under `%LOCALAPPDATA%\PhoneVR`; a manual Install / Repair action removes it. Host deletes only files inside its managed driver directory. If a PhoneVR manifest is registered from a user-managed location, Host unregisters it but leaves those files untouched. Restart SteamVR after driver changes.

## Live stereo video stream

When the Android client reports a hardware MediaCodec H.264 decoder supporting 1600×720 at 60 FPS, and Host has verified a working PC hardware H.264 encoder, the driver streams the final SBS compositor image to Host over loopback TCP `127.0.0.1:48106`. Host encodes with NVENC, AMF or QSV and sends MTU-safe H.264 Annex-B UDP fragments to the phone on port `48104`. CPU software encoding is disabled for this live path to protect SteamVR rendering performance. The virtual display remains fixed at 72 Hz; the retired Host refresh-rate setting is no longer present.



## LAN network interfaces

The Host listens on the configured network address (by default `0.0.0.0`) and reports active IPv4 interfaces at startup. The Android client automatically broadcasts discovery requests over its active IPv4 interfaces. Connect the phone and PC to the same LAN, and ensure Windows Firewall permits PhoneVR Host traffic.
