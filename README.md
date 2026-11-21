# PhoneVRStream

Repository revision: **0.4.2**. Windows Host: **0.4.2**. Android client: **0.4.2**. Control protocol: **v1**; video packet protocol: **v1 foundation**.

PhoneVRStream is a multi-component project for using an Android phone as a PC VR streaming HMD. Both applications live in one repository and use independent build/test pipelines.

## Repository layout

```text
PhoneVRStream/
├── android/                    # Android Studio / Gradle project
├── windows-host/               # Go Host service, native Windows desktop app and media capability discovery
│   ├── cmd/phonevr-host/       # Win32 UI, OpenGL HMD view, Host service
│   ├── internal/protocol/      # HMD pose packet protocol
│   ├── internal/encoder/       # FFmpeg NVENC / AMF / QSV capability discovery
│   ├── internal/videoprotocol/ # PVRV video fragment header
│   └── go.mod
├── steamvr-driver/             # Windows x64 OpenVR HMD + virtual display redirect foundation
├── scripts/                    # Local and CI build scripts
├── docs/                       # Shared protocol, Daydream calibration and model docs
├── .github/workflows/          # Unified CI and manual Release workflow
└── dist/                       # Local build output (git-ignored)
```

## Windows Host 0.4.2 hotfix

The Windows Host has been migrated from an Edge/Chrome app-mode dashboard to a native Windows desktop application.

- The main process creates a normal top-level Win32 window with `WS_EX_APPWINDOW`, so it appears as an application in Windows Task Manager rather than leaving a browser process as the visible app.
- The UI is drawn natively with Win32, GDI+ anti-aliased shapes, ClearType Segoe UI typography and custom-drawn status pills/cards. It preserves the former browser dashboard visual language: deep navy background, blue-gray surfaces, subtle borders, green Host/HMD/battery indicators and purple SteamVR state. The HMD pose viewport uses a native WGL/OpenGL context and the embedded Daydream View 2017 OBJ/MTL mesh. There is no WebView, Edge, Chrome, HTML dashboard, local HTTP UI server or browser runtime dependency.
- The model is rendered as a solid material mesh with depth testing and back-face culling. The local XYZ axes and gaze ray rotate with the received HMD quaternion; the floor grid remains world-aligned.
- The main window shows Host state, SteamVR state, HMD state, phone battery, the 3D HMD view and Yaw/Pitch/Roll. English labels are rendered as single-line text with end ellipsis and responsive positioning to prevent wrapping and overlap.
- Validated Android PVSP pose packets are forwarded on loopback UDP `127.0.0.1:48103` for the SteamVR driver. This bridge is local-only and does not change the phone network protocol.
- At Host startup, an optional runtime FFmpeg probe detects H.264/HEVC/AV1 encoder candidates exposed by the installed FFmpeg build across NVIDIA NVENC, AMD AMF, Intel QSV and software fallback. Supply `-ffmpeg <path>` when FFmpeg is not on PATH. Candidates are logged but not treated as usable until a later adapter/session initialization test succeeds.
- Native child windows provide Android-owned optical preset management, network settings, logs and appearance controls. The Host pulls the preset catalogue from Android when connected; the phone remains the authoritative persistent store.
- The Optical Presets list supports double-click editing, save, apply, create, clone, rename and delete. The former phone display settings editor is integrated into the preset editor.
- Light/dark themes use separate pose-preview and headset-material palettes. Five color-only swatches support left-click to apply and right-click to open the Windows color picker; customized swatches are saved. Child windows use an in-app Close action without a separate native caption close button.
- Chinese is the default UI language. English/Chinese switching is available and persists in the user's application data.
- Window sizing is constrained to a 1.2:1 outer-window ratio. The default size is 1080 x 900.
- The HMD is the only tracked device managed by PhoneVRStream. SlimeVR remains responsible for body tracking through SteamVR.

The normal `PhoneVRHost.exe` is built with the Windows GUI subsystem and opens the native desktop application. `PhoneVRHost-console.exe -headless` runs the network service without a desktop window for diagnostics or server-only use.

## Android client 0.4.2 hotfix

- Landscape immersive display with OpenGL ES 3.0 stereo rendering.
- Locally stored optical preset selector with Daydream View v2.0 (2017) and Generic Cardboard built-in presets.
- Existing manual display settings are migrated into an Android-local optical preset during the first upgrade; parameter editing is performed in the Windows Host preset editor.
- IMU sensor processing on a dedicated HandlerThread, quaternion pose transmission over UDP at up to 120 Hz, TCP pairing and heartbeat, battery reporting and bounded auto-reconnect.
- Automatic LAN discovery and connection to the remembered active Host.
- Android-owned optical preset synchronization and preset-ID application, plus recenter operations over the TCP control channel.
- Daydream Controller input provider interface is prepared; actual pairing and input forwarding remain future work.
- `CodecCapabilityProbe` enumerates device MediaCodec decoders for H.264, HEVC and AV1, including hardware classification, supported size/frame-rate limits, profiles and color formats.


## SteamVR driver 0.4.2

The Windows Host main menu now contains **SteamVR Driver** management. The user can check registration, download and install the Windows x64 driver ZIP from the latest GitHub Release, choose a local driver ZIP, or uninstall the registered PhoneVR driver. The Host validates the ZIP manifest and DLL, extracts it under `%LOCALAPPDATA%\PhoneVR\SteamVRDriver`, and calls SteamVR `vrpathreg.exe` to register or unregister it. Uninstall removes files only from the Host-managed directory; a manually installed driver in another directory is only unregistered. Restart SteamVR after uninstall.

