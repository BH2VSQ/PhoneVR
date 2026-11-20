# Changelog

## 0.3.9
- Reworked native Windows main UI to closely follow the original PhoneVR dark dashboard design: navy surfaces, compact status pills, rounded telemetry cards, centered SteamVR state and compact menu layout.
- Replaced flat GDI rounded shapes with GDI+ anti-aliased drawing and ClearType Segoe UI typography; enabled Per-Monitor DPI awareness where supported.
- Restored distinct Host/HMD/battery/SteamVR indicator colours with responsive, single-line Chinese/English labels and end ellipsis to prevent layout overlap.
- Added dark title-bar handling, high-DPI initialization, and improved English single-line layout behavior.
- Recessed the Daydream optical assemblies into the eye-side face cavity and reduced lens barrel depth.

# Changelog

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
