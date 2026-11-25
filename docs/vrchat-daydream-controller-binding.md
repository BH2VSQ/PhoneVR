# VRChat controls for PhoneVR Daydream controllers

PhoneVR registers two independent SteamVR controller devices, one left and one right. VRChat uses SteamVR Input 2.0 action sets, including `one_hand` when one controller is powered off or unavailable. The default PhoneVR binding maps the touchpad to the same continuous player-movement action in both Global and One_Hand contexts.

## Default movement layout

| Daydream control | VRChat action |
| --- | --- |
| Left touchpad position | Move (two-axis continuous movement) |
| Right touchpad position | Move (two-axis continuous movement) |
| Either App button, short press | Quick Menu / Menu action |
| Either App button, double press | Main Menu |
| Either App button, hold for 800 ms | Software yaw recenter |
| Either Home button, short press | SteamVR System click |
| Either touchpad click | Interact |
| Either Volume Down button | Grab |
| Either Volume Up button | Jump |
| Either touchpad position in Quick Menu | Scroll |
| Either touchpad click in Quick Menu | Confirm |
| Either touchpad position in Action Menu | Radial menu axis |
| Either touchpad click in Action Menu | Select |

## One-controller layout

When VRChat activates its `one_hand` action set, either physical controller's touchpad position is bound to `Move`. The default binding deliberately does not use `Move & Rotate`, which combines locomotion and rotation and can produce the direction-marker / release-to-move behavior reported with the previous configuration. A finger position away from the touchpad centre continuously supplies a two-axis movement vector. Returning to the centre or lifting the finger returns the axes to zero.

## Buttons and recentering

The App button uses a short-press / long-press gesture. Releasing it before 800 ms generates one Menu click. Holding it for 800 ms queues a software yaw recenter and suppresses the Menu click for that gesture. A double short press can still request Main Menu through the VRChat `double` binding. The Home button keeps a short-press SteamVR System click; it no longer controls recentering.

Recenter aligns only the controller's horizontal heading with the headset's horizontal forward direction. It preserves the controller's current pitch and roll, and ignores headset pitch and roll when building the alignment. The controller's raw Daydream Start Space orientation is kept stable while the headset turns. HMD yaw is used separately for arm-model positioning and does not get added to the controller's world orientation, preventing a stationary remote from appearing to rotate as the user turns their head.

## Direction convention

The Daydream BLE touch coordinates are normalized from 0 to 1. SteamVR receives the filtered values as a software joystick (two-sided X/Y axes), while the physical click and touch state remain separate inputs. X increases left-to-right and Y increases bottom-to-top, matching SteamVR's two-sided analogue axes (+X right, +Y up). A radial 0.15 deadzone, circular magnitude limit and soft response curve provide a joystick-like feel. When the finger is not touching the pad or the last controller report becomes stale, both axes return to zero.

The Daydream BLE protocol reports both touch coordinates as zero when the pad is untouched, so contact detection requires both coordinates to be non-zero. The edge of the active touch region remains valid input; the mapping does not require touching only the bottom edge.

The default profile targets VRChat on Steam (`steam.app.438100`) and is offered as a SteamVR driver default. SteamVR user-created bindings have higher priority and can override this default. After updating the PhoneVR driver, open SteamVR Settings → Controllers → Show Binding UI, select VRChat, then activate **PhoneVR Daydream VRChat Continuous Movement**. Reset an older personal binding if it continues to override this default.

For input-path diagnosis, Android Logcat (`PhoneVRDaydream`) reports the decoded touch state and normalized BLE coordinates. The Windows Host log reports the same left/right snapshots after the TCP control bridge. SteamVR `vrserver.txt` reports the raw coordinates and final joystick X/Y values after driver conversion. A valid forward path should show `touching=true` on Android and Host, followed by non-zero joystick axes in the driver log while the finger is away from the pad centre.

## Limitations

Daydream remotes provide 3DoF orientation. PhoneVR estimates controller position with an arm model, so the controllers do not provide real 6DoF hand translation, finger tracking or analog trigger travel. The physical touchpad click is used for Interact and Confirm. Volume buttons are repurposed for Grab and Jump in this VRChat profile.
