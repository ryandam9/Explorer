import 'dart:async';
import 'dart:convert';
import 'dart:io' show Platform;

import 'package:flutter/foundation.dart';
import 'package:flutter_webrtc/flutter_webrtc.dart';
import 'package:http/http.dart' as http;
import 'package:permission_handler/permission_handler.dart';

import '../models/log_event.dart';
import '../models/usage.dart';

enum SessionStatus { idle, connecting, connected, error }

/// Drives a real-time voice session against the OpenAI Realtime API over
/// WebRTC, following the same flow as `openai-webrtc.html`:
///
///  1. Capture the microphone.
///  2. Create an `RTCPeerConnection`, add the mic track, open the
///     `oai-events` data channel and create an SDP offer.
///  3. POST the offer (plus a small session config) to
///     `https://api.openai.com/v1/realtime/calls`.
///  4. Apply the returned SDP answer as the remote description.
///  5. Stream events over the data channel; parse `response.done` usage for
///     token/cost accounting.
class RealtimeSession extends ChangeNotifier {
  static const String _callsEndpoint =
      'https://api.openai.com/v1/realtime/calls';

  SessionStatus _status = SessionStatus.idle;
  SessionStatus get status => _status;

  bool _micMuted = false;
  bool get micMuted => _micMuted;

  String? _errorMessage;
  String? get errorMessage => _errorMessage;

  UsageCost? _lastUsage;
  UsageCost? get lastUsage => _lastUsage;

  UsageCost _sessionTotal = const UsageCost();
  UsageCost get sessionTotal => _sessionTotal;

  final List<LogEvent> _log = <LogEvent>[];
  List<LogEvent> get log => List<LogEvent>.unmodifiable(_log);

  String _model = '';
  String get model => _model;

  RTCPeerConnection? _pc;
  RTCDataChannel? _dataChannel;
  MediaStream? _localStream;
  MediaStream? _remoteStream;

  bool get isActive =>
      _status == SessionStatus.connecting || _status == SessionStatus.connected;

  void _log_(String message, {String? detail}) {
    _log.insert(0, LogEvent(message, detail: detail));
    if (_log.length > 200) {
      _log.removeLast();
    }
    notifyListeners();
  }

  void _setStatus(SessionStatus status) {
    _status = status;
    notifyListeners();
  }

  /// Start a session. [document], when non-empty, is injected as an initial
  /// user message once the data channel opens.
  Future<void> start({
    required String token,
    required String model,
    required String voice,
    String document = '',
  }) async {
    if (isActive) return;
    if (token.trim().isEmpty) {
      _errorMessage = 'An OpenAI API key is required.';
      _setStatus(SessionStatus.error);
      return;
    }

    _model = model;
    _errorMessage = null;
    _lastUsage = null;
    _sessionTotal = const UsageCost();
    _micMuted = false;
    _setStatus(SessionStatus.connecting);
    _log_('Starting session (model: $model, voice: $voice)');

    try {
      await _ensureMicPermission();

      _localStream = await navigator.mediaDevices.getUserMedia(<String, dynamic>{
        'audio': true,
        'video': false,
      });
      _log_('Microphone acquired');

      final RTCPeerConnection pc = await createPeerConnection(<String, dynamic>{
        'sdpSemantics': 'unified-plan',
      });
      _pc = pc;

      // Remote model audio. On native platforms flutter_webrtc renders the
      // incoming audio track automatically; we just hold a reference.
      pc.onTrack = (RTCTrackEvent event) {
        if (event.streams.isNotEmpty) {
          _remoteStream = event.streams.first;
          _log_('Receiving model audio track');
        }
      };

      pc.onConnectionState = (RTCPeerConnectionState state) {
        _log_('Peer connection: ${state.name}');
        if (state ==
                RTCPeerConnectionState.RTCPeerConnectionStateDisconnected ||
            state == RTCPeerConnectionState.RTCPeerConnectionStateFailed ||
            state == RTCPeerConnectionState.RTCPeerConnectionStateClosed) {
          if (_status == SessionStatus.connected) {
            _setStatus(SessionStatus.idle);
          }
        }
      };

      for (final MediaStreamTrack track in _localStream!.getAudioTracks()) {
        await pc.addTrack(track, _localStream!);
      }

      final RTCDataChannel dc = await pc.createDataChannel(
        'oai-events',
        RTCDataChannelInit(),
      );
      _dataChannel = dc;
      dc.onDataChannelState = (RTCDataChannelState state) {
        if (state == RTCDataChannelState.RTCDataChannelOpen) {
          _log_('Data channel open');
          if (document.trim().isNotEmpty) {
            _sendDocumentContext(document.trim());
          }
        }
      };
      dc.onMessage = (RTCDataChannelMessage message) {
        _handleEvent(message.text);
      };

      final RTCSessionDescription offer = await pc.createOffer();
      await pc.setLocalDescription(offer);
      _log_('Created SDP offer');

      final String answerSdp = await _exchangeSdp(
        token: token,
        model: model,
        voice: voice,
        offerSdp: offer.sdp!,
      );

      await pc.setRemoteDescription(
        RTCSessionDescription(answerSdp, 'answer'),
      );
      _log_('Applied SDP answer');
      _setStatus(SessionStatus.connected);
    } catch (e) {
      _errorMessage = e.toString();
      _log_('Error: $e');
      _setStatus(SessionStatus.error);
      await _teardown();
    }
  }

