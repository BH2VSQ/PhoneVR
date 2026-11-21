# PhoneVRStream

Repository revision: **0.4.7**. Windows Host: **0.4.7**. Android client: **0.4.7**. Control protocol: **v1**; video packet protocol: **v1 foundation**.

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

## Windows Host 0.4.7 bundled SteamVR driver

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

## Android client 0.4.7

- Landscape immersive display with OpenGL ES 3.0 stereo rendering.
- Locally stored optical preset selector with Daydream View v2.0 (2017) and Generic Cardboard built-in presets.
- Existing manual display settings are migrated into an Android-local optical preset during the first upgrade; parameter editing is performed in the Windows Host preset editor.
- IMU sensor processing on a dedicated HandlerThread, quaternion pose transmission over UDP at up to 120 Hz, TCP pairing and heartbeat, battery reporting and bounded auto-reconnect.
- Automatic LAN discovery and connection to the remembered active Host.
- Android-owned optical preset synchronization and preset-ID application, plus recenter operations over the TCP control channel.
- Daydream Controller input provider interface is prepared; actual pairing and input forwarding remain future work.
- `CodecCapabilityProbe` enumerates device MediaCodec decoders for H.264, HEVC and AV1, including hardware classification, supported size/frame-rate limits, profiles and color formats.


## SteamVR driver 0.4.7

The PhoneVR Host contains its own Windows x64 OpenVR driver inside the Host executable. The normal build compiles `steamvr-driver/`, embeds the resulting manifest, DLL and resources into both Host executables, and no longer asks users to download or select a ZIP. On startup, the Host checks whether SteamVR is installed and whether the user has disabled automatic deployment. When enabled, it copies the embedded driver into a content-versioned directory under `%LOCALAPPDATA%\PhoneVR\SteamVRDriver` and registers it with SteamVR's own `vrpathreg.exe`.

The main-menu **SteamVR Driver** manager checks status, installs or repairs the embedded driver, and uninstalls it. Uninstall writes a persistent opt-out marker so the next Host launch will not silently reinstall it; pressing Install / Repair clears that marker. Files in user-managed driver paths are unregistered but never deleted. Restart SteamVR after driver changes. This is a SteamVR user-mode server driver, not a Windows kernel/device-manager driver.

The 0.4.x driver captures the final SteamVR backbuffer through D3D11 shared textures and synchronizes completed frames. Video encoding and network transmission to Android remain unimplemented.

- `steamvr-driver/` is a Windows x64 C++ OpenVR driver built with CMake and the Valve OpenVR SDK pinned to commit `0924064316de3effbcd1acf1e309182a2deb1c05`.
- Registers `PhoneVRHmd` as `TrackedDeviceClass_HMD` with an `IVRDisplayComponent`, and `PhoneVRDisplayRedirect` as `TrackedDeviceClass_DisplayRedirect` with an `IVRVirtualDisplay` component.
- Receives validated Android `PVSP` quaternion packets from the Go Host through loopback UDP port 48103. The driver reports 3DoF orientation only; it does not invent positional tracking.
- The `IVRVirtualDisplay::Present` callback opens the D3D11 shared backbuffer and queues a full-frame staging copy with bounded keyed-mutex synchronization. A small synchronization texture is queued behind the full copy; `WaitForPresent` maps only that small texture to fence GPU completion without forcing a full-frame CPU readback. `GetTimeSinceLastVsync` supplies a monotonic 72 Hz virtual refresh clock. Completed frames and live SteamVR driver/HMD/pose/display state are reported to Host over loopback UDP 48105. Video encoding and hardware encoder submission are not implemented yet.
- The normal `build-windows-host.ps1` build compiles and embeds the driver. `build-steamvr-driver.ps1` can still be run separately to produce a standalone development package under `dist/steamvr-driver`.

## Video pipeline foundation

The shared video datagram envelope is documented in `docs/video-streaming-protocol-v1.md`. It defines a 32-byte `PVRV` header, bounded 1200-byte payload fragments and SBS frame timestamps. Go encode/decode validation is covered by tests. Actual capture, encode, UDP frame transmission, Android decode and texture presentation remain the next implementation slice.

## Current limitations

- PC VR final-backbuffer capture and GPU synchronization are implemented as P0; encoder-owned pixel buffering, hardware video encoding and Android transmission remain unimplemented.
- Android MediaCodec remote-stream decode and decoded-texture presentation are not implemented yet; decoder capability discovery is now available.
- SteamVR HMD and virtual-display driver capture foundation is present. Host SteamVR state now comes from a live loopback driver heartbeat; video encoding/streaming remains incomplete.
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

The Windows Host version is injected from the root VERSION file at build time. The standard Windows Host build requires Go 1.22+, CMake 3.24+, Git and Visual Studio C++ x64 tools because it compiles the PhoneVR SteamVR driver before embedding it in the Host executables. The Windows Host uses Go standard-library Win32 bindings, system GDI+ for anti-aliased native UI drawing, and system OpenGL 1.1 (`opengl32.dll`). Per-monitor DPI awareness is enabled where supported. No third-party Go GUI dependency or browser runtime is required. The main GUI executable is built with `-H=windowsgui`; the console variant is retained for diagnostics.

## Windows Host ports

| Port | Transport | Purpose |
|---|---|---|
| 48100 | UDP | LAN discovery |
| 48101 | TCP | Pairing, heartbeat, remote settings/actions |
| 48102 | UDP | HMD quaternion pose |
| 48103 | UDP loopback only | Go Host -> SteamVR driver pose bridge |
| 48104 | UDP | Reserved for encoded video stream |
| 48105 | UDP loopback only | SteamVR driver -> Host runtime heartbeat |

The PC and phone must be on the same LAN. Guest Wi-Fi or AP/client isolation may block discovery and direct connections.

## Continuous integration and releases

- `.github/workflows/ci.yml` builds Android and the Windows Host package; the Windows Host build also compiles and embeds the SteamVR driver. `.github/workflows/steamvr-driver.yml` remains a focused standalone driver build workflow.
- `.github/workflows/release.yml` is manually triggered from the Actions tab. Select `patch`, `minor` or `major`; it increments VERSION, Android versionName/versionCode, creates a version tag, builds the Android release APK and Windows Host package, then publishes a GitHub Release.
- Android release signing is optional. Add `PHONEVR_RELEASE_KEYSTORE_BASE64`, `PHONEVR_RELEASE_STORE_PASSWORD`, `PHONEVR_RELEASE_KEY_ALIAS` and `PHONEVR_RELEASE_KEY_PASSWORD` as repository Actions secrets to publish a signed APK. Without all four secrets, the workflow publishes a clearly marked unsigned APK.

## SteamVR driver management

The Windows Host executable includes the compiled PhoneVR OpenVR driver. At startup it checks SteamVR registration and, unless the user has explicitly disabled auto-deployment, stages the embedded driver in a content-versioned directory under `%LOCALAPPDATA%\PhoneVR\SteamVRDriver` before registering it with SteamVR's `vrpathreg.exe`.

The in-app manager provides status refresh, **Install / Repair Bundled Driver**, and **Uninstall & Disable Auto Deploy**. Uninstall persists the user's opt-out choice, so a later Host launch does not reinstall the driver unexpectedly. Manual installation clears the opt-out marker. Only files under PhoneVR's own managed directory are deleted; manually registered drivers in other paths are only unregistered. Restart SteamVR after driver changes. No driver ZIP selector or separate Release driver ZIP is required.
