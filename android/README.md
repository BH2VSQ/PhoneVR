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


## Remote display settings

When paired with a compatible Windows Host, the Android client publishes its current `DisplaySettings` over the TCP control channel. Host-side edits are validated, persisted in SharedPreferences, and applied to the OpenGL renderer immediately. The local Android calibration panel remains available and publishes changed values after each completed slider interaction.


## Remote display settings from the Windows Host

On successful TCP pairing the app sends its current `DisplaySettings` JSON. A `set_display_settings` control message is parsed on the control reader, applied on the main thread, normalized, saved through `SettingsStore`, and passed to `StereoRenderer.updateSettings`. The app returns `display_settings_ack` with the effective settings. The Daydream View 2017 preset seeds the published baseline coefficients and lens spacing; later manual calibration edits are preserved.
