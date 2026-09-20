import 'dart:convert';
import 'dart:io';
import 'dart:typed_data';

import 'package:chronicle/capture/capture_record.dart';
import 'package:chronicle/capture/ogg.dart';
import 'package:chronicle/capture/recovery.dart';
import 'package:flutter_test/flutter_test.dart';

import 'ogg_fixtures.dart';

/// An owner that answers however the test needs, including "cannot be asked".
class _Owner implements CaptureOwner {
  _Owner.holds(this._held) : _reachable = true;
  _Owner.holdsNothing()
      : _held = const {},
        _reachable = true;
  _Owner.unreachable()
      : _held = const {},
        _reachable = false;

  final Set<String> _held;
  final bool _reachable;

  @override
  Future<bool> isHeld(String captureId) async {
    if (!_reachable) throw StateError('the owner could not be asked');
    return _held.contains(captureId);
  }
}

late Directory _root;

CaptureDir _capture(String id) => CaptureDir(_root, id);

Future<CaptureRecord> _seed(
  String id, {
  required Uint8List audio,
  CaptureState state = CaptureState.recording,
  Duration? leaseAge,
  int heartbeatMs = 5000,
}) async {
  final capture = _capture(id);
  await capture.dir.create(recursive: true);
  await capture.audio.writeAsBytes(audio, flush: true);
  final record = CaptureRecord(
    captureId: id,
    idempotencyKey: 'chr-cap-$id',
    startedAt: DateTime.now().subtract(const Duration(minutes: 1)),
    state: state,
  );
  await capture.writeMeta(record);
  if (leaseAge != null) {
    await capture.lease.writeAsString(jsonEncode({
      'instance_id': 'instance-1',
      'capture_id': id,
      'state': 'recording',
      'heartbeat_at':
          DateTime.now().subtract(leaseAge).millisecondsSinceEpoch,
      'heartbeat_ms': heartbeatMs,
    }));
  }
  return record;
}

