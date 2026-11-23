# PhoneVR USB AOA Bulk transport

PhoneVR 0.5.29 provides a direct USB transport built on Android Open Accessory (AOA). The Windows Host switches the connected Android device into accessory mode, then exchanges a framed byte stream through the AOA interface's Bulk IN and Bulk OUT endpoints.

The USB mode does not use USB tethering, RNDIS, an IP address, ADB commands, ADB bulk data endpoints, or an ADB server. During initial AOA negotiation, Windows may expose the endpoint-zero control handle through a function labelled `Android ADB Interface`; PhoneVR uses that handle only for AOA control requests. After switching, the Host opens AOA accessory interface 0. For product ID `18D1:2D01`, which exposes an additional ADB data interface, PhoneVR leaves that second interface unopened. `18D1:2D00` is the accessory-only product ID.

## Windows driver prerequisite

The Windows Host implementation uses Microsoft's WinUSB user-mode API. Windows must have a WinUSB function driver bound to the Android USB function so the Host can issue the AOA vendor requests on endpoint zero. After the phone re-enumerates in AOA mode, its accessory interface must also be accessible through WinUSB.

**Important for product ID `18D1:2D01`: this is a composite accessory + ADB device with two interfaces. Interface 0 (`MI_00`) is the AOA accessory data interface used by PhoneVR. Interface 1 (`MI_01`) is the ADB debugging interface and must not be used for PhoneVR Bulk data.** The original `18D1:4E11` ADB WinUSB binding can make the AOA control handshake succeed, yet it does not automatically mean that the re-enumerated accessory data interface is bound to WinUSB. If the Host reports `WinUSB is bound to the ADB interface`, use Device Manager's device tree or Zadig's interface selection to bind WinUSB to `Android Accessory Interface (MI_00)` for `18D1:2D01`. Leave `Android ADB Interface (MI_01)` unchanged. For `18D1:2D00`, bind WinUSB to its single accessory interface. The Host validates the interface descriptor and refuses to use interface 1 for application data.

