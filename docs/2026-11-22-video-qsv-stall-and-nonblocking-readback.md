# PhoneVR QSV output stall and nonblocking D3D11 readback follow-up (2026-11-22)

## Evidence from the target PC

The 03:17 Host log showed that the H.264 session selected `h264_qsv`, but its output rate remained around 0 to 0.4 FPS while the raw-frame queue dropped more than one hundred frames per interval. The Host then reported `H.264 encoder output is lagging; timestamp queue is full` and FFmpeg exited with status 1. The prior QSV invocation requested `yuv420p`; FFmpeg warned that QSV did not support that format and silently changed the encoder format to `nv12`.

The SteamVR screenshot showed a visually abnormal magenta performance/display region. A screenshot alone cannot identify whether those pixels originate in the compositor output, color conversion, or the display UI, so the Host now samples the first three raw BGRA frames before they enter FFmpeg.

## Changes in this follow-up

### QSV format and throughput

- Hardware H.264 paths now request `nv12` explicitly. Software paths continue to use `yuv420p`.
- QSV now uses `async_depth=4` instead of forcing the queue depth to 1. This restores a modest hardware pipeline depth while the application-side pending timestamp queue remains bounded to 24 frames.
- QSV enables the encoder-native `aud=1` option. The host AU parser uses AUD NAL units to split the Annex-B stream, avoiding a QSV post-encode bitstream-filter dependency.
- Startup preflight now encodes one second of a varied `testsrc` BGRA pattern at the negotiated full resolution and frame rate, then verifies that a real H.264 stream containing AUD and IDR data was written. It rejects a full-profile test that takes longer than three seconds. The previous 160x90 two-frame test only established that the encoder could initialize and was too weak to detect a very slow live profile.

### Backpressure and process lifetime

- A full pending timestamp queue now means the current raw frame is discarded. It does not by itself classify FFmpeg as failed.
- The session tracks submitted frames, stdout bytes, complete access units and the timestamp backlog.
- A watchdog only declares a real output stall when at least half of the bounded queue has accumulated and no complete H.264 access unit has appeared for two seconds. A genuinely stalled process is closed with exponential retry backoff; transient QSV backpressure does not spawn another process.
- `WriteFrame` checks whether the FFmpeg process has already exited before trying to send another frame.
- FFmpeg inventory, full-profile preflight and persistent streaming processes use `CREATE_NO_WINDOW` on Windows, preventing transient or long-lived FFmpeg child processes from opening visible console windows.
- Host diagnostics now report raw capture FPS, submitted FPS, H.264 access-unit FPS, UDP fragments, both queue-drop counters, FFmpeg stdout bytes and complete access-unit count.
- The first raw frames report sampled BGRA channel ranges and averages. Near-constant zero values point to an upstream capture/backbuffer issue; varied values with no FFmpeg access units point to the encoder path.

### D3D11 readback scheduling

- The background capture worker maps staging textures with `D3D11_MAP_FLAG_DO_NOT_WAIT`.
- When the GPU has not completed the staging copy, `Map` returns `DXGI_ERROR_WAS_STILL_DRAWING`; the driver requeues that slot and polls again rather than blocking on the protected immediate context.
- This nonblocking change applies to the video readback worker. `WaitForPresent` retains the small synchronization readback required by the virtual-display timing contract.
- Driver telemetry now distinguishes pending GPU map polls from actual map failures.

## Expected next-run diagnostics

On a healthy stream, expect these one-time messages:

- `Video encoder session initialized: h264_qsv ...`
- `Persistent live VR hardware H.264 encoder started: h264_qsv/qsv`
- `FFmpeg H.264 stdout became active ...`
- `FFmpeg emitted first H.264 access unit ...`

Then the five-second `Video pipeline` line should show nonzero raw RX FPS, submitted FPS, H.264 FPS, UDP fragment count, stdout bytes and access-unit count. Short backpressure events may increment `encoder drops`, but should not create a new FFmpeg process. Only a sustained no-access-unit stall should emit the explicit watchdog message and restart the process with backoff.

In `vrserver.txt`, `gpu-map-pending` may be nonzero because it counts poll attempts while the GPU is still working. `gpu-map-fail` should remain zero in normal operation. Persistent high pending-poll counts together with low readback FPS suggest that GPU staging readback is taking too long.

## Validation and limits

- Go unit tests: passed.
- Go race tests: passed.
- `go vet`: passed.
- Windows/amd64 Host cross-build: passed.
- Python release regression suite: 42 tests passed.
- The local environment exposes FFmpeg's QSV encoder options but does not have the user's Windows Intel graphics driver/session. It cannot validate actual QSV throughput on the MX450 / Iris Xe laptop.
- Native SteamVR DLL compilation still requires Windows, MSVC and the Windows SDK. Android APK compilation requires Android SDK and Gradle distribution access.
- The Redmi K40 / LineageOS 24 end-to-end video path remains to be validated on the target devices.
