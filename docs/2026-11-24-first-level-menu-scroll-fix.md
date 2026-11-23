# PhoneVR 0.5.29 first-level menu scrolling fix

## Problem

The top-level menu opened from the `⋮` button still placed its status panel and action buttons directly in a vertical `LinearLayout`. The Daydream controller pairing panel had its own `ScrollView`, but that did not make the first-level menu scrollable. On short landscape viewports, lower menu actions could be clipped with no scroll container to receive vertical gestures.

## Change

- Move the connection/status panel and all first-level menu actions into one vertical content layout.
- Wrap the complete content in a `ScrollView`, including the screen-rotation and optical-preset actions.
- Bound the scroll viewport to the available display height while keeping the menu panel itself `wrap_content`, so short lists retain their compact height and long lists can scroll.
- Enable smooth scrolling, a vertical scrollbar, overscroll feedback and descendant focus handling.
- Keep the existing menu button actions and the 6 dp spacing between adjacent actions unchanged.

## Validation

- Release Python regression suite: 67 tests passed.
- Windows Host Go tests, race tests, vet and Windows AMD64 cross-build: see the release build status.
- Android APK compilation and physical device gesture verification still require an Android SDK build environment and a real device.
