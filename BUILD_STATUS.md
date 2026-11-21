# PhoneVR 0.4.19 Build Status

## LAN discovery and SteamVR D3D11 compile fix

- Included `<d3d11_4.h>` after `<d3d11.h>` so the `ID3D11Multithread` interface used by the D3D11 capture context is declared by the Windows SDK.
- Android discovery automatically enumerates active, non-loopback IPv4 interfaces, binds discovery sockets to each local address and sends directed broadcasts. The Android connection panel no longer exposes transport selection; manual IPv4 host entry remains available.
- The Windows Host binds its network services to the configured listen address (default `0.0.0.0`) and logs active IPv4 interface addresses. LAN discovery, TCP control 48101, UDP pose 48102 and outbound H.264 UDP 48104 continue using the existing protocol.
- Android versionCode is 35. Repository, Host and SteamVR driver version is 0.4.19.

## Validation status

- Go unit/integration tests, Go Race tests and Go Vet passed before this cleanup. They should be rerun after the interface-classification changes.
- Windows AMD64 `go vet`, test-binary cross-compilation and Host executable cross-compilation passed before this cleanup. This validates the Go Host only, not the native SteamVR DLL.
- Python release-tooling and network-discovery regression tests were updated for the simplified discovery flow; they have not yet been rerun.
- Kotlin discovery/model code was previously compiled using local Kotlin plus minimal Android/JSON/video stubs. This is a source/type check, not a full Android application build.
- GitHub Actions workflow YAML (CI, release and SteamVR driver) parsed successfully before this cleanup.
- Full Android Gradle/APK build remains unverified. Gradle Wrapper could not download Gradle 9.6.1 in the prior environment because `services.gradle.org` DNS/network access was unavailable.
- Full Windows MSVC SteamVR driver build remains unverified here. The reported `ID3D11Multithread` C2065 is addressed by including the Windows SDK header `<d3d11_4.h>`; rerun the native build with the Visual Studio toolchain to confirm.
- Full end-to-end LAN discovery, hardware H.264 encoder initialization and Android MediaCodec rendering still require testing with the target Windows PC and phone.
