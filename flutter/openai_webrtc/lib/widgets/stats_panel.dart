import 'package:flutter/material.dart';

import '../models/usage.dart';

/// One of the two side-by-side cost boxes: "This interaction" and
/// "Session total".
class StatsPanel extends StatelessWidget {
  const StatsPanel({super.key, required this.title, required this.usage});

  final String title;
  final UsageCost? usage;

  @override
  Widget build(BuildContext context) {
    final UsageCost u = usage ?? const UsageCost();
    final TextTheme text = Theme.of(context).textTheme;

    return Card(
      child: Padding(
        padding: const EdgeInsets.all(12),
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.start,
          mainAxisSize: MainAxisSize.min,
          children: <Widget>[
            Text(title, style: text.titleSmall),
            const Divider(height: 16),
            _row(context, 'Input tokens', '${u.inputTokens}'),
            _row(context, 'Output tokens', '${u.outputTokens}'),
            _row(context, 'Total tokens', '${u.totalTokens}'),
            const SizedBox(height: 8),
            _row(context, 'Input cost', _usd(u.inputCost)),
            _row(context, 'Output cost', _usd(u.outputCost)),
            _row(
              context,
              'Total cost',
              _usd(u.totalCost),
              emphasise: true,
            ),
          ],
        ),
      ),
    );
  }

  Widget _row(BuildContext context, String label, String value,
      {bool emphasise = false}) {
    final TextStyle? style = emphasise
        ? Theme.of(context)
            .textTheme
            .bodyMedium
            ?.copyWith(fontWeight: FontWeight.bold)
        : Theme.of(context).textTheme.bodyMedium;
    return Padding(
      padding: const EdgeInsets.symmetric(vertical: 2),
      child: Row(
        mainAxisAlignment: MainAxisAlignment.spaceBetween,
        children: <Widget>[
          Text(label, style: style),
          Text(value, style: style),
        ],
      ),
    );
  }

  String _usd(double value) => '\$${value.toStringAsFixed(6)}';
}
