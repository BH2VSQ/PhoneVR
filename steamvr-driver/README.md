# PhoneVR SteamVR Driver — 0.4.10

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
only to `127.0.0.1:48103`; it is not a LAN service. The provider also sends a
250 ms loopback-only `PVST` health heartbeat to Host UDP `127.0.0.1:48105`.
Host considers the driver connected only while these heartbeats are fresh.

## Build

Requirements: Windows 10/11 x64, Visual Studio C++ x64 toolchain, CMake 3.24+
and Git. The OpenVR SDK is fetched at the pinned Valve commit listed in
`CMakeLists.txt`.

```powershell
.\scripts\build-steamvr-driver.ps1
```

To use an already downloaded SDK checkout, configure CMake with
`-DOPENVR_SDK_ROOT=<path-to-openvr-checkout>`.

## Deployment through PhoneVR Host

The normal Windows Host build compiles this driver and embeds the complete package into `PhoneVRHost.exe` and `PhoneVRHost-console.exe`. At runtime, the Host extracts the embedded files into a content-versioned directory under `%LOCALAPPDATA%\PhoneVR\SteamVRDriver` and registers that directory with SteamVR's `vrpathreg.exe`.

The in-app **SteamVR Driver** manager supports status refresh, install/repair, and uninstall with persistent opt-out from automatic deployment. The Host only deletes driver files from its own managed directory. A manually managed path is unregistered but left untouched.

To build a standalone development package without building the Host, run:

```powershell
.\scripts\build-steamvr-driver.ps1
```

The output is `dist/steamvr-driver`. The `tools/install-steamvr-driver.ps1` helper remains available for manual developer testing, but it is not part of the normal user installation flow.

## Current implementation boundary

`IVRVirtualDisplay::Present` opens and caches SteamVR's D3D11 shared backbuffer,
uses a bounded keyed-mutex acquire (10 ms maximum), and queues only a small
staging-texture copy as a GPU-completion fence. `WaitForPresent` maps this small
synchronization texture. Because P0 has no encoder or frame consumer, it
intentionally does not copy the unused full-resolution image into staging
memory. `GetTimeSinceLastVsync` advances a 72 Hz virtual-vsync clock from the
last supplied compositor timestamp and keeps its frame counter monotonic. The
driver logs all enumerated DXGI adapters, the selected adapter LUID, opened
backbuffer dimensions/format, mutex contention and GPU synchronization errors
with rate limiting. This is the P0 synchronization foundation only: it does
**not** yet copy pixels into an encoder-owned buffer, submit to NVENC/AMF/QSV,
or send video packets. The HMD projection/distortion values and eye geometry
are initial SBS defaults; final optical-profile handoff from Android is a
subsequent integration step.

The driver exposes only the phone's 3DoF orientation. No room-scale position
or 6DoF tracking is synthesized.

## Chaperone / tracking universe

On HMD activation the driver publishes a built-in OpenVR v5 `chaperone_info`
JSON string through `Prop_DriverProvidedChaperoneJson_String` and sets
`Prop_CurrentUniverseId_Uint64` to the matching stable PhoneVR universe ID.
The JSON defines both seated and standing origins with neutral transforms.
Collision bounds are intentionally empty because this version has no position
tracking and must not claim a room-scale safety boundary. The `[1, 1]` play-area
value is metadata required by the Chaperone structure, not a measured physical
play area. PhoneVR remains a 3DoF orientation-only HMD.

The driver checks both property-write results and logs either successful
publication or the OpenVR property error codes. A Chaperone-property write
failure is non-fatal to HMD activation: pose tracking continues, while the log
warns that SteamVR may still request Room Setup. A user may also see SteamVR's
first-run onboarding independently of this driver property. Windows/SteamVR
runtime testing is required to confirm the prompt disappears on the target
installation.
