# Build Status - PhoneVR 0.3.11 UI and Optical Preset Update

Application version: `0.3.11`  
Android versionCode: `14`  
Network protocol: `v1` (additive `android-optical-presets-v1` capability)

## Changes in this revision

- Fixed the reported Android compilation error by importing `android.content.Intent` in `NetworkClient.kt`.
- Android now persistently stores the optical preset catalogue. Existing manually calibrated display settings are migrated to a local preset on first upgrade so the previous configuration is not discarded.
- The Windows Host actively requests the catalogue after a device connects. The Android app returns its local list; host-side create/edit/clone/rename/delete operations are written back to Android and acknowledged.
- Android's former manual calibration screen is now an optical preset selector. Applying a preset is performed locally on the phone; the host sends a preset ID rather than a second independent copy of its parameters.
- The Windows Optical Presets list double-click opens the selected preset's editor, which now contains the former phone display parameter controls.
- Added light/dark mode selection and a persisted custom HEX accent color. Theme changes refresh the main window, popup menu, child dialogs and their native controls.
- Child dialogs use a custom client-drawn header without a native caption close button; their in-app Close buttons remain available.
- The English SteamVR status pill has adaptive width, and the OpenGL viewport layout preserves at least 16 px between the preview and the Yaw/Pitch/Roll cards.

## Validation completed in this environment

- `go test -race -count=1 ./...`: passed.
- `go vet ./...`: passed.
- `GOOS=windows GOARCH=amd64 go build`: passed.
- `GOOS=windows GOARCH=amd64 go test -c`: passed.
- Android `NetworkClient.kt` now imports the missing `Intent` symbol identified by the provided CI log.
- A standalone Kotlin compiler check of the changed settings, optical preset and network modules passed against lightweight Android/JSON API stubs. This checks Kotlin syntax and internal type consistency, but is not a substitute for the Android Gradle build.
- `python3 -m unittest tools/release/test_bump_version.py`: passed (5 tests).

## Not run here

- Android Gradle unit tests and APK assembly: not run because this execution environment has no Android SDK (`ANDROID_HOME` and `ANDROID_SDK_ROOT` are unset, and no `android.jar`/SDK platform is installed). The Android changes therefore still require CI or an Android SDK machine to confirm Kotlin compilation and APK packaging.
- Interactive Windows desktop verification of light/dark Win32 control rendering, display scaling, SteamVR label fit and child-window dragging/scrolling. Windows AMD64 cross-compilation passed, but runtime appearance should be checked on Windows.


## Android unit-test runtime fix

The 0.3.11 CI log showed that Android Kotlin compilation succeeded, but two `OpticalPresetTest` cases failed with `NullPointerException` while constructing/using `org.json` objects. Local Android unit tests run against the mockable Android SDK jar, whose `org.json` methods are stubs. Added the JVM `org.json:json:20240303` implementation to `testImplementation` only; production Android runtime continues to use the platform `org.json` implementation.

Validation note: this container does not include an Android SDK (`ANDROID_HOME` / `ANDROID_SDK_ROOT` are unset and no `android.jar` is installed), so the Gradle test/build tasks cannot be rerun here. The fix targets the exact test-runtime failure shown in the supplied CI log.
