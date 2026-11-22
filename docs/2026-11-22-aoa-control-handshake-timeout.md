# AOA control handshake timeout and write deadline

## Target-PC evidence

The 0.5.5 logs show the Host successfully opening the AOA accessory data interface on `18D1:2D01` (interface 0, Bulk IN `0x81`, Bulk OUT `0x01`, 512-byte maximum packet size). The initial accessory session closed about eight seconds after detection. A later session reached `Device paired`, but the Host then logged `Pair response failed: write pipe: i/o timeout`.

## Root cause

`handleControlConnection` originally set an eight-second read deadline and a five-second write deadline at the start of the handler, before waiting for Android's initial `hello` message. The initial AOA permission flow can delay the hello. The five-second absolute write deadline therefore expired while the Host was still waiting for the hello, and the subsequent `welcome` response failed immediately with a named-pipe write timeout. The eight-second read deadline also closed the first accessory session while the Android permission/attach flow was still in progress.

## Fix in 0.5.6

- Use a 30-second initial hello deadline for USB AOA sessions only. Keep the 8-second initial deadline for LAN sessions.
- Do not set a write deadline while waiting for hello.
- Apply the five-second write deadline immediately around each JSON response, then clear it.
- Apply the same deadline cleanup to regular control JSON writes.

## Validation

Unit tests verify the transport-specific handshake timeout and that a stale expired write deadline is refreshed before writing a response. Hardware validation still needs to confirm accessory permission, the `welcome` exchange and sustained bulk data transfer.
