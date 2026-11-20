# Daydream View v2.0 (2017) optical calibration

This project targets compatibility with the **physical Daydream View v2.0 (2017) viewer**. It does not currently implement the discontinued Google Daydream Home / VR Services application platform.

## Optical profile used by this build

A 2020 IEEE ISMAR author preprint describes a Google Daydream View v2.0 (2017) experimental setup and reports:

- Lens-to-lens center spacing: **64 mm** in the reported apparatus.
- Daydream View v2.0 (2017) default viewer-profile radial coefficients: **K1 = 0.4331**, **K2 = -0.085**.
- The profile mapping is described as `r = rd + k1 * rd^3 + k2 * rd^5`, where `rd` is a radius in the distorted image and `r` is the corresponding radius in the original image.

The renderer follows that distorted-screen-to-source-image mapping. The 64 mm value is used as a starting point for the physical lens-spacing control. It should be verified on the actual headset. The paper's experimental field-of-view setup should not be treated as a universal Android projection FOV because screen geometry, eye relief and per-eye projection conventions vary.

## Separate values that must not be confused

- **User IPD**: distance between the user's pupils. It controls the separation of the virtual left/right eye cameras.
- **Viewer lens separation**: distance between the centers of the two physical lenses. It determines where the left/right lens centers fall within the two half-screen viewports.
- **Screen-to-lens distance**: distance from the phone's active display surface to each lens optical center. It influences the visible field of view.
- **Tray-edge-to-lens-center distance**: used when the phone is seated against the top or bottom edge of the viewer to calculate the vertical optical center.
- **Lens distortion K1/K2**: radial pre-warp profile. It does not change focus distance or correct refractive vision.

## Calibration procedure

1. Install the phone in the Daydream View with the final case, tray and insert orientation that will be used while playing.
2. Apply **Google Daydream View 2017** in the Android display calibration panel. This loads K1/K2 and sets the lens-spacing starting value to 64 mm.
3. Measure the center-to-center distance between the two physical lenses with a ruler or caliper. Enter it under **Viewer lens separation**. Do not use the user's IPD for this field.
4. Measure from the phone's active display surface to the lens optical center and enter **Screen-to-lens distance**. The initial 40 mm value is only a starting value, not a claimed Daydream factory dimension.
5. Select whether the phone screen is vertically aligned to the viewer's top edge, bottom edge or center. For top/bottom alignment, measure the distance from that seating edge to the lens center and enter it as **Tray edge to lens center**.
6. Use **Estimate vertical FOV** as a starting point. It uses the Android-reported physical display DPI, which can be inaccurate on some phones. Fine-tune FOV while viewing through the lenses.
7. Adjust **User IPD** until the left/right center targets fuse comfortably without eye strain. Then use lens center X/Y fine adjustment to align each optical center.
8. Inspect straight grid lines and right angles through each eye separately, then with both eyes open. Tune K1/K2 only after physical lens spacing and optical-center placement are correct.
9. Recenter the headset while looking straight ahead. Verify yaw, pitch and roll follow the actual headset motion.

Google's viewer-measurement guidance specifically calls for measuring screen-to-lens distance, inter-lens distance, vertical alignment and tray-edge-to-lens-center distance. It also recommends checking that lens-center markers appear sharp and vertically centered.

## Platform compatibility boundaries

- This build does not request NFC permission. Google documents NFC viewer pairing as a Daydream platform function that applications cannot use directly.
- This build does not declare a Daydream launcher category or `android:enableVrMode`, and does not bundle the Google VR SDK. Therefore, it should be described as a phone VR streaming application with a Daydream View optical profile, not as a certified/native Daydream platform application.
- The Daydream View 2017 headset includes a 3DoF controller, but this project does not yet receive or forward Daydream controller input to the PC. Controller integration is a separate future feature.
- Google has ended Daydream VR software support and warns that the Daydream app may not work correctly on Android 11 or later. Physical viewer optics and the headset itself can still be used with compatible standalone applications.

## References

- Allison, Wilcox and Tong, *Optical distortions in VR bias the perceived slant of moving surfaces*, author preprint of IEEE ISMAR 2020. The apparatus section describes Daydream View v2.0 (2017), 64 mm lens spacing, and the default profile's K1/K2 coefficients and mapping: https://percept.eecs.yorku.ca/papers/ISMAR_2020_VGTC_format.pdf
- Google Cardboard Manufacturer Help, *Enter physical viewer parameters*: https://support.google.com/cardboard/manufacturers/answer/6324808
- Google VR Android reference, `GvrViewerParams`: https://developers.google.com/vr/reference/android/com/google/vr/sdk/base/GvrViewerParams
- Google VR Daydream App Quality Requirements: https://developers.google.com/vr/distribute/daydream/functionality-requirements
- Google Daydream service update: https://support.google.com/daydream/answer/7185096
