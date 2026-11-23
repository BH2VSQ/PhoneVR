## 0.5.16 - Cross-link video fragment aggregation and repair

- Replace full-fragment duplication in parallel video mode with weighted fragment striping across the independently probe-verified network UDP and USB AOA Bulk routes. Each distinct PVRV fragment is sent on one route under normal operation; one-datagram access units remain on network.
- Estimate per-route capacity from Android-reported receive goodput and measured assigned-byte shares. Keep scheduler credit scoped to each persistent device ID. Preserve a network fragment in multi-fragment access units so the receiver can recognize frame progress while AOA is delayed.
- Extend Android reassembly with a bounded frame-order window for differing path latency, plus a 120 ms grace for sequence gaps where an entire prior frame has not arrived yet.
- Add control-channel `video_nack` reports and Host access-unit retention. Missing PVRV fragments are retransmitted over the alternate route from a bounded 16-frame / 16 MiB cache with a 700 ms lifetime.
- Scope retained frame cache entries by both device ID and frame ID, avoiding collisions when multiple Android clients use overlapping frame counters.
- Fix the Android CI compile error in `UsbAccessorySession.kt` by importing `java.io.IOException`.
- Update Android to `versionName 0.5.16`, `versionCode 51`, and synchronize repository, Host, AOA descriptor and SteamVR driver version metadata.
- Go tests, race tests, vet and Windows amd64 cross-build pass. Standalone Kotlin reassembler compilation passes. Full Android Gradle build and target-device performance tests remain pending.

## 0.5.15 - Persistent transport modes, AOA reconnect stability and mirrored video

- Make the Android AOA video-backup connection start idempotent. Duplicate startup and USB-accessory attach notifications no longer invalidate an in-progress permission request or queue another overlapping connection worker. Store the worker Future and cancel it on disconnect so an obsolete permission wait cannot block the next AOA reconnect.
- After Android grants USB accessory permission, resolve the current accessory enumeration again, confirm permission against that instance, and retry `openAccessory()` for transient accessory re-enumeration and descriptor-open failures. Add explicit permission, enumeration and open diagnostics.
- Add a Windows Host transport configuration file under the user's PhoneVRStream configuration directory. The selected mode and automatic-mode preference are saved when changed and restored after Host restart. First-run default remains Network only.
- Add **Network + USB AOA parallel** video mode. For every H.264 access unit, Host sends identical PVRV fragments over each currently probe-verified path. USB mirror writes use an independent bounded worker so a slow Bulk OUT operation cannot stall UDP delivery; a full USB queue skips only the redundant copy while preserving the network send. USB readiness probes also use the independent worker so standby AOA checks cannot stall UDP. Android feeds both copies into the existing receiver and deduplicates by frame ID and fragment index before the single MediaCodec decoder.
- Keep the two mirrored routes independently healthy. A route is added only after its own PVRP probe acknowledgement, removed after its probe health expires or a write fails, and automatically rejoined after its receive path responds again. The surviving route continues carrying the frame stream.
- Preserve Automatic failover, USB AOA only and Network only behavior. The Host status panel distinguishes mirrored delivery, a single surviving route and a route waiting to recover.
- Display the current network and USB AOA connection state in both the Android connection page and menu. Clarify Host fragment metrics as total sent fragments across all selected paths.
- Add Host regression tests for configuration persistence, independent path probe gating, byte-identical UDP/AOA mirrored video fragments and continued UDP delivery when the bounded AOA mirror queue is full.
- Android release metadata is `versionName 0.5.15`, `versionCode 50`; synchronized Host, AOA descriptor, SteamVR driver and repository versions are `0.5.15`.
- Go unit tests, race tests, `go vet` and Windows amd64 cross-build pass. Android Gradle compilation and APK packaging remain unverified in this environment because Gradle distribution download was blocked by unavailable outbound DNS; physical AOA reconnect and dual-path testing remain required.

**Parallel mode mirrors the stream for redundancy. It does not stripe fragments to add the two links' bandwidth together.**

## 0.5.14 - Virtual HMD height and network-first default

- Set the 3DoF SteamVR HMD virtual position to 1.65 m above the standing floor, including its initial untracked pose, preventing the headset origin from being placed at floor level.
- Keep the DisplayRedirect device origin unchanged; the height offset applies only to the tracked HMD.
- Change the Host default video transport to Network only, with Network as the default preference. USB AOA and automatic failover remain available when explicitly selected.
- Add release and Go regression tests for the HMD height and network-only default.
- Increment Android versionName to 0.5.14 and versionCode to 49 to keep release metadata synchronized.

## 0.5.13 - Dual-path USB AOA and network video failover

- Added a Windows Host video-transport selector with **Automatic failover**, **USB AOA only** and **Network only** modes. Automatic mode also lets the user choose USB AOA or network as the preferred initial route.
- The Android client now advertises a persistent device ID. Its LAN control/pose connection and an independent AOA video-only session can stay connected at the same time and are grouped as one phone by the Host.
- Both paths remain available while the Host sends each H.264 access unit over only the selected path. Android forwards AOA Bulk video packets into the same UDP receiver and MediaCodec pipeline used for LAN video, avoiding duplicate decoding and competing frame queues.
- Added per-path heartbeats, Android receiver feedback and one-second `PVRP` probes over each standby video path. The Host verifies the actual UDP or AOA Bulk receive path before selecting a standby route, so a healthy control connection cannot mask a broken video data path. Explicit write failures and silent active-path receive timeouts trigger failover without restarting capture or the H.264 encoder.
- On a path change, the Host drops queued delta frames and waits for the next IDR before sending on the new route. Android continues to accept both Host UDP packets and AOA loopback packets.
- The AOA auxiliary session also sends head pose only while the network TCP control route is unavailable, so a complete LAN control outage can keep orientation tracking alive over USB. It remains silent during healthy LAN operation to avoid duplicate pose samples.
- Bumped Android `versionName` to `0.5.13` and `versionCode` to `48`; synchronized the Host executable, repository version, AOA descriptor, SteamVR Driver CMake project version and runtime diagnostic marker.
- `go test ./...`, `go test -race ./...`, `go vet ./...` and Windows amd64 Host cross-build pass. Added automatic generation of the project-owned Gradle bootstrap JAR from checked-in Java source on both Windows and Unix launchers, with SHA-256 validation of the pinned Gradle distribution. Android Gradle compilation, APK packaging and physical USB/network failover tests remain pending until run on a network-enabled Android SDK environment.

## 0.5.12 - Fix Android AOA USB packet truncation and add wire diagnostics

- Fixed the Android AOA receive path to request 16 KiB raw reads before parsing PVRU frames. The previous code requested only 16 bytes for a frame header, which was smaller than a USB bulk packet and could cause the remainder of that packet to be discarded by the AOA file-descriptor read path.
- Added `UsbMuxStreamDecoder` to reassemble frames split across reads and multiple frames returned in a single read. Incomplete headers and payloads remain buffered until complete.
- Added Android diagnostics for raw Bulk IN read sizes, PVRU channel/sequence, payload CRC32, and invalid raw header bytes.
- Added Windows Host diagnostics for PVRU TX/RX frame lengths and CRC32, plus actual `WinUsb_WritePipe` requested/transferred byte counts, endpoint, sample bytes and errors.
- Serialized Android TX sequence assignment with its write lock so the sequence always reflects physical frame order.
- Android `versionName` is `0.5.12`; `versionCode` is `47`. Windows Host, SteamVR driver and AOA descriptor metadata are synchronized.
- Kotlin stream parser tests (9), Windows Go tests, race tests, vet and Windows amd64 cross-build passed. Full Gradle APK packaging and physical USB retest remain pending.

## 0.5.11 - Stabilize asynchronous AOA video dispatcher regression test

- Fixed a race in `UsbVideoPacketDispatcherTest.callbackFailureDoesNotStopFollowingPackets`: the packet callback signals its latch immediately before the dispatcher increments `deliveredPackets`, so the test now waits for the post-callback counter update instead of reading it too early.
- Kept production delivery accounting unchanged. A packet is counted as delivered only after its callback returns successfully; callback exceptions remain isolated and do not stop following packets.
- Synchronized repository, Android, Windows Host, SteamVR driver and AOA accessory metadata to 0.5.11. Android `versionCode` is now 46.
- The supplied CI run compiled Android Debug and Release Kotlin sources successfully; its only reported failure was this timing-sensitive assertion. The corrected unit test still needs a fresh CI run before this revision can be marked green.

## 0.5.10 - Harden USB Welcome framing and expose AOA control diagnostics

- Reworked `ControlLineReader` to consume UTF-8 bytes directly and decode only after a complete newline-terminated control message arrives. This avoids relying on an intermediate `InputStreamReader` decoder buffer while USB polling reads can time out between frames.
- Treat EOF with an unfinished control line as a transport truncation (`EOFException`). A partial Welcome is no longer passed to `JSONObject` as though it were a complete line.
- Preserve the original Android AOA receive-thread failure and surface it through the control input stream after queued control bytes have been drained. This distinguishes USB Bulk IN/frame errors from malformed JSON.
- Added bounded Android AOA Control TX/RX diagnostics with frame sequence, payload length, escaped preview and hex bytes.
- Added Windows Host diagnostics for the serialized Welcome length, first control frames in both directions, USB bridge failures and the underlying AOA Bulk IN stop reason.
- Added regression coverage for timeouts between byte fragments, UTF-8 code points split across reads, CRLF, clean EOF, truncated EOF and the maximum line size.
- Bumped Android `versionCode` to 45 and synchronized Android, Windows Host, SteamVR driver and AOA accessory metadata to 0.5.10.
- Windows Host tests, race tests, vet and Windows amd64 cross-build pass. Android Gradle/APK packaging and physical AOA behavior still require target-device validation.

## 0.5.9 - Preserve fragmented control JSON across USB read timeouts

- Replaced `BufferedReader.readLine()` on LAN and USB control connections with a persistent line reader that retains partially received JSON while the underlying stream reports a polling timeout.
- Fixed the USB-specific 500 ms read polling interaction: a timeout between PVRU control frames could cause `BufferedReader.readLine()` to discard the partial line, leaving a truncated JSON value that then failed with `Value ... of type java.lang.String cannot be converted to JSONObject` and disconnected the AOA session.
- Preserved periodic heartbeat transmission while waiting for control lines by servicing heartbeat deadlines from the line reader's timeout callback.
- Added bounded 64 KiB control-line parsing and diagnostic logging of malformed-message length and escaped preview, including whether the message came from USB welcome, LAN welcome or the established control channel.
- Added regression tests for JSON lines split across multiple USB chunks with injected polling timeouts, CRLF handling, final unterminated input and oversized lines.
- Synchronized repository, Android, Windows Host, SteamVR driver and AOA accessory metadata to 0.5.9. Android `versionCode` is now 44.
- Android Gradle/APK validation remains subject to an available Gradle distribution and Android SDK; physical AOA throughput and reconnect behavior require target-device confirmation.

## 0.5.8 - Asynchronous Android AOA video receive dispatch

- Removed synchronous UDP loopback forwarding from the Android AOA Bulk input thread. Incoming video PVRV packets are handed to a dedicated worker through a bounded 256-packet queue so a slow local forwarding path cannot stop draining USB Bulk IN.
- Added periodic Android diagnostics for accepted, forwarded, dropped and queued AOA video packets, callback failures and maximum forwarding callback duration.
- Added JVM regression tests proving the AOA receive-side handoff is non-blocking under a stalled consumer, remains bounded, and survives callback failures.
- Synchronized repository, Android, Windows Host, SteamVR driver and AOA accessory metadata to 0.5.8. Android `versionCode` is now 43.
- Go unit tests and standalone Kotlin dispatcher compilation pass. Android Gradle unit tests and APK packaging remain unverified because the Gradle 9.6.1 distribution could not be resolved from `services.gradle.org` in this environment.


