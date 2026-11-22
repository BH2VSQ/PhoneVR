# Android AOA video receive-side backpressure

## 0.5.7 target-PC evidence

The Host successfully opens AOA interface 0 on `18D1:2D01`, negotiates `Device paired`, starts the H.264 QSV session and emits an IDR access unit. WinUSB reports `OUT-timeout=1500ms`. Nevertheless, only two video transport fragments are counted before the Host reports repeated `USB AOA Bulk OUT: The semaphore timeout period has expired` errors. The first send takes 2523.6 ms and later sends take about 1500 ms. The USB control connection then closes and the Host repeatedly rediscovers the accessory.

These logs confirm that the configured WinUSB timeout is active. They do not, by themselves, prove whether the Android accessory read loop, USB controller, driver stack or local forwarding path stopped consuming data.

## Source-level risk found in 0.5.7

`UsbAccessorySession.readLoop()` decoded each PVRU frame and called `videoPacketListener` inline for `CHANNEL_VIDEO`. `NetworkClient` assigned that listener to `forwardUsbVideoPacket()`, which synchronously called `DatagramSocket.send()` to move the PVRV packet to the local `VideoStreamReceiver` UDP socket. Consequently, the USB Bulk IN reader was coupled to the local loopback forwarding call. Any scheduling delay, socket backpressure or callback stall could stop the accessory reader from draining incoming USB data and propagate backpressure to the PC Bulk OUT endpoint.

This is a concrete source-level blocking risk. It is not proof that the loopback send was the only cause of the observed hardware timeout.

## 0.5.8 change

- Added `UsbVideoPacketDispatcher`, a dedicated worker with a bounded 256-packet queue.
- `UsbAccessorySession.readLoop()` now performs only a non-blocking queue offer for video packets and continues reading subsequent PVRU frames.
- Queue overflow is counted rather than blocking the USB input reader or allowing unbounded memory growth.
- Callback exceptions are isolated so one failed local forwarding operation does not terminate the dispatcher.
- Android periodically logs accepted/forwarded/dropped packet counts, queue depth, callback failures and maximum callback duration.
- Added JVM tests for non-blocking handoff, bounded queue behavior and callback-failure isolation.

## Validation still required

The Android Gradle test task could not run in this environment because Gradle 9.6.1 was not cached and `services.gradle.org` could not be resolved. The dispatcher source compiles with the installed standalone Kotlin compiler. Sustained USB throughput must be re-tested with 0.5.8 on the actual phone and Windows PC.

On the next run, compare Host `transport fragments` with Android `AOA video RX dispatcher` counters:

- Host fragments flat and Android accepted flat: investigate WinUSB/AOA Bulk IN delivery.
- Android accepted rises but forwarded stalls and queue depth grows: investigate the local UDP forwarding call.
- Android accepted and forwarded both rise, but no complete video frames appear: inspect PVRV fragmentation/reassembly and MediaCodec input.
