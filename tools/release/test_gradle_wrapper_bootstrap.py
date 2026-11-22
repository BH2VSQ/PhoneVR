from pathlib import Path
import unittest

ROOT = Path(__file__).resolve().parents[2]
ANDROID = ROOT / "android"


class GradleWrapperBootstrapTests(unittest.TestCase):
    def test_bootstrap_source_verifies_distribution_sha256(self):
        source = (ANDROID / "tools/wrapper-bootstrap/src/org/gradle/wrapper/GradleWrapperMain.java").read_text(encoding="utf-8")
        self.assertIn('getProperty("distributionSha256Sum"', source)
        self.assertIn("MessageDigest.getInstance(\"SHA-256\")", source)
        self.assertIn("Gradle distribution SHA-256 verification failed", source)

    def test_unix_gradlew_generates_missing_launcher_jar(self):
        script = (ANDROID / "gradlew").read_text(encoding="utf-8")
        self.assertIn('if [ ! -f "$WRAPPER_JAR" ]', script)
        self.assertIn('tools/wrapper-bootstrap/build-wrapper.sh', script)

    def test_windows_gradlew_generates_missing_launcher_jar(self):
        script = (ANDROID / "gradlew.bat").read_text(encoding="utf-8")
        self.assertIn('if not exist "%WRAPPER_JAR%"', script)
        self.assertIn('tools\\wrapper-bootstrap\\build-wrapper.bat', script)

    def test_both_bootstrap_builders_compile_the_repository_owned_entrypoint(self):
        source = ANDROID / "tools/wrapper-bootstrap/src/org/gradle/wrapper/GradleWrapperMain.java"
        unix_builder = (ANDROID / "tools/wrapper-bootstrap/build-wrapper.sh").read_text(encoding="utf-8")
        windows_builder = (ANDROID / "tools/wrapper-bootstrap/build-wrapper.bat").read_text(encoding="utf-8")
        self.assertTrue(source.is_file())
        self.assertIn("GradleWrapperMain.java", unix_builder)
        self.assertIn("GradleWrapperMain.java", windows_builder)
        self.assertIn("org/gradle/wrapper/GradleWrapperMain.class", unix_builder)

    def test_generated_wrapper_jar_is_ignored(self):
        gitignore = (ROOT / ".gitignore").read_text(encoding="utf-8")
        self.assertIn("android/gradle/wrapper/gradle-wrapper.jar", gitignore)


if __name__ == "__main__":
    unittest.main()