## 0.5.7 - USB video backpressure isolation and bounded Bulk writes

- Decoupled H.264 stdout parsing from USB/UDP transport writes with a bounded eight-access-unit sender queue. A slow USB Bulk OUT transfer can no longer block FFmpeg stdout consumption and be misclassified as an encoder stall.
- When transport backpressure fills the output queue, stale access units are discarded and delta frames are held until the next IDR keyframe, limiting latency and restoring decoder continuity.
- Added transport-send latency and output-queue-drop telemetry to the periodic video pipeline log.
- Configured a 1500 ms WinUSB Bulk OUT transfer timeout. A failed or partial PVRU write now closes the multiplex session so the device is re-enumerated rather than continuing with a potentially corrupted byte stream.
- Host shutdown now aborts active USB multiplex sessions before waiting for video and USB workers.
- Android `versionName` is 0.5.7 and `versionCode` is 42; repository, Host, driver and AOA descriptors were synchronized.

## 0.5.6 - AOA accessory handshake deadline and response write fix

- Extended the initial control hello timeout to 30 seconds for USB AOA sessions while retaining the 8-second timeout for LAN clients. This gives Android time to display and receive approval for the USB accessory permission prompt after AOA re-enumeration.
- Removed the write deadline that was set before the Host waited for the initial hello. That absolute 5-second deadline could expire before Android completed the accessory permission flow, causing the later welcome response to fail with `write pipe: i/o timeout`.
- Apply a fresh 5-second deadline immediately around each direct JSON response and clear it after the write. Control-message writes now also clear their per-write deadline.
- Added regression tests proving USB gets the extended handshake window and an expired stale write deadline is refreshed before sending the welcome response.
- Synchronized repository, Android, Windows Host, SteamVR driver and AOA accessory metadata to 0.5.6. Android `versionCode` is now 41.
- Go tests, race tests, vet, Windows amd64 Host cross-build and Python regression tests pass. AOA Bulk session and sustained data transfer remain subject to target-device validation.

## 0.5.5 - AOA accessory interface and Bulk endpoint diagnostics

- Confirmed from the 0.5.4 target-PC log that AOA v2 negotiation and `START_ACCESSORY` succeed, and that Redmi K40 re-enumerates as `18D1:2D01`.
- Added visible post-switch interface-descriptor and endpoint-query diagnostics. The previous scanner silently ignored `findBulkEndpoints` failures, so a re-enumerated accessory could be detected without revealing why no Bulk session was created.
- Identify the ADB interface signature on `18D1:2D01` (interface 1, class `FF/42/01`) and explain that PhoneVR Bulk data must use accessory interface 0 (`MI_00`), never ADB interface 1 (`MI_01`).
- Log selected Bulk IN/OUT endpoint addresses and maximum packet size when a valid accessory interface is found.
- Updated Windows driver instructions for the composite `18D1:2D01` device.
- Synchronized repository, Android, Windows Host, SteamVR driver and AOA accessory metadata to 0.5.5. Android `versionCode` is now 40.

- Android versionName and versionCode bumped by the manual release workflow.
- Windows Host version metadata updated from the repository VERSION file.

## 0.5.4 - WinUSB open-path diagnostics

- Replaced the Host's direct `CreateFileW` procedure call with Go's standard Windows `syscall.CreateFile` wrapper while preserving `GENERIC_READ | GENERIC_WRITE`, shared read/write access, `OPEN_EXISTING`, `FILE_ATTRIBUTE_NORMAL` and `FILE_FLAG_OVERLAPPED`.
- When the Host receives `ERROR_ACCESS_DENIED`, it now probes read-only and write-only opens from the same process, reports the actual Host PID and token elevation state, and prints the exact Win32 access/flag values. These probes only open and close handles; they do not issue USB transfers.
- The supplied Windows tests showed that the same device path opens successfully with read/write, read-only and write-only access from PowerShell, and the installed `oem145.inf` contains no explicit `Security` directive. This revision uses that evidence to distinguish a Host-process/API discrepancy from a device ACL problem.
- Synchronized application, Host, SteamVR driver and AOA accessory metadata to 0.5.4. Android `versionCode` is now 39.
- Go tests, race tests, vet, Windows amd64 Host cross-build and 49 Python regression tests pass. Device-side AOA handshake remains pending.

## 0.5.3 - WinUSB open diagnostics and overlapped-handle fix

- Create WinUSB device handles with `FILE_FLAG_OVERLAPPED`, as required by `WinUsb_Initialize`.
- Report `CreateFileW` and `WinUsb_Initialize` failures as separate stages with the Win32 error code.
- Add actionable diagnostics for `ERROR_ACCESS_DENIED` without silently changing driver permissions or requiring permanent elevation.
- Bumped synchronized application, Host, SteamVR driver and AOA accessory metadata to 0.5.3. Android `versionCode` is now 38.

## 0.5.2 - OEM-independent AOA discovery

- Made WinUSB interface discovery generic: the Host reads each installed USB function's `DeviceInterfaceGUID` / `DeviceInterfaceGUIDs` registration and only enumerates interfaces assigned to the WinUSB service. Interface GUIDs are not hard-coded to a particular PC or phone.
- Removed the Android OEM VID allow-list from AOA probing. The Host sends the standard device-level `GET_PROTOCOL` request to present WinUSB functions and proceeds only for AOA protocol v1/v2 responses. Failed probes are rate-limited to once every five seconds per device path.
- Added tests for AOA v1/v2 validation and WinUSB service filtering. Updated the Windows driver guide to clarify ADB-labelled control interfaces, dynamic GUID discovery and hardware-specific AOA support.
- Bumped repository, Android, Windows Host, SteamVR driver and accessory descriptors to 0.5.2. Android `versionCode` is now 37.

## 0.5.1 - Native USB AOA Bulk streaming and enumeration diagnostics

- Added Windows-initiated Android Open Accessory mode negotiation and a full-duplex WinUSB Bulk transport. The USB stream multiplexes control, HMD pose and H.264 video without USB network sharing, RNDIS or ADB data-channel use.
- Added Android accessory attach/permission/session handling and reused the existing Host control, pose, H.264 encoding and Android MediaCodec paths.
- Improved WinUSB device-interface discovery to read both `DeviceInterfaceGUID` and `DeviceInterfaceGUIDs`; added detailed device enumeration and AOA control-transfer diagnostics.
- Fixed a GUID-selection defect found during Redmi K40 testing: the scanner previously fell back to the generic `GUID_DEVINTERFACE_USB_DEVICE` (`A5DCBF10-...`) and attempted WinUSB operations on generic USB device paths. It now enumerates only GUIDs explicitly registered under each USB device instance's `Device Parameters` key, including the reported `F72FE0D4-CBCB-407D-8814-9ED673D0DD6B` interface.
- Synchronized repository, Android, Windows Host, SteamVR driver and AOA accessory descriptor versions to 0.5.1; Android `versionCode` is now 36.
- Go tests, race tests, vet and Windows amd64 Host cross-build pass. Physical AOA switching, sustained USB throughput, Android APK build and native SteamVR runtime validation remain pending on the target devices.

## 0.4.19 - Virtual VSync stability and low-latency follow-up

- Corrected virtual-vsync publication order: `GetTimeSinceLastVsync` sees a VSync only after `WaitForPresent` reaches it. The scheduler now maintains a bounded 72 Hz timeline, advances the virtual frame counter across missed intervals, and serializes wait callbacks.
- Added `early-wait` and `VSync_None`/`VSync_WaitRender`/`VSync_NoWaitRender` counters to the driver timing line so runtime callback pairing and pacing mode are observable.
- Replaced coarse `Sleep(1)` timing with a high-resolution Windows waitable timer when supported, followed by a short yield-spin.
- Reduced the Host's unmatched H.264 timestamp backlog from 12 to 4 frames (about 67 ms at 60 FPS). QSV async depth remains 2 to preserve the throughput measured on the target PC.
- Confirmed that the current HMD render target is already 1600x720 SBS, matching the encoded stream; this revision does not change eye resolution.
- Go tests, race tests, vet, Windows/amd64 Host cross-build and 44 Python release tests pass. Native MSVC driver compilation and target-PC/Android runtime latency validation remain pending.

## 0.4.19 - QSV output stall and nonblocking capture readback follow-up

- Explicitly request NV12 for hardware H.264 encoding, retain YUV420P for software encoders, and use QSV-native AUD insertion.
- Raise QSV asynchronous depth to 4 while retaining a bounded 24-frame timestamp window.
- Upgrade hardware preflight from a 160x90 two-frame initialization check to a full-resolution BGRA test at the configured size and frame rate; reject a full-profile test slower than three seconds.
- Treat a full timestamp queue as recoverable raw-frame backpressure. Add a two-second complete-access-unit stall watchdog, exponential retry backoff, process-exit detection and first-stdout/first-AU logs.
- Launch FFmpeg inventory, preflight and streaming processes with `CREATE_NO_WINDOW` to avoid visible child console windows on Windows.
- Expand Host video telemetry with raw RX FPS, submitted FPS, encoded AU count, FFmpeg stdout bytes and drop counters; sample the first three BGRA frames to distinguish an upstream black frame from an encoder failure.
- Change the background D3D11 staging readback to `D3D11_MAP_FLAG_DO_NOT_WAIT`, requeueing `DXGI_ERROR_WAS_STILL_DRAWING` instead of waiting inside the protected immediate context.
- Go unit tests, race tests, vet, Windows/amd64 Host cross-build and 42 Python release tests pass. Native Windows driver build, real QSV throughput and Android device validation remain pending.

## 0.4.19 - Persistent video encoder and capture backpressure fix

- Moved the FFmpeg H.264 session out of individual driver TCP connection handlers into one Host service-lifetime encoder worker. A transient TCP reconnect no longer closes and respawns FFmpeg.
- Reused the H.264 hardware encoder selected and validated during synchronous Host startup preflight. Streaming startup no longer repeats FFmpeg encoder discovery and hardware test subprocesses on each connection.
- Decoupled loopback TCP reception from FFmpeg stdin writes through a bounded two-frame latest-frame-wins queue with pooled BGRA buffers. When QSV falls behind, stale frames are dropped instead of blocking TCP receive and triggering a reconnect cycle.
- Serialized the driver capture bridge to one active connection, increased the Host TCP receive buffer and idle timeout, and increased the driver's raw-frame send timeout from 250 ms to 5 seconds.
- Added rate-limited WSA socket-error diagnostics and Host input-queue-drop/FPS telemetry. The persistent encoder retries failed sessions with a five-second backoff.
- Go tests, race tests, `go vet`, Windows/amd64 Host cross-build, and 41 Python release regression tests pass. Native MSVC SteamVR driver compilation and end-to-end Windows/Redmi K40 LineageOS validation remain pending.

## 0.4.19 - DisplayRedirect startup-order and HMD override follow-up

- Pre-initialize the DisplayRedirect D3D11 capture device before registering tracked devices, then register DisplayRedirect before the PhoneVR HMD.
- Publish the HMD-facing `Prop_HasVirtualDisplayComponent_Bool` and `Prop_GraphicsAdapterLuid_Uint64` override values during pre-initialization, immediately after DisplayRedirect registration and again during activation. Each attempt logs its phase and OpenVR property result codes.
- Read back the effective virtual-display flag and graphics-adapter LUID from the HMD property container during HMD activation, making the compositor routing prerequisites observable.
- Keep encoder preflight synchronous before opening Host pairing listeners and log that startup stage explicitly.
- Go tests, race tests, `go vet`, Windows/amd64 Host cross-build, and 41 Python regression tests pass. Native MSVC SteamVR driver build and target runtime verification remain pending.

