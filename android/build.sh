#!/usr/bin/env bash
# Builds the Sonidex receiver APK with the plain Android SDK tools (no Gradle):
#   aapt2 (resources + manifest) -> javac -> d8 (dex) -> zipalign -> apksigner
# Usage: android/build.sh [output.apk]      (needs ANDROID_HOME and a JDK 17)
# Signs with a throwaway debug key unless KEYSTORE/KS_PASS are provided.
set -euo pipefail
cd "$(dirname "$0")"

SDK="${ANDROID_HOME:-${ANDROID_SDK_ROOT:-}}"
[ -d "$SDK" ] || { echo "set ANDROID_HOME to the Android SDK directory" >&2; exit 1; }
API="${ANDROID_API:-34}"
JAR="$SDK/platforms/android-$API/android.jar"
[ -f "$JAR" ] || { echo "missing $JAR (sdkmanager \"platforms;android-$API\")" >&2; exit 1; }
BT="$(ls -d "$SDK"/build-tools/* | sort -V | tail -n1)"
OUT="$(realpath -m "${1:-Sonidex.apk}")"
MIN_SDK=23   # AudioTrack.Builder

rm -rf build && mkdir -p build/classes
"$BT/aapt2" compile --dir res -o build/res.zip
"$BT/aapt2" link -o build/base.apk -I "$JAR" --manifest AndroidManifest.xml \
  --min-sdk-version "$MIN_SDK" --target-sdk-version "$API" \
  --version-code "${VERSION_CODE:-1}" --version-name "${VERSION_NAME:-1.0}" build/res.zip
javac -encoding UTF-8 --release 8 -classpath "$JAR" -d build/classes $(find src -name '*.java')
"$BT/d8" --release --min-api "$MIN_SDK" --lib "$JAR" --output build $(find build/classes -name '*.class')
(cd build && zip -q -j base.apk classes.dex)
"$BT/zipalign" -f -p 4 build/base.apk build/aligned.apk

KS="${KEYSTORE:-build/debug.keystore}"
KS_PASS="${KS_PASS:-android}"
if [ ! -f "$KS" ]; then
  keytool -genkeypair -keystore "$KS" -storepass "$KS_PASS" -keypass "$KS_PASS" \
    -alias androiddebugkey -keyalg RSA -keysize 2048 -validity 10000 \
    -dname "CN=Android Debug,O=Android,C=US" >/dev/null 2>&1
fi
"$BT/apksigner" sign --ks "$KS" --ks-pass "pass:$KS_PASS" --key-pass "pass:$KS_PASS" --out "$OUT" build/aligned.apk
echo "built $OUT"
