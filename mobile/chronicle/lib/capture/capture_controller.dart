/// One tap to record, and everything that has to be true around it.
library;

import 'dart:async';
import 'dart:io';

import 'package:flutter/foundation.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import 'capture_channel.dart';
import 'capture_record.dart';
import 'recovery.dart';

/// Leave this much of the phone free whatever happens.
///
/// A recorder that fills the last block takes the rest of the system down with
/// it, and the memo it was saving is not worth that.
///
/// **Declared twice, deliberately, and the two must move together.** This one
/// refuses the start and is the number a person is shown;
/// `CaptureChannel.FREE_SPACE_RESERVE` sizes the recorder's `setMaxFileSize`
/// budget and is the number that actually stops a running recording. Change one
/// without the other and the app refuses at one threshold while the recorder
/// stops at a different one.
const int _reserveBytes = 256 * 1024 * 1024;

/// Plus an hour of headroom, so a refusal means "not enough for a memo" rather
/// than "not enough for another second".
///
/// Measured rather than assumed: a 13.2 s device recording came to 53,769
/// bytes, which is ~4.1 kB/s, so an hour of mono Opus is about 15 MB. (The
/// canvas's `LOCAL BUFFER · 2.1 MB` at `00:47` is decorative — the real figure
/// at that point is nearer 190 kB.)
const int _hourOfAudioBytes = 16 * 1024 * 1024;

const int _minFreeBytes = _reserveBytes + _hourOfAudioBytes;

/// Below this much captured audio, a hold is treated as a tap.
///
/// **Measured on audio captured, never on how long the finger was down.** Audio
/// does not start until the microphone actually opens, so a 450 ms hold can
/// yield 150 ms of sound; a rule about the finger would call that a deliberate
/// hold and end a memo that never really began. This way the rule is about the
/// thing it is protecting.
const int _latchBelowMs = 400;

class CaptureUiState {
  const CaptureUiState({
    this.recorder = RecorderSnapshot.idle,
    this.current,
    this.recent = const [],
    this.refusal,
    this.latched = false,
  });

  final RecorderSnapshot recorder;

  /// The record for the capture being written, if any.
  final CaptureRecord? current;

  /// Finished captures, newest first. Local only until CHRN-61 and CHRN-63.
  final List<CaptureRecord> recent;

  /// Why the last attempt to start was refused, in words with a number in them.
  final String? refusal;

  /// A hold that was too short, or was cancelled, has latched into
  /// tap-to-talk: the recording continues and a tap ends it.
  final bool latched;

  bool get isRecording => recorder.state != RecorderState.idle;

  CaptureUiState copyWith({
    RecorderSnapshot? recorder,
    CaptureRecord? current,
    List<CaptureRecord>? recent,
    String? refusal,
    bool clearRefusal = false,
    bool clearCurrent = false,
    bool? latched,
  }) =>
      CaptureUiState(
        recorder: recorder ?? this.recorder,
        current: clearCurrent ? null : (current ?? this.current),
        recent: recent ?? this.recent,
        refusal: clearRefusal ? null : (refusal ?? this.refusal),
        latched: latched ?? this.latched,
      );
}

class CaptureController extends Notifier<CaptureUiState> {
  StreamSubscription<RecorderSnapshot>? _sub;
  Timer? _reLook;

  /// A finger is down on the capture control and a start is in flight.
  ///
  /// **The gesture cannot key off `isRecording`.** Starting is asynchronous --
  /// permission, a free-space check, writing `meta.json`, then the service --
  /// and a quick tap's pointer-up lands long before any of that reports back.
  /// Gating the release on "are we recording yet" therefore dropped the release
  /// entirely for exactly the shortest gestures, which are the ones the latch
  /// rule exists to catch. Found on device.
  bool _holdPending = false;

  /// A start is in flight.
  ///
  /// `state.isRecording` cannot serve: it is the asynchronous fact this whole
  /// gesture path already learned not to trust. Without this, two pointers on
  /// the control both pass the guard and mint two captures -- the second gets a
  /// `meta.json` and no audio, which recovery then marks `empty`, the state
  /// reserved for a memo whose audio never left the encoder.
  bool _starting = false;

  /// Resolved once at build, never through `ref` after an await.
  ///
  /// `recover()` is deliberately started unawaited and does real I/O, so it can
  /// still be running when the provider is disposed. Reaching back through
  /// `ref` at that point throws — and because nobody is awaiting it, that lands
  /// in a zone with no handler. Holding the dependencies directly makes the
  /// late work harmless; [ref.mounted] guards the state writes.
  late final CapturePlatform _platform = ref.read(capturePlatformProvider);
  late final CaptureOwner _owner = ref.read(captureOwnerProvider);

