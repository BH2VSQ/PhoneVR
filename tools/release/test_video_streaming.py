import unittest
from pathlib import Path

ROOT = Path(__file__).resolve().parents[2]
DRIVER = (ROOT / "steamvr-driver/src/driver.cpp").read_text(encoding="utf-8")
CMAKE = (ROOT / "steamvr-driver/CMakeLists.txt").read_text(encoding="utf-8")
HOST_SERVICE = (ROOT / "windows-host/cmd/phonevr-host/service.go").read_text(encoding="utf-8")
ENCODER = (ROOT / "windows-host/internal/encoder/h264_stream.go").read_text(encoding="utf-8")
ANDROID_NETWORK = (ROOT / "android/app/src/main/java/com/vsq/phonvrstream/network/NetworkClient.kt").read_text(encoding="utf-8")
ANDROID_RECEIVER = (ROOT / "android/app/src/main/java/com/vsq/phonvrstream/streaming/VideoStreamReceiver.kt").read_text(encoding="utf-8")
ANDROID_RENDERER = (ROOT / "android/app/src/main/java/com/vsq/phonvrstream/display/StereoRenderer.kt").read_text(encoding="utf-8")
ANDROID_CAPS = (ROOT / "android/app/src/main/java/com/vsq/phonvrstream/streaming/CodecCapabilityProbe.kt").read_text(encoding="utf-8")


class VideoStreamingIntegrationTests(unittest.TestCase):
    def test_driver_gpu_captures_sbs_to_bounded_staging_ring(self):
        self.assertIn("EnsureVideoCaptureResources", DRIVER)
        self.assertIn("CaptureVideoFrameUnlocked", DRIVER)
        self.assertIn("kVideoCaptureSlots = 3", DRIVER)
        self.assertIn("D3DCompile(kCapturePixelShader", DRIVER)
        self.assertIn("kVideoCapturePort = 48106", DRIVER)
        self.assertIn("d3dcompiler", CMAKE)

    def test_host_uses_loopback_capture_and_android_udp(self):
        self.assertIn("steamVRVideoCapturePort = 48106", HOST_SERVICE)
        self.assertRegex(HOST_SERVICE, r"androidVideoPort\s*=\s*48104")
        self.assertIn("videoCaptureLoop", HOST_SERVICE)
        self.assertIn("sendEncodedVideoFrame", HOST_SERVICE)
        self.assertIn("StartHardwareH264Encoder", HOST_SERVICE)

    def test_video_pipeline_reports_each_host_stage(self):
        for field in ("videoBridgeActive", "videoRawFrames", "videoEncodedFrames", "videoUDPFragments", "videoDroppedFrames"):
            self.assertIn(field, HOST_SERVICE)
        self.assertIn("Video pipeline:", HOST_SERVICE)
        self.assertIn("VIDEO  %s", (ROOT / "windows-host/cmd/phonevr-host/native_theme_windows.go").read_text(encoding="utf-8"))

    def test_driver_releases_d3d_capture_lock_before_network_send(self):
        worker = DRIVER[DRIVER.index("void VideoCaptureLoop()") : DRIVER.index("void ReleaseVideoCaptureResourcesUnlocked()")]
        self.assertIn("std::lock_guard<std::mutex> resourceLock(m_captureResourcesMutex)", worker)
        self.assertIn("SendAll(bridge", worker)
        self.assertLess(worker.index("slot.busy.store(false"), worker.index("SendAll(bridge"))
        self.assertIn("m_videoReadbackFailures", worker)

    def test_android_legacy_codec_probe_and_video_stage_status(self):
        self.assertIn("isLikelySoftwareCodec", ANDROID_CAPS)
        self.assertIn("hardwareClassificationReliable", ANDROID_CAPS)
        for stage in ("UDP 视频接收已启动", "已收到完整 H.264 帧", "H.264 解码器已启动"):
            self.assertIn(stage, ANDROID_RECEIVER)
        self.assertIn("reportVideoFrameRendered", ANDROID_NETWORK)
        self.assertIn("videoFrameListener", ANDROID_RENDERER)

    def test_live_vr_never_falls_back_to_cpu_software_encoder(self):
        self.assertIn("ProbeHardwareH264Encoder", ENCODER)
        self.assertIn("CPU software encoding is disabled", ENCODER)
        self.assertIn("StartHardwareH264Encoder", ENCODER)
        self.assertIn("h264PendingFrameLimit", ENCODER)
        self.assertIn("-flush_packets", ENCODER)

    def test_display_redirect_overrides_hmd_virtual_output_properties(self):
        self.assertIn("vr::k_ulDisplayRedirectContainer", DRIVER)
        self.assertIn("Prop_HasVirtualDisplayComponent_Bool, true", DRIVER)
        self.assertIn("Prop_GraphicsAdapterLuid_Uint64, adapterLuid", DRIVER)
        self.assertIn("HMD effective display properties", DRIVER)

    def test_display_redirect_is_prepared_and_registered_before_hmd(self):
        provider = DRIVER[DRIVER.index("class PhoneVRProvider") : DRIVER.index("PhoneVRProvider g_provider")]
        self.assertIn("m_redirect->Prepare()", provider)
        self.assertIn('PublishHmdOverrides("after-registration")', provider)
        self.assertLess(
            provider.index('TrackedDeviceAdded("PHONEVR-DISPLAY-001"'),
            provider.index('PublishHmdOverrides("after-registration")'),
        )
        self.assertLess(
            provider.index('PublishHmdOverrides("after-registration")'),
            provider.index('TrackedDeviceAdded("PHONEVR-HMD-001"'),
        )

    def test_encoder_preflight_is_synchronous_before_host_listener_start(self):
        probe = HOST_SERVICE[HOST_SERVICE.index("func (s *HostService) ProbeVideoEncoders") : HOST_SERVICE.index("func (s *HostService) Stop()")]
        main = (ROOT / "windows-host/cmd/phonevr-host/main.go").read_text(encoding="utf-8")
        self.assertNotIn("go func()", probe)
        self.assertIn("defer close(done)", probe)
        self.assertLess(main.index("service.ProbeVideoEncoders"), main.index("service.Start(config)"))

    def test_android_hardware_codec_reassembles_decodes_and_renders_sbs(self):
        self.assertIn("supportsSbs1600x720At60", ANDROID_CAPS)
        self.assertIn("hardwareClassificationReliable", ANDROID_CAPS)
        self.assertIn("MediaCodec.createByCodecName", ANDROID_RECEIVER)
        self.assertIn("videoDecoderName", ANDROID_NETWORK)
        self.assertIn("HardwareClassificationReliable", HOST_SERVICE)
        self.assertIn("VideoFragmentReassembler", ANDROID_RECEIVER)
        self.assertIn("video/avc", ANDROID_NETWORK)
        self.assertIn("samplerExternalOES", ANDROID_RENDERER)
        self.assertIn("VIDEO_FRAGMENT_SHADER", ANDROID_RENDERER)


if __name__ == "__main__":
    unittest.main()
