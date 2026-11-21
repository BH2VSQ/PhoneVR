# PhoneVR 0.4.19 DisplayRedirect diagnostic build status

This revision adds diagnostic telemetry around SteamVR's DisplayRedirect component selection.

## Changes in this diagnostic revision

- Log the first `GetComponent` request received by the PhoneVR HMD and DisplayRedirect devices.
- Log the exact requested interface name and whether it matches the expected OpenVR interface version.
- Log successful DisplayRedirect activation, object ID, adapter LUID, and the result of each property write.
- Report cumulative DisplayRedirect component-query and interface-match counters in the five-second driver telemetry.
- Use case-insensitive OpenVR component-name matching and an atomic DisplayRedirect active flag.

## Verification status

- Source-level validation: performed in the current Linux workspace.
- Go Host and Python integration tests: unchanged by this driver-only diagnostic patch; rerun before release.
- Windows x64 MSVC / SteamVR driver build: not run here because this environment does not have the Windows SDK and MSVC toolchain.
- SteamVR runtime callback verification: requires deploying this diagnostic driver on the target PC.

The version is intentionally identifiable as `PhoneVR 0.4.19 DisplayRedirect diagnostics SteamVR driver initialized` in `vrserver.txt`. Do not infer that the streaming path is fixed from a successful driver build. The purpose of this build is to show whether SteamVR requests and receives `IVRVirtualDisplay_002`.
