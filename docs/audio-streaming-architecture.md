# PhoneVR audio streaming architecture

Status: 0.5.35 now contains the Windows WASAPI loopback bridge, Host lifecycle supervision, Android PCM playback/recording, and a SysVAD WaveRT patch for the `PhoneVR Microphone` capture endpoint. The Windows user-mode and kernel components still require a native Windows MSVC/WDK build, test-signing, installation and hardware validation before the Windows microphone endpoint can be considered operational on a target PC.

## 0.5.35 implementation

PhoneVR uses one independent 48 kHz PCM audio path over LAN UDP `48110` and USB AOA channels `4/5`. The existing video UDP port `48104`, pose UDP port `48102`, control TCP port `48101`, H.264 queues and AOA video channel are unchanged.

The Windows Host starts a separately supervised `PhoneVRAudioBridge.exe` with the Host service. The bridge uses WASAPI shared-mode loopback on the current default console render endpoint, converts the device mix to signed 16-bit stereo PCM at 48 kHz, and emits exact 20 ms frames. A second bridge thread receives mono PCM frames from the Host pipe and writes them into the `PhoneVR Microphone` driver's shared ring. The Host cancels, closes and reaps the bridge process and both pipe workers during `HostService.Stop`; endpoint invalidation causes a bounded automatic restart.

The Android audio engine remains independent of video decode and pose processing. It uses `AudioTrack` for stereo playback and `AudioRecord` for mono microphone capture. It sends a silent registration frame at session start so the Host learns the ephemeral UDP return address even when Android microphone permission is denied. LAN and AOA continue to use the same 24-byte audio packet header and fixed 20 ms PCM frames.

## PC playback to phone

1. `PhoneVRAudioBridge.exe` opens the Windows default console render endpoint with `AUDCLNT_SHAREMODE_SHARED` and `AUDCLNT_STREAMFLAGS_LOOPBACK | AUDCLNT_STREAMFLAGS_EVENTCALLBACK`.
2. `IAudioCaptureClient` packets are converted to 48 kHz stereo PCM16, grouped into 960-sample/channel frames.
3. The Host reads `PVRA` records from the helper's stdout and calls `PublishAudioPCM`.
4. LAN sends the audio frame to the phone's registered UDP source port on the independent audio socket. USB sends over AOA channel `4`.
5. Android validates the packet, keeps a bounded jitter queue, prebuffers two frames and writes to a low-latency stereo `AudioTrack`.

The first version captures the Windows default console render mix. Applications forced to a different per-app render endpoint are not included unless that endpoint is made the default or a later endpoint-selection feature is added. WASAPI loopback requires a shared-mode render endpoint; Microsoft's event-driven loopback support is available on Windows 10 version 1703 and newer. On older Windows releases an additional render-event workaround would be required.

## Phone microphone to Windows applications

1. Android `AudioRecord` captures 48 kHz mono PCM16 in 20 ms frames. The platform AEC and noise suppressor are enabled when available.
2. LAN sends frames to Host UDP `48110`; USB sends over AOA channel `5`.
3. The Host validates packets and publishes them to a bounded 16-frame queue. When full, the oldest frame is discarded to limit latency.
4. The Windows Host writes PCM frames to the helper's stdin. The helper opens the kernel-created `Global\PhoneVR.Audio.UplinkV1` section and acts as the single producer of a bounded 64-slot SPSC ring.
5. The SysVAD `PhoneVR Microphone` WaveRT capture miniport creates the named section with an ACL for SYSTEM and interactive users. A PASSIVE_LEVEL system thread consumes shared-ring packets into a bounded nonpaged queue. WaveRT's capture-buffer worker reads aligned PCM bytes from that queue and zero-fills underruns.
6. Windows applications select **PhoneVR Microphone** in their recording-device settings. The SteamVR driver also publishes the discovered playback and recording endpoint IDs using the OpenVR audio properties.

