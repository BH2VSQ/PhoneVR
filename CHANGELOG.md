## 0.4.12 - HMD connection stability and Android control sync

- Decoupled OpenVR device presence from IMU packet freshness. An activated HMD now remains `deviceIsConnected = true`; stale or missing samples only mark its pose untracked instead of making SteamVR remove and rediscover the device.
- Published an initial connected-but-untracked identity pose during HMD activation so SteamVR receives an explicit device pose before the first phone sensor packet arrives.
- Submit the latest pose on every SteamVR `RunFrame`, even when the UDP sequence has not changed. OpenVR invalidates poses if the driver stops submitting updates, so this prevents a quiet sensor/network interval from aging out an otherwise usable pose.
- Increased the pose freshness grace period from 250 ms to 1000 ms to absorb short Android scheduling and Wi-Fi jitter without producing unnecessary tracking-invalid transitions.
- Added `vrserver.txt` transition logs for IMU tracking becoming stale or resuming, explicitly noting that the HMD remains connected.
- Android now replies to the Host's initial optical-preset catalogue request directly on the TCP control thread rather than queueing a network response on the UI thread.
- Host retries an unanswered optical-preset catalogue request up to three times and logs a missing/empty response without tearing down the connected control or pose session.
- Manual LAN discovery now automatically fills and persists the selected Host IPv4 address and control port; tapping another result still changes the selected target.
- Added regression tests for the HMD connected/untracked lifecycle, Android preset response path, Host address auto-fill, and Host preset-request retry.
- Android versionCode increased to 28. Live SteamVR and Android-device runtime validation remains required.

## 0.4.11 - MSVC Chaperone JSON literal build fix

- Shortened the embedded Chaperone JSON C++ raw-string delimiter from 17 characters to the standard maximum of 16, fixing MSVC C3512.
- Updated the Chaperone regression test to parse the raw-string delimiter generically and explicitly enforce the C++ 16-character limit, preventing this failure from recurring.
- Bumped synchronized Android, Windows Host and SteamVR driver version metadata; Android versionCode is now 27.

## 0.4.10 - Driver-provided Chaperone and stable universe

- Added a valid OpenVR v5 `chaperone_info` JSON directly to the HMD properties, avoiding an external per-user file dependency.
- Added a stable PhoneVR universe ID and set `Prop_CurrentUniverseId_Uint64` to the same identifier used by the JSON `universeID`.
- Defined neutral seated and standing transforms and empty collision bounds, accurately reflecting the current 3DoF orientation-only tracking capability without inventing room-scale position or safety boundaries.
- Checked both OpenVR property-write results and added explicit success/failure diagnostics to `vrserver.txt`; Chaperone failure is non-fatal so HMD pose tracking can continue.
- Added automated JSON/schema, ID-consistency and 3DoF-boundary regression tests.
- Bumped synchronized Android, Windows Host and SteamVR driver version metadata.

## 0.4.9 - SteamVR frame-sync load reduction and diagnostics

- Removed the unused full-resolution D3D11 staging copy from `IVRVirtualDisplay::Present`; P0 has no encoder or frame consumer yet, so the extra 2400x1080 copy was wasted GPU bandwidth and memory traffic.
- Kept the small synchronization staging texture and GPU completion wait, matching the intended fence path used by Valve's virtual-display sample.
- Increased bounded keyed-mutex acquisition timeout from 2 ms to 10 ms to reduce compositor frame drops under cross-process scheduling contention.
- Added diagnostics for enumerated DXGI adapters, selected adapter LUID, shared-backbuffer dimensions/format/sample count, keyed-mutex timeouts and D3D11 Map/device-removal failures. Error logging is rate-limited.
- Retained the live Host heartbeat and completed-frame state reporting. No Android video encoder or transport was added in this version.
- Bumped synchronized Android, Windows Host and SteamVR driver version metadata.

## 0.4.8 - SteamVR driver MSVC compatibility fix