void main() {
  setUp(() async {
    _root = await Directory.systemTemp.createTemp('chrn60');
  });

  tearDown(() async {
    if (await _root.exists()) await _root.delete(recursive: true);
  });

  group('classification: the owner is asked first, and its answer is definite', () {
    test('a capture the owner still holds is left completely alone', () async {
      // The swipe-away case. `state: recording` on disk says nothing here --
      // the service outlives the UI on purpose -- and acting on it would trim a
      // file that is still being appended to.
      await _seed('live', audio: oggStream(audioPages: 20));
      final outcome = await recoverAll(_root, _Owner.holds({'live'}));

      expect(outcome.stillRecording, ['live']);
      expect(outcome.salvaged, isEmpty);
      expect(outcome.empty, isEmpty);
      final record = await _capture('live').readMeta();
      expect(record!.state, CaptureState.recording);
      expect(await _capture('live').trimmed.exists(), isFalse);
    });

    test('a capture nobody holds is salvaged NOW, with no lease to wait out',
        () async {
      // The force-stop case, relaunched immediately: the lease is only seconds
      // old, and it must not matter. A registry that answers "not held" is a
      // definite answer, not a timeout.
      final whole = oggStream(audioPages: 20);
      final torn = Uint8List.sublistView(whole, 0, whole.length - 7);
      await _seed('killed', audio: torn, leaseAge: const Duration(seconds: 1));

      final outcome = await recoverAll(_root, _Owner.holdsNothing());

      expect(outcome.salvaged, ['killed']);
      expect(outcome.retryAfter, isNull,
          reason: 'a definite answer must never schedule a wait');
      expect((await _capture('killed').readMeta())!.state, CaptureState.salvaged);
    });

    test('an already-finished capture is not revisited', () async {
      await _seed('done',
          audio: oggStream(audioPages: 4), state: CaptureState.ready);
      final outcome = await recoverAll(_root, _Owner.holdsNothing());
      expect(outcome.isQuiet, isTrue);
    });
  });

  group('classification: the lease arbitrates only when the owner is silent', () {
    test('a young lease with no owner to ask defers and schedules a re-look',
        () async {
      await _seed('young',
          audio: oggStream(audioPages: 20), leaseAge: const Duration(seconds: 2));

      final outcome = await recoverAll(_root, _Owner.unreachable());

      expect(outcome.salvaged, isEmpty);
      expect(outcome.retryAfter, isNotNull,
          reason: 'it must look again rather than wait for the next launch');
      expect((await _capture('young').readMeta())!.state, CaptureState.recording);
    });

    test('a lease older than three heartbeats means the recorder is gone',
        () async {
      await _seed('stale',
          audio: oggStream(audioPages: 20), leaseAge: const Duration(seconds: 60));

      final outcome = await recoverAll(_root, _Owner.unreachable());

      expect(outcome.salvaged, ['stale']);
      expect(outcome.retryAfter, isNull);
    });

    test('no lease at all, and no owner, is not a reason to refuse a salvage',
        () async {
      await _seed('noleese', audio: oggStream(audioPages: 20));
      final outcome = await recoverAll(_root, _Owner.unreachable());
      expect(outcome.salvaged, ['noleese']);
    });
  });

  group('the salvage destroys nothing', () {
    test('audio.opus is byte-identical after a trim', () async {
      final whole = oggStream(audioPages: 30);
      final torn = Uint8List.sublistView(whole, 0, whole.length - 11);
      await _seed('keep', audio: torn);

      final before = await _capture('keep').audio.readAsBytes();
      await recoverAll(_root, _Owner.holdsNothing());
      final after = await _capture('keep').audio.readAsBytes();

      expect(after, equals(before),
          reason: 'the trim writes a new file; it never edits the only copy');
      expect(await _capture('keep').trimmed.exists(), isTrue);
    });

    test('the trimmed file is exactly the prefix the scan chose', () async {
      final whole = oggStream(audioPages: 30);
      final torn = Uint8List.sublistView(whole, 0, whole.length - 11);
      await _seed('prefix', audio: torn);

      await recoverAll(_root, _Owner.holdsNothing());

      final record = (await _capture('prefix').readMeta())!;
      final trimmed = await _capture('prefix').trimmed.readAsBytes();
      expect(trimmed.length, record.trimOffset);
      expect(trimmed, equals(torn.sublist(0, record.trimOffset!)));
      // And what it produced is a whole stream, which is the entire point:
      // the server can describe it rather than declining a duration.
      expect(scanOgg(Uint8List.fromList(trimmed)).endsAtEof, isTrue);
    });

    test('the hash covers the bytes that will be sent, not the original',
        () async {
      final whole = oggStream(audioPages: 30);
      final torn = Uint8List.sublistView(whole, 0, whole.length - 11);
      await _seed('hash', audio: torn);

      await recoverAll(_root, _Owner.holdsNothing());

      final record = (await _capture('hash').readMeta())!;
      expect(record.contentHash, isNotNull);
      expect(record.byteSize, record.trimOffset);
      expect(_capture('hash').sendable(record.state).path,
          _capture('hash').trimmed.path);
    });
  });

  group('a capture that recovered no audio is kept, and says so', () {
    test('headers with no audio page becomes empty rather than disappearing',
        () async {
      // Somebody spoke and none of it left the encoder. Deleting the remnant
      // would make the worst loss this system can suffer completely silent.
      final headersOnly = BytesBuilder()
        ..add(oggPage(
            serial: 3, sequence: 0, granule: 0, body: opusHead(), headerType: 0x02))
        ..add(oggPage(serial: 3, sequence: 1, granule: 0, body: opusTags()));
      await _seed('nothing', audio: headersOnly.toBytes());

      final outcome = await recoverAll(_root, _Owner.holdsNothing());

      expect(outcome.empty, ['nothing']);
      expect(outcome.salvaged, isEmpty);
      final capture = _capture('nothing');
      expect(await capture.dir.exists(), isTrue);
      expect(await capture.audio.exists(), isTrue);
      expect((await capture.readMeta())!.state, CaptureState.empty);
    });

    test('an empty capture is never given a hash, so nothing can try to send it',
        () async {
      await _seed('void', audio: Uint8List(0));
      await recoverAll(_root, _Owner.holdsNothing());
      final record = (await _capture('void').readMeta())!;
      expect(record.state, CaptureState.empty);
      expect(record.contentHash, isNull);
    });
  });

  group('finalise, for a recorder that stopped cleanly', () {
    test('a whole file becomes ready, hashed over itself', () async {
      final record = await _seed('clean', audio: oggStream(audioPages: 25));
      final done = await finalise(_capture('clean'), record);

      expect(done.state, CaptureState.ready);
      expect(done.durationMs, 500);
      expect(done.contentHash, isNotNull);
      expect(_capture('clean').sendable(done.state).path,
          _capture('clean').audio.path);
    });

    test('a "clean" stop that did not land on a page boundary is salvaged',
        () async {
      // A recorder that failed at stop() can leave a file that looks finished
      // and is not. Sent as-is, the server would refuse it a duration.
      final whole = oggStream(audioPages: 25);
      final torn = Uint8List.sublistView(whole, 0, whole.length - 5);
      final record = await _seed('liar', audio: torn);

      final done = await finalise(_capture('liar'), record);

      expect(done.state, CaptureState.salvaged);
      expect(await _capture('liar').trimmed.exists(), isTrue);
    });
  });

  group('the record carries what CHRN-20 and CHRN-62 need', () {
    test('the idempotency key clears the contract floor and survives a reload',
        () async {
      final identity = CaptureIdentity.mint();
      expect(identity.idempotencyKey.length, greaterThanOrEqualTo(16));
      expect(identity.idempotencyKey, contains(identity.captureId));

      final capture = _capture(identity.captureId);
      await capture.writeMeta(CaptureRecord(
        captureId: identity.captureId,
        idempotencyKey: identity.idempotencyKey,
        startedAt: DateTime.now(),
        state: CaptureState.recording,
      ));
      final read = await capture.readMeta();
      expect(read!.idempotencyKey, identity.idempotencyKey);
    });

    test('retention starts null and is never defaulted here', () async {
      // days_30 written at capture time could never afterwards be lowered to
      // DISCARD NOW: the server's ratchet only raises, and no operation sets
      // retention after a declaration. CHRN-62 fills this in.
      final record = await _seed('ret', audio: oggStream(audioPages: 3));
      expect(record.retention, isNull);
      final done = await finalise(_capture('ret'), record);
      expect(done.retention, isNull);
    });
  });
}
