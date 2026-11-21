# Build Status - Windows Host UI redesign

Application version: `0.3.10`  
Network protocol: `v1`

## Changes in this revision

- Main menu is an independent, non-activating owned popup. It never changes the OpenGL pose-preview viewport; clicks outside the popup and application deactivation close it cleanly.
- Dashboard, menu, dialog and viewport backgrounds use buffered painting to minimize visible intermediate redraws.
- Phone Display, Optical Presets, Network Settings and Runtime Logs have been rebuilt as larger, card-based interfaces with clearer grouping.
- Dialogs are resizable and recalculate their layouts when the window size changes.
- Phone Display and Network Settings use clipped content viewports with native vertical scroll bars and mouse-wheel support. Footer actions remain visible while content scrolls.
- Preset lists and runtime logs retain dedicated native scroll bars.
- Dashboard typography, telemetry values, dialog headings, field labels, input controls and buttons have been enlarged.
- Existing host networking, phone display settings, optical preset persistence and log actions are preserved.

## Validation completed in this environment

- `go test -race -count=1 ./...`: passed.
- `go vet ./...`: passed.
- `GOOS=windows GOARCH=amd64 go build ./...`: passed.
- `GOOS=windows GOARCH=amd64 go vet ./...`: passed.
- `GOOS=windows GOARCH=amd64 go test -c`: passed.
- Windows x64 host executable cross-build: passed.

## Not run here

- Interactive Windows desktop verification of GDI+/Win32 popup activation, display scaling, live resizing and scroll-bar behavior. Cross-compilation verifies the Windows code builds but does not replace a desktop runtime test.
- Android Gradle build; the Android application source was not changed in this revision.
