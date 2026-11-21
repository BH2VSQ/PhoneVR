# PhoneVR SteamVR Driver — 0.4.19 (DisplayRedirect diagnostics)

Windows x64 OpenVR server driver foundation for the PhoneVR wireless HMD.

## Devices

- `PHONEVR-HMD-001`: `TrackedDeviceClass_HMD`, 3DoF orientation sourced from
  Android `PVSP` packets forwarded by the Go Host.
- `PHONEVR-DISPLAY-001`: `TrackedDeviceClass_DisplayRedirect`, exposing
  `IVRVirtualDisplay` so SteamVR can provide the final composited backbuffer
  for a wireless output path.

The HMD pose receiver binds only to `127.0.0.1:48103`; it is not a LAN
service. A dedicated above-normal-priority worker wakes immediately when the
receiver publishes a new sample generation and calls
`IVRServerDriverHost::TrackedDevicePoseUpdated` independently of the
`IServerTrackedDeviceProvider::RunFrame()` cadence. A 10 ms keepalive republishes
the latest pose when sensor updates pause briefly. The provider's `RunFrame()`
only collects runtime-cadence diagnostics and sends the 250 ms loopback-only
`PVST` health heartbeat to Host UDP `127.0.0.1:48105`. Host considers the driver
connected only while these heartbeats are fresh.

The driver reports separate rates for UDP samples received, unique/repeated
pose publications and `RunFrame()` callbacks, plus sequence gaps, drained
packets, maximum pose publish gap and maximum sample age. The virtual-display
component separately reports `Present` / `WaitForPresent` callback rates and
maximum callback durations, so tracking-rate problems can be distinguished
from compositor/GPU synchronization stalls. The virtual display timing is fixed at 72 Hz in this revision; sensor input frequency is not the display refresh frequency.

The D3D11 capture implementation uses `ID3D11Multithread`, declared by the Windows SDK header `d3d11_4.h`, to enable context-level multithread protection when the capture and compositor callbacks share the immediate context.

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
uses a bounded keyed-mutex acquire (10 ms maximum), and queues a small staging
texture copy as a GPU-completion fence. `WaitForPresent` maps this synchronization
texture. When a compatible phone and hardware encoder are ready, the driver also
GPU-scales the final SBS compositor image to 1600×720 and queues it in a bounded
triple-staging ring. A background worker reads BGRA frames and forwards them to
the Host over loopback TCP `127.0.0.1:48106`; it never performs CPU readback,
encoding or socket I/O inside `Present`. Host performs hardware-only H.264
encoding and sends Annex-B access units as `PVRV` UDP fragments to Android
port `48104`. `GetTimeSinceLastVsync` uses a fixed 72 Hz virtual-vsync clock.
The HMD projection/distortion values and eye geometry remain initial SBS defaults;
final optical-profile handoff from Android remains a separate calibration task.

The driver exposes only the phone's 3DoF orientation. No room-scale position
or 6DoF tracking is synthesized.


## Direct display mode in SteamVR

`PhoneVRHmd` exposes `IVRDisplayComponent` and returns `false` from
`IsDisplayOnDesktop()`, which tells SteamVR that the HMD must not be treated as
an extended desktop monitor. The separate `PhoneVRDisplayRedirect` continues
to implement `IVRVirtualDisplay`, which is the OpenVR wireless-output path for
obtaining the final compositor backbuffer. It should not be replaced with
`IVRDriverDirectModeComponent`: that interface is for a driver's own display
compositor and would bypass the final-composited-frame handoff PhoneVR needs.

If SteamVR still reports that the HMD is running as a monitor, enable
**SteamVR → Settings → Developer → Enable Direct Mode**, then restart SteamVR.
This is a SteamVR runtime setting and can be unavailable when the graphics
driver/runtime cannot take direct display ownership; PhoneVR does not silently
rewrite the user's global `steamvr.vrsettings` file. The driver logs that it
reports `IsDisplayOnDesktop=false` when the HMD activates.

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

## Live video capture

The driver captures the final SBS compositor backbuffer only while a paired Android client has successfully negotiated hardware H.264 decode and Host has validated a hardware H.264 encoder. D3D11 performs GPU scaling to 1600×720; a bounded triple-staging ring and background worker deliver BGRA frames to the Host's loopback TCP bridge on `127.0.0.1:48106`. Host then hardware-encodes and transmits `PVRV` UDP fragments to Android port `48104`. The 72 Hz virtual-display timing is fixed in this revision; the removed refresh-rate menu no longer writes a per-user timing file.


## DisplayRedirect diagnostics (0.4.19)

This diagnostic build logs the first `GetComponent` request received by the HMD
and DisplayRedirect devices, whether the requested interface matches
`IVRDisplayComponent_003` / `IVRVirtualDisplay_002`, the DisplayRedirect
activation object index, and the return status of each display-redirection
property write. The five-second telemetry line includes the DisplayRedirect
active flag and cumulative `GetComponent=queries/matches` counters.

Interpretation:

- `GetComponent=0/0`: SteamVR has not queried the DisplayRedirect component.
- `GetComponent=N/0`: SteamVR queried the device, but the requested interface
  version did not match the implemented `IVRVirtualDisplay_002`.
- `GetComponent=N/N` with `Present=0Hz`: the component was queried and returned,
  but the compositor is not submitting frames to it. Confirm that a VR app is
  actively rendering and inspect compositor routing / HMD property overrides.
- `Present>0Hz` with `completed=0Hz`: only then inspect D3D11 shared-texture and
  `WaitForPresent` synchronization.


## DisplayRedirect startup ordering follow-up (2026-11-22)

The driver now pre-initializes the DisplayRedirect D3D11 device and registers the DisplayRedirect tracked device before registering the PhoneVR HMD. It also writes the virtual-display capability and graphics-adapter LUID to OpenVR's `k_ulDisplayRedirectContainer`, then logs the effective values read through the HMD's property container. This addresses the observed startup sequence where SteamVR queried the HMD display component before DisplayRedirect activation had finished. See `docs/2026-11-22-displayredirect-startup-order.md`.
