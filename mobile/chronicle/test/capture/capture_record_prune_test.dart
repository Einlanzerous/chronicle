/// CHRN-120's two additions to `CaptureDir`: the prune tombstone and the one
/// method that deletes a sendable file. The pass that calls them is proved in
/// `test/queue/prune_test.dart`; this is the shape of the pieces themselves.
library;

import 'dart:io';

import 'package:chronicle/capture/capture_record.dart';
import 'package:flutter_test/flutter_test.dart';

late Directory _root;

void main() {
  setUp(() async {
    _root = await Directory.systemTemp.createTemp('chrn120-capturedir');
  });

  tearDown(() async {
    if (await _root.exists()) await _root.delete(recursive: true);
  });

  Future<CaptureDir> dirWith(String id, {List<String> files = const []}) async {
    final d = CaptureDir(_root, id);
    await d.dir.create(recursive: true);
    for (final name in files) {
      await File('${d.dir.path}/$name').writeAsBytes([1, 2, 3], flush: true);
    }
    return d;
  }

  Future<List<String>> names(CaptureDir d) async => [
        await for (final e in d.dir.list()) e.path.split(Platform.pathSeparator).last
      ]..sort();

  group('the tombstone', () {
    final tombstone = PruneTombstone(
      memoId: 'memo-1',
      prunedAt: DateTime(2026, 9, 25, 12),
      prunedBytes: 4096,
      gate: 'audio_pruned',
    );

    test('writes and reads back whole, and leaves no temp file behind', () async {
      final d = await dirWith('a', files: ['audio.opus', 'meta.json']);

      await d.writePruneTombstone(tombstone);
      final read = (await d.readPruneTombstone())!;

      expect(read.memoId, 'memo-1');
      expect(read.prunedAt, DateTime(2026, 9, 25, 12));
      expect(read.prunedBytes, 4096);
      expect(read.gate, 'audio_pruned');
      expect(await names(d), ['audio.opus', 'meta.json', 'pruned'],
          reason: 'atomic: temp, flush, rename -- nothing half-written is ever visible');
    });

    test('writing it touches nothing else in the directory', () async {
      final d = await dirWith('a', files: ['audio.opus', 'meta.json', 'upload.json']);
      final before = await d.audio.readAsBytes();

      await d.writePruneTombstone(tombstone);

      expect(await d.audio.readAsBytes(), before);
      expect(await names(d), ['audio.opus', 'meta.json', 'pruned', 'upload.json']);
    });

    test('a capture with no tombstone reads as none', () async {
      final d = await dirWith('a');
      expect(await d.readPruneTombstone(), isNull);
    });

    test('it is independent of CHRN-119\'s dismissed marker', () async {
      final d = await dirWith('a');
      await d.markDismissed();
      expect(await d.readPruneTombstone(), isNull);
      await d.writePruneTombstone(tombstone);
      expect(await d.isDismissed(), isTrue);
      expect(await d.readPruneTombstone(), isNotNull);
    });

    // A tombstone that names no memo, or is not whole, is worse than none: it
    // would let the queue believe a capture was delivered without saying which
    // memo. So it reads as none, and `QueueDir.readOrEnqueue` treats it so.
    for (final entry in <String, String>{
      'not JSON': 'garbage',
      'a JSON array': '[]',
      'no memo id': '{"pruned_at":"2026-09-25T12:00:00.000","pruned_bytes":1,"gate":"g"}',
      'an empty memo id':
          '{"memo_id":"","pruned_at":"2026-09-25T12:00:00.000","pruned_bytes":1,"gate":"g"}',
      'no timestamp': '{"memo_id":"m","pruned_bytes":1,"gate":"g"}',
      'an unparseable timestamp':
          '{"memo_id":"m","pruned_at":"soon","pruned_bytes":1,"gate":"g"}',
      'no byte count': '{"memo_id":"m","pruned_at":"2026-09-25T12:00:00.000","gate":"g"}',
      'no gate': '{"memo_id":"m","pruned_at":"2026-09-25T12:00:00.000","pruned_bytes":1}',
      'a memo id that is not a string':
          '{"memo_id":7,"pruned_at":"2026-09-25T12:00:00.000","pruned_bytes":1,"gate":"g"}',
    }.entries) {
      test('${entry.key} reads as no tombstone', () {
        expect(PruneTombstone.tryParse(entry.value), isNull);
      });
    }
  });

  group('deleteSendable', () {
    test('on a ready capture deletes audio.opus and nothing else', () async {
      final d = await dirWith('a',
          files: ['audio.opus', 'meta.json', 'upload.json', 'lease', 'pruned']);

      await d.deleteSendable(CaptureState.ready);

      expect(await names(d), ['lease', 'meta.json', 'pruned', 'upload.json']);
    });

    test('on a salvaged capture deletes audio.trimmed and keeps audio.opus', () async {
      final d = await dirWith('a',
          files: ['audio.opus', 'audio.trimmed', 'meta.json', 'upload.json']);

      await d.deleteSendable(CaptureState.salvaged);

      expect(await names(d), ['audio.opus', 'meta.json', 'upload.json'],
          reason: 'the untrimmed original holds bytes no server copy has');
    });

    test('a file already gone is not an error, and a second call is a no-op',
        () async {
      final d = await dirWith('a', files: ['meta.json']);

      await d.deleteSendable(CaptureState.ready);
      await d.deleteSendable(CaptureState.ready);
      await d.deleteSendable(CaptureState.salvaged);

      expect(await names(d), ['meta.json']);
    });

    test('refuses a capture that was never sent, and deletes nothing', () async {
      final d = await dirWith('a', files: ['audio.opus', 'meta.json']);

      expect(() => d.deleteSendable(CaptureState.empty), throwsStateError);
      expect(() => d.deleteSendable(CaptureState.recording), throwsStateError);

      expect(await names(d), ['audio.opus', 'meta.json']);
    });
  });
}
