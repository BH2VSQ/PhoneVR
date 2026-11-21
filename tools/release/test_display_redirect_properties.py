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

    def test_hmd_registration_waits_for_display_redirect_activation(self):
        provider_start = self.source.index("class PhoneVRProvider")
        provider_end = self.source.index("PhoneVRProvider g_provider", provider_start)
        provider = self.source[provider_start:provider_end]
        init_start = provider.index("vr::EVRInitError Init(")
        cleanup_start = provider.index("void Cleanup()", init_start)
        init_body = provider[init_start:cleanup_start]
        self.assertIn(
            'TrackedDeviceAdded("PHONEVR-DISPLAY-001", vr::TrackedDeviceClass_DisplayRedirect, m_redirect)',
            init_body,
        )
        self.assertNotIn('TrackedDeviceAdded("PHONEVR-HMD-001"', init_body)

        runframe_start = provider.index("void RunFrame() override")
        runframe_end = provider.index("bool ShouldBlockStandbyMode()", runframe_start)
        runframe = provider[runframe_start:runframe_end]
        activation_gate = runframe.index("m_redirect->ActivationFinished()")
        hmd_add = runframe.index(
            'TrackedDeviceAdded("PHONEVR-HMD-001", vr::TrackedDeviceClass_HMD, m_hmd)'
        )
        self.assertLess(activation_gate, hmd_add)
        self.assertIn("redirectActivationTimedOut", runframe)
        self.assertIn("5000000000ULL", runframe)
        self.assertIn("registering HMD for pose-only operation without live video", runframe)

    def test_hmd_activation_reads_effective_redirect_override(self):
        hmd_start = self.source.index("class PhoneVRHmd")
        hmd_end = self.source.index("class VirtualDisplayComponent", hmd_start)
        hmd = self.source[hmd_start:hmd_end]
        self.assertIn("GetBoolProperty(", hmd)
        self.assertIn("Prop_HasVirtualDisplayComponent_Bool", hmd)
        self.assertIn("GetUint64Property(", hmd)
        self.assertIn("HMD activation sees the DisplayRedirect override before display initialization", hmd)


if __name__ == "__main__":
    unittest.main()
