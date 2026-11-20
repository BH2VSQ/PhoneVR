# PhoneVR HMD model coordinate system

## Geometry and orientation

- Compact HMD body only. No head strap or rear harness geometry.
- Smooth uninterrupted front cover at local -Z, with a centered PhoneVR emblem. No optical elements on the front face.
- Two optical assemblies are mounted on the wearer-facing rear side at local +Z.
- +X: headset right when worn; +Y: headset up; -Z: forward gaze direction through the front cover; +Z: wearer / eye side.
- The gaze ray starts between the rear optical assemblies and points along local -Z.

## Materials

The outer shell uses a light graphite-silver finish with a dark front cover and cyan emblem. Rear optical barrels are dark graphite with silver retaining rings and blue-black lens glass. This palette is intentionally high contrast against the native Host window's dark navy background.

## Runtime pose mapping

Android sends the reference-relative device-to-world quaternion. The native OpenGL renderer applies it directly to the model transform. The local XYZ triad and gaze ray share the same transform. The floor grid remains world-aligned. A physical left/right yaw test is still required on the target phone to verify mounting alignment.
