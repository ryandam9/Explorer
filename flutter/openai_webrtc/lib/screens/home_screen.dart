import 'package:flutter/material.dart';

import '../models/pricing.dart';
import '../services/realtime_session.dart';
import '../services/settings_store.dart';
import '../widgets/audio_indicator.dart';
import '../widgets/event_log_view.dart';
import '../widgets/stats_panel.dart';

class HomeScreen extends StatefulWidget {
  const HomeScreen({super.key});

  @override
  State<HomeScreen> createState() => _HomeScreenState();
}

class _HomeScreenState extends State<HomeScreen> {
  final SettingsStore _settings = SettingsStore();
  final RealtimeSession _session = RealtimeSession();

  final TextEditingController _tokenController = TextEditingController();
  final TextEditingController _documentController = TextEditingController();

  String _model = kRealtimeModels.first;
  String _voice = kVoices.first;
  bool _obscureToken = true;
  bool _loaded = false;

  @override
  void initState() {
    super.initState();
    _session.addListener(_onSessionChanged);
    _restore();
  }

  Future<void> _restore() async {
    final String key = await _settings.loadApiKey();
    final String model = await _settings.loadModel();
    final String voice = await _settings.loadVoice();
    if (!mounted) return;
    setState(() {
      _tokenController.text = key;
      _model = model;
      _voice = voice;
      _loaded = true;
    });
  }

  void _onSessionChanged() {
    if (mounted) setState(() {});
  }

  @override
  void dispose() {
    _session.removeListener(_onSessionChanged);
    _session.dispose();
    _tokenController.dispose();
    _documentController.dispose();
    super.dispose();
  }

  Future<void> _toggleSession() async {
    if (_session.isActive) {
      await _session.stop();
      return;
    }
    await _settings.saveApiKey(_tokenController.text.trim());
    await _settings.saveModel(_model);
    await _settings.saveVoice(_voice);
    await _session.start(
      token: _tokenController.text.trim(),
      model: _model,
      voice: _voice,
      document: _documentController.text,
    );
  }

  @override
  Widget build(BuildContext context) {
    if (!_loaded) {
      return const Scaffold(body: Center(child: CircularProgressIndicator()));
    }

    final bool active = _session.isActive;
    final bool connecting = _session.status == SessionStatus.connecting;

    return Scaffold(
      appBar: AppBar(
        title: const Text('OpenAI WebRTC Voice'),
        actions: <Widget>[
          Padding(
            padding: const EdgeInsets.symmetric(horizontal: 16),
            child: Center(
              child: AudioIndicator(active: active && !_session.micMuted),
            ),
          ),
        ],
      ),
      body: LayoutBuilder(
        builder: (BuildContext context, BoxConstraints constraints) {
          final bool wide = constraints.maxWidth >= 720;
          final Widget controls = _buildControls(active, connecting);
          final Widget activity = _buildActivity();
          if (wide) {
            return Row(
              crossAxisAlignment: CrossAxisAlignment.stretch,
              children: <Widget>[
                Expanded(child: SingleChildScrollView(child: controls)),
                const VerticalDivider(width: 1),
                Expanded(child: activity),
              ],
            );
          }
          return SingleChildScrollView(
            child: Column(
              children: <Widget>[
                controls,
                SizedBox(height: 360, child: activity),
              ],
            ),
          );
        },
      ),
    );
  }

