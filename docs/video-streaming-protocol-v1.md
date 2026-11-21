# PhoneVR Low-Latency Video Protocol v1 (0.4.19)

This protocol is additive to control protocol v1. It does not alter the existing
`PVSP` 36-byte Android HMD pose datagram. The 0.4.19 implementation wires the
SteamVR backbuffer capture, Host hardware H.264 encode, `PVRV` UDP fragments,
Android reassembly, MediaCodec decode and SBS stereo rendering. Full real-device
latency and Wi-Fi / USB-tether end-to-end qualification remain required.

## Transport layout

| Port | Transport | Direction | Purpose |
|---|---|---|---|
| 48100 | UDP broadcast | Android ↔ Host | Discovery |
| 48101 | TCP | Bidirectional | Pairing, codec capability negotiation and control |
| 48102 | UDP | Android → Host | HMD pose (`PVSP`) |
| 48103 | UDP loopback only | Host → SteamVR driver | Validated pose forwarding to `vrserver.exe` |
| 48104 | UDP | Host → Android | Encoded H.264 video fragments |
| 48105 | UDP loopback only | SteamVR driver → Host | Driver heartbeat and display diagnostics |
| 48106 | TCP loopback only | SteamVR driver → Host | Raw BGRA compositor capture bridge (`PVRC`) |

The PC may use Ethernet to a Wi-Fi access point while the phone uses a dedicated 5 GHz WLAN, or the phone may use a direct USB-tether/RNDIS network. Guest network/client isolation must be disabled for wireless discovery.
Ports 48103, 48105 and 48106 bind only to loopback; the video UDP sender targets
the phone's paired control-session IP.

## Negotiated streaming profile

- Codec: H.264/AVC Annex-B.
- Packed image: side-by-side stereo, 1600×720 (800×720 per eye).
- Capture and encode ceiling: 60 FPS. Source virtual-display timing is 72 Hz;
  capture is rate-limited independently to 60 FPS.
- H.264 B-frames are disabled. Encoder GOP is one second, with SPS/PPS repeated
  at keyframes and AUD boundaries inserted for access-unit framing.
- Host requires a verified hardware H.264 encoder (NVIDIA NVENC, AMD AMF or
  Intel QSV). CPU software encoding is disabled for live VR streaming to avoid
  competing with SteamVR rendering on CPU-bound systems.
- Android must report a reliably classified hardware MediaCodec decoder
  supporting the exact 1600×720@60 profile. Host selects that decoder by its
  advertised codec name and includes the name in the `welcome` message so the
  client creates the exact codec that passed capability validation. If either
  hardware side is unavailable, Host does not enable video; pose and control
  continue independently.

## Driver-to-Host capture bridge (`PVRC`)

After both codec sides are validated during the control handshake, the driver
connects to `127.0.0.1:48106`. Host sends a one-byte activation marker (`0x01`).
The driver uses D3D11 to GPU-scale the final SBS compositor backbuffer to
1600×720, copies into a three-slot staging ring, and a background worker sends
frames over this loopback TCP connection. The 32-byte `PVRC` header contains
magic/version/pixel format, frame ID, PC monotonic capture timestamp, width,
height, BGRA stride and payload size. BGRA bytes follow immediately.

The compositor callback never waits for CPU readback, encoder input or network
I/O. When all staging slots are busy, the oldest queued frame is dropped.

## Host-to-Android video packet (`PVRV`)

Each encoded H.264 access unit is fragmented into UDP datagrams. The 32-byte
header is big-endian; maximum datagram size is 1232 bytes (1200 payload bytes).

| Offset | Size | Field | Description |
|---:|---:|---|---|
| 0 | 4 | Magic | ASCII `PVRV` |
| 4 | 1 | Version | `1` |
| 5 | 1 | Flags | bit 0 first fragment, bit 1 last fragment, bit 2 key frame |
| 6 | 2 | Header size | `32` |
| 8 | 4 | Frame ID | monotonically wrapping `uint32` |
| 12 | 8 | Capture timestamp | PC monotonic nanoseconds |
| 20 | 4 | Encoded frame size | access-unit bytes |
| 24 | 4 | Fragment offset | byte offset in access unit |
| 28 | 2 | Payload size | bytes in this datagram |
| 30 | 2 | Reserved | must be zero |
| 32 | variable | Payload | Annex-B H.264 access-unit bytes |

Each fragment repeats the key-frame flag. Android validates every header and
fragment boundary, tolerates out-of-order fragments, and assembles only a small
number of in-flight frames. Incomplete or late frames are discarded. The
decoder queue is bounded to two frames; if a frame gap, queue overflow or
MediaCodec input-buffer drop occurs, the decoder discards dependent P-frames
until the next IDR keyframe. A transient
Host UDP send failure drops that access unit but does not terminate FFmpeg.

## Android presentation

The Android client selects a hardware AVC decoder via `MediaCodecList` and
validates 1600×720@60 using `VideoCapabilities.areSizeAndRateSupported`. A
dedicated UDP thread reassembles frames; a separate decoder thread feeds
MediaCodec and releases output to a `SurfaceTexture`-backed `Surface`. The GL
renderer samples the external OES texture as SBS, then applies the existing
lens warp and optical correction independently to each eye. Until the first
decoded video frame, the local test scene remains visible.

## Known limits

The PC capture source and Android output are currently fixed to 1600×720 SBS
at up to 60 FPS. Dynamic bitrate/rate adaptation, HEVC/AV1 transport, audio,
clock synchronization, capture-to-photon measurement and a remote keyframe
request control message are not yet implemented. The current H.264 encoder
repeats an IDR at least once per second so the receiver can recover after loss.
Real Windows MSVC/D3D11, target-GPU hardware encoding, Android device MediaCodec
and Wi-Fi/USB-tether end-to-end tests are still required before claiming validated runtime
performance.


## USB/RNDIS network path

The H.264 packet format and port assignment do not change for wired mode. Android discovers the Host using IPv4 directed broadcasts bound to each active interface address, including the USB tether interface. The Host replies to the source endpoint; Android then connects its control TCP socket and sends pose UDP to the Host address returned by discovery. The Host sends video UDP to the peer IP from the established control TCP connection. Thus USB tethering carries the same `PVRV` H.264 datagrams as Wi-Fi; there is no ADB or USB bulk-transfer protocol.