  /// Send the SDP offer and session config to OpenAI and return the SDP answer.
  Future<String> _exchangeSdp({
    required String token,
    required String model,
    required String voice,
    required String offerSdp,
  }) async {
    final http.MultipartRequest request = http.MultipartRequest(
      'POST',
      Uri.parse(_callsEndpoint),
    );
    request.headers['Authorization'] = 'Bearer $token';
    request.fields['sdp'] = offerSdp;
    request.fields['session'] = jsonEncode(<String, dynamic>{
      'type': 'realtime',
      'model': model,
      'audio': <String, dynamic>{
        'output': <String, dynamic>{'voice': voice},
      },
    });

    _log_('POST $_callsEndpoint');
    final http.StreamedResponse streamed = await request.send();
    final String body = await streamed.stream.bytesToString();

    if (streamed.statusCode < 200 || streamed.statusCode >= 300) {
      throw Exception(
        'OpenAI returned ${streamed.statusCode}: $body',
      );
    }
    return body;
  }

  void _sendDocumentContext(String document) {
    final Map<String, dynamic> payload = <String, dynamic>{
      'type': 'conversation.item.create',
      'item': <String, dynamic>{
        'type': 'message',
        'role': 'user',
        'content': <Map<String, dynamic>>[
          <String, dynamic>{
            'type': 'input_text',
            'text': 'The user has provided the following document. They want '
                'to have a conversation about it. Refer to it when answering '
                'their questions.\n\n<document>\n$document\n</document>',
          },
        ],
      },
    };
    _dataChannel?.send(RTCDataChannelMessage(jsonEncode(payload)));
    _log_('Injected document context (${document.length} chars)');
  }

  void _handleEvent(String raw) {
    Map<String, dynamic> event;
    try {
      event = jsonDecode(raw) as Map<String, dynamic>;
    } catch (_) {
      _log_('Event (unparseable)', detail: raw);
      return;
    }

    final String type = (event['type'] ?? 'unknown').toString();

    if (type == 'response.done') {
      final Map<String, dynamic>? response =
          event['response'] is Map<String, dynamic>
              ? event['response'] as Map<String, dynamic>
              : null;
      final Object? usage = response?['usage'];
      if (usage is Map<String, dynamic>) {
        final UsageCost cost = UsageCost.fromUsageJson(usage, _model);
        _lastUsage = cost;
        _sessionTotal = _sessionTotal + cost;
      }
      _log_('response.done',
          detail: const JsonEncoder.withIndent('  ').convert(event));
    } else {
      _log_(type);
    }
    notifyListeners();
  }

  /// Toggle the local microphone track on/off without tearing down the
  /// connection (matches the "Mute Mic" button).
  void toggleMute() {
    final List<MediaStreamTrack> tracks =
        _localStream?.getAudioTracks() ?? <MediaStreamTrack>[];
    if (tracks.isEmpty) return;
    _micMuted = !_micMuted;
    for (final MediaStreamTrack track in tracks) {
      track.enabled = !_micMuted;
    }
    _log_(_micMuted ? 'Microphone muted' : 'Microphone unmuted');
  }

  Future<void> stop() async {
    _log_('Stopping session');
    await _teardown();
    _setStatus(SessionStatus.idle);
  }

  Future<void> _teardown() async {
    try {
      await _dataChannel?.close();
    } catch (_) {}
    _dataChannel = null;

    for (final MediaStreamTrack track
        in _localStream?.getTracks() ?? <MediaStreamTrack>[]) {
      await track.stop();
    }
    await _localStream?.dispose();
    _localStream = null;

    await _remoteStream?.dispose();
    _remoteStream = null;

    try {
      await _pc?.close();
    } catch (_) {}
    _pc = null;
    _micMuted = false;
  }

  Future<void> _ensureMicPermission() async {
    // Only Android and iOS gate the microphone behind a runtime prompt that
    // permission_handler implements. On macOS the OS prompts automatically on
    // first capture (driven by the entitlement / usage string), and Linux has
    // no such plugin support, so we skip the explicit request there.
    if (!Platform.isAndroid && !Platform.isIOS) return;

    final PermissionStatus status = await Permission.microphone.request();
    if (status.isPermanentlyDenied) {
      throw Exception(
        'Microphone permission permanently denied. Enable it in settings.',
      );
    }
    if (!status.isGranted && !status.isLimited) {
      throw Exception('Microphone permission was not granted.');
    }
  }

  @override
  void dispose() {
    _teardown();
    super.dispose();
  }
}
