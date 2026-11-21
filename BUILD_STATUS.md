# Build status

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
