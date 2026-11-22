# PhoneVR 0.5.3 build status


## This revision

- Corrected the WinUSB `CreateFileW` flags by setting `FILE_FLAG_OVERLAPPED`, required by `WinUsb_Initialize`.
- Split device-open diagnostics into `CreateFileW` and `WinUsb_Initialize` stages and included Win32 error codes.
- `ERROR_ACCESS_DENIED` now points to process elevation and the USB device-interface ACL as diagnostic checks. This software change does not claim to modify Windows driver security settings.
- Android `versionName` is 0.5.3 and `versionCode` is 38.


Revision: WinUSB open diagnostics and overlapped-handle compatibility fix, 2026-11-22

## Changes in this revision

- Added native Android Open Accessory (AOA) mode switching from the Windows Host using `GET_PROTOCOL`, six `SEND_STRING` descriptors and `START_ACCESSORY`.
- Added Windows WinUSB Bulk IN/OUT transport and a shared `PVRU` framing layer for control, HMD pose and H.264 video channels.
- Added Android accessory attach handling, USB permission flow, `UsbManager.openAccessory()` stream management and USB AOA connection controls.
- Routed USB control, pose and video into the existing Host control handler, pose processing, H.264 encoder and Android MediaCodec decoder. Wi-Fi transport remains available and unchanged.
- Added WinUSB registry discovery for both `DeviceInterfaceGUID` and `DeviceInterfaceGUIDs`, with diagnostics for detected interface GUIDs, device paths, open failures and AOA control-transfer failures.
- Reworked WinUSB discovery to read `DeviceInterfaceGUID` / `DeviceInterfaceGUIDs` only from USB device instances whose Windows function service is `WinUSB`, then enumerate each currently present interface. No particular per-PC interface GUID is compiled into PhoneVR.
- Removed the Android OEM VID allow-list from the AOA handshake path. The scanner tries the standard device-level AOA `GET_PROTOCOL` request on present WinUSB functions and sends `SEND_STRING` / `START_ACCESSORY` only for recognized AOA protocol v1 or v2 responses. Failed candidates are rate-limited to one probe every five seconds.
- Clarified that some Windows installations expose the initial endpoint-zero AOA control handle as an `Android ADB Interface` node. PhoneVR does not send ADB protocol messages or use ADB bulk endpoints.
- Updated Android, Windows Host, SteamVR driver, AOA accessory descriptors and repository metadata to version 0.5.3. Android `versionCode` is now 38.
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

The Host now logs every registered WinUSB interface GUID, the corresponding interface paths and the exact AOA control-transfer error. For the Redmi K40 test, confirm that the path ends with `{F72FE0D4-CBCB-407D-8814-9ED673D0DD6B}` and that the scanner reports a successful WinUSB open before interpreting any AOA protocol result.
