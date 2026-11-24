# VRChat controls for PhoneVR Daydream controllers

PhoneVR registers two independent SteamVR controller devices, one left and one right. VRChat uses SteamVR Input 2.0 action sets, including `one_hand` when one controller is powered off or unavailable. The `one_hand` action set is mirrored by VRChat, and this profile also maps both physical sides to the same one-hand actions.

## Default two-hand layout

| Daydream control | VRChat action |
| --- | --- |
| Left touchpad position | Move |
| Right touchpad position | Rotate |
| Either App button, short press | Quick Menu / Menu action |
| Either App button, hold | Action Menu |
| Either App button, double press | Main Menu |
| Either touchpad click | Interact |
| Either Volume Down button | Grab |
| Either Volume Up button | Jump |
| Either touchpad position in Quick Menu | Scroll |
| Either touchpad click in Quick Menu | Confirm |
| Either touchpad position in Action Menu | Radial menu axis |
| Either touchpad click in Action Menu | Select |

## One-controller layout

When VRChat activates its `one_hand` action set, either controller's touchpad position drives `Move & Rotate`. App, touchpad click, Volume Down and Volume Up remain available for Menu, Interact, Grab and Jump. This provides a usable control path when only one Daydream remote is connected.

## Buttons and recentering

The App button is a holdable menu input: a short press opens the Quick Menu, holding it opens the Action Menu, and a double press requests the Main Menu. The Home button keeps its SteamVR system click on a short press. Holding Home for 800 ms requests a software yaw recenter.

Recenter now aligns only the controller's horizontal heading with the headset's horizontal forward direction. It preserves the controller's current pitch and roll, and ignores headset pitch and roll when building the alignment. This avoids changing the pointing elevation or banking angle merely because the user tilted their head during a reset.

## Direction convention

The Daydream BLE touch coordinates are normalized from 0 to 1. The driver maps them to SteamVR's two-sided trackpad range from -1 to +1. X increases to the right, and Y increases upward. When the finger is not touching the pad, both axes return to zero.

The default profile targets VRChat on Steam (`steam.app.438100`) and is offered as a SteamVR driver default. SteamVR user-created bindings have higher priority and can override this default.

## Limitations

Daydream remotes provide 3DoF orientation. PhoneVR estimates controller position with an arm model, so the controllers do not provide real 6DoF hand translation, finger tracking or analog trigger travel. The physical touchpad click is used for Interact and Confirm. Volume buttons are repurposed for Grab and Jump in this VRChat profile.