## 0.4.19 - SteamVR DisplayRedirect diagnostic refresh

- Added one-time `GetComponent` request logging for the HMD and DisplayRedirect devices, including requested and expected OpenVR interface versions.
- Added DisplayRedirect activation logging with tracked-device object index, selected graphics-adapter LUID and every display-related property write result.
- Added cumulative DisplayRedirect `GetComponent` query/match counters and the active state to the five-second virtual-display telemetry.
- Made the DisplayRedirect active flag atomic and changed interface-name comparisons to case-insensitive matching.
- This is a diagnostic driver revision. Native MSVC compilation and SteamVR runtime validation are still required on the target Windows machine.

## 0.4.19 - USB tethered streaming and D3D11 build fix

- Fixed the MSVC C2065 build failure by including the Windows SDK `d3d11_4.h` header that declares `ID3D11Multithread`.
- Added Android Auto / Wi-Fi / USB-RNDIS discovery modes. Discovery enumerates active IPv4 interfaces, binds one socket per interface and sends directed broadcasts through each selected interface.
- Reused the existing TCP control, UDP pose and H.264 video transport over USB tethering; no ADB tunnel or USB-debugging authorization is required.
- Added Host network-interface labels and a LocalSubnet-scoped Windows Firewall helper for inbound TCP 48101 and UDP 48100/48102.
- Bumped Android versionCode to 35 and synchronized Android, Host and SteamVR driver metadata to 0.4.19.
- Windows MSVC native driver build and Android Gradle/device validation remain required on target devices.

## 0.4.18 - Integrated SBS H.264 streaming

- Removed the Host display-refresh-rate UI and persistence; virtual display timing is fixed at 72 Hz.
- Added D3D11 GPU scaling and bounded triple-staging capture from the final SteamVR SBS compositor backbuffer, with a loopback-only TCP bridge to Host.
- Added a hardware-only H.264 encoder preflight (NVENC/AMF/QSV); CPU software encoding is disabled for live VR to avoid increasing load on the PC.
- Added Host H.264 Annex-B access-unit packetization over UDP 48104 using PVRV v1; raw BGRA input buffer is reused to reduce allocation/GC load.
- Added Android hardware MediaCodec capability gating, UDP fragment reassembly, bounded decode queue, IDR loss recovery and decoded SBS presentation through the existing per-eye optical warp renderer.
- Host now selects and returns the exact Android hardware decoder name proven to support 1600×720@60; Android instantiates that exact codec instead of relying on the platform's default MIME-type choice. Hardware classification must be reliable, and a dropped H.264 input access unit triggers IDR resynchronization.
- Android versionCode is now 34. Windows MSVC driver build, Android Gradle build and target-device end-to-end video tests remain unverified in this environment.

## 0.4.17 - Pose-state isolation and selectable display refresh

- Moved the high-frequency pose snapshot to a dedicated lock, isolating pose writes from the Host device/status/log mutex.
- Added Host display refresh settings for 60 Hz, 90 Hz, 120 Hz and Custom (60–120 Hz, 0.1 Hz steps); the SteamVR driver reads the persisted rate on startup and applies it to HMD display frequency and virtual VSync timing.
- Default virtual refresh remains 72 Hz (Custom) for backward compatibility; SteamVR must be fully restarted after changing the setting.
- Android versionCode/versionName and Host/driver version metadata updated.

## 0.4.16 - Independent SteamVR pose publishing and timing diagnostics

- Moved HMD pose publication out of `IServerTrackedDeviceProvider::RunFrame()` into a dedicated above-normal-priority worker. It wakes immediately for every new UDP pose generation and republishes the latest pose on a 10 ms keepalive, separating tracking update cadence from `vrserver` main-loop callbacks.
- Added receiver generation signalling and counters for valid driver RX samples, UDP sequence gaps and queued datagrams drained. Added unique/repeated published-pose counters, maximum publish gap, maximum sample age and independent `RunFrame` cadence metrics.
- Added `IVRVirtualDisplay::Present` and `WaitForPresent` callback frequency / maximum-duration diagnostics to distinguish compositor/GPU callback stalls from pose publication stalls.
- Retained the existing 72 Hz virtual-display clock and explicitly report it alongside sensor and runtime update rates; no unverified 90/120 Hz change was made.
- The publisher sends stack-local `DriverPose_t` values and is stopped/joined before the HMD object ID is invalidated. Android sampling, Host packet protocol, Chaperone settings and D3D11 synchronization policy are otherwise unchanged.
- Added regression checks for independent pose publishing and separate compositor timing telemetry. Android versionCode is now 32.
- Native MSVC driver compilation and SteamVR runtime validation remain required on the Windows target; this environment cannot run that native toolchain.

## 0.4.15 - Host pose scheduling and log-view stability

- Cached local IPv4 adapter enumeration at Host service startup. `Snapshot()` no longer calls `net.Interfaces()` while holding the shared service read lock, preventing slow VPN/virtual-adapter queries from blocking the high-rate pose receiver's state update lock.
- Added a `max pose-state lock wait` telemetry field so future logs show whether Host pose publication is stalled waiting for service state synchronization.
- Increased the native Host pose-preview timer from 30 Hz to a 60 Hz target while keeping status metadata refresh throttled to 500 ms.
- Replaced the runtime log's classic EDIT control with Windows RichEdit 5.0 (`Msftedit.dll`) when available, retaining a standard EDIT fallback. Added a 1 MiB text limit, preserved selection and scroll position during refresh, and stopped forwarding a child scrollbar notification back into the log control.
- Added regression coverage for cached network addresses and the log/pose scheduling changes.
- Android versionCode increased to 31. Android Gradle packaging and native Windows MSVC/SteamVR runtime verification remain pending in environments with those toolchains.

## 0.4.14 - Pose burst forwarding and log repaint stability

- Windows Host now forwards every sample in a short UDP receive burst instead of dropping all but the newest sample. Only a burst larger than 32 queued poses is collapsed to its newest sample, bounding stale-backlog replay while preserving native sensor cadence during normal scheduling jitter.
- Host pose telemetry now distinguishes the maximum interval between Host read operations from the maximum Android sensor timestamp interval, and reports the largest receive-batch size. This separates phone sensor/send stalls from Windows receive-thread scheduling pauses; sequence gaps remain a separate packet-loss signal.
- SteamVR's pose receiver now computes angular velocity across every valid sample drained from its loopback queue before publishing the newest orientation, retaining high-rate motion information even when packets arrive in short bursts.
- Raised the Android IMU HandlerThread and dedicated pose-transmit thread to display scheduling priority, with a safe fallback to normal priority if the platform rejects the request.
- Runtime Logs now updates the native EDIT control with redraw suppression and a full invalidation/erase/repaint, preventing stale glyphs from appearing stacked after scrolling or refresh. Mouse-wheel and vertical-scroll messages are routed to the log control rather than the custom dialog scroll handler.
- Added regression coverage for preserving short pose bursts and collapsing only large stale backlogs.
- Android versionCode increased to 30. Android Gradle packaging and native Windows MSVC/SteamVR runtime verification remain pending in environments with those toolchains.

## 0.4.13 - Pose latency, continuity and direct-display diagnostics

- Android aligns every absolute sensor quaternion to the previous sample before calculating relative orientation, avoiding equivalent q/-q representation flips around large pitch rotations.
- Kept sensor collection on the dedicated IMU HandlerThread and increased the latest-sample UDP sender to up to 200 Hz, matching the requested 5 ms sensor period without queueing historical poses.
- Windows Host now drains already-queued UDP pose datagrams in a short bounded window and forwards only the newest valid pose. The pose socket buffer is bounded; telemetry reports RX/forwarded frequency, coalesced packets, sequence gaps, invalid packets, maximum arrival interval and loopback write errors.
- The SteamVR driver continues to drain its loopback socket to the newest pose and now derives angular velocity using differences between Android sensor monotonic timestamps rather than Windows packet-processing intervals. This prevents a queued burst from producing a false sample delta; driver-side drained packet counts are included in pose telemetry.
- Increased angular-velocity filter responsiveness while retaining a speed clamp; populate `vecAngularVelocity` and a bounded negative `poseTimeOffset` so SteamVR can predict toward display time.
- Added periodic SteamVR telemetry for sample age, angular speed, pitch and sequence to correlate rare gray flashes at high head angles with tracking freshness and motion.
- Documented the SteamVR **Settings → Developer → Enable Direct Mode** runtime switch. PhoneVR's HMD reports `IsDisplayOnDesktop=false`; `IVRVirtualDisplay` remains the wireless final-compositor-backbuffer handoff and is not replaced with a driver-owned compositor.
- Added regression tests for quaternion continuity, 200 Hz pose pacing, Host UDP batch coalescing, sensor-clock angular velocity, OpenVR prediction fields and Direct Mode configuration.
- Android versionCode increased to 29. Android Gradle packaging and native Windows MSVC/SteamVR runtime verification remain pending in environments with those toolchains.

## 0.4.12 - HMD connection stability and Android control sync

- Decoupled OpenVR device presence from IMU packet freshness. An activated HMD now remains `deviceIsConnected = true`; stale or missing samples only mark its pose untracked instead of making SteamVR remove and rediscover the device.
- Published an initial connected-but-untracked identity pose during HMD activation so SteamVR receives an explicit device pose before the first phone sensor packet arrives.
- Submit the latest pose on every SteamVR `RunFrame`, even when the UDP sequence has not changed. OpenVR invalidates poses if the driver stops submitting updates, so this prevents a quiet sensor/network interval from aging out an otherwise usable pose.
- Increased the pose freshness grace period from 250 ms to 1000 ms to absorb short Android scheduling and Wi-Fi jitter without producing unnecessary tracking-invalid transitions.
- Added `vrserver.txt` transition logs for IMU tracking becoming stale or resuming, explicitly noting that the HMD remains connected.
- Android now replies to the Host's initial optical-preset catalogue request directly on the TCP control thread rather than queueing a network response on the UI thread.
- Host retries an unanswered optical-preset catalogue request up to three times and logs a missing/empty response without tearing down the connected control or pose session.
- Manual LAN discovery now automatically fills and persists the selected Host IPv4 address and control port; tapping another result still changes the selected target.
- Added regression tests for the HMD connected/untracked lifecycle, Android preset response path, Host address auto-fill, and Host preset-request retry.
- Android versionCode increased to 28. Live SteamVR and Android-device runtime validation remains required.

## 0.4.11 - MSVC Chaperone JSON literal build fix

- Shortened the embedded Chaperone JSON C++ raw-string delimiter from 17 characters to the standard maximum of 16, fixing MSVC C3512.
- Updated the Chaperone regression test to parse the raw-string delimiter generically and explicitly enforce the C++ 16-character limit, preventing this failure from recurring.
- Bumped synchronized Android, Windows Host and SteamVR driver version metadata; Android versionCode is now 27.

## 0.4.10 - Driver-provided Chaperone and stable universe

