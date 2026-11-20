# Daydream View 2017-inspired HMD model

## Source-based visual details

Google's Daydream View hardware page identifies 2017-specific headset components including the front cover latch, automatic screen-alignment posts, front-cover hook, and facepad. The 2017 design retained the fabric construction and added a removable top strap; for PhoneVR's HMD-only status visual, all strap geometry is intentionally omitted while side attachment mounts remain on the visor body.

The Google Design Team's Good Design entry describes the device as a soft, breathable, multi-layer fabric construction with a plastic frame that maintains lens-to-display distance and a contoured foam face interface. 2017 reviews describe the updated coarser fabric surface and revised front cover/heat-dissipation structure.

References:
- Google Daydream View hardware (2017): https://support.google.com/daydream/answer/7185269?hl=en
- Google Design Team, Daydream View (2017): https://good-design.org/projects/daydream-view/
- 9to5Google, 2017 redesign details (2017-10-04): https://9to5google.com/2017/10/04/daydream-view-announce-features-price/
- Tom's Guide, Daydream View 2017 design review: https://www.tomsguide.com/us/google-daydream-view%2Creview-4960.html

## Model coordinate convention

- +X: wearer's right
- +Y: up
- -Z: outside/front phone-cover face
- +Z: wearer-facing rear face and optical assemblies

## Included geometry

- Rounded textile-covered visor body and front phone-cover flap
- Front cover seam, upper latch/hook and small thermal vent details
- Side strap attachment mounts only; no straps or controller
- Rear rigid optical frame and soft facepad segments
- Two rear lens barrels, lens rims, convex lens surfaces and bridge relief
- Small side emblem detail

The asset is an original visualization model built from public product references. It is not Google's manufacturing CAD and is not intended to establish mechanical fit dimensions.

## Rebuild the model

From the repository root, install `tools/model/requirements.txt` and run:

```powershell
python tools/model/generate_daydream_view_model.py
```

The script writes the OBJ, MTL, GLB and fabric texture into the Windows Host embedded asset directory. The GLB embeds the woven fabric texture; the OBJ references `phonevr_fabric.png` through MTL. The native Windows renderer loads the OBJ/MTL mesh and applies the material diffuse colours. The GLB remains available for external 3D tools.
