/// Board 1a, screen 03 -- the CHRN-61 slice of it.
///
/// The design's own mockup for this screen mixes in on-device transcription
/// (CHRN-81) and Scribe routing review (board 1a's screens 04/05) rows.
/// Neither exists yet, and this screen renders only what this ticket owns:
/// what is held on device, sending, or not sent and why. Nothing here ever
/// claims a memo was transcribed or routed -- those chips do not exist
/// until the tickets that own them build them.
library;

import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../../capture/capture_controller.dart';
import '../../capture/capture_record.dart';
import '../../queue/device_block.dart';
import '../../queue/queue_controller.dart';
import '../../queue/queue_label.dart';
import '../../queue/queue_record.dart';
import '../../router/router.dart';
import '../../theme/theme.dart';
import '../../theme/tokens.dart';
import '../shared/bottom_tabs.dart';

class QueueScreen extends ConsumerWidget {
  const QueueScreen({super.key});

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final captures = ref.watch(captureControllerProvider.select((s) => s.recent));
    final queue = ref.watch(queueControllerProvider);

    // Sendable material -- a live recording is not queue material
    // (`engine.dart`'s own precondition) and never appears here. `empty` is
    // not sendable either, but CHRN-61's approved plan lists it anyway (see
    // `_EmptyCapturesSection`) so the loss it represents is never silent.
    final sendable = captures
        .where((c) => c.state == CaptureState.ready || c.state == CaptureState.salvaged)
        .toList();
    final empty = captures.where((c) => c.state == CaptureState.empty).toList();

    final held = sendable.where((c) {
      final record = queue.records[c.captureId];
      return record == null || record.status == QueueStatus.pending;
    }).toList();
    final heldBytes = held.fold<int>(0, (sum, c) => sum + (c.byteSize ?? 0));

    return Scaffold(
      body: SafeArea(
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            Padding(
              padding: const EdgeInsets.fromLTRB(space4, space4, space4, 0),
              child: Text('QUEUE', style: microLabel(color: chSignal, size: sizeSm)),
            ),
            const SizedBox(height: space3),
            _Banner(
              deviceBlock: queue.deviceBlock,
              heldCount: held.length,
              heldBytes: heldBytes,
              onRetry: () => ref.read(queueControllerProvider.notifier).wake(),
            ),
            const SizedBox(height: space2),
            Expanded(
              child: sendable.isEmpty && empty.isEmpty
                  ? Center(
                      child: Text(
                        'Nothing captured yet.',
                        style: const TextStyle(fontSize: sizeBody, color: chText2),
                      ),
                    )
                  : ListView(
                      padding: const EdgeInsets.symmetric(horizontal: space4),
                      children: [
                        if (empty.isNotEmpty) _EmptyCapturesSection(captures: empty),
                        for (final capture in sendable)
                          _QueueRow(
                            capture: capture,
                            record: queue.records[capture.captureId],
                            allRecords: queue.records,
                            deviceBlock: queue.deviceBlock,
                            sendingCaptureId: queue.sending,
                            // The engine never retries a rejection on its
                            // own (queue_record.dart's own rule) -- this is
                            // the ONLY path back to pending, so a rejected
                            // row has to be able to reach it directly. The
                            // banner's own RETRY calls wake(), which skips
                            // rejected captures entirely; that button is
                            // not a substitute for this one.
                            onRetry: () => ref
                                .read(queueControllerProvider.notifier)
                                .retryCapture(capture.captureId),
                          ),
                      ],
                    ),
            ),
            const BottomCaptureQueueTabs(current: queueRoute),
          ],
        ),
      ),
    );
  }
}

class _Banner extends StatelessWidget {
  const _Banner({
    required this.deviceBlock,
    required this.heldCount,
    required this.heldBytes,
    required this.onRetry,
  });

  final DeviceBlock? deviceBlock;
  final int heldCount;
  final int heldBytes;
  final VoidCallback onRetry;

  @override
  Widget build(BuildContext context) {
    if (deviceBlock == null && heldCount == 0) {
      // Nothing waiting and nothing blocked: the quiet, correct state. No
      // banner is itself the honest answer -- inventing an "up to date"
      // line would be one more thing that could go stale.
      return const SizedBox.shrink();
    }

    final String message;
    if (deviceBlock?.reason == DeviceBlockReason.signedOut) {
      message = 'Sign in to send what is held on this device.';
    } else if (deviceBlock?.reason == DeviceBlockReason.wrongHost) {
      message = 'This device is pointed at the wrong host. Re-scan to fix it.';
    } else {
      message = '$heldCount held on device · ${_mb(heldBytes)}';
    }

    return Padding(
      padding: const EdgeInsets.symmetric(horizontal: space4),
      child: Container(
        padding: const EdgeInsets.all(space3),
        decoration: BoxDecoration(
          color: chRaised,
          border: Border.all(color: chLine),
          borderRadius: BorderRadius.circular(space1),
        ),
        child: Row(
          children: [
            Container(
              width: space1,
              height: space1,
              decoration: const BoxDecoration(color: chTextMeta, shape: BoxShape.circle),
            ),
            const SizedBox(width: space2),
            Expanded(
              child: Text(message, style: const TextStyle(fontSize: sizeBody, color: chText)),
            ),
            if (deviceBlock == null)
              TextButton(onPressed: onRetry, child: const Text('RETRY')),
          ],
        ),
      ),
    );
  }

  static String _mb(int bytes) =>
      bytes < 1024 * 1024 ? '${(bytes / 1024).round()} KB' : '${(bytes / (1024 * 1024)).toStringAsFixed(1)} MB';
}