The SysVAD patcher is pinned to Microsoft's `Windows-driver-samples` commit `d49bb7d72f3651952b2b6e4ad28d14fe2247eb69`. It adapts the `TabletAudioSample` capture miniport, disables unrelated sample endpoints in the produced package, restricts the capture pin to exactly one mono 48 kHz PCM16 format and one underlying capture stream, and sets the endpoint friendly name to `PhoneVR Microphone`. It creates the global memory section from kernel mode so the normal desktop Host process only has to open the existing mapping and does not need `SeCreateGlobalPrivilege`. The WaveRT DPC uses DPC-level spin-lock operations because the sample updates the capture DMA buffer from a DISPATCH_LEVEL timer callback.

## Build on Windows

Build the virtual microphone endpoint on a Windows development PC with Visual Studio 2022, Desktop development with C++, Windows 10/11 SDK, matching WDK and Git. Clone and check out the pinned Microsoft sample revision:

```powershell
git clone https://github.com/microsoft/Windows-driver-samples.git C:\src\Windows-driver-samples
git -C C:\src\Windows-driver-samples checkout d49bb7d72f3651952b2b6e4ad28d14fe2247eb69
git -C C:\src\Windows-driver-samples submodule update --init
.\windows-audio-driver\Build-PhoneVRMicrophone.ps1 `
  -WindowsDriverSamplesRoot C:\src\Windows-driver-samples -Configuration Debug
```

To install a development-signed package, enable Windows test signing, reboot, then run the same script from an elevated PowerShell window with `-Install`. The script verifies the pinned source commit and checks that test-signing is enabled, then uses WDK DevCon to create the componentized root device `Root\sysvad_ComponentizedAudioSample`. Passing `-DevconPath` is supported when DevCon is not discoverable in the normal WDK tools directories. For production distribution, the driver must be signed through the appropriate Microsoft Hardware Dev Center workflow.

Build the Host and the native WASAPI bridge together from the PhoneVR repository root:

```powershell
.\scripts\build-windows-host.ps1
```

That script builds `PhoneVRAudioBridge.exe` with CMake/MSVC, embeds it into both Host executables, runs Go tests and `go vet`, and creates the Windows x64 Host binaries. The SysVAD driver is installed separately because it has an independent kernel signing and installation lifecycle.

## Formats and latency

- PC downlink: 48,000 Hz, stereo, signed 16-bit little-endian PCM, 960 samples per channel per 20 ms frame.
- Phone uplink: 48,000 Hz, mono, signed 16-bit little-endian PCM, 960 samples per 20 ms frame.
- Shared ring: 64 fixed 20 ms slots, single producer and single consumer. WaveRT has an additional eight-frame nonpaged queue. On overflow, it drops complete audio frames to preserve 16-bit PCM sample alignment. The capture miniport accepts only 48 kHz mono PCM16, matching the 1,920-byte phone uplink frame size.
- Host uplink queue: 16 frames. Android capture queue: four frames. Android playback queue: 12 frames with two-frame startup prebuffer.
- PCM transport bandwidth before packet overhead: approximately 1.536 Mbit/s downlink and 0.768 Mbit/s uplink.

Opus encoding, an adjustable jitter buffer, clock-drift correction and measured end-to-end latency remain future optimizations. PCM is used so the first Windows and Android implementation does not depend on a device-specific codec extension.

## Validation boundary

The Host Go lifecycle and packet routing are covered by Go tests, race tests and `go vet`; Windows amd64 cross-compilation validates the Host package. The SysVAD patcher has fixture tests for all source transformations and idempotence, and the shared-memory ABI header is compiled independently. This Linux build environment does not provide MSVC, Windows SDK, WDK, PowerShell, a target Windows audio stack or physical Android hardware. Consequently, it cannot produce or install the `.sys` package or compile the WASAPI helper here. A Windows native build, test-signing/install, microphone capture test, loopback playback test and simultaneous video/pose/audio load test remain required before production use.


## UDP datagram sizing

PCM access units are kept unchanged for USB AOA. On LAN, each 20 ms PVRA access unit is split into checksummed PVAF datagrams no larger than 1,200 bytes. The receiver accepts out-of-order fragments, discards incomplete assemblies after 180 ms, and caps concurrent assemblies at 16. This avoids relying on IP fragmentation across Wi-Fi, VPN and IPv6 paths.
