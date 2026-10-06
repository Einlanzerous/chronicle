/// The evening nudge (CHRN-64): one notification saying how many memos are
/// waiting to be triaged. This file is the rule; `nudge_surface.dart` is the
/// platform.
///
/// **It is a poll, not a push, and that is the decision.** Catenary's R2 gate
/// measured FCM data-only `priority: HIGH` reaching a cold, deep-Doze device in
/// seconds (60/60, worst 6.07 s) -- and that is the answer to a question this
/// notification does not ask. R2 was about a chat message that has to arrive
/// *now*. This one has to arrive *once, some time this evening*: the periodic
/// WorkManager wake the upload queue already runs (`queue/background.dart`) asks
/// `GET /triage/batch` and posts locally. Doze defers that wake to a
/// maintenance window, which costs minutes inside a window that is hours wide,
/// and picking the phone up ends Doze anyway. So: no Firebase project, no
/// device-token table, no sender on the server, and nothing about a memo leaves
/// the estate to tell a phone a number.
///
/// What R2 does transfer, and is applied here:
///
///  - **The signal carries no content.** The notification is a count. The
///    transcript and the proposal are fetched by the triage screen, through the
///    one path that already reads them.
///  - **A silent zero is the instrument's claim, not the system's.** A read that
///    fails posts nothing, remembers nothing and SAYS so in the device log
///    ([runNudgePass] throws; the caller logs) -- "no notification" must never be
///    indistinguishable from "the poll has been failing for a week".
///  - **A force-stopped app receives nothing**, WorkManager included. That is
///    Android's rule and not a bug to chase; test cold with `am kill`.
///
/// The three clauses of the ticket's `Done when`, and where each lives:
///
///  - *A batch produces exactly one notification* -- [decideNudge] posts only
///    when a memo is waiting that no earlier post covered, at most once a local
///    day, and the surface posts under one fixed id.
///  - *Tapping it opens triage* -- `nudge_surface.dart` and `main.dart`.
///  - *Nothing for a memo already triaged on the web* -- the count is read from
///    the server in the same pass that posts it, a row that already carries a
///    decision is not counted ([waitingIds]), and a posted notification is
///    withdrawn when the last waiting memo is decided elsewhere.
library;

import 'package:chronicle_api/api.dart';
import 'package:shared_preferences/shared_preferences.dart';

import 'nudge_surface.dart';

/// "A sensible hour": the first wake at or after 18:00 local, and never from
/// 22:00 on. The end matters as much as the start -- a wake Doze deferred to
/// 01:30 must not buzz then; it waits for the next evening.
const nudgeWindowStartHour = 18;
const nudgeWindowEndHour = 22;

const _notifiedKey = 'chronicle.nudge.notified';
const _postedAtKey = 'chronicle.nudge.posted_at';
const _postedCountKey = 'chronicle.nudge.posted_count';

/// What the last post covered. Device-local and disposable: losing it costs one
/// repeated nudge, never a missed one.
class NudgeMemory {
  const NudgeMemory({this.notified = const {}, this.postedAt, this.postedCount = 0});

  final Set<String> notified;
  final DateTime? postedAt;
  final int postedCount;

  static NudgeMemory read(SharedPreferences prefs) {
    final at = prefs.getString(_postedAtKey);
    return NudgeMemory(
      notified: (prefs.getStringList(_notifiedKey) ?? const []).toSet(),
      postedAt: at == null ? null : DateTime.tryParse(at),
      postedCount: prefs.getInt(_postedCountKey) ?? 0,
    );
  }

  Future<void> write(SharedPreferences prefs) async {
    await prefs.setStringList(_notifiedKey, notified.toList()..sort());
    await prefs.setInt(_postedCountKey, postedCount);
    final at = postedAt;
    if (at == null) {
      await prefs.remove(_postedAtKey);
    } else {
      await prefs.setString(_postedAtKey, at.toIso8601String());
    }
  }
}

/// The memos a person could act on now: routed, and not already decided.
///
/// `absent` is a memo the Scribe has not reached -- transcribed, but with
/// nothing to confirm yet. A row with a `link` already carries a decision whose
/// journey to Switchyard has not finished; it is still in the batch, and it is
/// exactly "a memo the operator already triaged".
List<String> waitingIds(TriageBatch batch) => [
      for (final item in batch.items)
        if (item.status != 'absent' && item.link == null) item.memoId,
    ];

enum NudgeKind { none, post, cancel }

class NudgeDecision {
  const NudgeDecision(this.kind, {this.count = 0, this.atLeast = false});

  final NudgeKind kind;
  final int count;

  /// The batch came back full, so [count] is a floor: the server hands over a
  /// screen and no total.
  final bool atLeast;
}

NudgeDecision decideNudge({
  required List<String> waiting,
  required bool full,
  required NudgeMemory memory,
  required DateTime now,
}) {
  if (waiting.isEmpty) return const NudgeDecision(NudgeKind.cancel);
  if (!waiting.any((id) => !memory.notified.contains(id))) {
    return const NudgeDecision(NudgeKind.none);
  }
  if (now.hour < nudgeWindowStartHour || now.hour >= nudgeWindowEndHour) {
    return const NudgeDecision(NudgeKind.none);
  }
  final last = memory.postedAt;
  if (last != null && last.year == now.year && last.month == now.month && last.day == now.day) {
    return const NudgeDecision(NudgeKind.none);
  }
  return NudgeDecision(NudgeKind.post, count: waiting.length, atLeast: full);
}

/// One pass: read the batch, decide, act, remember.
///
/// Throws whatever [fetch] throws, before anything is posted or remembered.
/// A refused post (notifications off) is not remembered either, so turning them
/// on later still gets the evening's nudge.
Future<NudgeDecision> runNudgePass({
  required Future<TriageBatch?> Function() fetch,
  required NudgeSurface surface,
  required SharedPreferences prefs,
  required DateTime now,
}) async {
  final batch = await fetch();
  if (batch == null) throw StateError('nudge: the triage batch came back empty-bodied');

  final waiting = waitingIds(batch);
  final full = batch.items.length >= batch.limit;
  final memory = NudgeMemory.read(prefs);
  final decision = decideNudge(waiting: waiting, full: full, memory: memory, now: now);

  switch (decision.kind) {
    case NudgeKind.cancel:
      await surface.cancel();
      if (memory.notified.isNotEmpty || memory.postedCount != 0) {
        await NudgeMemory(postedAt: memory.postedAt).write(prefs);
      }
    case NudgeKind.post:
      if (await surface.post(count: decision.count, atLeast: decision.atLeast)) {
        await NudgeMemory(
          notified: waiting.toSet(),
          postedAt: now,
          postedCount: decision.count,
        ).write(prefs);
      }
    case NudgeKind.none:
      // A notification still in the shade says a number, and a number that is
      // no longer true is a copy that lies. Corrected in place, silently, and
      // only while it is still there -- a dismissed one is never brought back.
      if (memory.postedCount != waiting.length && await surface.showing()) {
        await surface.update(count: waiting.length, atLeast: full);
        await NudgeMemory(
          notified: {...memory.notified, ...waiting},
          postedAt: memory.postedAt,
          postedCount: waiting.length,
        ).write(prefs);
      }
  }
  return decision;
}