- Removed the invalid `override` specifier from `VirtualDisplayComponent`'s destructor. The pinned OpenVR 2.15.6 `IVRVirtualDisplay` interface has no virtual destructor; the concrete component still runs `Shutdown()` when its owning display device is destroyed.
- Guarded `WIN32_LEAN_AND_MEAN` and `NOMINMAX` definitions so MSVC does not emit C4005 redefinition warnings when CMake already supplies them.
- Bumped the synchronized application and driver version metadata.

## 0.4.7 - P0 SteamVR status and display-frame synchronization

- Replaced the hard-coded SteamVR state with a live loopback driver heartbeat and explicit HMD, tracking, display and completed-frame states.
- Added D3D11 shared-backbuffer opening, bounded keyed-mutex synchronization, full-frame staging copies, GPU-completion waiting and virtual-vsync frame timing.
- Kept the future video stream port 48104 reserved and moved the internal driver heartbeat to loopback UDP 48105.
- Added heartbeat validation, Host integration tests and stale-state expiry checks.
- Bumped Android version metadata and synchronized repository release version to 0.4.7.

## 0.4.6 - Driver manager status readability

- Replaced the SteamVR driver status label with a read-only multiline text control that automatically wraps long paths and supports text selection and copying.
- Increased the status area height and moved the driver action buttons lower to improve vertical spacing.
- Removed the decorative footer frame surrounding the Close button while keeping the button in its original bottom position.

## 0.4.5 - Automated release

- Android versionName and versionCode bumped by the manual release workflow.
- Windows Host version metadata updated from the repository VERSION file.

# Changelog

## 0.4.4 - Bundled SteamVR driver deployment

- Removed the unstable local ZIP picker and ZIP-based driver installation UI from the Windows Host.
- Embedded the compiled PhoneVR OpenVR driver payload into both Windows Host executables using Go `embed.FS`.
- Updated the standard Windows Host build to compile the C++ driver before compiling the Host and stage its package for embedding.
- Added automatic startup deployment when SteamVR is detected, with content-hash version directories, manifest/DLL validation and post-copy verification.
- Added Install / Repair Bundled Driver and Uninstall & Disable Auto Deploy actions, including a persistent user opt-out marker.
- Restricted automatic cleanup to PhoneVR-managed files; external driver files are unregistered but never deleted.
- Removed obsolete ZIP download/extraction code and changed Release packaging to distribute self-contained Host executables without a separate driver ZIP.
- Added cross-platform unit tests for driver package validation, path safety, digest stability and file-copy integrity.

## 0.4.1 - Android codec probe compile fix

- Read `maxSupportedInstances` from `MediaCodecInfo.CodecCapabilities` (the API owner), not `MediaCodecInfo`.
- Convert `VideoCapabilities.supportedFrameRates.upper` from `Int` to `Double` for the normalized capability model.
- Keep the SteamVR driver install flow explicit: build the driver package, then register it with `vrpathreg.exe` using the provided PowerShell helper.

## 0.4.0 foundation - 2026-11-21

- Added Windows x64 OpenVR SteamVR driver foundation: `PhoneVRHmd` (HMD) and `PhoneVRDisplayRedirect` (`IVRVirtualDisplay`).
- Added loopback-only pose bridge on UDP 48103; Go Host forwards validated existing `PVSP` packets without changing Android protocol v1.
- Added Android MediaCodec runtime capability discovery for H.264, HEVC and AV1.
- Added a bounded Host-startup FFmpeg encoder-candidate probe (`-ffmpeg` override supported) and backend selection for NVIDIA NVENC, AMD AMF, Intel QSV and software fallback; candidates are explicitly not treated as usable until adapter/session initialization succeeds.
- Added `PVRV` video datagram v1 framing, MTU-safe access-unit fragmentation, validation and boundary tests, with 1200-byte bounded UDP payloads.
- Added SteamVR driver CMake build, installer helper and Windows CI workflow.
- Video capture/encoding/transport/decoding and Daydream BLE controller pairing remain future implementation tasks.

