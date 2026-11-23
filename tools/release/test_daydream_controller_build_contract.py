from pathlib import Path
import unittest


ROOT = Path(__file__).resolve().parents[2]


class DaydreamControllerBuildContractTests(unittest.TestCase):
    def test_openvr_driver_does_not_mark_non_virtual_interface_destructor_override(self):
        source = (ROOT / "steamvr-driver/src/driver.cpp").read_text(encoding="utf-8")
        self.assertIn("class PhoneVRDaydreamController final : public vr::ITrackedDeviceServerDriver", source)
        self.assertIn("~PhoneVRDaydreamController() { Deactivate(); }", source)
        self.assertNotIn("~PhoneVRDaydreamController() override", source)

    def test_daydream_pose_uses_axis_angle_and_openvr_arm_model(self):
        protocol = (ROOT / "android/app/src/main/java/com/vsq/phonvrstream/controller/DaydreamControllerProtocol.kt").read_text(encoding="utf-8")
        driver = (ROOT / "steamvr-driver/src/driver.cpp").read_text(encoding="utf-8")
        model = (ROOT / "steamvr-driver/src/daydream_pose_model.h").read_text(encoding="utf-8")
        self.assertIn("fromRotationVector(xOrientation, yOrientation, zOrientation)", protocol)
        self.assertNotIn("fromEulerRadians", protocol)
        self.assertIn("DaydreamArmModel m_armModel", driver)
        self.assertIn("pose.vecVelocity[0] = modeledPose.velocity[0]", driver)
        self.assertIn("pose.vecAngularVelocity[0] = modeledPose.angularVelocity[0]", driver)
        self.assertIn("kAppRecenterHoldNs = 800000000ULL", model)
        self.assertIn("0.195 * handed", model)
        self.assertIn("0.195 * handed, -0.5, -0.005", model)
        self.assertIn("0.14, -0.08", model)
        self.assertIn("DaydreamToOpenVR", model)

    def test_app_long_press_requests_hardware_recenter(self):
        manager = (ROOT / "android/app/src/main/java/com/vsq/phonvrstream/controller/DaydreamControllerManager.kt").read_text(encoding="utf-8")
        protocol = (ROOT / "android/app/src/main/java/com/vsq/phonvrstream/controller/DaydreamControllerProtocol.kt").read_text(encoding="utf-8")
        self.assertIn("APP_RECENTER_HOLD_MS = 800L", manager)
        self.assertIn("requestHardwareRecenter(link)", manager)
        self.assertIn("byteArrayOf(0x00)", manager)
        self.assertIn("CONTROL_CHARACTERISTIC_UUID", protocol)

    def test_controller_panel_scroll_view_uses_frame_layout_params(self):
        source = (ROOT / "android/app/src/main/java/com/vsq/phonvrstream/MainActivity.kt").read_text(encoding="utf-8")
        start = source.index("    private fun buildControllerPanel(): LinearLayout {")
        end = source.index("\n    private fun controllerStatusBackground()", start)
        panel = source[start:end]
        self.assertIn("val contentScroll = ScrollView(this)", panel)
        self.assertIn("addView(content, FrameLayout.LayoutParams(-1, -2))", panel)
        self.assertIn("content.addView(controllerCandidateList", panel)
        self.assertNotIn("ScrollView.LayoutParams", panel)


if __name__ == "__main__":
    unittest.main()