- Added a valid OpenVR v5 `chaperone_info` JSON directly to the HMD properties, avoiding an external per-user file dependency.
- Added a stable PhoneVR universe ID and set `Prop_CurrentUniverseId_Uint64` to the same identifier used by the JSON `universeID`.
- Defined neutral seated and standing transforms and empty collision bounds, accurately reflecting the current 3DoF orientation-only tracking capability without inventing room-scale position or safety boundaries.
- Checked both OpenVR property-write results and added explicit success/failure diagnostics to `vrserver.txt`; Chaperone failure is non-fatal so HMD pose tracking can continue.
- Added automated JSON/schema, ID-consistency and 3DoF-boundary regression tests.
- Bumped synchronized Android, Windows Host and SteamVR driver version metadata.

## 0.4.9 - SteamVR frame-sync load reduction and diagnostics

- Removed the unused full-resolution D3D11 staging copy from `IVRVirtualDisplay::Present`; P0 has no encoder or frame consumer yet, so the extra 2400x1080 copy was wasted GPU bandwidth and memory traffic.
- Kept the small synchronization staging texture and GPU completion wait, matching the intended fence path used by Valve's virtual-display sample.
- Increased bounded keyed-mutex acquisition timeout from 2 ms to 10 ms to reduce compositor frame drops under cross-process scheduling contention.
- Added diagnostics for enumerated DXGI adapters, selected adapter LUID, shared-backbuffer dimensions/format/sample count, keyed-mutex timeouts and D3D11 Map/device-removal failures. Error logging is rate-limited.
- Retained the live Host heartbeat and completed-frame state reporting. No Android video encoder or transport was added in this version.
- Bumped synchronized Android, Windows Host and SteamVR driver version metadata.

## 0.4.8 - SteamVR driver MSVC compatibility fix

- Removed the invalid `override` specifier from `VirtualDisplayComponent`'s destructor. The pinned OpenVR 2.15.6 `IVRVirtualDisplay` interface has no virtual destructor; the concrete component still runs `Shutdown()` when its owning display device is destroyed.
- Guarded `WIN32_LEAN_AND_MEAN` and `NOMINMAX` definitions so MSVC does not emit C4005 redefinition warnings when CMake already supplies them.
- Bumped the synchronized application and driver version metadata.

## 0.4.7 - P0 SteamVR status and display-frame synchronization

- Replaced the hard-coded SteamVR state with a live loopback driver heartbeat and explicit HMD, tracking, display and completed-frame states.
- Added D3D11 shared-backbuffer opening, bounded keyed-mutex synchronization, full-frame staging copies, GPU-completion waiting and virtual-vsync frame timing.
- Kept the future video stream port 48104 reserved and moved the internal driver heartbeat to loopback UDP 48105.
- Added heartbeat validation, Host integration tests and stale-state expiry checks.
- Bumped Android version metadata and synchronized repository release version to 0.4.7.

## 0.4.6 - Driver manager status readability

- Replaced the SteamVR driver status label with a read-only multiline text control that automatically wraps long paths and supports text selection and copying.
- Increased the status area height and moved the driver action buttons lower to improve vertical spacing.
- Removed the decorative footer frame surrounding the Close button while keeping the button in its original bottom position.

## 0.4.5 - Automated release

- Android versionName and versionCode bumped by the manual release workflow.
- Windows Host version metadata updated from the repository VERSION file.

# Changelog

## 0.4.4 - Bundled SteamVR driver deployment

- Removed the unstable local ZIP picker and ZIP-based driver installation UI from the Windows Host.
- Embedded the compiled PhoneVR OpenVR driver payload into both Windows Host executables using Go `embed.FS`.
- Updated the standard Windows Host build to compile the C++ driver before compiling the Host and stage its package for embedding.
- Added automatic startup deployment when SteamVR is detected, with content-hash version directories, manifest/DLL validation and post-copy verification.
- Added Install / Repair Bundled Driver and Uninstall & Disable Auto Deploy actions, including a persistent user opt-out marker.
- Restricted automatic cleanup to PhoneVR-managed files; external driver files are unregistered but never deleted.
- Removed obsolete ZIP download/extraction code and changed Release packaging to distribute self-contained Host executables without a separate driver ZIP.
- Added cross-platform unit tests for driver package validation, path safety, digest stability and file-copy integrity.

## 0.4.1 - Android codec probe compile fix

- Read `maxSupportedInstances` from `MediaCodecInfo.CodecCapabilities` (the API owner), not `MediaCodecInfo`.
- Convert `VideoCapabilities.supportedFrameRates.upper` from `Int` to `Double` for the normalized capability model.
- Keep the SteamVR driver install flow explicit: build the driver package, then register it with `vrpathreg.exe` using the provided PowerShell helper.

## 0.4.0 foundation - 2026-11-21

- Added Windows x64 OpenVR SteamVR driver foundation: `PhoneVRHmd` (HMD) and `PhoneVRDisplayRedirect` (`IVRVirtualDisplay`).
- Added loopback-only pose bridge on UDP 48103; Go Host forwards validated existing `PVSP` packets without changing Android protocol v1.
- Added Android MediaCodec runtime capability discovery for H.264, HEVC and AV1.
- Added a bounded Host-startup FFmpeg encoder-candidate probe (`-ffmpeg` override supported) and backend selection for NVIDIA NVENC, AMD AMF, Intel QSV and software fallback; candidates are explicitly not treated as usable until adapter/session initialization succeeds.
- Added `PVRV` video datagram v1 framing, MTU-safe access-unit fragmentation, validation and boundary tests, with 1200-byte bounded UDP payloads.
- Added SteamVR driver CMake build, installer helper and Windows CI workflow.
- Video capture/encoding/transport/decoding and Daydream BLE controller pairing remain future implementation tasks.

- Added a dedicated light OpenGL pose-preview palette with a pale background, softer floor grid, and distinct high-contrast XYZ axes and gaze ray; dark mode preserves its original dark preview palette.
- Added a light-mode headset material variant that raises only very dark mesh materials so frame, foam and lens details remain visible against the light background.
- Fixed light-theme optical-editor and network-settings surfaces; viewport cards and their static labels now use the active theme surface brush instead of hard-coded dark fills.
- Replaced the appearance dialog's text-labeled color controls with five color-only swatches. Left-click applies a swatch; right-click opens the Windows color picker and saves the customized swatch.
- Removed the HEX input field, custom-color apply button and redundant instructional/subtitle text from Windows dialogs and the menu.
- Added regression coverage for light-mode model-material contrast and responsive appearance-dialog swatch geometry.

## 0.3.13

- Replaced the non-responsive native appearance dropdowns with explicit dark/light mode buttons and five directly clickable accent-color swatches.
- Mode and preset-color button clicks immediately save settings and redraw the complete Windows UI; custom HEX remains supported.
- Added layout regression coverage for the new controls and synchronized platform version metadata.

## 0.3.12

- Fixed optical-preset dialog alignment by calculating card borders and all controls from one shared responsive layout; the two-button action rows now fill and align with the right-hand card.
- Separated optical-preset and appearance cards from their footers to eliminate overlapping surfaces and improve visual balance.
- Made light/dark mode and predefined accent selections apply and persist immediately; custom HEX colors remain editable and are applied with the Apply Theme button.
- Reworked theme refresh to update every native child control, trigger `WM_THEMECHANGED`, and force a full redraw using the newly-created brushes.
- Applied an explicit Explorer common-controls theme in light mode rather than relying on a blank theme name, preventing stale dark/system control rendering.
- Added platform-independent layout regression tests for preset and appearance dialogs.

## 0.3.11

- Fixed the Android Kotlin compile error caused by the missing `android.content.Intent` import.
- Added Android-local optical preset persistence, including migration of existing display calibration into a preset on upgrade.
- Made the Windows Host request the Android preset catalogue at session startup; the phone remains the source of truth and acknowledges edits synchronized from the Host.
- Replaced the Android manual display-calibration panel with an optical preset selector.
- Unified the Windows optical preset editor with phone display parameters; double-clicking a preset opens its editor, and save/apply operations synchronize through the control protocol.
- Added switchable light/dark themes and a custom `#RRGGBB` accent color, persisted in the user's application settings.
- Removed native caption close buttons from child dialogs in favor of the in-app Close action, and aligned their client areas with the selected theme.
- Expanded the SteamVR status pill for English labels and reserved a 16-pixel gap above the Yaw/Pitch/Roll cards.
- Added Android preset JSON tests, Go preset normalization tests and protocol documentation for Android-owned optical presets.

# Changelog

## 0.4.1 - Android codec probe compile fix

- Read `maxSupportedInstances` from `MediaCodecInfo.CodecCapabilities` (the API owner), not `MediaCodecInfo`.
- Convert `VideoCapabilities.supportedFrameRates.upper` from `Int` to `Double` for the normalized capability model.
- Keep the SteamVR driver install flow explicit: build the driver package, then register it with `vrpathreg.exe` using the provided PowerShell helper.

## 0.3.10
- Rebuilt the Windows Host main menu as an independent, non-activating owned popup. Opening it never resizes or shifts the OpenGL pose preview; clicking outside or switching applications dismisses it without leaving stale menu state.
- Added buffered painting for the dashboard, popup menu, scroll viewports and dialogs to reduce intermediate redraw flashing.
- Redesigned Phone Display, Optical Presets, Network Settings and Runtime Logs with larger card-based layouts, clearer grouping and resizable windows.
- Added responsive layouts, native vertical scroll bars and mouse-wheel scrolling for long settings pages; action buttons remain pinned in the footer.
- Increased application typography and control dimensions, including body text, labels, telemetry values, inputs and buttons.
- Preserved existing settings, preset management, network service and log actions.

## 0.3.9
- Reworked native Windows main UI to closely follow the original PhoneVR dark dashboard design: navy surfaces, compact status pills, rounded telemetry cards, centered SteamVR state and compact menu layout.
- Replaced flat GDI rounded shapes with GDI+ anti-aliased drawing and ClearType Segoe UI typography; enabled Per-Monitor DPI awareness where supported.
- Restored distinct Host/HMD/battery/SteamVR indicator colours with responsive, single-line Chinese/English labels and end ellipsis to prevent layout overlap.
- Added dark title-bar handling, high-DPI initialization, and improved English single-line layout behavior.
- Recessed the Daydream optical assemblies into the eye-side face cavity and reduced lens barrel depth.

## 0.3.8 - Native Windows desktop UI
- Replaced the Edge/Chrome app-mode dashboard and loopback HTTP UI server with a native Win32 desktop application.
- Added a native WGL/OpenGL HMD viewport that loads the embedded Daydream View 2017 OBJ/MTL mesh and renders a solid 3D model with depth testing and material shading.
- Kept HMD-local XYZ axes and gaze ray attached to the phone quaternion; the world floor grid remains fixed.
- Added native child windows for phone display settings, optical presets, network settings and logs.
- Kept remote phone display settings, center-view action, network start/stop/restart, battery status and applied-settings confirmation.
- Added persistent Chinese/English language selection, with Chinese as the default.
- Enforced a 1.2:1 native outer-window resize ratio and retained a 1080 x 900 default window size.
- Removed browser dashboard endpoints, Edge/Chrome launch helpers and browser-embedded HTML/JavaScript assets.
- Added mesh loading, Daydream preset and native preset persistence tests.
- Removed the old dashboard window resize test that failed when a test process had no real GUI window; native resize geometry now runs in the actual main-window `WM_SIZING` handler.
- Reworked the `WM_SIZING` RECT copy to use Win32 `RtlMoveMemory`, removing the `go vet` unsafe uintptr-to-pointer warning while keeping the native aspect-ratio resize logic.
- Consolidated Android and Windows checks/builds into one `PhoneVR CI` workflow with parallel platform jobs.
- Added a manually triggered Release workflow with patch/minor/major version bump, Android `versionCode` increment, synchronized client/Host version metadata, tagged builds, packaged Windows binaries, APK and GitHub Release publication.
- Network protocol remains v1. Android client source remains unchanged at version 0.3.6.


