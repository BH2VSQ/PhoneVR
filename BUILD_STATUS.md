# Build Status — PhoneVR 0.4.0 foundation

## Implemented in this milestone

- Windows Host receives Android `PVSP` pose protocol v1 and best-effort forwards each validated latest pose to `127.0.0.1:48103` for the separate SteamVR driver. The LAN protocol remains backward compatible.
- Added a Windows x64 OpenVR server-driver foundation registering a 3DoF `TrackedDeviceClass_HMD` and a `TrackedDeviceClass_DisplayRedirect` / `IVRVirtualDisplay` device. HMD pose is published through `IVRServerDriverHost::TrackedDevicePoseUpdated`.
- Added FFmpeg encoder candidate discovery and selection for H.264 / HEVC / AV1 across NVIDIA NVENC, AMD AMF, Intel QSV and software encoders. The Windows Host runs a bounded background probe at startup and logs candidate availability; `-ffmpeg <path>` can specify the executable. This does not yet prove that a GPU adapter can open an encoder session.
- Added `PVRV` video packet v1, 32-byte header validation and an MTU-safe access-unit fragmenter with tests.
- Android `CodecCapabilityProbe` queries MediaCodec H.264 / HEVC / AV1 decoders at runtime and includes optional capability metadata in the existing TCP v1 `hello`. No handset model name is used as a codec capability substitute.
- Added CMake / PowerShell driver build and installation helpers, plus dedicated Windows x64 CI jobs for the SteamVR driver.

## Local validation

- `go test ./...`: passed.
- `go test -race ./...`: passed.
- `go vet ./...`: passed.
- Windows AMD64 Host cross-build: passed.
- Release tooling tests: 5/5 passed.
- `CodecCapabilityProbe.kt` Kotlin syntax/type check against Android API-shaped stubs: passed; this is not a substitute for an Android SDK/Gradle build.
- Android Gradle tests and APK build: not run here; Android SDK is not configured in this container. The repository CI job is configured to run the full Gradle test + Debug/Release APK build.
- SteamVR C++ driver build and runtime load: not run here; the current container is Linux and has no Windows SDK / Visual Studio toolchain. A dedicated Windows x64 CI build job is configured, but the artifact still needs to pass that CI job and be tested with SteamVR on a Windows PC.

## Explicitly not complete yet

- D3D11 final-composited-backbuffer acquisition from `PresentInfo_t`, pixel-format conversion and hardware encoder session initialization.
- Physical GPU adapter enumeration/selection (DXGI), and validation that the selected NVENC / AMF / QSV encoder can open on the selected adapter. Current FFmpeg parsing only reports candidates compiled into the selected FFmpeg binary.
- UDP video sender, frame reassembly/jitter buffer, key-frame request/recovery, clock synchronization and adaptive bitrate. The packet format and access-unit fragmentation are implemented and tested.
- Android MediaCodec video stream decoder, `SurfaceTexture` / OpenGL texture presentation and optical-warp integration. Decoder capability discovery is implemented only.
- Daydream View controller BLE/Google VR SDK pairing and SteamVR controller forwarding.
- Audio stream and end-to-end latency, Wi-Fi loss, battery and thermal validation on Redmi K40 / Redmi Note 12 Turbo.

## Remote repository status

GitHub branch creation was denied by the current GitHub integration (HTTP 403). The work is prepared in the local source package and has not been pushed to `BH2VSQ/PhoneVR`.
