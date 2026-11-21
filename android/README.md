# PhoneVRStream Android Client

Android Studio project for the PhoneVRStream mobile HMD client.

## Requirements

- Android Studio
- JDK 17+
- Android SDK Platform 36
- Android SDK Build-Tools 36.0.0
- Android Gradle Plugin 9.5.0
- Gradle 9.6.1 (downloaded by the repository launcher)

## Open in Android Studio

Open this `android/` directory as the project root. In Gradle settings, use the project Gradle Wrapper.

The project includes `gradlew`, `gradlew.bat`, `gradle/wrapper/` and the repository-owned bootstrap launcher. A system-wide Gradle installation is not required.

## Windows PowerShell

For the full repository build, run `.\scripts\build-all.ps1` from the repository root. To build Android only on Windows, run `.\scripts\build-android.ps1` from the repository root. On Linux/macOS, use `bash ./scripts/build-android.sh` from the repository root.

The Gradle Wrapper can also be invoked directly from this directory:

```powershell
# Run unit tests
.\gradlew.bat :app:testDebugUnitTest

# Build Debug and Release APKs
.\gradlew.bat :app:assembleDebug :app:assembleRelease
```

Debug APK:

```text
app\build\outputs\apk\debug\app-debug.apk
```

Release APK (unsigned):

```text
app\build\outputs\apk\release\app-release-unsigned.apk
```

## GitHub Actions

`.github/workflows/android.yml` runs `scripts/build-android.sh`, which executes Android unit tests, assembles debug and release APKs, and uploads them as a workflow artifact. The workflow is triggered when files under `android/`, the root `scripts/` directory or the shared network protocol document change.

The release APK is currently unsigned. A signing configuration should only be added when release distribution is required, using GitHub Actions secrets for the keystore and credentials.


## PC connection panel

The PC connection overlay is designed for landscape use. The PC address, control port, LAN discovery action, discovered host list and guidance text are inside a vertical ScrollView. The Connect and Disconnect actions remain fixed at the bottom of the panel so they stay available on shorter phone displays.


## Local optical presets

Optical presets are persisted in Android app storage. The first launch after upgrading an installation without a preset catalogue migrates the previous `DisplaySettings` values into an `原有显示配置` preset, preserving the user's prior calibration. Built-in Daydream View 2017 and Generic Cardboard presets are also stored locally. The Android overlay is a selector only: it lists available presets and applies the selected entry; it no longer exposes the former manual calibration sliders.

## Windows Host synchronization

After pairing, the Windows Host sends `get_optical_presets`. Android returns the locally stored catalogue as `optical_presets`. Host-side create/edit/clone/rename/delete operations are sent back with `set_optical_presets`; Android validates the entries, persists them, and acknowledges the normalized list with `optical_presets_ack`. To apply a preset, the Host sends `apply_optical_preset` with only the preset ID. Android resolves that ID from its local store, persists the active preset and effective display settings, updates `StereoRenderer`, and returns `display_settings_ack`.

The `display_settings` report and `set_display_settings` command remain supported for compatibility with earlier Host builds. The current Windows UI manages parameters through optical presets instead of directly editing a separate phone calibration state.


## Wired USB connection

The PC connection panel offers Auto, Wi-Fi and USB/RNDIS discovery. For wired streaming, connect the handset to the Windows PC using a data-capable USB cable and enable **USB tethering / USB 网络共享** in Android Settings. Select **USB 有线（RNDIS）** and search. The app enumerates active IPv4 interfaces and sends directed discovery broadcasts through the USB tether interface (normally `rndis0`, `usb0` or an Ethernet-style interface). Once discovered, the same TCP control, UDP pose and H.264 video flows run over the USB network. This mode does not require ADB or Android USB debugging.
