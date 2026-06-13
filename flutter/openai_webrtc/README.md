# OpenAI WebRTC Voice (Flutter)

A cross-platform Flutter port of Simon Willison's
[`openai-webrtc.html`](https://github.com/simonw/tools/blob/main/openai-webrtc.html)
tool: real-time, spoken conversations with the OpenAI Realtime models over a
WebRTC peer connection.

Targets **Android, iOS, Linux and macOS**. (The same code also builds for
Windows and web, since `flutter_webrtc` supports them, but those are not part
of the requested scope.)

## What it does

- Captures your microphone and streams it to an OpenAI Realtime model over
  WebRTC.
- Plays the model's spoken response back through your speakers.
- Lets you pick the **model** (`gpt-realtime-2`, `gpt-realtime-1.5`,
  `gpt-realtime-mini`, `gpt-realtime`) and the **voice** (Ash, Ballad, Coral,
  Sage, Verse).
- Optionally injects a **document** as initial context to talk about.
- Shows a live **event log** and **token/cost accounting** for the current
  interaction and the running session total.
- Persists your API key, model and voice locally (via `shared_preferences`,
  the equivalent of the original's `localStorage`).

> ⚠️ Your OpenAI API key is sent directly from the device to
> `api.openai.com`. It is stored locally on the device only. Costs shown are
> estimates and may not exactly match OpenAI billing.

## How it works (connection flow)

This mirrors the original tool exactly. See
[`lib/services/realtime_session.dart`](lib/services/realtime_session.dart):

1. `getUserMedia({audio: true})` to capture the mic.
2. Create an `RTCPeerConnection`, add the mic track, and open a data channel
   named **`oai-events`**.
3. `createOffer()` → `setLocalDescription(offer)`.
4. `POST https://api.openai.com/v1/realtime/calls` as `multipart/form-data`
   with:
   - `sdp` = the offer SDP
   - `session` = `{"type":"realtime","model":<model>,"audio":{"output":{"voice":<voice>}}}`
   - header `Authorization: Bearer <api-key>`
5. The response body **is** the answer SDP → `setRemoteDescription({type:
   'answer', sdp})`.
6. When the data channel opens and a document was provided, send a
   `conversation.item.create` user message containing it.
7. Parse `response.done` events for `usage` and compute costs from the
   per-token pricing table in [`lib/models/pricing.dart`](lib/models/pricing.dart).

## Project layout

```
lib/
  main.dart                      App entry / theme
  models/
    pricing.dart                 Models, voices, per-token pricing table
    usage.dart                   Token-usage parsing + cost calculation
    log_event.dart               Event-log entry
  services/
    realtime_session.dart        WebRTC + OpenAI Realtime session controller
    settings_store.dart          Persisted API key / model / voice
  screens/
    home_screen.dart             Main UI
  widgets/
    audio_indicator.dart         Animated "listening" pulse
    stats_panel.dart             Cost/token panel
    event_log_view.dart          Event log list
test/
  usage_test.dart                Unit tests for cost calculation
```

## Getting started

You need the [Flutter SDK](https://docs.flutter.dev/get-started/install)
(3.19+). This repository contains the application code (`lib/`, `pubspec.yaml`,
`test/`) but not the generated native folders (`android/`, `ios/`, `linux/`,
`macos/`). Generate them and apply the required permissions with the included
script:

```bash
cd flutter/openai_webrtc
./setup.sh          # runs `flutter create .` + `flutter pub get` + patches permissions
```

Then run on a target:

```bash
flutter run -d macos
flutter run -d linux
flutter run -d android      # a connected device or emulator
flutter run -d <ios-device> # requires Xcode + signing on a Mac
```

Run the unit tests:

```bash
flutter test
```

### What `setup.sh` patches

If you prefer to set up the platforms by hand after `flutter create .`, apply
these edits:

**Android** — `android/app/src/main/AndroidManifest.xml`, inside `<manifest>`:

```xml
<uses-permission android:name="android.permission.RECORD_AUDIO"/>
<uses-permission android:name="android.permission.INTERNET"/>
<uses-permission android:name="android.permission.MODIFY_AUDIO_SETTINGS"/>
<uses-permission android:name="android.permission.BLUETOOTH"/>
```

Set `minSdkVersion 23` in `android/app/build.gradle(.kts)` (required by
`flutter_webrtc`).

**iOS** — `ios/Runner/Info.plist`:

```xml
<key>NSMicrophoneUsageDescription</key>
<string>This app uses the microphone for real-time voice conversations with OpenAI.</string>
```

**macOS** — `macos/Runner/Info.plist` (same `NSMicrophoneUsageDescription` as
above) and **both** entitlements files
(`macos/Runner/DebugProfile.entitlements`,
`macos/Runner/Release.entitlements`):

```xml
<key>com.apple.security.network.client</key>
<true/>
<key>com.apple.security.network.server</key>
<true/>
<key>com.apple.security.device.audio-input</key>
<true/>
```

**Linux** — no manifest changes needed. Building `flutter_webrtc` on Linux
requires native libraries; install them first, e.g. on Debian/Ubuntu:

```bash
sudo apt-get install -y \
  clang cmake ninja-build pkg-config libgtk-3-dev \
  libpulse-dev libasound2-dev
```

## Notes & differences from the original

- The web version uses a Web Audio `AnalyserNode` FFT to drive the audio
  meter. Native WebRTC tracks don't expose raw PCM the same way, so the
  microphone indicator here is a simple animated "listening" pulse that is
  active while the session is live and unmuted.
- Remote audio playback is handled automatically by the native WebRTC engine
  (`flutter_webrtc`); there is no HTML `<audio>` element.
- Everything else — endpoint, multipart body, session config, document
  injection prompt, pricing table and usage parsing — matches the original.
