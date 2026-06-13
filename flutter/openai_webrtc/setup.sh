#!/usr/bin/env bash
#
# Generates the native platform scaffolding for this project and patches in the
# microphone / network permissions required by flutter_webrtc.
#
# Run once, from this directory, after installing Flutter:
#
#     ./setup.sh
#
# It is safe to re-run: the permission patches are idempotent.

set -euo pipefail
cd "$(dirname "$0")"

echo "==> flutter create (android, ios, linux, macos)"
flutter create --platforms=android,ios,linux,macos --project-name openai_webrtc .

echo "==> flutter pub get"
flutter pub get

# --- Android: RECORD_AUDIO + INTERNET + min SDK ---------------------------
ANDROID_MANIFEST="android/app/src/main/AndroidManifest.xml"
if [[ -f "$ANDROID_MANIFEST" ]] && ! grep -q "RECORD_AUDIO" "$ANDROID_MANIFEST"; then
  echo "==> Patching $ANDROID_MANIFEST"
  # Insert permissions just after the opening <manifest ...> tag.
  python3 - "$ANDROID_MANIFEST" <<'PY'
import sys, re
path = sys.argv[1]
src = open(path).read()
perms = (
    '    <uses-permission android:name="android.permission.RECORD_AUDIO"/>\n'
    '    <uses-permission android:name="android.permission.INTERNET"/>\n'
    '    <uses-permission android:name="android.permission.MODIFY_AUDIO_SETTINGS"/>\n'
    '    <uses-permission android:name="android.permission.BLUETOOTH"/>\n'
)
src = re.sub(r'(<manifest[^>]*>\n)', r'\1' + perms, src, count=1)
open(path, 'w').write(src)
PY
fi

# flutter_webrtc requires minSdkVersion 23. Newer Flutter templates read this
# from flutter.minSdkVersion; override explicitly if a literal value is present.
GRADLE="android/app/build.gradle"
GRADLE_KTS="android/app/build.gradle.kts"
for f in "$GRADLE" "$GRADLE_KTS"; do
  if [[ -f "$f" ]]; then
    sed -i.bak -E 's/minSdkVersion[ =]+flutter\.minSdkVersion/minSdkVersion 23/' "$f" || true
    sed -i.bak -E 's/minSdk[ =]+flutter\.minSdkVersion/minSdk = 23/' "$f" || true
    rm -f "$f.bak"
  fi
done

# --- iOS: NSMicrophoneUsageDescription ------------------------------------
IOS_PLIST="ios/Runner/Info.plist"
if [[ -f "$IOS_PLIST" ]] && ! grep -q "NSMicrophoneUsageDescription" "$IOS_PLIST"; then
  echo "==> Patching $IOS_PLIST"
  /usr/libexec/PlistBuddy -c \
    "Add :NSMicrophoneUsageDescription string 'This app uses the microphone for real-time voice conversations with OpenAI.'" \
    "$IOS_PLIST" 2>/dev/null || \
  python3 - "$IOS_PLIST" <<'PY'
import sys, re
path = sys.argv[1]
src = open(path).read()
entry = ('\t<key>NSMicrophoneUsageDescription</key>\n'
         '\t<string>This app uses the microphone for real-time voice '
         'conversations with OpenAI.</string>\n')
src = src.replace('<dict>\n', '<dict>\n' + entry, 1)
open(path, 'w').write(src)
PY
fi

# --- macOS: mic usage string + sandbox entitlements -----------------------
MACOS_PLIST="macos/Runner/Info.plist"
if [[ -f "$MACOS_PLIST" ]] && ! grep -q "NSMicrophoneUsageDescription" "$MACOS_PLIST"; then
  echo "==> Patching $MACOS_PLIST"
  python3 - "$MACOS_PLIST" <<'PY'
import sys
path = sys.argv[1]
src = open(path).read()
entry = ('\t<key>NSMicrophoneUsageDescription</key>\n'
         '\t<string>This app uses the microphone for real-time voice '
         'conversations with OpenAI.</string>\n')
src = src.replace('<dict>\n', '<dict>\n' + entry, 1)
open(path, 'w').write(src)
PY
fi

for ENT in macos/Runner/DebugProfile.entitlements macos/Runner/Release.entitlements; do
  if [[ -f "$ENT" ]]; then
    echo "==> Patching $ENT"
    python3 - "$ENT" <<'PY'
import sys
path = sys.argv[1]
src = open(path).read()
adds = {
    'com.apple.security.network.client': '<true/>',
    'com.apple.security.network.server': '<true/>',
    'com.apple.security.device.audio-input': '<true/>',
}
inject = ''.join(
    f'\t<key>{k}</key>\n\t{v}\n' for k, v in adds.items() if k not in src
)
if inject:
    src = src.replace('<dict>\n', '<dict>\n' + inject, 1)
    open(path, 'w').write(src)
PY
  fi
done

echo "==> Done. Build with e.g.:"
echo "    flutter run -d macos       # or linux / android / <ios device>"
