# PhoneVRStream

Repository revision: **0.4.21**. Windows Host: **0.4.21**. Android client: **0.4.21**. Control protocol: **v1**; video packet protocol: **v1**.

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

## Windows Host 0.4.21 bundled SteamVR driver

The virtual display timing is fixed at 72 Hz in this revision. The adjustable refresh-rate menu and its configuration file have been removed; this avoids presenting refresh-rate choices before the PC compositor and phone rendering path have been profiled end to end.

The Windows Host has been migrated from an Edge/Chrome app-mode dashboard to a native Windows desktop application.

- The main process creates a normal top-level Win32 window with `WS_EX_APPWINDOW`, so it appears as an application in Windows Task Manager rather than leaving a browser process as the visible app.
- The UI is drawn natively with Win32, GDI+ anti-aliased shapes, ClearType Segoe UI typography and custom-drawn status pills/cards. It preserves the former browser dashboard visual language: deep navy background, blue-gray surfaces, subtle borders, green Host/HMD/battery indicators and purple SteamVR state. The HMD pose viewport uses a native WGL/OpenGL context and the embedded Daydream View 2017 OBJ/MTL mesh. There is no WebView, Edge, Chrome, HTML dashboard, local HTTP UI server or browser runtime dependency.
- The model is rendered as a solid material mesh with depth testing and back-face culling. The local XYZ axes and gaze ray rotate with the received HMD quaternion; the floor grid remains world-aligned.
- The main window shows Host state, SteamVR state, HMD state, phone battery, the 3D HMD view and Yaw/Pitch/Roll. English labels are rendered as single-line text with end ellipsis and responsive positioning to prevent wrapping and overlap.
- Validated Android PVSP pose packets are forwarded on loopback UDP `127.0.0.1:48103` for the SteamVR driver. This bridge is local-only and does not change the phone network protocol.
- At Host startup, FFmpeg is probed for a working hardware H.264 encoder. NVENC, AMF and QSV candidates are actually initialized with a short IDR test; CPU software encoding is deliberately disabled for live VR video to avoid competing with an already CPU-bound SteamVR compositor. Supply `-ffmpeg <path>` when FFmpeg is not on PATH.
- Native child windows provide Android-owned optical preset management, network settings, logs and appearance controls. The Host pulls the preset catalogue from Android when connected; the phone remains the authoritative persistent store.
- The Optical Presets list supports double-click editing, save, apply, create, clone, rename and delete. The former phone display settings editor is integrated into the preset editor.
- Light/dark themes use separate pose-preview and headset-material palettes. Five color-only swatches support left-click to apply and right-click to open the Windows color picker; customized swatches are saved. Child windows use an in-app Close action without a separate native caption close button.
- Chinese is the default UI language. English/Chinese switching is available and persists in the user's application data.
- Window sizing is constrained to a 1.2:1 outer-window ratio. The default size is 1080 x 900.
- The HMD is the only tracked device managed by PhoneVRStream. SlimeVR remains responsible for body tracking through SteamVR.

The normal `PhoneVRHost.exe` is built with the Windows GUI subsystem and opens the native desktop application. `PhoneVRHost-console.exe -headless` runs the network service without a desktop window for diagnostics or server-only use.

## Live video streaming diagnostics

The Wi-Fi video path is staged independently from the pose path: SteamVR `IVRVirtualDisplay::Present` → D3D11 SBS capture/readback → loopback TCP raw-frame bridge → Host hardware H.264 → UDP 48104 → Android MediaCodec → SurfaceTexture/OpenGL stereo display. The Host status pill and Runtime Logs distinguish each stage. USB tether/RNDIS streaming is deferred in this revision.

The SteamVR driver writes periodic capture counters to `vrserver.txt`: `captured`, `readback`, `sent`, `map-retries` and `skipped`. Android reports `receiver_started`, `udp_wait`, `udp_first_datagram`, `fragment_wait`, `first_access_unit`, `decoder_ready`, `decoded_frame`, `decoder_error` and `surface_frame` over the TCP control channel. Use these stages to identify where the video path stops before changing codecs or network settings.

## Android client 0.4.21

