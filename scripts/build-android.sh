#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd -- "${SCRIPT_DIR}/.." && pwd)"
ANDROID_ROOT="${REPO_ROOT}/android"
OUTPUT_ROOT="${REPO_ROOT}/dist/android"
CONFIGURATION="${1:-both}"
SKIP_TESTS="false"

if [[ "${2:-}" == "--skip-tests" ]]; then
  SKIP_TESTS="true"
fi

CONFIGURATION="$(printf '%s' "${CONFIGURATION}" | tr '[:upper:]' '[:lower:]')"
case "${CONFIGURATION}" in
  debug) CONFIGURATION="debug" ;;
  release) CONFIGURATION="release" ;;
  both) CONFIGURATION="both" ;;
  *)
    echo "Usage: $0 [debug|release|both] [--skip-tests]" >&2
    exit 2
    ;;
esac

if [[ ! -f "${ANDROID_ROOT}/gradlew" ]]; then
  echo "Android Gradle Wrapper not found: ${ANDROID_ROOT}/gradlew" >&2
  exit 1
fi

SDK_ROOT="${ANDROID_HOME:-${ANDROID_SDK_ROOT:-}}"
if [[ -z "${SDK_ROOT}" ]]; then
  echo "ANDROID_HOME or ANDROID_SDK_ROOT must point to the Android SDK." >&2
  exit 1
fi
if [[ ! -f "${SDK_ROOT}/platforms/android-36/android.jar" ]]; then
  echo "Android SDK Platform 36 is missing from ${SDK_ROOT}." >&2
  exit 1
fi
if [[ ! -d "${SDK_ROOT}/build-tools/36.0.0" ]]; then
  echo "Android SDK Build-Tools 36.0.0 is missing from ${SDK_ROOT}." >&2
  exit 1
fi

chmod +x "${ANDROID_ROOT}/gradlew"
tasks=()
if [[ "${SKIP_TESTS}" != "true" ]]; then
  tasks+=(":app:testDebugUnitTest")
fi
if [[ "${CONFIGURATION}" == "debug" || "${CONFIGURATION}" == "both" ]]; then
  tasks+=(":app:assembleDebug")
fi
if [[ "${CONFIGURATION}" == "release" || "${CONFIGURATION}" == "both" ]]; then
  tasks+=(":app:assembleRelease")
fi

printf '\n==> Android Gradle tasks: %s\n' "${tasks[*]}"
(
  cd "${ANDROID_ROOT}"
  ./gradlew --no-daemon --stacktrace "${tasks[@]}"
)

mkdir -p "${OUTPUT_ROOT}"
artifacts=()

if [[ "${CONFIGURATION}" == "debug" || "${CONFIGURATION}" == "both" ]]; then
  debug_apk="${ANDROID_ROOT}/app/build/outputs/apk/debug/app-debug.apk"
  [[ -f "${debug_apk}" ]] || { echo "Debug APK was not produced: ${debug_apk}" >&2; exit 1; }
  destination="${OUTPUT_ROOT}/PhoneVRStream-Android-debug.apk"
  cp -f "${debug_apk}" "${destination}"
  artifacts+=("${destination}")
fi

if [[ "${CONFIGURATION}" == "release" || "${CONFIGURATION}" == "both" ]]; then
  release_dir="${ANDROID_ROOT}/app/build/outputs/apk/release"
  release_apk="$(find "${release_dir}" -maxdepth 1 -type f -name '*.apk' -print -quit 2>/dev/null || true)"
  [[ -n "${release_apk}" && -f "${release_apk}" ]] || { echo "Release APK was not produced in: ${release_dir}" >&2; exit 1; }
  destination="${OUTPUT_ROOT}/PhoneVRStream-Android-release-unsigned.apk"
  cp -f "${release_apk}" "${destination}"
  artifacts+=("${destination}")
fi

printf '\n==> Android build completed\n'
for artifact in "${artifacts[@]}"; do
  if command -v sha256sum >/dev/null 2>&1; then
    sha256sum "${artifact}"
  else
    shasum -a 256 "${artifact}"
  fi
done
