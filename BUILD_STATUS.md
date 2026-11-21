# PhoneVR 0.4.15 Build Status

## 0.4.15 Host pose scheduling and log-view stability

- The 0.4.14 log showed Host RX near 177–180 Hz but frequent batches of 100–220 datagrams, `max Host read gap` around 0.5–1.2 seconds, and matching bursts of coalesced samples. Android sensor timestamp intervals generally remained near 9.8 ms, indicating the phone sensor was mostly sampling continuously while the Host receive/update path was intermittently delayed.
- The main UI polls `HostService.Snapshot()` every 500 ms. In 0.4.14, `Snapshot()` called `net.Interfaces()` and enumerated each adapter's addresses while holding `HostService.mu.RLock()`. The UDP pose receiver needs the same mutex's write lock to publish its latest state. On machines with multiple VPN/virtual adapters, that OS enumeration could hold the lock long enough to stall pose processing and create a large socket backlog.
- 0.4.15 now enumerates local IPv4 addresses once during Host startup and caches a detached copy. `Snapshot()` only copies cached strings under the lock; it no longer calls network-interface APIs in the high-frequency UI/status path. Host logs now include `max pose-state lock wait` to show whether the receive loop is blocked while publishing the latest state.
- Increased the native pose preview timer from 33 ms (~30 Hz) to 16 ms (~60 Hz). The status dashboard and metadata are still refreshed only every 500 ms.
- Runtime Logs now uses the Windows RichEdit 5.0 control from `Msftedit.dll` when available, with a standard EDIT fallback. The log view has a 1 MiB text limit, preserves the user's text selection and scroll location during refresh, and no longer re-sends a child scrollbar notification back into the child control. A startup log entry confirms whether RichEdit 5.0 was enabled.
- The pose telemetry keeps separate metrics for Host receive gaps, Android sensor sample intervals, receive-batch size, forwarding/coalescing and now state-lock wait. This avoids treating a large batch as a network packet-loss event when sequence gaps remain zero.

## Validation in this environment

- Go Host unit/integration tests — passed
- Go Race tests — passed
- Go Vet and Windows AMD64 Go Vet — passed
- Windows AMD64 test binary and GUI/console Host cross-builds — passed; these checks use the tracked embedded-driver placeholder, not a real native SteamVR DLL
- Python release/source-regression tests — 22 passed
- Android Kotlin sources unchanged in this revision; Android Gradle packaging not rerun
- Native MSVC SteamVR DLL compilation, RichEdit visual test and actual SteamVR runtime validation require the Windows target environment

## Runtime checks still required

- Compare `Host RX` and `forwarded`; they should be close during normal traffic and `coalesced` should remain near zero outside large scheduling stalls.
- Check `max pose-state lock wait`. It should remain close to zero after moving adapter enumeration out of `Snapshot()`.
- Compare `max Host read gap`, `max Android sensor interval` and `max batch`. A low Android interval with a high Host read gap now points to OS receive scheduling or another Host-side stall, not slow sensor sampling.
- Confirm the host's local pose preview feels smoother at the new 60 Hz target.
- Verify RichEdit log scrolling, page-up/page-down, refresh, selection and copy on the Windows target. The startup log should say `Runtime Logs: RichEdit 5.0 enabled`.
- If gray flashes remain while Host pose telemetry is fresh and state-lock wait is low, the next investigation should focus on the D3D11 virtual-display shared texture / SteamVR compositor path rather than the Host pose queue.
- Video encoding, network transport to Android and Android video display remain unfinished work.