- Landscape immersive display with OpenGL ES 3.0 stereo rendering.
- Locally stored optical preset selector with Daydream View v2.0 (2017) and Generic Cardboard built-in presets.
- Existing manual display settings are migrated into an Android-local optical preset during the first upgrade; parameter editing is performed in the Windows Host preset editor.
- IMU sensor processing on a dedicated HandlerThread, quaternion-continuous pose transmission over UDP at up to 200 Hz, TCP pairing and heartbeat, battery reporting and bounded auto-reconnect.
- Automatic LAN discovery and connection to the remembered active Host, with selectable Auto / Wi-Fi interface discovery.
- Android-owned optical preset synchronization and preset-ID application, plus recenter operations over the TCP control channel.
- Daydream Controller input provider interface is prepared; actual pairing and input forwarding remain future work.
- `CodecCapabilityProbe` enumerates device MediaCodec decoders and advertises only a hardware H.264 decoder that explicitly supports the negotiated 1600×720@60 profile. `MediaCodec` decodes the H.264 SBS stream to a `SurfaceTexture`; the OpenGL stereo renderer applies the optical warp independently to the left and right halves.


## SteamVR driver 0.4.21

The PhoneVR Host contains its own Windows x64 OpenVR driver inside the Host executable. The normal build compiles `steamvr-driver/`, embeds the resulting manifest, DLL and resources into both Host executables, and no longer asks users to download or select a ZIP. On startup, the Host checks whether SteamVR is installed and whether the user has disabled automatic deployment. When enabled, it copies the embedded driver into a content-versioned directory under `%LOCALAPPDATA%\PhoneVR\SteamVRDriver` and registers it with SteamVR's own `vrpathreg.exe`.

The main-menu **SteamVR Driver** manager checks status, installs or repairs the embedded driver, and uninstalls it. Uninstall writes a persistent opt-out marker so the next Host launch will not silently reinstall it; pressing Install / Repair clears that marker. Files in user-managed driver paths are unregistered but never deleted. Restart SteamVR after driver changes. This is a SteamVR user-mode server driver, not a Windows kernel/device-manager driver.

The driver opens the final SteamVR compositor backbuffer through D3D11 shared textures. When a compatible phone is paired, a GPU shader scales the SBS image to 1600×720 and a three-slot staging queue sends frames over a loopback-only TCP bridge to Host. The capture worker never performs network or CPU texture readback on the compositor callback thread.

- `steamvr-driver/` is a Windows x64 C++ OpenVR driver built with CMake and the Valve OpenVR SDK pinned to commit `0924064316de3effbcd1acf1e309182a2deb1c05`.
- Registers `PhoneVRHmd` as `TrackedDeviceClass_HMD` with an `IVRDisplayComponent`, and `PhoneVRDisplayRedirect` as `TrackedDeviceClass_DisplayRedirect` with an `IVRVirtualDisplay` component.
- Uses latest-only pose forwarding at Android, Host and driver stages; driver angular velocity is derived from Android sensor timestamps and supplied to SteamVR for prediction. Host and driver expose pose-rate/age diagnostics.
- `IsDisplayOnDesktop=false` is reported by the HMD. When SteamVR warns that the HMD is running as a monitor, enable **SteamVR → Settings → Developer → Enable Direct Mode** and restart SteamVR.
- Receives validated Android `PVSP` quaternion packets from the Go Host through loopback UDP port 48103. A dedicated above-normal-priority pose worker publishes each new pose immediately to OpenVR, independently of `RunFrame`, with a 10 ms keepalive. Driver diagnostics compare received / unique / repeated pose rates against `RunFrame` cadence and report maximum publish gap and sample age. `IVRVirtualDisplay` separately reports `Present` / `WaitForPresent` rates and callback durations. The virtual display timing is fixed at 72 Hz in this revision; the retired refresh-rate setting is no longer exposed. The driver reports 3DoF orientation only; it does not invent positional tracking.
- Publishes a stable OpenVR v5 driver-provided Chaperone JSON and matching universe ID at HMD activation. The seated/standing transforms are neutral; collision bounds are empty because PhoneVR does not yet provide 6DoF position tracking or a real room-scale safety perimeter.
- The `IVRVirtualDisplay::Present` callback opens the D3D11 shared backbuffer, uses bounded keyed-mutex synchronization and queues a small GPU-completion fence. Once a compatible Android hardware decoder and a verified PC hardware H.264 encoder are available, the driver GPU-scales the final SBS compositor image to 1600×720 and places it in a three-slot staging ring. A background worker reads back BGRA frames and sends them to Host over loopback TCP 48106; no network I/O or CPU pixel readback occurs in the compositor callback. Host hardware-encodes H.264 and sends `PVRV` fragments to Android UDP 48104. `WaitForPresent` maps the small fence texture; virtual display timing remains fixed at 72 Hz. Completed frames and live SteamVR state are reported to Host over loopback UDP 48105.
- The normal `build-windows-host.ps1` build compiles and embeds the driver. `build-steamvr-driver.ps1` can still be run separately to produce a standalone development package under `dist/steamvr-driver`.

