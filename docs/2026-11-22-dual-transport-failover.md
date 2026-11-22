# PhoneVR dual-transport video failover (0.5.13)

## Transport model

The Android client uses a persistent `deviceId` stored in app preferences. The Host pairs the LAN control connection and the AOA auxiliary control connection by this ID, so the two connections appear as one Android device in the Host UI. Older clients without an ID remain isolated as separate legacy sessions.

The normal Android session remains responsible for LAN discovery, TCP control and UDP pose. A second AOA session is opened independently with `videoOnly=true`. It has its own heartbeat and video feedback channel. The Host ignores device-settings and optical-preset operations on this auxiliary session. When the LAN TCP control connection is unavailable, the AOA session also forwards head pose through the USB mux Pose channel.

Both video paths are kept connected when available, but the Host transmits each H.264 access unit through only one path at a time. Network video is sent to the Android UDP port. AOA video uses the existing multiplex Video channel and is forwarded by Android to loopback UDP. The Android receiver binds one socket and accepts packets from the negotiated Host address and `127.0.0.1`, feeding one bounded reassembly queue and one MediaCodec decoder.

## Host choices

- **Automatic failover**: use the preferred route when choosing a new path, keep the current route while healthy, and switch to the remaining healthy path when required. USB AOA is the default preference.
- **USB AOA only**: video is sent only through AOA. If AOA is unavailable, video waits rather than falling back to network.
- **Network only**: video is sent only through LAN/Wi-Fi UDP. AOA may remain connected as a standby control session, but is not used for video.

The dashboard shows the selected mode, active route and whether the alternate route is available. The Video Transport dialog displays both path states and the most recent switch reason and time.

## Health and switching

Both control sessions send heartbeats. The Host also sends a small `PVRP` probe over each connected standby video path once per second. Android acknowledges a probe only after it reaches the shared video receiver, and reports the probed source path over both control connections. This tests the actual UDP or AOA Bulk data path, not only its control connection. Probe packets are filtered before H.264 reassembly and do not enter the decoder or change media packet-age statistics.

For the active path, Android receiver statistics identify the source address of the latest media datagram and report packet and decoded-frame age over the control connections. A failed AOA Bulk write or UDP socket write temporarily penalizes that route. If the active path has no recent source-matched media feedback after its startup grace period, the Host marks it unhealthy for a cooldown and chooses an alternate path only when that path has a recent probe acknowledgement. When the selected path changes, the Host drops queued delta access units and waits for the next IDR. The SteamVR capture bridge and persistent FFmpeg H.264 process are not restarted.

The client does not decode two parallel copies of the same video stream. Only the selected Host route transmits video, while the second physical/control path remains ready. This avoids duplicate frame IDs, duplicate MediaCodec input and unnecessary USB/LAN bandwidth.

## Key source files

- `windows-host/cmd/phonevr-host/video_transport.go`: route selection, pairing, path probes, health feedback and switch cooldowns.
- `windows-host/cmd/phonevr-host/service.go`: control session identity, per-route send failure handling, route-change IDR gating and Host snapshot.
- `windows-host/cmd/phonevr-host/native_ui_windows.go`: Video Transport settings dialog.
- `windows-host/cmd/phonevr-host/native_theme_windows.go`: active route and backup availability on the Host dashboard.
- `android/app/src/main/java/com/vsq/phonvrstream/network/NetworkClient.kt`: persistent identity, independent AOA session, shared video receiver, receiver feedback and USB pose fallback.
- `android/app/src/main/java/com/vsq/phonvrstream/streaming/VideoStreamReceiver.kt`: source-address filtering, standby path-probe acknowledgements and last-media-source reporting.

## Validation status

Go tests, race tests and vet pass, including explicit USB/network selection, failover, silent active-path loss and rejection of unverified standby paths. The Windows amd64 Host package cross-compiles, including the native Win32 UI. Android Gradle compilation and APK creation remain pending because `android/gradle/wrapper/gradle-wrapper.jar` is absent from the supplied source archive and the environment has no installed Gradle distribution or Android SDK. Physical USB AOA, Wi-Fi loss, and cross-path recovery still require target-device validation.
