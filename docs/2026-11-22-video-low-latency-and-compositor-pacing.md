# 2026-11-22 Video low-latency and compositor pacing

## Changes

- HMD recommended render target is aligned to the 1600x720 encoded SBS stream (800x720 per eye), removing an unnecessary 2400x1080 render followed by downscaling.
- Exact-profile BGRA backbuffers use a direct copy into the staging ring, bypassing the previous full-size input copy and shader downscale. A scale path remains for other backbuffer dimensions.
- `Present` uses a zero-timeout keyed-mutex acquisition and drops a contended capture frame instead of blocking the compositor for up to 10 ms.
- `WaitForPresent` polls a D3D11 event query instead of blocking in staging-texture `Map`, then paces the synthetic virtual display clock to the advertised 72 Hz refresh. `GetTimeSinceLastVsync` uses a timing-only mutex, independent of D3D resource synchronization.
- The capture worker requeues a not-yet-ready staging slot while still holding the capture-resource lock, preventing a slot-reuse race.
- QSV async depth is reduced from 4 to 2, VBV buffer from 2M to 1M, GOP to half a second, and the unmatched timestamp queue from 24 to 12 frames to constrain latency.

## Verification limitations

Go tests, vet, race checks, and Windows Host cross-compilation are run in the available environment. Native MSVC/Windows SDK driver compilation and 72 Hz timing validation require the target Windows/SteamVR machine. Confirm `Present` approaches 72 Hz and `WaitForPresent` max duration is near or below one 13.89 ms frame interval. Check Android `Video RX` and `Video decode` logs to measure the final phone-side stage.
