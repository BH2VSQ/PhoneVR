# PhoneVR 0.5.31 build status

## This revision (0.5.31)

- Correct the Daydream controller orientation path. Decode the three XKF3 orientation fields as a signed axis-angle rotation vector and construct a quaternion. The old Euler-angle interpretation could skew the controller pointer when multiple rotation axes were active.
- Add an explicit Daydream-to-OpenVR controller-frame conversion. The arm-model implementation uses OpenVR's right-handed world convention with local `-Z` forward.
- Add a Google Daydream-style arm model for both hands. It estimates elbow, wrist and controller positions from the HMD pose, controller orientation and hand assignment. It is a kinematic estimate and does not claim real 6DoF hand tracking.
- Add App-button hold behavior. A short App press retains its menu action. Holding App for about 800 ms aligns the controller with the current HMD view direction. The Android BLE manager also attempts the Daydream FE55 Control Characteristic `0x00` hardware-fusion reference recenter command when available.
- Provide filtered linear and angular velocities in the SteamVR controller pose so OpenVR can predict between pose updates. Clear the derived angular velocity on a recenter event to avoid reporting the calibration jump as physical motion.
- Add a pairing-panel hint that explains the App-button recenter gesture. Keep Home mapped to the system input.
- Add a standalone CTest target for the Daydream pose model and tests for coordinate conversion, left/right arm symmetry, App short/long press behavior, recentering and motion velocity output.
- Android metadata is `versionName 0.5.31`, `versionCode 66`; repository, Host, SteamVR driver and AOA accessory metadata are synchronized to `0.5.31`.

## 0.5.31 validation status

- `python3 -m unittest discover -s tools/release -p 'test_*.py'`: passed, 69 tests.
- `g++ -std=c++17 -Wall -Wextra -Werror steamvr-driver/tests/daydream_pose_model_test.cpp`: compiled successfully; pose-model test passed.
- `cd windows-host && go test ./...`: passed.
- `cd windows-host && go test -race ./...`: passed.
- `cd windows-host && go vet ./...`: passed.
- `GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go build -o /tmp/phonevr-host-0.5.31.exe ./cmd/phonevr-host`: passed.
- Android Gradle build was not run because this environment has no `ANDROID_HOME` or `ANDROID_SDK_ROOT`. The release script exits at its SDK preflight. CI still needs to run `bash ./scripts/build-android.sh both` to validate Kotlin compilation and APK packaging.
- Native SteamVR MSVC driver build was not run in this Linux environment. The standalone C++ arm-model test was compiled with GCC; the full OpenVR driver still requires the Windows/MSVC build environment.
- No physical Daydream controller was available. BLE Control Characteristic hardware recenter, actual hand orientation, arm-model comfort and SteamVR laser pointer calibration still require testing on the target hardware.

## Previous build status: 0.5.25

## This revision (0.5.25)

- Fix the Host/SteamVR stream-profile handoff. The Host stores the UI selection in `video-profile.json` and publishes the profile actually active for its running service in `video-profile-active.json`.
- The SteamVR driver reads only `video-profile-active.json`, so restarting SteamVR while Host is still running cannot silently switch its capture dimensions away from the Host encoder/Android negotiation dimensions.
- Every Host service start reloads the pending profile and reruns hardware H.264 preflight if the selected profile differs from the previous probe.
- Raw-frame profile mismatches are drained and discarded without reconnecting for every frame; the Host emits one actionable mismatch log. See `docs/2026-11-24-video-profile-handoff-fix.md`.
- Product metadata is synchronized to 0.5.25; Android versionCode is 60.

## 0.5.25 validation status

- `cd windows-host && go test ./...`: passed.
- `cd windows-host && go test -race ./...`: passed.
- `cd windows-host && go vet ./...`: passed.
- `GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go build ./cmd/phonevr-host`: passed.
- `python3 -m unittest discover -s tools/release -p 'test_*.py'`: passed, 62 tests.
- `git diff --check`: passed.
- SteamVR MSVC driver build and Android APK build: not run in this Linux environment.
- Physical Windows/SteamVR/Android profile-switch test: not run.

## This revision (0.5.24)

- Add four selectable SBS stream profiles: 1280×576, 1600×720, 1920×864 and 2400×1080 at 60 FPS.
- Synchronize driver recommended target size, capture texture/CPU frame dimensions, Host raw-video buffers, FFmpeg encoder input, Welcome dimensions and Android hardware decoder capability negotiation.
- Apply profile-specific H.264 target bitrates: 5, 8, 13 and 18 Mbps. Raise Android frame reassembly capacity to 8 MiB.
- Add the Host Stream Resolution dialog and persisted `%APPDATA%\PhoneVRStream\video-profile.json` setting. Host and SteamVR driver must both be restarted after applying a new profile.
- Android metadata is `versionName 0.5.24`, `versionCode 59`; Host, driver and AOA descriptor metadata are synchronized to `0.5.24`.
- Audio status: no end-to-end capture, playback, virtual endpoint or audio network transport is included. `docs/audio-streaming-architecture.md` specifies the next implementation stages.

