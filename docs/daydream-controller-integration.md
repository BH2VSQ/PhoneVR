# Google Daydream View Controller Integration

Status: dual-controller BLE and SteamVR input path implemented in PhoneVR 0.5.26. Android dual-device concurrency and physical SteamVR input still require validation with two real remotes.

## Supported hardware and scope

The initial target is the original Google Daydream View handheld remote. PhoneVR supports two independently assigned remotes, one left and one right. Each remote connects directly from the Android app through BLE GATT. The Android system Bluetooth settings page is not required for routine connection; pairing/assignment is performed in the PhoneVR controller panel and the selected device addresses are stored locally.

The Daydream remote provides 3DoF orientation and buttons/touchpad. It does not provide room-scale 6DoF hand position. PhoneVR therefore registers two tracked-controller devices with rotational tracking and a fixed head-relative hand anchor. The position is a visualization/interaction estimate, not measured hand position.

## BLE protocol from `mrdoob/daydream-controller.js`

The implementation follows the publicly available `DaydreamController.js` connection and notification format: https://github.com/mrdoob/daydream-controller.js

| Item | Value |
|---|---|
| Advertised local name | `Daydream controller` |
| GATT service | `0000fe55-0000-1000-8000-00805f9b34fb` (`0xFE55`) |
| Notification characteristic | `00000001-1000-1000-8000-00805f9b34fb` |
| CCCD | `00002902-0000-1000-8000-00805f9b34fb` |
| Notification size | 19 bytes minimum |

The Android parser decodes the fields using the bit-packing and scale factors in the reference project: 5-bit sequence, controller time ticks, three signed 13-bit orientation values, three signed 13-bit acceleration values, three signed 13-bit gyro values, touch X/Y and button flags. The wire order for each sensor triplet is X, Z, Y; the parser maps those values into the normal X, Y, Z axes before further processing. Button flags in byte 18 are click `0x01`, Home `0x02`, App `0x04`, volume down `0x08` and volume up `0x10`. The three orientation values are converted to a normalized axis-angle quaternion for the OpenVR controller pose.

The reference JavaScript code does not define an explicit touch-contact bit. PhoneVR infers touch contact from non-zero coordinates. Edge contact at an exact `(0,0)` coordinate and firmware-specific release-coordinate behavior need physical-device validation. The 5-bit sequence number wraps frequently and is used for diagnostics, not as a globally unique packet ID.

The reference JavaScript implementation creates one WebBluetooth connection per controller. It does not demonstrate concurrent dual-controller operation. PhoneVR uses two independent Android `BluetoothGatt` instances, but simultaneous operation is still a device compatibility test item.

## Android pairing and connection

1. Open the PhoneVR menu and select **Daydream 手柄配对**.
2. Wake a remote with its touchpad or App button, then select **扫描 Daydream Controller**.
3. Assign the discovered device to the left hand or right hand.
4. Repeat the scan for the second remote and assign it to the other hand.
5. The two addresses are saved independently. Reopening PhoneVR attempts to restore both links.
6. Use the left/right disconnect buttons to remove an assignment and clear its saved address.

Android 12 and later require runtime `BLUETOOTH_SCAN` and `BLUETOOTH_CONNECT` permission. Earlier Android releases use the legacy Bluetooth permissions and fine-location permission for scanning. The controller manager maintains independent reconnect backoff for each hand. If no valid notification arrives for 750 ms, it publishes a neutral disconnected input snapshot so held buttons and tracking do not remain stuck. A later valid notification restores input automatically.

## Input mapping in SteamVR

| Daydream control | PhoneVR SteamVR input | Notes |
|---|---|---|
| Touchpad X/Y | `/input/joystick/x`, `/input/joystick/y` | Normalized to -1..1 while touching |
| Touchpad contact | `/input/joystick/touch` | Contact inferred from coordinates |
| Touchpad click | `/input/joystick/click` | Digital click |
| Touchpad click compatibility alias | `/input/trigger/click`, `/input/trigger/value` | Exposed to legacy applications as a trigger; hardware has no independent analog trigger report in this protocol |
| App button | `/input/menu/click` | Application/menu button |
| Home button | `/input/system/click` | System button |
| Volume + | `/input/volume_up/click` | Exposed as a separate button |
| Volume - | `/input/volume_down/click` | Exposed as a separate button |
| Controller orientation | tracked-device pose | 3DoF orientation |

The protocol described by the reference does not expose separate grip or trigger sensors. PhoneVR does not synthesize a grip component. Touchpad click is also exposed as a trigger compatibility alias because many legacy SteamVR applications expect a trigger-like primary action. The SteamVR input profile and legacy bindings are installed under `steamvr-driver/resources/input/`.

## Separation from HMD tracking

The phone IMU remains the sole HMD pose source. HMD orientation continues over the existing `PVSP` UDP channel and is never overwritten by either controller quaternion. Controller state travels over the existing TCP control connection as a `controller_state` JSON message and is forwarded separately through the loopback-only SteamVR controller bridge on UDP port `48108`.

