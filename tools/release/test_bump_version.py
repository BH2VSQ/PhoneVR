import sys
import tempfile
import unittest
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parent))
from bump_version import bump_repository, next_version


class VersionBumpTests(unittest.TestCase):
    def test_patch(self):
        self.assertEqual(next_version("0.3.8", "patch"), "0.3.9")

    def test_minor(self):
        self.assertEqual(next_version("0.3.8", "minor"), "0.4.0")

    def test_major(self):
        self.assertEqual(next_version("0.3.8", "major"), "1.0.0")

    def test_rejects_non_semver(self):
        with self.assertRaises(ValueError):
            next_version("v0.3.8", "patch")

    def test_updates_all_release_metadata(self):
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            (root / "android/app/src/main/java/com/vsq/phonvrstream/network").mkdir(parents=True)
            (root / "windows-host/cmd/phonevr-host").mkdir(parents=True)
            (root / "windows-host").mkdir(exist_ok=True)
            (root / "android/app/build.gradle.kts").write_text('versionCode = 12\nversionName = "0.3.8"\n', encoding="utf-8")
            (root / "android/app/src/main/java/com/vsq/phonvrstream/network/NetworkClient.kt").write_text('private const val CLIENT_VERSION = "PhoneVRStream-0.3.8"\n', encoding="utf-8")
            (root / "windows-host/cmd/phonevr-host/main.go").write_text('var appVersion = "0.3.8"\n', encoding="utf-8")
            (root / "README.md").write_text('Repository revision: **0.3.8**. Windows Host: **0.3.8**. Android client: **0.3.8**.\n', encoding="utf-8")
            (root / "windows-host/README.md").write_text('# PhoneVR Host 0.3.8\n', encoding="utf-8")
            (root / "VERSION").write_text('0.3.8\n', encoding="ascii")
            (root / "CHANGELOG.md").write_text('# Changelog\n', encoding="utf-8")

            version, version_code = bump_repository(root, "patch")
            self.assertEqual(version, "0.3.9")
            self.assertEqual(version_code, 13)
            self.assertIn('versionCode = 13', (root / "android/app/build.gradle.kts").read_text())
            self.assertIn('versionName = "0.3.9"', (root / "android/app/build.gradle.kts").read_text())
            self.assertIn('PhoneVRStream-0.3.9', (root / "android/app/src/main/java/com/vsq/phonvrstream/network/NetworkClient.kt").read_text())
            self.assertIn('appVersion = "0.3.9"', (root / "windows-host/cmd/phonevr-host/main.go").read_text())
            self.assertEqual((root / "VERSION").read_text().strip(), "0.3.9")
            self.assertTrue((root / "CHANGELOG.md").read_text().startswith("## 0.3.9 - Automated release"))


if __name__ == "__main__":
    unittest.main()
