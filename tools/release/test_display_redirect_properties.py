"""Regression checks for OpenVR's HMD display-redirect property bridge."""
from pathlib import Path
import re
import unittest

ROOT = Path(__file__).resolve().parents[2]
DRIVER = ROOT / "steamvr-driver/src/driver.cpp"
MANIFEST = ROOT / "steamvr-driver/driver.vrdrivermanifest"


class DisplayRedirectPropertyTests(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        cls.source = DRIVER.read_text(encoding="utf-8")
        cls.redirect_start = cls.source.index("class PhoneVRDisplayRedirect")
        cls.redirect_end = cls.source.index("class PhoneVRProvider", cls.redirect_start)
        cls.redirect = cls.source[cls.redirect_start:cls.redirect_end]

    def test_manifest_declares_display_redirection(self):
        self.assertIn('"redirectsDisplay": true', MANIFEST.read_text(encoding="utf-8"))

    def test_redirect_device_publishes_adapter_luid_to_hmd_override_container(self):
        self.assertRegex(
            self.redirect,
            r"SetUint64Property\s*\(\s*vr::k_ulDisplayRedirectContainer\s*,\s*"
            r"vr::Prop_GraphicsAdapterLuid_Uint64\s*,\s*adapterLuid\s*\)",
        )

    def test_redirect_device_marks_hmd_as_virtual_display(self):
        self.assertRegex(
            self.redirect,
            r"SetBoolProperty\s*\(\s*vr::k_ulDisplayRedirectContainer\s*,\s*"
            r"vr::Prop_HasVirtualDisplayComponent_Bool\s*,\s*true\s*\)",
        )

    def test_accessory_keeps_its_own_adapter_property(self):
        self.assertRegex(
            self.redirect,
            r"SetUint64Property\s*\(\s*container\s*,\s*"
            r"vr::Prop_GraphicsAdapterLuid_Uint64\s*,\s*adapterLuid\s*\)",
        )

    def test_property_write_failures_are_logged(self):
        self.assertIn("hmdRedirectLuidResult != vr::TrackedProp_Success", self.redirect)
        self.assertIn("hmdVirtualDisplayResult != vr::TrackedProp_Success", self.redirect)
        self.assertIn("k_ulDisplayRedirectContainer", self.redirect)


if __name__ == "__main__":
    unittest.main()