The Host converts each hand to one 56-byte `PVSC` v2 packet, including the decoded controller gyroscope vector. The driver validates each packet and publishes its left/right controller state independently. The control channel carries controller data at up to 50 Hz while a controller is active; pose updates are still published on the SteamVR driver frame loop.

## Position and coordinate limitations

The Daydream report contains orientation, accelerometer and gyro data, but no physical translation measurement. PhoneVR estimates controller position with a Google GvrArmModel-style arm model: HMD world position and horizontal gaze define the head/torso reference, the controller orientation determines arm extension and elbow/wrist rotation, and the torso reference follows HMD yaw with a time-based filter independent of controller gyroscope magnitude. The model uses configurable left/right handedness and fixed anthropometric offsets. It provides a plausible head-relative hand position, not measured hand translation, optical tracking or room-scale 6DoF. User arm length, grip angle and shoulder geometry can still differ from the model defaults.

The current implementation does not include controller haptics, battery telemetry, a separate grip sensor, an analog trigger sensor, or an adjustable hand-model calibration screen.

## Test checklist

- Pair one controller as left and verify its touchpad, click, App, Home and volume inputs independently.
- Pair a second controller as right and verify both controllers update concurrently without exchanging hand assignment.
- Turn off one remote or let it sleep; confirm the other remains active and the inactive controller releases all inputs after the 750 ms report timeout.
- Wake the inactive remote and confirm it resumes without restarting the HMD session.
- Close and reopen PhoneVR; confirm saved left/right assignments reconnect.
- Test touchpad edges, center click, all buttons and orientation axes in SteamVR's controller test/input binding UI.
- Verify the controller tracking pose remains independent while the phone HMD is moved.
- Test with the target phone's Android version and record whether two concurrent GATT links remain stable.

## Reference

- `mrdoob/daydream-controller.js`: https://github.com/mrdoob/daydream-controller.js
- Android Bluetooth GATT: https://developer.android.com/develop/connectivity/bluetooth/ble/ble-overview
- OpenVR driver input components: https://github.com/ValveSoftware/openvr


## 3DoF controller pose calibration and arm model

PhoneVR converts the Daydream XKF3 orientation fields from their signed axis-angle rotation-vector representation into a quaternion. Treating those three values as independent Euler angles produces incorrect rotations, especially when more than one axis is active. The BLE packet reference describes the fields as an axis-angle rotation vector in turns; PhoneVR converts each packed component to radians before quaternion construction.

The driver keeps the controller's Start Space orientation and applies the HMD horizontal-yaw delta to that reference every update. The estimated hand therefore follows the player's heading even when the controller gyro is stationary, while controller pitch and roll remain available for pointing. The arm-model torso follows HMD yaw with a time-based filter independent of controller gyro magnitude. The controller quaternion is aligned to the HMD world reference at initial tracking and can be recalibrated on demand. The App button has a dual action: a short press remains the menu action; holding it for approximately 800 ms requests a host-side software recenter against the current HMD view direction. The Home button follows the Daydream convention: a short press maps to SteamVR System, while a long press also requests software recenter. Gesture detection runs from fresh controller input even while HMD tracking is temporarily invalid. A recenter request is queued until a valid HMD pose is available, then applied once. The Android app and SteamVR driver log App/Home press, release, long-press recognition and applied recenter stages so the event path can be diagnosed.

The BLE Control Characteristic remains unused for recentering. The available reverse-engineering reference documents its write-only property but does not specify `0x00` as a recenter command, so PhoneVR does not send an undocumented hardware write. Home remains mapped to the SteamVR system button.

For position, the SteamVR driver uses a Google Daydream-style arm model. It estimates elbow, wrist and controller offsets from controller orientation, handedness and the HMD pose. The estimated pose includes filtered linear and angular velocities so OpenVR can predict motion between updates. These positions are inferred from a kinematic model and do not contain real hand translation. Physical reach, hand separation and depth movement cannot be recovered from a 3DoF sensor.

For calibration, keep the HMD facing the direction that should become forward and point the controller in that same direction. Hold App or Home for about 0.8 seconds. A short App tap continues to invoke the menu; a short Home tap invokes SteamVR System. For diagnosis, Android Logcat uses the `PhoneVRDaydream` tag; SteamVR driver messages appear in `vrserver.txt`. The expected order is button press received, long-press recognized, then software recenter applied. If Android logs the button but SteamVR does not, inspect the Host control connection and controller bridge. If the driver recognizes the hold but does not apply it, check whether HMD tracking becomes valid.

References:

- [Google VR GvrArmModel reference](https://developers.google.com/vr/reference/unity/class/GvrArmModel)
- [Google VR Daydream arm model overview](https://developers.google.com/vr/elements/arm-model)
- [OpenVR Driver API pose coordinate system](https://github.com/ValveSoftware/openvr/blob/master/docs/Driver_API_Documentation.md)
- [Daydream controller BLE packet protocol reverse engineering](https://hardfault.life/p/daydream-controller)