  Widget _buildControls(bool active, bool connecting) {
    return Padding(
      padding: const EdgeInsets.all(16),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.stretch,
        children: <Widget>[
          TextField(
            controller: _tokenController,
            obscureText: _obscureToken,
            enabled: !active,
            decoration: InputDecoration(
              labelText: 'OpenAI API key',
              border: const OutlineInputBorder(),
              suffixIcon: IconButton(
                icon: Icon(
                  _obscureToken ? Icons.visibility : Icons.visibility_off,
                ),
                onPressed: () =>
                    setState(() => _obscureToken = !_obscureToken),
              ),
            ),
          ),
          const SizedBox(height: 12),
          Row(
            children: <Widget>[
              Expanded(
                child: DropdownButtonFormField<String>(
                  value: _model,
                  decoration: const InputDecoration(
                    labelText: 'Model',
                    border: OutlineInputBorder(),
                  ),
                  items: kRealtimeModels
                      .map((String m) => DropdownMenuItem<String>(
                            value: m,
                            child: Text(m),
                          ))
                      .toList(),
                  onChanged:
                      active ? null : (String? v) => setState(() => _model = v!),
                ),
              ),
              const SizedBox(width: 12),
              Expanded(
                child: DropdownButtonFormField<String>(
                  value: _voice,
                  decoration: const InputDecoration(
                    labelText: 'Voice',
                    border: OutlineInputBorder(),
                  ),
                  items: kVoices
                      .map((String v) => DropdownMenuItem<String>(
                            value: v,
                            child: Text(_capitalise(v)),
                          ))
                      .toList(),
                  onChanged:
                      active ? null : (String? v) => setState(() => _voice = v!),
                ),
              ),
            ],
          ),
          const SizedBox(height: 12),
          TextField(
            controller: _documentController,
            enabled: !active,
            maxLines: 5,
            decoration: const InputDecoration(
              labelText: 'Document context (optional)',
              hintText:
                  'Paste a document here to discuss it with the model.',
              border: OutlineInputBorder(),
              alignLabelWithHint: true,
            ),
          ),
          const SizedBox(height: 16),
          Row(
            children: <Widget>[
              Expanded(
                child: FilledButton.icon(
                  onPressed: connecting ? null : _toggleSession,
                  icon: Icon(active ? Icons.stop : Icons.play_arrow),
                  label: Text(
                    connecting
                        ? 'Connecting…'
                        : active
                            ? 'Stop Session'
                            : 'Start Session',
                  ),
                ),
              ),
              const SizedBox(width: 12),
              Expanded(
                child: OutlinedButton.icon(
                  onPressed: active ? _session.toggleMute : null,
                  icon: Icon(_session.micMuted ? Icons.mic_off : Icons.mic),
                  label: Text(_session.micMuted ? 'Unmute Mic' : 'Mute Mic'),
                ),
              ),
            ],
          ),
          if (_session.errorMessage != null) ...<Widget>[
            const SizedBox(height: 12),
            Container(
              padding: const EdgeInsets.all(12),
              decoration: BoxDecoration(
                color: Theme.of(context).colorScheme.errorContainer,
                borderRadius: BorderRadius.circular(8),
              ),
              child: Text(
                _session.errorMessage!,
                style: TextStyle(
                  color: Theme.of(context).colorScheme.onErrorContainer,
                ),
              ),
            ),
          ],
          const SizedBox(height: 16),
          Row(
            crossAxisAlignment: CrossAxisAlignment.start,
            children: <Widget>[
              Expanded(
                child: StatsPanel(
                  title: 'This interaction',
                  usage: _session.lastUsage,
                ),
              ),
              const SizedBox(width: 12),
              Expanded(
                child: StatsPanel(
                  title: 'Session total',
                  usage: _session.sessionTotal,
                ),
              ),
            ],
          ),
          const SizedBox(height: 8),
          Text(
            'Costs are estimates and may not exactly match OpenAI billing.',
            style: Theme.of(context).textTheme.bodySmall,
          ),
        ],
      ),
    );
  }

  Widget _buildActivity() {
    return Column(
      crossAxisAlignment: CrossAxisAlignment.stretch,
      children: <Widget>[
        Padding(
          padding: const EdgeInsets.fromLTRB(16, 12, 16, 4),
          child: Text('Event log',
              style: Theme.of(context).textTheme.titleMedium),
        ),
        Expanded(child: EventLogView(events: _session.log)),
      ],
    );
  }

  String _capitalise(String s) =>
      s.isEmpty ? s : '${s[0].toUpperCase()}${s.substring(1)}';
}
