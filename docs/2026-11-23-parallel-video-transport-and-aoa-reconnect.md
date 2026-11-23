# PhoneVR 0.5.15: parallel video transport and AOA reconnect stability

## Android AOA reconnect lifecycle

PhoneVR starts an AOA video-only backup session while the LAN control/pose session remains active. The application may request this session both during startup and after receiving an Android USB accessory attach event. The session start method is now idempotent: while the single auxiliary worker is running, duplicate start requests are ignored and do not increment its generation or clear the current session. The worker Future is retained and cancelled on disconnect, interrupting permission-dialog waits or accessory-enumeration sleeps so a stale attempt cannot hold the single-threaded executor ahead of a new attach.

The worker retains the existing bounded retry/backoff behavior. On each attempt it waits for the current accessory enumeration, requests permission when required, then resolves the accessory again after the permission result. It confirms permission on the currently enumerated instance and retries `UsbManager.openAccessory()` across short enumeration/descriptor-open windows. The logs include permission result, accessory identity, attempt count and final error.

## Parallel video delivery

The Host exposes four transport modes:

| Mode | Behavior |
|---|---|
| Automatic | Select the preferred verified route and fail over to the other route when needed |
| USB AOA only | Send video over AOA Bulk |
| Network only | Send video over LAN UDP |
| Network + USB AOA parallel | Mirror every PVRV fragment to each currently verified path |

The Host continues to group LAN and AOA sessions using the Android persistent device ID. It does not create two independent decoder sessions. Android's `VideoStreamReceiver` accepts LAN UDP packets from the Host address and AOA packets forwarded over localhost UDP. Both copies enter the same `VideoFragmentReassembler`, which identifies a fragment by frame ID and fragment index and ignores duplicates.

Each route has an independent PVRP readiness probe. In parallel mode, a route is added only after that route's own probe acknowledgement has been received within the health window. The Host sends probes continuously to both connected routes while parallel mode is selected. A write failure or expired probe health temporarily excludes the affected route, while the other route continues. A recovered route rejoins after another acknowledgement. Neither route can be selected simply because the other route is active, preventing the Android USB loopback bind race from being bypassed.

The Host treats a frame as delivered when every fragment was sent successfully over at least one route. A partial write failure on one path marks only that path unhealthy. The complete copy over the surviving path can still finish the frame. If both paths fail, the send loop requests a fresh IDR as in the existing failover behavior.

## Important bandwidth behavior

Parallel mode is redundant mirroring. It does not add the throughput of Wi-Fi and USB together, because the same encoded fragments are transmitted on both links. It is intended to improve continuity when one path drops or delays packets. Sending alternate fragments over different paths would require path-aware scheduling, frame assembly deadlines and a repair mechanism for missing fragments; that is outside this revision.

USB mirror writes are handled by a dedicated worker with a bounded four-access-unit queue. The UDP sender does not wait for WinUSB Bulk OUT to complete. If the AOA queue fills, the Host marks that path unhealthy and skips the redundant USB copy while preserving a successfully sent network copy. All USB PVRP health probes, including standby checks in automatic and network-only modes, use the same worker, so they cannot block the network video sender either.

## Host setting persistence

The selected transport mode and automatic-mode preference are stored in the user's PhoneVRStream configuration directory as `video-transport.json`. On Windows the base directory is returned by `os.UserConfigDir()` (normally `%APPDATA%`), followed by `PhoneVRStream`. The first-run fallback remains Network only. Saving is performed before applying a changed selection so the UI does not report a successful persistent setting when the file cannot be written.

## Validation

Host tests cover configuration save/reload, parallel route activation only after independent probe acknowledgement, single-route operation while the second path is still waiting, and byte-for-byte identity of UDP PVRV datagrams and PVRU channel-3 payloads. The standard Go test suite, race detector, vet and Windows amd64 cross-build pass for this revision.

Android Gradle compilation and APK packaging require Gradle 9.6.1 and Android SDK Platform 36. The current build container could generate the repository's bootstrap JAR, but could not resolve `services.gradle.org` to download Gradle. Physical USB attach/permission/reconnect tests and the complete parallel-streaming hardware test therefore remain required on a network-enabled Windows/Android development machine.
