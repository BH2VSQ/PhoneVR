# Build status 0.3.8

- Replaced the browser app-mode UI with a Win32 native top-level application. The normal EXE is built with `-H=windowsgui` and creates a visible application window in the PhoneVRHost process.
- Removed Edge/Chrome launch code, the loopback HTTP dashboard server, dashboard HTML/JavaScript and the dashboard-specific resize endpoint/test.
- Added native Win32 controls and separate native child windows for device settings, optical presets, network configuration and logs.
- Added WGL/OpenGL 1.1 rendering with the embedded Daydream View 2017 OBJ/MTL mesh, material colours, depth testing, back-face culling, HMD quaternion transform, HMD-local XYZ axes and gaze ray.
- Added built-in Daydream View 2017 and Generic Cardboard optical presets plus persistent custom preset CRUD.
- Added persistent Chinese/English UI selection, Chinese default.
- Added native window `WM_SIZING` aspect-ratio enforcement at 1.2:1, default outer size 1080 x 900.
- `go test ./...`: passed in the current Linux build environment.
- `go test -race ./...`: passed.
- `go vet ./...`: passed.
- Windows x64 GUI cross-build: passed.
- Windows x64 Console cross-build: passed.
- Windows test binary cross-compilation: passed.
- Actual Win32 window, WGL rendering, DPI behavior and native control interactions still require runtime verification on Windows 10/11. This environment cannot launch a Windows desktop executable.
- Android application source and TCP/UDP protocol v1 are unchanged.
