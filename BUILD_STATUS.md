# PhoneVR 0.5.19 build status

## This revision

- Add `verticalTiltDegrees` to Android `DisplaySettings`, JSON serialization, legacy SharedPreferences storage, optical preset persistence, and the Windows Host `PhoneDisplaySettings` contract.
- Apply vertical image-plane tilt in the Android GLES post-process shader as a perspective transform around the horizontal axis. Supported range: -20 to +20 degrees.
- Increase per-eye horizontal and vertical image translation limits from ±0.08 to ±0.20 normalized single-eye UV units.
- Add a headset-side **画面上下倾斜** slider. The existing four per-eye position sliders retain live local preview and persist changes to the active optical preset.
- Add Windows Host optical editor fields for left/right horizontal and vertical offsets and vertical tilt. **Apply Position Live** sends the edited settings directly to the connected phone without requiring a preset save. **Save & Apply** persists the preset and applies it.
- Android metadata is `versionName 0.5.19`, `versionCode 54`; repository, Host, SteamVR driver and AOA accessory metadata are synchronized to `0.5.19`.

## Validation status

- `cd windows-host && go test ./...`: passed.
- `cd windows-host && go vet ./...`: passed.
- `cd windows-host && GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go build ./cmd/phonevr-host`: passed.
- Windows amd64 test binary cross-compilation with `go test -c`: passed.
- `go test -race ./...`: one run encountered an unrelated local UDP port 48105 bind collision in `TestHostServiceLifecycleAndPairing`; rerun is required before claiming race-suite success.
- Android Gradle build and APK packaging are not verified in this environment because Android SDK/Gradle distribution availability is not established. GLES shader compilation and real headset fit/tilt validation remain pending on device.

## Network streaming investigation

The current Network Only implementation uses H.264 access-unit fragmentation over UDP with a 1200-byte payload, Android fragment reassembly, partial-fragment and whole-frame NACK feedback, and Host-side retained access units for retransmission. Android currently expires incomplete assemblies after 350 ms and uses a 10 ms NACK delay; a UDP receive gap of at least 1 second triggers decoder resynchronization at the next IDR. The Host retains network frames for bounded repair.

These mechanisms show where interruptions can occur, but source inspection alone cannot establish whether the reported failures originate in Wi-Fi loss/jitter, Host UDP send drops, encoder output gaps, TCP control-channel NACK delays, or MediaCodec stalls. Do not change packet pacing or timeouts blindly. Capture the following from the same 30-60 second failure window:

1. Windows Host runtime log, including `Video pipeline` lines, H.264 FPS, UDP fragment counts, input queue drops, UDP send errors, NACK repair counts and transport switch reasons.
2. SteamVR driver log, including raw-frame queue/readback/TCP send counters and socket errors.
3. Android `adb logcat -v time` filtered for `PhoneVRVideo`, `PhoneVR`, and `MediaCodec`, plus the displayed receive FPS, decode FPS, fragment rate, expired assemblies, queue drops, decoder input waits, last packet age, last complete-frame age and last decoded-frame age.
4. Connection topology: PC-to-phone Wi-Fi band (2.4/5 GHz), access point model, distance, whether PC is wired to the AP, negotiated video resolution/FPS/bitrate, and whether USB AOA is connected at the same time.

## Device validation

1. Install matching 0.5.19 Host and Android builds. Open the Host optical preset editor while wearing the headset.
2. Adjust left/right horizontal and vertical offsets and use **Apply Position Live**. Confirm the headset image moves immediately without closing the editor or removing the headset.
3. Adjust **Vertical image tilt** in small increments first. Positive and negative values should produce opposite vertical perspective changes. Return to 0 degrees if the image clips or eye strain occurs.
4. Save and apply the optical preset, disconnect/reconnect the phone, and confirm all five new calibration values persist.
5. For streaming diagnosis, reproduce the interruption for 30-60 seconds and collect the four evidence groups listed above.
