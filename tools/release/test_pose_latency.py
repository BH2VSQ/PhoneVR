import unittest
from pathlib import Path

ROOT = Path(__file__).resolve().parents[2]
DRIVER = (ROOT / "steamvr-driver/src/driver.cpp").read_text(encoding="utf-8")
NETWORK = (ROOT / "android/app/src/main/java/com/vsq/phonvrstream/network/NetworkClient.kt").read_text(encoding="utf-8")
ACTIVITY = (ROOT / "android/app/src/main/java/com/vsq/phonvrstream/MainActivity.kt").read_text(encoding="utf-8")
QUATERNION = (ROOT / "android/app/src/main/java/com/vsq/phonvrstream/tracking/Quaternion.kt").read_text(encoding="utf-8")
HOST_SERVICE = (ROOT / "windows-host/cmd/phonevr-host/service.go").read_text(encoding="utf-8")
HOST_UI = (ROOT / "windows-host/cmd/phonevr-host/native_ui_windows.go").read_text(encoding="utf-8")
HOST_THEME = (ROOT / "windows-host/cmd/phonevr-host/native_theme_windows.go").read_text(encoding="utf-8")
DRIVER_README = (ROOT / "steamvr-driver/README.md").read_text(encoding="utf-8")


class PoseLatencyRegressionTests(unittest.TestCase):
    def test_android_keeps_sensor_work_off_ui_and_aligns_quaternion_hemisphere(self):
        self.assertIn('HandlerThread("PhoneVR-IMU", Process.THREAD_PRIORITY_DISPLAY)', ACTIVITY)
        self.assertIn("Process.THREAD_PRIORITY_DISPLAY", NETWORK)
        self.assertIn(".alignedTo(lastRawPose)", ACTIVITY)
        self.assertIn("fun alignedTo(reference: Quaternion)", QUATERNION)
        self.assertIn("if (dot < 0f)", QUATERNION)

    def test_android_pose_sender_matches_requested_200hz_sensor_rate(self):
        self.assertIn("HEAD_TRACKING_SAMPLE_PERIOD_US = 5_000", ACTIVITY)
        self.assertIn("MAX_POSE_SEND_HZ = 200", NETWORK)
        self.assertIn("latestPose.get()", NETWORK)

    def test_host_logs_pose_rate_sequence_gaps_and_bridge_errors(self):
        self.assertIn("Pose pipeline: Host RX %.1f Hz", HOST_SERVICE)
        self.assertIn("statsSequenceGaps", HOST_SERVICE)
        self.assertIn("statsBridgeErrors", HOST_SERVICE)
        self.assertIn("readLatestPoseBatch(conn, buffer, time.Second)", HOST_SERVICE)
        self.assertIn("forwarded %.1f Hz, coalesced %d", HOST_SERVICE)
        self.assertIn("max Host read gap %.1f ms", HOST_SERVICE)
        self.assertIn("max Android sensor interval %.1f ms", HOST_SERVICE)
        self.assertIn("max batch %d", HOST_SERVICE)
        self.assertIn("max pose-state lock wait %.2f ms", HOST_SERVICE)
        self.assertIn("poseBatchToForward(batch)", HOST_SERVICE)
        self.assertIn("maxPoseBatchForward = 32", HOST_SERVICE)
        self.assertIn("SetReadBuffer(16 * 1024)", HOST_SERVICE)
        self.assertIn("time.Now().Add(250 * time.Microsecond)", HOST_SERVICE)

    def test_pose_publisher_is_independent_of_runframe_and_reports_both_cadences(self):
        hmd = DRIVER.split("class PhoneVRHmd", 1)[1].split("class VirtualDisplayComponent", 1)[0]
        run_frame = hmd.split("void RunFrame() {", 1)[1].split("\nprivate:", 1)[0]
        publisher = hmd.split("void PosePublisherLoop() {", 1)[1].split("\n    }\n\n    uint32_t m_objectId", 1)[0]
        self.assertIn("controlled by vrserver", run_frame)
        self.assertNotIn("TrackedDevicePoseUpdated", run_frame)
        self.assertIn("TrackedDevicePoseUpdated", publisher)
        self.assertIn("WaitForUpdate", publisher)
        self.assertIn("THREAD_PRIORITY_ABOVE_NORMAL", publisher)
        self.assertIn("m_uniquePoseCount", publisher)
        self.assertIn("m_repeatedPoseCount", publisher)
        self.assertIn("max-publish-gap", run_frame)
        self.assertIn("runtime RunFrame=%.1fHz", run_frame)

    def test_virtual_display_callback_timing_is_reported_separately(self):
        self.assertIn("ScopedCallbackTiming timing(m_presentCalls", DRIVER)
        self.assertIn("ScopedCallbackTiming timing(m_waitCalls", DRIVER)
        self.assertIn("TakeTimingSnapshot()", DRIVER)
        self.assertIn("max-Present=%.2fms", DRIVER)
        self.assertIn("max-WaitForPresent=%.2fms", DRIVER)
        self.assertIn("display-refresh=%.1fHz", DRIVER)

    def test_driver_drains_stale_udp_queue_and_predicts_with_angular_velocity(self):
        self.assertIn("ioctlsocket(socket, FIONREAD, &pendingBytes)", DRIVER)
        self.assertIn("stores\n            // only its newest orientation", DRIVER)
        self.assertIn("m_sequenceGapCount", DRIVER)
        self.assertIn("EstimateAngularVelocity", DRIVER)
        self.assertIn("EstimateAngularVelocity(velocityBase, candidate)", DRIVER)
        self.assertIn("next.sensorTimestampNs = ReadBE64(data + 12)", DRIVER)
        self.assertIn("current.sensorTimestampNs - previous.sensorTimestampNs", DRIVER)
        self.assertIn("pose.vecAngularVelocity[0]", DRIVER)
        self.assertIn("pose.vecAngularVelocity[1]", DRIVER)
        self.assertIn("pose.vecAngularVelocity[2]", DRIVER)
        self.assertIn("pose.poseTimeOffset = -static_cast<double>(predictionAgeNs)", DRIVER)
        self.assertIn("pose rates driver-rx=%.1fHz published=%.1fHz", DRIVER)
        self.assertIn("constexpr double alpha = 0.80", DRIVER)

    def test_snapshot_never_enumerates_network_adapters_under_pose_mutex(self):
        self.assertIn("networkAddresses := localIPv4NetworkAddresses()", HOST_SERVICE)
        self.assertIn("s.localIPv4 = append([]string(nil), localIPs...)", HOST_SERVICE)
        self.assertIn("LocalIPv4: append([]string(nil), s.localIPv4...)", HOST_SERVICE)
        snapshot_body = HOST_SERVICE.split("func (s *HostService) Snapshot() HostSnapshot {", 1)[1].split("// PoseFrame", 1)[0]
        self.assertNotIn("localIPv4Addresses()", snapshot_body)

    def test_native_log_view_uses_rich_edit_and_preserves_scroll_state(self):
        self.assertIn('logClass = "RICHEDIT50W"', HOST_UI)
        self.assertIn("Msftedit.dll", HOST_UI)
        self.assertIn("EM_EXLIMITTEXT", HOST_UI)
        self.assertIn("embedded RichEdit/EDIT window processes its own scrollbar", HOST_UI)
        self.assertIn("EM_GETFIRSTVISIBLELINE", HOST_UI)
        self.assertIn("EM_GETSEL", HOST_UI)
        self.assertIn("EM_LINESCROLL", HOST_UI)
        self.assertIn("WM_SETREDRAW(FALSE)", HOST_UI)
        self.assertIn("EM_SCROLLCARET", HOST_UI)
        self.assertIn("dialog.kind == \"logs\"", HOST_UI)
        self.assertIn("pSendMessage.Call(dialog.controls[501], uintptr(message), wParam, 0)", HOST_UI)

    def test_host_pose_preview_runs_at_sixty_hz_target(self):
        self.assertIn("pSetTimer.Call(hwnd, 1, 16, 0)", HOST_UI)

    def test_virtual_display_redirect_uses_extended_display_mode(self):
        self.assertIn("bool IsDisplayOnDesktop() override { return true; }", DRIVER)
        self.assertIn("Extended Display Mode required", DRIVER)
        self.assertIn("keep SteamVR Direct Mode disabled", DRIVER)
        self.assertIn("Extended\nDisplay Mode", DRIVER_README)
        self.assertNotIn("enable **SteamVR → Settings → Developer → Enable Direct Mode**", DRIVER_README)

    def test_component_query_state_is_exposed_to_host(self):
        self.assertIn("kStatus2VirtualDisplayComponentQueried", DRIVER)
        self.assertEqual(DRIVER.count("bool VirtualDisplayComponentQueried() const"), 1)
        self.assertIn("SteamVRVirtualDisplayComponentQueried", HOST_SERVICE)
        self.assertIn("STEAMVR HAS NOT QUERIED IVRVirtualDisplay", HOST_THEME)
        self.assertIn("LogInvalidPresentInfoOnce", DRIVER)
        self.assertIn("invalidPresent=%llu", DRIVER)

    def test_host_pose_state_has_a_dedicated_lock_separate_from_general_service_state(self):
        self.assertIn("poseStateMu           sync.RWMutex", HOST_SERVICE)
        self.assertIn("latestPose            *PoseSnapshot // guarded by poseStateMu", HOST_SERVICE)
        snapshot_body = HOST_SERVICE.split("func (s *HostService) Snapshot() HostSnapshot {", 1)[1].split("// PoseFrame", 1)[0]
        self.assertIn("s.mu.RUnlock()", snapshot_body)
        self.assertIn("s.poseStateMu.RLock()", snapshot_body)
        pose_loop = HOST_SERVICE.split("func (s *HostService) poseLoop", 1)[1]
        self.assertIn("s.poseStateMu.Lock()", pose_loop)
        self.assertIn("s.mu.Lock()", pose_loop) is False

    def test_display_refresh_setting_is_removed_and_driver_uses_fixed_72hz(self):
        self.assertNotIn("Display Refresh Rate", HOST_UI)
        self.assertNotIn("显示刷新率", HOST_UI)
        self.assertNotIn("display-refresh.json", DRIVER)
        self.assertIn("constexpr double gDisplayRefreshRateHz = 72.0", DRIVER)



if __name__ == "__main__":
    unittest.main()
