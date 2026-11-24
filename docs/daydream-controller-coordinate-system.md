# Daydream Controller Coordinate System

## Coordinate convention

Google Daydream Start Space and Controller Space are right-handed:

- +X points right.
- +Y points up.
- +Z points backward.
- The controller's physical forward direction is local -Z.

OpenVR uses the same right-handed convention for `DriverPose_t`: +X right, +Y up and +Z backward. Therefore, the Daydream orientation quaternion is already in the correct basis for OpenVR and must be normalized without reflecting or negating its X/Y components.

## Rotation checks

The driver regression test checks the physical forward vector `q * (0, 0, -1)`:

- Positive rotation around Y turns the forward vector toward -X.
- Positive rotation around X points the forward vector toward +Y.
- Positive rotation around Z banks the controller while preserving its forward axis.

These checks are deliberately independent of Euler-angle conversion. The runtime path carries the quaternion directly to OpenVR.

## Startup and recenter

Daydream Start Space is defined relative to the headset space when controller tracking starts. PhoneVR therefore preserves the raw quaternion on first connection. A user-triggered App/Home long-press applies an explicit alignment offset between the current controller orientation and the current HMD orientation. Startup no longer silently replaces the initial controller orientation with the HMD forward direction.

## SteamVR controller models

The two tracked devices retain distinct left/right role hints and the `controller_handed` input profile. They now set `Prop_RenderModelName_String` to SteamVR's native `oculus_quest2_controller_left` and `oculus_quest2_controller_right` render models. The legacy left/right pose bindings use `/pose/tip`; `/pose/raw` remains available for applications that need the unmodified device pose.

The Quest 2 render models are visual controller representations. They do not imply Quest hardware tracking or additional input capabilities. PhoneVR continues to expose Daydream's actual touchpad and button inputs and 3DoF orientation.
