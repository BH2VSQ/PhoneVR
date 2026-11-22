# PhoneVR 0.5.13 build status

## This revision

- Added Host transport modes: automatic failover, USB AOA only, and network only. Automatic mode has a selectable preferred path.
- The Android app now maintains independent LAN and AOA sessions under one persistent `deviceId`. The Host combines these paths into one Android device entry and excludes the auxiliary AOA video-only session from phone settings and optical preset management.
- Host video transmission uses one path at a time. UDP network packets and AOA Bulk video packets reach the same Android receiver and decoder. The receiver reports which source delivered the latest packets.
- Host health checks use per-path control heartbeats, Android media feedback and one-second `PVRP` probe acknowledgements over each standby video path. This verifies actual UDP or AOA Bulk delivery before selecting the alternate path. Explicit send errors trigger a temporary route penalty; lack of end-to-end media feedback on the selected path also marks it unhealthy.
- A route change purges queued delta frames and waits for an IDR before sending on the new path. Video capture and the H.264 encoder remain alive across the switch.
- Android's AOA auxiliary session carries head-pose samples only when the network TCP control session is unavailable. This avoids duplicate pose delivery when the LAN link is healthy.
- Android is `versionName 0.5.13`, `versionCode 48`. Root `VERSION`, Windows Host executable version, and AOA accessory descriptor metadata are updated to `0.5.13`. The embedded SteamVR driver payload itself is unchanged.

## Validation status

- `cd windows-host && go test ./...`: passed, including USB-preferred selection, USB-to-network failover, manual transport restrictions, silent active-path loss, and standby-probe gating.
- `cd windows-host && go test -race ./...`: passed.
- `cd windows-host && go vet ./...`: passed.
- `cd windows-host && GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go build ./...`: passed, including the Windows native UI and Host executable packages.
- `cd windows-host && GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go test -c ./cmd/phonevr-host`: passed. This compiles the Windows-only native UI and Host package for Windows amd64.
- Android Gradle tasks could not start: `android/gradle/wrapper/gradle-wrapper.jar` is absent from the supplied source bundle, no system Gradle executable is installed, and no Android SDK environment is configured. The Kotlin compiler parse check reported no syntax errors, but it cannot perform Android type checking without the Android SDK and project dependencies.
- APK packaging, Android unit tests, Windows runtime behavior and physical AOA/network switching have not been verified in this environment.

## Target-device validation

1. Install matching PhoneVR 0.5.13 Android and Windows Host builds. In Host **Video Transport**, select **Automatic failover** and keep both AOA and LAN connections established.
2. With USB preferred, confirm the Host reports `USB AOA` as active and `NET READY` as backup. Disconnect the USB cable or interrupt AOA Bulk, then confirm the Host changes to `NETWORK` after the transport failure is detected and the next IDR arrives.
3. Restore AOA, choose network preference, then interrupt LAN video delivery while keeping the AOA cable attached. Confirm the Host changes to `USB AOA` and the Android video receiver reports loopback source `127.0.0.1`.
4. Select **USB AOA only** and verify no network video fallback occurs. Select **Network only** and verify AOA is not used for video. The AOA auxiliary control path may remain connected in either mode.
5. In each switch, verify that H.264 capture and encoder counters continue increasing, Android resumes decoding after an IDR, and no duplicate video frames are produced by simultaneous USB and UDP transmission.
6. Disable LAN connectivity completely and verify that AOA video and pose continue. Restore LAN and confirm its control/pose path resumes without forcing a video route change while the current route remains healthy.

---

# PhoneVR 0.5.12 build status

## This revision

