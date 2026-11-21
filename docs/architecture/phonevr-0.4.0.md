# PhoneVR 0.4.0 — SteamVR wireless HMD foundation

## Hardware-first compatibility targets

The first reference phones are Redmi K40 (Snapdragon 870) and Redmi Note 12
Turbo (Snapdragon 7+ Gen 2). Both have 120 Hz-class OLED panels; the Note 12
Turbo's official specification lists Wi-Fi 6 Enhanced and Bluetooth 5.3. K40
is a Wi-Fi 6 / Bluetooth 5.1 device. The implementation does not hard-code a
Qualcomm-only path: Android's MediaCodec runtime query decides the available
hardware decoders and accepted resolution/frame-rate combinations. The app
sends that optional capability report in the existing TCP `hello` JSON; the Go
Host stores it alongside the connected device for subsequent negotiation.

The first universal video profile is H.264 AVC. HEVC is an optional bandwidth
optimization. AV1 is capability-gated and is not a baseline requirement. The
initial stream geometry is SBS with 960x1080 pixels per eye at 60 Hz; 1200x1080
per eye at 72 Hz is a higher target, subject to device testing.

## Windows GPU strategy

PhoneVR must support a mixed-vendor PC. The initial FFmpeg capability layer
recognizes NVENC, AMF and QSV encoders and provides deterministic candidate
selection:
preferred backend if specified, otherwise NVENC -> AMF -> QSV -> software.
This ordering is only a candidate preference. Since the PC may contain multiple
GPU vendors and multiple adapters of one vendor, the next stage must enumerate
DXGI adapters, bind the selected adapter to an encoder device (NVENC D3D11,
AMF D3D11, or oneVPL/QSV) and run an actual test encode for the requested codec,
profile, resolution, frame rate and input format before advertising it as
usable. Software encoding is a diagnostic/compatibility fallback, not the
recommended low-latency path. The UI will expose Auto and explicit
adapter/backend choices later.

## SteamVR architecture

- `PhoneVRHmd` is registered as `TrackedDeviceClass_HMD`. Its `RunFrame()`
  publishes the Android 3DoF quaternion with
  `IVRServerDriverHost::TrackedDevicePoseUpdated`; OpenVR documents
  `ITrackedDeviceServerDriver::GetPose()` as unused.
- `PhoneVRDisplayRedirect` is registered as `TrackedDeviceClass_DisplayRedirect`
  and exposes `IVRVirtualDisplay`, the OpenVR path intended to access the final
  compositor backbuffer for wireless transport.
- The Go Host remains a separate process and retains phone pairing/configuration.
- The Host forwards the validated existing `PVSP` pose datagram to
  `127.0.0.1:48103`. The SteamVR driver listens only on loopback; port 48103 is
  never advertised on the LAN.
- The `IVRVirtualDisplay::Present` callback is intentionally non-blocking. In
  this foundation it records presentation timing only; D3D11 texture capture
  and encoder queue submission are the next step.

OpenVR SDK is fetched at a pinned Valve commit during the SteamVR driver build.
The driver is Windows x64 for the first milestone. No proprietary SteamVR
runtime files are redistributed.

## Device separation and tracking limits

The phone IMU is the HMD orientation source. Daydream View controller remains a
separate 3DoF controller target; it must not be merged into the HMD quaternion.
No 6DoF room position is fabricated. BLE/GATT pairing is not implemented in
this milestone; the existing Android controller provider boundary remains the
future integration point.

## 0.4.0 milestone acceptance

1. Build and register the driver with SteamVR.
2. SteamVR enumerates a PhoneVR HMD and display redirect device.
3. With a paired Android phone, turning the phone changes the HMD pose in
   SteamVR; with no pose packets, the HMD remains connected but reports an
   invalid/uninitialized pose.
4. Android logs real decoder capabilities for H.264/HEVC/AV1.
5. Host tests verify pose forwarding and video datagram framing.
6. A subsequent streaming milestone captures `PresentInfo_t` backbuffer,
   hardware-encodes it, transports frames, and presents decoded SBS video.
