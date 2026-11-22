# Virtual VSync stability and end-to-end latency

## Evidence from the 03:43 SteamVR/Host run

- The HMD refresh property is 72 Hz, but the driver reported `Present`/`WaitForPresent` rates ranging from about 9.8 Hz to about 199.7 Hz.
- `WaitForPresent` maximum duration reached 34.05 ms, longer than the 13.89 ms 72 Hz frame interval.
- The D3D11 readback failure counter remained at zero.
- Host H.264 and UDP output generally followed the raw-frame arrival rate. The stream could reach about 50 FPS when raw frames arrived, but dropped to about 9.9 FPS during periods where `Present` was also about 9.8 Hz.
- The Host's unmatched H.264 timestamp queue often held 5 to 9 frames, adding avoidable latency.

## Driver changes

- `GetTimeSinceLastVsync` now observes only a VSync event that has actually completed. The previous implementation published a future VSync timestamp and frame counter before `WaitForPresent` had reached that timestamp. This allowed the compositor to observe a future event as the last completed event.
- The virtual VSync scheduler now uses the first comparable `PresentInfo_t::flVSyncTimeInSeconds` as its phase reference, then advances at a fixed 72 Hz interval. Stale reservations are rebased to at most one interval from the current time rather than preserving a far-future reservation.
- The frame counter advances by the number of virtual refresh periods elapsed, including periods skipped because the callback arrived late. It remains monotonically increasing and is committed only after the scheduled wait completes.
- `WaitForPresent` callbacks are serialized so concurrent calls cannot reserve overlapping virtual scan-out slots.
- The wait path uses a high-resolution Windows waitable timer when supported, with a short yield-spin at the end of the wait. This avoids depending on `Sleep(1)` rounding to the system's coarse timer quantum, which can exceed a 72 Hz frame budget.
- The HMD render target in this source tree is already 1600x720 SBS (`800x720` per eye), matching the encoded stream. This revision does not change that resolution.

## Host encoder latency change

- The unmatched H.264 timestamp queue is reduced from 12 to 4 frames, limiting this queue to about 67 ms at 60 FPS.
- QSV `async_depth=2`, NV12 output, no look-ahead, no B-frames and the existing low-delay rate-control settings are retained. The QSV depth is kept at 2 to preserve the throughput already observed on the target PC while trimming the larger application-side queue.
- When the four-frame timestamp queue is full, the current raw frame is dropped and the same FFmpeg session remains alive. The next frames can proceed as soon as output access units retire timestamps.

## Expected next-run diagnostics

At a stable 72 Hz virtual display clock:

- `Present` and `WaitForPresent` should normally settle near 72 callbacks per second while a VR scene is active.
- `max-WaitForPresent` should generally stay within one 13.89 ms refresh interval, apart from scheduler/driver outliers.
- `GetTimeSinceLastVsync` should report a small non-negative offset after each completed wait, and the virtual frame counter must never decrease.
- The five-second driver line now includes `early-wait` plus counts for `VSync_None`, `VSync_WaitRender` and `VSync_NoWaitRender`, helping identify callback pairing or pacing-mode differences if the compositor still reports unexpected rates.
- Host `pending` should normally remain at or below 4. If it is constantly 4 and H.264 output remains below the target, the remaining limit is encoder throughput or UDP/decoder service time rather than a large timestamp backlog.
- Compare Host `H.264` FPS with Android `Video RX` and `Video decode` FPS to determine whether the remaining frame loss is before or after Wi-Fi delivery.

## Validation status

Go unit tests, race tests, `go vet`, Windows/amd64 Host cross-build and Python release tests were run after these changes. Native MSVC compilation of the SteamVR driver, QSV runtime validation on the MX450/Iris Xe laptop and Redmi K40/LineageOS 24 end-to-end latency measurements still require the target machine.
