# Build Status - Native UI polish

Application version: `0.3.9`  
Network protocol: `v1`

## Changes

- Reworked the Win32 main window to match the former PhoneVR browser dashboard palette and compact layout.
- Added GDI+ anti-aliased rounded cards, pills, dots and menu surfaces.
- Restored green Host/HMD/battery indicators and purple SteamVR state.
- Kept text in single-line layouts with end ellipsis and responsive positions in English mode.
- Added Per-Monitor DPI awareness where supported, with legacy fallback.
- Recessed Daydream optical elements remain behind the rear face foam plane.
- Kept the native Win32/WGL application model; no Edge, Chrome, WebView or browser runtime is used.

## Validation completed

- `go test -race -count=1 ./...`: passed.
- `go vet ./...`: passed.
- `GOOS=windows GOARCH=amd64 go vet ./...`: passed.
- Windows x64 test binary cross-compile: passed.
- Windows GUI EXE cross-compile: passed.
- Windows Console EXE cross-compile: passed.

## Not run here

- Native Windows desktop runtime, GDI+ rendering, DPI changes and actual language switching: must be confirmed on a Windows desktop.
- Android Gradle build: Android source was not changed in this UI polish revision.
