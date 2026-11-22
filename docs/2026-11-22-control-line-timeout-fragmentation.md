# Android control JSON line loss on polling timeouts

## Observed failure

During PhoneVR 0.5.8 AOA streaming, the Android app reported:

`Value e" of type java.lang.String cannot be converted to JSONObject`

The Host subsequently observed the accessory disconnect and a WinUSB Bulk OUT semaphore timeout. The phone's outer USB connection loop formats its retry status from the thrown exception in `NetworkClient.usbConnectionLoop`; the established control loop parses each complete line with `JSONObject(line)`.

## Root cause in the 0.5.8 implementation

`UsbAccessorySession.controlInput` polls its bounded control-chunk queue for 500 ms and throws `SocketTimeoutException` when no next chunk is available. `NetworkClient` wrapped that stream in `BufferedReader` and called `readLine()`, catching and ignoring the timeout so the connection could continue polling.

`BufferedReader.readLine()` keeps the partially accumulated line in a local buffer for that invocation. If the underlying reader times out after some characters but before the newline, the invocation throws and its partial line is lost. The next call starts reading from the remaining bytes. Once this is treated as a complete line, JSON parsing can receive a truncated/scalar value instead of the intended control object. The uncaught JSON exception exits the USB connection loop, closes `UsbAccessorySession`, and stops Android from draining Host-to-phone Bulk OUT data.

## 0.5.9 change

- Added `ControlLineReader`, which keeps the partial line in persistent object state across `SocketTimeoutException` and supports LF, CRLF and EOF.
- Used the new reader for USB and LAN welcome/control messages.
- Serviced heartbeat sends and heartbeat deadlines from the reader's timeout callback, preserving the existing liveness behavior while waiting for complete lines.
- Limited control lines to 64 KiB.
- Added malformed JSON diagnostics with message source, length and an escaped preview.
- Added regression tests that inject polling timeouts between JSON fragments and verify that the resulting line is intact.

## Remaining validation

The source-level failure mode is covered by the new line-reader regression test. Android Gradle packaging and sustained AOA transfer still need to be verified on the build machine and target Redmi K40/Windows PC.
