/// A single entry in the event log, shown newest-first in the UI.
class LogEvent {
  LogEvent(this.message, {this.detail}) : timestamp = DateTime.now();

  final DateTime timestamp;
  final String message;

  /// Optional pretty-printed payload (e.g. the JSON of a received event).
  final String? detail;

  /// ISO-8601 timestamp, matching the format used by the original tool.
  String get isoTimestamp => timestamp.toIso8601String();
}
