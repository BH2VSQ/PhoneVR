# Build Status - PhoneVR 0.3.13 Theme Selector Interaction Fix

Application version: `0.3.13`  
Android versionCode: `16`  
Network protocol: `v1`

## 0.3.13 changes

- Removed the two native COMBOBOX controls from the Windows Appearance dialog. In the borderless custom dialog they did not reliably display/respond to popup-list interactions on the affected Windows system.
- Replaced the mode dropdown with two owner-drawn buttons: Dark Mode and Light Mode. Each button uses the dialog's standard `BN_CLICKED` -> `WM_COMMAND` path.
- Replaced the color dropdown with five directly clickable color swatches (Sky Blue, Sakura Pink, Lavender, Mint and Amber). Selecting a swatch updates the HEX edit and applies/persists the color immediately.
- Kept custom HEX input; Apply Custom Color applies the current mode plus the entered `#RRGGBB`.
- Added active-state button styling and expanded appearance-layout regression tests for mode buttons and all palette swatches.
- Synchronized Host, Android `versionName`, Android `versionCode` and client version identifier to 0.3.13 / 16.

## Validation completed in this environment

- `go test -race -count=1 ./...`: passed.
- `go vet ./...`: passed.
- `GOOS=windows GOARCH=amd64 go build`: passed.
- `GOOS=windows GOARCH=amd64 go test -c`: passed.
- Patch dry-run against the 0.3.12 source: passed.

## Not run here

- Android Gradle unit tests and APK assembly: not run in this container; Android SDK is unavailable. No Android functional code changed in this release apart from version metadata; the prior `Intent` and test-only JSON dependency fixes remain.
- Interactive Windows desktop verification: not available in this environment. The appearance page now uses ordinary owner-drawn buttons rather than native combo boxes, but the actual mouse interaction and visual feedback must still be confirmed on Windows.
