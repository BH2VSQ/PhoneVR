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
