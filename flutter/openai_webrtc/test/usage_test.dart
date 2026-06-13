import 'package:flutter_test/flutter_test.dart';
import 'package:openai_webrtc/models/usage.dart';

void main() {
  group('UsageCost.fromUsageJson', () {
    test('computes costs for gpt-realtime-2 with cached tokens', () {
      final Map<String, dynamic> usage = <String, dynamic>{
        'input_tokens': 1000,
        'output_tokens': 500,
        'total_tokens': 1500,
        'input_token_details': <String, dynamic>{
          'audio_tokens': 800,
          'text_tokens': 200,
          'cached_tokens': 300,
          'cached_tokens_details': <String, dynamic>{
            'audio_tokens': 250,
            'text_tokens': 50,
          },
        },
        'output_token_details': <String, dynamic>{
          'audio_tokens': 400,
          'text_tokens': 100,
        },
      };

      final UsageCost cost = UsageCost.fromUsageJson(usage, 'gpt-realtime-2');

      // Non-cached audio in: (800-250)=550 * 0.000032 = 0.0176
      // Non-cached text in:  (200-50)=150  * 0.000004 = 0.0006
      // Cached audio in:     250 * 0.0000004 = 0.0001
      // Cached text in:      50  * 0.0000004 = 0.00002
      expect(cost.inputCost, closeTo(0.01832, 1e-9));
      // Audio out: 400 * 0.000064 = 0.0256 ; Text out: 100 * 0.000016 = 0.0016
      expect(cost.outputCost, closeTo(0.0272, 1e-9));
      expect(cost.totalTokens, 1500);
    });

    test('accumulates a running session total', () {
      const UsageCost a = UsageCost(
        inputTokens: 10,
        outputTokens: 5,
        totalTokens: 15,
        inputCost: 0.1,
        outputCost: 0.2,
      );
      final UsageCost b = a + a;
      expect(b.totalTokens, 30);
      expect(b.totalCost, closeTo(0.6, 1e-9));
    });

    test('handles missing detail objects gracefully', () {
      final UsageCost cost =
          UsageCost.fromUsageJson(<String, dynamic>{}, 'gpt-realtime-mini');
      expect(cost.totalCost, 0);
      expect(cost.totalTokens, 0);
    });
  });
}
