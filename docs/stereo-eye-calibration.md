# PhoneVR stereo eye routing and headset calibration (0.5.19)

## Purpose

This procedure separates three questions that were previously difficult to distinguish: which SBS source half reaches each physical display half, whether the two physical eye images can be fused, and whether the viewer's lens post-processing changes that alignment. The built-in patterns are rendered locally by the Android client and do not require the Windows Host or a working network video stream.

## Controls

Open **菜单 → 光学预设 → 双目诊断**. The diagnostic modes are:

- **双眼同图**: both source eyes receive an identical grid, crosshair and concentric-ring pattern. Use this to check that the two views can fuse without stereo disparity.
- **左右眼标识**: source-left is red and carries an `L` glyph; source-right is blue and carries an `R` glyph. View each physical lens separately to identify the actual source mapping.
- **仅显示左眼** / **仅显示右眼**: one SBS source half contains the chart and the other is black. Use these to verify eye routing without relying on colors or text.
- **立体视差**: the central target uses opposite horizontal offsets in the two source eyes, creating crossed disparity for a near target. The grid and rings remain at zero disparity.
- **恢复实时视频**: leaves diagnostic mode and returns to the decoded SBS stream.

The **测试图镜片畸变** control is off by default. In that state the calibration patterns bypass the current K1/K2 warp, blur and sharpening so source-eye routing and image offsets can be tested without lens post-processing. Enabling it applies the active preset's current post-processing to the diagnostic patterns. It does not change the coefficients or claim to validate their physical correctness.

## Source-eye routing

**左右眼源交换** is a persistent optical-preset setting. It exchanges which SBS source half is sampled by each physical display eye. The physical display eye still selects its own lens center and per-eye offset. This distinction matters because swapping SBS sources must not also swap the physical lens centers.

With source-eye swap disabled, the expected mapping is: physical left display half samples SBS left; physical right display half samples SBS right. If the L/R identifier pattern shows the opposite mapping through the headset, enable source-eye swap and repeat the left-only/right-only checks.

## Per-eye image offsets

Four controls adjust the source image independently for each physical display eye:

- Left eye horizontal / vertical
- Right eye horizontal / vertical

Positive values move the displayed source image right/down. The range is ±0.20 normalized units of one eye image in 0.5.19 (expanded from ±0.08). These are image-sampling translations, not changes to the viewer lens spacing or the user's IPD. Offsets are stored in the active optical preset and are included in display-settings JSON. The Windows Host optical preset editor exposes all four values and provides **Apply Position Live**, allowing adjustments while the headset is worn without saving the preset after every change.

The built-in Daydream View 2016, Daydream View 2017 and Generic Cardboard presets start with these per-eye offsets: left horizontal `+0.09`, left vertical `+0.09`, right horizontal `-0.09`, and right vertical `+0.09`. These are normalized translations within each eye image. First confirm the L/R labels and single-eye routing, then use **双眼同图** to compare the center crosshair and rings. Adjust only the eye that visibly needs correction. The **重置左右眼偏移** action remains available and returns all four values to zero.

## Vertical image tilt (0.5.19)

The **画面上下倾斜** control applies a vertical perspective transform around the horizontal image axis. It can compensate for the image plane appearing too high or low across the field of view while preserving independent left/right translation. The supported range is -20 to +20 degrees. Begin near zero and adjust in small increments; large values can compress one edge, move content outside the eye viewport, or cause discomfort. The value is persisted with the active optical preset and is also available in the Windows Host editor.

In the Windows Host optical preset editor, edit the four per-eye offsets and vertical tilt, then select **Apply Position Live** to send the current fields to the connected phone immediately. Use **Save & Apply** when the settings should also be saved as a persistent optical preset.

## Recommended test order

1. Select **左右眼标识** with source-eye swap off and test distortion off.
2. Verify the labels through the physical left and right lens separately. Toggle source-eye swap only if the mapping is reversed.
3. Select each single-eye mode and confirm the other eye is black.
4. Select **双眼同图** and check fusion with the grid and rings. Adjust per-eye offsets only after eye routing is correct.
5. Select **立体视差** and confirm the near target fuses in front of the zero-disparity grid. Stop if the image causes discomfort.
6. Enable test distortion and compare center/edge geometry with the raw chart.
7. Select **恢复实时视频** before resuming streaming.

## Interpretation boundaries

- If L/R labels are reversed but the single-eye modes are otherwise isolated, source-eye swapping is the first setting to test.
- If only one physical eye receives the wrong or missing half after toggling source-eye mapping, collect a screenshot of the calibration UI state and a description of which physical eye sees each label.
- If both eyes show the correct labels but the identical chart does not fuse, inspect physical lens centers, phone seating, display-to-lens distance and per-eye offsets.
- If the raw chart fuses but the distortion-enabled chart does not, the remaining issue is in the optical warp or the assumed viewer geometry.
- If the built-in chart works but live video remains reversed, the problem is upstream of the optical post-process, such as the Host/SteamVR SBS source ordering or the decoded texture orientation.

The 0.5.18 changes add routing controls and diagnostic tools. They do not yet replace the current radial distortion shader with a fully measured Daydream View 2017 lens model, nor do they change Host UDP pacing, encoder frame cadence or SteamVR projection parameters.
