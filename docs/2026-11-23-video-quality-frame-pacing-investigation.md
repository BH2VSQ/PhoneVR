# 2026-11-23 Video quality and frame-pacing investigation

## Confirmed source configuration in 0.5.22

- The SteamVR virtual display clock is fixed at 72 Hz (`gDisplayRefreshRateHz`).
- The driver captures at a maximum of 60 FPS (`kVideoFrameRate`). Host and Android negotiate the same 1600x720@60 H.264 profile.
- The H.264 encoder bitrate is currently fixed at 8 Mbps for NVENC, AMF, QSV and the software fallback argument builder. The live VR path still selects hardware encoders only.
- The Android optical renderer's blur setting defaults to zero. Built-in Daydream presets use a small sharpening value of 0.15.
- Capture is initiated from the SteamVR driver's `Present` callback and is rate-limited to 60 FPS. The driver already reports Present, WaitForPresent and completed-frame rates, plus capture/readback counters.

## Interpretation

The 72 Hz virtual display clock and 60 FPS video stream have a cadence mismatch. A 60 FPS stream cannot provide a new image for every 72 Hz display refresh, so some video frames are held for an additional display refresh. This can be perceived as judder during head or scene motion. It does not by itself prove the source of the user's reported low FPS.

The fixed 8 Mbps budget may reduce fine detail in high-motion scenes at 1600x720@60. Increasing bitrate can improve compression detail when the encoder and Wi-Fi link have headroom, but it cannot repair a low capture rate or a slow decoder. The current blur parameter is zero by default, so temporal trails need to be distinguished from shader blur, repeated frames, panel persistence and motion-compensated/compressed image artifacts.

The user's observation that performance changes when SteamVR Home is enabled makes compositor pacing a priority for measurement. The driver's capture path is driven by compositor `Present` callbacks, so a change in Present cadence can directly change the number of captured video frames.

## Required A/B diagnostics

Run two 60-second captures under the same PC power mode, Wi-Fi conditions, headset display mode and scene workload: one with SteamVR Home disabled and one enabled. Preserve the full logs for each run.

1. SteamVR `vrserver.txt`: compare `Present=`, `WaitForPresent=`, `completed=`, `max-Present`, `max-WaitForPresent`, `gpu-map-pending`, `gpu-map-fail`, capture skipped/readback counters and TCP bridge send failures.
2. Windows Host log: compare `raw RX FPS`, encoder submitted FPS, H.264 access-unit FPS, UDP fragments/s, raw queue drops, encoded queue drops, encoder backpressure and encoder name.
3. Android logcat: compare `Video state`, RX FPS, decode FPS, fragments/s, expired assemblies, queue drops, decoder input waits, decoder recoveries, packet age, complete-frame age and decoded-frame age.

Suggested Android collection command:

```powershell
adb logcat -c
adb logcat -v time | Select-String "PhoneVRVideo|PhoneVR|MediaCodec" | Tee-Object .\PhoneVR-video-ab.log
```

## Stage diagnosis

- Low driver Present/completed FPS and low Host raw RX FPS: investigate SteamVR compositor scheduling and virtual-vsync behavior.
- Healthy Present FPS but low readback FPS or growing `gpu-map-pending`: investigate D3D11 staging readback and GPU contention.
- Healthy raw RX/submitted FPS but low H.264 FPS: investigate the selected hardware encoder, GPU adapter selection, QSV/NVENC/AMF throughput and encoder backpressure.
- Healthy H.264 FPS and UDP send rate but low Android RX FPS or high expired assemblies: investigate Wi-Fi loss, AP airtime, UDP receive buffering and fragment repair.
- Healthy Android RX FPS but low decode FPS or rising decoder waits/recoveries: investigate the selected MediaCodec decoder and its scheduling.
- Healthy decode FPS with visible temporal trails: inspect actual display refresh/persistence, repeated presentation cadence, optical settings and H.264 motion artifacts separately.

## Scope of 0.5.22

This revision fixes the release test's whitespace-sensitive assertion. It does not change runtime FPS, bitrate, encoder presets, decoder scheduling or compositor timing. Those changes should follow the A/B measurements above so a higher target rate or bitrate does not overload a stage that is already behind.

## Follow-up in 0.5.23

The driver now reports maximum Present-to-Present interval, long Present gaps, pending-Present overwrites, virtual-VSync wake overshoot, skipped virtual ticks and keyed-mutex acquisition failures. See `2026-11-24-compositor-frame-pacing-metrics.md` for interpretation. These counters are diagnostic only and do not change scheduling.
