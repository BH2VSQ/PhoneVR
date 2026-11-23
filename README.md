# PhoneVRStream

Repository revision: **0.5.27**. Windows Host: **0.5.27**. Android client: **0.5.27**. Control protocol: **v1**; video packet protocol: **v1**.

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

## Windows Host 0.5.27 bundled SteamVR driver

The virtual display timing is fixed at 72 Hz in this revision. Stream resolution is selected independently using the **Stream Resolution** dialog. The chosen SBS profile changes the driver's recommended target size and capture output, Host raw-frame buffers and H.264 encoder dimensions, plus the Android decoder capability negotiation. SteamVR's own resolution scale controls supersampling before capture and does not replace the PhoneVR stream profile.

The Windows Host has been migrated from an Edge/Chrome app-mode dashboard to a native Windows desktop application.

- The main process creates a normal top-level Win32 window with `WS_EX_APPWINDOW`, so it appears as an application in Windows Task Manager rather than leaving a browser process as the visible app.
- The UI is drawn natively with Win32, GDI+ anti-aliased shapes, ClearType Segoe UI typography and custom-drawn status pills/cards. It preserves the former browser dashboard visual language: deep navy background, blue-gray surfaces, subtle borders, green Host/HMD/battery indicators and purple SteamVR state. The HMD pose viewport uses a native WGL/OpenGL context and the embedded Daydream View 2017 OBJ/MTL mesh. There is no WebView, Edge, Chrome, HTML dashboard, local HTTP UI server or browser runtime dependency.
- The model is rendered as a solid material mesh with depth testing and back-face culling. The local XYZ axes and gaze ray rotate with the received HMD quaternion; the floor grid remains world-aligned.
- The main window shows Host state, SteamVR state, HMD state, phone battery, the 3D HMD view and Yaw/Pitch/Roll. English labels are rendered as single-line text with end ellipsis and responsive positioning to prevent wrapping and overlap.
- Validated Android PVSP pose packets are forwarded on loopback UDP `127.0.0.1:48103` for the SteamVR driver. This bridge is local-only and does not change the phone network protocol.
- At Host startup, FFmpeg is probed for a working hardware H.264 encoder. NVENC, AMF and QSV candidates are actually initialized with a short IDR test; CPU software encoding is deliberately disabled for live VR video to avoid competing with an already CPU-bound SteamVR compositor. Supply `-ffmpeg <path>` when FFmpeg is not on PATH.
- All helper processes launched by the GUI Host run without separate console windows, including Windows registry queries, SteamVR `vrpathreg.exe`, FFmpeg inventory/preflight and the persistent FFmpeg encoder. Their captured output and errors remain available in the Host log.
- Native child windows provide Android-owned optical preset management, network settings, logs and appearance controls. The Host pulls the preset catalogue from Android when connected; the phone remains the authoritative persistent store.
- The **Video Transport** dialog selects Automatic failover, USB AOA only, Network only or Network + USB AOA aggregation. The first-run default is **Network only**; the selected mode and automatic-mode preference are saved and restored after Host restart.
- The **Stream Resolution** dialog selects Performance 1280×576, Balanced 1600×720, High 1920×864 or Maximum 2400×1080 SBS. The Host applies profile-specific H.264 target bitrates of 5, 8, 13 and 18 Mbps respectively, checks the phone's explicit hardware MediaCodec size/rate capabilities, and disables video if the selected profile cannot be decoded in hardware. The UI selection is stored as a pending profile. Each Host service start reloads it, re-runs hardware encoder preflight if its dimensions changed, and publishes `video-profile-active.json`; the SteamVR driver reads only this active file. After saving a profile, exit SteamVR, stop and restart the Host service, then launch SteamVR. SteamVR's per-application resolution scale should remain at 100% unless supersampling is deliberately desired.
- The Android client keeps LAN control/pose and AOA auxiliary sessions available together under one persistent device ID. Automatic mode fails over between the paths. Aggregation mode stripes distinct PVRV fragments across independently probe-verified UDP and AOA routes, while Android feeds both into one offset-based reassembler and one MediaCodec decoder.
- Per-path receive goodput drives a weighted scheduler. Missing fragment indexes are reported over the control channel; Host temporarily retains encoded access units and retransmits requested fragments over the alternate route. A bounded out-of-order window protects frame order when USB and network latency differ. If one path disconnects or its queue is congested, fragments fall back to the surviving path and the recovered route rejoins after a fresh receive-path probe.
- The Optical Presets list supports double-click editing, save, apply, create, clone, rename and delete. The former phone display settings editor is integrated into the preset editor.
- Light/dark themes use separate pose-preview and headset-material palettes. Five color-only swatches support left-click to apply and right-click to open the Windows color picker; customized swatches are saved. Child windows use an in-app Close action without a separate native caption close button.
- Chinese is the default UI language. English/Chinese switching is available and persists in the user's application data.
- Window sizing is constrained to a 1.2:1 outer-window ratio. The default size is 1080 x 900.
- The HMD is the only tracked device managed by PhoneVRStream. SlimeVR remains responsible for body tracking through SteamVR.

