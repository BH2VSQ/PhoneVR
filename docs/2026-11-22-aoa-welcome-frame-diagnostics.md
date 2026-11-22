# AOA Welcome framing and transport diagnostics

## What the 0.5.9 source showed

The Windows Host and Android client agree on the 16-byte `PVRU` multiplex header, protocol version 1, and Control channel 1. The Host bridges Control payload bytes through a `net.Pipe`; Android unwraps the PVRU frame and feeds its payload to the control line reader. The Welcome response is serialized as newline-terminated JSON by Go's JSON encoder.

The 0.5.9 `ControlLineReader` retained partial text across `SocketTimeoutException`, addressing the earlier `BufferedReader.readLine()` timeout-loss case. However, when its source returned EOF while bytes were still pending, it returned those bytes as a final line. That behavior could turn an interrupted USB Welcome into a short string which then failed JSON parsing. The reported 24-character failure is consistent with this path, although the Windows log alone does not prove why Android's AOA receive stream ended.

The Host-side `io: read/write on closed pipe` after the Welcome attempt is consistent with the Android-side control pipe/session being closed. It does not, by itself, identify whether the first failure was a USB disconnect, a malformed PVRU frame, an Android receive-thread exception, or an early close elsewhere.

## 0.5.10 changes

- Read control messages directly as bytes from `InputStream`. Preserve pending bytes over the USB 500 ms polling timeout and decode UTF-8 only after a complete LF/CR-delimited line arrives.
- Reject EOF with pending bytes as an unterminated control line. This prevents incomplete Welcome data from entering `JSONObject`.
- Retain the Android AOA Bulk IN receive-thread error and surface it through `controlInput` after already-queued Control payloads are drained.
- Log the first six Android Control TX/RX frames with sequence, byte length, escaped text preview and hex bytes.
- Log Host Welcome JSON length, initial Control payloads in each direction, bridge write failures and the actual error that stopped Host-side Bulk IN.

## How to interpret the next run

1. Compare `USB AOA Welcome prepared: wireBytes=N` on Windows with the Android `AOA control RX frame` entries. The sum of Control RX payload bytes carrying the Welcome should equal `N`.
2. Check Android `AOA Bulk receive stopped`. If present before the Welcome is complete, its exception identifies the receive-side failure that caused the short line.
3. If Android receives all Welcome bytes, check `Malformed USB welcome JSON` and its escaped preview. A complete frame with invalid JSON would point to a serialization/byte-stream issue rather than a line timeout.
4. If Android receives all Welcome bytes and accepts the session, but Windows later reports `USB AOA control Bulk OUT failed` or `USB AOA Bulk IN stopped`, investigate the post-handshake transfer path separately.

The 0.5.10 changes improve framing safety and capture the missing evidence. Hardware AOA behavior remains unverified until matching 0.5.10 builds are tested on the phone and target Windows PC.
