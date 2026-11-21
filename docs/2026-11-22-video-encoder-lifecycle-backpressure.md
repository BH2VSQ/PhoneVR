# PhoneVR video encoder lifecycle and backpressure fix (2026-11-22)

## Observed failure

The Windows Host log showed the driver capture bridge repeatedly changing between `connected` and `waiting`, while the SteamVR driver reported successful GPU readback and intermittent TCP frame sends. The user also observed many short-lived FFmpeg command-line processes and system-wide stalls.

## Root cause in the previous implementation

1. `handleVideoCaptureConnection` owned its `H264EncoderSession`. Any loopback TCP disconnect returned from the handler and closed FFmpeg.
2. The next TCP connection started another encoder through `StartHardwareH264Encoder`. That path re-ran `ffmpeg -encoders` and hardware test processes before launching the streaming encoder.
3. The TCP reader called `session.WriteFrame` synchronously. While FFmpeg/QSV consumed a large 1600x720 BGRA frame, Host stopped draining the loopback TCP socket.
4. The SteamVR driver used a 250 ms `SO_SNDTIMEO` for a 4.6 MiB raw frame. Short encoder or scheduler stalls could therefore create a send failure, TCP reconnect, FFmpeg close, and another set of FFmpeg probe processes.

This formed a feedback loop that could rapidly create and destroy FFmpeg processes and overload the PC.

## Changes

- The Host now has a dedicated, service-lifetime `videoEncoderLoop` that owns one FFmpeg H.264 session independently of the capture TCP connection.
- Encoder selection from the synchronous startup preflight is saved. The streaming session uses `StartH264EncoderWithCapability`, avoiding a second FFmpeg inventory and hardware-test cycle on each bridge connection.
- The loopback TCP reader now only validates and receives complete raw frames, then places them into a two-frame latest-frame-wins queue.
- The bounded queue uses pooled BGRA buffers. When the encoder falls behind, old queued frames are dropped and replaced by newer frames instead of blocking TCP receive and accumulating stale latency.
- Only one active loopback video bridge is accepted at a time.
- The Host TCP read buffer was increased to 16 MiB and the idle read timeout to 10 seconds.
- The driver send timeout was increased from 250 ms to 5 seconds, with an 8 MiB send buffer and rate-limited WSA error logging.
- The FFmpeg session remains alive through brief TCP bridge reconnects and is closed when the Host service stops. A failed encoder session has a five-second retry backoff to prevent rapid process restart loops.

## Expected diagnostics

The Host should report one `Video encoder session initialized` and one `Persistent live VR hardware H.264 encoder started` during a normal Host lifetime. Those messages should not repeat for every TCP bridge reconnect.

The periodic Host line now reports:

`Video pipeline: queued-to-FFmpeg ... | H.264 ... | UDP fragments ... | input queue drops ...`

A nonzero `input queue drops` counter means the encoder is temporarily slower than capture and stale frames are being discarded to preserve freshness. It should not cause the TCP bridge to reconnect or restart FFmpeg.

The driver now reports `raw video TCP send failed WSA error=...` when a TCP send actually fails. That error code can distinguish a timeout from a reset or other socket failure.

## Validation

- `go test ./...`: passed.
- `go test -race ./...`: passed.
- `go vet ./...`: passed.
- Windows/amd64 Go Host cross-build: passed.
- Python release regression suite: 41 tests passed.
- Native Windows MSVC SteamVR DLL build and real-device throughput still require testing on the user's Windows machine.
