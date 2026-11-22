# Silent Host child processes and Windows UDP pose batching

## Silent child-process policy

The normal `PhoneVRHost.exe` is linked with the Windows GUI subsystem. It must not flash a console window while checking SteamVR or preparing the video encoder.

All Windows child processes launched by the Host use `internal/childprocess.HideConsoleWindow`, which sets both `SysProcAttr.HideWindow` and the `CREATE_NO_WINDOW` creation flag while preserving any existing process creation flags.

The policy currently covers every Host subprocess call site:

- `reg.exe query` used to discover the SteamVR installation directory.
- `vrpathreg.exe finddriver` used to query PhoneVR driver registration.
- `vrpathreg.exe` add/remove operations used by install, repair and uninstall.
- FFmpeg encoder inventory (`-encoders`).
- FFmpeg hardware H.264 full-profile preflight.
- The persistent FFmpeg H.264 streaming encoder.

The Host still captures stdout and stderr through `CombinedOutput`, `StdoutPipe` and `StderrPipe`. Output and errors therefore remain available in the application log. The policy suppresses only child console windows; it does not suppress diagnostic text.

The separate `PhoneVRHost-console.exe` is intentionally a console-subsystem diagnostic build. Users who launch it directly will still see its own console window.

## Windows UDP pose batch test

The Windows UDP receive path uses IOCP. A burst of loopback datagrams written synchronously by a test may not all have been surfaced into the socket receive queue at the exact instant the test begins reading. A 250-microsecond drain deadline proved too short on the user's Windows build runner and caused `TestReadLatestPoseBatchCoalescesQueuedDatagrams` to intermittently observe only the first packet.

The drain interval is now one millisecond, and the unit test allows the sent datagrams to reach the local receive queue before it asserts burst coalescing. One millisecond is below the approximately 5.5-millisecond sample interval of the 180Hz phone sensor. The deadline is only a maximum drain wait; packets already queued are read immediately.
