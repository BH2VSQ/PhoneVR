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
            (root / "windows-host/internal/aoa").mkdir(parents=True)
            (root / "windows-host").mkdir(exist_ok=True)
            (root / "steamvr-driver/src").mkdir(parents=True)
            (root / "android/app/src/main/res/xml").mkdir(parents=True)
            (root / "docs").mkdir(parents=True)
            (root / "steamvr-driver").mkdir(exist_ok=True)
            (root / "android/app/build.gradle.kts").write_text('versionCode = 12\nversionName = "0.3.8"\n', encoding="utf-8")
            (root / "android/app/src/main/java/com/vsq/phonvrstream/network/NetworkClient.kt").write_text('private const val CLIENT_VERSION = "PhoneVRStream-0.3.8"\n', encoding="utf-8")
            (root / "windows-host/cmd/phonevr-host/main.go").write_text('var appVersion = "0.3.8"\n', encoding="utf-8")
            (root / "windows-host/internal/aoa/aoa_windows.go").write_text('stringsToSend := []string{"VSQ Studio", "PhoneVR", "PhoneVR USB streaming", "0.3.8", "https://github.com/BH2VSQ/PhoneVR", "PhoneVR-Android"}\n', encoding="utf-8")
            (root / "android/app/src/main/res/xml/accessory_filter.xml").write_text('<resources>\n    <usb-accessory manufacturer="VSQ Studio" model="PhoneVR" version="0.3.8" />\n</resources>\n', encoding="utf-8")
            (root / "steamvr-driver/src/driver.cpp").write_text('"PhoneVR 0.3.8 DisplayRedirect order-fix diagnostics SteamVR driver initialized"\n', encoding="utf-8")
            (root / "docs/usb-aoa-windows-driver.md").write_text('PhoneVR 0.3.8 provides a direct USB transport\nThe accessory filter descriptors are `VSQ Studio` / `PhoneVR` / `0.3.8`.\n', encoding="utf-8")
            (root / "README.md").write_text('Repository revision: **0.3.8**. Windows Host: **0.3.8**. Android client: **0.3.8**.\n\n## Windows Host 0.3.8 hotfix\n\n## Android client 0.3.8\n\n## SteamVR driver 0.3.8\n', encoding="utf-8")
            (root / "windows-host/README.md").write_text('# PhoneVR Host 0.3.8\n', encoding="utf-8")
            (root / "steamvr-driver/CMakeLists.txt").write_text('project(PhoneVRSteamVRDriver VERSION 0.3.8 LANGUAGES CXX)\n', encoding="utf-8")
            (root / "steamvr-driver/README.md").write_text('# PhoneVR SteamVR Driver — 0.3.8\n', encoding="utf-8")
            (root / "VERSION").write_text('0.3.8\n', encoding="ascii")
            (root / "CHANGELOG.md").write_text('# Changelog\n', encoding="utf-8")
            (root / "BUILD_STATUS.md").write_text(
                '# PhoneVR 0.3.8 build status\n\n'
                '- Android metadata is `versionName 0.3.8`, `versionCode 12`; repository, Host, SteamVR driver and AOA accessory metadata are synchronized to `0.3.8`.\n',
                encoding="utf-8",
            )

            version, version_code = bump_repository(root, "patch")
            self.assertEqual(version, "0.3.9")
            self.assertEqual(version_code, 13)
            self.assertIn('versionCode = 13', (root / "android/app/build.gradle.kts").read_text())
            self.assertIn('versionName = "0.3.9"', (root / "android/app/build.gradle.kts").read_text())
            self.assertIn('PhoneVRStream-0.3.9', (root / "android/app/src/main/java/com/vsq/phonvrstream/network/NetworkClient.kt").read_text())
            self.assertIn('appVersion = "0.3.9"', (root / "windows-host/cmd/phonevr-host/main.go").read_text())
            self.assertIn('PhoneVR USB streaming", "0.3.9"', (root / "windows-host/internal/aoa/aoa_windows.go").read_text())
            self.assertIn('version="0.3.9"', (root / "android/app/src/main/res/xml/accessory_filter.xml").read_text())
            self.assertIn('PhoneVR 0.3.9 DisplayRedirect', (root / "steamvr-driver/src/driver.cpp").read_text())
            aoa_doc = (root / "docs/usb-aoa-windows-driver.md").read_text()
            self.assertIn('PhoneVR 0.3.9 provides a direct USB transport', aoa_doc)
            self.assertIn('`PhoneVR` / `0.3.9`', aoa_doc)
            self.assertIn('VERSION 0.3.9', (root / "steamvr-driver/CMakeLists.txt").read_text())
            self.assertIn('Driver — 0.3.9', (root / "steamvr-driver/README.md").read_text())
            self.assertIn('## Windows Host 0.3.9 bundled SteamVR driver', (root / "README.md").read_text())
            self.assertIn('## Android client 0.3.9', (root / "README.md").read_text())
            self.assertIn('## SteamVR driver 0.3.9', (root / "README.md").read_text())
            self.assertEqual((root / "VERSION").read_text().strip(), "0.3.9")
            self.assertTrue((root / "CHANGELOG.md").read_text().startswith("## 0.3.9 - Automated release"))
            build_status = (root / "BUILD_STATUS.md").read_text()
            self.assertTrue(build_status.startswith("# PhoneVR 0.3.9 build status"))
            self.assertIn("versionCode 13", build_status)


if __name__ == "__main__":
    unittest.main()
