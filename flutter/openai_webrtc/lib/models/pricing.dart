/// Per-token pricing for the OpenAI Realtime models, mirroring the rates used
/// in Simon Willison's `openai-webrtc.html` tool.
///
/// All values are dollars **per single token**. Costs are computed by
/// multiplying these rates against the token counts reported in the
/// `response.done` event's `usage` object.
class ModelPricing {
  const ModelPricing({
    required this.audioInput,
    required this.cachedAudioInput,
    required this.textInput,
    required this.cachedTextInput,
    required this.audioOutput,
    required this.textOutput,
  });

  final double audioInput;
  final double cachedAudioInput;
  final double textInput;
  final double cachedTextInput;
  final double audioOutput;
  final double textOutput;
}

/// The selectable realtime models. The first entry is the default.
const List<String> kRealtimeModels = <String>[
  'gpt-realtime-2',
  'gpt-realtime-1.5',
  'gpt-realtime-mini',
  'gpt-realtime',
];

/// The selectable voices. Values are the lowercase identifiers expected by the
/// API; the labels shown in the UI are derived by capitalising them.
const List<String> kVoices = <String>[
  'ash',
  'ballad',
  'coral',
  'sage',
  'verse',
];

/// Pricing for the "standard" realtime tier shared by gpt-realtime-2,
/// gpt-realtime-1.5 and gpt-realtime.
const ModelPricing _standardPricing = ModelPricing(
  audioInput: 0.000032,
  cachedAudioInput: 0.0000004,
  textInput: 0.000004,
  cachedTextInput: 0.0000004,
  audioOutput: 0.000064,
  textOutput: 0.000016,
);

const Map<String, ModelPricing> kPricingByModel = <String, ModelPricing>{
  'gpt-realtime-2': _standardPricing,
  'gpt-realtime-1.5': _standardPricing,
  'gpt-realtime': _standardPricing,
  'gpt-realtime-mini': ModelPricing(
    audioInput: 0.00001,
    cachedAudioInput: 0.0000003,
    textInput: 0.0000006,
    cachedTextInput: 0.00000006,
    audioOutput: 0.000024,
    textOutput: 0.0000024,
  ),
};

ModelPricing pricingFor(String model) =>
    kPricingByModel[model] ?? _standardPricing;
