import json
import re
import unittest
from datetime import datetime
from pathlib import Path

ROOT = Path(__file__).resolve().parents[2]
DRIVER_CPP = ROOT / "steamvr-driver/src/driver.cpp"


class ChaperoneConfigTests(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        cls.source = DRIVER_CPP.read_text(encoding="utf-8")
        json_match = re.search(
            r'constexpr\s+char\s+kDriverProvidedChaperoneJson\[\]\s*=\s*'
            r'R"(?P<delimiter>[^()\\\s]{0,32})\((.*?)\)(?P=delimiter)";',
            cls.source,
            re.DOTALL,
        )
        if not json_match:
            raise AssertionError("Could not locate the embedded PhoneVR Chaperone JSON")
        cls.raw_string_delimiter = json_match.group("delimiter")
        cls.config = json.loads(json_match.group(2))
        id_match = re.search(
            r'constexpr\s+uint32_t\s+kPhoneVRUniverseId\s*=\s*0x([0-9A-Fa-f]+)u;',
            cls.source,
        )
        if not id_match:
            raise AssertionError("Could not locate kPhoneVRUniverseId")
        cls.cpp_universe_id = int(id_match.group(1), 16)

    def test_cpp_raw_string_delimiter_respects_standard_limit(self):
        # C++ raw-string delimiters may contain at most 16 characters.
        self.assertLessEqual(len(self.raw_string_delimiter), 16)

    def test_json_matches_openvr_v5_schema_basics(self):
        self.assertEqual(self.config["json_id"], "chaperone_info")
        self.assertEqual(self.config["version"], 5)
        self.assertIsInstance(self.config["universes"], list)
        self.assertEqual(len(self.config["universes"]), 1)
        timestamp = self.config["time"].replace("Z", "+00:00")
        self.assertIsNotNone(datetime.fromisoformat(timestamp).tzinfo)

    def test_universe_id_matches_hmd_property_constant(self):
        universe = self.config["universes"][0]
        self.assertEqual(universe["universeID"], self.cpp_universe_id)
        self.assertGreater(self.cpp_universe_id, 0)
        self.assertLessEqual(self.cpp_universe_id, 0xFFFFFFFF)
        self.assertIn("vr::Prop_CurrentUniverseId_Uint64", self.source)
        self.assertIn("vr::Prop_DriverProvidedChaperoneJson_String", self.source)

    def test_3dof_configuration_does_not_claim_roomscale_bounds(self):
        universe = self.config["universes"][0]
        self.assertEqual(universe["collision_bounds"], [])
        self.assertEqual(universe["play_area"], [1.0, 1.0])
        for origin_name in ("seated", "standing"):
            origin = universe[origin_name]
            self.assertEqual(origin["translation"], [0.0, 0.0, 0.0])
            self.assertEqual(origin["yaw"], 0.0)

    def test_properties_are_checked_and_failures_are_logged_without_disabling_hmd(self):
        self.assertIn("universeResult != vr::TrackedProp_Success", self.source)
        self.assertIn("chaperoneResult != vr::TrackedProp_Success", self.source)
        self.assertIn("failed to publish driver Chaperone", self.source)
        self.assertIn("continuing HMD activation without driver Chaperone", self.source)
        self.assertIn("driver-provided Chaperone published", self.source)
        activation_block = self.source.split("const auto universeResult", 1)[1].split(
            "if (!m_receiver.Start()", 1
        )[0]
        self.assertNotIn("return vr::VRInitError_Driver_Unknown", activation_block)


if __name__ == "__main__":
    unittest.main()