## 0.5.24 validation status

- `cd windows-host && go test ./...`: passed.
- `cd windows-host && go test -race ./...`: passed.
- `cd windows-host && go vet ./...`: passed.
- `GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go build -o /tmp/PhoneVRHost-0.5.24.exe ./cmd/phonevr-host`: passed.
- `python3 -m unittest discover -s tools/release -p 'test_*.py'`: passed, 61 tests.
- `git diff --check`: passed.
- SteamVR native MSVC driver build and Android Gradle packaging were not run because the Windows SDK/WDK and Android SDK build environments are unavailable here.
- No physical phone, actual SteamVR runtime, negotiated high-resolution decode, Windows audio endpoint, or end-to-end audio test has been performed for this revision.
- Audio is explicitly not implemented in 0.5.24. The architecture and virtual-endpoint requirements are documented in `docs/audio-streaming-architecture.md`.

# PhoneVR 0.5.23 build status

## This revision (0.5.23)

- Add SteamVR virtual-display timing telemetry for maximum `Present` callback interval, intervals exceeding 1.5 and 3 nominal 72 Hz periods, pending-Present overwrites, actual `WaitForPresent` wake overshoot, missed virtual VSync ticks, and keyed-mutex acquisition failures.
- Emit the counters in the existing five-second `virtual display` driver diagnostic line. The new counters are observational only. The 72 Hz clock, 60 FPS capture ceiling, pose prediction, D3D11 capture behavior and H.264 encoder configuration are unchanged.
- Keep the 800x720 per-eye render target, 1600x720 SBS stream and 8 Mbps H.264 target unchanged. A higher-resolution profile needs end-to-end capability negotiation and runtime performance validation.
- Android metadata is `versionName 0.5.29`, `versionCode 64`; repository, Host, SteamVR driver and AOA accessory metadata are synchronized to `0.5.29`.

## 0.5.23 validation status

- `python3 -m unittest discover -s tools/release -p 'test_*.py'`: passed, 61 tests.
- `cd windows-host && go test ./...`: passed.
- `cd windows-host && go test -race ./...`: passed.
- `cd windows-host && go vet ./...`: passed.
- `GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go build ./cmd/phonevr-host`: passed.
- SteamVR native driver compilation, Android Gradle packaging, physical headset runtime and compositor frame-timing A/B remain unverified in this environment.

## Previous revision (0.5.21)

- Separate active/standby video-route reachability from media freshness. The Host probes every selected UDP/AOA path and keeps a reachable route available while the H.264 encoder is temporarily idle.
- Fix Android `video_feedback`: report actual packet/decode age for the latest network or USB media source instead of always sending `-1`.
- Increase H.264 startup no-output timeout to 8 seconds and steady-state stall timeout to 5 seconds. Only recycle a backed-up encoder when both the completed-access-unit stream and FFmpeg stdout have stopped progressing.
- Cap QSV retry backoff at 8 seconds and reset it after a sustained output run. Log each rejected hardware H.264 candidate and the first/recovered path-probe acknowledgement.
- Add Host regression tests for active-route probes, preserving active Network paths during a media pause, and FFmpeg stdout progress preventing false stall detection.
- Android metadata was `versionName 0.5.21`, `versionCode 56`; repository, Host, SteamVR driver and AOA accessory metadata were synchronized to `0.5.21`.

## 0.5.21 validation status

- `cd windows-host && go test ./...`: passed.
- `cd windows-host && go test -race ./...`: passed.
- `cd windows-host && go vet ./...`: passed.
- `cd windows-host && GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go build ./cmd/phonevr-host`: passed.
- Android Gradle build/APK packaging, SteamVR Driver native build, real Intel QSV runtime and physical Wi-Fi headset streaming remain unverified in this environment.

## Log findings from the 2026-11-23 runtime session