- Added a dedicated light OpenGL pose-preview palette with a pale background, softer floor grid, and distinct high-contrast XYZ axes and gaze ray; dark mode preserves its original dark preview palette.
- Added a light-mode headset material variant that raises only very dark mesh materials so frame, foam and lens details remain visible against the light background.
- Fixed light-theme optical-editor and network-settings surfaces; viewport cards and their static labels now use the active theme surface brush instead of hard-coded dark fills.
- Replaced the appearance dialog's text-labeled color controls with five color-only swatches. Left-click applies a swatch; right-click opens the Windows color picker and saves the customized swatch.
- Removed the HEX input field, custom-color apply button and redundant instructional/subtitle text from Windows dialogs and the menu.
- Added regression coverage for light-mode model-material contrast and responsive appearance-dialog swatch geometry.

## 0.3.13

- Replaced the non-responsive native appearance dropdowns with explicit dark/light mode buttons and five directly clickable accent-color swatches.
- Mode and preset-color button clicks immediately save settings and redraw the complete Windows UI; custom HEX remains supported.
- Added layout regression coverage for the new controls and synchronized platform version metadata.

## 0.3.12

- Fixed optical-preset dialog alignment by calculating card borders and all controls from one shared responsive layout; the two-button action rows now fill and align with the right-hand card.
- Separated optical-preset and appearance cards from their footers to eliminate overlapping surfaces and improve visual balance.
- Made light/dark mode and predefined accent selections apply and persist immediately; custom HEX colors remain editable and are applied with the Apply Theme button.
- Reworked theme refresh to update every native child control, trigger `WM_THEMECHANGED`, and force a full redraw using the newly-created brushes.
- Applied an explicit Explorer common-controls theme in light mode rather than relying on a blank theme name, preventing stale dark/system control rendering.
- Added platform-independent layout regression tests for preset and appearance dialogs.

## 0.3.11

- Fixed the Android Kotlin compile error caused by the missing `android.content.Intent` import.
- Added Android-local optical preset persistence, including migration of existing display calibration into a preset on upgrade.
- Made the Windows Host request the Android preset catalogue at session startup; the phone remains the source of truth and acknowledges edits synchronized from the Host.
- Replaced the Android manual display-calibration panel with an optical preset selector.
- Unified the Windows optical preset editor with phone display parameters; double-clicking a preset opens its editor, and save/apply operations synchronize through the control protocol.
- Added switchable light/dark themes and a custom `#RRGGBB` accent color, persisted in the user's application settings.
- Removed native caption close buttons from child dialogs in favor of the in-app Close action, and aligned their client areas with the selected theme.
- Expanded the SteamVR status pill for English labels and reserved a 16-pixel gap above the Yaw/Pitch/Roll cards.
- Added Android preset JSON tests, Go preset normalization tests and protocol documentation for Android-owned optical presets.

# Changelog

## 0.4.1 - Android codec probe compile fix

- Read `maxSupportedInstances` from `MediaCodecInfo.CodecCapabilities` (the API owner), not `MediaCodecInfo`.
- Convert `VideoCapabilities.supportedFrameRates.upper` from `Int` to `Double` for the normalized capability model.
- Keep the SteamVR driver install flow explicit: build the driver package, then register it with `vrpathreg.exe` using the provided PowerShell helper.

## 0.3.10
- Rebuilt the Windows Host main menu as an independent, non-activating owned popup. Opening it never resizes or shifts the OpenGL pose preview; clicking outside or switching applications dismisses it without leaving stale menu state.
- Added buffered painting for the dashboard, popup menu, scroll viewports and dialogs to reduce intermediate redraw flashing.
- Redesigned Phone Display, Optical Presets, Network Settings and Runtime Logs with larger card-based layouts, clearer grouping and resizable windows.
- Added responsive layouts, native vertical scroll bars and mouse-wheel scrolling for long settings pages; action buttons remain pinned in the footer.
- Increased application typography and control dimensions, including body text, labels, telemetry values, inputs and buttons.
- Preserved existing settings, preset management, network service and log actions.

