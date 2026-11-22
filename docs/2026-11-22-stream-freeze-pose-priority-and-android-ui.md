# PhoneVR 0.4.19 stream-freeze, pose scheduling and Android UI update

Date: 2026-11-22

## Evidence and diagnosis

The recent Windows logs showed that H.264 frames were being produced and UDP fragments were being sent, while the Android device and Host pose display could intermittently stop updating. The Android decoder used `MediaCodec.dequeueInputBuffer(0)` in a retry loop. When the decoder temporarily had no input buffer, the pending access unit remained in the loop without a delay. The decoder thread also ran at `THREAD_PRIORITY_DISPLAY`. Together, a busy retry loop and elevated scheduling priority could consume CPU time needed by the 180 Hz sensor callback, pose sender and GL rendering thread.

The Host pose snapshot is already published through `atomic.Pointer[PoseSnapshot]`; readers no longer wait on a pose-state RWMutex. The log metric was renamed from a lock-wait metric to a publish-duration metric to match the implementation.

## Android video receiver changes

- Run the MediaCodec decode worker at normal thread priority so the IMU and display callback can be scheduled ahead of decoder backpressure retries.
- When `dequeueInputBuffer(0)` has no buffer, keep the current access unit and retry after a 1 ms backoff. Do not spin at display priority for the full 200 ms input wait timeout.
- If the UDP stream resumes after a gap of at least one second, discard partial reassembly state and queued stale access units, request an IDR boundary, and ask the decoder thread to flush MediaCodec. The flush occurs on the decoder thread.
- Count expired fragment assemblies and expose in-flight reassembly, decoder queue depth, queue drops, decoder drops, decoder input waits, decoder recoveries, packet age, complete-frame age and decode age.
- Log per-second receiver/decoder/reassembly state so a future freeze can be identified as UDP packet loss, incomplete frame assembly, decoder input starvation, decoder output starvation or no fresh packets.
- When the stream becomes stale after a live image has been displayed, render black instead of leaving the last decoded head pose frozen on the lenses. The initial diagnostic scene remains available before the first video frame.

## Android UI changes

- Move `PC 已连接`/searching/disconnected details into the menu panel.
- Move the live yaw/pitch/roll pose text into the menu panel.
- Add a compact stream-status pill at the top-right. It reports waiting, active display FPS, UDP assembly, decoder stall, display stall, stream interruption or negotiation failure. It is confined to the edge of the display and does not change the SBS render viewport or cover the central eye images.
- Add a persistent 180-degree landscape rotation toggle in the menu. It switches between `SCREEN_ORIENTATION_LANDSCAPE` and `SCREEN_ORIENTATION_REVERSE_LANDSCAPE`, allowing the USB charging connector to be placed on the opposite side. The manifest already handles orientation/screen-size changes without recreating the activity; sensor coordinate remapping follows the display rotation.
- Add detailed receive/decode/display FPS, UDP fragments, reassembly queue, decoder queue, drop and recovery counters inside the menu.

## Control-channel exception diagnostics

Control replies are queued through the dedicated single-thread control writer. The Android device-action rejection catch now records the complete exception stack with `Log.e`, so any future `NetworkOnMainThreadException` can be traced to its originating call instead of only returning the exception name to the Host.

## Validation

Passed:

- `go test ./...`
- `go test -race ./...`
- `go vet ./...`
- `GOOS=windows GOARCH=amd64 go build ./cmd/phonevr-host`
- Python release/integration suite: 49 tests
- Kotlin compilation of `VideoFragmentReassembler.kt` and `VideoStreamStats.kt`
- Kotlin reassembler expiration smoke test
- Kotlin compiler syntax pass over the changed Android source files; full type-check is unavailable without Android SDK stubs

Not run:

- Android APK build. The Gradle wrapper attempted to download Gradle 9.6.1 from `services.gradle.org`, but DNS/network access was unavailable; no Android SDK is installed in this environment.
- Runtime confirmation on Redmi K40 / LineageOS 24.
- Native SteamVR driver build in this environment. The SteamVR C++ driver sources were not changed in this revision; Windows/MSVC target build remains the final full-package verification.

## Next device-side checks

On the Redmi K40, watch Logcat tag `PhoneVRVideo` for:

- `Video UDP resumed after ... gap; flushing decoder and waiting for IDR`
- `MediaCodec input remained unavailable ...`
- `H.264 output stalled ...`
- `Video state=... RX=... decode=... fragments=... expired=... decoderQueue=... queueDrops=... decoderDrops=... inputWaits=... recoveries=... packetAge=... completeAge=... decodedAge=...`

The menu status provides the same key counters in a compact form. A freeze with fragment rate continuing but `expiredAssemblies` increasing points to incomplete UDP frames. A freeze with completed RX continuing and `decoderRecoveries` increasing points to MediaCodec. A decoded FPS above zero with display FPS at zero points to the SurfaceTexture/GL presentation stage.
