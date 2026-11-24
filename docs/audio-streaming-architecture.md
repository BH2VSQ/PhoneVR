# PhoneVR audio streaming architecture

Status: 0.5.35 contains the Android PCM audio engine, independent LAN/USB audio transport, Windows WASAPI loopback bridge, Host lifecycle supervision, user-selectable Windows audio endpoint routing and a native Host Audio & Microphone manager. VAC/VB-CABLE pairing is optional convenience detection; any third-party virtual audio driver can be selected manually. Windows WASAPI native compilation and real-device full-duplex validation still require a Windows build machine.

## Transport and isolation

PhoneVR uses one independent 48 kHz PCM audio path over LAN UDP `48110` and USB AOA channels `4/5`. The existing video UDP port `48104`, pose UDP port `48102`, control TCP port `48101`, H.264 queues and AOA video framing are unchanged. Audio capture, playback, buffering and restart handling stay outside the video and pose worker paths.

The Windows Host starts a separately supervised `PhoneVRAudioBridge.exe` with the Host service. `HostService.Stop` cancels the helper, closes both pipes, waits for both I/O workers and reaps the process. Device invalidation causes a bounded automatic restart. The Android audio engine uses `AudioTrack` for stereo playback and `AudioRecord` for mono microphone capture.

## PC playback to phone

1. The helper opens the user-selected Windows playback/render endpoint in WASAPI shared-mode loopback. If left on automatic, it uses the Windows default console render endpoint.
2. WASAPI packets are converted to 48 kHz stereo PCM16 and grouped into 960-sample-per-channel, 20 ms frames.
3. The Host reads `PVRA` records from the helper's stdout and calls `PublishAudioPCM`.
4. LAN sends the audio frame through UDP `48110`; USB sends it over AOA channel `4`.
5. Android validates the packet, keeps a bounded jitter queue and writes it to a low-latency stereo `AudioTrack`.

The phone-microphone render target must not become the automatic loopback source. Some virtual cable installers can make their own playback endpoint the Windows default. The helper avoids known VAC/VB-CABLE render endpoints and also excludes the exact user-selected phone-microphone render endpoint when choosing the automatic PC-to-phone source. If the user explicitly chooses the same endpoint for both directions, PhoneVR reports a feedback warning because that selection may feed the phone microphone back to the phone.

WASAPI Loopback captures the render mix of the selected playback endpoint. Applications forced to a different per-app render endpoint are not included unless that endpoint is selected as the PC-to-phone source. With the automatic option, PhoneVR uses the Windows console default unless it conflicts with the phone-microphone virtual render route.

## Phone microphone to Windows applications

1. Android `AudioRecord` captures 48 kHz mono PCM16 in 20 ms frames. Platform AEC and noise suppression are enabled when available.
2. LAN sends frames to Host UDP `48110`; USB sends over AOA channel `5`.
3. The Host validates packets and publishes them to a bounded queue. When full, the oldest frame is discarded to limit latency.
4. The Windows Host writes PCM frames to the helper's stdin.
5. The helper opens the user-selected virtual-audio playback/render endpoint in WASAPI shared mode and writes the phone microphone samples into it. The independently selected paired recording/capture endpoint is published to SteamVR and can be selected by VRChat and other Windows recording applications.
6. The helper publishes the user-selected capture endpoint ID to the Host. The Host writes it to `%LOCALAPPDATA%\PhoneVR\audio-record-endpoint.txt`; the SteamVR driver refreshes OpenVR's default recording endpoint property at runtime.

### User-selectable endpoint routing

Open **Menu → Audio & Microphone**. The dialog enumerates all Windows MMDevice endpoints, including active and disabled playback/render endpoints and recording/capture endpoints. It presents three independent selectors:

- **PC → Phone playback source**: a Windows render endpoint to capture with WASAPI Loopback. Any physical or virtual playback endpoint can be selected. The automatic option uses the Windows console default.
- **Phone microphone → PC write target**: the virtual-audio playback/render endpoint that accepts PCM written by PhoneVR. This is usually called the cable's `Input` or playback side.
- **Phone microphone → PC recording endpoint**: the paired Windows recording/capture endpoint that VRChat, SteamVR and other applications open as a microphone. Select the capture side belonging to the same virtual-audio route as the write target.

VAC and VB-CABLE are detected by their known endpoint naming conventions and can be selected with automatic-pair shortcuts. This is optional convenience behavior. Other third-party virtual-audio drivers are supported by selecting their playback/render and recording/capture endpoints manually. PhoneVR deliberately does not infer arbitrary pairs from matching friendly names, because ordinary physical devices can expose identical render and capture names. PhoneVR stores endpoint IDs in `%LOCALAPPDATA%\PhoneVR\audio-routing.json` and passes the selected IDs to the bridge. Saving the route restarts only the audio helper, leaving the Host network, video, pose and USB AOA paths active.