The normal `PhoneVRHost.exe` is built with the Windows GUI subsystem and opens the native desktop application. `PhoneVRHost-console.exe -headless` runs the network service without a desktop window for diagnostics or server-only use.

All helper processes launched by the GUI Host use `CREATE_NO_WINDOW`, including Steam registry discovery, SteamVR driver registration and FFmpeg inventory/preflight/streaming. Their captured output remains available in the Host log. See `docs/2026-11-22-silent-host-child-processes.md`.

## Android client 0.5.27

- Landscape immersive display with OpenGL ES 3.0 stereo rendering.
- Locally stored optical preset selector with Daydream View v1.0 (2016), Daydream View v2.0 (2017) and Generic Cardboard built-in presets.
- Existing manual display settings are migrated into an Android-local optical preset during the first upgrade. The Android **双目诊断** panel provides source-eye swap, local L/R identifiers, single-eye tests, identical-image alignment and crossed-disparity checks, plus independent per-eye X/Y image offsets.
- Per-eye routing and offsets are part of the shared display-settings JSON model and are retained by the Windows Host preset manager. Diagnostic patterns bypass lens warp by default, with an independent toggle for testing the current optical post-process.
- IMU sensor processing on a dedicated HandlerThread, quaternion-continuous pose transmission over UDP at up to 200 Hz on LAN or over the native AOA Bulk channel on USB, TCP-style pairing/control framing and heartbeat, battery reporting and bounded auto-reconnect.
- Automatic LAN discovery across active IPv4 interfaces and connection to the remembered Host. A separate **USB AOA 直连** action uses Android Open Accessory mode and a full-duplex Bulk stream, with no USB tethering, RNDIS or ADB data channel.
- On normal startup, the app discovers the Host over LAN while independently monitoring for an AOA accessory session. The AOA auxiliary session feeds USB video into the same receiver as LAN UDP video and sends pose only when the network control session is unavailable. The explicit **USB AOA 直连** action remains available for USB-only operation. Duplicate AOA startup notifications are coalesced into one connection worker, and Android re-resolves the accessory after permission is granted before opening its descriptor.
- The connection page and quick menu show whether network control, USB AOA, both paths or neither path are currently connected.
- Android-owned optical preset synchronization and preset-ID application, plus recenter operations over the TCP control channel.
- Daydream Controller input provider interface is prepared; actual pairing and input forwarding remain future work.
- `CodecCapabilityProbe` enumerates device MediaCodec decoders and advertises hardware H.264 support for the 1280×576, 1600×720, 1920×864 and 2400×1080 SBS profiles at 60 FPS only when the codec reports each exact size/rate combination. `MediaCodec` decodes the negotiated H.264 SBS stream to a `SurfaceTexture`; the OpenGL stereo renderer applies the optical warp independently to the left and right halves.
- USB AOA video packets are handed from the accessory reader to a dedicated bounded asynchronous dispatcher before local decoder forwarding. This keeps loopback forwarding work off the USB Bulk IN reader and exposes queue/drop/forwarding timing diagnostics.
- The AOA reader uses 16 KiB raw reads before parsing PVRU headers. The stream decoder reassembles frames split across reads or coalesced by the USB stack; it does not issue 16-byte reads directly to the accessory file descriptor. Android raw-read sizes and PVRU CRC32 values are logged for diagnosis.


