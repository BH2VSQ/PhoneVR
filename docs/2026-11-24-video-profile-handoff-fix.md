# 2026-11-24: Stream-profile handoff and Android video interruption

## Observed failure

The Host log repeatedly reported:

```text
Unexpected SteamVR video profile 1920x864 stride=7680; expected 1600x720 BGRA
```

The `PVRC` raw-frame header was valid. The mismatch was between two independently retained settings: the SteamVR driver had read the newly selected `high` profile, while the already-running Host process still used its previously loaded `balanced` profile. The Host compared each frame against its cached dimensions and closed the capture TCP connection. The SteamVR capture worker reconnected and retried on subsequent frames, producing the repeated log. Android therefore received no new H.264 access units even though the pose bridge remained active.

## Profile ownership in 0.5.25

- `video-profile.json` is the pending profile selected in the Host UI.
- On every Host service start, the Host reloads this pending profile.
- If the profile differs from the one used for the prior encoder preflight, the Host re-runs the hardware H.264 preflight at the new width, height and frame rate.
- After the active profile is selected, the Host writes `video-profile-active.json`.
- The SteamVR driver reads only `video-profile-active.json`. Saving a new pending setting while Host remains running therefore cannot change the dimensions used by a newly restarted driver.

## Mismatch handling

The Host still rejects frames whose width, height or BGRA stride does not match the negotiated active profile. It drains each complete mismatched payload rather than closing the TCP connection on every frame. A profile mismatch is logged once with the required restart sequence.

## Apply sequence

1. Save the desired profile in the PhoneVR Host **Stream Resolution** dialog.
2. Exit SteamVR completely.
3. Stop and restart the PhoneVR Host service. This reloads the pending profile, re-probes the hardware encoder when needed, and publishes the active marker.
4. Start SteamVR. The driver now reads the active marker written by the Host.
5. Confirm the Host startup log reports the expected active SBS profile and that the first raw capture frames match that size.

## Validation boundary

Go unit tests, race tests, `go vet`, Windows AMD64 Host cross-build and Python release checks cover the Host-side profile handoff and frame-mismatch handling. The SteamVR MSVC driver build and physical Host/SteamVR/Android resolution-switch test still need to be performed on the target Windows PC and phone.