### Android CI follow-up
- Fixed JVM unit-test JSON runtime by adding `org.json:json` as a test-only dependency, preventing the mockable Android SDK `org.json` stubs from returning null in `OpticalPresetTest`.


## 0.4.2

- Added a SteamVR Driver Manager window to PhoneVR Host.
- Host can discover the latest PhoneVR Windows x64 driver ZIP from GitHub Releases, download it, validate the package, install it under the current user's LocalAppData directory, and register it with SteamVR through `vrpathreg.exe`.
- Added local driver ZIP selection, install/update, registered driver status inspection, and in-app driver uninstallation.
- Installation checks the `phonevr` manifest identity, avoids duplicate registration by replacing the existing PhoneVR entry, rolls back registration if an update fails, and only deletes files from the PhoneVR-managed driver directory.
- Release workflow now builds and publishes `PhoneVR-SteamVR-Driver-Windows-x64.zip` as a dedicated GitHub Release asset.
- Added ZIP traversal, package-integrity, and driver-manifest validation tests.

## 0.4.3

- Fixed the Windows local SteamVR driver ZIP picker crash. The `OPENFILENAMEW` ABI now includes `lpEditInfo` and `lpstrPrompt` before `pvReserved`, `dwReserved`, and `FlagsEx`.
- Added amd64/386 structure size and field-offset regression tests.
- Kept the selected-file buffer and UTF-16 filter/title buffers alive across the native dialog call.
- Added `CommDlgExtendedError` reporting so a real common-dialog failure is reported in the Host instead of being silently treated as cancellation.

## 0.4.19 diagnostics follow-up (2026-11-22)

- Start hardware H.264 encoder preflight before opening the Host control listener to remove the first-pairing race.
- Align Android's control handshake timeout with the Host's bounded encoder-preflight wait.
- Log Android-advertised H.264 decoder names and their exact resolution, frame-rate, hardware and target-profile capabilities when negotiation fails.
- Add Android Logcat diagnostics for the MediaCodec H.264 decoder inventory, useful for vendor codec stacks such as LineageOS on Redmi K40.


## 0.4.19 DisplayRedirect startup-order follow-up (2026-11-22)

- Pre-initialize the SteamVR DisplayRedirect D3D11 device before adding tracked devices.
- Register `PHONEVR-DISPLAY-001` before `PHONEVR-HMD-001` so the virtual-output device is available before the HMD display component is queried.
- Publish `Prop_HasVirtualDisplayComponent_Bool` and `Prop_GraphicsAdapterLuid_Uint64` into OpenVR's `k_ulDisplayRedirectContainer` override container and read the effective values back from the HMD during activation.
- Make Host hardware H.264 preflight synchronous before the TCP pairing listener starts; the previous goroutine-based probe could still race with the first Android handshake.
- Add regression checks for DisplayRedirect registration order, HMD property overrides, and synchronous encoder preflight.

## 0.4.19 - Silent Host child-process startup and Windows UDP test stabilization

- Centralized Windows child-process launch configuration in `internal/childprocess`. The Host now applies both `STARTF_USESHOWWINDOW`/`HideWindow` and `CREATE_NO_WINDOW` to every helper command it starts.
- Applied silent launch configuration to Steam registry discovery (`reg.exe`), SteamVR driver lookup/registration (`vrpathreg.exe`), FFmpeg encoder inventory, hardware preflight and the persistent H.264 encoder. Captured stdout/stderr and error reporting remain available to the Host log.
- Fixed the Windows pose-batch unit-test flake by allowing the UDP/IOCP receive path a one-millisecond drain interval. The production change remains below the 5.5ms period of a 180Hz sensor and avoids falsely treating a short burst as a single datagram.
- All Go tests, repeated pose-batch test runs, `go vet`, Windows/amd64 cross-compilation and Python release tests pass in the current environment. Native Windows build and runtime confirmation that no helper console windows flash remain to be confirmed on the target PC.

## 0.4.19 - Android stream resilience, pose scheduling and headset UI (2026-11-22)

- Fixed a MediaCodec input-buffer retry loop that could busy-spin at display thread priority for up to 200 ms. The decoder now runs at normal priority and backs off by 1 ms while preserving the pending access unit, preventing avoidable CPU starvation of the 180 Hz IMU, pose sender and GL renderer.
- Detect UDP stream resume after a one-second-or-longer gap. Clear stale partial assemblies and queued access units, flush MediaCodec on its owning decoder thread, and resume decoding at the next IDR.
- Expose fragment reassembly expirations, in-flight frames, decoder queue depth, input waits, decode recoveries and packet/frame/decode age. Add structured per-second Android video pipeline logs.
- After live video has been shown, stop presenting a frozen old frame when the stream is stale; render black until fresh decoded content resumes. Keep the initial diagnostic scene before the first decoded frame.
- Move PC connection and pose details into the Android menu. Add a compact top-edge stream-state/FPS indicator and detailed receive/decode/display counters in the menu.
- Add a persisted 180-degree reverse-landscape switch to change which side of the headset receives the phone's charging connector. Sensor coordinate mapping follows the active display rotation.
- Keep Host pose snapshots lock-free through atomic publication and rename the associated timing metric to `max pose publish duration`.
- Log complete Android device-action exceptions to Logcat; control acknowledgements remain serialized on the background control-writer executor.

## Android menu layout refinement

- Removed the explanatory caption beneath the 180-degree screen flip control.
- Repositioned the three-dot menu trigger to the horizontal center of the top edge.
- Kept the compact stream status indicator at the top-right and retained the menu panel's centered alignment.


## Windows Host theme defaults and switch order

- Changed the first-run/default Host appearance to Day mode (light).
- Kept an explicitly saved Night mode preference unchanged. Unknown or missing theme modes normalize to the Day mode default.
- Reordered the Appearance & Theme buttons so Day mode is on the left and Night mode is on the right, including keyboard focus order.

## 0.4.19 USB AOA Bulk transport (2026-11-22)

- Added a native Android Open Accessory (AOA) USB direct-connection mode. Windows initiates the AOA mode switch through the standard vendor requests and opens the Accessory interface's Bulk IN/OUT endpoints through WinUSB.
- Added a shared 16-byte `PVRU` framing header with separate control, pose and video channels. Control JSON keeps the existing message format; pose keeps the `PVSP` packet format; video keeps the `PVRV` fragment format.
- Integrated Android `UsbManager.openAccessory()` permission handling and USB accessory attach intents. The Android client forwards USB video fragments into the existing local video receiver to reuse reassembly and MediaCodec decoding.
- Added a Windows WinUSB AOA scanner and routed USB pose packets into the existing Host pose receiver. The ADB interface is not opened or used; USB tethering/RNDIS is not used.
- Documented the WinUSB driver binding prerequisite and physical-device validation limitations.

### USB AOA scanner diagnostics and WinUSB GUID compatibility

- Added support for both `DeviceInterfaceGUID` (REG_SZ) and `DeviceInterfaceGUIDs` (REG_MULTI_SZ) registry values when discovering WinUSB device interfaces.
- Added throttled Host runtime diagnostics for WinUSB GUID enumeration, device paths, descriptor reads, AOA `GET_PROTOCOL`, `SEND_STRING`, and `START_ACCESSORY` failures.
- Clarified that an Android interface labelled ADB may provide the WinUSB handle used solely for device-level AOA control requests; PhoneVR does not exchange ADB protocol data.

## 0.5.4 - WinUSB open-path diagnostics

- Replaced the Host's direct `CreateFileW` procedure call with Go's standard Windows `syscall.CreateFile` wrapper while preserving `GENERIC_READ | GENERIC_WRITE`, shared read/write access, `OPEN_EXISTING`, `FILE_ATTRIBUTE_NORMAL` and `FILE_FLAG_OVERLAPPED`.
- When the Host receives `ERROR_ACCESS_DENIED`, it now probes read-only and write-only opens from the same process, reports the actual Host PID and token elevation state, and prints the exact Win32 access/flag values. These probes only open and close handles; they do not issue USB transfers.
- The supplied Windows tests showed that the same device path opens successfully with read/write, read-only and write-only access from PowerShell, and the installed `oem145.inf` contains no explicit `Security` directive. This revision uses that evidence to distinguish a Host-process/API discrepancy from a device ACL problem.
- Synchronized application, Host, SteamVR driver and AOA accessory metadata to 0.5.4. Android `versionCode` is now 39.
- Go tests, race tests, vet, Windows amd64 Host cross-build and 49 Python regression tests pass. Device-side AOA handshake remains pending.

## 0.5.3 - WinUSB open diagnostics and overlapped-handle fix

- Create WinUSB device handles with `FILE_FLAG_OVERLAPPED`, as required by `WinUsb_Initialize`.
- Report `CreateFileW` and `WinUsb_Initialize` failures as separate stages with the Win32 error code.
- Add actionable diagnostics for `ERROR_ACCESS_DENIED` without silently changing driver permissions or requiring permanent elevation.
- Bumped synchronized application, Host, SteamVR driver and AOA accessory metadata to 0.5.3. Android `versionCode` is now 38.

## 0.5.2 - OEM-independent AOA discovery

- Made WinUSB interface discovery generic: the Host reads each installed USB function's `DeviceInterfaceGUID` / `DeviceInterfaceGUIDs` registration and only enumerates interfaces assigned to the WinUSB service. Interface GUIDs are not hard-coded to a particular PC or phone.
- Removed the Android OEM VID allow-list from AOA probing. The Host sends the standard device-level `GET_PROTOCOL` request to present WinUSB functions and proceeds only for AOA protocol v1/v2 responses. Failed probes are rate-limited to once every five seconds per device path.
- Added tests for AOA v1/v2 validation and WinUSB service filtering. Updated the Windows driver guide to clarify ADB-labelled control interfaces, dynamic GUID discovery and hardware-specific AOA support.
- Bumped repository, Android, Windows Host, SteamVR driver and accessory descriptors to 0.5.2. Android `versionCode` is now 37.

## 0.5.1 - Native USB AOA Bulk streaming and enumeration diagnostics

- Added Windows-initiated Android Open Accessory mode negotiation and a full-duplex WinUSB Bulk transport. The USB stream multiplexes control, HMD pose and H.264 video without USB network sharing, RNDIS or ADB data-channel use.
- Added Android accessory attach/permission/session handling and reused the existing Host control, pose, H.264 encoding and Android MediaCodec paths.
- Improved WinUSB device-interface discovery to read both `DeviceInterfaceGUID` and `DeviceInterfaceGUIDs`; added detailed device enumeration and AOA control-transfer diagnostics.
- Fixed a GUID-selection defect found during Redmi K40 testing: the scanner previously fell back to the generic `GUID_DEVINTERFACE_USB_DEVICE` (`A5DCBF10-...`) and attempted WinUSB operations on generic USB device paths. It now enumerates only GUIDs explicitly registered under each USB device instance's `Device Parameters` key, including the reported `F72FE0D4-CBCB-407D-8814-9ED673D0DD6B` interface.
- Synchronized repository, Android, Windows Host, SteamVR driver and AOA accessory descriptor versions to 0.5.1; Android `versionCode` is now 36.
- Go tests, race tests, vet and Windows amd64 Host cross-build pass. Physical AOA switching, sustained USB throughput, Android APK build and native SteamVR runtime validation remain pending on the target devices.

## 0.4.19 - Virtual VSync stability and low-latency follow-up

