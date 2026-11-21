# PhoneVR 0.4.18 Build Status

## Integrated SBS H.264 streaming and fixed virtual display timing

- Removed the Host display-refresh UI, persistence file and driver-side dynamic refresh loader. The virtual display remains fixed at 72 Hz while the end-to-end frame pipeline is brought up and profiled.
- SteamVR `IVRVirtualDisplay` captures the final SBS backbuffer only after a compatible Android client has negotiated video. D3D11 GPU-scales the frame to 1600×720 and uses a bounded three-slot staging ring. A background worker performs CPU readback and loopback TCP delivery to Host port 48106.
- Host validates a real hardware H.264 encoder session (NVENC/AMF/QSV) with an actual IDR output test. CPU software encoding is intentionally disabled for live VR video to avoid adding load to a CPU-bound SteamVR compositor.
- Host reuses a single BGRA receive buffer to avoid allocating 4.6 MiB per frame at 60 FPS. The encoder timestamp backlog is bounded to eight frames and H.264 output packets are flushed immediately to avoid accumulating seconds of stale video. Encoded H.264 Annex-B access units are sent as 1200-byte payload fragments using the `PVRV` v1 UDP packet format to Android port 48104. A transient UDP send failure drops the access unit without terminating the encoder; Android requests recovery by waiting for the next IDR after frame loss.
- Android advertises the exact hardware decoder profile only when a reliably classified hardware MediaCodec decoder supports 1600×720@60. Host selects and sends that exact codec name in the welcome message; Android creates it by name rather than relying on the system's default MIME-type decoder selection. `VideoStreamReceiver` reassembles complete access units into a two-frame bounded queue, decodes on a dedicated thread and releases output buffers to the renderer's SurfaceTexture. A dropped H.264 input access unit triggers IDR resynchronization. `StereoRenderer` samples the decoded SBS stream and then applies the existing optical warp independently per eye.
- Pose UDP and control TCP remain independent. Video capture, encoding and decoding use their own workers and bounded queues.

## Network ports

- TCP 48101: Host control/pairing.
- UDP 48102: Android-to-Host IMU pose.
- TCP 127.0.0.1:48106: SteamVR driver-to-Host raw BGRA capture bridge (loopback only).
- UDP 48104: Host-to-Android H.264 video fragments.

## Validation and known limitations

- Go unit/integration tests, Race, Vet, Windows AMD64 Host cross-compilation, Python release tests, raw protocol tests and the local FFmpeg H.264 encoder integration test are the available source-level checks for this revision.
- Local FFmpeg integration confirms that the encoder session emits Annex-B access units and an IDR with the locally available encoder; Host live streaming explicitly requires a verified hardware backend. Android's pure Kotlin fragment reassembler compiles independently with kotlinc.
- Windows MSVC SteamVR DLL compilation and D3D11 runtime capture, GPU hardware encoder operation on the target PC, Android Gradle/MediaCodec execution, Wi-Fi packet loss behavior and full end-to-end phone display remain to be verified on target devices.
- Software H.264 fallback is deliberately disabled for the live stream because the reported PC CPU utilization already reaches 100% during SteamVR movement. If NVENC/AMF/QSV cannot initialize, Host will disable video and log the exact reason while pose/control remain operational.
