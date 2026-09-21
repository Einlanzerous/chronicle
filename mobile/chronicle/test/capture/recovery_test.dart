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

/// A stream cut mid-page, so a test that is about something else does not
/// accidentally assert a label.
///
/// The two lease-arbitration tests below used a WHOLE stream and asserted
/// `salvaged`. That was incidental to their subject and became wrong under
/// CHRN-114, where a recovered whole file is `ready`. They take this instead.
Uint8List _torn({int audioPages = 20}) {
  final whole = oggStream(audioPages: audioPages);
  return Uint8List.sublistView(whole, 0, whole.length - 7);
}

/// The device-written fixtures, shared with `internal/audio` and read across
/// the repository rather than copied — see `device_fixture_test.dart`.
///
/// `chrn60_trimmed.opus` is a COMPLETE stream a phone actually wrote, which is
/// the case CHRN-114 is about and the case a hand-built stream can only contain
/// what we already believed about. `chrn60_torn.opus` is that recording cut
/// host-side: a real tear cannot be produced over adb, since the media server
/// finalises the file when the client dies — which is the measurement this
/// whole ticket rests on.
Uint8List _fixture(String name) => Uint8List.fromList(
      File('../../internal/audio/testdata/$name').readAsBytesSync(),
    );

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
      expect(outcome.ready, isEmpty);
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
      expect(outcome.ready, isEmpty);
      expect(outcome.retryAfter, isNotNull,
          reason: 'it must look again rather than wait for the next launch');
      expect((await _capture('young').readMeta())!.state, CaptureState.recording);
    });

    test('a lease older than three heartbeats means the recorder is gone',
        () async {
      await _seed('stale',
          audio: _torn(), leaseAge: const Duration(seconds: 60));

      final outcome = await recoverAll(_root, _Owner.unreachable());

      expect(outcome.salvaged, ['stale']);
      expect(outcome.retryAfter, isNull);
    });

    test('no lease at all, and no owner, is not a reason to refuse a salvage',
        () async {
      await _seed('noleese', audio: _torn());
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
      expect(outcome.ready, isEmpty);
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

  group('CHRN-114: a recovered WHOLE capture is ready, not salvaged', () {
    test('a device-written complete stream recovers to ready, with one file',
        () async {
      await _seed('whole', audio: _fixture('chrn60_trimmed.opus'));

      final outcome = await recoverAll(_root, _Owner.holdsNothing());
      final record = (await _capture('whole').readMeta())!;

      expect(record.state, CaptureState.ready);
      expect(await _capture('whole').trimmed.exists(), isFalse,
          reason: 'there is nothing to trim, so writing a byte-for-byte '
              'duplicate is pure waste against the free-space floor');
      expect(_capture('whole').sendable(record.state).path,
          _capture('whole').audio.path,
          reason: 'sendable must name the file that exists');
      expect(outcome.ready, ['whole']);
      expect(outcome.salvaged, isEmpty,
          reason: 'a field named salvaged must not list captures no trim '
              'touched');
    });

    test('but it is marked as recovered, which is the only record that it was',
        () async {
      // Without this the capture is field-for-field identical to a clean stop:
      // same state, same hash, same duration, one file. The Kotlin logs are
      // logcat, a ring buffer, and are gone long before anybody asks.
      await _seed('whole', audio: _fixture('chrn60_trimmed.opus'));
      await recoverAll(_root, _Owner.holdsNothing());

      final record = (await _capture('whole').readMeta())!;
      expect(record.recoveredAt, isNotNull);
    });

    test('a capture the app watched stop is ready with NO recovered mark',
        () async {
      final record = await _seed('clean2', audio: _fixture('chrn60_trimmed.opus'));
      final done = await finalise(_capture('clean2'), record);

      expect(done.state, CaptureState.ready);
      expect(done.recoveredAt, isNull,
          reason: 'the mark is what separates the two, so it must not be set '
              'by the path the app watched');
    });

    test('a device-written TORN stream still salvages, and still destroys nothing',
        () async {
      await _seed('torn', audio: _fixture('chrn60_torn.opus'));
      final before = await _capture('torn').audio.readAsBytes();

      final outcome = await recoverAll(_root, _Owner.holdsNothing());
      final record = (await _capture('torn').readMeta())!;

      expect(record.state, CaptureState.salvaged);
      expect(record.recoveredAt, isNotNull);
      expect(await _capture('torn').trimmed.exists(), isTrue);
      expect(await _capture('torn').audio.readAsBytes(), equals(before));
      expect(outcome.salvaged, ['torn']);
      expect(outcome.ready, isEmpty);
      // The number the Go side pins independently.
      expect(record.trimOffset, 20076);
      expect(record.durationMs, 4514);
    });

    test('one pass over a mixed directory buckets each by what it became',
        () async {
      await _seed('a-whole', audio: _fixture('chrn60_trimmed.opus'));
      await _seed('b-torn', audio: _fixture('chrn60_torn.opus'));
      await _seed('c-void', audio: Uint8List(0));

      final outcome = await recoverAll(_root, _Owner.holdsNothing());

      expect(outcome.ready, ['a-whole']);
      expect(outcome.salvaged, ['b-torn']);
      expect(outcome.empty, ['c-void']);
      expect(outcome.isQuiet, isFalse);
    });

    test('the empty path is untouched by any of this', () async {
      await _seed('still-empty', audio: Uint8List(0));
      await recoverAll(_root, _Owner.holdsNothing());

      final record = (await _capture('still-empty').readMeta())!;
      expect(record.state, CaptureState.empty);
      expect(record.contentHash, isNull,
          reason: 'nothing may try to send a capture with no audio');
      expect(await _capture('still-empty').audio.exists(), isTrue,
          reason: 'the remnant is the only visible trace that a memo was '
              'attempted at all');
      expect(record.recoveredAt, isNotNull);
    });
  });
}
