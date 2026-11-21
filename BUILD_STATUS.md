# PhoneVR 0.4.11 Build Status

## 0.4.11 MSVC Chaperone JSON literal build fix

- Shortened the embedded Chaperone JSON raw-string delimiter to `PVCHAP` (6 characters), within the C++ maximum of 16 characters.
- Updated the Chaperone parser regression test to assert the standard raw-string delimiter length limit.
- Fixed the specific MSVC C3512 error reported when building with Visual Studio 18 2026. The source change is verified by automated tests; a native Windows/MSVC driver build still needs to be run on the target toolchain.

## 0.4.10 Driver-provided Chaperone and stable universe

- Added OpenVR v5 `chaperone_info` JSON on the HMD property container and published the matching stable `Prop_CurrentUniverseId_Uint64` value.
- Chaperone provides neutral seated and standing transforms, an empty collision-bounds array, and a 1 x 1 metadata play area. The empty bounds are deliberate because the phone currently supplies only 3DoF orientation and cannot provide room-scale safety boundaries.
- Both property writes are checked. If SteamVR rejects either property, the driver logs both error codes and continues HMD activation without the driver Chaperone so pose tracking is not taken down by a setup-metadata failure.
- Added Python regression tests for JSON syntax/schema basics, the shared 32-bit universe ID, origin transforms and empty bounds.
- The configuration is intended to remove PhoneVR's dependency on manually creating a room-scale setup. Whether the SteamVR first-run/room-setup prompt is suppressed must still be verified on the target Windows installation.

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
- Python release tooling and Chaperone regression tests: 9 tests passed
- GitHub Actions workflow YAML parsing
- ZIP integrity and SHA-256 verification


## Requires Windows build and SteamVR runtime validation

- Build the native driver with the pinned OpenVR SDK, Windows SDK and Visual Studio C++ x64 toolchain. This Linux environment does not have MSVC/Windows SDK, so the updated C++ driver cannot be natively compiled here.
- Restart SteamVR and inspect `vrserver.txt` for `PhoneVR: driver-provided Chaperone published`. If it reports a property error, also capture `PhoneVR: continuing HMD activation without driver Chaperone` and include both error numbers in the report.
- Confirm whether the SteamVR Room Setup prompt no longer appears for PhoneVR. SteamVR first-run onboarding may still show independently of the driver's Chaperone properties.
- Verify SteamVR's seated origin behaves as expected. The current configuration is orientation-only, uses neutral transforms and intentionally provides no room-scale boundary; do not use it as a 6DoF or guardian solution.
- Continue checking the previously added DXGI adapter, shared-backbuffer, mutex and GPU synchronization diagnostics if the display remains unstable. Chaperone setup does not repair the separate D3D11 frame-submission path.

P0 source changes are included. Final acceptance requires the user's Windows/SteamVR hardware validation. P1 video streaming remains unimplemented.
