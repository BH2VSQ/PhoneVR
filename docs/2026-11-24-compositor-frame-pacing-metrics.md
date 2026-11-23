# 2026-11-24 SteamVR compositor frame-pacing diagnostics

## Scope

PhoneVR 0.5.23 adds observation-only counters to the SteamVR virtual-display driver. It does not alter the 72 Hz virtual display clock, 60 FPS video capture ceiling, pose prediction, D3D11 capture queue, H.264 bitrate or Android decoder scheduling.

The new values are included in the existing five-second `PhoneVR: virtual display` line:

- `max-Present-gap`: longest interval between two `IVRVirtualDisplay::Present` callbacks in the current diagnostic window.
- `gaps>20.8ms`: count of callback intervals longer than 1.5 nominal refresh intervals at 72 Hz.
- `gaps>41.7ms`: count of callback intervals longer than 3 nominal refresh intervals at 72 Hz.
- `pending-overwrite`: number of times a new `Present` arrived before the preceding pending callback was consumed by `WaitForPresent`.
- `VSync-overshoot[max=...]`: maximum difference between the scheduled virtual VSync target and the actual time `WaitForPresent` resumed. The two counters count overshoots above 1 ms and 5 ms.
- `missed-ticks`: number of virtual VSync slots skipped when the scheduler had to advance its reserved clock to a later interval.
- `keyed-mutex-fail`: count of failed non-blocking acquisitions of the compositor backbuffer's `IDXGIKeyedMutex`.

## Reading a reproduction

Collect at least 30 seconds of the SteamVR driver log while repeatedly turning the head in a fixed scene. Keep the matching Host and Android logs from the same interval.

- High `gaps>20.8ms` or `gaps>41.7ms`, with low VSync overshoot, means the driver is receiving irregular or delayed `Present` callbacks. Compare SteamVR compositor frame timing and GPU frame time.
- Stable `Present` intervals but repeated VSync overshoot, especially above 5 ms, points to late wakeups in the virtual display wait path or Windows scheduling pressure.
- Non-zero `pending-overwrite` means the callback sequence is not being consumed one-for-one. Compare `Present`, `WaitForPresent` and `completed` rates before changing scheduling.
- Non-zero `keyed-mutex-fail` means the shared compositor backbuffer could not be acquired immediately for those frames. Those video captures are skipped. The counter does not establish whether the runtime or driver owns the underlying contention.
- If these values remain healthy while Host H.264 FPS or Android decode FPS drops, continue investigating the relevant downstream video stage instead of changing the virtual display clock.

## Separate image-quality constraint

The current HMD display component still advertises 800x720 pixels per eye, and the Host/Android profile remains 1600x720 SBS. Fine text is therefore limited by the source render resolution before H.264 compression. Raising the bitrate alone cannot restore detail that was never rendered. A higher per-eye resolution requires coordinated changes to the OpenVR render target, D3D11 capture resources, Host profile negotiation, H.264 encoder preflight, Android decoder capability checks and the headset's display pipeline. That work is intentionally separate from this diagnostic release.

## Capture guidance

1. Run the same game scene for 30-60 seconds with SteamVR Home disabled, and capture logs.
2. Repeat with SteamVR Home enabled without changing game, PC power mode, video transport or headset refresh settings.
3. Reproduce the head-turn flash during both runs and note its local timestamp to the nearest second.
4. Compare the new driver counters with SteamVR `vrcompositor` frame timing, Host `Video pipeline` FPS/drop counters and Android `PhoneVRVideo` receive/decode counters.
