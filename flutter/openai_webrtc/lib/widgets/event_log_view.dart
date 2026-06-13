import 'package:flutter/material.dart';

import '../models/log_event.dart';

/// Chronological (newest-first) display of WebRTC / API events.
class EventLogView extends StatelessWidget {
  const EventLogView({super.key, required this.events});

  final List<LogEvent> events;

  @override
  Widget build(BuildContext context) {
    if (events.isEmpty) {
      return const Center(child: Text('No events yet.'));
    }
    return ListView.separated(
      itemCount: events.length,
      separatorBuilder: (_, __) => const Divider(height: 1),
      itemBuilder: (BuildContext context, int index) {
        final LogEvent e = events[index];
        final String time = e.isoTimestamp.substring(11, 23); // HH:MM:SS.mmm
        return ExpansionTile(
          dense: true,
          tilePadding: const EdgeInsets.symmetric(horizontal: 12),
          childrenPadding: const EdgeInsets.fromLTRB(12, 0, 12, 12),
          title: Text(
            e.message,
            style: const TextStyle(fontFamily: 'monospace', fontSize: 13),
          ),
          subtitle: Text(time, style: const TextStyle(fontSize: 11)),
          trailing: e.detail == null ? const SizedBox.shrink() : null,
          children: e.detail == null
              ? const <Widget>[]
              : <Widget>[
                  SelectableText(
                    e.detail!,
                    style:
                        const TextStyle(fontFamily: 'monospace', fontSize: 11),
                  ),
                ],
        );
      },
    );
  }
}