  @override
  CaptureUiState build() {
    ref.onDispose(() {
      _sub?.cancel();
      _reLook?.cancel();
    });
    // The UI reads the recorder's state rather than remembering its own, which
    // is what lets a recreated UI re-attach to a recording already in flight
    // instead of showing idle over the top of one.
    _sub = _platform.watch().listen(_onSnapshot);
    // Recovery is NOT kicked off here. `main()` does it, once, at launch --
    // and reading `.notifier` there is what builds this, so a call in both
    // places ran two concurrent passes over the same directory microseconds
    // apart, each reaching `salvage()` for the same capture.
    return const CaptureUiState();
  }

  /// The last capture the recorder reported, so a stop can be attributed.
  ///
  /// An idle snapshot carries no id, and `state.current` is only ever set by
  /// this UI's own `_start` — so neither can name the capture when a recording
  /// ends some other way.
  String? _lastSeenCaptureId;

  /// Captures a finalise is already running for.
  final Set<String> _finalising = {};

  /// Watches the recorder and finalises whatever stops, however it stopped.
  ///
  /// **Every stop has to come through here, not just the in-app button.** The
  /// notification's Stop action exists precisely for when the UI is gone, and a
  /// STOP pressed after a swipe-away relaunch runs in a process where
  /// `state.current` is null. Both used to leave `meta.json` reading
  /// `recording`, which meant `refresh()` skipped it and the memo was missing
  /// from RECENT the instant somebody pressed Stop — then reappeared next
  /// launch as a `SALVAGED` duplicate, a label that says the recording was torn
  /// when it was not.
  void _onSnapshot(RecorderSnapshot snapshot) {
    if (!ref.mounted) return;
    final was = state.recorder.state;
    if (snapshot.captureId != null) _lastSeenCaptureId = snapshot.captureId;
    state = state.copyWith(recorder: snapshot);

    if (was != RecorderState.idle && snapshot.state == RecorderState.idle) {
      // Guarded for the same reason `recoverAll` is: this is unawaited, so
      // anything thrown lands in a zone with nobody to catch it and takes the
      // rest of the app's error handling with it. A capture that could not be
      // finalised now is one the next launch recovers -- the bytes are on disk
      // either way, which is the whole point of finalising being idempotent.
      unawaited(_finaliseStopped().catchError((Object e, StackTrace s) {
        debugPrint('chronicle: finalising a stopped capture failed: $e');
      }));
    }
  }

  /// Finalises the capture that just stopped, if nobody else already has.
  Future<void> _finaliseStopped() async {
    final id = _lastSeenCaptureId;
    if (id == null || !_finalising.add(id)) return;
    try {
      final root = await _platform.capturesRoot();
      final capture = CaptureDir(root, id);
      final record = await capture.readMeta();
      // Only a capture still marked `recording` needs finishing. This makes the
      // call idempotent, so the button path and this path cannot double up.
      if (record == null || record.state != CaptureState.recording) return;
      await finalise(capture, record);
      if (!ref.mounted) return;
      await refresh();
    } finally {
      _finalising.remove(id);
    }
  }

  /// Finishes anything a previous run left behind, then refreshes the list.
  Future<void> recover() async {
    final root = await _platform.capturesRoot();
    final outcome = await recoverAll(root, _owner);
    if (!ref.mounted) return;

    _reLook?.cancel();
    final wait = outcome.retryAfter;
    if (wait != null) {
      // Something was too young to classify and had no owner to ask. Look again
      // shortly; waiting for the next launch could mean waiting until tomorrow.
      _reLook = Timer(wait, () => unawaited(recover()));
    }
    await refresh();
  }

  /// Reloads the finished captures on disk, newest first.
  Future<void> refresh() async {
    final root = await _platform.capturesRoot();
    if (!await root.exists() || !ref.mounted) return;
    final records = <CaptureRecord>[];
    await for (final entry in root.list()) {
      if (entry is! Directory) continue;
      final id = entry.path.split(Platform.pathSeparator).last;
      final record = await CaptureDir(root, id).readMeta();
      if (record != null && record.state != CaptureState.recording) {
        records.add(record);
      }
    }
    records.sort((a, b) => b.startedAt.compareTo(a.startedAt));
    if (!ref.mounted) return;
    state = state.copyWith(recent: records);
  }

  /// The idle screen's primary control: start, or stop if already running.
  Future<void> toggle() async {
    if (state.isRecording) {
      await stop();
    } else {
      await _start(latched: true);
    }
  }