- The supplied 2026-11-22 Android Logcat shows the first Host Welcome control frame is decoded as a 521-byte payload, but its beginning is missing. Its remaining 25 bytes are the tail of the Welcome JSON; the following bytes begin with the next `PVRU` frame header. The next parser iteration then reports `Invalid USB multiplex frame signature`.
- The 25-byte tail is consistent with a 537-byte PVRU wire frame (16-byte header plus 521-byte payload) split at a 512-byte USB packet boundary. Android's previous `readLoop()` requested a 16-byte buffer for the PVRU header directly from the AOA `FileInputStream`, which is smaller than the USB packet. Android's AOA documentation explicitly says the read buffer must be large enough for a USB packet and describes packet buffers up to 16,384 bytes.
- Reworked Android AOA input to call `FileInputStream.read()` only with a 16 KiB buffer, then pass returned bytes to `UsbMuxStreamDecoder`. The decoder handles arbitrary read boundaries, coalesced frames, and frames spanning multiple bulk reads. Incomplete bytes remain buffered until the full frame arrives.
- Added a regression test reproducing the exact 512-byte split of a PVRU frame with a 521-byte payload. Added tests for coalesced frames, partial headers/payloads and invalid-header HEX diagnostics.
- Added Android raw Bulk IN read-size and prefix logs, PVRU channel/sequence/payload CRC32, and control-frame CRC32. Invalid headers include the raw 16-byte header HEX.
- Added Windows Host PVRU frame CRC32/length/sequence logging and WinUSB Bulk IN/OUT actual requested/transferred byte counts. Bulk OUT logs include endpoint, sample bytes, CRC32 and native error.
- Android TX sequence numbers are now assigned inside the write lock, ensuring sequence values match the serialized wire order.
- Android `versionName` is `0.5.12`; `versionCode` is `47`. Windows Host, SteamVR driver and AOA accessory descriptors are synchronized to `0.5.12`.

## Validation status

- Kotlin PVRU stream parser and protocol tests: 9 passed in a standalone Kotlin/JVM harness.
- `UsbAccessorySession.kt`, `UsbMuxProtocol.kt`, `UsbMuxStreamDecoder.kt` and `UsbVideoPacketDispatcher.kt` compiled with `kotlinc` against minimal Android API stubs.
- `cd windows-host && go test ./...`: passed.
- `cd windows-host && go test -race ./...`: passed.
- `cd windows-host && go vet ./...`: passed.
- `GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go build ./...`: passed.
- `bash ./scripts/build-android.sh both` could not start because this runtime has no configured `ANDROID_HOME` or `ANDROID_SDK_ROOT`. The Android Gradle unit tests and APK packaging therefore remain unverified here. Physical AOA validation is also pending.

## Target-device validation

Build matching PhoneVR 0.5.12 Host and Android APK. The Android Logcat should show `AOA raw Bulk IN read` entries with `requestBytes=16384`, followed by `AOA mux RX` entries. The first Host Welcome frame should be received with the full payload length and a CRC32 identical to the Windows Host `USB AOA mux TX frame` payload CRC32. Windows `Bulk OUT WritePipe` entries must show `transferred=requested` for each write. Any mismatch now has enough length, sequence, CRC and raw-byte context to identify the failing layer.

## Root-cause reference

Android USB accessory overview: https://developer.android.com/develop/connectivity/usb/accessory
Android accessory gadget `acc_read()` implementation: https://android.googlesource.com/kernel/common/+/android-trusty-3.18/drivers/usb/gadget/function/f_accessory.c

---

# PhoneVR 0.5.11 build status

## This revision

- The supplied CI run compiled both Debug and Release Kotlin sources, then reported one failing unit test: `UsbVideoPacketDispatcherTest.callbackFailureDoesNotStopFollowingPackets` at its `deliveredPackets` assertion.
- The test callback signals `secondDelivered` before the dispatcher returns from the callback and executes `delivered.incrementAndGet()`. The test thread could therefore wake and inspect the counter during that small scheduling window.
- Updated the test to wait up to one second for `deliveredPackets == 1` after the callback latch has fired. Production dispatcher behavior and delivery-counter semantics remain unchanged.
- Android `versionName` is `0.5.11`; `versionCode` is `46`. Repository, Windows Host, SteamVR driver and AOA accessory version metadata are synchronized to 0.5.11.

## Validation status

- Standalone Kotlin/JVM execution of the updated dispatcher tests passed 500 repetitions of `callbackFailureDoesNotStopFollowingPackets` and 20 repetitions of the bounded non-blocking queue test.
- Python release/integration regression suite: 50 tests passed.
- Pending a fresh GitHub Actions run of `:app:testDebugUnitTest`, `:app:assembleDebug` and `:app:assembleRelease`.
- The user-provided CI log confirms Debug and Release Kotlin compilation completed before the single unit-test assertion failed. It does not confirm APK packaging because the task graph stopped at `testDebugUnitTest`.

## Previous revision: 0.5.10

