# PhoneVR audio streaming architecture

Status: PhoneVR 0.5.35 transport and Android PCM pipeline integrated; Windows audio-device endpoints are still pending.

## 0.5.35 implementation status

PhoneVR 0.5.35 adds a dedicated audio packet format, an independent LAN UDP listener, two USB AOA multiplex channels, Android playback with `AudioTrack`, and Android microphone capture with `AudioRecord`. The audio receive and playback work uses its own bounded queue and worker threads. Existing H.264 video packetization, video UDP port `48104`, pose UDP port `48102`, TCP control port `48101`, and AOA video channel remain unchanged.

The first implementation uses signed 16-bit little-endian PCM because it can be tested without assuming a device-specific Opus codec implementation. It is a transport baseline, not the final bandwidth-optimized codec. At 48 kHz, 20 ms frames, it uses 1,536 kbit/s for stereo PC playback and 768 kbit/s for mono phone microphone audio, excluding framing and transport overhead.

The Windows Host currently exposes `PublishAudioPCM` for the future PC playback capture worker and `AudioUplinkFrames` for the future Windows microphone endpoint worker. These are transport integration points. This revision does not yet contain the Windows WASAPI loopback capture worker, the Windows audio render/capture endpoint driver, audio-device selection UI, or OpenVR playback/recording endpoint-ID publication. Therefore, a complete PC game-audio-to-phone and phone-microphone-to-Windows-application experience still requires those endpoint components. The Host will receive microphone packets, but no Windows virtual microphone currently consumes them.

## Independent paths

### PC playback to phone

1. A future Windows WASAPI shared-mode loopback worker captures the selected PC render endpoint.
2. The Host converts the mix to 48 kHz stereo PCM16 and calls `PublishAudioPCM` once per 20 ms frame.
3. LAN sends the framed audio packet over UDP port `48110`; USB sends it over AOA channel `4`.
4. Android validates the packet, enqueues it in a bounded 12-frame queue, prebuffers two frames, and writes to a low-latency stereo `AudioTrack`.
5. A missing frame is currently replaced by silence. There is no Opus PLC decoder in this PCM baseline.

### Phone microphone to PC

1. Android requests `RECORD_AUDIO` at connection time. If permission is denied, PC-to-phone playback remains available and microphone capture is disabled.
2. `AudioRecord` captures 48 kHz mono PCM16 in 20 ms blocks. Android attempts to enable the platform acoustic echo canceller and noise suppressor when available.
3. LAN sends packets to Host UDP port `48110`; USB sends them over AOA channel `5`.
4. Host validates packets and exposes them on the bounded `AudioUplinkFrames` queue. When full, stale frames are discarded to keep latency bounded.
5. A Windows virtual microphone capture endpoint still needs to consume this queue and publish the stream as a Windows recording device.

For LAN receive-only use, Android sends one silent microphone-format registration frame when the audio socket starts. This lets the Host learn the phone's ephemeral UDP source port even when microphone permission is unavailable, so the Host can send playback audio to the phone.

## Wire format

All audio packets use the same 24-byte little-endian header on UDP and AOA. The signature is `PVRA`; protocol version is `1`; direction `1` is Host-to-phone and direction `2` is phone-to-Host; channel count is `2` for playback and `1` for microphone; sample format `1` is PCM16LE; then follow a 32-bit sequence number, 64-bit sender-local nanosecond timestamp, 16-bit samples per channel (`960`) and 16-bit payload length. The payload contains interleaved PCM samples. Timestamps are sender-local and must not be compared directly across device clocks.

The UDP socket is independent of pose/video sockets. USB AOA multiplex channel `4` is Host-to-phone audio and channel `5` is phone-to-Host audio. Both directions use the same audio packet payload. The AOA framing layer retains its 16-byte `PVRU` frame header.

## Remaining Windows integration

- Add a WASAPI loopback worker for the selected playback endpoint and call `PublishAudioPCM` with exact 20 ms frames. WASAPI loopback uses a render `IMMDevice` opened in shared mode with `AUDCLNT_STREAMFLAGS_LOOPBACK`.
- Add or integrate a virtual capture endpoint named `PhoneVR Microphone`, feed it from `AudioUplinkFrames`, and expose its real endpoint ID to Windows applications. A user-mode callback alone cannot create a system-wide Windows recording endpoint.
- Publish actual endpoint IDs to SteamVR using OpenVR tracked-device properties `Prop_Audio_DefaultPlaybackDeviceId_String` and `Prop_Audio_DefaultRecordingDeviceId_String` after the endpoints are installed and enumerated.
- Add endpoint selection, mute/volume controls, sample-rate conversion, clock-drift correction and a measured jitter buffer.
- Replace PCM with Opus after a supported encoder/decoder is integrated and tested on both Windows and Android.

A virtual cable pair can be used as an interim test: one render endpoint feeds Host loopback capture; a separate render endpoint receives the phone microphone stream, while its paired capture endpoint is selected by Windows applications. Keep the playback and microphone paths separate to prevent feedback.

## Validation requirements

Before enabling audio by default, measure LAN and USB latency independently, packet loss, jitter, long-run clock drift, reconnect behavior, Android output-route changes, microphone permission changes, echo behavior and simultaneous H.264/video/pose load. The current source tests validate packet encoding/decoding and AOA channel framing; they do not yet establish end-to-end audio device operation.
