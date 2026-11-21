# PhoneVR SteamVR Driver — 0.4.3

Windows x64 OpenVR server driver foundation for the PhoneVR wireless HMD.

## Devices

- `PHONEVR-HMD-001`: `TrackedDeviceClass_HMD`, 3DoF orientation sourced from
  Android `PVSP` packets forwarded by the Go Host.
- `PHONEVR-DISPLAY-001`: `TrackedDeviceClass_DisplayRedirect`, exposing
  `IVRVirtualDisplay` so SteamVR can provide the final composited backbuffer
  for a wireless output path.

The HMD pose is published from the provider's non-blocking `RunFrame()` via
`IVRServerDriverHost::TrackedDevicePoseUpdated`. The OpenVR header marks
`ITrackedDeviceServerDriver::GetPose()` as unused. The UDP pose listener binds
only to `127.0.0.1:48103`; it is not a LAN service.

## Build

Requirements: Windows 10/11 x64, Visual Studio C++ x64 toolchain, CMake 3.24+
and Git. The OpenVR SDK is fetched at the pinned Valve commit listed in
`CMakeLists.txt`.

```powershell
.\scripts\build-steamvr-driver.ps1
```

To use an already downloaded SDK checkout, configure CMake with
`-DOPENVR_SDK_ROOT=<path-to-openvr-checkout>`.

## Register / unregister

After building, the package is under `dist/steamvr-driver`:

```powershell
.\tools\install-steamvr-driver.ps1 -DriverDirectory .\dist\steamvr-driver
.\tools\install-steamvr-driver.ps1 -DriverDirectory .\dist\steamvr-driver -Remove
```

The helper uses SteamVR's `vrpathreg.exe`; SteamVR must be installed.

## Current implementation boundary

`IVRVirtualDisplay::Present` currently records frame ID/timing only and returns
without blocking. It does **not** yet open the D3D11 shared backbuffer handle,
copy/convert the texture, submit it to NVENC/AMF/QSV, or send a video packet.
The HMD projection/distortion values and eye geometry are initial SBS defaults;
final optical-profile handoff from Android is a subsequent integration step.

The driver exposes only the phone's 3DoF orientation. No room-scale position
or 6DoF tracking is synthesized.
