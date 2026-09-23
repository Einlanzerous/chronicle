/// Board 1a, screens 01 · IDLE and 02 · RECORDING.
///
/// Three rules here are not cosmetic, and each one is a way this screen could
/// otherwise lie:
///
/// * **RECORDING appears only when the microphone is really open.** Not when
///   `start()` returned — Android silences a recorder that has lost the mic
///   rather than refusing it, and says nothing. A screen claiming to record
///   over a stream of zeros is the same class of lie as a queue showing a memo
///   as sent, which CHRN-61 names as the thing it must never do.
/// * **`LOCAL BUFFER` is the real file size**, read from disk, never elapsed ×
///   bitrate. It is the one number here that proves the invariant to the person
///   holding the phone, and an estimate would keep climbing after the disk
///   stopped accepting writes.
/// * **A silent stretch is drawn as a fault**, not as a flat line.
///
/// Everything on screen comes from the capture service's own state rather than
/// from this widget's, which is what lets a recreated UI re-attach to a
/// recording already in flight instead of painting idle over the top of one.
library;

import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../../capture/capture_channel.dart';
import '../../capture/capture_controller.dart';
import '../../capture/capture_record.dart';
import '../../queue/queue_controller.dart';
import '../../queue/queue_label.dart';
import '../../router/router.dart';
import '../../theme/theme.dart';
import '../../theme/tokens.dart';
import '../shared/bottom_tabs.dart';

/// How many amplitude samples the waveform keeps.
const _waveSamples = 48;

/// A run of exact zeros this long is a fault, not a quiet passage.
const _silenceFaultMs = 3000;

class CaptureScreen extends ConsumerStatefulWidget {
  const CaptureScreen({super.key});

  @override
  ConsumerState<CaptureScreen> createState() => _CaptureScreenState();
}

class _CaptureScreenState extends ConsumerState<CaptureScreen> {
  final List<int> _wave = [];
  int _silentMs = 0;
  int _lastElapsedMs = 0;

  @override
  Widget build(BuildContext context) {
    final state = ref.watch(captureControllerProvider);
    ref.listen(captureControllerProvider, (previous, next) {
      if (next.recorder.state == RecorderState.recording) {
        // The gap is taken from the recorder's own clock rather than assumed
        // from a tick length, so a change in the service's cadence cannot make
        // this quietly over- or under-count.
        final delta = (next.recorder.elapsedMs - _lastElapsedMs).clamp(0, 5000);
        setState(() {
          _lastElapsedMs = next.recorder.elapsedMs;
          _wave.add(next.recorder.amplitude);
          if (_wave.length > _waveSamples) _wave.removeAt(0);
          _silentMs = next.recorder.amplitude == 0 ? _silentMs + delta : 0;
        });
      } else if (_wave.isNotEmpty) {
        setState(() {
          _wave.clear();
          _silentMs = 0;
          _lastElapsedMs = 0;
        });
      }
    });

    return Scaffold(
      body: SafeArea(
        child: Column(
          children: [
            Expanded(
              child: Padding(
                padding: const EdgeInsets.all(space4),
                child: state.isRecording ? _recording(state) : _idle(state),
              ),
            ),
            // Chrome, not content: a recording is a focused, full-screen
            // state with nothing to navigate away to mid-memo, matching
            // board 1a's own screen 02, which reserves no space for this
            // row at all.
            if (!state.isRecording) const BottomCaptureQueueTabs(current: captureRoute),
          ],
        ),
      ),
    );
  }

  // ── 01 · IDLE ──────────────────────────────────────────────────────────────

