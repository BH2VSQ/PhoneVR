# PhoneVR 0.4.3 Build Status

## Fixed
- Corrected the Win32 OPENFILENAMEW layout by including lpEditInfo and lpstrPrompt before pvReserved/dwReserved/FlagsEx. The previous structure was 16 bytes too short on x64 and caused the native common dialog to overwrite memory.
- Added architecture-specific ABI regression checks for x64 and x86 structure sizes and critical field offsets.
- Kept native dialog buffers alive for the full syscall and added CommDlgExtendedError diagnostics.

## Verified
- `go test ./...`
- `go test -race ./...`
- `go vet ./...`
- Windows AMD64 GUI build
- Windows AMD64 Console build
- Windows AMD64 test binary cross-compilation

## Not verified in this environment
- Interactive GetOpenFileNameW execution on a real Windows desktop.
- SteamVR driver install/uninstall against a live SteamVR runtime.
