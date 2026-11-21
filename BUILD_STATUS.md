# PhoneVR 0.4.16 Build Status

## 0.4.16 independent SteamVR pose publishing

- HMD pose publication has moved to a dedicated above-normal-priority worker. The worker is awakened by a new receiver generation and performs a 10 ms keepalive publication when no new sensor sample arrives. `IServerTrackedDeviceProvider::RunFrame()` no longer determines the HMD pose publication cadence.
- `vrserver.txt` now reports the driver UDP RX rate, total pose publications, unique/repeated publications, sequence gaps, drained queue packets, maximum publication gap and maximum sample age. It also reports `RunFrame` frequency and maximum callback gap for side-by-side comparison.
- `IVRVirtualDisplay::Present` and `WaitForPresent` callbacks now report their rates and maximum durations. These metrics can determine whether smooth Host preview but jerky SteamVR view comes from pose scheduling or virtual-display/compositor GPU synchronization.
- The virtual display remains configured at 72 Hz. A 180 Hz sensor feed does not imply 180 Hz image presentation. Higher refresh must be selected only after confirming the target phone panel and SteamVR timing behavior.
- Publisher thread is stopped and joined before HMD object ID invalidation; it submits stack-local pose records. Receiver uses generation-based condition-variable wakeups to avoid missed updates.

## Validation in this environment

- Release/source regression tests: 24 passed
- Go Host unit tests and Race tests: passed
- Go Vet and Windows AMD64 Go Vet: passed
- Windows AMD64 Go test binary, GUI Host and console Host cross-builds: passed (PE32+ x64 outputs)
- All three GitHub Actions workflow YAML files parse successfully
- Native MSVC SteamVR DLL build and actual SteamVR runtime test: not available in this environment; must be verified on the Windows target
- Android sources unchanged in this revision; no Gradle build required for these C++/Host changes

## Runtime interpretation guide

- `driver-rx` close to Host `forwarded`, and `published` close to `driver-rx`, with low `max-publish-gap` and `max-sample-age`: pose data is arriving and being submitted continuously; investigate compositor/display timing if motion still feels jerky.
- Low `published` despite high `driver-rx`: investigate pose worker / driver thread scheduling.
- Low `RunFrame` rate while `published` remains high: Runtime callback cadence is not throttling pose submissions.
- High `max-Present` or `max-WaitForPresent`: investigate D3D11 shared texture, keyed mutex, GPU device selection or compositor load.
- `Present`, `WaitForPresent` and pose rates all stable but view still feels low cadence: the configured virtual display refresh (72 Hz) or PC rendering workload may be the limiting factor.

