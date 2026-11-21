# PhoneVR 0.4.13 Build Status

## 0.4.13 pose latency and tracking continuity

- Android collects IMU samples on its dedicated `PhoneVR-IMU` HandlerThread at a requested 5 ms period and sends the newest available pose at up to 200 Hz. The queue remains latest-sample-wins; historical poses are not accumulated.
- Android quaternion samples are hemisphere-aligned before deriving the relative pose so `q` and `-q` cannot produce an artificial discontinuity.
- Windows Host uses a bounded UDP receive buffer. After the first datagram it drains packets already queued (up to a 250 microsecond coalescing window) and forwards only the newest valid pose to the loopback driver bridge. This prevents the Host from replaying a backlog after a scheduling pause.
- Host logs five-second windows with RX rate, forwarded rate, coalesced count, sequence gaps, invalid packets, maximum inter-arrival interval and loopback write errors.
- The SteamVR driver also drains its local UDP queue, computes angular velocity from Android sensor timestamp deltas (not Windows packet processing time), applies a responsive low-pass filter and bounded angular-speed clamp, and sets `vecAngularVelocity` plus a negative `poseTimeOffset` based on local post-receive age (capped at 50 ms). The local age does not include phone-to-PC one-way transit because the Android and Windows monotonic clocks are not synchronized in this protocol version.
- SteamVR logs sample age, estimated angular speed, pitch, packet sequence and driver-drained packet count every five seconds. These diagnostics help correlate a gray flash at large pitch angles with tracking delivery versus compositor/display events.
- The HMD display component already returns `IsDisplayOnDesktop=false`. SteamVR's runtime **Settings → Developer → Enable Direct Mode** switch still needs to be enabled by the user when the runtime warns that the HMD is operating as a desktop monitor. The wireless `IVRVirtualDisplay` final-compositor handoff is intentionally retained; it is not replaced with `IVRDriverDirectModeComponent`.

## Validation in this environment

- Python release/source-regression tests — 19 passed
- Go Host unit/integration tests — passed, including queued-pose batch coalescing
- Go Race tests — passed
- Go Vet and Windows AMD64 Go Vet — passed
- Windows AMD64 GUI Host and console Host cross-builds — passed (these checks use the tracked embedded-driver placeholder, not a real native SteamVR DLL)
- Kotlin compilation of the standalone quaternion and pose-throttle helpers — passed
- GitHub Actions workflow YAML parsing — 3 workflows passed
- Android Gradle unit/build verification — not completed: Gradle 9.6.1 bootstrap attempted to download from `services.gradle.org`, but DNS resolution was unavailable in this environment
- Native MSVC SteamVR DLL compilation and actual SteamVR runtime testing — not available in this Linux environment

## Runtime checks still required

- Enable **SteamVR → Settings → Developer → Enable Direct Mode**, then restart SteamVR. If the option is disabled or cannot be enabled, follow Steam's GPU driver / Direct Mode troubleshooting; PhoneVR does not modify global SteamVR settings automatically.
- While rotating the phone, inspect Host's `Pose pipeline: Host RX ... forwarded ... coalesced ...` log. The receive rate should be close to the device's real sensor rate (up to 200 Hz); coalescing should be rare during normal operation.
- Inspect `vrserver.txt` `PhoneVR: pose telemetry age=... angular-speed=... pitch=...` and compare its timestamp to any gray flash. Low sample age and no Host sequence gaps point away from the pose transport and toward the D3D11/compositor path.
- Repeat slow and fast high-pitch/head-down movement. Hemisphere continuity and sensor-clock derivative reduce discontinuities and prediction error, but a gray flash cannot be declared fixed without the target GPU/SteamVR runtime test.
- The D3D11 virtual-display path still requires Windows runtime validation. Encoder-owned pixel buffering, video encoding, network transmission to Android and Android video display remain unimplemented in this version.
