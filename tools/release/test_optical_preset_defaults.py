from pathlib import Path
import unittest

ROOT = Path(__file__).resolve().parents[2]


class OpticalPresetDefaultOffsetTests(unittest.TestCase):
    def test_android_builtin_presets_use_requested_per_eye_offsets(self):
        source = (ROOT / "android/app/src/main/java/com/vsq/phonvrstream/settings/OpticalPreset.kt").read_text(encoding="utf-8")
        for field, value in (("leftEyeOffsetX", "0.09f"), ("leftEyeOffsetY", "0.09f"),
                             ("rightEyeOffsetX", "-0.09f"), ("rightEyeOffsetY", "0.09f")):
            self.assertIn(f"{field} = {value}", source)

    def test_existing_untouched_builtins_are_migrated_without_replacing_user_presets(self):
        presets = (ROOT / "android/app/src/main/java/com/vsq/phonvrstream/settings/OpticalPreset.kt").read_text(encoding="utf-8")
        store = (ROOT / "android/app/src/main/java/com/vsq/phonvrstream/settings/SettingsStore.kt").read_text(encoding="utf-8")
        self.assertIn("fun migrateLegacyZeroEyeOffsets", presets)
        self.assertIn("preset.builtin && preset.id in builtInIds && offsetsAreZero", presets)
        self.assertIn("OpticalPreset.migrateLegacyZeroEyeOffsets(parsed)", store)

    def test_windows_host_defaults_match_android_offsets(self):
        source = (ROOT / "windows-host/cmd/phonevr-host/phone_settings.go").read_text(encoding="utf-8")
        for fragment in ("LeftEyeOffsetX: .09", "LeftEyeOffsetY: .09",
                         "RightEyeOffsetX: -.09", "RightEyeOffsetY: .09"):
            self.assertIn(fragment, source)


if __name__ == "__main__":
    unittest.main()