- The 0.5.9 source still allowed `ControlLineReader` to return an unterminated partial line when its source reached EOF. If the Android AOA Bulk receive thread stopped after only part of the Host Welcome had arrived, the partial bytes could still be passed to `JSONObject`, producing the reported malformed Welcome length.
- The 0.5.10 reader consumes bytes directly from `InputStream`, retains them across `SocketTimeoutException`, and decodes UTF-8 only after LF/CR line completion. EOF with pending bytes now raises `EOFException`; it cannot be mistaken for a complete JSON line.
- The Android AOA control input now preserves the underlying Bulk IN/framing failure and reports it after queued control frames are consumed. This makes USB transport failure visible separately from JSON parsing failure.
- Added bounded Android logs for the first six Control TX/RX frames, including PVRU sequence, byte count, escaped preview and hex. Added Windows Host logs for the Welcome JSON/wire length, initial control chunks, Control bridge failures and the root Bulk IN read error.
- Android `versionName` is `0.5.10`; `versionCode` is `45`.

## Validation completed

- `cd windows-host && go test ./...` passed.
- `cd windows-host && go test -race ./...` passed.
- `cd windows-host && go vet ./...` passed.
- `GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go build ./...` passed.
- All five `ControlLineReaderTest` cases compiled and passed in a standalone Kotlin/JVM harness: timeout-fragment retention, UTF-8 code point split across reads, CRLF/clean EOF, truncated EOF rejection and maximum line length.
- `UsbAccessorySession.kt`, `UsbMuxProtocol.kt` and `UsbVideoPacketDispatcher.kt` compiled with `kotlinc` against minimal Android API stubs, including the new receive-failure propagation and frame diagnostics.
- Python release/integration suite: 50 tests passed. Version-bump regression suite: 5 tests passed.

## Not verified in this environment

- Android Gradle unit tests and APK packaging were not completed because the Gradle 9.6.1 distribution is not cached in this runtime and network access to the Gradle distribution is unavailable.
- Full Android SDK type-check and physical AOA validation remain outstanding.

## Target-device validation

Build and install matching PhoneVR 0.5.10 Windows Host and Android APK. On Android, capture `PhoneVRAOA` and `NetworkClient` logs. Compare these with Host entries beginning `USB AOA Welcome prepared`, `USB AOA control TX chunk`, `USB AOA control RX frame`, `USB AOA Bulk IN stopped` and `USB AOA control Bulk OUT failed`. The expected Host Welcome `wireBytes` should equal the sum of the corresponding Android AOA Control RX frame payload bytes for the Welcome line, including its newline.

# PhoneVR 0.5.9 build status

## This revision

- The 0.5.8 target-device log showed the AOA session pairing, followed by a phone-side `JSONObject` type error and USB session teardown. The Host then reported a Bulk OUT semaphore timeout because Android had stopped draining the accessory stream.
- The Android USB control `InputStream` intentionally polls with a 500 ms timeout. `BufferedReader.readLine()` can lose the partial line it has accumulated when the underlying reader throws during an incomplete line. The caller previously swallowed that timeout and resumed with the remaining bytes, which could produce a truncated or misaligned JSON value.
- Added `ControlLineReader`, which owns the partial-line buffer across read timeouts for both USB and LAN control channels. It retains partial JSON until newline or EOF and enforces a 64 KiB line limit.
- Moved heartbeat servicing into the line reader's timeout callback so USB/LAN heartbeats and timeout detection continue while a fragmented control message is being received.
- Added diagnostic logging for malformed welcome/control JSON, including source, length and an escaped 120-character preview, so any remaining protocol corruption can be identified without logging an entire settings payload.
- Added JVM regression tests for a JSON control line split across multiple chunks with injected read timeouts, CRLF framing, EOF handling and maximum line length.
- Updated version metadata to 0.5.9 and Android `versionCode=44`.

## Validation status

- Standalone Kotlin compilation and execution of the new ControlLineReader regression cases: 3 tests passed (timeout-fragmented JSON, CRLF/EOF and maximum line length).
- Python release/integration regression suite: 50 tests passed.
- Windows Host `go test ./...`, `go test -race ./...`, `go vet ./...` and Windows amd64 cross-build passed.
- Version bump regression tests: 5 passed; synchronized metadata check confirms `VERSION=0.5.9` and Android `versionCode=44`.
- Android Gradle unit tests and APK packaging were attempted, but the Gradle 9.6.1 distribution could not be downloaded because DNS resolution for `services.gradle.org` failed. Full Android SDK type-check and APK build remain unverified.
- Physical AOA sustained transfer and reconnect behavior remain to be validated on the Redmi K40 and target Windows PC.

## Target-device validation

