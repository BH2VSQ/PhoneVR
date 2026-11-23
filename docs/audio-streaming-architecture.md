# PhoneVR audio streaming architecture

Status: design note for PhoneVR 0.5.24. No end-to-end audio transport is implemented in this revision.

## Current behavior

The 0.5.24 codebase still has no Windows WASAPI capture/render worker, no Android `AudioRecord` or `AudioTrack` pipeline, no audio packet type, and no Android `RECORD_AUDIO` permission. The SteamVR driver did not publish `Prop_Audio_DefaultPlaybackDeviceId_String` or `Prop_Audio_DefaultRecordingDeviceId_String`. Consequently, choosing an HMD-related input/output entry in SteamVR cannot create the missing Windows endpoints or transport audio to the phone.

OpenVR defines the playback and recording endpoint ID properties as tracked-device properties 2300 and 2301. PhoneVR must populate them with real Windows audio endpoint IDs after the virtual endpoints are installed and enumerated.

## Required independent paths

### PC playback to the phone

1. Windows applications and SteamVR render to a dedicated virtual render endpoint named `PhoneVR Headphones`.
2. PhoneVR Host opens that endpoint in WASAPI shared mode with loopback capture enabled.
3. Host converts the endpoint mix to 48 kHz stereo PCM and encodes low-latency Opus frames.
4. Audio packets are sent to Android over the selected LAN or USB AOA path.
5. Android decodes with Opus and writes to an `AudioTrack`, using the phone's selected physical output route.

WASAPI loopback capture is specifically designed to capture the stream being played by a render endpoint. It obtains the render `IMMDevice` and initializes the capture client with `AUDCLNT_STREAMFLAGS_LOOPBACK` in shared mode.

### Phone microphone to Windows applications

1. Android requests runtime microphone permission and captures the selected microphone with `AudioRecord`.
2. Android converts to 48 kHz mono PCM and encodes low-latency Opus frames.
3. Host receives the frames, applies a small jitter buffer and clock-drift correction, then decodes to PCM.
4. Host feeds the PCM to a dedicated virtual microphone capture endpoint.
5. Windows applications select `PhoneVR Microphone` as their recording device.

The playback and microphone paths must use independent endpoint IDs and independent stream queues. Reusing the same virtual cable for both directions would risk capturing the phone microphone in the PC-to-phone loopback stream and creating an audio feedback path.

## Windows endpoint strategy

For an early integration test, two independent virtual audio cable pairs can model the two paths:

- Cable A render endpoint: Windows game/SteamVR output; Host captures it with WASAPI loopback.
- Cable B render endpoint: Host plays the phone microphone stream into it; Windows applications use Cable B's paired capture endpoint as their microphone.

For a self-contained PhoneVR installer, use a dedicated virtual audio driver exposing the corresponding playback and microphone endpoints. Microsoft's SysVAD sample is a useful WDM/WaveRT starting point, but it is a driver-development sample rather than a drop-in finished bridge. Its published sample explicitly simulates some capture/loopback behavior, so PhoneVR would need to implement the real ring-buffer transport and endpoint data flow, validate it with the Windows Hardware Lab Kit, and package/sign the driver appropriately.

After endpoint installation, Host should enumerate endpoint IDs using `IMMDeviceEnumerator`, store the selected IDs in `%APPDATA%\\PhoneVRStream\\audio-devices.json`, and expose those IDs to the SteamVR driver. The driver should assign the OpenVR playback and recording ID properties to the selected endpoints. The Host must also confirm at startup that the IDs still resolve, because Windows endpoint IDs can change after a device reinstall or driver replacement.

## Audio packet design

Use a separate framed audio channel rather than putting audio into the video access-unit protocol. The initial stream should use:

- Codec: Opus.
- Output: 48 kHz, stereo, 20 ms frames.
- Microphone: 48 kHz, mono, 20 ms frames.
- Independent sequence number and capture timestamp for each direction.
- A bounded 40 to 80 ms jitter buffer with packet-loss concealment.
- USB AOA and UDP paths with the same packet payload and sequence semantics.
- Audio queue priority above video retransmissions, while keeping pose updates on their existing low-latency channel.

The implementation also needs sample-rate conversion, drift correction between the PC and phone clocks, mute/volume controls, Android audio focus handling, runtime microphone permission, disconnect flushing and recovery. Acoustic echo cancellation and noise suppression should be configurable and measured on-device; they must not be assumed to work automatically for every microphone route.

## Integration order

1. Install and enumerate the two virtual endpoint paths. Prove PC loopback capture and virtual-microphone playback independently with a local test tone and microphone recording.
2. Add a Windows audio worker using WASAPI and expose selected endpoint IDs to the SteamVR driver.
3. Add the audio packet protocol and USB AOA/UDP transport.
4. Add Android `AudioTrack` playback and `AudioRecord` capture, including permission and lifecycle handling.
5. Validate one-way latency, round-trip latency, drift, packet loss, reconnection and feedback prevention before enabling audio by default.

Audio remains disabled until both directions pass those tests. Merely setting SteamVR's input and output selectors to an HMD entry is not evidence that a transport path is active.
