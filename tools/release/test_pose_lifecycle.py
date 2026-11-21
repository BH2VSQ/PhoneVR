import re
import unittest
from pathlib import Path

ROOT = Path(__file__).resolve().parents[2]
DRIVER = ROOT / "steamvr-driver/src/driver.cpp"
NETWORK = ROOT / "android/app/src/main/java/com/vsq/phonvrstream/network/NetworkClient.kt"
ACTIVITY = ROOT / "android/app/src/main/java/com/vsq/phonvrstream/MainActivity.kt"


class PoseLifecycleTests(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        cls.driver = DRIVER.read_text(encoding="utf-8")
        cls.network = NETWORK.read_text(encoding="utf-8")
        cls.activity = ACTIVITY.read_text(encoding="utf-8")

    def test_pose_timeout_does_not_disconnect_activated_hmd(self):
        self.assertIn("kTrackingPoseTimeoutNs = 1000000000ULL", self.driver)
        self.assertIn("pose.deviceIsConnected = true", self.driver)
        self.assertNotIn("pose.deviceIsConnected = deviceConnected", self.driver)
        self.assertNotIn("m_lastConnected", self.driver)
        hmd = self.driver.split("class PhoneVRHmd", 1)[1].split("class VirtualDisplayComponent", 1)[0]
        run_frame = hmd.split("void RunFrame() {", 1)[1].split("\nprivate:", 1)[0]
        publisher = hmd.split("void PosePublisherLoop() {", 1)[1].split("\n    }\n\n    uint32_t m_objectId", 1)[0]
        self.assertNotIn("TrackedDevicePoseUpdated", run_frame)
        self.assertIn("TrackedDevicePoseUpdated", publisher)
        self.assertIn("m_receiver.WaitForUpdate", publisher)
        self.assertIn("std::chrono::milliseconds(10)", publisher)
        self.assertIn("m_posePublisherThread = std::thread", hmd)
        self.assertIn("m_posePublisherThread.join()", hmd)
        self.assertIn("~PhoneVRHmd() { Deactivate(); }", hmd)

    def test_initial_pose_is_connected_but_untracked(self):
        helper = re.search(
            r"static\s+vr::DriverPose_t\s+MakeUntrackedPose\(\)\s*\{(?P<body>.*?)\n    \}",
            self.driver,
            re.DOTALL,
        )
        self.assertIsNotNone(helper)
        body = helper.group("body")
        self.assertIn("pose.deviceIsConnected = true", body)
        self.assertIn("pose.poseIsValid = false", body)
        self.assertIn("vr::TrackingResult_Uninitialized", body)

    def test_optical_catalogue_reply_is_not_queued_on_ui_thread(self):
        block = re.search(
            r'"get_optical_presets"\s*->\s*\{(?P<body>.*?)\n                    \}',
            self.network,
            re.DOTALL,
        )
        self.assertIsNotNone(block)
        body = block.group("body")
        self.assertIn("currentOpticalPresets()", body)
        self.assertIn('"optical_presets"', body)
        self.assertNotIn("mainHandler.post", body)

    def test_manual_discovery_autofills_and_persists_selected_host(self):
        self.assertIn("fillConnectionAddress(selected, persist = true)", self.activity)
        self.assertIn("putString(\"host\", host.address)", self.activity)
        self.assertIn("putInt(\"port\", host.controlPort)", self.activity)


if __name__ == "__main__":
    unittest.main()
