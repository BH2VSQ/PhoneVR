# PhoneVR Host 0.3.8

Native Windows desktop application and Go networking service for the PhoneVRStream Android HMD client.

## Native desktop UI

`PhoneVRHost.exe` creates a native Win32 top-level application window. Its controls are Win32 child windows; the HMD viewer uses WGL/OpenGL and loads the Daydream View 2017-inspired OBJ/MTL mesh embedded in the executable.

The UI does not use Edge, Chrome, WebView, HTML, JavaScript, Electron, Wails or a local HTTP dashboard. The process running the desktop UI is the PhoneVRHost process itself and is marked as a Windows GUI application.

The main window contains:

- Host service state
- SteamVR connection state
- HMD connection state and phone battery
- Solid 3D HMD model, local XYZ axes and gaze direction
- Yaw, Pitch and Roll
- Native action buttons for center view, phone settings, optical presets, network and logs

Separate native child windows provide:

- Phone display settings: IPD, physical lens spacing, screen-to-lens distance, tray offset, optical center, K1/K2, blur, sharpness and FOV
- Optical presets: built-in Daydream View 2017 and Generic Cardboard profiles; custom presets can be saved, cloned, renamed and deleted
- Network: listener address and discovery/control/pose ports, service start/stop/restart
- Logs: Host event log

Phone settings are normalized, sent over the existing TCP control channel and confirmed from the Android `display_settings` acknowledgement. The language toggle defaults to Simplified Chinese and supports English; the selection is saved in the user's application data.

The main window defaults to 1080 x 900 and keeps a 1.2:1 outer-window ratio while resizing.

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
