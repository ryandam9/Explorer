import 'package:flutter/material.dart';

/// Animated green pulse that indicates the microphone is live (session active
/// and not muted), echoing the audio-activity dot in the original tool.
class AudioIndicator extends StatefulWidget {
  const AudioIndicator({super.key, required this.active});

  final bool active;

  @override
  State<AudioIndicator> createState() => _AudioIndicatorState();
}

class _AudioIndicatorState extends State<AudioIndicator>
    with SingleTickerProviderStateMixin {
  late final AnimationController _controller = AnimationController(
    vsync: this,
    duration: const Duration(milliseconds: 900),
  );

  @override
  void initState() {
    super.initState();
    _sync();
  }

  @override
  void didUpdateWidget(covariant AudioIndicator oldWidget) {
    super.didUpdateWidget(oldWidget);
    _sync();
  }

  void _sync() {
    if (widget.active) {
      _controller.repeat(reverse: true);
    } else {
      _controller.stop();
      _controller.value = 0;
    }
  }

  @override
  void dispose() {
    _controller.dispose();
    super.dispose();
  }

  @override
  Widget build(BuildContext context) {
    final Color color =
        widget.active ? Colors.green : Theme.of(context).disabledColor;
    return Row(
      mainAxisSize: MainAxisSize.min,
      children: <Widget>[
        AnimatedBuilder(
          animation: _controller,
          builder: (BuildContext context, _) {
            final double scale = widget.active ? 1 + _controller.value * 0.6 : 1;
            return Container(
              width: 16,
              height: 16,
              transform: Matrix4.identity()..scale(scale),
              transformAlignment: Alignment.center,
              decoration: BoxDecoration(
                color: color,
                shape: BoxShape.circle,
                boxShadow: widget.active
                    ? <BoxShadow>[
                        BoxShadow(
                          color: color.withOpacity(0.5),
                          blurRadius: 8 * _controller.value,
                          spreadRadius: 2 * _controller.value,
                        ),
                      ]
                    : null,
              ),
            );
          },
        ),
        const SizedBox(width: 8),
        Text(widget.active ? 'Listening' : 'Idle'),
      ],
    );
  }
}
