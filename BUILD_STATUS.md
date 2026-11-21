# Build status

Revision: PhoneVR 0.4.19 DisplayRedirect startup-order follow-up, 2026-11-22

## Passed in this environment

- `go test ./...`
- `go test -race ./...`
- `go vet ./...`
- `GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go build ./cmd/phonevr-host`
- Python release/integration tests: 41 tests

## Not verified in this environment

- Android Gradle/Kotlin APK build: requires the Android SDK and Gradle distribution availability.
- SteamVR native driver build: requires Windows, MSVC and Windows SDK.
- Runtime validation on Redmi K40 / LineageOS 24 and NVIDIA MX450: requires the target devices and SteamVR runtime.

See `docs/2026-11-22-displayredirect-startup-order.md` for the log evidence, changes, and next verification steps.