## 0.3.9
- Reworked native Windows main UI to closely follow the original PhoneVR dark dashboard design: navy surfaces, compact status pills, rounded telemetry cards, centered SteamVR state and compact menu layout.
- Replaced flat GDI rounded shapes with GDI+ anti-aliased drawing and ClearType Segoe UI typography; enabled Per-Monitor DPI awareness where supported.
- Restored distinct Host/HMD/battery/SteamVR indicator colours with responsive, single-line Chinese/English labels and end ellipsis to prevent layout overlap.
- Added dark title-bar handling, high-DPI initialization, and improved English single-line layout behavior.
- Recessed the Daydream optical assemblies into the eye-side face cavity and reduced lens barrel depth.

## 0.3.8 - Native Windows desktop UI
- Replaced the Edge/Chrome app-mode dashboard and loopback HTTP UI server with a native Win32 desktop application.
- Added a native WGL/OpenGL HMD viewport that loads the embedded Daydream View 2017 OBJ/MTL mesh and renders a solid 3D model with depth testing and material shading.
- Kept HMD-local XYZ axes and gaze ray attached to the phone quaternion; the world floor grid remains fixed.
- Added native child windows for phone display settings, optical presets, network settings and logs.
- Kept remote phone display settings, center-view action, network start/stop/restart, battery status and applied-settings confirmation.
- Added persistent Chinese/English language selection, with Chinese as the default.
- Enforced a 1.2:1 native outer-window resize ratio and retained a 1080 x 900 default window size.
- Removed browser dashboard endpoints, Edge/Chrome launch helpers and browser-embedded HTML/JavaScript assets.
- Added mesh loading, Daydream preset and native preset persistence tests.
- Removed the old dashboard window resize test that failed when a test process had no real GUI window; native resize geometry now runs in the actual main-window `WM_SIZING` handler.
- Reworked the `WM_SIZING` RECT copy to use Win32 `RtlMoveMemory`, removing the `go vet` unsafe uintptr-to-pointer warning while keeping the native aspect-ratio resize logic.
- Consolidated Android and Windows checks/builds into one `PhoneVR CI` workflow with parallel platform jobs.
- Added a manually triggered Release workflow with patch/minor/major version bump, Android `versionCode` increment, synchronized client/Host version metadata, tagged builds, packaged Windows binaries, APK and GitHub Release publication.
- Network protocol remains v1. Android client source remains unchanged at version 0.3.6.


### Android CI follow-up
- Fixed JVM unit-test JSON runtime by adding `org.json:json` as a test-only dependency, preventing the mockable Android SDK `org.json` stubs from returning null in `OpticalPresetTest`.


## 0.4.2

- Added a SteamVR Driver Manager window to PhoneVR Host.
- Host can discover the latest PhoneVR Windows x64 driver ZIP from GitHub Releases, download it, validate the package, install it under the current user's LocalAppData directory, and register it with SteamVR through `vrpathreg.exe`.
- Added local driver ZIP selection, install/update, registered driver status inspection, and in-app driver uninstallation.
- Installation checks the `phonevr` manifest identity, avoids duplicate registration by replacing the existing PhoneVR entry, rolls back registration if an update fails, and only deletes files from the PhoneVR-managed driver directory.
- Release workflow now builds and publishes `PhoneVR-SteamVR-Driver-Windows-x64.zip` as a dedicated GitHub Release asset.
- Added ZIP traversal, package-integrity, and driver-manifest validation tests.

## 0.4.3

- Fixed the Windows local SteamVR driver ZIP picker crash. The `OPENFILENAMEW` ABI now includes `lpEditInfo` and `lpstrPrompt` before `pvReserved`, `dwReserved`, and `FlagsEx`.
- Added amd64/386 structure size and field-offset regression tests.
- Kept the selected-file buffer and UTF-16 filter/title buffers alive across the native dialog call.
- Added `CommDlgExtendedError` reporting so a real common-dialog failure is reported in the Host instead of being silently treated as cancellation.
