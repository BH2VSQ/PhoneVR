# Build status

Revision: PhoneVR 0.4.19 QSV output-stall and nonblocking D3D11 readback follow-up, 2026-11-22

## Changes in this revision

- Kept one persistent FFmpeg hardware H.264 session across driver TCP reconnects. The capture TCP reader is independent from FFmpeg stdin and uses a pooled, bounded latest-frame-wins queue.
- Explicitly request NV12 for hardware H.264, use QSV-native AUD insertion and restore QSV asynchronous depth to 4. Keep the application-side unencoded timestamp queue bounded to 24 frames.
- Replace the tiny encoder initialization preflight with a full configured resolution/frame-rate BGRA encode test. The test verifies an H.264 Annex-B stream with AUD/IDR data and rejects a full-profile run exceeding three seconds.
- Treat timestamp queue saturation as recoverable frame drop. Detect a genuine no-access-unit stall only after the queue has backed up and no complete access unit has appeared for two seconds; recycle a stalled process with exponential backoff.
- Add first stdout/first H.264 access-unit messages and detailed raw RX, submitted, output, UDP and queue-drop statistics. Sample the first three BGRA frames to reveal black/constant raw captures.
- Start FFmpeg inventory, preflight and stream processes with `CREATE_NO_WINDOW` on Windows so the Host does not spawn visible console windows.
- Use `D3D11_MAP_FLAG_DO_NOT_WAIT` in the background staging readback worker and requeue `DXGI_ERROR_WAS_STILL_DRAWING`; this avoids waiting on GPU completion while holding the protected D3D11 immediate-context lock.

## Passed in this environment

- `go test ./...`
- `go test -race ./...`
- `go vet ./...`
- `GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go build ./cmd/phonevr-host`
- Python release/integration tests: 42 tests
- Local FFmpeg H.264 parser/session integration test (software encoder path): passed when available
- ZIP integrity validation

## Not verified in this environment

- Native SteamVR driver build: requires Windows, MSVC and Windows SDK.
- Actual Intel QSV runtime throughput: this environment does not have the target PC's Intel graphics driver and QSV device session.
- Android Gradle/Kotlin APK build: requires Android SDK and an available Gradle distribution.
- End-to-end runtime validation on the user's NVIDIA MX450 / Intel Iris Xe PC and Redmi K40 / LineageOS 24.

See `docs/2026-11-22-video-encoder-lifecycle-backpressure.md` and `docs/2026-11-22-video-qsv-stall-and-nonblocking-readback.md` for rationale and next-run diagnostics.
