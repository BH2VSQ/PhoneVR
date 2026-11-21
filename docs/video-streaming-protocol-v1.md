# PhoneVR Low-Latency Video Protocol v1 (0.4.0 foundation)

This protocol is additive to control protocol v1. It does not alter the existing
`PVSP` 36-byte Android HMD pose datagram. The initial implementation defines
packet framing and validation; frame capture, hardware encoding, packet
reassembly, hardware decoding and presentation are separate milestones.

## Initial transport layout

| Port | Transport | Direction | Purpose |
|---|---|---|---|
| 48100 | UDP broadcast | Android -> Host / Host -> Android | Discovery (existing) |
| 48101 | TCP | Bidirectional | Pairing, capabilities, control (existing) |
| 48102 | UDP | Android -> Host | HMD pose (existing `PVSP`) |
| 48103 | UDP loopback only | Host -> SteamVR driver | Forward validated `PVSP` pose to `vrserver.exe` |
| 48104 | UDP | PC -> Android | Encoded video fragments (reserved for streaming) |

The PC should preferably use Ethernet to the Wi-Fi 6 access point while the
phone uses its dedicated 5 GHz WLAN. The video port is not exposed as an
internet-facing service. Guest network/client isolation must be disabled.

## Video frame format

The encoder produces one side-by-side (SBS) frame per presentation timestamp:
left eye occupies the left half, right eye the right half. H.264 is the required
interoperability baseline. HEVC is optional. AV1 is optional and must only be
selected when the phone reports a suitable decoder and measured performance.

Each encoded access unit is fragmented into UDP datagrams. The 32-byte header
is big-endian; the current maximum datagram size is 1232 bytes (1200 payload
bytes), avoiding IP fragmentation on common IPv6 paths.

| Offset | Size | Field | Description |
|---:|---:|---|---|
| 0 | 4 | Magic | ASCII `PVRV` |
| 4 | 1 | Version | `1` |
| 5 | 1 | Flags | bit 0 first fragment, bit 1 last fragment, bit 2 key frame |
| 6 | 2 | Header size | `32` |
| 8 | 4 | Frame ID | monotonically wrapping `uint32` |
| 12 | 8 | Capture timestamp | monotonic nanoseconds on the PC |
| 20 | 4 | Encoded frame size | total access-unit bytes |
| 24 | 4 | Fragment offset | offset in access unit |
| 28 | 2 | Payload size | payload bytes in this datagram |
| 30 | 2 | Reserved | must be zero |
| 32 | variable | Payload | encoded access-unit bytes |

The receiver must validate every header and reject fragments that exceed the
announced frame boundary. It must keep only a small bounded number of in-flight
frames, discard incomplete or late frames, and never queue old frames behind a
newer frame. A key-frame flag is required for recovery; an explicit key-frame
request/control message will be added with the full streaming session handshake.

## Initial negotiated profile

- Codec priority: H.264 first; HEVC only when both sides report capability;
  AV1 only after device capability and thermal/performance qualification.
- Stream geometry: SBS, with each eye's dimensions negotiated independently
  and the packed width equal to `2 * eyeWidth`.
- Starting profile: 960 x 1080 per eye at 60 Hz (1920 x 1080 packed).
- Higher target: 1200 x 1080 per eye at 72 Hz (2400 x 1080 packed), gated by
  measured encoder, Wi-Fi, decoder, GPU and panel performance.
- B-frames: disabled for the low-latency profile. Encoder lookahead and large
  reorder queues are disabled where the backend supports it.
- The packet timestamp is a PC monotonic timestamp. Clock-offset estimation
  and capture-to-photon timing are not yet implemented.

These are initial targets, not claims about achieved latency or guaranteed
capability on every handset.

## Capability discovery

The Android client queries `MediaCodecList` and each decoder's
`MediaCodecInfo.CodecCapabilities` / `VideoCapabilities`. The optional `hello.mediaCapabilities` JSON array reports codecs,
hardware/software classification, maximum dimensions, frame-rate range,
profiles and color formats. It must validate a requested width/height/frame-rate
combination with `areSizeAndRateSupported`; a model name or SoC name is never
used as a substitute for runtime capability discovery. The Host retains the optional report in its connected-device snapshot; older v1 Hosts ignore the additional JSON field.

The Windows Host enumerates FFmpeg encoders present in the installed FFmpeg
build (`h264_nvenc`, `h264_amf`, `h264_qsv`, software fallback, and matching
HEVC/AV1 variants). A listed encoder is only a candidate: the next stage must
initialize the actual device/session to prove that the driver, GPU, format and
requested profile work. When multiple GPU vendors are present, runtime probing
and explicit user preference take precedence over any fixed vendor assumption.

## Implementation state in 0.4.0 foundation

Implemented: Go encoder capability parser/selector; Go 32-byte video packet
encode/decode validation; Android MediaCodec capability query; local SteamVR
pose bridge; SteamVR HMD and display-redirect driver skeleton.

Not implemented yet: SteamVR final-backbuffer texture capture, D3D11/FFmpeg
hardware encoder sessions, UDP video sender/reassembler, Android MediaCodec
stream decoder, decoder-to-OpenGL texture integration, clock synchronization,
rate adaptation, audio and Daydream controller forwarding.
