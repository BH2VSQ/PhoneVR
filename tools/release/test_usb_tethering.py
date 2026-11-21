from pathlib import Path
import re
import unittest

ROOT = Path(__file__).resolve().parents[2]


class UsbTetheringTests(unittest.TestCase):
    def test_d3d11_multithread_header_is_included(self):
        source = (ROOT / "steamvr-driver/src/driver.cpp").read_text(encoding="utf-8")
        self.assertRegex(source, r"#include\s+<d3d11_4\.h>")
        self.assertIn("ID3D11Multithread", source)

    def test_android_discovery_binds_each_interface_and_uses_directed_broadcast(self):
        source = (ROOT / "android/app/src/main/java/com/vsq/phonvrstream/network/NetworkClient.kt").read_text(encoding="utf-8")
        self.assertIn("bind(InetSocketAddress(target.localAddress, 0))", source)
        self.assertIn("target.broadcastAddress", source)
        self.assertIn("enumerateDiscoveryTargets(mode)", source)
        self.assertIn("ConnectionInterfaceClassifier.accepts(mode, type)", source)

    def test_android_exposes_auto_wifi_and_usb_modes(self):
        models = (ROOT / "android/app/src/main/java/com/vsq/phonvrstream/network/HeadPose.kt").read_text(encoding="utf-8")
        activity = (ROOT / "android/app/src/main/java/com/vsq/phonvrstream/MainActivity.kt").read_text(encoding="utf-8")
        self.assertIn('AUTO("auto")', models)
        self.assertIn('WIFI("wifi")', models)
        self.assertIn('USB("usb")', models)
        self.assertIn("USB 有线（RNDIS）", activity)

    def test_host_listens_on_all_ipv4_interfaces_and_logs_interface_kind(self):
        service = (ROOT / "windows-host/cmd/phonevr-host/service.go").read_text(encoding="utf-8")
        self.assertIn('net.JoinHostPort(config.ListenAddress', service)
        self.assertIn('"USB/RNDIS"', service)
        self.assertIn('Host interface [%s] %s: %s', service)

    def test_firewall_helper_only_opens_required_inbound_ports(self):
        helper = (ROOT / "tools/enable-usb-tethering-firewall.ps1").read_text(encoding="utf-8")
        self.assertIn("48100", helper)
        self.assertIn("48101", helper)
        self.assertIn("48102", helper)
        self.assertIn("LocalSubnet", helper)
        self.assertNotIn("48104';", helper)  # Video is outbound from Host.

    def test_version_metadata_is_synchronized(self):
        version = (ROOT / "VERSION").read_text(encoding="ascii").strip()
        gradle = (ROOT / "android/app/build.gradle.kts").read_text(encoding="utf-8")
        network = (ROOT / "android/app/src/main/java/com/vsq/phonvrstream/network/NetworkClient.kt").read_text(encoding="utf-8")
        cmake = (ROOT / "steamvr-driver/CMakeLists.txt").read_text(encoding="utf-8")
        self.assertEqual(version, "0.4.19")
        self.assertIn(f'versionName = "{version}"', gradle)
        self.assertIn(f"PhoneVRStream-{version}", network)
        self.assertIn(f"VERSION {version} LANGUAGES CXX", cmake)


if __name__ == "__main__":
    unittest.main()
