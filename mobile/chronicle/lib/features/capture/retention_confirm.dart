/// CHRN-62: the retention choice, made at the moment of decision.
///
/// Drawn from board 1a's screen 06 block `THE AUDIO` -- the title line, its
/// meta caption, and the three chips `DISCARD NOW` · `30 DAYS` · `FOREVER`
/// with 30 DAYS selected. The canvas shows that block after routing; it sits
/// here instead, straight after recording, because by the time a memo is
/// routed the server already holds its declaration and can only ever raise
/// retention, never lower it. DISCARD NOW is only honest before the first
/// send.
///
/// **30 DAYS is the fast path and the other two are not the same tap**
/// (the ticket's words): a chip only selects, and CONFIRM commits whatever
/// is selected. Keeping 30 days is one tap; discarding or pinning is two,
/// and never a single mis-tap.
///
/// SKIP is the "seen, and chose not to choose" marker: the capture goes at
/// once with no opinion, which the server reads as its 30-day default.
/// Leaving the card without either leaves the capture undecided, and the
/// queue holds it for the retention grace (`retention_gate.dart`).
library;

import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../../capture/capture_controller.dart';
import '../../capture/capture_record.dart';
import '../../capture/retention_choice.dart';
import '../../theme/theme.dart';
import '../../theme/tokens.dart';

class RetentionConfirm extends ConsumerStatefulWidget {
  const RetentionConfirm({super.key, required this.capture, this.onSettled});

  final CaptureRecord capture;

  /// Called once a choice or a skip has been written -- or refused, because
  /// the moment to choose had already passed. The card has nothing left to
  /// offer either way.
  final VoidCallback? onSettled;

  @override
  ConsumerState<RetentionConfirm> createState() => _RetentionConfirmState();
}

class _RetentionConfirmState extends ConsumerState<RetentionConfirm> {
  RetentionChoice _selected = RetentionChoice.days30;
  bool _busy = false;

  @override
  void didUpdateWidget(RetentionConfirm old) {
    super.didUpdateWidget(old);
    // A different capture is a different decision: never carry a selection
    // made for one memo over to the next.
    if (old.capture.captureId != widget.capture.captureId) {
      _selected = RetentionChoice.days30;
      _busy = false;
    }
  }

  Future<void> _settle(Future<bool> Function() write) async {
    if (_busy) return;
    setState(() => _busy = true);
    await write();
    if (!mounted) return;
    setState(() => _busy = false);
    widget.onSettled?.call();
  }

  @override
  Widget build(BuildContext context) {
    final controller = ref.read(captureControllerProvider.notifier);
    final id = widget.capture.captureId;

    return Container(
      key: const ValueKey('retention-confirm'),
      padding: const EdgeInsets.all(space3),
      decoration: BoxDecoration(
        color: chRaised,
        border: Border.all(color: chLine),
        borderRadius: BorderRadius.circular(space1),
      ),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        mainAxisSize: MainAxisSize.min,
        children: [
          Row(
            children: [
              Text('THE AUDIO', style: microLabel()),
              const Spacer(),
              Text(_facts(widget.capture), style: monoMeta(size: sizeXs)),
            ],
          ),
          const SizedBox(height: space2),
          Text(
            _selected.title,
            style: const TextStyle(fontSize: sizeMd, color: chText),
          ),
          const SizedBox(height: space1 / 2),
          Text(_selected.detail, style: monoMeta(size: sizeXs, color: chTextMeta)),
          const SizedBox(height: space2),
          Row(
            children: [
              for (final choice in RetentionChoice.values) ...[
                if (choice != RetentionChoice.values.first) const SizedBox(width: space1),
                Expanded(
                  child: _Chip(
                    choice: choice,
                    selected: choice == _selected,
                    onTap: _busy ? null : () => setState(() => _selected = choice),
                  ),
                ),
              ],
            ],
          ),
          const SizedBox(height: space2),
          Row(
            children: [
              InkWell(
                onTap: _busy ? null : () => _settle(() => controller.skipRetention(id)),
                child: Container(
                  constraints: const BoxConstraints(minHeight: minTapTarget, minWidth: 72),
                  alignment: Alignment.center,
                  child: Text('SKIP', style: microLabel(color: chText2, size: sizeSm)),
                ),
              ),
              const SizedBox(width: space2),
              Expanded(
                child: InkWell(
                  onTap: _busy
                      ? null
                      : () => _settle(() => controller.chooseRetention(id, _selected)),
                  child: Container(
                    constraints: const BoxConstraints(minHeight: minTapTarget),
                    alignment: Alignment.center,
                    decoration: BoxDecoration(
                      color: chSignal,
                      borderRadius: BorderRadius.circular(space1),
                    ),
                    child: Text(
                      'CONFIRM · ${_selected.chip}',
                      style: microLabel(color: chBase, size: sizeSm),
                    ),
                  ),
                ),
              ),
            ],
          ),
          const SizedBox(height: space2),
          // The rule, stated outright where the choice is made -- the epic's
          // own words.
          Text('TRANSCRIPT IS THE DURABLE ARTEFACT', style: microLabel(size: sizeXxs)),
          Text('AUDIO PRUNES AT 30 DAYS', style: microLabel(size: sizeXxs, color: chText2)),
        ],
      ),
    );
  }

  static String _facts(CaptureRecord c) {
    final parts = <String>[];
    final ms = c.durationMs;
    if (ms != null) {
      final total = ms ~/ 1000;
      parts.add('${total ~/ 60}:${(total % 60).toString().padLeft(2, '0')}');
    }
    final bytes = c.byteSize;
    if (bytes != null) {
      parts.add(bytes < 1024 * 1024
          ? '${(bytes / 1024).round()} KB'
          : '${(bytes / (1024 * 1024)).toStringAsFixed(1)} MB');
    }
    return parts.join(' · ');
  }
}

class _Chip extends StatelessWidget {
  const _Chip({required this.choice, required this.selected, required this.onTap});

  final RetentionChoice choice;
  final bool selected;
  final VoidCallback? onTap;

  @override
  Widget build(BuildContext context) => Semantics(
        selected: selected,
        button: true,
        child: InkWell(
          onTap: onTap,
          child: Container(
            constraints: const BoxConstraints(minHeight: minTapTarget),
            alignment: Alignment.center,
            decoration: BoxDecoration(
              color: selected ? chSignal.withValues(alpha: 0.10) : null,
              border: Border.all(
                color: selected ? chSignal.withValues(alpha: 0.40) : chLine,
              ),
            ),
            child: Text(
              choice.chip,
              style: microLabel(color: selected ? chText : chTextMeta, size: sizeXs),
            ),
          ),
        ),
      );
}
