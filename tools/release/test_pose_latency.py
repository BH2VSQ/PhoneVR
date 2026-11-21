import unittest
from pathlib import Path

ROOT = Path(__file__).resolve().parents[2]
DRIVER = (ROOT / "steamvr-driver/src/driver.cpp").read_text(encoding="utf-8")
NETWORK = (ROOT / "android/app/src/main/java/com/vsq/phonvrstream/network/NetworkClient.kt").read_text(encoding="utf-8")
ACTIVITY = (ROOT / "android/app/src/main/java/com/vsq/phonvrstream/MainActivity.kt").read_text(encoding="utf-8")
QUATERNION = (ROOT / "android/app/src/main/java/com/vsq/phonvrstream/tracking/Quaternion.kt").read_text(encoding="utf-8")
HOST_SERVICE = (ROOT / "windows-host/cmd/phonevr-host/service.go").read_text(encoding="utf-8")
HOST_UI = (ROOT / "windows-host/cmd/phonevr-host/native_ui_windows.go").read_text(encoding="utf-8")
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
        self.assertIn("poseBatchToForward(batch)", HOST_SERVICE)
        self.assertIn("maxPoseBatchForward = 32", HOST_SERVICE)
        self.assertIn("SetReadBuffer(16 * 1024)", HOST_SERVICE)
        self.assertIn("time.Now().Add(250 * time.Microsecond)", HOST_SERVICE)

    def test_driver_drains_stale_udp_queue_and_predicts_with_angular_velocity(self):
        self.assertIn("ioctlsocket(socket, FIONREAD, &pendingBytes)", DRIVER)
        self.assertIn("only the freshest one", DRIVER)
        self.assertIn("EstimateAngularVelocity", DRIVER)
        self.assertIn("EstimateAngularVelocity(velocityBase, latest)", DRIVER)
        self.assertIn("EstimateAngularVelocity(velocityBase, candidate)", DRIVER)
        self.assertIn("next.sensorTimestampNs = ReadBE64(data + 12)", DRIVER)
        self.assertIn("current.sensorTimestampNs - previous.sensorTimestampNs", DRIVER)
        self.assertIn("pose.vecAngularVelocity[0]", DRIVER)
        self.assertIn("pose.vecAngularVelocity[1]", DRIVER)
        self.assertIn("pose.vecAngularVelocity[2]", DRIVER)
        self.assertIn("pose.poseTimeOffset = -static_cast<double>(predictionAgeNs)", DRIVER)
        self.assertIn("pose telemetry age=%.2f ms angular-speed=%.1f deg/s pitch=%.1f deg", DRIVER)
        self.assertIn("constexpr double alpha = 0.80", DRIVER)

    def test_native_log_edit_forces_repaint_and_receives_scroll_messages(self):
        self.assertIn("setNativeEditTextAndRedraw(d.controls[501], text)", HOST_UI)
        self.assertIn("WM_SETREDRAW(FALSE)", HOST_UI)
        self.assertIn("EM_SCROLLCARET", HOST_UI)
        self.assertIn("dialog.kind == \"logs\"", HOST_UI)
        self.assertIn("pSendMessage.Call(dialog.controls[501], uintptr(message), wParam, 0)", HOST_UI)

    def test_driver_requests_non_desktop_hmd_display_and_documents_runtime_toggle(self):
        self.assertIn("bool IsDisplayOnDesktop() override { return false; }", DRIVER)
        self.assertIn("Enable Direct Mode", DRIVER)
        self.assertIn("SteamVR Settings > Developer > Enable Direct Mode", DRIVER)
        self.assertIn("IVRVirtualDisplay", DRIVER_README)


if __name__ == "__main__":
    unittest.main()
