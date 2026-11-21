# PhoneVR 0.4.7 Build Status

## P0 changes

- Replaced the hard-coded SteamVR disconnected label with a live loopback health handshake from the OpenVR server driver. The Host now separately tracks driver loaded, HMD activated, valid pose, virtual display activated, and completed full-frame staging-copy synchronization.
- Added a 16-byte versioned `PVST` heartbeat on loopback UDP `127.0.0.1:48105`, sent every 250 ms from the driver's `RunFrame()`. Host expires the state after 2.5 seconds. Port `48104` remains reserved for future encoded video transport.
- Added Host integration and parser tests for valid status flags, frame counter, malformed packets and stale-heartbeat expiry.
- Implemented D3D11 virtual-display initialization on a hardware DXGI adapter and published its adapter LUID to SteamVR.
- Implemented `IVRVirtualDisplay::Present` shared-texture opening/caching, bounded keyed-mutex acquisition, full-frame copy into a CPU-readable staging texture and non-blocking frame queueing.
- Implemented `WaitForPresent` GPU-copy completion using a small 1x1 synchronization staging texture queued after the full-frame staging copy. Only the synchronization texture is mapped, avoiding full-frame CPU readback during P0. `GetTimeSinceLastVsync` derives time-since-vsync and a monotonic frame counter from the 72 Hz virtual display clock and supplied compositor timestamp.
- Improved the main Host SteamVR pill so it distinguishes driver disconnected, HMD inactive, display initialization error, waiting for a frame, waiting for pose, and healthy display/tracking.
- Kept the Android LAN protocol unchanged. No encoded video is sent to Android in P0; encoder-owned pixel buffering, hardware encoding and network video transport remain P1.

## Verified in this environment

- `go test ./...`
- `go test -race ./...`
- `go vet ./...`
- Windows AMD64 Go Vet and test-binary cross-compilation
- Windows AMD64 GUI and console Host cross-compilation (embedded-driver directory contains the tracked placeholder; real DLL embedding happens in the Windows build script)
- Python release tooling unit tests
- GitHub Actions workflow YAML parsing
- ZIP integrity and SHA-256 verification

## Requires Windows build and SteamVR runtime validation

- Compile `steamvr-driver/` with the pinned OpenVR SDK, Windows SDK, CMake and Visual Studio C++ x64 toolchain. The current Linux environment has no MSVC/Windows SDK, so the new native D3D11 code has not been compiled here.
- On Windows, launch SteamVR and confirm the Host changes through each live heartbeat state: driver loaded, HMD active, pose valid, display redirect active and completed frame counter increasing.
- Verify `OpenSharedResource`, keyed-mutex acquisition, full-frame staging copy, synchronization-texture Map / Unmap and `WaitForPresent` on the actual graphics adapter. GPU-driver and SteamVR-version behavior cannot be fully established by source-level checks.
- Confirm SteamVR VR View remains stable during a sustained session and inspect `vrserver.txt` for any D3D11 initialization, shared-texture, keyed-mutex or Map errors.
- Confirm the Host top-bar status changes to disconnected within 2.5 seconds after SteamVR closes.

P0 has source implementation and automated Host-side tests, but final runtime acceptance still requires the Windows/SteamVR checks above. P1 Android video streaming and encoder integration are not part of this package.