A regular physical speaker is valid as a PC-to-phone loopback source. For the phone-microphone uplink, choose a virtual-audio render endpoint with a paired capture endpoint. Selecting a physical microphone as the capture side does not make that hardware input writable by PhoneVR. For every non-VAC/VB-CABLE driver, users must select both sides of the same third-party virtual route, even when their friendly names happen to match.

### VAC and VB-CABLE convenience detection

For Eugene Muzychenko VAC, a standard render/capture pair often uses the same friendly name, such as `Line 1 (Virtual Audio Cable)`. For VB-CABLE, the pair is `CABLE Input` playback/render and `CABLE Output` recording/capture. The Host can automatically detect these common conventions. When multiple lines exist, VAC pairing compares the complete normalized name so Line 1 is not crossed with Line 2.

The optional VB-Audio VB-CABLE installer remains a fallback for users who have not configured another phone-microphone route. If an arbitrary third-party render/capture pair has already been manually selected, the installer is suppressed to avoid installing an unnecessary second virtual driver. The original VB-Audio installer is downloaded from its official source, its Authenticode signature and expected publisher are verified, and the vendor package is kept unchanged. VB-CABLE remains a VB-Audio donationware product; professional deployments must follow the vendor's current distribution and licensing terms. See the [VB-Audio distribution and licensing terms](https://vb-audio.com/Services/licensing.htm).

The Host does not rename third-party endpoints to `PhoneVR Microphone`. Windows applications display the original vendor endpoint name selected in the recording/capture list.

## Host user interface

Open **Menu → Audio & Microphone**.

- The first selector is labelled **PC → Phone** and chooses any Windows playback endpoint for WASAPI Loopback.
- The second selector is labelled **Phone microphone → PC write target** and chooses the virtual-audio playback/render side that receives the phone microphone PCM.
- The third selector is labelled **Phone microphone → PC recording endpoint** and chooses the corresponding capture side that Windows applications read.
- Active endpoints are shown normally, disabled/unplugged endpoints are marked and cannot be saved. Enable a disabled endpoint in Windows Sound settings and refresh the list.
- The status panel displays the current endpoint choices, explains the signal direction and warns when the loopback source may be the same as the phone-microphone render target.
- VAC and VB-CABLE automatic pairing remains available, but all enumerated Windows endpoints can be selected manually.

After a route is active, select the chosen capture endpoint as the microphone in VRChat or another Windows application. For standard VAC this may be `Line N (Virtual Audio Cable)`; for VB-CABLE it is `CABLE Output (VB-Audio Virtual Cable)`; third-party drivers retain their own original names.

## Formats, buffering and latency

- PC downlink: 48,000 Hz, stereo, signed 16-bit little-endian PCM, 960 samples per channel per 20 ms frame.
- Phone uplink: 48,000 Hz, mono, signed 16-bit little-endian PCM, 960 samples per 20 ms frame. The bridge duplicates the mono stream to stereo when writing to the virtual cable render endpoint.
- Host uplink queue: bounded to 16 frames. Android capture queue: four frames. Android playback queue: 12 frames with two-frame startup prebuffer. The native render helper uses a bounded 12-frame queue and drops the oldest frame on overflow.
- PCM transport bandwidth before packet overhead: approximately 1.536 Mbit/s downlink and 0.768 Mbit/s uplink.

Opus encoding, clock-drift correction and measured end-to-end latency remain future optimizations. Users can manually select their preferred render and capture endpoint IDs.

## Build and validation

Build the Host and the native WASAPI bridge together on Windows with Go, CMake and Visual Studio C++ x64 tools:

```powershell
.\scripts\build-windows-host.ps1
```

This builds `PhoneVRAudioBridge.exe`, embeds it into both Host executables and builds the Windows Host package. The third-party virtual audio driver is not modified by the build. Users can select any existing virtual-audio endpoint pair; the original VB-CABLE signed installer is offered only as an optional fallback when no custom phone-microphone route has been configured.

The Go Host lifecycle and packet routing are covered by Go tests, race tests and `go vet`; Windows amd64 cross-compilation validates the Host package. Endpoint routing configuration is covered by tests for arbitrary third-party endpoint IDs, render/capture flow validation, disabled endpoint rejection and independent direction persistence. VAC/VB-CABLE automatic pairing remains covered by the legacy endpoint tests. This Linux build environment does not provide MSVC, Windows SDK, PowerShell, a Windows audio stack or physical Android hardware. Consequently it cannot compile the native WASAPI helper or perform UAC installation. A Windows native build, signature check of the downloaded vendor installer, microphone capture test, loopback playback test and simultaneous video/pose/audio load test remain required before production use.
