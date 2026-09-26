/// CHRN-120's prune pass: the CONDITION under which local audio is deleted, and
/// that nothing else can fire it -- not just that a file can be unlinked.
///
/// The groups are the ticket's own claims, in order: the pass ships dark; the
/// gate is the only thing that permits a delete and every refusal leaves the
/// capture untouched; age alone never deletes; the poll floor bounds WHEN the
/// server is asked, never WHETHER a delete happens; a crash at each step of
/// tombstone -> mark -> unlink is finished without asking the server again;
/// only the file that was sent goes; and a lost `upload.json` never turns a
/// pruned capture back into a pending one.
library;

import 'dart:io';

import 'package:chronicle/api/transport.dart';
import 'package:chronicle/capture/capture_record.dart';
import 'package:chronicle/queue/audio_gate_transport.dart';
import 'package:chronicle/queue/device_block.dart';
import 'package:chronicle/queue/engine.dart';
import 'package:chronicle/queue/failure.dart';
import 'package:chronicle/queue/prune.dart';
import 'package:chronicle/queue/queue_record.dart';
import 'package:flutter_test/flutter_test.dart';

import 'support/capture_fixture.dart';
import 'support/fake_audio_gate.dart';

late Directory _root;
final _now = DateTime(2026, 9, 25, 12);

/// 55 days before [_now]: past the server's 30-day window with room to spare.
final _old = DateTime(2026, 8, 1);

final _bytes = List<int>.generate(32, (i) => i);

