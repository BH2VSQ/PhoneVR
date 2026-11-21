# Build Status — PhoneVR 0.4.1

## Android hotfix

Fixed `CodecCapabilityProbe.kt` compile errors from the 0.4.0 CI log:

- `maxSupportedInstances` is read from `MediaCodecInfo.CodecCapabilities`, where Android defines it (API 23+).
- `VideoCapabilities.getSupportedFrameRates()` returns `Range<Integer>`, so the upper bound is converted to `Double` for `VideoDecoderCapability.maxFrameRate`.
- The probe source passes a Kotlin compiler check against API-shaped Android/JSON stubs. Full Gradle and APK validation still requires the Android SDK Platform 36 build environment.

## SteamVR driver install

The driver must be built and registered separately on Windows. `scripts/build-steamvr-driver.ps1` builds the DLL into `dist/steamvr-driver`; `tools/install-steamvr-driver.ps1 -DriverDirectory .\dist\steamvr-driver` registers it with SteamVR's `vrpathreg.exe`. Add `-Remove` to unregister it. SteamVR must be installed.

The 0.4.x driver is still a foundation: `IVRVirtualDisplay::Present` does not yet open/copy the D3D11 backbuffer, encode frames, or send video to Android. It is not yet a working PC-to-phone streaming driver.