class _QueueRow extends StatelessWidget {
  const _QueueRow({
    required this.capture,
    required this.record,
    required this.allRecords,
    required this.deviceBlock,
    required this.sendingCaptureId,
    required this.onRetry,
  });

  final CaptureRecord capture;
  final QueueRecord? record;
  final Map<String, QueueRecord> allRecords;
  final DeviceBlock? deviceBlock;
  final String? sendingCaptureId;

  /// Moves this capture from `rejected` back to `pending` and re-sends it.
  /// Only ever called from the button below, which only exists on a
  /// rejected row -- see `QueueController.retryCapture`'s own doc for why
  /// this is the sole way back.
  final VoidCallback onRetry;

  @override
  Widget build(BuildContext context) {
    final at = capture.startedAt.toLocal();
    final clock = '${at.hour.toString().padLeft(2, '0')}:'
        '${at.minute.toString().padLeft(2, '0')}';
    final label = queueLabel(
      captureId: capture.captureId,
      record: record,
      allRecords: allRecords,
      deviceBlock: deviceBlock,
      sendingCaptureId: sendingCaptureId,
      retention: capture.retention,
      enqueuedAt: record?.enqueuedAt ?? capture.startedAt,
      now: DateTime.now(),
    );
    final rejected = record?.status == QueueStatus.rejected;

    return Padding(
      padding: const EdgeInsets.symmetric(vertical: space2),
      child: Row(
        children: [
          SizedBox(width: 44, child: Text(clock, style: monoMeta(size: sizeXs))),
          const SizedBox(width: space2),
          Expanded(
            child: Text(
              capture.durationMs != null ? _duration(capture.durationMs!) : 'nothing recovered',
              style: const TextStyle(fontSize: sizeBody, color: chText),
            ),
          ),
          const SizedBox(width: space2),
          Text(
            label,
            style: microLabel(
              color: label == 'SENT' ? chResolved : chText2,
              size: sizeXxs,
            ),
          ),
          if (rejected) ...[
            const SizedBox(width: space2),
            InkWell(
              onTap: onRetry,
              child: Padding(
                // The 44px minimum tap target would blow out this row's
                // height; the padding trades a slightly generous hit area
                // for keeping the row compact, which a screen that is
                // mostly a list of these has to do somewhere.
                padding: const EdgeInsets.symmetric(vertical: space1),
                child: Text('TRY AGAIN', style: microLabel(color: chSignal, size: sizeXxs)),
              ),
            ),
          ],
        ],
      ),
    );
  }

  static String _duration(int ms) {
    final total = ms ~/ 1000;
    return total >= 60 ? '${total ~/ 60}m ${total % 60}s' : '${total}s';
  }
}

/// CHRN-61's approved plan, built here rather than there: every `empty`
/// capture is listed, with no retry action, because it never enters the
/// queue and no retry could do anything for it. At three or fewer each
/// shows on its own row; past three they collapse into one summary row that
/// expands in place, so a growing list cannot teach the operator to stop
/// reading a screen whose job is "what has not reached the server".
class _EmptyCapturesSection extends StatefulWidget {
  const _EmptyCapturesSection({required this.captures});

  /// Every `empty` capture, newest first. Never empty -- the caller only
  /// builds this widget when there is at least one.
  final List<CaptureRecord> captures;

  @override
  State<_EmptyCapturesSection> createState() => _EmptyCapturesSectionState();
}

class _EmptyCapturesSectionState extends State<_EmptyCapturesSection> {
  bool _expanded = false;

  @override
  Widget build(BuildContext context) {
    if (widget.captures.length <= 3 || _expanded) {
      return Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [for (final c in widget.captures) _EmptyCaptureRow(capture: c)],
      );
    }
    return InkWell(
      onTap: () => setState(() => _expanded = true),
      child: Padding(
        padding: const EdgeInsets.symmetric(vertical: space2),
        child: Text(
          '${widget.captures.length} EMPTY CAPTURES',
          style: microLabel(color: chText2, size: sizeXxs),
        ),
      ),
    );
  }
}

class _EmptyCaptureRow extends ConsumerWidget {
  const _EmptyCaptureRow({required this.capture});

  final CaptureRecord capture;

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final at = capture.startedAt.toLocal();
    final clock = '${at.hour.toString().padLeft(2, '0')}:'
        '${at.minute.toString().padLeft(2, '0')}';

    return Padding(
      padding: const EdgeInsets.symmetric(vertical: space2),
      child: Row(
        children: [
          SizedBox(width: 44, child: Text(clock, style: monoMeta(size: sizeXs))),
          const SizedBox(width: space2),
          // No duration estimate is persisted for an `empty` capture --
          // `nothing recovered` matches RECENT's own wording for the same
          // null `durationMs`.
          const Expanded(
            child: Text('nothing recovered',
                style: TextStyle(fontSize: sizeBody, color: chText)),
          ),
          const SizedBox(width: space2),
          Text('NOT SENT — EMPTY', style: microLabel(color: chText2, size: sizeXxs)),
          const SizedBox(width: space2),
          InkWell(
            onTap: () =>
                ref.read(captureControllerProvider.notifier).dismiss(capture.captureId),
            child: Padding(
              padding: const EdgeInsets.symmetric(vertical: space1),
              child: Text('DISMISS', style: microLabel(color: chSignal, size: sizeXxs)),
            ),
          ),
        ],
      ),
    );
  }
}