void main() {
  late List<String> logs;

  setUp(() async {
    _root = await Directory.systemTemp.createTemp('chrn120-prune');
    logs = [];
  });

  tearDown(() async {
    if (await _root.exists()) await _root.delete(recursive: true);
  });

  Future<PruneReport> run(
    List<QueueCapture> captures,
    FakeAudioGate gate, {
    DeviceBlock? block,
    bool enabled = true,
    DateTime? at,
    int? maxProbes,
  }) =>
      prunePass(
        captures: captures,
        transport: gate,
        deviceBlock: block,
        now: () => at ?? _now,
        enabled: enabled,
        maxProbes: maxProbes ?? maxProbesPerPass,
        log: logs.add,
      );

  Future<Map<String, Object?>> snapshot(QueueCapture qc) async =>
      (await qc.queueDir.read())!.toJson();

  group('it ships dark', () {
    test('the compile-time default is off, so a build that does not opt in never '
        'deletes', () {
      // Pinned here so changing the default fails CI. Flipping it is the
      // follow-up that closes the backup gap (CHRN-68 / SERV-43) -- see
      // prune.dart -- and is never done by accident.
      expect(pruneLocalAudioEnabled, isFalse);
    });

    test('with no explicit opt-in the pass asks nothing and deletes nothing', () async {
      final qc = await writeAckedCapture(_root, id: 'a', bytes: _bytes, startedAt: _old);
      final gate = FakeAudioGate({'memo-a': Answers.pruned});

      final report = await prunePass(
        captures: [qc],
        transport: gate,
        now: () => _now,
        log: logs.add,
      );

      expect(gate.probed, isEmpty);
      expect(report.probed, 0);
      expect(await qc.queueDir.capture.audio.exists(), isTrue);
      expect(await qc.queueDir.capture.pruned.exists(), isFalse);
    });

    test('an explicit false is the same', () async {
      final qc = await writeAckedCapture(_root, id: 'a', bytes: _bytes, startedAt: _old);
      final gate = FakeAudioGate({'memo-a': Answers.pruned});

      await run([qc], gate, enabled: false);

      expect(gate.probed, isEmpty);
      expect(await qc.queueDir.capture.audio.exists(), isTrue);
    });
  });

  group('the gate is the only thing that permits a delete', () {
    test('410 audio_pruned deletes the sendable file and nothing else', () async {
      final qc = await writeAckedCapture(_root, id: 'a', bytes: _bytes, startedAt: _old);
      final metaBefore = await qc.queueDir.capture.meta.readAsString();
      final gate = FakeAudioGate({'memo-a': Answers.pruned});

      final report = await run([qc], gate);

      expect(report.pruned, 1);
      expect(gate.probed, ['memo-a']);
      expect(await qc.queueDir.capture.audio.exists(), isFalse);
      // The directory listing IS the assertion: only the audio went.
      expect(await listCaptureDir(qc), ['meta.json', 'pruned', 'upload.json']);
      expect(await qc.queueDir.capture.meta.readAsString(), metaBefore);

      final tombstone = (await qc.queueDir.capture.readPruneTombstone())!;
      expect(tombstone.memoId, 'memo-a');
      expect(tombstone.gate, 'audio_pruned');
      expect(tombstone.prunedBytes, _bytes.length);
      expect(tombstone.prunedAt, _now);

      final record = (await qc.queueDir.read())!;
      expect(record.status, QueueStatus.acknowledged);
      expect(record.memoId, 'memo-a');
      expect(record.locallyPrunedAt, _now);
      expect(record.lastPolledAt, _now);

      expect(logs.where((l) => l.contains('memo=memo-a gate=audio_pruned bytes=32')),
          hasLength(1));
    });

    // Every refusal shape, end to end through the pass: the capture is left
    // exactly as it was. Only `lastPolledAt` may move, and only when the server
    // actually ANSWERED.
    final refusals = <(String, void Function(FakeAudioGate), bool)>[
      ('206 still there', (g) => g.fallback = Answers.stillThere, true),
      ('bare 200, Range ignored', (g) => g.fallback = const AudioProbeResponse(200), true),
      ('400', (g) => g.fallback = const AudioProbeResponse(400), true),
      ('404', (g) => g.fallback = Answers.error(404, 'not_found'), true),
      ('500 audio_missing', (g) => g.fallback = Answers.error(500, 'audio_missing'), true),
      ('500 other', (g) => g.fallback = Answers.error(500, 'internal'), true),
      ('502', (g) => g.fallback = const AudioProbeResponse(502), true),
      ('503', (g) => g.fallback = const AudioProbeResponse(503), true),
      ('429', (g) => g.fallback = const AudioProbeResponse(429), true),
      ('410 with the wrong code', (g) => g.fallback = Answers.error(410, 'gone'), true),
      ('410 with a non-JSON body',
          (g) => g.fallback = const AudioProbeResponse(410, 'gone'), true),
      ('410 with no body', (g) => g.fallback = const AudioProbeResponse(410), true),
      ('401', (g) => g.fallback = const AudioProbeResponse(401), false),
      ('a network error', (g) => g.throwInstead = const SocketException('down'), false),
      (
        'NotChronicleException',
        (g) => g.throwInstead = NotChronicleException(
              statusCode: 302,
              location: 'https://x.cloudflareaccess.com/login',
              requestedUrl: Uri.parse('https://chronicle.example.com/audio/x'),
            ),
        false,
      ),
    ];

    for (final (label, configure, answered) in refusals) {
      test('$label leaves the capture untouched; only lastPolledAt may change', () async {
        final qc = await writeAckedCapture(_root, id: 'a', bytes: _bytes, startedAt: _old);
        final gate = FakeAudioGate();
        configure(gate);
        final before = await snapshot(qc);

        final report = await run([qc], gate);

        expect(report.pruned, 0);
        expect(gate.probed, ['memo-a']);
        expect(await qc.queueDir.capture.audio.readAsBytes(), _bytes,
            reason: 'the audio is exactly where it was');
        expect(await qc.queueDir.capture.pruned.exists(), isFalse,
            reason: 'no tombstone: nothing was decided');
        final after = await snapshot(qc);
        expect(after['locally_pruned_at'], isNull);
        expect({...after}..remove('last_polled_at'), {...before}..remove('last_polled_at'),
            reason: 'nothing but lastPolledAt differs');
        expect(after['last_polled_at'], answered ? _now.toIso8601String() : isNull,
            reason: 'a poll is spent only when the server answered');
        expect(await listCaptureDir(qc), ['audio.opus', 'meta.json', 'upload.json']);
      });
    }
  });

  group('a capture that is not acknowledged is never asked about', () {
    test('pending, rejected and blockedLocalFileChanged are never probed or touched, '
        'even carrying a memo id', () async {
      // A memo id is set only on an acknowledged record, so none of these should
      // have one. Each is given one anyway -- what a corrupt or legacy record
      // might carry -- so that it is the STATUS check, and not the absence of an
      // id, that keeps them away from the gate.
      final pending = await writeFixtureCapture(_root, id: 'p', bytes: _bytes, startedAt: _old);
      await pending.queueDir.write(
          QueueRecord(status: QueueStatus.pending, enqueuedAt: _old, memoId: 'memo-p'));
      final rejected = await writeFixtureCapture(_root, id: 'r', bytes: _bytes, startedAt: _old);
      await rejected.queueDir.write(QueueRecord(
        status: QueueStatus.rejected,
        enqueuedAt: _old,
        rejectReason: RejectReason.serverRefused,
        memoId: 'memo-r',
      ));
      final blocked = await writeFixtureCapture(_root, id: 'b', bytes: _bytes, startedAt: _old);
      await blocked.queueDir.write(QueueRecord(
        status: QueueStatus.blockedLocalFileChanged,
        enqueuedAt: _old,
        memoId: 'memo-b',
      ));

      // Even a memo id the gate would happily call pruned: none is ever asked.
      final gate = FakeAudioGate()..fallback = Answers.pruned;

      await run([pending, rejected, blocked], gate);

      expect(gate.probed, isEmpty);
      for (final qc in [pending, rejected, blocked]) {
        expect(await qc.queueDir.capture.audio.exists(), isTrue);
        expect(await qc.queueDir.capture.pruned.exists(), isFalse);
      }
    });

    test('a stale snapshot does not fool it: the pass re-reads upload.json', () async {
      final qc = await writeAckedCapture(_root, id: 'a', bytes: _bytes, startedAt: _old);
      // The snapshot the caller holds says pending; disk says acknowledged...
      final stale = QueueCapture(
        queueDir: qc.queueDir,
        capture: qc.capture,
        queueRecord: QueueRecord.fresh(_old),
      );
      final gate = FakeAudioGate({'memo-a': Answers.pruned});
      await run([stale], gate);
      expect(await qc.queueDir.capture.audio.exists(), isFalse,
          reason: 'disk is the authority, and it says acknowledged');

      // ...and the reverse: a snapshot that says acknowledged, disk says
      // pending, must not delete.
      final other = await writeFixtureCapture(_root, id: 'o', bytes: _bytes, startedAt: _old);
      await other.queueDir.write(QueueRecord.fresh(_old));
      final lying = QueueCapture(
        queueDir: other.queueDir,
        capture: other.capture,
        queueRecord: QueueRecord(
          status: QueueStatus.acknowledged,
          enqueuedAt: _old,
          memoId: 'memo-o',
          acknowledgedAt: _old,
        ),
      );
      final gate2 = FakeAudioGate({'memo-o': Answers.pruned});
      await run([lying], gate2);
      expect(gate2.probed, isEmpty);
      expect(await other.queueDir.capture.audio.exists(), isTrue);
    });

    test('an active device block skips the whole pass', () async {
      final qc = await writeAckedCapture(_root, id: 'a', bytes: _bytes, startedAt: _old);
      final gate = FakeAudioGate({'memo-a': Answers.pruned});

      await run(
        [qc],
        gate,
        block: const DeviceBlock(
          reason: DeviceBlockReason.signedOut,
          serverUrl: 'https://chronicle.example.com',
          tokenDigest: '',
        ),
      );

      expect(gate.probed, isEmpty);
      expect(await qc.queueDir.capture.audio.exists(), isTrue);
    });
  });

  group('nothing but the gate permits a delete -- never a timer, never a local value', () {
    test('a very old acknowledged capture the server still holds is never deleted',
        () async {
      final qc = await writeAckedCapture(
        _root,
        id: 'a',
        bytes: _bytes,
        startedAt: DateTime(2020, 1, 1),
        acknowledgedAt: DateTime(2020, 1, 1),
      );
      final gate = FakeAudioGate(); // 206 for everything: the server has it

      // Every day for a year.
      for (var d = 0; d < 365; d++) {
        await run([qc], gate, at: _now.add(Duration(days: d)));
      }

      expect(gate.probed, hasLength(365), reason: 'asked daily, as the cap allows');
      expect(await qc.queueDir.capture.audio.exists(), isTrue,
          reason: 'age alone is not a reason -- only the server\'s own prune is');
      expect((await qc.queueDir.read())!.locallyPrunedAt, isNull);
    });

    test('a locally declared discard_now with no server answer yet deletes nothing',
        () async {
      final qc = await writeAckedCapture(
        _root,
        id: 'a',
        bytes: _bytes,
        startedAt: _old,
        retention: 'discard_now',
      );
      final gate = FakeAudioGate(); // still there

      await run([qc], gate);

      expect(gate.probed, ['memo-a']);
      expect(await qc.queueDir.capture.audio.exists(), isTrue,
          reason: 'a local value can only hasten a question, never an answer');
    });

    test('a locally declared forever is never asked and never deleted, even if the '
        'server would have said 410', () async {
      final qc = await writeAckedCapture(
        _root,
        id: 'a',
        bytes: _bytes,
        startedAt: _old,
        retention: 'forever',
      );
      final gate = FakeAudioGate({'memo-a': Answers.pruned});

      await run([qc], gate);

      expect(gate.probed, isEmpty, reason: 'skipped as an optimisation');
      expect(await qc.queueDir.capture.audio.exists(), isTrue,
          reason: 'and a local value never PERMITS a delete, so nothing went');
    });
  });

  group('the poll floor decides when to ask, never whether to delete', () {
    test('days_30 and no-opinion captures are not asked before startedAt + 30 days',
        () async {
      final justUnder = await writeAckedCapture(_root,
          id: 'u', bytes: _bytes, startedAt: _now.subtract(const Duration(days: 29, hours: 23)));
      final exactly = await writeAckedCapture(_root,
          id: 'e', bytes: _bytes, startedAt: _now.subtract(const Duration(days: 30)));
      final gate = FakeAudioGate();

      await run([justUnder, exactly], gate);

      expect(gate.probed, ['memo-e']);
    });

    test('a discard_now capture is asked from its acknowledgement, not before',
        () async {
      final acked = await writeAckedCapture(_root,
          id: 'a',
          bytes: _bytes,
          startedAt: _now.subtract(const Duration(days: 1)),
          acknowledgedAt: _now.subtract(const Duration(hours: 1)),
          retention: 'discard_now');
      final future = await writeAckedCapture(_root,
          id: 'f',
          bytes: _bytes,
          startedAt: _now.subtract(const Duration(days: 1)),
          acknowledgedAt: _now.add(const Duration(hours: 1)),
          retention: 'discard_now');
      final gate = FakeAudioGate();

      await run([acked, future], gate);

      expect(gate.probed, ['memo-a']);
    });

    test('lastPolledAt is persisted, read back, and caps a capture at one poll per '
        '24 hours', () async {
      final qc = await writeAckedCapture(_root, id: 'a', bytes: _bytes, startedAt: _old);
      final gate = FakeAudioGate();

      await run([qc], gate);
      expect((await qc.queueDir.read())!.lastPolledAt, _now, reason: 'persisted');
      expect(QueueRecord.fromJson(jsonRoundTrip((await qc.queueDir.read())!)).lastPolledAt,
          _now, reason: 'and survives a serialise/parse');

      await run([qc], gate, at: _now.add(const Duration(hours: 23, minutes: 59)));
      expect(gate.probed, hasLength(1), reason: 'not again inside 24 hours');

      await run([qc], gate, at: _now.add(const Duration(hours: 24)));
      expect(gate.probed, hasLength(2), reason: 'again once the 24 hours are up');
    });

    test('a backlog is processed least-recently-polled first, bounded per pass',
        () async {
      expect(maxProbesPerPass, 10);

      final captures = <QueueCapture>[];
      for (var i = 0; i < 12; i++) {
        final id = 'c${i.toString().padLeft(2, '0')}';
        // c05 and c09 have never been asked. The rest were last asked
        // 25h + i hours ago, so a HIGHER i means asked longer ago.
        final neverAsked = i == 5 || i == 9;
        captures.add(await writeAckedCapture(
          _root,
          id: id,
          bytes: _bytes,
          startedAt: _old.add(Duration(minutes: i)),
          lastPolledAt: neverAsked ? null : _now.subtract(Duration(hours: 25 + i)),
        ));
      }
      final gate = FakeAudioGate();

      final report = await run(captures, gate);

      expect(report.probed, maxProbesPerPass);
      expect(gate.probed, [
        'memo-c05', 'memo-c09', // never asked, oldest startedAt first
        'memo-c11', 'memo-c10', 'memo-c08', 'memo-c07', 'memo-c06', 'memo-c04',
        'memo-c03', 'memo-c02', // then asked-longest-ago first
      ]);
      expect(gate.probed, isNot(contains('memo-c00')));
      expect(gate.probed, isNot(contains('memo-c01')),
          reason: 'the two asked most recently wait for the next pass');
    });
  });

  group('a crash at each step is finished without asking the server again', () {
    // The order is tombstone -> locallyPrunedAt -> unlink. Each test makes ONE
    // step fail, checks what the disk holds, then runs an ordinary pass against
    // a gate that records if it is asked.
    Future<QueueCapture> crashing(
      QueueCapture base, {
      bool failTombstone = false,
      bool failMark = false,
      bool failUnlink = false,
    }) async {
      final dir = _CrashingDir(
        _root,
        base.capture.captureId,
        failTombstone: failTombstone,
        failUnlink: failUnlink,
      );
      return QueueCapture(
        queueDir: failMark ? _MarkFailingQueueDir(dir) : QueueDir(dir),
        capture: base.capture,
        queueRecord: base.queueRecord,
      );
    }

    test('before the tombstone: nothing was decided, the capture is asked about again',
        () async {
      final base = await writeAckedCapture(_root, id: 'a', bytes: _bytes, startedAt: _old);
      final qc = await crashing(base, failTombstone: true);

      await run([qc], FakeAudioGate({'memo-a': Answers.pruned}));

      expect(await base.queueDir.capture.audio.exists(), isTrue);
      expect(await base.queueDir.capture.pruned.exists(), isFalse);
      expect((await base.queueDir.read())!.locallyPrunedAt, isNull);
      expect((await base.queueDir.read())!.lastPolledAt, isNull,
          reason: 'the poll was not spent, so the retry is not delayed a day');

      final gate = FakeAudioGate({'memo-a': Answers.pruned});
      await run([base], gate);
      expect(gate.probed, ['memo-a'], reason: 'no tombstone, so it is asked again');
      expect(await base.queueDir.capture.audio.exists(), isFalse);
    });

    test('after the tombstone, before the mark: finished with no request', () async {
      final base = await writeAckedCapture(_root, id: 'a', bytes: _bytes, startedAt: _old);
      final qc = await crashing(base, failMark: true);

      await run([qc], FakeAudioGate({'memo-a': Answers.pruned}));

      expect(await base.queueDir.capture.pruned.exists(), isTrue);
      expect((await base.queueDir.read())!.locallyPrunedAt, isNull);
      expect(await base.queueDir.capture.audio.exists(), isTrue,
          reason: 'the file is never unlinked before the mark is written');

      final gate = FakeAudioGate({'memo-a': Answers.pruned});
      final report = await run([base], gate);
      expect(gate.probed, isEmpty, reason: 'a decided capture is never re-polled');
      expect(report.finished, 1);
      expect(await base.queueDir.capture.audio.exists(), isFalse);
      expect((await base.queueDir.read())!.locallyPrunedAt, isNotNull);
    });

    test('after the mark, before the unlink: finished with no request', () async {
      final base = await writeAckedCapture(_root, id: 'a', bytes: _bytes, startedAt: _old);
      final qc = await crashing(base, failUnlink: true);

      await run([qc], FakeAudioGate({'memo-a': Answers.pruned}));

      expect(await base.queueDir.capture.pruned.exists(), isTrue);
      expect((await base.queueDir.read())!.locallyPrunedAt, isNotNull);
      expect(await base.queueDir.capture.audio.exists(), isTrue);

      final gate = FakeAudioGate({'memo-a': Answers.pruned});
      final report = await run([base], gate);
      expect(gate.probed, isEmpty);
      expect(report.finished, 1);
      expect(await base.queueDir.capture.audio.exists(), isFalse);
    });

    test('a file already gone is tolerated, and a fully pruned capture is a no-op',
        () async {
      final qc = await writeAckedCapture(_root, id: 'a', bytes: _bytes, startedAt: _old);
      final gate = FakeAudioGate({'memo-a': Answers.pruned});
      await run([qc], gate);
      expect(await qc.queueDir.capture.audio.exists(), isFalse);

      final again = FakeAudioGate({'memo-a': Answers.pruned});
      final report = await run([qc], again);

      expect(again.probed, isEmpty);
      expect(report.finished, 0);
      expect(report.pruned, 0);
    });

    test('a mark with no tombstone never unlinks: the pass will not delete a file '
        'that has no tombstone', () async {
      final qc = await writeAckedCapture(_root, id: 'a', bytes: _bytes, startedAt: _old);
      final record = (await qc.queueDir.read())!;
      await qc.queueDir.write(record.copyWith(locallyPrunedAt: _now));
      final gate = FakeAudioGate({'memo-a': Answers.pruned});

      await run([qc], gate);

      expect(gate.probed, isEmpty);
      expect(await qc.queueDir.capture.audio.exists(), isTrue);
      expect(logs.where((l) => l.contains('no tombstone')), hasLength(1));
    });

    test('a record that stops being acknowledged mid-probe is never marked, so the '
        'file is never unlinked', () async {
      final qc = await writeAckedCapture(_root, id: 'a', bytes: _bytes, startedAt: _old);
      final gate = FakeAudioGate({'memo-a': Answers.pruned})
        ..onProbe = (_) async {
          await qc.queueDir.write(QueueRecord.fresh(_old));
        };

      final report = await run([qc], gate);

      expect(report.pruned, 0);
      expect(await qc.queueDir.capture.audio.exists(), isTrue,
          reason: 'mark failed, so unlink must not have run');
      expect(logs.where((l) => l.contains('prune failed')), hasLength(1));
    });
  });

  group('only the file that was sent is ever eligible', () {
    test('on a salvage the trimmed file goes and the untrimmed original stays',
        () async {
      final qc = await writeAckedCapture(_root,
          id: 'a', bytes: _bytes, startedAt: _old, state: CaptureState.salvaged);
      // The untrimmed original, whose tail the server never received.
      final original = List<int>.generate(48, (i) => 200 - i);
      await qc.queueDir.capture.audio.writeAsBytes(original, flush: true);

      final gate = FakeAudioGate({'memo-a': Answers.pruned});
      await run([qc], gate);

      expect(await qc.queueDir.capture.trimmed.exists(), isFalse);
      expect(await qc.queueDir.capture.audio.readAsBytes(), original,
          reason: 'the untrimmed original is never touched by this ticket');
      expect(await listCaptureDir(qc),
          ['audio.opus', 'meta.json', 'pruned', 'upload.json']);
    });

    test('a file that is no longer the acknowledged size is neither asked about nor '
        'deleted', () async {
      final qc = await writeAckedCapture(_root, id: 'a', bytes: _bytes, startedAt: _old);
      await qc.queueDir.capture.audio
          .writeAsBytes([..._bytes, 99], flush: true); // one byte longer
      final gate = FakeAudioGate({'memo-a': Answers.pruned});

      await run([qc], gate);

      expect(gate.probed, isEmpty);
      expect(await qc.queueDir.capture.audio.exists(), isTrue);
    });

    test('an acknowledged capture whose file is already absent is left alone',
        () async {
      final qc = await writeAckedCapture(_root, id: 'a', bytes: _bytes, startedAt: _old);
      await qc.queueDir.capture.audio.delete();
      final before = await snapshot(qc);
      final gate = FakeAudioGate({'memo-a': Answers.pruned});

      await run([qc], gate);

      expect(gate.probed, isEmpty, reason: 'nothing to delete, so nothing to ask');
      expect(await snapshot(qc), before);
      expect(await qc.queueDir.capture.pruned.exists(), isFalse,
          reason: 'never claim a prune that did not happen');
    });

    test('an empty capture is never touched, whatever else is true of it', () async {
      final qc = await writeAckedCapture(_root,
          id: 'a', bytes: _bytes, startedAt: _old, state: CaptureState.empty);
      final gate = FakeAudioGate({'memo-a': Answers.pruned});

      await run([qc], gate);

      expect(gate.probed, isEmpty);
      expect(await qc.queueDir.capture.audio.exists(), isTrue);
    });

    test('CaptureDir.deleteSendable itself refuses a state whose file was never sent',
        () async {
      final qc = await writeAckedCapture(_root,
          id: 'a', bytes: _bytes, startedAt: _old, state: CaptureState.empty);
      expect(() => qc.queueDir.capture.deleteSendable(CaptureState.empty),
          throwsStateError);
      expect(() => qc.queueDir.capture.deleteSendable(CaptureState.recording),
          throwsStateError);
      expect(await qc.queueDir.capture.audio.exists(), isTrue);
    });
  });

  group('the pass ends where the send path ends', () {
    Future<List<QueueCapture>> three() async => [
          for (final id in ['a', 'b', 'c'])
            await writeAckedCapture(_root,
                id: id,
                bytes: _bytes,
                startedAt: _old.add(Duration(minutes: id.codeUnitAt(0)))),
        ];

    test('the first network failure stops the pass and records nothing', () async {
      final captures = await three();
      final gate = FakeAudioGate()..throwInstead = const SocketException('down');

      final report = await run(captures, gate);

      expect(gate.probed, hasLength(1), reason: 'the rest would fare no better');
      expect(report.endedBy, PassEndReason.network);
      for (final qc in captures) {
        expect((await qc.queueDir.read())!.lastPolledAt, isNull,
            reason: 'a flaky network must not spend anyone\'s daily poll');
        expect(await qc.queueDir.capture.audio.exists(), isTrue);
      }
    });

    test('a 401 stops the pass, records nothing, and raises no device block',
        () async {
      final captures = await three();
      final gate = FakeAudioGate()..fallback = const AudioProbeResponse(401);

      final report = await run(captures, gate);

      expect(gate.probed, hasLength(1));
      expect(report.endedBy, PassEndReason.signedOut);
      for (final qc in captures) {
        expect((await qc.queueDir.read())!.lastPolledAt, isNull);
      }
    });

    test('a 503 stops the pass but does spend that capture\'s poll', () async {
      final captures = await three();
      final gate = FakeAudioGate()..fallback = const AudioProbeResponse(503);

      await run(captures, gate);

      expect(gate.probed, hasLength(1));
      expect((await captures.first.queueDir.read())!.lastPolledAt, _now);
      expect((await captures.last.queueDir.read())!.lastPolledAt, isNull);
    });

    test('a capture the server answers 410 for does not stop the pass', () async {
      final captures = await three();
      final gate = FakeAudioGate({'memo-a': Answers.pruned, 'memo-c': Answers.pruned});

      final report = await run(captures, gate);

      expect(report.pruned, 2);
      expect(await captures[0].queueDir.capture.audio.exists(), isFalse);
      expect(await captures[1].queueDir.capture.audio.exists(), isTrue);
      expect(await captures[2].queueDir.capture.audio.exists(), isFalse);
    });
  });

  group('a lost upload.json never turns a pruned capture back into a pending one', () {
    Future<QueueCapture> prunedCapture() async {
      final qc = await writeAckedCapture(_root, id: 'a', bytes: _bytes, startedAt: _old);
      await run([qc], FakeAudioGate({'memo-a': Answers.pruned}));
      expect(await qc.queueDir.capture.audio.exists(), isFalse);
      return qc;
    }

    test('an absent upload.json is rebuilt from the tombstone as acknowledged',
        () async {
      final qc = await prunedCapture();
      await qc.queueDir.uploadFile.delete();

      final record = await qc.queueDir.readOrEnqueue(qc.capture, _now);

      expect(record.status, QueueStatus.acknowledged);
      expect(record.status, isNot(QueueStatus.pending));
      expect(record.memoId, 'memo-a');
      expect(record.locallyPrunedAt, isNotNull);
      expect((await qc.queueDir.read())!.status, QueueStatus.acknowledged,
          reason: 'and persisted, so the next read agrees');
    });

    test('a corrupt upload.json is the same', () async {
      final qc = await prunedCapture();
      await qc.queueDir.uploadFile.writeAsString('{ not json', flush: true);

      final record = await qc.queueDir.readOrEnqueue(qc.capture, _now);

      expect(record.status, QueueStatus.acknowledged);
      expect(record.memoId, 'memo-a');
    });

    test('and it is not handed to the prune pass as something to ask about',
        () async {
      final qc = await prunedCapture();
      await qc.queueDir.uploadFile.delete();
      final record = await qc.queueDir.readOrEnqueue(qc.capture, _now);
      final rebuilt = QueueCapture(
        queueDir: qc.queueDir,
        capture: qc.capture,
        queueRecord: record,
      );
      final gate = FakeAudioGate({'memo-a': Answers.pruned});

      await run([rebuilt], gate);

      expect(gate.probed, isEmpty);
    });

    test('with no tombstone either, a sendable capture whose file is absent is '
        'never written as pending -- it says so instead', () async {
      final qc = await writeFixtureCapture(_root, id: 'x', bytes: _bytes, startedAt: _old);
      await qc.queueDir.capture.audio.delete();

      final record = await qc.queueDir.readOrEnqueue(qc.capture, _now);

      expect(record.status, QueueStatus.blockedLocalFileChanged);
      expect(record.status, isNot(QueueStatus.pending));
    });

    test('an unreadable tombstone does not count: same answer as no tombstone',
        () async {
      final qc = await writeFixtureCapture(_root, id: 'x', bytes: _bytes, startedAt: _old);
      await qc.queueDir.capture.audio.delete();
      await qc.queueDir.capture.pruned.writeAsString('{"memo_id": ""}', flush: true);

      final record = await qc.queueDir.readOrEnqueue(qc.capture, _now);

      expect(record.status, QueueStatus.blockedLocalFileChanged);
    });

    test('a capture whose file IS there and has no record is still enqueued as '
        'pending, exactly as before', () async {
      final qc = await writeFixtureCapture(_root, id: 'x', bytes: _bytes, startedAt: _old);

      final record = await qc.queueDir.readOrEnqueue(qc.capture, _now);

      expect(record.status, QueueStatus.pending);
      expect(record.enqueuedAt, _now);
    });
  });
}

