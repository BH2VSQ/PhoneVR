# Build status

## 0.4.24 DisplayRedirect activation ordering

- Root cause identified in the previous provider initialization order: `PHONEVR-HMD-001` was registered before `PHONEVR-DISPLAY-001`. `TrackedDeviceAdded` schedules device activation, while OpenVR only makes `k_ulDisplayRedirectContainer` valid after the DisplayRedirect device's `Activate()` has run. Thus the HMD could initialize its display properties before the redirect overrides existed.
- Provider `Init()` now queues only the DisplayRedirect device. It waits for the device's `Activate()` to finish (including both HMD override writes) before queuing the HMD from `RunFrame()`. This avoids relying on `TrackedDeviceAdded()` queue order alone. If DisplayRedirect activation fails or does not finish within 5 seconds, the HMD still registers for pose-only operation and logs that video redirection is unavailable; phone tracking is not blocked by the video path.
- HMD `Activate()` reads back the effective merged `HasVirtualDisplayComponent` and graphics adapter LUID values and logs both values and property read errors. This turns the ordering assumption into an explicit runtime check.
- Regression tests require the DisplayRedirect registration statement to precede HMD registration and verify the HMD readback diagnostics.
- **Runtime confirmation still required:** after rebuilding and restarting SteamVR, confirm the HMD readback says `HasVirtualDisplay=true`, then `IVRVirtualDisplay_002` is queried, `Present` increases, and the Host gets its first BGRA frame. If the readback is false, the property override is not being applied; if true but Present remains zero, inspect SteamVR display redirection / compositor routing next.

## 0.4.23 DisplayRedirect-to-HMD property association

- Fixed a missing OpenVR property bridge in `PhoneVRDisplayRedirect::Activate()`. The driver previously wrote `Prop_GraphicsAdapterLuid_Uint64` only to the DisplayRedirect accessory's own container.
- The DisplayRedirect now writes the graphics adapter LUID and `Prop_HasVirtualDisplayComponent_Bool=true` into `vr::k_ulDisplayRedirectContainer`, which the OpenVR SDK defines as the property override container surfaced through the active HMD's property container.
- Accessory-local LUID metadata remains for diagnostics; logs report both property-write results and warn if either HMD override fails.
- Added static regression tests for the manifest and required property overrides.
- This addresses the missing association suspected from the prior `Present`-never-called / no-raw-frame behavior. It is not yet confirmed until the target SteamVR runtime reports `Present` callbacks and the Host receives a raw BGRA frame.

## Video bridge diagnostics and Wi-Fi-only scope

- USB tether/RNDIS selection and its firewall helper are deferred; the Android connection UI now offers Auto and Wi-Fi discovery only.
- Focus is on diagnosing the SteamVR virtual-display Present callback, D3D11 backbuffer capture, Host raw-frame bridge, H.264 encoder, UDP transport and Android MediaCodec output as separate stages.
- Host encoder preflight and Android handshake deadlines are extended so hardware encoder probing cannot silently exceed the client's handshake timeout.
- The HMD continues to provide `IVRDisplayComponent` for display geometry/distortion; the separate `TrackedDeviceClass_DisplayRedirect` remains the `IVRVirtualDisplay` endpoint for final compositor backbuffers.
- Capture supports common SDR/HDR compositor formats where D3D11 shader sampling is supported, including resolving multisampled textures before downscaling.
- The capture worker no longer holds the shared D3D resource lock while sending a full raw frame over the local TCP bridge.
- D3D11 capture-resource readiness is now asserted only after all capture resources are successfully created (and on the already-created fast path); the Host status therefore no longer remains falsely stuck at "waiting for GPU capture resources".
- The staging readback worker retries `DXGI_ERROR_WAS_STILL_DRAWING` for a bounded interval without holding the capture resource mutex while sleeping, instead of dropping the first nonblocking Map result.
- SteamVR logs separate captured, successful readback and sent BGRA frame rates, plus Map retries and skipped frames.
- Android `VideoStreamReceiver` now reports its stages through the TCP control session; Host status distinguishes receiver startup, missing UDP packets, unexpected packet sources, incomplete fragment assembly, decoder startup/failure, decoded output and the first frame consumed by the stereo renderer.

## Validation limitations

- Go tests, Race, Vet and Windows AMD64 Host cross-compilation are run in this environment.
- Native MSVC D3D11 driver compilation, Windows SteamVR compositor integration, hardware H.264 encoder initialization, Android Gradle APK packaging, MediaCodec playback and Wi-Fi end-to-end streaming require the target Windows PC and Android handset.


## 0.4.22 virtual-display Present bootstrap and mode correction

- The 0.4.21 driver told SteamVR `IsDisplayOnDesktop=false` while implementing only `IVRVirtualDisplay` and not `IVRDriverDirectModeComponent`. Valve's virtual_display sample uses Extended Display Mode. The HMD now reports `IsDisplayOnDesktop=true`; Direct Mode must remain disabled for this implementation.
- The Host heartbeat now exposes whether SteamVR queried `IVRVirtualDisplay_002`, whether VSync timing was queried, and whether Present was called. This separates driver component discovery from compositor submission.
- `GetTimeSinceLastVsync` is seeded at activation so initial timing queries are valid before the first Present.
- Added diagnostics for invalid Present arguments and duplicate `VirtualDisplayComponentQueried` method cleanup.
- Fixed Android control acknowledgements to leave the UI thread (including remote actions such as `center_view`) to avoid `NetworkOnMainThreadException`.
- Requires Windows MSVC/SteamVR runtime validation; do not claim Present or live video is fixed until Host reports Present calls and the first raw frame.
