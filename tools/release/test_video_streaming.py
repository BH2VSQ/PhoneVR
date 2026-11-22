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

    def test_virtual_display_clock_commits_only_after_vsync_wait(self):
        wait = DRIVER[DRIVER.index("void WaitForPresent() override") : DRIVER.index("bool GetTimeSinceLastVsync") ]
        self.assertLess(wait.index("WaitUntilMonotonic(schedule.targetNs)"), wait.index("m_vsyncOriginNs = schedule.targetNs"))
        self.assertIn("CreateWaitableTimerExW", DRIVER)
        self.assertIn("std::mutex m_waitMutex", DRIVER)
        self.assertIn("m_waitCallsWithoutPresent", DRIVER)
        self.assertIn("VSync[none=", DRIVER)
        schedule = DRIVER[DRIVER.index("VsyncSchedule ScheduleVirtualVsync") : DRIVER.index("void LogMutexAcquireFailure") ]
        self.assertIn("m_lastScheduledVsyncNs = target", schedule)
        self.assertNotIn("m_vsyncOriginNs = target", schedule)

    def test_encoder_backlog_is_bounded_for_low_latency(self):
        self.assertIn("h264PendingFrameLimit   = 4", ENCODER)
        self.assertIn('"-async_depth", "2"', ENCODER)

    def test_host_uses_loopback_capture_and_android_udp(self):
        self.assertRegex(HOST_SERVICE, r"steamVRVideoCapturePort\s*=\s*48106")
        self.assertRegex(HOST_SERVICE, r"androidVideoPort\s*=\s*48104")
        self.assertIn("videoCaptureLoop", HOST_SERVICE)
        self.assertIn("sendEncodedVideoFrame", HOST_SERVICE)
        self.assertIn("StartH264EncoderWithCapability", HOST_SERVICE)
        self.assertIn("videoEncoderLoop", HOST_SERVICE)
        self.assertIn("enqueueRawVideoFrame", HOST_SERVICE)
        self.assertIn("videoInputQueueDepth", HOST_SERVICE)
        self.assertIn("enqueueEncodedVideoFrame", HOST_SERVICE)
        self.assertIn("videoTransportSendLoop", HOST_SERVICE)
        self.assertIn("videoOutputQueueDepth", HOST_SERVICE)
        self.assertIn("videoOutputNeedsKeyFrame", HOST_SERVICE)
        self.assertIn("output-queue drops", HOST_SERVICE)
        self.assertIn("persistent H.264 encoder", HOST_SERVICE)
        capture_handler = HOST_SERVICE[HOST_SERVICE.index("func (s *HostService) handleVideoCaptureConnection") : HOST_SERVICE.index("// videoEncoderLoop owns exactly one FFmpeg process")]
        self.assertNotIn("StartHardwareH264Encoder", capture_handler)
        self.assertNotIn("StartH264EncoderWithCapability", capture_handler)
        self.assertIn("CompareAndSwap(false, true)", HOST_SERVICE)

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
        self.assertIn("DWORD timeout = 5000", worker)
        self.assertIn("raw video TCP send failed WSA error", worker)
        self.assertIn("D3D11_MAP_FLAG_DO_NOT_WAIT", worker)
        self.assertIn("DXGI_ERROR_WAS_STILL_DRAWING", worker)
        self.assertIn("m_videoReadbackPending", worker)
        self.assertIn("gpu-map-pending", DRIVER)


    def test_device_action_ack_is_written_from_background_control_executor(self):
        action_start = ANDROID_NETWORK.index('"device_action" -> {')
        action_end = ANDROID_NETWORK.index('\n                }\n            } catch', action_start)
        action_block = ANDROID_NETWORK[action_start:action_end]
        self.assertIn("mainHandler.post", action_block)
        self.assertIn("queueControlMessage", action_block)
        self.assertNotIn("writeControlMessage", action_block)
        self.assertIn("Phone device action failed", ANDROID_NETWORK)

    def test_android_legacy_codec_probe_and_video_stage_status(self):
        self.assertIn("isLikelySoftwareCodec", ANDROID_CAPS)
        self.assertIn("hardwareClassificationReliable", ANDROID_CAPS)
        for stage in ("$transportLabel 视频接收已启动", "已收到完整 H.264 帧", "H.264 解码器已启动"):
            self.assertIn(stage, ANDROID_RECEIVER)
        self.assertIn("reportVideoFrameRendered", ANDROID_NETWORK)
        self.assertIn("videoFrameListener", ANDROID_RENDERER)

    def test_live_vr_never_falls_back_to_cpu_software_encoder(self):
        self.assertIn("ProbeHardwareH264Encoder", ENCODER)
        self.assertIn("CPU software encoding is disabled", ENCODER)
        self.assertIn("StartHardwareH264Encoder", ENCODER)
        self.assertIn("StartH264EncoderWithCapability", ENCODER)
        self.assertIn("h264PendingFrameLimit", ENCODER)
        self.assertIn("ErrH264Backpressure", ENCODER)
        self.assertIn("-async_depth", ENCODER)
        self.assertIn("H264OutputStallTimeout", ENCODER)
        self.assertIn("format=bgra", ENCODER)
        self.assertIn("configureFFmpegProcess", ENCODER)
        self.assertIn("encoderOutputPixelFormat", ENCODER)
        self.assertIn("-flush_packets", ENCODER)
        self.assertIn("no encoder restart", HOST_SERVICE)
        self.assertIn("OutputStalled", HOST_SERVICE)
        self.assertIn("FFmpeg stdout", HOST_SERVICE)
        self.assertIn("describeRawBGRAFrame", HOST_SERVICE)

    def test_all_host_children_do_not_create_console_windows_on_windows(self):
        helper = (ROOT / "windows-host" / "internal" / "childprocess" / "process_windows.go").read_text(encoding="utf-8")
        driver_manager = (ROOT / "windows-host" / "cmd" / "phonevr-host" / "driver_manager_windows.go").read_text(encoding="utf-8")
        encoder_helper = (ROOT / "windows-host" / "internal" / "encoder" / "process_windows.go").read_text(encoding="utf-8")
        self.assertIn("HideWindow = true", helper)
        self.assertIn("CREATE_NO_WINDOW", helper)
        self.assertEqual(driver_manager.count("exec.Command("), 3)
        self.assertEqual(driver_manager.count("childprocess.HideConsoleWindow(cmd)"), 3)
        self.assertIn("childprocess.HideConsoleWindow(cmd)", encoder_helper)


    def test_android_decoder_backpressure_cannot_busy_spin_at_display_priority(self):
        decode_loop = ANDROID_RECEIVER.split("private fun decodeLoop()", 1)[1].split("private fun createDecoder()", 1)[0]
        decode_session = ANDROID_RECEIVER.split("private fun decodeSession(codec: MediaCodec)", 1)[1].split("private fun setThreadPriority", 1)[0]
        self.assertIn("Process.THREAD_PRIORITY_DEFAULT", decode_loop)
        self.assertIn("sleepQuietly(1)", decode_session)
        self.assertIn("decoderFlushRequested.getAndSet(false)", decode_session)
        self.assertIn("UDP_RESUME_GAP_NS", ANDROID_RECEIVER)
        self.assertIn("Video UDP resumed after", ANDROID_RECEIVER)

    def test_android_video_health_exposes_reassembly_decoder_and_display_stages(self):
        stats = (ROOT / "android/app/src/main/java/com/vsq/phonvrstream/streaming/VideoStreamStats.kt").read_text(encoding="utf-8")
        for field in ("reassemblyInFlight", "expiredAssemblies", "decoderRecoveries", "lastPacketAgeMs", "lastCompleteFrameAgeMs"):
            self.assertIn(field, stats)
            self.assertIn(field, ANDROID_RECEIVER)
        self.assertIn("takeExpiredAssemblyCount", ANDROID_RECEIVER)
        self.assertIn("inFlightCount", ANDROID_RECEIVER)
        self.assertIn("Video UDP resumed", ANDROID_RECEIVER)

    def test_android_main_menu_hides_pc_and_pose_details_and_adds_compact_stream_hud(self):
        activity = (ROOT / "android/app/src/main/java/com/vsq/phonvrstream/MainActivity.kt").read_text(encoding="utf-8")
        top_bar = activity.split("private fun buildTopBar()", 1)[1].split("private fun buildMenuPanel()", 1)[0]
        menu = activity.split("private fun buildMenuPanel()", 1)[1].split("private fun screenFlipButtonText()", 1)[0]
        self.assertIn("streamStatusText", top_bar)
        self.assertIn("Gravity.END", top_bar)
        self.assertNotIn("PC 已连接", top_bar)
        self.assertNotIn("poseText", top_bar)
        self.assertIn("menuConnectionText", menu)
        self.assertIn("poseText", menu)
        self.assertIn("menuVideoDetailText", menu)

    def test_android_screen_flip_persists_reverse_landscape_orientation(self):
        activity = (ROOT / "android/app/src/main/java/com/vsq/phonvrstream/MainActivity.kt").read_text(encoding="utf-8")
        store = (ROOT / "android/app/src/main/java/com/vsq/phonvrstream/settings/SettingsStore.kt").read_text(encoding="utf-8")
        self.assertIn("SCREEN_ORIENTATION_REVERSE_LANDSCAPE", activity)
        self.assertIn("saveScreenFlipped(screenFlipped)", activity)
        self.assertIn("loadScreenFlipped()", activity)
        self.assertIn('"screen_flipped"', store)
        manifest = (ROOT / "android/app/src/main/AndroidManifest.xml").read_text(encoding="utf-8")
        self.assertIn('android:configChanges="density|keyboardHidden|navigation|orientation|screenSize|uiMode"', manifest)

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
