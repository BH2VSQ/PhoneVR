# PhoneVR audio streaming architecture

Status: 0.5.35 contains the Android PCM audio engine, independent LAN/USB audio transport, Windows WASAPI loopback bridge, Host lifecycle supervision, virtual-cable microphone routing and a native Host Audio & Microphone manager. Windows WASAPI native compilation and real-device full-duplex validation still require a Windows build machine.

## Transport and isolation

PhoneVR uses one independent 48 kHz PCM audio path over LAN UDP `48110` and USB AOA channels `4/5`. The existing video UDP port `48104`, pose UDP port `48102`, control TCP port `48101`, H.264 queues and AOA video framing are unchanged. Audio capture, playback, buffering and restart handling stay outside the video and pose worker paths.

The Windows Host starts a separately supervised `PhoneVRAudioBridge.exe` with the Host service. `HostService.Stop` cancels the helper, closes both pipes, waits for both I/O workers and reaps the process. Device invalidation causes a bounded automatic restart. The Android audio engine uses `AudioTrack` for stereo playback and `AudioRecord` for mono microphone capture.

## PC playback to phone

1. The helper opens the Windows default console render endpoint in WASAPI shared-mode loopback.
2. WASAPI packets are converted to 48 kHz stereo PCM16 and grouped into 960-sample-per-channel, 20 ms frames.
3. The Host reads `PVRA` records from the helper's stdout and calls `PublishAudioPCM`.
4. LAN sends the audio frame through UDP `48110`; USB sends it over AOA channel `4`.
5. Android validates the packet, keeps a bounded jitter queue and writes it to a low-latency stereo `AudioTrack`.

A virtual microphone cable must never become the loopback source for this path. Some virtual cable installers can make their own playback endpoint the Windows default. The helper therefore detects VAC/VB-CABLE render endpoints and chooses an active non-virtual playback endpoint for game-audio loopback if the Windows default points to the microphone cable. This avoids capturing the phone microphone and feeding it back to the phone.

The first version captures the effective default physical render mix. Applications forced to a different per-app render endpoint are not included unless that endpoint becomes the effective default. Shared-mode WASAPI loopback and the helper's current endpoint selection are independent from the virtual microphone route.

## Phone microphone to Windows applications

1. Android `AudioRecord` captures 48 kHz mono PCM16 in 20 ms frames. Platform AEC and noise suppression are enabled when available.
2. LAN sends frames to Host UDP `48110`; USB sends over AOA channel `5`.
3. The Host validates packets and publishes them to a bounded queue. When full, the oldest frame is discarded to limit latency.
4. The Windows Host writes PCM frames to the helper's stdin.
5. The helper opens the selected virtual cable's playback/render endpoint in WASAPI shared mode and writes the phone microphone samples into it. The paired recording/capture endpoint is then available to VRChat, SteamVR and other Windows recording applications.
6. The helper publishes the actual capture endpoint ID to the Host. The Host writes it to `%LOCALAPPDATA%\PhoneVR\audio-record-endpoint.txt`; the SteamVR driver refreshes OpenVR's default recording endpoint property at runtime.

### Existing Virtual Audio Cable (VAC)

Eugene Muzychenko's Virtual Audio Cable exposes paired playback and recording endpoints. A standard pair uses the same friendly name on both sides, for example `Line 1 (Virtual Audio Cable)`. The Windows helper enumerates active render and capture endpoints, matches the full normalized friendly name and uses the matching pair. It does not reinstall, rename, patch, update or uninstall an existing VAC driver.

When both VAC and VB-CABLE are present, the current automatic policy prefers VAC. When multiple VAC lines are active, the first complete pair returned by Windows endpoint enumeration is selected. If a VAC pair exists but either endpoint is disabled, Host reports the installed pair and asks the user to enable it rather than offering to install another virtual cable. The detected provider and both endpoint friendly names are shown in Host logs and in **Audio & Microphone**.

### VB-Audio VB-CABLE fallback