## Audio streaming status

Audio transport is not implemented in 0.5.26. The current build has no WASAPI capture/render workers, Android `AudioRecord`/`AudioTrack` path, audio packet protocol or virtual headset audio endpoints. The SteamVR input/output selectors therefore cannot provide end-to-end phone audio by themselves. The proposed two-way architecture and Windows endpoint requirements are documented in `docs/audio-streaming-architecture.md`.

## SteamVR driver 0.5.27

The PhoneVR Host contains its own Windows x64 OpenVR driver inside the Host executable. The normal build compiles `steamvr-driver/`, embeds the resulting manifest, DLL and resources into both Host executables, and no longer asks users to download or select a ZIP. On startup, the Host checks whether SteamVR is installed and whether the user has disabled automatic deployment. When enabled, it copies the embedded driver into a content-versioned directory under `%LOCALAPPDATA%\PhoneVR\SteamVRDriver` and registers it with SteamVR's own `vrpathreg.exe`.

The main-menu **SteamVR Driver** manager checks status, installs or repairs the embedded driver, and uninstalls it. Uninstall writes a persistent opt-out marker so the next Host launch will not silently reinstall it; pressing Install / Repair clears that marker. Files in user-managed driver paths are unregistered but never deleted. Restart SteamVR after driver changes. This is a SteamVR user-mode server driver, not a Windows kernel/device-manager driver.

The driver opens the final SteamVR compositor backbuffer through D3D11 shared textures. When a compatible phone is paired, a GPU shader scales the SBS image to the selected stream profile and a three-slot staging queue sends frames over a loopback-only TCP bridge to Host. The capture worker never performs network or CPU texture readback on the compositor callback thread.

- `steamvr-driver/` is a Windows x64 C++ OpenVR driver built with CMake and the Valve OpenVR SDK pinned to commit `0924064316de3effbcd1acf1e309182a2deb1c05`.
- Registers `PhoneVRHmd` as `TrackedDeviceClass_HMD` with an `IVRDisplayComponent`, and `PhoneVRDisplayRedirect` as `TrackedDeviceClass_DisplayRedirect` with an `IVRVirtualDisplay` component.
- Uses latest-only pose forwarding at Android, Host and driver stages; driver angular velocity is derived from Android sensor timestamps and supplied to SteamVR for prediction. Host and driver expose pose-rate/age diagnostics. The current 3DoF HMD pose uses a 1.65 m virtual standing eye height above the SteamVR floor because the phone supplies orientation without positional tracking.
- `IsDisplayOnDesktop=false` is reported by the HMD. When SteamVR warns that the HMD is running as a monitor, enable **SteamVR → Settings → Developer → Enable Direct Mode** and restart SteamVR.
- Receives validated Android `PVSP` quaternion packets from the Go Host through loopback UDP port 48103. A dedicated above-normal-priority pose worker publishes each new pose immediately to OpenVR, independently of `RunFrame`, with a 10 ms keepalive. Driver diagnostics compare received / unique / repeated pose rates against `RunFrame` cadence and report maximum publish gap and sample age. `IVRVirtualDisplay` separately reports `Present` / `WaitForPresent` rates and callback durations. The virtual display timing is fixed at 72 Hz in this revision; the retired refresh-rate setting is no longer exposed. The driver reports 3DoF orientation only; it does not invent positional tracking.
- Publishes a stable OpenVR v5 driver-provided Chaperone JSON and matching universe ID at HMD activation. The seated/standing transforms are neutral; collision bounds are empty because PhoneVR does not yet provide 6DoF position tracking or a real room-scale safety perimeter.
- The `IVRVirtualDisplay::Present` callback opens the D3D11 shared backbuffer, uses bounded keyed-mutex synchronization and queues a small GPU-completion fence. Once a compatible Android hardware decoder and a verified PC hardware H.264 encoder are available, the driver GPU-scales the final SBS compositor image to the selected 1280×576, 1600×720, 1920×864 or 2400×1080 profile and places it in a three-slot staging ring. A background worker reads back BGRA frames and sends them to Host over loopback TCP 48106; no network I/O or CPU pixel readback occurs in the compositor callback. Host hardware-encodes H.264 and sends `PVRV` fragments to Android UDP 48104. `WaitForPresent` maps the small fence texture; virtual display timing remains fixed at 72 Hz. Completed frames and live SteamVR state are reported to Host over loopback UDP 48105.
- The normal `build-windows-host.ps1` build compiles and embeds the driver. `build-steamvr-driver.ps1` can still be run separately to produce a standalone development package under `dist/steamvr-driver`.

