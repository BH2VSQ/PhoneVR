# PhoneVRStream

Repository revision: **0.3.8**. Windows Host: **0.3.8**. Android client: **0.3.8**. Network protocol: **v1**.

PhoneVRStream is a two-part project for using an Android phone as a PC VR streaming HMD. Both applications live in one repository and use independent build/test pipelines.

## Repository layout

```text
PhoneVRStream/
├── android/                    # Android Studio / Gradle project
├── windows-host/               # Go Host service and native Windows desktop app
│   ├── cmd/phonevr-host/       # Win32 UI, OpenGL HMD view, Host service
│   ├── internal/protocol/      # HMD pose packet protocol
│   └── go.mod
├── scripts/                    # Local and CI build scripts
├── docs/                       # Shared protocol, Daydream calibration and model docs
├── .github/workflows/          # Unified CI and manual Release workflow
└── dist/                       # Local build output (git-ignored)
```

## Windows Host 0.3.8

The Windows Host has been migrated from an Edge/Chrome app-mode dashboard to a native Windows desktop application.

- The main process creates a normal top-level Win32 window with `WS_EX_APPWINDOW`, so it appears as an application in Windows Task Manager rather than leaving a browser process as the visible app.
- The UI uses Win32 windowing and native controls. The HMD pose viewport uses a native WGL/OpenGL context and the embedded Daydream View 2017 OBJ/MTL mesh. There is no WebView, Edge, Chrome, HTML dashboard, local HTTP UI server or browser runtime dependency.
- The model is rendered as a solid material mesh with depth testing and back-face culling. The local XYZ axes and gaze ray rotate with the received HMD quaternion; the floor grid remains world-aligned.
- The main window shows Host state, SteamVR state, HMD state, phone battery, the 3D HMD view and Yaw/Pitch/Roll.
- Native child windows provide phone display settings, optical preset management, network settings and logs. Phone settings are sent through the existing TCP control channel and the UI waits for the phone's applied settings acknowledgement before showing confirmation.
- Built-in Daydream View 2017 and Generic Cardboard optical presets are available. User presets support save, clone, rename and delete. Built-in presets cannot be renamed or deleted.
- Chinese is the default UI language. English/Chinese switching is available and persists in the user's application data.
- Window sizing is constrained to a 1.2:1 outer-window ratio. The default size is 1080 x 900.
- The HMD is the only tracked device managed by PhoneVRStream. SlimeVR remains responsible for body tracking through SteamVR.

The normal `PhoneVRHost.exe` is built with the Windows GUI subsystem and opens the native desktop application. `PhoneVRHost-console.exe -headless` runs the network service without a desktop window for diagnostics or server-only use.

## Android client

- Landscape immersive display with OpenGL ES 3.0 stereo rendering.
- Virtual IPD, lens-center adjustment, radial K1/K2 distortion correction, blur and sharpening controls.
- Daydream View v2.0 (2017) optical starting profile, with editable physical lens spacing and distortion values.
- IMU sensor processing on a dedicated HandlerThread, quaternion pose transmission over UDP at up to 60 Hz, TCP pairing and heartbeat, battery reporting and bounded auto-reconnect.
- Automatic LAN discovery and connection to the remembered active Host.
- Host-to-phone remote display settings and recenter operations over the TCP control channel.
- Daydream Controller input provider interface is prepared; actual pairing and input forwarding remain future work.

## Current limitations

- PC VR frame acquisition and hardware video encoding are not implemented yet.
- Android MediaCodec video decode and remote-eye texture presentation are not implemented yet.
- SteamVR OpenVR HMD Driver is not implemented yet. The top-bar SteamVR state therefore remains disconnected until that driver is added.
- Daydream Controller Bluetooth pairing and SteamVR controller forwarding are not implemented yet.
- The current Android network path carries HMD pose and control data, not VRChat video.
- Daydream View 2017 support currently means optical profile/calibration compatibility with the physical viewer. It does not implement the discontinued Google Daydream Home/VR Services platform integration.

## Build

From the repository root on Windows:

```powershell
.\scripts\build-all.ps1
```

Build only the Windows Host:

```powershell
.\scripts\build-windows-host.ps1
```

Build only Android:

```powershell
.\scripts\build-android.ps1
```

The Windows Host version is injected from the root VERSION file at build time. The Windows Host uses Go standard-library Win32 bindings and system OpenGL 1.1 (`opengl32.dll`). No third-party Go GUI dependency or browser runtime is required. The main GUI executable is built with `-H=windowsgui`; the console variant is retained for diagnostics.

## Windows Host ports

| Port | Transport | Purpose |
|---|---|---|
| 48100 | UDP | LAN discovery |
| 48101 | TCP | Pairing, heartbeat, remote settings/actions |
| 48102 | UDP | HMD quaternion pose |

The PC and phone must be on the same LAN. Guest Wi-Fi or AP/client isolation may block discovery and direct connections.

## Continuous integration and releases

- `.github/workflows/ci.yml` is the single CI workflow. Android and Windows Host build/test jobs run in parallel inside the same Action.
- `.github/workflows/release.yml` is manually triggered from the Actions tab. Select `patch`, `minor` or `major`; it increments VERSION, Android versionName/versionCode, creates a version tag, builds the Android release APK and Windows Host package, then publishes a GitHub Release.
- Android release signing is optional. Add `PHONEVR_RELEASE_KEYSTORE_BASE64`, `PHONEVR_RELEASE_STORE_PASSWORD`, `PHONEVR_RELEASE_KEY_ALIAS` and `PHONEVR_RELEASE_KEY_PASSWORD` as repository Actions secrets to publish a signed APK. Without all four secrets, the workflow publishes a clearly marked unsigned APK.
