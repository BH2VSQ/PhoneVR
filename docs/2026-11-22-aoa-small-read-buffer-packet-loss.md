# Android AOA small-read buffer packet loss

## Observed failure

The 2026-11-22 Host and Android logs show:

- Host sends a Welcome JSON payload of 521 bytes in a PVRU frame. The PVRU header is 16 bytes, so the wire frame is 537 bytes.
- Android's AOA receive path reports a 521-byte control payload, but the payload begins with the final 25 bytes of the Welcome JSON.
- Immediately after those 25 bytes, Android's receive buffer contains the next `PVRU` frame header and the beginning of the 70-byte `get_optical_presets` request.
- `UsbMuxProtocol.decodeHeader()` then reports `Invalid USB multiplex frame signature` because the prior parser consumed the wrong 521-byte payload boundary.

The missing 512 bytes correspond exactly to the first USB bulk packet of the 537-byte wire frame. This explains the 24-character Welcome JSON parsing error: only the JSON tail is delivered as the first fragment.

## Root cause

`UsbAccessorySession.readLoop()` previously read the PVRU header by calling `FileInputStream.read()` with a 16-byte destination buffer. Android's USB accessory documentation says the read buffer must be large enough to contain a USB packet. It notes that the AOA protocol supports packet buffers up to 16,384 bytes.

The AOA kernel gadget read implementation sizes its queued USB request from the userspace `read()` count. A 16-byte request is therefore too small for a 512-byte full-speed/high-speed bulk packet. The unused remainder of the packet can be lost before the next read. The previous code then read the expected 521-byte Payload length from the remaining USB stream, causing the next PVRU header to be consumed as Welcome JSON.

References:

- Android USB accessory overview: https://developer.android.com/develop/connectivity/usb/accessory
- Android kernel `f_accessory.c` `acc_read()`: https://android.googlesource.com/kernel/common/+/android-trusty-3.18/drivers/usb/gadget/function/f_accessory.c

## Fix in 0.5.12

- Read from the AOA file descriptor only with a 16 KiB buffer.
- Feed each raw read into `UsbMuxStreamDecoder`, which reconstructs PVRU frames from arbitrary read chunks.
- Retain partial headers and payloads until complete; return multiple frames when one bulk read contains more than one frame.
- Log raw read byte counts, request buffer size, PVRU channel/sequence, payload lengths, CRC32 values and invalid header bytes.
- On Windows, log the serialized wire frame CRC32 and each `WinUsb_WritePipe` requested/transferred byte count.

## Regression coverage

`UsbMuxStreamDecoderTest` covers:

- A 537-byte wire frame split at the 512-byte USB packet boundary.
- Two complete PVRU frames coalesced into one raw read.
- Partial header and payload data arriving over several raw reads.
- Invalid header diagnostics including the raw 16-byte header HEX.

The parser is intentionally a byte-stream parser. It does not treat a USB packet, an `InputStream.read()` call, or a single PVRU frame as equivalent boundaries.