This driver binding is separate from ADB. It is not an ADB installation and PhoneVR does not communicate using the ADB protocol. Windows does not provide a universal WinUSB binding for every Android vendor/product combination, so the first connection may require a one-time driver binding with a WinUSB-capable driver installer such as [Zadig](https://zadig.akeo.ie/). Use the official Zadig distribution and select the exact Android USB interface, not an unrelated USB device. After AOA re-enumeration, check the new `Google Inc.` / `18D1:2D00` or `18D1:2D01` device and bind its accessory interface to WinUSB as well if Windows has not associated it automatically.

The Host reads both `DeviceInterfaceGUIDs` (REG_MULTI_SZ) and `DeviceInterfaceGUID` (REG_SZ) from each USB device instance whose Windows function service is `WinUSB`, then enumerates the currently present interfaces registered under those GUIDs. GUIDs are discovered dynamically from the local Windows installation. The Host does not hard-code the `F72FE0D4-...` GUID observed on one PC, any other fixed interface GUID, or an Android OEM VID allow-list. It probes the AOA `GET_PROTOCOL` request on each present WinUSB device function and proceeds only when the response reports AOA protocol v1 or v2. AOA v1/v2 detection therefore works across OEMs and per-PC GUID differences, provided Windows exposes the relevant device function through WinUSB.

The AOA Bulk OUT endpoint uses a 1500 ms WinUSB `PIPE_TRANSFER_TIMEOUT`. This prevents a stalled phone-side receive endpoint from holding the shared control, pose and video writer indefinitely. A failed or partial `PVRU` frame write closes that accessory session and lets the scanner reconnect; continuing on a partially written byte stream would lose frame alignment. Bulk IN remains available while idle and does not use this transfer timeout.

AOA mode switching uses vendor-specific control requests on the USB device's default control endpoint (endpoint zero). A phone may label its current function `Android ADB Interface` and report interface class `FF/42/01`. On some Windows/phone combinations, this is the only function node exposed with a WinUSB handle for the initial device-level control transfer. PhoneVR uses that handle only to issue AOA requests (`GET_PROTOCOL`, `SEND_STRING`, `START_ACCESSORY`) through endpoint zero. It does not open or exchange data on ADB bulk endpoints, start an ADB server, or use USB debugging. Binding WinUSB to that function can make ADB tools unavailable while that driver binding is active; restore the original driver if ADB access is needed again. After AOA re-enumeration, data moves through the AOA accessory Bulk IN/OUT endpoints.

Caution: changing a device interface's function driver can make that interface unavailable to MTP, ADB tools or vendor-specific phone-management tools until its original driver is restored. Avoid replacing every interface on a composite phone. Bind WinUSB only to the USB function needed to expose the AOA device-level control endpoint, and separately to the AOA accessory interface after re-enumeration if Windows does not bind it automatically.

If Device Manager shows the accessory interface without a WinUSB driver, the Host will keep scanning and the phone will remain in accessory mode; install/bind WinUSB for that accessory interface and the Host should detect it. If the initial Android function cannot be opened through WinUSB, the Host cannot send the AOA mode-switch requests. The Host runtime log reports enumerated WinUSB interface GUIDs, device paths, descriptor/open failures and the exact AOA `GET_PROTOCOL` or `START_ACCESSORY` error. For a device that remains in its original VID/PID, inspect those messages before changing drivers.

## Connection procedure

1. Start PhoneVR Host on Windows.
2. Connect the phone to the PC with a USB data cable. Keep the phone unlocked for the initial Windows driver prompt if required.
3. Bind WinUSB to the individual Android USB function that Windows exposes for the initial endpoint-zero AOA control requests. Depending on the phone and driver package, its displayed name may be `Android ADB Interface`; PhoneVR will use that function only for AOA control requests and will not exchange ADB data. This driver replacement may make ADB debugging unavailable until the original driver is restored. Avoid changing the composite parent or unrelated MTP/USB functions.
4. Open PhoneVR on Android and choose **USB AOA 直连**. The Android app waits for the Host to switch the device into accessory mode.
5. The Host sends AOA `GET_PROTOCOL`, six `SEND_STRING` descriptors and `START_ACCESSORY`. Android disconnects and re-enumerates under Google's AOA VID/PID.
6. If Windows has not already bound WinUSB to the new AOA accessory interface, bind it once. For `18D1:2D01`, select `MI_00` (Android Accessory Interface), not `MI_01` (Android ADB Interface). The Host then opens interface 0 and the Android app receives the accessory permission prompt. Grant it.
7. The PhoneVR status should report that USB AOA Bulk is connected. Control, HMD pose and H.264 video then use the same full-duplex USB stream.

The accessory filter descriptors are `VSQ Studio` / `PhoneVR` / `0.5.29`. The accessory mode attach intent can launch the Android app when it is not already running.

## Transport framing

AOA Bulk is a byte stream. PhoneVR adds a 16-byte little-endian frame header and does not depend on USB packet boundaries:

| Offset | Size | Field |
|---:|---:|---|
| 0 | 4 | ASCII `PVRU` magic |
| 4 | 1 | Multiplex protocol version, currently `1` |
| 5 | 1 | Channel: `1` control, `2` pose, `3` video |
| 6 | 2 | Reserved flags, zero |
| 8 | 4 | Payload length, little-endian, maximum 1 MiB |
| 12 | 4 | Per-direction frame sequence, little-endian |
| 16 | variable | Payload |

Control payloads retain the existing newline-delimited JSON protocol. Pose payloads retain the existing `PVSP` packet format and are forwarded into the Host's existing local pose pipeline. Video payloads retain the existing `PVRV` fragment format. In aggregation mode, network UDP and AOA Bulk carry different PVRV fragments from the same access unit. Android forwards USB fragments to the same loopback UDP receiver used by network video, where both paths share the frame reassembler and MediaCodec decoder. Missing fragment indexes can be requested over the control channel and retransmitted over the alternate route.

The physical USB channel is full duplex. Host and Android serialize writes independently in each direction; frame headers provide message boundaries when individual Bulk transfers are split or coalesced by the USB stack.

## Compatibility and limitations

- The Windows Host currently implements the WinUSB AOA scanner. Other Host platforms report USB AOA as unsupported.
- Windows must have a WinUSB function driver and registered device-interface GUID for the initial phone function and the re-enumerated AOA accessory function. Driver installation remains device/Windows specific even though the scanner itself uses dynamic GUID discovery.
- The phone and its USB controller must support Android Open Accessory mode and Bulk endpoints. AOA support is hardware/firmware dependent and is not available on every Android device. A USB-C cable must support data, not charging only.
- AOA switches the phone to accessory mode and may temporarily make MTP unavailable. Unplugging/reconnecting returns the phone to its normal USB mode.
- Android accessory mode and MTP cannot be active on the same USB connection at the same time.
- This revision has compile- and protocol-level validation only. Physical AOA enumeration, driver binding, sustained Bulk throughput, thermal behavior and end-to-end headset latency still require testing on the target Windows PC and Android phone.

## References

- Android Open Accessory protocol: <https://source.android.com/docs/core/interaction/accessories/protocol>
- Android USB accessory host overview: <https://source.android.com/docs/core/interaction/accessories>
- Microsoft WinUSB overview: <https://learn.microsoft.com/windows-hardware/drivers/usbcon/winusb>
- Microsoft `WinUsb_ControlTransfer` ABI and setup-packet parameter: <https://learn.microsoft.com/windows/win32/api/winusb/nf-winusb-winusb_controltransfer>
- Zadig official distribution and WinUSB binding utility: <https://zadig.akeo.ie/>
- Microsoft WinUSB functions: <https://learn.microsoft.com/windows/win32/api/_usb>
