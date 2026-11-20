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
- Network protocol remains v1. Android client source remains unchanged at version 0.3.6.
