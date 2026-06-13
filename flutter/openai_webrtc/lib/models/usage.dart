import 'pricing.dart';

/// Token counts and the dollar costs derived from a single `response.done`
/// event, plus helpers for accumulating a running session total.
class UsageCost {
  const UsageCost({
    this.inputTokens = 0,
    this.outputTokens = 0,
    this.totalTokens = 0,
    this.inputCost = 0,
    this.outputCost = 0,
  });

  final int inputTokens;
  final int outputTokens;
  final int totalTokens;
  final double inputCost;
  final double outputCost;

  double get totalCost => inputCost + outputCost;

  /// Sum two usage records, used to maintain the session-wide total.
  UsageCost operator +(UsageCost other) => UsageCost(
        inputTokens: inputTokens + other.inputTokens,
        outputTokens: outputTokens + other.outputTokens,
        totalTokens: totalTokens + other.totalTokens,
        inputCost: inputCost + other.inputCost,
        outputCost: outputCost + other.outputCost,
      );

  /// Parse the `usage` object from a `response.done` event and compute costs
  /// using the pricing for [model].
  ///
  /// The structure mirrors the OpenAI Realtime API:
  /// ```json
  /// "usage": {
  ///   "input_tokens": 0,
  ///   "output_tokens": 0,
  ///   "total_tokens": 0,
  ///   "input_token_details": {
  ///     "audio_tokens": 0,
  ///     "text_tokens": 0,
  ///     "cached_tokens": 0,
  ///     "cached_tokens_details": { "audio_tokens": 0, "text_tokens": 0 }
  ///   },
  ///   "output_token_details": { "audio_tokens": 0, "text_tokens": 0 }
  /// }
  /// ```
  factory UsageCost.fromUsageJson(
    Map<String, dynamic> usage,
    String model,
  ) {
    final ModelPricing p = pricingFor(model);

    final Map<String, dynamic> inputDetails =
        _asMap(usage['input_token_details']);
    final Map<String, dynamic> outputDetails =
        _asMap(usage['output_token_details']);
    final Map<String, dynamic> cachedDetails =
        _asMap(inputDetails['cached_tokens_details']);

    final int cachedAudio = _asInt(cachedDetails['audio_tokens']);
    final int cachedText = _asInt(cachedDetails['text_tokens']);

    final int inputAudio = _asInt(inputDetails['audio_tokens']);
    final int inputText = _asInt(inputDetails['text_tokens']);

    // Cached tokens are billed at the (much cheaper) cached rate, so the
    // full-price portion is whatever is left after removing cached tokens.
    final int nonCachedAudioInput = (inputAudio - cachedAudio).clamp(0, 1 << 62);
    final int nonCachedTextInput = (inputText - cachedText).clamp(0, 1 << 62);

    final double inputCost = nonCachedAudioInput * p.audioInput +
        nonCachedTextInput * p.textInput +
        cachedAudio * p.cachedAudioInput +
        cachedText * p.cachedTextInput;

    final int outputAudio = _asInt(outputDetails['audio_tokens']);
    final int outputText = _asInt(outputDetails['text_tokens']);
    final double outputCost =
        outputAudio * p.audioOutput + outputText * p.textOutput;

    return UsageCost(
      inputTokens: _asInt(usage['input_tokens']),
      outputTokens: _asInt(usage['output_tokens']),
      totalTokens: _asInt(usage['total_tokens']),
      inputCost: inputCost,
      outputCost: outputCost,
    );
  }

  static Map<String, dynamic> _asMap(Object? value) =>
      value is Map<String, dynamic> ? value : const <String, dynamic>{};

  static int _asInt(Object? value) =>
      value is num ? value.toInt() : 0;
}
