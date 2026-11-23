from pathlib import Path
import unittest


ROOT = Path(__file__).resolve().parents[2]


class DaydreamControllerBuildContractTests(unittest.TestCase):
    def test_openvr_driver_does_not_mark_non_virtual_interface_destructor_override(self):
        source = (ROOT / "steamvr-driver/src/driver.cpp").read_text(encoding="utf-8")
        self.assertIn("class PhoneVRDaydreamController final : public vr::ITrackedDeviceServerDriver", source)
        self.assertIn("~PhoneVRDaydreamController() { Deactivate(); }", source)
        self.assertNotIn("~PhoneVRDaydreamController() override", source)

    def test_controller_panel_scroll_view_uses_frame_layout_params(self):
        source = (ROOT / "android/app/src/main/java/com/vsq/phonvrstream/MainActivity.kt").read_text(encoding="utf-8")
        self.assertIn("val contentScroll = ScrollView(this)", source)
        self.assertIn("addView(content, FrameLayout.LayoutParams(-1, -2))", source)
        self.assertIn("content.addView(controllerCandidateList", source)
        self.assertNotIn("ScrollView.LayoutParams", source)


if __name__ == "__main__":
    unittest.main()
