import 'package:shared_preferences/shared_preferences.dart';

import '../models/pricing.dart';

/// Persists the user's API key, model and voice between launches, mirroring the
/// `localStorage` behaviour of the original web tool.
class SettingsStore {
  static const String _keyApiKey = 'openai_api_key';
  static const String _keyModel = 'openai_model';
  static const String _keyVoice = 'openai_voice';

  Future<String> loadApiKey() async {
    final SharedPreferences prefs = await SharedPreferences.getInstance();
    return prefs.getString(_keyApiKey) ?? '';
  }

  Future<void> saveApiKey(String value) async {
    final SharedPreferences prefs = await SharedPreferences.getInstance();
    await prefs.setString(_keyApiKey, value);
  }

  Future<String> loadModel() async {
    final SharedPreferences prefs = await SharedPreferences.getInstance();
    final String? stored = prefs.getString(_keyModel);
    return (stored != null && kRealtimeModels.contains(stored))
        ? stored
        : kRealtimeModels.first;
  }

  Future<void> saveModel(String value) async {
    final SharedPreferences prefs = await SharedPreferences.getInstance();
    await prefs.setString(_keyModel, value);
  }

  Future<String> loadVoice() async {
    final SharedPreferences prefs = await SharedPreferences.getInstance();
    final String? stored = prefs.getString(_keyVoice);
    return (stored != null && kVoices.contains(stored)) ? stored : kVoices.first;
  }

  Future<void> saveVoice(String value) async {
    final SharedPreferences prefs = await SharedPreferences.getInstance();
    await prefs.setString(_keyVoice, value);
  }
}
