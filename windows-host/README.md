# PhoneVR Host 0.4.1 hotfix

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
- Go 1.22 or newer to build

## Build

From the repository root:

```powershell
.\scripts\build-windows-host.ps1
```

Outputs:

- `dist/windows/PhoneVRHost.exe`: native Win32 + OpenGL application, no console window
- `dist/windows/PhoneVRHost-console.exe`: console build for diagnostics and headless operation
- `dist/windows/SHA256SUMS.txt`: executable hashes

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
| 48104 | UDP | Reserved for video stream |


## SteamVR local pose bridge

Validated Android `PVSP` pose packets received on the existing UDP pose port are forwarded unchanged to `127.0.0.1:48103`. This loopback-only bridge is consumed by the separate SteamVR OpenVR driver inside `vrserver.exe`; it is not exposed to the LAN.

## Video encoder capability discovery

`internal/encoder` parses the installed FFmpeg encoder list and recognizes NVENC, AMF, QSV and software H.264/HEVC/AV1 candidates. Hardware selection is runtime/capability based and supports mixed-vendor systems. A listed FFmpeg encoder is a candidate only; actual device initialization and format validation are performed by the encoder session in the next streaming milestone.
