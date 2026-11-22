# PhoneVR 0.4.19 build status

Revision: Android stream resilience, pose scheduling and headset UI update, 2026-11-22

## Changes in this revision

- Removed a high-priority MediaCodec busy-wait path. The decoder thread now uses normal priority and a 1 ms retry backoff whenever MediaCodec has no immediately available input buffer.
- Added stale-UDP recovery: after at least one second without packets, clear stale fragment assemblies and queued access units, request an IDR boundary and flush MediaCodec on the decoder thread.
- Added receiver observability for packet age, complete-frame age, decoded-frame age, expired assemblies, queue depth, dropped frames, decoder input waits and recovery count.
- When video becomes stale after playback starts, the renderer blanks the old frame instead of leaving a frozen image visible. The initial diagnostic scene remains available before first video playback.
- Moved PC connection and pose details into the Android menu. Added a compact top-edge stream status pill and menu diagnostics for receive/decode/display FPS, UDP fragments, reassembly state, decoder queue and drops.
- Added a persisted 180-degree reverse-landscape toggle. This changes the charging connector side without changing the SBS left/right stream layout; sensor coordinate remapping follows the display rotation.
- Kept Host pose snapshots lock-free through atomic pointer publication; the metric now reports pose publish duration rather than a removed mutex wait.
- Added full exception stack logging for failed device actions. Control-channel writes remain on a dedicated background writer executor.

## Passed in this environment

- `go test ./...`
- `go test -race ./...`
- `go vet ./...`
- `GOOS=windows GOARCH=amd64 go build ./cmd/phonevr-host`
- Python release/integration tests: 49 tests
- Standalone Kotlin compile of `VideoFragmentReassembler.kt` and `VideoStreamStats.kt`
- Kotlin reassembler expiration smoke test
- Kotlin syntax scan of changed Android files: no parser errors reported

## Not verified in this environment

- Android APK build. The wrapper could not download Gradle 9.6.1 from `services.gradle.org` because network/DNS access is unavailable, and no Android SDK is installed.
- Full Android/Kotlin type-check against Android SDK and runtime test on Redmi K40 / LineageOS 24.
- Native SteamVR driver build and in-runtime validation. This revision did not modify the SteamVR C++ source, but the complete Windows package still needs target-machine integration verification.
- End-to-end measurement of frame latency, Wi-Fi packet loss and actual 72 Hz headset presentation on the user's PC/phone.

See `docs/2026-11-22-stream-freeze-pose-priority-and-android-ui.md` for implementation details and device-side diagnostics.

## Android menu layout refinement

- MainActivity layout source updated: three-dot menu trigger is horizontally centered at the top of the landscape screen.
- Removed explanatory orientation text below the screen-flip control.
- Android Gradle/APK build has not been run in this environment; Windows/Android device rendering still requires target-device verification.