- Host selected Network Only and correctly logged `USB AOA scanner paused (network-only video transport)`. The Android device `M2012K11AC` paired over Wi-Fi/LAN at `192.168.2.242`; Host's LAN interface was `192.168.2.3`. Pose telemetry initially remained near 180-190 Hz.
- Encoder preflight validated only one hardware H.264 backend: `h264_qsv`. NVENC and AMF appeared in FFmpeg's encoder inventory but were not validated for the live 1600x720@60 profile. The Host therefore had no hardware failover backend.
- QSV repeatedly entered backpressure. The log shows successful output bursts, including 25.5 FPS / 1,868 UDP fragments at 21:04:23 and 20.9 FPS / 1,124 fragments at 21:06:23. The process was then recycled after the old 2-second no-access-unit threshold. The retry backoff grew to 60 seconds, accounting for much of the prolonged blackout.
- The log repeatedly reported that no healthy negotiated video route existed, including near periods where UDP fragments were being sent. Source inspection found the cause: Android's periodic feedback code always sent `lastPacketAgeMs=-1` and `lastDecodedAgeMs=-1`, while Host required valid media age after the active route's initial grace period. In addition, Host probed only standby routes, not the active one. This could demote a healthy Wi-Fi path whenever media temporarily paused.
- Version 0.5.21 addresses the Host route-health logic and the Android feedback values. It also relaxes the H.264 startup/runtime watchdogs and limits recovery backoff. The supplied Host log does not include Android `PhoneVRVideo`/MediaCodec diagnostics, so actual Wi-Fi packet loss, Android reassembly loss and decoder stalls still require a paired Android log if interruptions remain after this change.


## 0.5.19 previous revision

- Add `verticalTiltDegrees` to Android `DisplaySettings`, JSON serialization, legacy SharedPreferences storage, optical preset persistence, and the Windows Host `PhoneDisplaySettings` contract.
- Apply vertical image-plane tilt in the Android GLES post-process shader as a perspective transform around the horizontal axis. Supported range: -20 to +20 degrees.
- Increase per-eye horizontal and vertical image translation limits from ±0.08 to ±0.20 normalized single-eye UV units.
- Add a headset-side **画面上下倾斜** slider. The existing four per-eye position sliders retain live local preview and persist changes to the active optical preset.
- Add Windows Host optical editor fields for left/right horizontal and vertical offsets and vertical tilt. **Apply Position Live** sends the edited settings directly to the connected phone without requiring a preset save. **Save & Apply** persists the preset and applies it.
- Android metadata was `versionName 0.5.19`, `versionCode 54`; repository, Host, SteamVR driver and AOA accessory metadata were synchronized to `0.5.19`.

## 0.5.19 validation status

- `cd windows-host && go test ./...`: passed.
- `cd windows-host && go vet ./...`: passed.
- `cd windows-host && GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go build ./cmd/phonevr-host`: passed.
- Windows amd64 test binary cross-compilation with `go test -c`: passed.
- `go test -race ./...`: one run encountered an unrelated local UDP port 48105 bind collision in `TestHostServiceLifecycleAndPairing`; rerun is required before claiming race-suite success.
- Android Gradle build and APK packaging are not verified in this environment because Android SDK/Gradle distribution availability is not established. GLES shader compilation and real headset fit/tilt validation remain pending on device.

## 0.5.19 network streaming investigation

The current Network Only implementation uses H.264 access-unit fragmentation over UDP with a 1200-byte payload, Android fragment reassembly, partial-fragment and whole-frame NACK feedback, and Host-side retained access units for retransmission. Android currently expires incomplete assemblies after 350 ms and uses a 10 ms NACK delay; a UDP receive gap of at least 1 second triggers decoder resynchronization at the next IDR. The Host retains network frames for bounded repair.

These mechanisms show where interruptions can occur, but source inspection alone cannot establish whether the reported failures originate in Wi-Fi loss/jitter, Host UDP send drops, encoder output gaps, TCP control-channel NACK delays, or MediaCodec stalls. Do not change packet pacing or timeouts blindly. Capture the following from the same 30-60 second failure window:

1. Windows Host runtime log, including `Video pipeline` lines, H.264 FPS, UDP fragment counts, input queue drops, UDP send errors, NACK repair counts and transport switch reasons.
2. SteamVR driver log, including raw-frame queue/readback/TCP send counters and socket errors.
3. Android `adb logcat -v time` filtered for `PhoneVRVideo`, `PhoneVR`, and `MediaCodec`, plus the displayed receive FPS, decode FPS, fragment rate, expired assemblies, queue drops, decoder input waits, last packet age, last complete-frame age and last decoded-frame age.
4. Connection topology: PC-to-phone Wi-Fi band (2.4/5 GHz), access point model, distance, whether PC is wired to the AP, negotiated video resolution/FPS/bitrate, and whether USB AOA is connected at the same time.

## 0.5.19 device validation

1. Install matching 0.5.19 Host and Android builds. Open the Host optical preset editor while wearing the headset.
2. Adjust left/right horizontal and vertical offsets and use **Apply Position Live**. Confirm the headset image moves immediately without closing the editor or removing the headset.
3. Adjust **Vertical image tilt** in small increments first. Positive and negative values should produce opposite vertical perspective changes. Return to 0 degrees if the image clips or eye strain occurs.
4. Save and apply the optical preset, disconnect/reconnect the phone, and confirm all five new calibration values persist.
5. For streaming diagnosis, reproduce the interruption for 30-60 seconds and collect the four evidence groups listed above.
