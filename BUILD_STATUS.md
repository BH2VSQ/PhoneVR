# Build Status - PhoneVR 0.3.14 Theme and Light-Mode Rendering

Application version: `0.3.14`  
Android versionCode: `17`  
Network protocol: `v1`

## 0.3.14 changes

- Added a dedicated light OpenGL preview palette (background, grid, XYZ axes and gaze ray) while preserving the dark preview palette.
- Compiled separate light/dark HMD display lists. The light material transform lifts only very dark mesh materials to preserve edge and foam visibility on the pale preview.
- Replaced hard-coded dark viewport colors in the optical editor and network settings with theme-aware surface colors; static labels hosted by the viewport use the same surface brush.
- Changed the appearance dialog to five color-only custom-painted swatches. Left-click applies, right-click opens the native Windows color picker and persists the edited swatch.
- Removed HEX editing, the custom-color apply action and redundant explanatory subtitle/help text.
- Added model color contrast and appearance layout regression tests.

## Validation in this environment

- `go test -race -count=1 ./...`: passed.
- `go vet ./...`: passed.
- `GOOS=windows GOARCH=amd64 go build` (GUI): passed.
- `GOOS=windows GOARCH=amd64 go build` (console): passed.
- `GOOS=windows GOARCH=amd64 go test -c`: passed.
- `GOOS=windows GOARCH=amd64 go vet ./...`: passed.
- `python3 tools/release/test_bump_version.py`: 5 tests passed.
- Release archive integrity (`unzip -t`): passed.
- Patch dry-run against the 0.3.13 source archive: passed.

## Not run here

- Interactive Windows desktop testing is unavailable in this container. Confirm actual theme visuals, left-click swatch application, right-click Windows color picker and saved palette persistence on the target Windows machine.
- Android Gradle/APK assembly was not run in this pass. Android runtime code is unchanged; version metadata was advanced to `0.3.14` / versionCode `17`. The prior test-only `org.json` dependency fix remains included.
