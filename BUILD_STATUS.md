# PhoneVR 0.4.17 Build Status

## 0.4.17 pose-state isolation and selectable virtual refresh

- Moved latest pose, pose packet count and last-packet timestamp behind a dedicated `poseStateMu`; the 180 Hz pose writer no longer contends with the general Host service mutex used by device lists, heartbeats and logs. `Snapshot()` releases the general service lock before taking the pose snapshot lock.
- Added regression tests for display refresh presets and custom-rate validation.
- Added Host menu item **Display Refresh Rate** with 60 Hz, 90 Hz, 120 Hz and Custom. Custom is bounded to 60–120 Hz in 0.1 Hz increments to stay within the known 120 Hz phone panel ceiling. Default remains 72 Hz (Custom) for backward compatibility; select 90 Hz or 120 Hz explicitly after confirming runtime performance.
- Host writes `%LOCALAPPDATA%\PhoneVR\display-refresh.json`; the SteamVR driver reads it at provider startup and uses the same rate for `Prop_DisplayFrequency_Float`, timing diagnostics and `GetTimeSinceLastVsync`. A complete SteamVR restart is required after changing the value.
- Host and SteamVR display timing are deliberately kept separate from the ~180 Hz IMU update rate. Increasing virtual refresh does not create additional source frames; if the PC compositor cannot sustain the selected rate, dropped/reprojected frames may increase.

## Validation

- Go unit and integration tests, Race and Vet: run in this revision's validation.
- Windows AMD64 Host cross-compilation and vet: run in this revision's validation.
- Python release-tool tests and workflow YAML parse: run in this revision's validation.
- C++ raw-source checks: config parser, shared setting file path and dynamic VSync interval statically inspected.
- Native MSVC SteamVR DLL build and real SteamVR timing test: requires the Windows target; not claimed as completed in this environment.
- Rich Win32 dialog interaction and actual 60/90/120/custom mode switching also require Windows runtime verification.

## How to interpret the next SteamVR log

- `driver-rx` and `unique` near 170–180 Hz with low `max-publish-gap` means fresh IMU poses reach OpenVR continuously.
- `Present` / `WaitForPresent` / `completed` near the selected refresh rate with low callback durations indicates the virtual display is keeping pace.
- Low `completed` relative to selected refresh or high `max-WaitForPresent` points to compositor/GPU synchronization or PC rendering load.
- If pose metrics remain healthy but motion still appears uneven, test 60, 90 and 120 Hz one at a time and compare SteamVR frame timing; do not infer that the highest setting is necessarily the smoothest.
