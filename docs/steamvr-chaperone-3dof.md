# PhoneVR 3DoF Chaperone configuration

PhoneVR currently reports orientation only. It does not report tracked position, floor height, room geometry, or a physical play-space perimeter. The driver therefore supplies a driver-owned OpenVR v5 Chaperone configuration so SteamVR has an explicit tracking universe without requiring PhoneVR users to create a fictitious room-scale setup.

## Runtime properties

When `PhoneVRHmd::Activate()` runs, it writes these HMD properties:

- `Prop_CurrentUniverseId_Uint64`: stable ID `0x50485652` (`PHVR`).
- `Prop_DriverProvidedChaperoneJson_String`: the matching JSON with `json_id = chaperone_info`, `version = 5`, and `universeID = 1346917970`.

The numeric JSON `universeID` is 32-bit, while the OpenVR device property is 64-bit. Both intentionally carry the same numeric value. The JSON timestamp is ISO-8601 UTC.

## Coordinate policy

Both `seated` and `standing` currently use identity translation and zero yaw. This is a neutral orientation-only reference because the incoming PhoneVR pose has no positional component. `collision_bounds` is empty; the `[1, 1]` `play_area` value is structural metadata only and is not a claim that a measured one-meter room exists.

This configuration does not create 6DoF tracking, estimate the floor, provide guardian collision protection, or make room-scale experiences safe. Seated experiences are the intended initial use case.

## Diagnostics

After restarting SteamVR, inspect `vrserver.txt` for one of these messages:

- `PhoneVR: driver-provided Chaperone published ...` — both OpenVR property writes succeeded.
- `PhoneVR: failed to publish driver Chaperone ...` — one or both property writes failed; the numbers in the log are `ETrackedPropertyError` values.

The property-write success message proves the driver submitted the configuration to OpenVR. It does not prove that SteamVR accepted the JSON semantically or that its setup wizard will never be shown by first-run onboarding. Confirm on the target SteamVR installation.
