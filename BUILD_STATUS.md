# PhoneVR 0.4.14 Build Status

## 0.4.14 pose burst forwarding and log repaint stability

- Host pose telemetry previously showed RX around 150–230 Hz but forwarded only 23–95 Hz with hundreds of coalesced datagrams. The previous Host policy forwarded exactly one sample per receive batch, dropping the remaining samples even when the burst contained only two or three nearby samples.
- The Host now forwards every datagram in a short batch (up to 32 samples). Only unusually large stale backlogs are collapsed to the newest sample. The SteamVR receiver drains the loopback queue while estimating angular velocity across each valid sensor sample, then publishes the newest orientation to OpenVR.
- Host telemetry now reports `max Host read gap`, `max Android sensor interval`, and `max batch` separately. `max Host read gap` measures time between Go receive operations and may include Windows thread scheduling delays or batched socket delivery; it is not a direct one-way network latency measurement. `max Android sensor interval` is calculated from Android monotonic sensor timestamps and identifies whether the phone's sensor/send stream itself paused.
- Android's dedicated IMU HandlerThread and pose transmit worker request display scheduling priority. If Android disallows the priority change, both continue with the default priority.
- Runtime Logs uses an explicit redraw-suppressed text replacement followed by a full erase/invalidate/repaint. Wheel and vertical-scroll messages are forwarded to the native EDIT control that owns the log scrollbar.
- The Android sensor timestamp is still not directly comparable to the Windows monotonic clock. A true one-way phone-to-PC latency figure requires a clock-offset exchange protocol; this version reports the sensor-side inter-sample cadence and Host-side read scheduling separately.

## Validation in this environment

- Go Host unit/integration tests — passed
- Go Race tests — passed
- Go Vet and Windows AMD64 Go Vet — passed
- Windows AMD64 test binary, console Host and GUI Host cross-builds — passed; these checks use the tracked embedded-driver placeholder, not a real native SteamVR DLL
- Python release/source-regression tests — 20 passed
- Android Kotlin/Gradle packaging — not verified in this environment; full Gradle build requires the Android toolchain and dependencies
- Native MSVC SteamVR DLL compilation and actual SteamVR runtime / log-window visual test — requires the Windows target environment

## Runtime checks still required

- Compare `Host RX` and `forwarded`; during regular short batches they should now be close. `coalesced` should remain zero except when the Host resumes after a very large backlog.
- Compare `max Host read gap` with `max Android sensor interval`. If sensor interval remains near 5–8 ms while Host read gap spikes, the phone is sampling regularly and the pause is after the sensor timestamp (Android UDP scheduling, Wi-Fi delivery, or Windows receive scheduling). If both spike together, investigate Android sensor/send scheduling.
- Compare `vrserver.txt` pose telemetry and high-pitch gray flashes. Low sensor intervals and fresh driver samples point away from tracking transport and toward D3D11/compositor presentation.
- Verify log text no longer overlaps during refresh, mouse-wheel scrolling, scrollbar paging, selection and copying on Windows.
- D3D11 virtual-display runtime validation, video encoding, network transmission to Android and Android video display remain separate unfinished work.