  Widget _idle(CaptureUiState state) => Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          Text('CHRONICLE', style: microLabel(color: chSignal, size: sizeSm)),
          const Spacer(),
          Center(
            child: Column(
              children: [
                _control(state),
                const SizedBox(height: space4),
                Text('TAP TO TALK', style: microLabel(color: chText, size: sizeSm)),
                const SizedBox(height: space1),
                Text('HOLD FOR PUSH-TO-TALK', style: microLabel(size: sizeXxs)),
              ],
            ),
          ),
          if (state.refusal != null) ...[
            const SizedBox(height: space4),
            _Refusal(message: state.refusal!),
          ],
          const Spacer(),
          Text('RECENT', style: microLabel()),
          const SizedBox(height: space2),
          Expanded(
            flex: 2,
            child: state.recent.isEmpty
                ? Text('Nothing captured yet.',
                    style: const TextStyle(fontSize: sizeBody, color: chText2))
                : ListView(
                    children: [
                      for (final record in state.recent.take(8))
                        _RecentRow(record: record),
                    ],
                  ),
          ),
        ],
      );

  // ── 02 · RECORDING ─────────────────────────────────────────────────────────

  Widget _recording(CaptureUiState state) {
    final recorder = state.recorder;
    final paused = recorder.state == RecorderState.paused;
    final silentFault = _silentMs >= _silenceFaultMs && !paused;

    return Column(
      crossAxisAlignment: CrossAxisAlignment.start,
      children: [
        Row(
          children: [
            Container(
              width: space1,
              height: space1,
              decoration: BoxDecoration(
                // Steel is tier 1's colour and the reserved pair belongs to
                // Switchyard and Amber, so a live recorder gets the resolved
                // green and a fault gets plain meta text.
                color: silentFault || !recorder.micOpen ? chTextMeta : chResolved,
                shape: BoxShape.circle,
              ),
            ),
            const SizedBox(width: space2),
            Text(
              paused
                  ? 'PAUSED'
                  : recorder.micOpen
                      ? 'RECORDING'
                      : 'OPENING THE MICROPHONE',
              style: microLabel(color: chText, size: sizeSm),
            ),
          ],
        ),
        const SizedBox(height: space1),
        Text('LOCAL BUFFER · ${_bytes(recorder.byteSize)}',
            style: monoMeta(size: sizeXs)),
        const Spacer(),
        Center(
          child: Text(
            _clock(recorder.elapsedMs),
            style: const TextStyle(
              fontFamily: fontMono,
              fontSize: 44,
              color: chText,
            ),
          ),
        ),
        const SizedBox(height: space4),
        SizedBox(height: 64, child: _Waveform(samples: _wave, fault: silentFault)),
        if (silentFault) ...[
          const SizedBox(height: space2),
          Text(
            recorder.silenced
                ? 'Something else has taken the microphone.'
                : recorder.hasConfig
                    ? 'No sound is reaching the recorder.'
                    : 'The recorder is not capturing.',
            style: const TextStyle(fontSize: sizeBody, color: chText2),
          ),
        ],
        const Spacer(),
        Row(
          mainAxisAlignment: MainAxisAlignment.spaceEvenly,
          children: [
            _Action(
              label: paused ? 'RESUME' : 'PAUSE',
              onTap: () => paused
                  ? ref.read(captureControllerProvider.notifier).resume()
                  : ref.read(captureControllerProvider.notifier).pause(),
            ),
            _Action(
              label: 'STOP',
              emphasis: true,
              onTap: () => ref.read(captureControllerProvider.notifier).stop(),
            ),
          ],
        ),
        const SizedBox(height: space4),
      ],
    );
  }

  // ── the one control ────────────────────────────────────────────────────────

  Widget _control(CaptureUiState state) {
    final controller = ref.read(captureControllerProvider.notifier);
    return Listener(
      onPointerDown: (_) {
        if (!state.isRecording) controller.beginHold();
      },
      onPointerUp: (_) {
        // Always reported. The controller decides what a release means,
        // because only it knows whether a start is still in flight.
        state.latched && state.isRecording
            ? controller.stop()
            : controller.endHold();
      },
      // A cancel is NOT a release. The system cancels a pointer for reasons
      // that have nothing to do with the finger -- a system gesture, palm
      // rejection, the shade coming down, a call ringing -- and treating those
      // as "stop" would end a memo mid-thought.
      onPointerCancel: (_) => controller.cancelHold(),
      child: Container(
        width: 148,
        height: 148,
        decoration: BoxDecoration(
          color: chRaised,
          shape: BoxShape.circle,
          border: Border.all(color: chSignal, width: 2),
        ),
        child: const Icon(Icons.mic_none, size: 56, color: chSignal),
      ),
    );
  }

  static String _clock(int ms) {
    final total = ms ~/ 1000;
    final m = (total ~/ 60).toString().padLeft(2, '0');
    final s = (total % 60).toString().padLeft(2, '0');
    return '$m:$s';
  }

  static String _bytes(int b) => b < 1024 * 1024
      ? '${(b / 1024).round()} KB'
      : '${(b / (1024 * 1024)).toStringAsFixed(1)} MB';
}

class _Waveform extends StatelessWidget {
  const _Waveform({required this.samples, required this.fault});

  final List<int> samples;
  final bool fault;

  @override
  Widget build(BuildContext context) {
    if (samples.isEmpty) {
      return Center(child: Text('—', style: monoMeta()));
    }
    final peak = samples.fold<int>(1, (a, b) => b > a ? b : a);
    return Row(
      crossAxisAlignment: CrossAxisAlignment.center,
      children: [
        for (final s in samples)
          Expanded(
            child: Padding(
              padding: const EdgeInsets.symmetric(horizontal: 1),
              child: Container(
                height: (s / peak * 60).clamp(2.0, 60.0),
                color: fault ? chTextMeta : chSignal,
              ),
            ),
          ),
      ],
    );
  }
}

