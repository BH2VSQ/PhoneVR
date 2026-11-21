# Build status

Revision: PhoneVR 0.4.19 diagnostics follow-up, 2026-11-22

## Passed

- `go test ./...`
- `go test -race ./...`
- `go vet ./...`
- `GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go build ./cmd/phonevr-host`
- `python -m unittest discover -s tools/release -p 'test_*.py'` (38 tests)

## Not verified in this environment

- Android Gradle/Kotlin APK build: requires Android SDK and Gradle distribution availability.
- SteamVR native driver build: requires Windows, MSVC and Windows SDK.
- Runtime validation on Redmi K40 / LineageOS 24 and NVIDIA MX450: requires the actual devices and SteamVR runtime.

See `docs/2026-11-22-video-startup-race.md` for log interpretation and device test commands.
