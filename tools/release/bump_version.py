#!/usr/bin/env python3
"""Bump PhoneVRStream's canonical version and synchronized app metadata."""
from __future__ import annotations

import argparse
import re
from pathlib import Path

SEMVER = re.compile(r"^(0|[1-9]\d*)\.(0|[1-9]\d*)\.(0|[1-9]\d*)$")


def next_version(current: str, bump: str) -> str:
    match = SEMVER.fullmatch(current.strip())
    if not match:
        raise ValueError(f"VERSION must contain a plain semantic version, got: {current!r}")
    major, minor, patch = map(int, match.groups())
    if bump == "major":
        return f"{major + 1}.0.0"
    if bump == "minor":
        return f"{major}.{minor + 1}.0"
    if bump == "patch":
        return f"{major}.{minor}.{patch + 1}"
    raise ValueError(f"Unsupported bump type: {bump}")


def replace_one(path: Path, pattern: str, replacement: str, flags: int = 0) -> None:
    text = path.read_text(encoding="utf-8")
    updated, count = re.subn(pattern, replacement, text, count=1, flags=flags)
    if count != 1:
        raise RuntimeError(f"Expected exactly one version field in {path}, pattern={pattern!r}")
    path.write_text(updated, encoding="utf-8", newline="\n")


def bump_repository(root: Path, bump: str) -> tuple[str, int]:
    version_file = root / "VERSION"
    current = version_file.read_text(encoding="ascii").strip()
    version = next_version(current, bump)

    gradle = root / "android/app/build.gradle.kts"
    gradle_text = gradle.read_text(encoding="utf-8")
    code_match = re.search(r"versionCode\s*=\s*(\d+)", gradle_text)
    if not code_match:
        raise RuntimeError("Could not find Android versionCode in app/build.gradle.kts")
    version_code = int(code_match.group(1)) + 1
    gradle_text, code_count = re.subn(r"versionCode\s*=\s*\d+", f"versionCode = {version_code}", gradle_text, count=1)
    gradle_text, name_count = re.subn(r'versionName\s*=\s*"[^"]+"', f'versionName = "{version}"', gradle_text, count=1)
    if code_count != 1 or name_count != 1:
        raise RuntimeError("Could not update Android versionCode/versionName")
    gradle.write_text(gradle_text, encoding="utf-8", newline="\n")

    replace_one(
        root / "android/app/src/main/java/com/vsq/phonvrstream/network/NetworkClient.kt",
        r'private const val CLIENT_VERSION = "PhoneVRStream-[^"]+"',
        f'private const val CLIENT_VERSION = "PhoneVRStream-{version}"',
    )
    replace_one(
        root / "windows-host/cmd/phonevr-host/main.go",
        r'var appVersion = "[^"]+"',
        f'var appVersion = "{version}"',
    )
    replace_one(
        root / "README.md",
        r'Repository revision: \*\*[^*]+\*\*\. Windows Host: \*\*[^*]+\*\*\. Android client: \*\*[^*]+\*\*\.',
        f'Repository revision: **{version}**. Windows Host: **{version}**. Android client: **{version}**.',
    )
    replace_one(
        root / "README.md",
        r"## Windows Host [0-9]+\.[0-9]+\.[0-9]+[^\n]*",
        f"## Windows Host {version} bundled SteamVR driver",
    )
    replace_one(
        root / "README.md",
        r"## Android client [0-9]+\.[0-9]+\.[0-9]+",
        f"## Android client {version}",
    )
    replace_one(
        root / "windows-host/README.md",
        r"# PhoneVR Host [0-9]+\.[0-9]+\.[0-9]+",
        f"# PhoneVR Host {version}",
    )
    replace_one(
        root / "steamvr-driver/CMakeLists.txt",
        r"project\(PhoneVRSteamVRDriver VERSION [0-9]+\.[0-9]+\.[0-9]+ LANGUAGES CXX\)",
        f"project(PhoneVRSteamVRDriver VERSION {version} LANGUAGES CXX)",
    )
    replace_one(
        root / "steamvr-driver/README.md",
        r"# PhoneVR SteamVR Driver — [0-9]+\.[0-9]+\.[0-9]+",
        f"# PhoneVR SteamVR Driver — {version}",
    )
    replace_one(
        root / "README.md",
        r"## SteamVR driver [0-9]+\.[0-9]+\.[0-9]+",
        f"## SteamVR driver {version}",
    )

    version_file.write_text(version + "\n", encoding="ascii")

    changelog = root / "CHANGELOG.md"
    old_log = changelog.read_text(encoding="utf-8")
    changelog.write_text(
        f"## {version} - Automated release\n\n"
        "- Android versionName and versionCode bumped by the manual release workflow.\n"
        "- Windows Host version metadata updated from the repository VERSION file.\n\n"
        + old_log,
        encoding="utf-8",
        newline="\n",
    )
    return version, version_code


def main() -> None:
    parser = argparse.ArgumentParser()
    parser.add_argument("bump", choices=("patch", "minor", "major"))
    parser.add_argument("--root", type=Path, default=Path(__file__).resolve().parents[2])
    args = parser.parse_args()
    version, version_code = bump_repository(args.root.resolve(), args.bump)
    print(f"{version}\t{version_code}")


if __name__ == "__main__":
    main()