## SteamVR direct display mode

The PhoneVR HMD display component reports `IsDisplayOnDesktop=false`; the
separate display-redirect device remains on `IVRVirtualDisplay` for the final
wireless compositor backbuffer. If SteamVR warns that the HMD is running as a
monitor, enable **SteamVR → Settings → Developer → Enable Direct Mode** and
restart SteamVR. PhoneVR does not force-edit the user's global SteamVR settings.

## Video pipeline foundation

The real-time video path captures the final SteamVR SBS backbuffer at up to 60 FPS, GPU-scales it to the selected SBS profile, reads back through a bounded triple-buffer and passes BGRA frames to a hardware-only H.264 FFmpeg session. For LAN connections, Annex-B access units are fragmented into MTU-safe `PVRV` UDP packets and sent to Android port 48104. For USB connections, the same `PVRV` fragments are multiplexed over AOA Bulk channel 3. Android reassembles only complete frames, drops stale/incomplete access units, recovers at the next IDR after loss, decodes with `MediaCodec`, and presents to a `SurfaceTexture` consumed by the stereo OpenGL renderer. Control JSON and pose samples use separate channels on the same USB Bulk stream when USB is selected.

## Daydream View dual-controller input

The Android controller panel scans for Daydream Controller BLE devices and assigns one remote to the left hand and one to the right hand. Each assignment is saved separately and restored on later launches. The 19-byte BLE report is decoded locally, then controller snapshots travel on the TCP control channel. Windows Host forwards separate validated `PVSC` packets to the SteamVR driver over loopback UDP `48108`. SteamVR registers two controller devices with touchpad axes/click, trigger compatibility alias, App/Menu, Home/System and volume buttons. HMD pose continues on the independent `PVSP` channel. See `docs/daydream-controller-integration.md` for protocol, mapping and limitations.

## Current limitations

- The integrated live path captures the final SteamVR SBS backbuffer, GPU-scales it, hardware-encodes H.264 on the Host and sends MTU-safe UDP fragments to Android.
- Android reassembles complete H.264 access units, decodes them with a hardware MediaCodec decoder and presents the SBS image to SurfaceTexture; the stereo OpenGL renderer applies the existing optical warp independently to each eye.
- Live video is enabled only after both PC hardware encoder initialization and Android hardware decoder profile validation succeed. If either is unavailable, pose/control remain available and video is disabled rather than falling back to CPU software encoding.
- PhoneVR 0.5.26 adds Android BLE pairing for two Daydream View remotes, independent left/right SteamVR controller devices, touchpad/button forwarding and 3DoF controller orientation. Simultaneous dual-GATT behavior and physical SteamVR/game bindings still require target-device validation. Controller position is a fixed head-relative estimate, not 6DoF tracking.
- The live H.264 stream requires PC-side NVENC/AMF/QSV and a compatible hardware MediaCodec decoder. Available SBS profiles are 1280×576, 1600×720, 1920×864 and 2400×1080 at up to 60 FPS, with profile-specific target bitrates of 5/8/13/18 Mbps. End-to-end Wi-Fi tuning remains a device-level validation task.
- Daydream View 2016 and 2017 support currently means optical profile/calibration compatibility with the physical viewer. It does not implement the discontinued Google Daydream Home/VR Services platform integration.

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

The Android Gradle launcher is generated automatically from `android/tools/wrapper-bootstrap/` when missing. A JDK with `javac` and `jar` is required for the first invocation; the pinned Gradle distribution is downloaded and SHA-256 verified automatically.

The Windows Host version is injected from the root VERSION file at build time. The standard Windows Host build requires Go 1.22+, CMake 3.24+, Git and Visual Studio C++ x64 tools because it compiles the PhoneVR SteamVR driver before embedding it in the Host executables. The Windows Host uses Go standard-library Win32 bindings, system GDI+ for anti-aliased native UI drawing, and system OpenGL 1.1 (`opengl32.dll`). Per-monitor DPI awareness is enabled where supported. No third-party Go GUI dependency or browser runtime is required. The main GUI executable is built with `-H=windowsgui`; the console variant is retained for diagnostics.