  /// Finger down on the capture control.
  Future<void> beginHold() {
    _holdPending = true;
    return _start(latched: false);
  }

  /// Finger lifted.
  ///
  /// A hold that captured less than [_latchBelowMs] of audio latches instead of
  /// stopping, so a brush against the control cannot produce a 200 ms memo that
  /// then needs triaging.
  Future<void> endHold() async {
    if (state.latched) return;

    // The finger lifted before the recorder even reported in. That is by
    // definition less than the threshold, so it latches -- and it must be
    // handled here rather than ignored, or the shortest gestures fall through
    // every rule and leave the controller believing a finger is still down.
    if (!state.isRecording) {
      if (_holdPending) {
        _holdPending = false;
        state = state.copyWith(latched: true);
      }
      return;
    }

    _holdPending = false;
    if (state.recorder.elapsedMs < _latchBelowMs) {
      state = state.copyWith(latched: true);
      return;
    }
    await stop();
  }

  /// The gesture was cancelled by the system rather than by the person.
  ///
  /// **A cancel is not a release.** "Release stops" is the only gesture in this
  /// app that ends a recording without anybody deciding to, and the system
  /// cancels a pointer for reasons that have nothing to do with the finger — a
  /// system gesture, palm rejection, the notification shade coming down, a call
  /// ringing. Every one of those would otherwise end a memo mid-thought, so a
  /// cancel keeps recording and latches.
  void cancelHold() {
    if (!state.isRecording && !_holdPending) return;
    _holdPending = false;
    state = state.copyWith(latched: true);
  }

  Future<void> _start({required bool latched}) async {
    if (state.isRecording || _starting) return;
    _starting = true;
    try {
      await _startInner(latched: latched);
    } finally {
      _starting = false;
    }
  }

  Future<void> _startInner({required bool latched}) async {
    // Set BEFORE the awaits, never after. Written at the end it would land
    // after a quick tap's `endHold` had already latched, and overwrite the
    // gesture's own decision with the value the gesture started with.
    state = state.copyWith(clearRefusal: true, latched: latched);

    if (!await _platform.requestPermissions()) {
      state = state.copyWith(
        refusal: 'Chronicle cannot record without the microphone. '
            'Grant it in Settings and try again.',
      );
      return;
    }

    final free = await _platform.freeBytes();
    if (free < _minFreeBytes) {
      // Named with the actual number, because a refusal somebody can act on
      // beats a recording that dies at minute nine.
      state = state.copyWith(
        refusal: 'Not enough space to record safely — '
            '${_mb(free)} free, ${_mb(_minFreeBytes)} needed.',
      );
      return;
    }

    final identity = CaptureIdentity.mint();
    final root = await _platform.capturesRoot();
    final capture = CaptureDir(root, identity.captureId);

    // Written and flushed BEFORE the recorder starts. CHRN-20 asks only that
    // the idempotency key be persisted before the request goes out; persisting
    // it before the first audio byte is stricter and free, and it means a
    // capture that survives a crash carries the key it would have had.
    final record = CaptureRecord(
      captureId: identity.captureId,
      idempotencyKey: identity.idempotencyKey,
      startedAt: DateTime.now(),
      state: CaptureState.recording,
    );
    await capture.writeMeta(record);

    await _platform.start(identity.captureId);
    if (!ref.mounted) return;
    _lastSeenCaptureId = identity.captureId;
    state = state.copyWith(current: record);
  }

  Future<void> stop() async {
    if (!state.isRecording) return;
    await _platform.stop();
    // Finalising happens in one place for every stop -- this one, the
    // notification's Stop action, and a stop after a re-attach -- so it is
    // awaited here rather than duplicated. `_finaliseStopped` is idempotent.
    await _finaliseStopped();
    if (!ref.mounted) return;
    _holdPending = false;
    state = state.copyWith(clearCurrent: true, latched: false);
    await refresh();
  }

  Future<void> pause() => _platform.pause();

  Future<void> resume() => _platform.resume();

  static String _mb(int bytes) => '${(bytes / (1024 * 1024)).round()} MB';
}

final capturePlatformProvider =
    Provider<CapturePlatform>((ref) => const CapturePlatform());

/// Who can say whether a recorder still holds a capture.
///
/// Injectable so the classification can be tested without a platform channel —
/// and because "the owner could not be asked" is a distinct answer that the
/// tests have to be able to produce on demand.
final captureOwnerProvider =
    Provider<CaptureOwner>((ref) => const PlatformCaptureOwner());

final captureControllerProvider =
    NotifierProvider<CaptureController, CaptureUiState>(CaptureController.new);