The driver is a SteamVR user-mode server driver, not a Windows kernel/device-manager driver. The 0.4.x foundation does not yet send encoded video frames; its `IVRVirtualDisplay::Present` callback currently records frame metadata only. The Release workflow now builds `PhoneVR-SteamVR-Driver-Windows-x64.zip` and publishes it as a dedicated GitHub Release asset.

- `steamvr-driver/` is a Windows x64 C++ OpenVR driver built with CMake and the Valve OpenVR SDK pinned to commit `0924064316de3effbcd1acf1e309182a2deb1c05`.
- Registers `PhoneVRHmd` as `TrackedDeviceClass_HMD` with an `IVRDisplayComponent`, and `PhoneVRDisplayRedirect` as `TrackedDeviceClass_DisplayRedirect` with an `IVRVirtualDisplay` component.
- Receives validated Android `PVSP` quaternion packets from the Go Host through loopback UDP port 48103. The driver reports 3DoF orientation only; it does not invent positional tracking.
- The `IVRVirtualDisplay::Present` callback is currently a non-blocking foundation that records frame timing. D3D11 final-backbuffer capture and hardware encoder submission are not implemented yet.
- Build with `.\scripts\build-steamvr-driver.ps1`; register the installed driver with `tools\install-steamvr-driver.ps1 -DriverDirectory <package path>`.

## Video pipeline foundation

The shared video datagram envelope is documented in `docs/video-streaming-protocol-v1.md`. It defines a 32-byte `PVRV` header, bounded 1200-byte payload fragments and SBS frame timestamps. Go encode/decode validation is covered by tests. Actual capture, encode, UDP frame transmission, Android decode and texture presentation remain the next implementation slice.

## Current limitations

- PC VR final-backbuffer capture and hardware video encoding are not implemented yet; the new virtual display driver currently provides the non-blocking present hook only.
- Android MediaCodec remote-stream decode and decoded-texture presentation are not implemented yet; decoder capability discovery is now available.
- SteamVR HMD and virtual-display driver foundation is present, but final-backbuffer capture/streaming is incomplete. SteamVR connection status is not yet driven by a full runtime health handshake.
- Daydream Controller Bluetooth pairing and SteamVR controller forwarding are not implemented yet.
- The current Android network path carries HMD pose and control data, not VRChat video. Video packet v1 is specified and validated but not yet transmitted.
- Daydream View 2017 support currently means optical profile/calibration compatibility with the physical viewer. It does not implement the discontinued Google Daydream Home/VR Services platform integration.

## Build

From the repository root on Windows:

```powershell
.\scripts\build-all.ps1
```

Build the SteamVR driver (Windows x64, Visual Studio required):

```powershell
.\scripts\build-steamvr-driver.ps1
```

Build only the Windows Host:

```powershell
.\scripts\build-windows-host.ps1
```

Build only Android:

```powershell
.\scripts\build-android.ps1
```

The Windows Host version is injected from the root VERSION file at build time. The Windows Host uses Go standard-library Win32 bindings, system GDI+ for anti-aliased native UI drawing, and system OpenGL 1.1 (`opengl32.dll`). Per-monitor DPI awareness is enabled where supported. No third-party Go GUI dependency or browser runtime is required. The main GUI executable is built with `-H=windowsgui`; the console variant is retained for diagnostics.

## Windows Host ports

| Port | Transport | Purpose |
|---|---|---|
| 48100 | UDP | LAN discovery |
| 48101 | TCP | Pairing, heartbeat, remote settings/actions |
| 48102 | UDP | HMD quaternion pose |
| 48103 | UDP loopback only | Go Host -> SteamVR driver pose bridge |
| 48104 | UDP | Reserved for encoded video stream |

The PC and phone must be on the same LAN. Guest Wi-Fi or AP/client isolation may block discovery and direct connections.

## Continuous integration and releases

- `.github/workflows/ci.yml` builds Android, Windows Host and SteamVR driver jobs independently. `.github/workflows/steamvr-driver.yml` also provides a focused driver build workflow.
- `.github/workflows/release.yml` is manually triggered from the Actions tab. Select `patch`, `minor` or `major`; it increments VERSION, Android versionName/versionCode, creates a version tag, builds the Android release APK and Windows Host package, then publishes a GitHub Release.
- Android release signing is optional. Add `PHONEVR_RELEASE_KEYSTORE_BASE64`, `PHONEVR_RELEASE_STORE_PASSWORD`, `PHONEVR_RELEASE_KEY_ALIAS` and `PHONEVR_RELEASE_KEY_PASSWORD` as repository Actions secrets to publish a signed APK. Without all four secrets, the workflow publishes a clearly marked unsigned APK.

## SteamVR driver management

The Windows Host includes a **SteamVR Driver** entry in its main menu. It can:

- Query whether the `phonevr` driver is registered with the installed SteamVR runtime.
- Download the `PhoneVR-SteamVR-Driver-Windows-x64.zip` asset from the latest GitHub Release, validate the archive and manifest, extract it under `%LOCALAPPDATA%\\PhoneVR\\SteamVRDriver`, and register it with SteamVR.
- Install the same package from a locally selected ZIP file.
- Unregister the PhoneVR driver from SteamVR and remove files only when they are inside the Host-managed driver directory.

The release workflow builds the SteamVR driver on Windows x64 and publishes the ZIP as a separate Release asset. SteamVR's own `vrpathreg.exe` is located from the SteamVR installation registry entry; it is never copied or bundled by PhoneVR. After uninstall, restart SteamVR to ensure the runtime unloads the removed driver. If the driver was manually installed outside PhoneVR's managed directory, Host unregisters it but deliberately leaves those user-managed files in place.
