# 2026-11-22 Video startup and Android codec diagnostics

## Findings from the reported Host log

The first Android connection arrived while `main.go` was still deploying the SteamVR driver and before `ProbeVideoEncoders()` was called. `Start()` opened the TCP control listener first, so `handleControlConnection()` could observe `videoEncoderProbeDone == nil`. In that case it did not wait for the asynchronous hardware encoder preflight, selected no ready Host encoder, and permanently stored `VideoEnabled=false` for that control session.

That explains the sequence:

- `Device paired` followed immediately by `hardware encoder preflight did not complete before the pairing deadline`.
- `Live VR video hardware encoder validated: h264_qsv/qsv` appears later.
- Subsequent SteamVR video bridge connections are rejected because the already-paired device remains `VideoEnabled=false`.

After the phone disconnected and paired again, the Host accepted the video bridge but timed out waiting for the first raw frame header. The bridge is accepted only when a ready Android device has negotiated a hardware H.264 decoder and the Host encoder is ready. Therefore this second session passed the Host's H.264 negotiation gate. The remaining stall is before the first raw frame reaches Host, in the SteamVR driver `IVRVirtualDisplay::Present()` / capture path.

## Changes in this revision

1. `main.go` starts `ProbeVideoEncoders()` before `HostService.Start()` opens the control listener. The pairing handler can no longer race a not-yet-created probe channel during normal application startup.
2. Host waits up to eight seconds for the preflight. Android's initial welcome read timeout is raised to ten seconds so it does not abandon the control handshake before the Host finishes that wait.
3. When the Android H.264 profile is rejected, Host logs every advertised `video/avc` decoder with maximum resolution/frame rate, hardware classification and exact `1600x720@60` result.
4. Android logs the H.264 MediaCodec enumeration to Logcat under `PhoneVRCodecProbe`, including the exact component names reported by the installed ROM.

## Device-specific diagnostic commands

On the phone, enable USB debugging and run:

```powershell
adb logcat -c
adb logcat -v time -s PhoneVRCodecProbe:I PhoneVRVideo:I AndroidRuntime:E
```

The codec probe should show the hardware H.264 decoder components supplied by the Redmi K40's installed LineageOS/vendor codec stack. The exact names and capability ranges must be read from the device; do not assume them from the model name alone.

On Windows, after launching a VR application and confirming its scene is visible in SteamVR's VR View, collect the driver component and frame counters:

```powershell
$steam = (Get-ItemProperty 'HKCU:\Software\Valve\Steam').SteamPath
$vrserver = Join-Path $steam 'logs\vrserver.txt'
Select-String -Path $vrserver -Pattern `
  'DisplayRedirect diagnostics',
  'DisplayRedirect Activate',
  'DisplayRedirect GetComponent',
  'HMD GetComponent',
  'virtual display active=',
  'virtual display Present=',
  'video capture pipeline'
```

## Expected interpretation

- `Video streaming disabled ... hardware encoder preflight...` should disappear after the startup ordering fix.
- A second successful pairing followed by Host's `video capture bridge stalled while reading a frame header` means the Host accepted the negotiated video session but did not receive the first `PVRC` raw-frame header.
- If `DisplayRedirect GetComponent` remains zero, SteamVR did not request the virtual display component.
- If the component was requested and matched but `Present=0Hz`, the compositor has not delivered a frame to the PhoneVR virtual display.
- If `Present>0Hz` but `completed=0Hz`, then inspect D3D11 shared texture synchronization/readback.

## Verification

Windows Host tests, race tests, `go vet`, Windows/amd64 cross compilation, and the 38 Python release/integration tests passed in the build environment. Android APK compilation and native Windows SteamVR driver compilation require the target toolchains and must still be performed on the user's Windows/Android build machine.