## SteamVR direct display mode

The PhoneVR HMD display component reports `IsDisplayOnDesktop=false`; the
separate display-redirect device remains on `IVRVirtualDisplay` for the final
wireless compositor backbuffer. If SteamVR warns that the HMD is running as a
monitor, enable **SteamVR → Settings → Developer → Enable Direct Mode** and
restart SteamVR. PhoneVR does not force-edit the user's global SteamVR settings.

## Video pipeline foundation

The real-time video path captures the final SteamVR SBS backbuffer at up to 60 FPS, GPU-scales it to 1600×720, reads back through a bounded triple-buffer, passes BGRA frames to a hardware-only H.264 FFmpeg session, fragments Annex-B access units into MTU-safe `PVRV` UDP packets and sends them to Android port 48104. Android reassembles only complete frames, drops stale/incomplete access units, recovers at the next IDR after loss, decodes with `MediaCodec`, and presents to a `SurfaceTexture` consumed by the stereo OpenGL renderer. The existing optical warp is then applied separately to each eye. Pose UDP and control TCP remain independent of the video path.

## Current limitations

- The integrated live path captures the final SteamVR SBS backbuffer, GPU-scales it, hardware-encodes H.264 on the Host and sends MTU-safe UDP fragments to Android.
- Android reassembles complete H.264 access units, decodes them with a hardware MediaCodec decoder and presents the SBS image to SurfaceTexture; the stereo OpenGL renderer applies the existing optical warp independently to each eye.
- Live video is enabled only after both PC hardware encoder initialization and Android hardware decoder profile validation succeed. If either is unavailable, pose/control remain available and video is disabled rather than falling back to CPU software encoding.
- Daydream Controller Bluetooth pairing and SteamVR controller forwarding are not implemented yet.
- The live H.264 stream requires PC-side NVENC/AMF/QSV and a compatible hardware MediaCodec decoder. The current coded profile is 1600×720 SBS at up to 60 FPS; bitrate adaptation and end-to-end Wi-Fi tuning remain future work.
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
| 48104 | UDP | Host -> Android H.264 video fragments |
| 48105 | UDP loopback only | SteamVR driver -> Host runtime heartbeat |
| 48106 | TCP loopback only | SteamVR driver -> Host raw BGRA capture bridge |

The PC and phone should use the same LAN. Guest Wi-Fi or AP/client isolation may block wireless discovery and direct connections.

## Continuous integration and releases

- `.github/workflows/ci.yml` builds Android and the Windows Host package; the Windows Host build also compiles and embeds the SteamVR driver. `.github/workflows/steamvr-driver.yml` remains a focused standalone driver build workflow.
- `.github/workflows/release.yml` is manually triggered from the Actions tab. Select `patch`, `minor` or `major`; it increments VERSION, Android versionName/versionCode, creates a version tag, builds the Android release APK and Windows Host package, then publishes a GitHub Release.
- Android release signing is optional. Add `PHONEVR_RELEASE_KEYSTORE_BASE64`, `PHONEVR_RELEASE_STORE_PASSWORD`, `PHONEVR_RELEASE_KEY_ALIAS` and `PHONEVR_RELEASE_KEY_PASSWORD` as repository Actions secrets to publish a signed APK. Without all four secrets, the workflow publishes a clearly marked unsigned APK.

## SteamVR driver management

The Windows Host executable includes the compiled PhoneVR OpenVR driver. At startup it checks SteamVR registration and, unless the user has explicitly disabled auto-deployment, stages the embedded driver in a content-versioned directory under `%LOCALAPPDATA%\PhoneVR\SteamVRDriver` before registering it with SteamVR's `vrpathreg.exe`.

The in-app manager provides status refresh, **Install / Repair Bundled Driver**, and **Uninstall & Disable Auto Deploy**. Uninstall persists the user's opt-out choice, so a later Host launch does not reinstall the driver unexpectedly. Manual installation clears the opt-out marker. Only files under PhoneVR's own managed directory are deleted; manually registered drivers in other paths are only unregistered. Restart SteamVR after driver changes. No driver ZIP selector or separate Release driver ZIP is required.