- Corrected virtual-vsync publication order: `GetTimeSinceLastVsync` sees a VSync only after `WaitForPresent` reaches it. The scheduler now maintains a bounded 72 Hz timeline, advances the virtual frame counter across missed intervals, and serializes wait callbacks.
- Added `early-wait` and `VSync_None`/`VSync_WaitRender`/`VSync_NoWaitRender` counters to the driver timing line so runtime callback pairing and pacing mode are observable.
- Replaced coarse `Sleep(1)` timing with a high-resolution Windows waitable timer when supported, followed by a short yield-spin.
- Reduced the Host's unmatched H.264 timestamp backlog from 12 to 4 frames (about 67 ms at 60 FPS). QSV async depth remains 2 to preserve the throughput measured on the target PC.
- Confirmed that the current HMD render target is already 1600x720 SBS, matching the encoded stream; this revision does not change eye resolution.
- Go tests, race tests, vet, Windows/amd64 Host cross-build and 44 Python release tests pass. Native MSVC driver compilation and target-PC/Android runtime latency validation remain pending.

## 0.4.19 - QSV output stall and nonblocking capture readback follow-up

- Explicitly request NV12 for hardware H.264 encoding, retain YUV420P for software encoders, and use QSV-native AUD insertion.
- Raise QSV asynchronous depth to 4 while retaining a bounded 24-frame timestamp window.
- Upgrade hardware preflight from a 160x90 two-frame initialization check to a full-resolution BGRA test at the configured size and frame rate; reject a full-profile test slower than three seconds.
- Treat a full timestamp queue as recoverable raw-frame backpressure. Add a two-second complete-access-unit stall watchdog, exponential retry backoff, process-exit detection and first-stdout/first-AU logs.
- Launch FFmpeg inventory, preflight and streaming processes with `CREATE_NO_WINDOW` to avoid visible child console windows on Windows.
- Expand Host video telemetry with raw RX FPS, submitted FPS, encoded AU count, FFmpeg stdout bytes and drop counters; sample the first three BGRA frames to distinguish an upstream black frame from an encoder failure.
- Change the background D3D11 staging readback to `D3D11_MAP_FLAG_DO_NOT_WAIT`, requeueing `DXGI_ERROR_WAS_STILL_DRAWING` instead of waiting inside the protected immediate context.
- Go unit tests, race tests, vet, Windows/amd64 Host cross-build and 42 Python release tests pass. Native Windows driver build, real QSV throughput and Android device validation remain pending.

## 0.4.19 - Persistent video encoder and capture backpressure fix

- Moved the FFmpeg H.264 session out of individual driver TCP connection handlers into one Host service-lifetime encoder worker. A transient TCP reconnect no longer closes and respawns FFmpeg.
- Reused the H.264 hardware encoder selected and validated during synchronous Host startup preflight. Streaming startup no longer repeats FFmpeg encoder discovery and hardware test subprocesses on each connection.
- Decoupled loopback TCP reception from FFmpeg stdin writes through a bounded two-frame latest-frame-wins queue with pooled BGRA buffers. When QSV falls behind, stale frames are dropped instead of blocking TCP receive and triggering a reconnect cycle.
- Serialized the driver capture bridge to one active connection, increased the Host TCP receive buffer and idle timeout, and increased the driver's raw-frame send timeout from 250 ms to 5 seconds.
- Added rate-limited WSA socket-error diagnostics and Host input-queue-drop/FPS telemetry. The persistent encoder retries failed sessions with a five-second backoff.
- Go tests, race tests, `go vet`, Windows/amd64 Host cross-build, and 41 Python release regression tests pass. Native MSVC SteamVR driver compilation and end-to-end Windows/Redmi K40 LineageOS validation remain pending.

## 0.4.19 - DisplayRedirect startup-order and HMD override follow-up

- Pre-initialize the DisplayRedirect D3D11 capture device before registering tracked devices, then register DisplayRedirect before the PhoneVR HMD.
- Publish the HMD-facing `Prop_HasVirtualDisplayComponent_Bool` and `Prop_GraphicsAdapterLuid_Uint64` override values during pre-initialization, immediately after DisplayRedirect registration and again during activation. Each attempt logs its phase and OpenVR property result codes.
- Read back the effective virtual-display flag and graphics-adapter LUID from the HMD property container during HMD activation, making the compositor routing prerequisites observable.
- Keep encoder preflight synchronous before opening Host pairing listeners and log that startup stage explicitly.
- Go tests, race tests, `go vet`, Windows/amd64 Host cross-build, and 41 Python regression tests pass. Native MSVC SteamVR driver build and target runtime verification remain pending.

## 0.4.19 - SteamVR DisplayRedirect diagnostic refresh

- Added one-time `GetComponent` request logging for the HMD and DisplayRedirect devices, including requested and expected OpenVR interface versions.
- Added DisplayRedirect activation logging with tracked-device object index, selected graphics-adapter LUID and every display-related property write result.
- Added cumulative DisplayRedirect `GetComponent` query/match counters and the active state to the five-second virtual-display telemetry.
- Made the DisplayRedirect active flag atomic and changed interface-name comparisons to case-insensitive matching.
- This is a diagnostic driver revision. Native MSVC compilation and SteamVR runtime validation are still required on the target Windows machine.

## 0.4.19 - USB tethered streaming and D3D11 build fix

- Fixed the MSVC C2065 build failure by including the Windows SDK `d3d11_4.h` header that declares `ID3D11Multithread`.
- Added Android Auto / Wi-Fi / USB-RNDIS discovery modes. Discovery enumerates active IPv4 interfaces, binds one socket per interface and sends directed broadcasts through each selected interface.
- Reused the existing TCP control, UDP pose and H.264 video transport over USB tethering; no ADB tunnel or USB-debugging authorization is required.
- Added Host network-interface labels and a LocalSubnet-scoped Windows Firewall helper for inbound TCP 48101 and UDP 48100/48102.
- Bumped Android versionCode to 35 and synchronized Android, Host and SteamVR driver metadata to 0.4.19.
- Windows MSVC native driver build and Android Gradle/device validation remain required on target devices.

## 0.4.18 - Integrated SBS H.264 streaming

- Removed the Host display-refresh-rate UI and persistence; virtual display timing is fixed at 72 Hz.
- Added D3D11 GPU scaling and bounded triple-staging capture from the final SteamVR SBS compositor backbuffer, with a loopback-only TCP bridge to Host.
- Added a hardware-only H.264 encoder preflight (NVENC/AMF/QSV); CPU software encoding is disabled for live VR to avoid increasing load on the PC.
- Added Host H.264 Annex-B access-unit packetization over UDP 48104 using PVRV v1; raw BGRA input buffer is reused to reduce allocation/GC load.
- Added Android hardware MediaCodec capability gating, UDP fragment reassembly, bounded decode queue, IDR loss recovery and decoded SBS presentation through the existing per-eye optical warp renderer.
- Host now selects and returns the exact Android hardware decoder name proven to support 1600×720@60; Android instantiates that exact codec instead of relying on the platform's default MIME-type choice. Hardware classification must be reliable, and a dropped H.264 input access unit triggers IDR resynchronization.
- Android versionCode is now 34. Windows MSVC driver build, Android Gradle build and target-device end-to-end video tests remain unverified in this environment.

## 0.4.17 - Pose-state isolation and selectable display refresh

- Moved the high-frequency pose snapshot to a dedicated lock, isolating pose writes from the Host device/status/log mutex.
- Added Host display refresh settings for 60 Hz, 90 Hz, 120 Hz and Custom (60–120 Hz, 0.1 Hz steps); the SteamVR driver reads the persisted rate on startup and applies it to HMD display frequency and virtual VSync timing.
- Default virtual refresh remains 72 Hz (Custom) for backward compatibility; SteamVR must be fully restarted after changing the setting.
- Android versionCode/versionName and Host/driver version metadata updated.

## 0.4.16 - Independent SteamVR pose publishing and timing diagnostics

- Moved HMD pose publication out of `IServerTrackedDeviceProvider::RunFrame()` into a dedicated above-normal-priority worker. It wakes immediately for every new UDP pose generation and republishes the latest pose on a 10 ms keepalive, separating tracking update cadence from `vrserver` main-loop callbacks.
- Added receiver generation signalling and counters for valid driver RX samples, UDP sequence gaps and queued datagrams drained. Added unique/repeated published-pose counters, maximum publish gap, maximum sample age and independent `RunFrame` cadence metrics.
- Added `IVRVirtualDisplay::Present` and `WaitForPresent` callback frequency / maximum-duration diagnostics to distinguish compositor/GPU callback stalls from pose publication stalls.
- Retained the existing 72 Hz virtual-display clock and explicitly report it alongside sensor and runtime update rates; no unverified 90/120 Hz change was made.
- The publisher sends stack-local `DriverPose_t` values and is stopped/joined before the HMD object ID is invalidated. Android sampling, Host packet protocol, Chaperone settings and D3D11 synchronization policy are otherwise unchanged.
- Added regression checks for independent pose publishing and separate compositor timing telemetry. Android versionCode is now 32.
- Native MSVC driver compilation and SteamVR runtime validation remain required on the Windows target; this environment cannot run that native toolchain.

## 0.4.15 - Host pose scheduling and log-view stability

- Cached local IPv4 adapter enumeration at Host service startup. `Snapshot()` no longer calls `net.Interfaces()` while holding the shared service read lock, preventing slow VPN/virtual-adapter queries from blocking the high-rate pose receiver's state update lock.
- Added a `max pose-state lock wait` telemetry field so future logs show whether Host pose publication is stalled waiting for service state synchronization.
- Increased the native Host pose-preview timer from 30 Hz to a 60 Hz target while keeping status metadata refresh throttled to 500 ms.
- Replaced the runtime log's classic EDIT control with Windows RichEdit 5.0 (`Msftedit.dll`) when available, retaining a standard EDIT fallback. Added a 1 MiB text limit, preserved selection and scroll position during refresh, and stopped forwarding a child scrollbar notification back into the log control.
- Added regression coverage for cached network addresses and the log/pose scheduling changes.
- Android versionCode increased to 31. Android Gradle packaging and native Windows MSVC/SteamVR runtime verification remain pending in environments with those toolchains.

## 0.4.14 - Pose burst forwarding and log repaint stability

- Windows Host now forwards every sample in a short UDP receive burst instead of dropping all but the newest sample. Only a burst larger than 32 queued poses is collapsed to its newest sample, bounding stale-backlog replay while preserving native sensor cadence during normal scheduling jitter.
- Host pose telemetry now distinguishes the maximum interval between Host read operations from the maximum Android sensor timestamp interval, and reports the largest receive-batch size. This separates phone sensor/send stalls from Windows receive-thread scheduling pauses; sequence gaps remain a separate packet-loss signal.
- SteamVR's pose receiver now computes angular velocity across every valid sample drained from its loopback queue before publishing the newest orientation, retaining high-rate motion information even when packets arrive in short bursts.
- Raised the Android IMU HandlerThread and dedicated pose-transmit thread to display scheduling priority, with a safe fallback to normal priority if the platform rejects the request.
- Runtime Logs now updates the native EDIT control with redraw suppression and a full invalidation/erase/repaint, preventing stale glyphs from appearing stacked after scrolling or refresh. Mouse-wheel and vertical-scroll messages are routed to the log control rather than the custom dialog scroll handler.
- Added regression coverage for preserving short pose bursts and collapsing only large stale backlogs.
- Android versionCode increased to 30. Android Gradle packaging and native Windows MSVC/SteamVR runtime verification remain pending in environments with those toolchains.

## 0.4.13 - Pose latency, continuity and direct-display diagnostics

