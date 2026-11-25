from pathlib import Path
import re
import unittest

ROOT = Path(__file__).resolve().parents[2]


class NetworkDiscoveryTests(unittest.TestCase):
    def test_d3d11_multithread_header_is_included(self):
        source = (ROOT / "steamvr-driver/src/driver.cpp").read_text(encoding="utf-8")
        self.assertRegex(source, r"#include\s+<d3d11_4\.h>")
        self.assertIn("ID3D11Multithread", source)

    def test_android_discovery_uses_active_ipv4_interfaces(self):
        network = (ROOT / "android/app/src/main/java/com/vsq/phonvrstream/network/NetworkClient.kt").read_text(encoding="utf-8")
        activity = (ROOT / "android/app/src/main/java/com/vsq/phonvrstream/MainActivity.kt").read_text(encoding="utf-8")
        self.assertIn("NetworkInterface.getNetworkInterfaces()", network)
        self.assertIn("enumerateDiscoveryTargets()", network)
        self.assertIn("bind(InetSocketAddress(target.localAddress, 0))", network)
        self.assertIn("target.broadcastAddress", network)
        self.assertNotIn("connectionModeSpinner", activity)
        self.assertIn("connectionTransportLabel", activity)
        self.assertIn("transportStatusListener", activity)
        self.assertNotIn("DiscoveryMode", network + activity)

    def test_removed_transport_selector_and_rndis_classifier_are_absent(self):
        network = (ROOT / "android/app/src/main/java/com/vsq/phonvrstream/network/NetworkClient.kt").read_text(encoding="utf-8")
        model = (ROOT / "android/app/src/main/java/com/vsq/phonvrstream/network/HeadPose.kt").read_text(encoding="utf-8")
        activity = (ROOT / "android/app/src/main/java/com/vsq/phonvrstream/MainActivity.kt").read_text(encoding="utf-8")
        service = (ROOT / "windows-host/cmd/phonevr-host/service.go").read_text(encoding="utf-8")
        self.assertNotIn("ConnectionInterfaceClassifier", network)
        self.assertNotIn("connectionType", network + model)
        self.assertNotIn("USB/RNDIS", service)
        self.assertNotIn("USB 有线", activity)

    def test_host_listens_on_configured_ipv4_interfaces(self):
        service = (ROOT / "windows-host/cmd/phonevr-host/service.go").read_text(encoding="utf-8")
        self.assertIn("localIPv4NetworkAddresses()", service)
        self.assertIn("net.JoinHostPort(config.ListenAddress", service)
        self.assertIn("Host interface [%s] %s: %s", service)

    def test_3dof_hmd_uses_standing_eye_height(self):
        driver = (ROOT / "steamvr-driver/src/driver.cpp").read_text(encoding="utf-8")
        self.assertIn("constexpr double kDefaultHeadHeightMeters = 1.65;", driver)
        self.assertIn("pose.vecPosition[1] = kDefaultHeadHeightMeters;", driver)

    def test_default_video_transport_is_network_only(self):
        service = (ROOT / "windows-host/cmd/phonevr-host/service.go").read_text(encoding="utf-8")
        transport = (ROOT / "windows-host/cmd/phonevr-host/video_transport.go").read_text(encoding="utf-8")
        self.assertIn("loadVideoTransportConfigFrom(transportConfigPath)", service)
        self.assertIn("mode, preferred := VideoTransportNetwork, VideoPreferNetwork", transport)
        self.assertIn("return VideoTransportNetwork", transport)
        self.assertIn("preferred = VideoPreferNetwork", transport)

    def test_version_metadata_is_synchronized(self):
        version = (ROOT / "VERSION").read_text(encoding="ascii").strip()
        gradle = (ROOT / "android/app/build.gradle.kts").read_text(encoding="utf-8")
        network = (ROOT / "android/app/src/main/java/com/vsq/phonvrstream/network/NetworkClient.kt").read_text(encoding="utf-8")
        cmake = (ROOT / "steamvr-driver/CMakeLists.txt").read_text(encoding="utf-8")
        host = (ROOT / "windows-host/cmd/phonevr-host/main.go").read_text(encoding="utf-8")
        aoa = (ROOT / "windows-host/internal/aoa/aoa_windows.go").read_text(encoding="utf-8")
        accessory_filter = (ROOT / "android/app/src/main/res/xml/accessory_filter.xml").read_text(encoding="utf-8")
        driver = (ROOT / "steamvr-driver/src/driver.cpp").read_text(encoding="utf-8")
        aoa_doc = (ROOT / "docs/usb-aoa-windows-driver.md").read_text(encoding="utf-8")
        release_workflow = (ROOT / ".github/workflows/release.yml").read_text(encoding="utf-8")
        self.assertRegex(version, r"^\d+\.\d+\.\d+$")
        self.assertIn(f'versionName = "{version}"', gradle)
        self.assertRegex(gradle, r"versionCode\s*=\s*\d+")
        self.assertIn(f"PhoneVRStream-{version}", network)
        self.assertIn(f'var appVersion = "{version}"', host)
        self.assertIn(f'"PhoneVR USB streaming", "{version}"', aoa)
        self.assertIn(f'version="{version}"', accessory_filter)
        self.assertIn(f"VERSION {version} LANGUAGES CXX", cmake)
        self.assertIn(f"PhoneVR {version} DisplayRedirect", driver)
        self.assertIn(f"PhoneVR {version} provides a direct USB transport", aoa_doc)
        self.assertIn(f"`VSQ Studio` / `PhoneVR` / `{version}`", aoa_doc)
        self.assertRegex(release_workflow, r"(?m)^\s+git add -A$")
        self.assertIn('git push --force origin "refs/tags/${tag}"', release_workflow)


if __name__ == "__main__":
    unittest.main()