/// Round-trips a record through JSON the way `QueueDir` does on disk.
Map<String, Object?> jsonRoundTrip(QueueRecord r) =>
    Map<String, Object?>.from(r.toJson());

/// A [CaptureDir] that fails at exactly one step of the deletion.
class _CrashingDir extends CaptureDir {
  _CrashingDir(
    super.root,
    super.captureId, {
    this.failTombstone = false,
    this.failUnlink = false,
  });

  final bool failTombstone;
  final bool failUnlink;

  @override
  Future<void> writePruneTombstone(PruneTombstone tombstone) async {
    if (failTombstone) throw const FileSystemException('crash before the tombstone');
    await super.writePruneTombstone(tombstone);
  }

  @override
  Future<void> deleteSendable(CaptureState state) async {
    if (failUnlink) throw const FileSystemException('crash before the unlink');
    await super.deleteSendable(state);
  }
}

/// A [QueueDir] whose write fails for exactly the mark: the record that sets
/// `locallyPrunedAt`.
class _MarkFailingQueueDir extends QueueDir {
  _MarkFailingQueueDir(super.capture);

  @override
  Future<void> write(QueueRecord record) async {
    if (record.locallyPrunedAt != null) {
      throw const FileSystemException('crash before the mark');
    }
    await super.write(record);
  }
}