## Windows Host ports

| Port | Transport | Purpose |
|---|---|---|
| 48100 | UDP | LAN discovery |
| 48101 | TCP | Pairing, heartbeat, remote settings/actions |
| 48102 | UDP | HMD quaternion pose |
| 48103 | UDP loopback only | Go Host -> SteamVR driver pose bridge |
| 48104 | UDP | Host -> Android H.264 video fragments on LAN; unused by USB AOA sessions |
| 48105 | UDP loopback only | SteamVR driver -> Host runtime heartbeat |
| 48106 | TCP loopback only | SteamVR driver -> Host raw BGRA capture bridge |

The PC and phone should use the same LAN for Wi-Fi streaming. Guest Wi-Fi or AP/client isolation may block discovery and direct connections. USB AOA is a separate direct cable transport and does not require a shared LAN. See [USB AOA Bulk transport](docs/usb-aoa-windows-driver.md) for the Windows WinUSB driver prerequisite and setup steps.

## Continuous integration and releases

- `.github/workflows/ci.yml` builds Android and the Windows Host package; the Windows Host build also compiles and embeds the SteamVR driver. `.github/workflows/steamvr-driver.yml` remains a focused standalone driver build workflow.
- `.github/workflows/release.yml` is manually triggered from the Actions tab. Select `patch`, `minor` or `major`; it increments VERSION, Android versionName/versionCode, creates a version tag, builds the Android release APK and Windows Host package, then publishes a GitHub Release.
- Android release signing is optional. Add `PHONEVR_RELEASE_KEYSTORE_BASE64`, `PHONEVR_RELEASE_STORE_PASSWORD`, `PHONEVR_RELEASE_KEY_ALIAS` and `PHONEVR_RELEASE_KEY_PASSWORD` as repository Actions secrets to publish a signed APK. Without all four secrets, the workflow publishes a clearly marked unsigned APK.

## SteamVR driver management

The Windows Host executable includes the compiled PhoneVR OpenVR driver. At startup it checks SteamVR registration and, unless the user has explicitly disabled auto-deployment, stages the embedded driver in a content-versioned directory under `%LOCALAPPDATA%\PhoneVR\SteamVRDriver` before registering it with SteamVR's `vrpathreg.exe`.

The in-app manager provides status refresh, **Install / Repair Bundled Driver**, and **Uninstall & Disable Auto Deploy**. Uninstall persists the user's opt-out choice, so a later Host launch does not reinstall the driver unexpectedly. Manual installation clears the opt-out marker. Only files under PhoneVR's own managed directory are deleted; manually registered drivers in other paths are only unregistered. Restart SteamVR after driver changes. No driver ZIP selector or separate Release driver ZIP is required.




### 2026-11-22 video-output follow-up

The current source includes a DisplayRedirect initialization-order and HMD property-override fix, plus synchronous Host H.264 hardware preflight before opening the pairing listener. See `docs/2026-11-22-displayredirect-startup-order.md` for the log evidence and device-side validation steps.

## Video-stream stability follow-up

The 2026-11-22 video encoder lifecycle and backpressure fix keeps one hardware H.264 encoder session alive across brief loopback TCP reconnects. Raw BGRA frames are received independently of FFmpeg input through a bounded latest-frame queue. See `docs/2026-11-22-video-encoder-lifecycle-backpressure.md` for details and validation status.


### 2026-11-22 QSV and D3D11 readback stability follow-up

The live H.264 path now requests NV12 for hardware encoders, preflights a full-resolution BGRA stream, limits QSV's asynchronous queue, and distinguishes transient input backpressure from a genuine no-access-unit stall. FFmpeg child processes run without visible console windows on Windows. Driver staging readback uses nonblocking `Map` polling so the capture worker does not wait on GPU completion while holding the protected immediate-context lock. New logs expose raw BGRA samples, FFmpeg stdout bytes and complete H.264 access-unit counts. See `docs/2026-11-22-video-qsv-stall-and-nonblocking-readback.md`.
