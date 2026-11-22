#!/usr/bin/env sh
set -eu

SCRIPT_DIR=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
ANDROID_ROOT=$(CDPATH= cd -- "$SCRIPT_DIR/../.." && pwd)
WRAPPER_DIR="$ANDROID_ROOT/gradle/wrapper"
WRAPPER_JAR="$WRAPPER_DIR/gradle-wrapper.jar"
SOURCE="$SCRIPT_DIR/src/org/gradle/wrapper/GradleWrapperMain.java"

if [ -f "$WRAPPER_JAR" ]; then
  exit 0
fi

JAVAC_CMD="${JAVAC:-}"
JAR_CMD="${JAR:-}"
if [ -z "$JAVAC_CMD" ] && [ -n "${JAVA_HOME:-}" ] && [ -x "$JAVA_HOME/bin/javac" ]; then
  JAVAC_CMD="$JAVA_HOME/bin/javac"
fi
if [ -z "$JAR_CMD" ] && [ -n "${JAVA_HOME:-}" ] && [ -x "$JAVA_HOME/bin/jar" ]; then
  JAR_CMD="$JAVA_HOME/bin/jar"
fi
if [ -z "$JAVAC_CMD" ]; then JAVAC_CMD=$(command -v javac || true); fi
if [ -z "$JAR_CMD" ]; then JAR_CMD=$(command -v jar || true); fi

if [ -z "$JAVAC_CMD" ] || [ -z "$JAR_CMD" ]; then
  echo "Gradle bootstrap JAR is missing and a JDK was not found." >&2
  echo "Set JAVA_HOME to a JDK 8+ installation or add javac and jar to PATH." >&2
  exit 1
fi
if [ ! -f "$SOURCE" ]; then
  echo "Gradle bootstrap source is missing: $SOURCE" >&2
  exit 1
fi

BUILD_DIR=$(mktemp -d "${TMPDIR:-${TEMP:-/tmp}}/phonevr-gradle-wrapper.XXXXXX")
cleanup() { rm -rf "$BUILD_DIR"; }
trap cleanup EXIT HUP INT TERM
mkdir -p "$BUILD_DIR/classes" "$WRAPPER_DIR"
printf 'Manifest-Version: 1.0\nMain-Class: org.gradle.wrapper.GradleWrapperMain\n\n' > "$BUILD_DIR/MANIFEST.MF"
"$JAVAC_CMD" -source 8 -target 8 -Xlint:-options -encoding UTF-8 -d "$BUILD_DIR/classes" "$SOURCE"
"$JAR_CMD" cfm "$BUILD_DIR/gradle-wrapper.jar" "$BUILD_DIR/MANIFEST.MF" -C "$BUILD_DIR/classes" .
"$JAR_CMD" tf "$BUILD_DIR/gradle-wrapper.jar" | grep -Fx 'org/gradle/wrapper/GradleWrapperMain.class' >/dev/null
mv -f "$BUILD_DIR/gradle-wrapper.jar" "$WRAPPER_JAR"
echo "Generated PhoneVR Gradle bootstrap JAR: $WRAPPER_JAR"
