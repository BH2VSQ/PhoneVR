# PhoneVR 0.5.30 Android ScrollView compilation hotfix

## Reported CI failure

The 0.5.29 Android build failed in `MainActivity.kt` at the first-level menu's `menuScroll.addView(...)` call:

```text
Unresolved reference: LayoutParams
```

The code used `ScrollView.LayoutParams`. Android's `ScrollView` is a `FrameLayout` subclass, and the direct child therefore uses `FrameLayout.LayoutParams`.

## Fix

- Replace the invalid `ScrollView.LayoutParams` reference with `FrameLayout.LayoutParams` for the first-level menu's scroll content.
- Extend the menu layout regression test to assert the valid parameter type and reject `ScrollView.LayoutParams`.
- Bump the synchronized release metadata to PhoneVR 0.5.30 and Android versionCode 65.

## Validation

Run `python3 -m unittest discover -s tools/release -p 'test_*.py'` and the Android CI workflow (`:app:testDebugUnitTest`, `:app:assembleDebug`, and `:app:assembleRelease`).
