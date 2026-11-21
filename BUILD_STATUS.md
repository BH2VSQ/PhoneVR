# PhoneVR 0.4.2 Build Status

## Implemented

- Added a native **SteamVR Driver** manager entry to the Windows Host main menu.
- Host can query the registered `phonevr` driver through SteamVR `vrpathreg.exe`.
- Host can query the latest PhoneVR GitHub Release and download the fixed asset `PhoneVR-SteamVR-Driver-Windows-x64.zip`.
- Host can select a local driver ZIP through the Windows file picker.
- Before installation, the ZIP is checked for path traversal, symlinks, archive size, expanded size, the root `driver.vrdrivermanifest` (`name=phonevr`) and `bin/win64/driver_phonevr.dll`.
- Host extracts the validated driver under `%LOCALAPPDATA%\PhoneVR\SteamVRDriver`, registers it with SteamVR, and replaces a previous `phonevr` registration. Failed updates attempt to restore the prior registration.
- Host can unregister the PhoneVR driver. It deletes only files installed under its managed LocalAppData directory; paths installed manually elsewhere are only unregistered.
- Release workflow builds the Windows x64 driver, packages it as `PhoneVR-SteamVR-Driver-Windows-x64.zip`, and uploads it as a GitHub Release asset.

## Validation completed in this environment

- `go test ./...` — passed.
- `go test -race ./...` — passed.
- `go vet ./...` — passed.
- Windows AMD64 GUI Host cross-build — passed.
- Windows AMD64 Console Host cross-build — passed.
- Windows AMD64 Host test-binary cross-compile — passed.
- GitHub Actions workflow YAML parse and dependency checks — passed.
- ZIP validation tests cover valid package extraction, incorrect manifest identity, missing DLL and path traversal rejection.

## Not verified here

- Native Windows file picker and Win32 dialog interaction were not exercised on a Windows desktop.
- SteamVR `vrpathreg.exe` registration/unregistration was not run against a real SteamVR installation.
- The C++ SteamVR DLL was not built in this Linux environment; the Release workflow now builds it on `windows-latest` and packages it for download.
- The workflow has not been run against GitHub Releases from this local work copy. The new Release asset becomes available after these changes are pushed and a Release workflow run completes.
- Actual SteamVR video capture/encoding/Android streaming remains out of scope for this driver-manager feature.

## Release asset contract

The Host queries `https://api.github.com/repos/BH2VSQ/PhoneVR/releases/latest` and expects the asset name exactly:

`PhoneVR-SteamVR-Driver-Windows-x64.zip`

The ZIP root must contain `driver.vrdrivermanifest` and `bin/win64/driver_phonevr.dll`.
