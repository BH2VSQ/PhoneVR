from pathlib import Path
import re
import unittest

ROOT = Path(__file__).resolve().parents[2]


class NativeDropdownLayoutTests(unittest.TestCase):
    def test_transport_combos_reserve_expanded_list_height(self):
        source = (ROOT / "windows-host/cmd/phonevr-host/native_ui_windows.go").read_text(encoding="utf-8")

        self.assertRegex(source, r"cbNoIntegralHeight\s*=\s*0x0400")
        self.assertIn("cbDropDownList | cbNoIntegralHeight", source)

        mode = re.search(
            r"move\(d\.controls\[idTransportMode\],\s*margin,\s*(\d+),\s*w-2\*margin,\s*(\d+)\)",
            source,
        )
        preference = re.search(
            r"move\(d\.controls\[idTransportPref\],\s*margin,\s*(\d+),\s*w-2\*margin,\s*(\d+)\)",
            source,
        )
        self.assertIsNotNone(mode, "transport mode combo layout was not found")
        self.assertIsNotNone(preference, "transport preference combo layout was not found")

        mode_top, mode_height = map(int, mode.groups())
        preference_top, preference_height = map(int, preference.groups())
        self.assertGreaterEqual(mode_height, 96)
        self.assertGreaterEqual(preference_height, 80)
        self.assertLessEqual(mode_top + mode_height, 258)
        self.assertLessEqual(preference_top + preference_height, 400)

    def test_transport_dialog_has_room_for_expanded_controls(self):
        source = (ROOT / "windows-host/cmd/phonevr-host/native_ui_windows.go").read_text(encoding="utf-8")
        self.assertIn('width, height, title = 720, 560, app.text("视频传输模式", "Video Transport")', source)


if __name__ == "__main__":
    unittest.main()
