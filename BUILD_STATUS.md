# PhoneVR 0.4.9 Build Status

## 0.4.9 SteamVR flicker and load investigation

- Removed the unused full-resolution staging texture and `CopyResource` operation from the virtual display Present path. The current P0 milestone does not consume that frame data; copying the full 2400x1080 buffer every present was unnecessary GPU/memory traffic.
- Retained a small 32x32 CPU-readable staging texture and a 1x1 copy solely as a GPU-completion fence, mapped in `WaitForPresent`.
- Increased the bounded keyed-mutex wait from 2 ms to 10 ms, aligned with Valve's virtual-display sample, to reduce unnecessary frame drops while retaining a finite timeout.
- Added logs for every enumerated DXGI adapter, selected adapter LUID, opened compositor backbuffer size/format/sample count, keyed-mutex acquisition failures, and D3D11 Map failure/device-removed reason. Failure logs are rate-limited.
- Map failures now clear the pending synchronization flag rather than leaving the driver in a stale pending state.
- This change reduces avoidable D3D11 copy overhead and improves diagnosis. It does not establish that the user's intermittent black/gray view is fully fixed; actual Windows/SteamVR validation is still required.
- Steam Client WebHelper CPU use is not directly controlled by PhoneVR's D3D11 driver code. It may be related to Steam's Chromium-based dashboard/overlay content or another Steam UI component; the process name and CPU percentage alone do not prove causation.

## Earlier P0 implementation retained

- Host connection status uses a loopback `PVST` heartbeat from the OpenVR driver and tracks driver loaded, HMD active, pose valid, display active and completed virtual-display frames.
- The status heartbeat is sent every 250 ms to `127.0.0.1:48105`; Host state expires after 2.5 seconds.
- `IVRVirtualDisplay::Present` opens/caches the D3D11 shared compositor backbuffer. `WaitForPresent` waits for the queued small GPU fence readback. `GetTimeSinceLastVsync` returns a monotonic virtual-vsync count at the configured 72 Hz refresh rate.
- Android video encoding, network transmission, MediaCodec decode and live SBS display remain P1 and are not included in this package.

## Validation in this environment

- `go test ./...`
- `go test -race ./...`
- `go vet ./...`
- Windows AMD64 Go Vet and test-binary cross-compilation
- Windows AMD64 GUI and console Host cross-compilation (embedded-driver directory contains the tracked placeholder; real DLL embedding happens in the Windows build script)
- Python release tooling unit tests
- GitHub Actions workflow YAML parsing
- ZIP integrity and SHA-256 verification


## Requires Windows build and SteamVR runtime validation

- Build the native driver with the pinned OpenVR SDK, Windows SDK and Visual Studio C++ x64 toolchain. This Linux environment does not have MSVC/Windows SDK, so the updated D3D11 driver cannot be natively compiled here.
- Run SteamVR and inspect `vrserver.txt` for the enumerated adapter list, selected LUID, shared backbuffer dimensions/format, keyed-mutex timeout counts and GPU Map/device-removal errors.
- Confirm Host's completed-frame counter increases continuously during a sustained SteamVR session, and verify the VR View remains stable.
- Compare the logged adapter LUID with the GPU used by `vrcompositor.exe` in Task Manager/GPU Engine or SteamVR logs. A mismatch would require adapter selection to be corrected for the user's multi-GPU configuration.
- Confirm Steam Client WebHelper CPU usage with SteamVR dashboard/overlay closed and open separately; do not assume it is caused by the PhoneVR driver without that comparison.

P0 source changes are included. Final acceptance requires the user's Windows/SteamVR hardware validation. P1 video streaming remains unimplemented.