- Android aligns every absolute sensor quaternion to the previous sample before calculating relative orientation, avoiding equivalent q/-q representation flips around large pitch rotations.
- Kept sensor collection on the dedicated IMU HandlerThread and increased the latest-sample UDP sender to up to 200 Hz, matching the requested 5 ms sensor period without queueing historical poses.
- Windows Host now drains already-queued UDP pose datagrams in a short bounded window and forwards only the newest valid pose. The pose socket buffer is bounded; telemetry reports RX/forwarded frequency, coalesced packets, sequence gaps, invalid packets, maximum arrival interval and loopback write errors.
- The SteamVR driver continues to drain its loopback socket to the newest pose and now derives angular velocity using differences between Android sensor monotonic timestamps rather than Windows packet-processing intervals. This prevents a queued burst from producing a false sample delta; driver-side drained packet counts are included in pose telemetry.
- Increased angular-velocity filter responsiveness while retaining a speed clamp; populate `vecAngularVelocity` and a bounded negative `poseTimeOffset` so SteamVR can predict toward display time.
- Added periodic SteamVR telemetry for sample age, angular speed, pitch and sequence to correlate rare gray flashes at high head angles with tracking freshness and motion.
- Documented the SteamVR **Settings → Developer → Enable Direct Mode** runtime switch. PhoneVR's HMD reports `IsDisplayOnDesktop=false`; `IVRVirtualDisplay` remains the wireless final-compositor-backbuffer handoff and is not replaced with a driver-owned compositor.
- Added regression tests for quaternion continuity, 200 Hz pose pacing, Host UDP batch coalescing, sensor-clock angular velocity, OpenVR prediction fields and Direct Mode configuration.
- Android versionCode increased to 29. Android Gradle packaging and native Windows MSVC/SteamVR runtime verification remain pending in environments with those toolchains.

## 0.4.12 - HMD connection stability and Android control sync

- Decoupled OpenVR device presence from IMU packet freshness. An activated HMD now remains `deviceIsConnected = true`; stale or missing samples only mark its pose untracked instead of making SteamVR remove and rediscover the device.
- Published an initial connected-but-untracked identity pose during HMD activation so SteamVR receives an explicit device pose before the first phone sensor packet arrives.
- Submit the latest pose on every SteamVR `RunFrame`, even when the UDP sequence has not changed. OpenVR invalidates poses if the driver stops submitting updates, so this prevents a quiet sensor/network interval from aging out an otherwise usable pose.
- Increased the pose freshness grace period from 250 ms to 1000 ms to absorb short Android scheduling and Wi-Fi jitter without producing unnecessary tracking-invalid transitions.
- Added `vrserver.txt` transition logs for IMU tracking becoming stale or resuming, explicitly noting that the HMD remains connected.
- Android now replies to the Host's initial optical-preset catalogue request directly on the TCP control thread rather than queueing a network response on the UI thread.
- Host retries an unanswered optical-preset catalogue request up to three times and logs a missing/empty response without tearing down the connected control or pose session.
- Manual LAN discovery now automatically fills and persists the selected Host IPv4 address and control port; tapping another result still changes the selected target.
- Added regression tests for the HMD connected/untracked lifecycle, Android preset response path, Host address auto-fill, and Host preset-request retry.
- Android versionCode increased to 28. Live SteamVR and Android-device runtime validation remains required.

## 0.4.11 - MSVC Chaperone JSON literal build fix

- Shortened the embedded Chaperone JSON C++ raw-string delimiter from 17 characters to the standard maximum of 16, fixing MSVC C3512.
- Updated the Chaperone regression test to parse the raw-string delimiter generically and explicitly enforce the C++ 16-character limit, preventing this failure from recurring.
- Bumped synchronized Android, Windows Host and SteamVR driver version metadata; Android versionCode is now 27.

## 0.4.10 - Driver-provided Chaperone and stable universe

- Added a valid OpenVR v5 `chaperone_info` JSON directly to the HMD properties, avoiding an external per-user file dependency.
- Added a stable PhoneVR universe ID and set `Prop_CurrentUniverseId_Uint64` to the same identifier used by the JSON `universeID`.
- Defined neutral seated and standing transforms and empty collision bounds, accurately reflecting the current 3DoF orientation-only tracking capability without inventing room-scale position or safety boundaries.
- Checked both OpenVR property-write results and added explicit success/failure diagnostics to `vrserver.txt`; Chaperone failure is non-fatal so HMD pose tracking can continue.
- Added automated JSON/schema, ID-consistency and 3DoF-boundary regression tests.
- Bumped synchronized Android, Windows Host and SteamVR driver version metadata.

## 0.4.9 - SteamVR frame-sync load reduction and diagnostics

- Removed the unused full-resolution D3D11 staging copy from `IVRVirtualDisplay::Present`; P0 has no encoder or frame consumer yet, so the extra 2400x1080 copy was wasted GPU bandwidth and memory traffic.
- Kept the small synchronization staging texture and GPU completion wait, matching the intended fence path used by Valve's virtual-display sample.
- Increased bounded keyed-mutex acquisition timeout from 2 ms to 10 ms to reduce compositor frame drops under cross-process scheduling contention.
- Added diagnostics for enumerated DXGI adapters, selected adapter LUID, shared-backbuffer dimensions/format/sample count, keyed-mutex timeouts and D3D11 Map/device-removal failures. Error logging is rate-limited.
- Retained the live Host heartbeat and completed-frame state reporting. No Android video encoder or transport was added in this version.
- Bumped synchronized Android, Windows Host and SteamVR driver version metadata.

## 0.4.8 - SteamVR driver MSVC compatibility fix

- Removed the invalid `override` specifier from `VirtualDisplayComponent`'s destructor. The pinned OpenVR 2.15.6 `IVRVirtualDisplay` interface has no virtual destructor; the concrete component still runs `Shutdown()` when its owning display device is destroyed.
- Guarded `WIN32_LEAN_AND_MEAN` and `NOMINMAX` definitions so MSVC does not emit C4005 redefinition warnings when CMake already supplies them.
- Bumped the synchronized application and driver version metadata.

## 0.4.7 - P0 SteamVR status and display-frame synchronization

- Replaced the hard-coded SteamVR state with a live loopback driver heartbeat and explicit HMD, tracking, display and completed-frame states.
- Added D3D11 shared-backbuffer opening, bounded keyed-mutex synchronization, full-frame staging copies, GPU-completion waiting and virtual-vsync frame timing.
- Kept the future video stream port 48104 reserved and moved the internal driver heartbeat to loopback UDP 48105.
- Added heartbeat validation, Host integration tests and stale-state expiry checks.
- Bumped Android version metadata and synchronized repository release version to 0.4.7.

## 0.4.6 - Driver manager status readability

- Replaced the SteamVR driver status label with a read-only multiline text control that automatically wraps long paths and supports text selection and copying.
- Increased the status area height and moved the driver action buttons lower to improve vertical spacing.
- Removed the decorative footer frame surrounding the Close button while keeping the button in its original bottom position.

## 0.4.5 - Automated release

- Android versionName and versionCode bumped by the manual release workflow.
- Windows Host version metadata updated from the repository VERSION file.

# Changelog

## 0.4.4 - Bundled SteamVR driver deployment

- Removed the unstable local ZIP picker and ZIP-based driver installation UI from the Windows Host.
- Embedded the compiled PhoneVR OpenVR driver payload into both Windows Host executables using Go `embed.FS`.
- Updated the standard Windows Host build to compile the C++ driver before compiling the Host and stage its package for embedding.
- Added automatic startup deployment when SteamVR is detected, with content-hash version directories, manifest/DLL validation and post-copy verification.
- Added Install / Repair Bundled Driver and Uninstall & Disable Auto Deploy actions, including a persistent user opt-out marker.
- Restricted automatic cleanup to PhoneVR-managed files; external driver files are unregistered but never deleted.
- Removed obsolete ZIP download/extraction code and changed Release packaging to distribute self-contained Host executables without a separate driver ZIP.
- Added cross-platform unit tests for driver package validation, path safety, digest stability and file-copy integrity.

## 0.4.1 - Android codec probe compile fix

- Read `maxSupportedInstances` from `MediaCodecInfo.CodecCapabilities` (the API owner), not `MediaCodecInfo`.
- Convert `VideoCapabilities.supportedFrameRates.upper` from `Int` to `Double` for the normalized capability model.
- Keep the SteamVR driver install flow explicit: build the driver package, then register it with `vrpathreg.exe` using the provided PowerShell helper.

## 0.4.0 foundation - 2026-11-21

- Added Windows x64 OpenVR SteamVR driver foundation: `PhoneVRHmd` (HMD) and `PhoneVRDisplayRedirect` (`IVRVirtualDisplay`).
- Added loopback-only pose bridge on UDP 48103; Go Host forwards validated existing `PVSP` packets without changing Android protocol v1.
- Added Android MediaCodec runtime capability discovery for H.264, HEVC and AV1.
- Added a bounded Host-startup FFmpeg encoder-candidate probe (`-ffmpeg` override supported) and backend selection for NVIDIA NVENC, AMD AMF, Intel QSV and software fallback; candidates are explicitly not treated as usable until adapter/session initialization succeeds.
- Added `PVRV` video datagram v1 framing, MTU-safe access-unit fragmentation, validation and boundary tests, with 1200-byte bounded UDP payloads.
- Added SteamVR driver CMake build, installer helper and Windows CI workflow.
- Video capture/encoding/transport/decoding and Daydream BLE controller pairing remain future implementation tasks.

- Added a dedicated light OpenGL pose-preview palette with a pale background, softer floor grid, and distinct high-contrast XYZ axes and gaze ray; dark mode preserves its original dark preview palette.
- Added a light-mode headset material variant that raises only very dark mesh materials so frame, foam and lens details remain visible against the light background.
- Fixed light-theme optical-editor and network-settings surfaces; viewport cards and their static labels now use the active theme surface brush instead of hard-coded dark fills.
- Replaced the appearance dialog's text-labeled color controls with five color-only swatches. Left-click applies a swatch; right-click opens the Windows color picker and saves the customized swatch.
- Removed the HEX input field, custom-color apply button and redundant instructional/subtitle text from Windows dialogs and the menu.
- Added regression coverage for light-mode model-material contrast and responsive appearance-dialog swatch geometry.

## 0.3.13

- Replaced the non-responsive native appearance dropdowns with explicit dark/light mode buttons and five directly clickable accent-color swatches.
- Mode and preset-color button clicks immediately save settings and redraw the complete Windows UI; custom HEX remains supported.
- Added layout regression coverage for the new controls and synchronized platform version metadata.

## 0.3.12

- Fixed optical-preset dialog alignment by calculating card borders and all controls from one shared responsive layout; the two-button action rows now fill and align with the right-hand card.
- Separated optical-preset and appearance cards from their footers to eliminate overlapping surfaces and improve visual balance.
- Made light/dark mode and predefined accent selections apply and persist immediately; custom HEX colors remain editable and are applied with the Apply Theme button.
- Reworked theme refresh to update every native child control, trigger `WM_THEMECHANGED`, and force a full redraw using the newly-created brushes.
- Applied an explicit Explorer common-controls theme in light mode rather than relying on a blank theme name, preventing stale dark/system control rendering.
- Added platform-independent layout regression tests for preset and appearance dialogs.

## 0.3.11

