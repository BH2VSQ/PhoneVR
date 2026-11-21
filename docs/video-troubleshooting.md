# PhoneVR video pipeline diagnostics

PhoneVR has separate control, pose, and video transports. The SteamVR heartbeat only confirms that the driver is reporting status. It does not prove that a video frame has reached the Android display.

## Pipeline and checkpoints

1. SteamVR compositor calls the virtual display `Present` callback. The Host heartbeat frame counter advances only after the subsequent GPU synchronization readback in `WaitForPresent` succeeds, so a zero count can mean that `Present` was not called or that the GPU completion readback did not complete.
2. The driver opens the shared D3D11 texture, scales the SBS image, and queues a staging-texture readback.
3. The capture worker maps the staging texture, copies BGRA pixels to CPU memory, and sends a `PVRC` raw-frame record to `127.0.0.1:48106`.
4. Windows Host reads the complete raw frame and submits it to the hardware H.264 encoder.
5. The Host fragments each encoded H.264 access unit and sends UDP packets to the Android client on port `48104`.
6. Android reassembles complete access units, waits for an IDR after packet loss, and feeds MediaCodec.
7. MediaCodec renders to the SurfaceTexture-backed surface. The OpenGL renderer samples the SBS frame and applies separate left-eye and right-eye lens correction.

## Reading the diagnostics

The Windows Host dashboard reports whether the driver capture bridge is active and cumulative raw-frame, H.264 access-unit, UDP-fragment, and dropped-access-unit counters. The Host log also reports raw and encoded FPS windows, UDP fragment counts, dropped access units, and encoder backend.

The native SteamVR driver log reports queued frames, successful GPU readbacks, frames sent over TCP, D3D11 readback failures, bridge failures, and skipped frames in five-second windows.

The Android connection status progresses through UDP receiver startup, first complete H.264 access unit, decoder startup, decoded output, and first SurfaceTexture frame. A status stuck at one stage narrows the fault to that stage or the next transition.

| Observation | Likely area to inspect |
| --- | --- |
| SteamVR connected, but the completed-frame counter remains zero | SteamVR virtual display registration and compositor activation, plus the D3D11 completion-fence `Map` in `WaitForPresent` |
| Driver queues no capture frames | Present callback, shared texture handle, adapter compatibility, and capture-resource initialization |
| Frames queued but readback count remains zero | D3D11 staging texture creation, GPU synchronization, format, and Map failures |
| Readbacks increase but TCP sent count does not | Host listener on `127.0.0.1:48106`, activation handshake, and TCP write failures |
| Host raw-frame count increases but H.264 count does not | FFmpeg hardware encoder startup, input format, and encoder logs |
| H.264 count increases but Android receives no complete frame | Wi-Fi path, firewall, UDP port `48104`, packet loss, or fragment reassembly |
| Android receives a complete frame but has no decoded output | Negotiated decoder name, MediaCodec configure/start, codec profile, and input queue |
| Android decoded output increases but test spheres remain visible | SurfaceTexture callback, GL context lifecycle, external OES texture, and render-thread updates |

## Older Android releases

Android versions before API 29 do not expose the same hardware-accelerated codec classification API. PhoneVR uses codec-name conventions as a fallback to exclude common software decoders, while keeping `hardwareClassificationReliable=false` for diagnostics. A codec is still required to advertise H.264 and the negotiated SBS dimensions and frame rate. The definitive device-side check is whether the selected MediaCodec can configure and start with the negotiated output Surface.

## Capture performance note

The capture worker releases the shared D3D11 resource lock after the GPU readback and CPU copy. TCP transmission runs after the lock is released so a slow or blocked socket does not hold the Present-side capture resources. A transient staging-texture Map failure drops that frame and is counted; it does not automatically tear down a healthy TCP bridge.

## Information to collect when the stream still fails

Collect the Windows Host log, SteamVR driver log, and Android `logcat` output from one launch attempt. Include the dashboard counters and the phone's Android version and model. These three logs together identify the last completed stage without treating the SteamVR heartbeat as proof of a working video path.
