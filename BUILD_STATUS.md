# PhoneVR 0.4.6 Build Status

## Changed
- Updated the SteamVR Driver Manager status area to a read-only multiline edit control. Long paths wrap automatically, and users can select and copy the status text with Ctrl+A / Ctrl+C.
- Increased the status area height, moved the three driver action buttons down, and removed the decorative footer frame around the Close button.
- Removed the local ZIP picker, ZIP download/install entry points, and the native OPENFILENAMEW structure/call path from the Host UI and driver manager.
- Added a Go `embed.FS` payload for the compiled PhoneVR OpenVR driver. The standard Windows Host build compiles the C++ driver first, stages its manifest/DLL/resources into the embed directory, and embeds them in both GUI and console Host executables.
- Added content-hash versioned extraction under `%LOCALAPPDATA%\PhoneVR\SteamVRDriver`, manifest/PE-signature validation, copy verification and SteamVR `vrpathreg.exe` registration.
- Added startup auto-deployment when SteamVR is installed, plus a persistent opt-out marker written by uninstall. The in-app Install / Repair operation clears the marker.
- Uninstall only deletes PhoneVR-managed files. A registered external driver path is unregistered but its files are left untouched.
- Removed the obsolete ZIP release downloader/extractor Go package and its tests. The separate standalone CMake build workflow remains available for developer testing.
- Updated release packaging so the Windows Host binaries contain the driver and no separate driver ZIP is published.

## Verified in this environment
- `go test ./...`
- `go test -race ./...`
- `go vet ./...`
- Windows AMD64 Host test-binary cross-compilation
- Windows AMD64 GUI Host cross-build (source checkout contains only the embed placeholder; the real driver payload is populated by the Windows build script)
- Windows AMD64 GUI Host build after the 0.4.6 Driver Manager UI update

## Requires a Windows build/runtime check
- Run `scripts/build-windows-host.ps1` on Windows with CMake, Git and Visual Studio C++ x64 tools to compile and embed the real driver DLL.
- Launch the resulting Host with SteamVR installed and verify automatic registration and driver loading.
- Verify Install / Repair, Uninstall & Disable Auto Deploy, restart behavior, and registration paths against the live SteamVR runtime.
- The native desktop UI appearance/interaction and real SteamVR runtime cannot be exercised in this Linux build environment; verify text wrapping, Ctrl+A/Ctrl+C, button spacing and the frameless Close area on Windows.
