# DisplayRedirect startup-order investigation

## Evidence from the 02:42–02:46 SteamVR log

The DisplayRedirect was active and its `GetComponent` counter reported five queries with one exact `IVRVirtualDisplay_002` match. The first query being logged as `IVRDisplayComponent_003` is an ordinary interface probe that does not match the redirect component; the later exact match shows that the expected virtual-display interface was requested successfully.

The important timestamp order was:

- `02:43:58.523`: HMD `GetComponent` first query for `IVRDisplayComponent_003`.
- `02:43:58.885`: DisplayRedirect `Activate` completed after D3D11 initialization.
- After that, `Present`, `WaitForPresent`, and completed-frame rates remained zero.

This exposed a startup race candidate: the HMD display interface was queried while the DisplayRedirect was still performing its comparatively slow GPU initialization and had not yet published the HMD display overrides.

## Changes in this source revision

1. Initialize the DisplayRedirect D3D11 device before registering either tracked device.
2. Register `PHONEVR-DISPLAY-001` before `PHONEVR-HMD-001`.
3. Publish `Prop_HasVirtualDisplayComponent_Bool` and `Prop_GraphicsAdapterLuid_Uint64` into `vr::k_ulDisplayRedirectContainer`, which OpenVR defines as the override container whose properties are returned when reading the active HMD's properties. Attempt publication during pre-initialization, retry immediately after `TrackedDeviceAdded(DisplayRedirect)` and before registering the HMD, then retry again during DisplayRedirect activation.
4. Continue writing the same metadata to the DisplayRedirect device container.
5. Log the effective virtual-display flag and graphics-adapter LUID as read from the HMD property container during HMD activation.
6. Run the Host hardware H.264 encoder preflight synchronously before opening the TCP pairing listener. The previous goroutine-based probe could race with an early phone connection even though the method was called before `Start()`.

## Expected next log sequence

The exact order may include runtime-generated lines, but the PhoneVR lines should show:

```text
PhoneVR: DisplayRedirect D3D11 pre-initialized ...
PhoneVR: DisplayRedirect HMD override publish phase=pre-registration ...
PhoneVR: DisplayRedirect HMD override publish phase=after-registration ...
PhoneVR: DisplayRedirect registered and HMD override properties published before HMD registration
PhoneVR: HMD Activate entered ...
PhoneVR: HMD effective display properties ... virtualDisplay=true ... adapterLuid=... 
```

Then test with an actual SteamVR application rendering a moving scene. The expected critical change is `Present` becoming nonzero. If `Present` remains zero despite the HMD readback showing `virtualDisplay=true` and the correct adapter LUID, the next investigation should be compositor-side routing and the current SteamVR runtime's `DisplayRedirect` selection; GPU readback and UDP should remain out of scope until `Present` advances.

## Validation status

Go tests, Go race tests, `go vet`, Windows/amd64 Go cross-build, and Python source integration tests run in the Linux development environment. Native MSVC compilation and runtime verification still require the target Windows machine and SteamVR installation.
