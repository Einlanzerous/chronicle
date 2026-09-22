/// A real, on-disk capture for the engine to drive: `meta.json` and the
/// sendable file, written the same way `capture_record.dart` itself would,
/// so the engine's own file-length precondition check
/// (`_attemptOne`/`QueueRecord.blockedLocalFileChanged`) is exercised
/// against real bytes rather than a stub.
library;

import 'dart:io';

import 'package:chronicle/capture/capture_record.dart';
import 'package:chronicle/queue/engine.dart';
import 'package:chronicle/queue/queue_record.dart';
import 'package:crypto/crypto.dart';

Future<QueueCapture> writeFixtureCapture(
  Directory root, {
  required String id,
  required List<int> bytes,
  DateTime? startedAt,
  DateTime? enqueuedAt,
  String? retention,
  CaptureState state = CaptureState.ready,
  String? contentHashOverride,
}) async {
  final captureDir = CaptureDir(root, id);
  final started = startedAt ?? DateTime(2026, 1, 1);
  final record = CaptureRecord(
    captureId: id,
    idempotencyKey: 'chr-cap-$id',
    startedAt: started,
    state: state,
    retention: retention,
    contentHash: contentHashOverride ?? sha256.convert(bytes).toString(),
    byteSize: bytes.length,
  );
  // Order matters: writeMeta creates the capture directory; the sendable
  // file is written into it afterwards.
  await captureDir.writeMeta(record);
  await captureDir.sendable(state).writeAsBytes(bytes, flush: true);
  return QueueCapture(
    queueDir: QueueDir(captureDir),
    capture: record,
    queueRecord: QueueRecord.fresh(enqueuedAt ?? started),
  );
}

/// Re-reads a capture's persisted [QueueRecord] and returns a fresh
/// [QueueCapture] carrying it -- what a second `drainPass` call needs,
/// since [QueueCapture.queueRecord] is a snapshot, not a live view.
Future<QueueCapture> reloadCapture(QueueCapture qc) async {
  final record = await qc.queueDir.read() ?? qc.queueRecord;
  return QueueCapture(queueDir: qc.queueDir, capture: qc.capture, queueRecord: record);
}
