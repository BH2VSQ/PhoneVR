# PhoneVR Microphone virtual recording endpoint

This folder integrates the phone microphone uplink with a Windows WaveRT capture endpoint. The Windows audio endpoint is implemented by adapting Microsoft's SysVAD `TabletAudioSample` capture miniport. The PhoneVR patch narrows that sample to one capture endpoint, names it **PhoneVR Microphone**, and replaces the sample tone generator with the incoming 48 kHz mono PCM stream.

## Audio path

```text
Android AudioRecord (48 kHz, mono, PCM16, 20 ms)
        |
        v
PhoneVR audio packet transport (UDP 48110 or USB AOA channel 5)
        |
        v
HostService AudioUplinkFrames bounded queue
        |
        v
PhoneVRAudioBridge.exe stdin
        |
        v
Global\PhoneVR.Audio.UplinkV1 shared-memory SPSC ring
        |
        v
SysVAD PASSIVE_LEVEL worker thread
        |
        v
nonpaged WaveRT capture queue
        |
        v
PhoneVR Microphone Windows recording endpoint
```

The separate PC playback path is:

```text
Windows default render endpoint -> WASAPI shared-mode loopback
        -> 48 kHz stereo PCM16 / 20 ms -> PhoneVR Host
        -> UDP 48110 or USB AOA channel 4 -> Android AudioTrack
```

The WASAPI helper is a separate process. Its stdout carries framed audio/status records and its stdin carries fixed-size 20 ms mono PCM uplink frames. Standard error is used for diagnostics. The Host supervises the helper from `HostService.Start/Stop`, with automatic restart after endpoint invalidation.

## Build the PhoneVR Microphone driver

Kernel audio endpoints require the Windows Driver Kit and a signed driver package. The Linux/CI Host build cannot compile or install this `.sys` driver. Build this part on the target Windows development PC.

1. Install Visual Studio 2022 with **Desktop development with C++**, Windows 10/11 SDK, and the matching Windows Driver Kit.
2. Clone Microsoft's Windows Driver Samples at the reviewed commit and initialize its submodules:

   ```powershell
   git clone https://github.com/microsoft/Windows-driver-samples.git C:\src\Windows-driver-samples
   git -C C:\src\Windows-driver-samples checkout d49bb7d72f3651952b2b6e4ad28d14fe2247eb69
   git -C C:\src\Windows-driver-samples submodule update --init
   ```

3. From an elevated or ordinary PowerShell session, patch and build the one-endpoint SysVAD package:

   ```powershell
   .\windows-audio-driver\Build-PhoneVRMicrophone.ps1 `
       -WindowsDriverSamplesRoot C:\src\Windows-driver-samples `
       -Configuration Debug
   ```

4. For a development machine, enable test signing and reboot. Then install the generated INF from an elevated PowerShell session:

   ```powershell
   bcdedit /set testsigning on
   # Reboot Windows after changing the boot setting.
   .\windows-audio-driver\Build-PhoneVRMicrophone.ps1 `
       -WindowsDriverSamplesRoot C:\src\Windows-driver-samples `
       -Configuration Debug -Install
   ```

   The build/install script locates the WDK `devcon.exe` and creates the componentized SysVAD root device `Root\sysvad_ComponentizedAudioSample`. This step is required because staging the INF with `pnputil /add-driver` alone does not create the virtual adapter. When DevCon is outside the standard WDK tools path, pass `-DevconPath C:\path\to\devcon.exe`.

   The driver package must be signed with a test certificate trusted by that development machine. Do not disable Secure Boot or enable test-signing on a production machine without understanding the security implications. For production distribution, obtain a Microsoft-trusted driver signature through the appropriate Hardware Dev Center workflow.

5. Open **Settings > System > Sound > Input** and verify that **PhoneVR Microphone** appears. Select it as the input device in VRChat or the target Windows application.

The patcher creates `*.phonevr.bak` backups beside each modified SysVAD source file. It is safe to rerun after the patch has been applied.

## Build the Host and WASAPI bridge

On Windows, install Go 1.22+, CMake 3.21+, and Visual Studio C++ Build Tools. Run:

```powershell
.\scripts\build-windows-host.ps1
```

The build compiles `windows-host/native-audio/PhoneVRAudioBridge.cpp`, embeds `PhoneVRAudioBridge.exe` into both Host executables, and runs the Go tests and `go vet`. `-SkipWindowsAudioBridge` is intended only for development builds that deliberately omit audio capture.

At runtime the helper captures the current Windows **default console render endpoint** using WASAPI loopback. It periodically checks for the **PhoneVR Microphone** capture endpoint and publishes the Windows endpoint IDs to `%LOCALAPPDATA%\PhoneVR`. The SteamVR HMD driver refreshes OpenVR's `Prop_Audio_DefaultPlaybackDeviceId_String` and `Prop_Audio_DefaultRecordingDeviceId_String` while SteamVR is running.

## Format and latency

- PC downlink: 48,000 Hz, stereo, signed 16-bit little-endian PCM, 960 samples/channel per 20 ms frame.
- Phone uplink: 48,000 Hz, mono, signed 16-bit little-endian PCM, 960 samples per 20 ms frame.
- The kernel miniport reads the shared section only in a PASSIVE_LEVEL worker. The WaveRT timer/DPC reads a private nonpaged queue and outputs silence on underrun. The PhoneVR endpoint exposes exactly one 48 kHz mono PCM16 format and one underlying capture stream because the shared-memory bridge is single-consumer; Windows Audio Engine handles shared-mode fan-out to multiple applications. The queue is bounded and drops stale complete 20 ms frames on overflow to avoid accumulating speech latency. The WaveRT DPC uses the AtDpcLevel spin-lock APIs when it already runs at DISPATCH_LEVEL.
- The current transport remains PCM. LAN UDP frames are split into checksummed PVAF datagrams no larger than 1,200 bytes and reassembled within a bounded 180 ms window; USB AOA retains the original PVRA frame. Opus encoding, echo cancellation on the Windows capture path, and audio clock drift correction are subsequent optimizations.

## Validation boundary

The Go Host, transport and protocol can be tested in a non-Windows environment. The WASAPI bridge requires Windows and MSVC. The SysVAD endpoint requires the WDK, test signing, and a reboot/install cycle. This repository change includes the source integration and build/install procedure, but a `.sys` binary is not shipped because it must be built and signed for the target Windows SDK/WDK environment.
