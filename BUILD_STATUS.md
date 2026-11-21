# PhoneVR 0.4.19 Build Status

## USB tethered connection support and SteamVR D3D11 compile fix

- Included `<d3d11_4.h>` after `<d3d11.h>` so the `ID3D11Multithread` interface used by the D3D11 capture context is declared by the Windows SDK. Microsoft documents this interface in `d3d11_4.h`.
- Android discovery now supports Auto, Wi-Fi-only and USB/RNDIS-only modes. It enumerates active IPv4 interfaces, binds discovery sockets to each local interface address and sends directed broadcasts to each interface's IPv4 broadcast address. Results are tagged with their interface transport, and Auto mode prefers a USB-discovered host when multiple addresses are returned.
- The Host continues binding to `0.0.0.0`, so TCP control 48101, UDP discovery 48100, UDP pose 48102 and outbound H.264 UDP 48104 use the selected RNDIS subnet without a protocol tunnel. Existing loopback pose, status and raw-frame bridge ports remain loopback-only.
- Added Host interface/address logging and `tools/enable-usb-tethering-firewall.ps1` to add/remove LocalSubnet-scoped inbound rules on all Windows firewall profiles for TCP 48101 and UDP 48100/48102.
- USB mode requires a data-capable USB cable and Android system USB tethering / USB 网络共享 enabled. It does not require ADB or USB debugging. Wi-Fi and USB can coexist; the user chooses Auto, Wi-Fi or USB in the Android connection panel.
- Android versionCode is 35. Repository, Host and SteamVR driver version is 0.4.19.

## Validation status

- Go unit/integration tests: passed.
- Go Race tests and Go Vet: passed.
- Windows AMD64 `go vet`, test-binary cross-compilation and Host executable cross-compilation: passed. This validates the Go Host only, not the native SteamVR DLL.
- Python release and USB/RNDIS static regression suite: 36 tests passed.
- Kotlin discovery/model code and the updated `NetworkClient.kt` compiled using local Kotlin plus minimal Android/JSON/video stubs. This is a source/type check, not a full Android application build.
- GitHub Actions workflow YAML (CI, release and SteamVR driver): parsed successfully.
- Full Android Gradle/APK build: not verified. Gradle Wrapper could not download Gradle 9.6.1 because `services.gradle.org` DNS/network access was unavailable in this environment.
- Full Windows MSVC SteamVR driver build: not verified here. The reported `ID3D11Multithread` C2065 is addressed by including the Windows SDK header `<d3d11_4.h>`; rerun the native build with the user's Visual Studio 2026 toolchain to confirm.
- USB/RNDIS discovery and routing, Windows Firewall rules, actual hardware H.264 encoder initialization, and end-to-end Android MediaCodec rendering require testing with the target Windows PC and phone. The firewall PowerShell helper could not be parsed in this non-Windows environment.