- Install matching Android and Windows Host 0.5.9 builds.
- Confirm `PhoneVR-AOA-RX` remains active and Android no longer reports the `Value ... cannot be converted to JSONObject` exception.
- If malformed JSON remains, inspect the new `Malformed control JSON` log entry and its escaped preview.
- Confirm Host `transport fragments` continues increasing without repeated `USB AOA Bulk OUT` semaphore timeouts or accessory disconnects.

# PhoneVR 0.5.8 build status

## This revision

- The 0.5.7 target-PC log confirms AOA pairing succeeds and WinUSB applies the configured 1500 ms Bulk OUT transfer timeout, but video writes still time out repeatedly (approximately 1500 ms after the first 2523.6 ms write).
- The log shows only two successful video transport fragments before repeated Bulk OUT timeouts, followed by the AOA control pipe closing and the device reconnecting.
- In 0.5.7, Android's `PhoneVR-AOA-RX` thread invoked the USB video packet listener directly. That listener synchronously sent every received PVRV fragment through a local UDP socket to the decoder receiver. A stalled local forwarding call could therefore stop the AOA Bulk IN reader from draining the device stream.
- 0.5.8 moves those callbacks to a dedicated worker using a bounded 256-packet queue. The USB input thread only parses the PVRU frame and performs a non-blocking queue offer.
- Android now periodically logs accepted, forwarded, dropped and queued video packets, callback failures and maximum callback duration. This will distinguish USB receive starvation from a slow loopback forwarding/decoder path on the next target-device run.
- Added JVM tests for non-blocking handoff, bounded queue overflow and callback failure isolation.
- Android `versionName` is 0.5.8 and `versionCode` is 43.

## Passed in this environment

- `cd windows-host && go test ./...`
- Standalone Kotlin compilation of `UsbVideoPacketDispatcher.kt` with `kotlinc`.
- Kotlin compile smoke check of `UsbAccessorySession.kt`, `UsbMuxProtocol.kt` and the dispatcher against minimal Android API stubs.
- Dispatcher stress smoke check: packet offer remained non-blocking while the consumer was deliberately held; bounded overflow was counted and queued delivery resumed after release.
- Windows amd64 cross-build: `GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go build ./...`.
- Python release/integration regression suite: 49 tests passed.
- Version metadata consistency check (`VERSION=0.5.8`, Android `versionCode=43`).

## Not verified in this environment

- Android Gradle unit tests and APK packaging. The wrapper attempted to download Gradle 9.6.1 but DNS resolution for `services.gradle.org` failed.
- Full Kotlin type-check against Android SDK APIs.
- Sustained AOA throughput and reconnect behavior on the Redmi K40 and target Windows PC.

## Next target-device validation

- Install Android 0.5.8 and Windows Host 0.5.8 together.
- Confirm Android logcat reports a rising `AOA video RX dispatcher: accepted=... forwarded=...` count while the Host `transport fragments` counter also rises.
- If `accepted` does not rise, inspect the WinUSB/AOA Bulk IN delivery path.
- If `accepted` rises but `forwarded` stalls and `queued` grows, inspect Android UDP loopback forwarding.
- If both counters rise but the decoder reports no complete frames, inspect PVRV reassembly and H.264 decoder input.
- Confirm the USB control/pose session remains connected while video is active.


# PhoneVR 0.5.5 build status

## This revision

- The 0.5.4 target-PC log proves AOA v2 negotiation and mode switching succeed: `GET_PROTOCOL` returned v2, `START_ACCESSORY` was accepted, and the phone re-enumerated as `18D1:2D01`.
- Added explicit diagnostics for the post-switch AOA interface descriptor and each endpoint discovered through WinUSB. The prior scanner silently discarded `findBulkEndpoints` errors, hiding the reason it never promoted the accessory into a Bulk session.
- Detects the known `18D1:2D01` ADB-interface signature (`interface 1`, class `FF`, subclass `42`, protocol `01`) and tells the user to bind WinUSB to the AOA accessory interface `MI_00`, leaving ADB interface `MI_01` untouched.
- Logs successful AOA Bulk IN/OUT endpoint selection, endpoint addresses, packet sizes and interface metadata.
- Updated the Windows driver guide with the 2D01 composite-interface mapping and the exact driver-binding distinction.
- Synchronized application, Host, SteamVR driver and AOA accessory metadata to 0.5.5. Android `versionCode` is now 40.

## Validation