class _Action extends StatelessWidget {
  const _Action({required this.label, required this.onTap, this.emphasis = false});

  final String label;
  final VoidCallback onTap;
  final bool emphasis;

  @override
  Widget build(BuildContext context) => InkWell(
        onTap: onTap,
        child: Container(
          constraints: const BoxConstraints(
            minWidth: 116,
            minHeight: minTapTarget,
          ),
          alignment: Alignment.center,
          decoration: BoxDecoration(
            color: emphasis ? chSignal : chRaised,
            border: Border.all(color: emphasis ? chSignal : chLine),
            borderRadius: BorderRadius.circular(space1),
          ),
          child: Text(
            label,
            style: microLabel(color: emphasis ? chBase : chText, size: sizeSm),
          ),
        ),
      );
}

class _Refusal extends StatelessWidget {
  const _Refusal({required this.message});

  final String message;

  @override
  Widget build(BuildContext context) => Container(
        padding: const EdgeInsets.all(space3),
        decoration: BoxDecoration(
          color: chRaised,
          border: Border.all(color: chLine),
          borderRadius: BorderRadius.circular(space1),
        ),
        child: Text(message,
            style: const TextStyle(fontSize: sizeBody, color: chText)),
      );
}

class _RecentRow extends ConsumerWidget {
  const _RecentRow({required this.record});

  final CaptureRecord record;

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final at = record.startedAt.toLocal();
    final clock = '${at.hour.toString().padLeft(2, '0')}:'
        '${at.minute.toString().padLeft(2, '0')}';

    // CHRN-61's own chip, alongside CHRN-60's capture-state one. A capture
    // not yet sendable (still `recording`, or `empty`) has nothing the
    // queue can say about it, so it wears only its capture chip.
    String? queueChip;
    if (record.state == CaptureState.ready || record.state == CaptureState.salvaged) {
      final queue = ref.watch(queueControllerProvider);
      queueChip = queueLabel(
        captureId: record.captureId,
        record: queue.records[record.captureId],
        allRecords: queue.records,
        deviceBlock: queue.deviceBlock,
        sendingCaptureId: queue.sending,
        retention: record.retention,
        enqueuedAt: queue.records[record.captureId]?.enqueuedAt ?? record.startedAt,
        now: DateTime.now(),
      );
    }

    return Padding(
      padding: const EdgeInsets.only(bottom: space2),
      child: Row(
        children: [
          SizedBox(width: 44, child: Text(clock, style: monoMeta(size: sizeXs))),
          const SizedBox(width: space2),
          Expanded(
            child: Text(
              record.durationMs != null
                  ? _duration(record.durationMs!)
                  : 'nothing recovered',
              style: const TextStyle(fontSize: sizeBody, color: chText),
            ),
          ),
          if (_chip(record) != null) ...[
            Text(_chip(record)!, style: microLabel(size: sizeXxs)),
            const SizedBox(width: space1),
          ],
          if (queueChip != null)
            Text(
              queueChip,
              style: microLabel(
                color: queueChip == 'SENT' ? chResolved : chTextMeta,
                size: sizeXxs,
              ),
            ),
          if (record.state == CaptureState.empty) ...[
            const SizedBox(width: space1),
            _DismissAction(captureId: record.captureId),
          ],
        ],
      ),
    );
  }

  /// What this capture's chip says, or nothing.
  ///
  /// `SALVAGED` is reserved for a capture a trim actually shortened. It used to
  /// fire on anything recovered, which on this platform is the ordinary outcome
  /// of a crash — so it fired constantly and meant nothing when it fired on the
  /// real thing. `RECOVERED` carries the softer fact: the app did not see this
  /// one stop. A capture the app watched stop wears no chip at all.
  static String? _chip(CaptureRecord record) => switch (record.state) {
        CaptureState.salvaged => 'SALVAGED',
        CaptureState.empty => 'EMPTY',
        CaptureState.recording => null,
        CaptureState.ready =>
          record.recoveredAt != null ? 'RECOVERED' : null,
      };

  static String _duration(int ms) {
    final total = ms ~/ 1000;
    return total >= 60 ? '${total ~/ 60}m ${total % 60}s' : '${total}s';
  }
}

/// Hides an `empty` capture -- the only capture state that ever wears this.
/// Deletes nothing; see `CaptureController.dismiss`.
class _DismissAction extends ConsumerWidget {
  const _DismissAction({required this.captureId});

  final String captureId;

  @override
  Widget build(BuildContext context, WidgetRef ref) => InkWell(
        onTap: () => ref.read(captureControllerProvider.notifier).dismiss(captureId),
        child: Padding(
          padding: const EdgeInsets.symmetric(vertical: space1),
          child: Text('DISMISS', style: microLabel(color: chSignal, size: sizeXxs)),
        ),
      );
}
