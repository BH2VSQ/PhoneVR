# Build status

Revision: PhoneVR 0.4.19 persistent video encoder and capture backpressure fix, 2026-11-22

## Changes in this revision

- FFmpeg streaming session is owned by a Host service-lifetime worker rather than a single TCP bridge handler.
- The selected hardware H.264 capability from startup preflight is reused when the persistent session is created. TCP reconnects no longer rerun FFmpeg inventory or hardware probe subprocesses.
- Raw BGRA frames are read into pooled buffers and passed through a bounded two-frame latest-frame-wins queue. TCP receive continues independently while QSV consumes frames.
- Only one loopback video bridge connection is accepted at a time.
- Host receive buffer is 16 MiB and frame/idle reads use a 10-second timeout.
- Driver raw-frame send timeout is 5 seconds, send buffer request is 8 MiB, and TCP send failures report a rate-limited WSA error code.
- Failed encoder sessions use a five-second retry backoff to prevent repeated rapid process launches.

## Passed in this environment

- `go test ./...`
- `go test -race ./...`
- `go vet ./...`
- `GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go build -o PhoneVRHost-check.exe ./cmd/phonevr-host`
- Python release/integration tests: 41 tests
- ZIP integrity validation

## Not verified in this environment

- Native SteamVR driver build: requires Windows, MSVC and Windows SDK.
- Android Gradle/Kotlin APK build: requires Android SDK and an available Gradle distribution.
- End-to-end runtime validation on the user's NVIDIA MX450 PC and Redmi K40 / LineageOS 24.

See `docs/2026-11-22-video-encoder-lifecycle-backpressure.md` for the root-cause analysis and expected post-fix diagnostics.
