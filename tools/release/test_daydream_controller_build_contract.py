from pathlib import Path
import unittest
import json


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
        self.assertIn("return NormalizeQuaternion(input);", model)
        self.assertIn("bool m_hasOrientationAlignment = true;", model)
        self.assertNotIn("Recenter(rawDaydreamOrientation, headQ);", model)
        self.assertIn('vr::Prop_RenderModelName_String', driver)
        self.assertIn('oculus_quest2_controller_left', driver)
        self.assertIn('oculus_quest2_controller_right', driver)
        profile = (ROOT / "steamvr-driver/resources/input/phonevr_daydream_controller_profile.json").read_text(encoding="utf-8")
        legacy = (ROOT / "steamvr-driver/resources/input/phonevr_daydream_legacy_bindings.json").read_text(encoding="utf-8")
        self.assertIn('"input_bindingui_mode": "controller_handed"', profile)
        self.assertIn('"/pose/tip"', profile)
        self.assertIn('/user/hand/left/pose/tip', legacy)
        self.assertIn('/user/hand/right/pose/tip', legacy)
        self.assertIn('"type": "joystick"', profile)
        binding = json.loads((ROOT / "steamvr-driver/resources/input/default_bindings/steam.app.438100_phonevr_daydream_controller.json").read_text(encoding="utf-8"))
        self.assertEqual(binding["app_key"], "steam.app.438100")
        self.assertIn("/actions/global", binding["bindings"])
        self.assertIn("/actions/one_hand", binding["bindings"])
        self.assertIn("/actions/menu", binding["bindings"])
        self.assertIn("/actions/action_menu", binding["bindings"])
        global_sources = binding["bindings"]["/actions/global"]["sources"]
        one_hand_sources = binding["bindings"]["/actions/one_hand"]["sources"]
        self.assertTrue(all(any(source["path"] == f"/user/hand/{hand}/input/joystick" and source["mode"] == "joystick" and source["inputs"].get("position", {}).get("output") == "/actions/global/in/move" for source in global_sources) for hand in ("left", "right")))
        self.assertTrue(all(any(source["path"] == f"/user/hand/{hand}/input/joystick" and source["inputs"].get("position", {}).get("output") == "/actions/one_hand/in/move_rotate" for source in one_hand_sources) for hand in ("left", "right")))
        menu_sources = binding["bindings"]["/actions/menu"]["sources"]
        action_menu_sources = binding["bindings"]["/actions/action_menu"]["sources"]
        for hand in ("left", "right"):
            self.assertTrue(any(source["path"] == f"/user/hand/{hand}/input/joystick" and source["inputs"].get("position", {}).get("output") == "/actions/menu/in/scroll" for source in menu_sources))
            self.assertTrue(any(source["path"] == f"/user/hand/{hand}/input/joystick" and source["inputs"].get("click", {}).get("output") == "/actions/menu/in/confirm" for source in menu_sources))
            self.assertTrue(any(source["path"] == f"/user/hand/{hand}/input/joystick" and source["inputs"].get("position", {}).get("output") == "/actions/action_menu/in/axis" for source in action_menu_sources))
            self.assertTrue(any(source["path"] == f"/user/hand/{hand}/input/joystick" and source["inputs"].get("click", {}).get("output") == "/actions/action_menu/in/select" for source in action_menu_sources))
        cmake = (ROOT / "steamvr-driver/CMakeLists.txt").read_text(encoding="utf-8")
        self.assertIn("install(DIRECTORY resources/ DESTINATION resources OPTIONAL)", cmake)
        self.assertTrue((ROOT / "steamvr-driver/resources/input/default_bindings/steam.app.438100_phonevr_daydream_controller.json").is_file())
        for action_set in ("/actions/global", "/actions/one_hand"):
            for hand in ("left", "right"):
                app_source = next(source for source in binding["bindings"][action_set]["sources"] if source["path"] == f"/user/hand/{hand}/input/menu")
                self.assertIn("click", app_source["inputs"])
                self.assertIn("held", app_source["inputs"])
                self.assertEqual(app_source["inputs"].get("double", {}).get("output"), f"{action_set}/in/main_menu")
        self.assertIn("class DaydreamButtonGesture", model)
        self.assertIn("kLongPressNs = 800000000ULL", model)
        self.assertIn("DaydreamTouchpadToOpenVR", model)
        self.assertIn("controllerHeading = HorizontalHeadRotation(rawOpenVr)", model)
        self.assertIn("headHeading = HorizontalHeadRotation(headQ)", model)
        self.assertIn(".put(\"gyroscopeX\", state.gyroscopeX.toDouble())", (ROOT / "android/app/src/main/java/com/vsq/phonvrstream/network/NetworkClient.kt").read_text(encoding="utf-8"))
        self.assertIn("GyroscopeX", (ROOT / "windows-host/internal/protocol/controller.go").read_text(encoding="utf-8"))
        self.assertIn("(void)controllerGyroscope", model)
        self.assertIn("m_headYawReference", model)
        self.assertIn("HorizontalHeadRotation(headQ)", model)
        self.assertIn("kHeadFollowTimeConstantSeconds", model)
        self.assertNotIn("(gyroMagnitude - 0.2) / 45.0", model)
        self.assertIn("kControllerPacketSize = 56", driver)
        self.assertIn("data[4] != 2", driver)
        self.assertIn("state.gyroX", driver)

    def test_app_is_holdable_menu_and_home_long_press_recenters(self):
        manager = (ROOT / "android/app/src/main/java/com/vsq/phonvrstream/controller/DaydreamControllerManager.kt").read_text(encoding="utf-8")
        driver = (ROOT / "steamvr-driver/src/driver.cpp").read_text(encoding="utf-8")
        protocol = (ROOT / "android/app/src/main/java/com/vsq/phonvrstream/controller/DaydreamControllerProtocol.kt").read_text(encoding="utf-8")
        self.assertIn("Daydream App button", manager)
        self.assertNotIn("requestHardwareRecenter", manager)
        self.assertNotIn("byteArrayOf(0x00)", manager)
        self.assertNotIn("m_appGesture", driver)
        self.assertIn("m_homeGesture.Update(controllerFresh, state.home, nowNs)", driver)
        self.assertIn("m_pendingRecenter = true", driver)
        self.assertIn("DaydreamButtonGesture m_homeGesture", driver)
        self.assertIn("connected && state.app", driver)
        self.assertIn("m_systemPulseUntilNs", driver)
        self.assertIn("m_armModel.Recenter(controllerQ, headQ)", driver)
        self.assertIn("DaydreamTouchpadToOpenVR(state.touchX, state.touchY, state.touching)", driver)
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
