from pathlib import Path
import unittest


ROOT = Path(__file__).resolve().parents[2]


class DaydreamControllerBuildContractTests(unittest.TestCase):
    def test_openvr_driver_does_not_mark_non_virtual_interface_destructor_override(self):
        source = (ROOT / "steamvr-driver/src/driver.cpp").read_text(encoding="utf-8")
        self.assertIn("class PhoneVRDaydreamController final : public vr::ITrackedDeviceServerDriver", source)
        self.assertIn("~PhoneVRDaydreamController() { Deactivate(); }", source)
        self.assertNotIn("~PhoneVRDaydreamController() override", source)

    def test_daydream_pose_uses_wire_axis_order_and_openvr_arm_model(self):
        protocol = (ROOT / "android/app/src/main/java/com/vsq/phonvrstream/controller/DaydreamControllerProtocol.kt").read_text(encoding="utf-8")
        driver = (ROOT / "steamvr-driver/src/driver.cpp").read_text(encoding="utf-8")
        model = (ROOT / "steamvr-driver/src/daydream_pose_model.h").read_text(encoding="utf-8")
        self.assertIn("XKF3 packs the rotation vector in X, Z, Y order", protocol)
        self.assertIn("fromRotationVector(xOrientation, yOrientation, zOrientation)", protocol)
        self.assertNotIn("fromEulerRadians", protocol)
        self.assertIn("DaydreamArmModel m_armModel", driver)
        self.assertIn("pose.vecVelocity[0] = modeledPose.velocity[0]", driver)
        self.assertIn("pose.vecAngularVelocity[0] = modeledPose.angularVelocity[0]", driver)
        self.assertIn("0.195 * handed, -0.5, -0.005", model)
        self.assertIn("0.14, -0.08", model)
        self.assertIn("DaydreamToOpenVR", model)
        self.assertIn("class DaydreamButtonGesture", model)
        self.assertIn("kLongPressNs = 800000000ULL", model)
        self.assertIn(".put(\"gyroscopeX\", state.gyroscopeX.toDouble())", (ROOT / "android/app/src/main/java/com/vsq/phonvrstream/network/NetworkClient.kt").read_text(encoding="utf-8"))
        self.assertIn("GyroscopeX", (ROOT / "windows-host/internal/protocol/controller.go").read_text(encoding="utf-8"))
        self.assertIn("controllerGyroscope", model)
        self.assertIn("(gyroMagnitude - 0.2) / 45.0", model)
        self.assertIn("kControllerPacketSize = 56", driver)
        self.assertIn("data[4] != 2", driver)
        self.assertIn("state.gyroX", driver)

    def test_app_long_press_is_detected_independent_of_hmd_tracking(self):
        manager = (ROOT / "android/app/src/main/java/com/vsq/phonvrstream/controller/DaydreamControllerManager.kt").read_text(encoding="utf-8")
        driver = (ROOT / "steamvr-driver/src/driver.cpp").read_text(encoding="utf-8")
        protocol = (ROOT / "android/app/src/main/java/com/vsq/phonvrstream/controller/DaydreamControllerProtocol.kt").read_text(encoding="utf-8")
        self.assertIn("Daydream App button", manager)
        self.assertNotIn("requestHardwareRecenter", manager)
        self.assertNotIn("byteArrayOf(0x00)", manager)
        self.assertIn("m_appGesture.Update(controllerFresh, state.app, nowNs)", driver)
        self.assertIn("m_homeGesture.Update(controllerFresh, state.home, nowNs)", driver)
        self.assertIn("m_pendingRecenter = true", driver)
        self.assertIn("DaydreamButtonGesture m_appGesture", driver)
        self.assertIn("DaydreamButtonGesture m_homeGesture", driver)
        self.assertIn("m_systemPulseUntilNs", driver)
        self.assertLess(driver.index("m_appGesture.Update(controllerFresh, state.app, nowNs)"), driver.index("if (trackingValid) {", driver.index("void Update(const DaydreamControllerSnapshot&")))
        self.assertIn("m_armModel.Recenter(controllerQ, headQ)", driver)
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