- Go unit tests, race tests and vet.
- Windows amd64 Host cross-build and AOA Windows test cross-compilation.
- Python release/integration regression tests.
- Patch application and source ZIP integrity checks.

## Still requires target-device validation

- The log proves AOA mode switching only. It does not yet prove that the Host opened the accessory interface or discovered both Bulk endpoints.
- Android accessory permission prompt, `UsbManager.openAccessory`, bidirectional PVRU frames, sustained video throughput and pose latency remain to be confirmed on the Redmi K40.
- Android APK build remains unavailable in this environment because Android SDK/Gradle distribution access is unavailable.

# PhoneVR 0.5.4 build status

## This revision

- Replaced the direct `CreateFileW` `LazyProc.Call` path with Go's standard Windows `syscall.CreateFile` wrapper. The requested access, share flags, creation disposition and overlapped flags remain identical to the independent PowerShell test.
- If the combined read/write open fails, the Host now performs read-only and write-only open/close probes from the same process and records both results.
- Added Host PID and actual process token elevation diagnostics to distinguish the Host security context from the PowerShell session.
- No device ACL, registry permission or OEM driver INF is modified by PhoneVR.
- Android `versionName` is 0.5.4 and `versionCode` is 39.


Revision: WinUSB CreateFile wrapper and in-process access diagnostics, 2026-11-22

## Changes in this revision

- Added native Android Open Accessory (AOA) mode switching from the Windows Host using `GET_PROTOCOL`, six `SEND_STRING` descriptors and `START_ACCESSORY`.
- Added Windows WinUSB Bulk IN/OUT transport and a shared `PVRU` framing layer for control, HMD pose and H.264 video channels.
- Added Android accessory attach handling, USB permission flow, `UsbManager.openAccessory()` stream management and USB AOA connection controls.
- Routed USB control, pose and video into the existing Host control handler, pose processing, H.264 encoder and Android MediaCodec decoder. Wi-Fi transport remains available and unchanged.
- Added WinUSB registry discovery for both `DeviceInterfaceGUID` and `DeviceInterfaceGUIDs`, with diagnostics for detected interface GUIDs, device paths, open failures and AOA control-transfer failures.
- Reworked WinUSB discovery to read `DeviceInterfaceGUID` / `DeviceInterfaceGUIDs` only from USB device instances whose Windows function service is `WinUSB`, then enumerate each currently present interface. No particular per-PC interface GUID is compiled into PhoneVR.
- Removed the Android OEM VID allow-list from the AOA handshake path. The scanner tries the standard device-level AOA `GET_PROTOCOL` request on present WinUSB functions and sends `SEND_STRING` / `START_ACCESSORY` only for recognized AOA protocol v1 or v2 responses. Failed candidates are rate-limited to one probe every five seconds.
- Clarified that some Windows installations expose the initial endpoint-zero AOA control handle as an `Android ADB Interface` node. PhoneVR does not send ADB protocol messages or use ADB bulk endpoints.
- Updated Android, Windows Host, SteamVR driver, AOA accessory descriptors and repository metadata to version 0.5.4. Android `versionCode` is now 39.
- Retained Android stale-video recovery, lock-free Host pose snapshots, reverse-landscape support and the Host Day theme default from the preceding revisions.

## Passed in this environment

- `go test ./...`
- `go test -race ./...`
- `go vet ./...`
- AOA registry service filtering and AOA protocol-version unit tests
- `GOOS=windows GOARCH=amd64 go build ./cmd/phonevr-host`
- Windows amd64 cross-compilation of Host and AOA packages
- Python release and integration regression tests
- Kotlin AOA/PVRU framing and USB session compile smoke checks against Android API stubs
- Source archive integrity and patch application checks

## Not verified in this environment

- Android APK build and full Kotlin type-check against the Android SDK. The Android SDK is unavailable and Gradle distribution download is blocked by DNS/network access.
- Physical AOA mode switching and re-enumeration on the Redmi K40 / LineageOS 24.
- WinUSB binding behavior on the target PC, sustained Bulk throughput, USB disconnect/reconnect recovery and end-to-end pose/video latency.
- Native MSVC SteamVR driver build and runtime validation on the target Windows machine.

The Host now logs every registered WinUSB interface GUID, device path, in-process read/write access probes and token elevation state when opening fails. For the Redmi K40 test, the path should still end with `{F72FE0D4-CBCB-407D-8814-9ED673D0DD6B}`; compare the Host-side read-only/write-only probe results with the PowerShell results before changing any driver permissions.