- Fixed the Android Kotlin compile error caused by the missing `android.content.Intent` import.
- Added Android-local optical preset persistence, including migration of existing display calibration into a preset on upgrade.
- Made the Windows Host request the Android preset catalogue at session startup; the phone remains the source of truth and acknowledges edits synchronized from the Host.
- Replaced the Android manual display-calibration panel with an optical preset selector.
- Unified the Windows optical preset editor with phone display parameters; double-clicking a preset opens its editor, and save/apply operations synchronize through the control protocol.
- Added switchable light/dark themes and a custom `#RRGGBB` accent color, persisted in the user's application settings.
- Removed native caption close buttons from child dialogs in favor of the in-app Close action, and aligned their client areas with the selected theme.
- Expanded the SteamVR status pill for English labels and reserved a 16-pixel gap above the Yaw/Pitch/Roll cards.
- Added Android preset JSON tests, Go preset normalization tests and protocol documentation for Android-owned optical presets.

# Changelog

## 0.4.1 - Android codec probe compile fix

- Read `maxSupportedInstances` from `MediaCodecInfo.CodecCapabilities` (the API owner), not `MediaCodecInfo`.
- Convert `VideoCapabilities.supportedFrameRates.upper` from `Int` to `Double` for the normalized capability model.
- Keep the SteamVR driver install flow explicit: build the driver package, then register it with `vrpathreg.exe` using the provided PowerShell helper.

## 0.3.10
- Rebuilt the Windows Host main menu as an independent, non-activating owned popup. Opening it never resizes or shifts the OpenGL pose preview; clicking outside or switching applications dismisses it without leaving stale menu state.
- Added buffered painting for the dashboard, popup menu, scroll viewports and dialogs to reduce intermediate redraw flashing.
- Redesigned Phone Display, Optical Presets, Network Settings and Runtime Logs with larger card-based layouts, clearer grouping and resizable windows.
- Added responsive layouts, native vertical scroll bars and mouse-wheel scrolling for long settings pages; action buttons remain pinned in the footer.
- Increased application typography and control dimensions, including body text, labels, telemetry values, inputs and buttons.
- Preserved existing settings, preset management, network service and log actions.

## 0.3.9
- Reworked native Windows main UI to closely follow the original PhoneVR dark dashboard design: navy surfaces, compact status pills, rounded telemetry cards, centered SteamVR state and compact menu layout.
- Replaced flat GDI rounded shapes with GDI+ anti-aliased drawing and ClearType Segoe UI typography; enabled Per-Monitor DPI awareness where supported.
- Restored distinct Host/HMD/battery/SteamVR indicator colours with responsive, single-line Chinese/English labels and end ellipsis to prevent layout overlap.
- Added dark title-bar handling, high-DPI initialization, and improved English single-line layout behavior.
- Recessed the Daydream optical assemblies into the eye-side face cavity and reduced lens barrel depth.

## 0.3.8 - Native Windows desktop UI
- Replaced the Edge/Chrome app-mode dashboard and loopback HTTP UI server with a native Win32 desktop application.
- Added a native WGL/OpenGL HMD viewport that loads the embedded Daydream View 2017 OBJ/MTL mesh and renders a solid 3D model with depth testing and material shading.
- Kept HMD-local XYZ axes and gaze ray attached to the phone quaternion; the world floor grid remains fixed.
- Added native child windows for phone display settings, optical presets, network settings and logs.
- Kept remote phone display settings, center-view action, network start/stop/restart, battery status and applied-settings confirmation.
- Added persistent Chinese/English language selection, with Chinese as the default.
- Enforced a 1.2:1 native outer-window resize ratio and retained a 1080 x 900 default window size.
- Removed browser dashboard endpoints, Edge/Chrome launch helpers and browser-embedded HTML/JavaScript assets.
- Added mesh loading, Daydream preset and native preset persistence tests.
- Removed the old dashboard window resize test that failed when a test process had no real GUI window; native resize geometry now runs in the actual main-window `WM_SIZING` handler.
- Reworked the `WM_SIZING` RECT copy to use Win32 `RtlMoveMemory`, removing the `go vet` unsafe uintptr-to-pointer warning while keeping the native aspect-ratio resize logic.
- Consolidated Android and Windows checks/builds into one `PhoneVR CI` workflow with parallel platform jobs.
- Added a manually triggered Release workflow with patch/minor/major version bump, Android `versionCode` increment, synchronized client/Host version metadata, tagged builds, packaged Windows binaries, APK and GitHub Release publication.
- Network protocol remains v1. Android client source remains unchanged at version 0.3.6.


### Android CI follow-up
- Fixed JVM unit-test JSON runtime by adding `org.json:json` as a test-only dependency, preventing the mockable Android SDK `org.json` stubs from returning null in `OpticalPresetTest`.


## 0.4.2

- Added a SteamVR Driver Manager window to PhoneVR Host.
- Host can discover the latest PhoneVR Windows x64 driver ZIP from GitHub Releases, download it, validate the package, install it under the current user's LocalAppData directory, and register it with SteamVR through `vrpathreg.exe`.
- Added local driver ZIP selection, install/update, registered driver status inspection, and in-app driver uninstallation.
- Installation checks the `phonevr` manifest identity, avoids duplicate registration by replacing the existing PhoneVR entry, rolls back registration if an update fails, and only deletes files from the PhoneVR-managed driver directory.
- Release workflow now builds and publishes `PhoneVR-SteamVR-Driver-Windows-x64.zip` as a dedicated GitHub Release asset.
- Added ZIP traversal, package-integrity, and driver-manifest validation tests.

## 0.4.3

- Fixed the Windows local SteamVR driver ZIP picker crash. The `OPENFILENAMEW` ABI now includes `lpEditInfo` and `lpstrPrompt` before `pvReserved`, `dwReserved`, and `FlagsEx`.
- Added amd64/386 structure size and field-offset regression tests.
- Kept the selected-file buffer and UTF-16 filter/title buffers alive across the native dialog call.
- Added `CommDlgExtendedError` reporting so a real common-dialog failure is reported in the Host instead of being silently treated as cancellation.

## 0.4.19 diagnostics follow-up (2026-11-22)

- Start hardware H.264 encoder preflight before opening the Host control listener to remove the first-pairing race.
- Align Android's control handshake timeout with the Host's bounded encoder-preflight wait.
- Log Android-advertised H.264 decoder names and their exact resolution, frame-rate, hardware and target-profile capabilities when negotiation fails.
- Add Android Logcat diagnostics for the MediaCodec H.264 decoder inventory, useful for vendor codec stacks such as LineageOS on Redmi K40.


## 0.4.19 DisplayRedirect startup-order follow-up (2026-11-22)

- Pre-initialize the SteamVR DisplayRedirect D3D11 device before adding tracked devices.
- Register `PHONEVR-DISPLAY-001` before `PHONEVR-HMD-001` so the virtual-output device is available before the HMD display component is queried.
- Publish `Prop_HasVirtualDisplayComponent_Bool` and `Prop_GraphicsAdapterLuid_Uint64` into OpenVR's `k_ulDisplayRedirectContainer` override container and read the effective values back from the HMD during activation.
- Make Host hardware H.264 preflight synchronous before the TCP pairing listener starts; the previous goroutine-based probe could still race with the first Android handshake.
- Add regression checks for DisplayRedirect registration order, HMD property overrides, and synchronous encoder preflight.

## 0.4.19 - Silent Host child-process startup and Windows UDP test stabilization

- Centralized Windows child-process launch configuration in `internal/childprocess`. The Host now applies both `STARTF_USESHOWWINDOW`/`HideWindow` and `CREATE_NO_WINDOW` to every helper command it starts.
- Applied silent launch configuration to Steam registry discovery (`reg.exe`), SteamVR driver lookup/registration (`vrpathreg.exe`), FFmpeg encoder inventory, hardware preflight and the persistent H.264 encoder. Captured stdout/stderr and error reporting remain available to the Host log.
- Fixed the Windows pose-batch unit-test flake by allowing the UDP/IOCP receive path a one-millisecond drain interval. The production change remains below the 5.5ms period of a 180Hz sensor and avoids falsely treating a short burst as a single datagram.
- All Go tests, repeated pose-batch test runs, `go vet`, Windows/amd64 cross-compilation and Python release tests pass in the current environment. Native Windows build and runtime confirmation that no helper console windows flash remain to be confirmed on the target PC.

## 0.4.19 - Android stream resilience, pose scheduling and headset UI (2026-11-22)

- Fixed a MediaCodec input-buffer retry loop that could busy-spin at display thread priority for up to 200 ms. The decoder now runs at normal priority and backs off by 1 ms while preserving the pending access unit, preventing avoidable CPU starvation of the 180 Hz IMU, pose sender and GL renderer.
- Detect UDP stream resume after a one-second-or-longer gap. Clear stale partial assemblies and queued access units, flush MediaCodec on its owning decoder thread, and resume decoding at the next IDR.
- Expose fragment reassembly expirations, in-flight frames, decoder queue depth, input waits, decode recoveries and packet/frame/decode age. Add structured per-second Android video pipeline logs.
- After live video has been shown, stop presenting a frozen old frame when the stream is stale; render black until fresh decoded content resumes. Keep the initial diagnostic scene before the first decoded frame.
- Move PC connection and pose details into the Android menu. Add a compact top-edge stream-state/FPS indicator and detailed receive/decode/display counters in the menu.
- Add a persisted 180-degree reverse-landscape switch to change which side of the headset receives the phone's charging connector. Sensor coordinate mapping follows the active display rotation.
- Keep Host pose snapshots lock-free through atomic publication and rename the associated timing metric to `max pose publish duration`.
- Log complete Android device-action exceptions to Logcat; control acknowledgements remain serialized on the background control-writer executor.

## Android menu layout refinement

- Removed the explanatory caption beneath the 180-degree screen flip control.
- Repositioned the three-dot menu trigger to the horizontal center of the top edge.
- Kept the compact stream status indicator at the top-right and retained the menu panel's centered alignment.


## Windows Host theme defaults and switch order

- Changed the first-run/default Host appearance to Day mode (light).
- Kept an explicitly saved Night mode preference unchanged. Unknown or missing theme modes normalize to the Day mode default.
- Reordered the Appearance & Theme buttons so Day mode is on the left and Night mode is on the right, including keyboard focus order.

## 0.4.19 USB AOA Bulk transport (2026-11-22)

- Added a native Android Open Accessory (AOA) USB direct-connection mode. Windows initiates the AOA mode switch through the standard vendor requests and opens the Accessory interface's Bulk IN/OUT endpoints through WinUSB.
- Added a shared 16-byte `PVRU` framing header with separate control, pose and video channels. Control JSON keeps the existing message format; pose keeps the `PVSP` packet format; video keeps the `PVRV` fragment format.
- Integrated Android `UsbManager.openAccessory()` permission handling and USB accessory attach intents. The Android client forwards USB video fragments into the existing local video receiver to reuse reassembly and MediaCodec decoding.
- Added a Windows WinUSB AOA scanner and routed USB pose packets into the existing Host pose receiver. The ADB interface is not opened or used; USB tethering/RNDIS is not used.
- Documented the WinUSB driver binding prerequisite and physical-device validation limitations.

### USB AOA scanner diagnostics and WinUSB GUID compatibility

- Added support for both `DeviceInterfaceGUID` (REG_SZ) and `DeviceInterfaceGUIDs` (REG_MULTI_SZ) registry values when discovering WinUSB device interfaces.
- Added throttled Host runtime diagnostics for WinUSB GUID enumeration, device paths, descriptor reads, AOA `GET_PROTOCOL`, `SEND_STRING`, and `START_ACCESSORY` failures.
- Clarified that an Android interface labelled ADB may provide the WinUSB handle used solely for device-level AOA control requests; PhoneVR does not exchange ADB protocol data.
