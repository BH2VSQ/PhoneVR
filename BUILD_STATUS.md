# PhoneVR 0.4.12 Build Status

## 0.4.12 HMD connection stability and Android control synchronization

- Fixed a disconnect/re-enumeration trigger in the HMD pose lifecycle: packet age no longer changes `DriverPose_t::deviceIsConnected` to false. During an activated session, the device remains connected; only `poseIsValid`/tracking result changes when IMU data is stale.
- An initial identity pose is published as connected-but-untracked during activation. Pose freshness timeout is now 1000 ms rather than 250 ms to tolerate brief scheduling and Wi-Fi jitter. The latest pose is submitted on every `RunFrame` even if its UDP sequence is unchanged, matching OpenVR guidance that drivers must keep submitting poses or the runtime will invalidate stale pose data.
- Added transition diagnostics in `vrserver.txt` to show when IMU tracking becomes stale or resumes while preserving HMD connection.
- Android optical-preset replies are sent immediately from the TCP control thread, avoiding an unnecessary dependency on UI-thread scheduling. Host retries catalogue requests three times and keeps the session alive if the phone does not answer.
- Android manual discovery auto-fills and persists the chosen Host address and control port.
- Added Python source-regression tests and a Go integration test for catalogue retry/control-session continuity.

## Validation in this environment

- `go test ./...` — passed
- `go test -race ./...` — passed
- `go vet ./...` — passed
- Python release and source-regression tests — 14 tests passed
- Windows AMD64 Go vet, test-binary cross-compilation and Host executable cross-build — passed
- GitHub Actions workflow YAML parsing — 3 workflows passed
- Android Gradle unit/build verification — not run: the wrapper could not download Gradle 9.6.1 because `services.gradle.org` DNS/network access is unavailable (`UnknownHostException`)
- Native MSVC SteamVR driver compilation and SteamVR runtime test — must be performed on Windows

## Remaining runtime checks

- Confirm in `vrserver.txt` that `IMU tracking stale/resumed` transitions no longer cause SteamVR HMD device removal or rediscovery.
- Confirm the HMD's connected flag remains stable while temporarily pausing the Android sensor sender; it should become untracked but stay enumerated.
- Confirm Host logs show `Received N optical presets from ...` after pairing. If Android does not answer, Host should log each retry and the final timeout while keeping pose reception active.
- Confirm Android search fills the IP and control port before pressing Connect PC.
- No claim is made that the D3D11 gray/black visual cycle is fully resolved; this change specifically removes the device-disconnect behavior caused by stale IMU samples. The display redirect's GPU/shared-texture path still requires separate runtime telemetry if flicker persists.