If no supported VAC or VB-CABLE pair is active, the Host Audio & Microphone manager offers the official VB-Audio VB-CABLE package as a fallback. The Host downloads the original vendor package from `https://vb-audio.com/Cable/`, extracts it without modifying its contents, validates the installer Authenticode signature and expected VB-Audio publisher, then starts the original installer through Windows UAC. The Host never changes the signed INF, CAT or SYS files.

With VB-CABLE, phone microphone audio is rendered to `CABLE Input` and Windows applications record from `CABLE Output`. VB-CABLE remains identifiable as a VB-Audio product and follows the vendor's donationware/licensing conditions. For professional deployments where end users cannot independently identify or support the donationware, obtain the appropriate VB-Audio license. See the current [VB-Audio distribution and licensing terms](https://vb-audio.com/Services/licensing.htm).

A VAC installation and a VB-CABLE installation are both independent Windows audio devices. PhoneVR reuses an installed compatible pair before offering the fallback installer. If the pair is installed but disabled, PhoneVR asks the user to enable it instead of installing a duplicate driver. It does not create a custom kernel audio driver or claim that the third-party endpoint is renamed to `PhoneVR Microphone`; Windows recording applications display the endpoint's original vendor name.

## Host user interface

Open **Menu → Audio & Microphone**.

- If a compatible VAC or VB-CABLE pair is active, Host displays the provider and the selected playback/recording endpoint names. It does not offer a redundant driver installation.
- Endpoint detection works even when Host streaming is stopped. The menu starts the embedded helper once with `--probe`, which enumerates Windows MMDevice endpoints without opening audio streams. The probe includes disabled endpoint records, so an installed-but-disabled VAC/VB-CABLE is reported as installed and the user is asked to enable its paired endpoints. PhoneVR does not offer a second driver installation in that state.
- If an installed pair is present but disabled, Host shows its endpoint names and asks the user to enable both endpoints without offering another installation.
- If no compatible pair is installed, Host offers the official signed VB-CABLE fallback installer.
- Installation runs outside the UI thread. The result and Authenticode publisher are reported in Runtime Logs.

After a route is active, select its capture endpoint as the microphone in VRChat or another Windows application. For standard VAC this is the matching `Line N (Virtual Audio Cable)` recording endpoint. For VB-CABLE it is `CABLE Output (VB-Audio Virtual Cable)`.

## Formats, buffering and latency

- PC downlink: 48,000 Hz, stereo, signed 16-bit little-endian PCM, 960 samples per channel per 20 ms frame.
- Phone uplink: 48,000 Hz, mono, signed 16-bit little-endian PCM, 960 samples per 20 ms frame. The bridge duplicates the mono stream to stereo when writing to the virtual cable render endpoint.
- Host uplink queue: bounded to 16 frames. Android capture queue: four frames. Android playback queue: 12 frames with two-frame startup prebuffer. The native render helper uses a bounded 12-frame queue and drops the oldest frame on overflow.
- PCM transport bandwidth before packet overhead: approximately 1.536 Mbit/s downlink and 0.768 Mbit/s uplink.

Opus encoding, configurable endpoint selection when multiple virtual cable pairs are installed, clock-drift correction and measured end-to-end latency remain future optimizations.

## Build and validation

Build the Host and the native WASAPI bridge together on Windows with Go, CMake and Visual Studio C++ x64 tools:

```powershell
.\scripts\build-windows-host.ps1
```

This builds `PhoneVRAudioBridge.exe`, embeds it into both Host executables and builds the Windows Host package. The third-party virtual cable driver is installed through its original signed vendor installer only when Host does not detect a compatible existing pair.

The Go Host lifecycle and packet routing are covered by Go tests, race tests and `go vet`; Windows amd64 cross-compilation validates the Host package. Endpoint matching policy is covered by tests for standard VAC pairs, VB-CABLE pairs and incomplete/mismatched pairs. This Linux build environment does not provide MSVC, Windows SDK, PowerShell, a Windows audio stack or physical Android hardware. Consequently it cannot compile the native WASAPI helper or perform UAC installation. A Windows native build, signature check of the downloaded vendor installer, microphone capture test, loopback playback test and simultaneous video/pose/audio load test remain required before production use.
