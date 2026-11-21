# Build Status - PhoneVR 0.3.12 UI Layout and Theme Fix

Application version: `0.3.12`  
Android versionCode: `15`  
Network protocol: `v1` (additive Android optical-preset capability)

## 0.3.12 changes

- Optical-preset dialog card geometry and child-control geometry now come from `ComputeNativePresetDialogLayout`; both action columns are aligned and use the available card width instead of a fixed 178 px maximum.
- Preset and appearance content cards stop above the fixed footer, eliminating the card/footer overlap visible in the supplied screenshots.
- Appearance form geometry comes from `ComputeNativeAppearanceDialogLayout`; the controls expand to the usable width.
- Light/dark mode and preset accent selections now immediately apply and persist. Manually entered HEX values continue to use the Apply Theme action.
- Theme refresh recreates brushes, applies the explicit `Explorer` or `DarkMode_Explorer` common-control theme, sends `WM_THEMECHANGED`, and forces an all-children redraw.
- Added platform-independent regression tests covering preset action-column alignment, card/footer separation, and appearance control bounds.

## Validation completed in this environment

- `go test -race -count=1 ./...`: passed.
- `go vet ./...`: passed.
- `GOOS=windows GOARCH=amd64 go build`: passed.
- `GOOS=windows GOARCH=amd64 go test -c`: passed.
- Release script unit tests: 5/5 passed.
- The 0.3.11 Android CI correction (`Intent` import and test-only JVM `org.json` dependency) is retained. Android versionCode incremented to 15 for 0.3.12.

## Not run here

- Android Gradle unit tests and APK assembly: not run because Android SDK is unavailable in this container.
- Interactive Windows desktop verification: not available in this environment. Please check the dropdowns, live theme updates, layout at different window sizes and Windows display scaling on a Windows machine.
