# PhoneVRStream Network Protocol v1

This document is the shared protocol contract for `android/` and `windows-host/`.
Any incompatible change must increment the protocol version and update both implementations and their tests in the same pull request.

## Ports

| Port | Transport | Direction | Purpose |
|---|---|---|---|
| 48100 | UDP broadcast | Android -> Host and Host -> Android | LAN discovery |
| 48101 | TCP | Bidirectional | Pairing, session setup, heartbeat |
| 48102 | UDP | Android -> Host | 3DoF head pose |

## Discovery

Android broadcasts the UTF-8 text `PHONEVR_DISCOVER_V1` to UDP port `48100`.
The host responds to the sender with:

```text
PHONEVR_HOST_V1 {"name":"PC-NAME","controlPort":48101,"protocol":1}
```

The host name is informational. The Android client should use the source IPv4 address from the UDP datagram when connecting.

## TCP pairing

Messages are UTF-8 JSON objects terminated by a newline (`\n`). Android starts the session with:

```json
{"type":"hello","protocol":1,"deviceName":"Phone","client":"PhoneVRStream-0.2.0"}
```

The host replies:

```json
{"type":"welcome","protocol":1,"sessionId":"...","serverName":"PC-NAME","posePort":48102,"capabilities":["head-pose-3dof-udp-v1","control-heartbeat-v1","display-settings-control-v1","android-optical-presets-v1"]}
```

Heartbeat messages are newline-delimited JSON `ping` and `pong` objects. The Android client sends a heartbeat every two seconds, expects a pong within 10 seconds and retries a lost session with bounded 1/2/4/8 second backoff. The Host applies a 15-second idle read deadline before each message and a 5-second write deadline. Unknown message types should be ignored or rejected without crashing the host.

## UDP pose packet

Every pose packet is exactly 36 bytes. All multibyte fields use big-endian byte order.

| Offset | Size | Field | Encoding |
|---:|---:|---|---|
| 0 | 4 | Magic | ASCII `PVSP` |
| 4 | 1 | Protocol version | Unsigned integer, value `1` |
| 5 | 1 | Flags | Unsigned integer, currently `0` |
| 6 | 2 | Reserved | Unsigned integer, currently `0` |
| 8 | 4 | Sequence number | `uint32` |
| 12 | 8 | Android sensor timestamp | `int64`, nanoseconds |
| 20 | 4 | Quaternion X | IEEE-754 `float32` |
| 24 | 4 | Quaternion Y | IEEE-754 `float32` |
| 28 | 4 | Quaternion Z | IEEE-754 `float32` |
| 32 | 4 | Quaternion W | IEEE-754 `float32` |

Quaternion values are relative to the recentered headset pose. The Android client remaps the sensor axes to the current display orientation before transmission. The Go receiver rejects malformed packets, non-finite quaternion components, and quaternion norms outside the accepted range.

## Compatibility and security

- Protocol v1 supplies 3DoF orientation only. It does not contain position tracking.
- UDP pose packets are best-effort and may be lost or arrive out of order. The sequence number allows the receiver to detect this.
- TCP is used for session control, not high-rate pose streaming.
- The current protocol is unauthenticated and unencrypted. Use only on a trusted private network. Authentication and encryption should be added before exposing the host to untrusted networks.


## Pose sender rate policy

The Android client samples the IMU at the sensor-supported rate but sends only the newest pose at a maximum of 60 packets per second. Updates are conflated in an atomic latest-value slot; historical sensor samples are never queued. UDP pose delivery is best-effort and a transient UDP send error does not close the TCP pairing/heartbeat channel. The TCP control channel remains responsible for connection liveness. High-rate IMU events are processed off the Android UI thread and coalesced to the newest sample before the 60 Hz UDP sender, so sensor bursts do not create an unbounded datagram queue.


## Legacy remote phone display settings

The host advertises the additive capability `display-settings-control-v1` in the TCP `welcome.capabilities` array. The following direct settings exchange is retained for compatibility with earlier client and Host builds. The current UI manages calibration parameters through Android-owned optical presets in the next section. After pairing, Android also reports its current effective display settings to the host:

```json
{"type":"display_settings","settings":{"viewerProfileId":"custom","ipdMm":63.0,"lensSeparationMm":63.9,"screenToLensDistanceMm":40.0,"screenVerticalAlignment":"center","trayToLensCenterMm":35.0,"lensCenterX":0.5,"lensCenterY":0.5,"distortionK1":0.18,"distortionK2":0.04,"blur":0.0,"sharpness":0.35,"fovDegrees":90.0}}
```

The host may update the connected phone with:

```json
{"type":"set_display_settings","requestId":"...","settings":{...}}
```

The Android client validates and clamps values to the same supported ranges as its local display settings, persists the result, updates the stereo renderer, and replies:

```json
{"type":"display_settings_ack","requestId":"...","status":"applied","settings":{...}}
```

Rejected updates use `status:"rejected"` and include a `message`. Settings are exchanged over the existing newline-delimited TCP control channel. UDP pose packets remain unchanged. The Windows native desktop UI calls the Host service directly. No local HTTP dashboard/API or browser runtime is used.

Supported fields: `viewerProfileId` (`custom` or `daydream_view_2017`), `ipdMm` (50..75), `lensSeparationMm` (50..75), `screenToLensDistanceMm` (25..70), `screenVerticalAlignment` (`center`, `top`, `bottom`), `trayToLensCenterMm` (10..60), `lensCenterX/Y` (0.35..0.65), `distortionK1` (-0.30..0.60), `distortionK2` (-0.20..0.30), `blur` (0..1), `sharpness` (0..1), and `fovDegrees` (60..115). The Daydream View 2017 profile normalizes lens spacing to 64 mm and distortion coefficients to K1=0.4331, K2=-0.085.


## Android-owned optical presets

The host advertises `android-optical-presets-v1` in `welcome.capabilities`. After the TCP session is ready, the host requests the phone's local preset catalogue:

```json
{"type":"get_optical_presets","requestId":"..."}
```

Android reads the persistent list from its local settings store and replies with `optical_presets` containing an array of `{id,name,builtin,settings}` objects. The host must treat this list as the authoritative catalogue for the connected device; the host does not persist a competing catalogue. The Windows UI can create, rename, clone, edit, or delete entries and sends the complete updated list with `set_optical_presets`. Android validates the entries, saves them locally, and responds with `optical_presets_ack` (`status:"applied"` plus the normalized list, or `status:"rejected"` plus a message).

To apply a preset, the host sends only its ID:

```json
{"type":"apply_optical_preset","requestId":"...","presetId":"builtin-cardboard"}
```

Android resolves the ID against its own local catalogue, persists the active preset and display settings, updates the stereo renderer, and returns `display_settings_ack`. Missing IDs are rejected. This keeps the phone-side preset catalogue authoritative while allowing the host to provide the complete editing interface. These messages use the existing newline-delimited TCP control channel; UDP pose packets are unchanged.
