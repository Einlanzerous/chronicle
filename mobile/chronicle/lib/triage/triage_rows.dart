/// Pure batch-triage rules (CHRN-63, board 1a's B2), kept out of the widgets so
/// the rules the screen exists to hold are unit testable without mounting
/// anything.
///
/// **This is a port of `web/src/lib/triage.ts` (CHRN-55), not a second
/// opinion.** The two clients talk to one API and must agree on what a row
/// is, which statuses leave it pending and what an override has to carry, so
/// where this file differs from that one it is because the phone does less
/// (no DEFERRED list, no keyboard), never because it reads the contract
/// differently. The facts that shape everything below:
///
///  - AN OVERRIDE IS NOT A PATCH. The server builds the decision from the
///    override alone and validates it like a model's, so an edit sends every
///    field its destination requires.
///  - A DISCARD IS TERMINAL and is never accepted as shown: it takes an
///    override, and the phone holds it back for [discardUndo] before sending
///    (the server has no undo; see the controller).
///  - `confirm_edit` is legal on an append or a supersede and on nothing else.
///    It is set for ONE row at a time, never by ACCEPT ALL.
///  - Only `applied` takes a row out of the batch. The other four statuses
///    leave it VISIBLY STILL PENDING, each saying why.
library;

import 'package:chronicle_api/api.dart';

/// The board's `DISCARDED · UNDO 10 MIN`.
const discardUndo = Duration(minutes: 10);

/// Switchyard's ticket types, as `internal/scribe` holds them.
const ticketTypes = ['task', 'bug', 'spike', 'epic'];

/// What a decision the server did not land says, by its own status.
enum ProblemStatus { needsInput, stale, refused, failed }

/// What this session has done to a row, on top of what the batch said.
sealed class LocalState {
  const LocalState();
}

class Pending extends LocalState {
  const Pending();
}

class Sending extends LocalState {
  const Sending();
}

class Accepted extends LocalState {
  const Accepted({required this.edited, required this.result, required this.at});
  final bool edited;
  final TriageResult result;
  final DateTime at;
}

class Held extends LocalState {
  const Held(this.reason);
  final String reason;
}

class Discarding extends LocalState {
  const Discarding(this.due);
  final DateTime due;
}

class Discarded extends LocalState {
  const Discarded();
}

class Problem extends LocalState {
  const Problem(this.status, this.reason);
  final ProblemStatus status;
  final String reason;
}

class TriageRow {
  const TriageRow({required this.item, this.local = const Pending(), this.draft, this.notice});

  final BatchItem item;
  final LocalState local;

  /// What the person typed, kept when an edit did not land so EDIT reopens it
  /// rather than the proposal.
  final Draft? draft;

  /// One line carried across a re-read -- "the proposal changed".
  final String? notice;

  String get memoId => item.memoId;

  TriageRow copyWith({
    BatchItem? item,
    LocalState? local,
    Draft? draft,
    bool clearDraft = false,
    String? notice,
    bool clearNotice = false,
  }) =>
      TriageRow(
        item: item ?? this.item,
        local: local ?? this.local,
        draft: clearDraft ? null : (draft ?? this.draft),
        notice: clearNotice ? null : (notice ?? this.notice),
      );
}

/// The drawn states: the board's, plus `sending` while a request is out.
enum RowKind {
  prefilled,
  needsInput,
  discardProposed,
  sending,
  accepted,
  held,
  discarding,
  discarded,
  linkInFlight,
  linkUnresolved,
  linkAmbiguous,
}

RowKind rowKind(TriageRow row) {
  final local = row.local;
  if (local is Sending) return RowKind.sending;
  if (local is Accepted) return RowKind.accepted;
  if (local is Held) return RowKind.held;
  if (local is Discarding) return RowKind.discarding;
  if (local is Discarded) return RowKind.discarded;
  // A decision ALREADY RECORDED outranks the proposal: the memo is waiting for
  // the sweep, not for a person. `refused` is the exception -- Switchyard
  // caches the refusal, so the remedy is a changed decision.
  switch (row.item.link?.state) {
    case 'in_flight':
      return RowKind.linkInFlight;
    case 'unresolved':
      return RowKind.linkUnresolved;
    case 'ambiguous':
      return RowKind.linkAmbiguous;
  }
  // A transient failure of an accept-as-shown is retried as it was; everything
  // else the server sent back needs the editor.
  if (local is Problem && (local.status != ProblemStatus.failed || row.draft != null)) {
    return RowKind.needsInput;
  }
  final p = row.item.proposal;
  if (row.item.status != 'valid' || p == null) return RowKind.needsInput;
  if (p.destination == ProposalDestinationEnum.DISCARD) return RowKind.discardProposed;
  return RowKind.prefilled;
}

/// Whether a person can still decide this row from this screen.
bool isDecidable(TriageRow row) {
  final k = rowKind(row);
  return k == RowKind.prefilled || k == RowKind.needsInput || k == RowKind.discardProposed;
}

/// Whether the row still wants a decision from somebody. A discard inside its
/// undo window does not: the person has decided, and the row says so.
bool isWaiting(TriageRow row) {
  final l = row.local;
  return l is! Accepted && l is! Held && l is! Discarding && l is! Discarded;
}

/// Whether ACCEPT ALL takes this row. `pre_acceptable` is the server's -- the
/// one reader of the confidence threshold -- and this only adds "and nobody
/// has touched it here". A row the server already answered about is never
/// swept up again by a batch key.
bool isPrefilled(TriageRow row) =>
    row.local is Pending && rowKind(row) == RowKind.prefilled && row.item.preAcceptable;

/// append and supersede write into a note somebody already wrote.
bool editsExistingNote(Proposal? p) =>
    p != null &&
    p.destination == ProposalDestinationEnum.NOTE &&
    (p.verb == ProposalVerbEnum.append || p.verb == ProposalVerbEnum.supersede);

// -- Decisions ---------------------------------------------------------------

/// Accept as shown. `single` is set for ONE row at a time and never by the
/// batch path -- the half of the rule the server cannot check.
TriageDecision acceptDecision(BatchItem item, {required bool single}) => TriageDecision(
      memoId: item.memoId,
      proposer: item.proposer,
      generation: item.generation,
      confirmEdit: single && editsExistingNote(item.proposal) ? true : null,
    );

TriageDecision discardDecision(BatchItem item) => TriageDecision(
      memoId: item.memoId,
      proposer: item.proposer,
      generation: item.generation,
      proposalOverride: Override(destination: 'DISCARD'),
    );

TriageDecision editDecision(BatchItem item, Draft d) => TriageDecision(
      memoId: item.memoId,
      proposer: item.proposer,
      generation: item.generation,
      proposalOverride: buildOverride(d),
    );

/// What the editor holds. One `text` for the three destinations that carry
/// prose, so changing NOTE to TICKET does not throw the draft away.
class Draft {
  const Draft({
    this.destination = 'NOTE',
    this.title = '',
    this.pagePath = '',
    this.projectKey = '',
    this.ticketType = 'task',
    this.verb = 'create',
    this.targetNote = '',
    this.text = '',
  });

  final String destination;
  final String title;
  final String pagePath;
  final String projectKey;
  final String ticketType;
  final String verb;
  final String targetNote;
  final String text;

  Draft copyWith({
    String? destination,
    String? title,
    String? pagePath,
    String? projectKey,
    String? ticketType,
    String? verb,
    String? targetNote,
    String? text,
  }) =>
      Draft(
        destination: destination ?? this.destination,
        title: title ?? this.title,
        pagePath: pagePath ?? this.pagePath,
        projectKey: projectKey ?? this.projectKey,
        ticketType: ticketType ?? this.ticketType,
        verb: verb ?? this.verb,
        targetNote: targetNote ?? this.targetNote,
        text: text ?? this.text,
      );
}

Draft draftFor(BatchItem item) {
  final p = item.proposal;
  String firstNonEmpty(List<String?> xs) => xs.firstWhere((s) => s != null && s.isNotEmpty, orElse: () => '') ?? '';
  return Draft(
    destination: p?.destination.value ?? 'NOTE',
    title: p?.title ?? '',
    // `page_path` when the proposal still has one, else the board's
    // "pre-filled from nearest_page".
    pagePath: p?.pagePath ?? p?.nearestPage ?? '',
    projectKey: p?.projectKey ?? '',
    ticketType: (p?.ticketType == null || p!.ticketType!.isEmpty) ? 'task' : p.ticketType!,
    verb: p?.verb?.value ?? 'create',
    targetNote: p?.targetNote ?? '',
    text: firstNonEmpty([p?.body, p?.description, p?.openingPost, item.excerpt]),
  );
}

/// The label over the editor's prose field, per destination.
String textLabel(String destination) => switch (destination) {
      'NOTE' => 'BODY',
      'TICKET' => 'DESCRIPTION',
      'DISCUSSION' => 'OPENING POST',
      _ => 'TEXT',
    };

/// The first thing wrong with a draft, in a person's words, or null. The
/// server validates again and is the authority; this only saves a round trip
/// for the blanks it would refuse anyway.
String? validateDraft(Draft d) {
  if (d.destination == 'DISCARD') return null;
  if (d.title.trim().isEmpty) return 'A title is required.';
  if (d.text.trim().isEmpty) return 'The ${textLabel(d.destination).toLowerCase()} is required.';
  if (d.destination == 'TICKET' && d.projectKey.trim().isEmpty) {
    return 'A ticket needs a project key — it cannot be moved between projects afterwards.';
  }
  if (d.destination == 'NOTE') {
    if (d.verb != 'create' && d.targetNote.trim().isEmpty) {
      return 'A note reference is required to ${d.verb}.';
    }
    if (d.verb == 'create' && d.pagePath.trim().isEmpty) {
      return 'A note must name the page it belongs on.';
    }
  }
  return null;
}

Override buildOverride(Draft d) {
  switch (d.destination) {
    case 'TICKET':
      return Override(
        destination: 'TICKET',
        title: d.title.trim(),
        projectKey: d.projectKey.trim().toUpperCase(),
        ticketType: d.ticketType,
        description: d.text,
      );
    case 'DISCUSSION':
      return Override(destination: 'DISCUSSION', title: d.title.trim(), openingPost: d.text);
    case 'NOTE':
      final verb = OverrideVerbEnum.values.firstWhere(
        (v) => v.value == d.verb,
        orElse: () => OverrideVerbEnum.create,
      );
      return Override(
        destination: 'NOTE',
        title: d.title.trim(),
        verb: verb,
        body: d.text,
        targetNote: d.verb == 'create' ? null : d.targetNote.trim(),
        pagePath: d.pagePath.trim().isEmpty ? null : normalisePagePath(d.pagePath),
      );
    default:
      return Override(destination: 'DISCARD');
  }
}

/// The board draws a path as `estate / storage / amber`; the API takes
/// `estate/storage/amber`. Accept either from a person's hands.
String normalisePagePath(String path) =>
    path.split('/').map((s) => s.trim()).where((s) => s.isNotEmpty).join('/');

// -- Results -----------------------------------------------------------------

/// Folds one per-item result back into its row. FIVE STATUSES AND THEY STAY
/// FIVE on the screen: only `applied` takes the row out of the batch. The
/// other four leave it VISIBLY STILL PENDING, each saying why -- the ticket's
/// second Done-when, and the reason there is no batch-wide status.
TriageRow applyResult(TriageRow row, TriageResult result, {required bool edited, required DateTime now}) {
  switch (result.status) {
    case 'applied':
      return row.copyWith(clearNotice: true, local: Accepted(edited: edited, result: result, at: now));
    case 'needs_input':
      // Carries the POST-BUMP generation, so the completed resend is not
      // `stale` without an intervening GET.
      final item = row.item
        ..status = 'needs_input'
        ..preAcceptable = false
        ..generation = result.generation ?? row.item.generation
        ..clearedFields = result.cleared.isNotEmpty ? result.cleared : row.item.clearedFields;
      return row.copyWith(
        item: item,
        local: Problem(
          ProblemStatus.needsInput,
          _reason(result.reason, 'A target no longer resolves — supply it and confirm.'),
        ),
      );
    case 'stale':
      return row.copyWith(
        local: const Problem(ProblemStatus.stale, 'The proposal changed since this screen read it.'),
      );
    case 'refused':
      return row.copyWith(local: Problem(ProblemStatus.refused, _reason(result.reason, 'Refused.')));
    default:
      return row.copyWith(
        local: Problem(ProblemStatus.failed, _reason(result.reason, 'It did not land. Nothing was lost.')),
      );
  }
}

String _reason(String? given, String fallback) => (given == null || given.isEmpty) ? fallback : given;

/// A request that never produced per-item results at all.
TriageRow failRow(TriageRow row, String reason) =>
    row.copyWith(local: Problem(ProblemStatus.failed, reason));

/// Folds a fresh batch into the rows on screen. What this session decided
/// stays as it is drawn. A row still waiting takes the server's current word
/// -- which is what un-sticks a `stale` one -- and a waiting row the server no
/// longer lists was decided somewhere else, so it leaves. New memos append.
List<TriageRow> mergeBatch(List<TriageRow> rows, List<BatchItem> items) {
  final fresh = {for (final it in items) it.memoId: it};
  final out = <TriageRow>[];
  final seen = <String>{};
  for (final row in rows) {
    seen.add(row.memoId);
    final it = fresh[row.memoId];
    final settled = row.local is! Pending && row.local is! Problem;
    if (settled) {
      out.add(row);
    } else if (it != null) {
      final local = row.local;
      final wasStale = local is Problem && local.status == ProblemStatus.stale;
      final moved = it.generation != row.item.generation;
      out.add(row.copyWith(
        item: it,
        // A refusal or a failure is about THIS decision and outlives a
        // re-read; a stale one is answered by the re-read itself.
        local: wasStale || (moved && local is Problem) ? const Pending() : local,
        notice: wasStale || moved ? 'PROPOSAL CHANGED SINCE YOU READ IT' : row.notice,
      ));
    }
  }
  for (final it in items) {
    if (!seen.contains(it.memoId)) out.add(TriageRow(item: it));
  }
  return out;
}

// -- Counts ------------------------------------------------------------------

class TriageCounts {
  int waiting = 0;
  int prefilled = 0;
  int needInput = 0;

  /// Decisions that did not finish landing: this session's refusals and
  /// failures, and the links the sweep has not resolved.
  int failed = 0;
  int accepted = 0;
  int edited = 0;
  int held = 0;
  int discarded = 0;
}

TriageCounts countRows(List<TriageRow> rows) {
  final c = TriageCounts();
  for (final row in rows) {
    if (isWaiting(row)) c.waiting++;
    switch (rowKind(row)) {
      case RowKind.accepted:
        final l = row.local;
        if (l is Accepted && l.edited) {
          c.edited++;
        } else {
          c.accepted++;
        }
      case RowKind.held:
        c.held++;
      case RowKind.discarding || RowKind.discarded:
        c.discarded++;
      case RowKind.linkUnresolved || RowKind.linkAmbiguous:
        c.failed++;
      case RowKind.prefilled || RowKind.needsInput || RowKind.discardProposed:
        final l = row.local;
        final problem = l is Problem ? l.status : null;
        if (problem == ProblemStatus.failed ||
            problem == ProblemStatus.refused ||
            row.item.link?.state == 'refused') {
          c.failed++;
        } else if (isPrefilled(row)) {
          c.prefilled++;
        } else {
          c.needInput++;
        }
      case RowKind.sending || RowKind.linkInFlight:
        break;
    }
  }
  return c;
}

/// `40 MEMOS · 31 PRE-FILLED · 6 NEED INPUT · 3 FAILED`. [more] is the batch
/// cap talking: the server hands over one screen (25) and no total, so a full
/// screen reads `25+` rather than claiming 25 is all there is.
String headerCounts(TriageCounts c, {required bool more}) {
  final noun = c.waiting == 1 && !more ? 'MEMO' : 'MEMOS';
  return '${c.waiting}${more ? '+' : ''} $noun · ${c.prefilled} PRE-FILLED · '
      '${c.needInput} NEED INPUT · ${c.failed} FAILED';
}

String footerCounts(TriageCounts c, {required bool more}) =>
    '${c.accepted} ACCEPTED · ${c.edited} EDITED · ${c.held} HELD · '
    '${c.discarded} DISCARDED · ${c.waiting}${more ? '+' : ''} REMAINING';

// -- Formatting --------------------------------------------------------------

/// `0:41`, `12:05`, `1:02:09` -- or an em dash when nothing measured it.
String formatDuration(int? ms) {
  if (ms == null || ms <= 0) return '—';
  final total = (ms / 1000).round();
  final h = total ~/ 3600;
  final m = (total % 3600) ~/ 60;
  final s = (total % 60).toString().padLeft(2, '0');
  return h > 0 ? '$h:${m.toString().padLeft(2, '0')}:$s' : '$m:$s';
}

/// The earliest year a capture time is believed. A device clock can assert any
/// value from year 100 to 9900 (CHRN-118) and nothing upstream filters an
/// implausible-but-in-range one, so anything outside the window below is shown
/// as unknown rather than as a date it never was.
const _earliestPlausibleYear = 2020;

/// The moment a row's time label should be drawn from, or null when the clock
/// that wrote it cannot be believed: before [_earliestPlausibleYear], or more
/// than a day ahead of [now]. Local time, like the rest of Chronicle.
DateTime? plausibleTime(DateTime? at, DateTime now) {
  if (at == null) return null;
  final local = at.toLocal();
  if (local.year < _earliestPlausibleYear) return null;
  if (local.isAfter(now.add(const Duration(days: 1)))) return null;
  return local;
}

/// `14:05`, or `--:--` when [plausibleTime] says the clock is not believable.
String formatClock(DateTime? at, DateTime now) {
  final t = plausibleTime(at, now);
  if (t == null) return '--:--';
  return '${t.hour.toString().padLeft(2, '0')}:${t.minute.toString().padLeft(2, '0')}';
}

/// The batch's date line: `TODAY`, `YESTERDAY`, `MON 29 SEP`, or, when the
/// memos span days, the oldest one's. Unbelievable times are skipped, and a
/// batch with none says nothing rather than guessing.
String? batchDayLabel(Iterable<DateTime> times, DateTime now) {
  DateTime? oldest;
  for (final raw in times) {
    final t = plausibleTime(raw, now);
    if (t != null && (oldest == null || t.isBefore(oldest))) oldest = t;
  }
  if (oldest == null) return null;
  DateTime day(DateTime d) => DateTime(d.year, d.month, d.day);
  final diff = day(now).difference(day(oldest)).inDays;
  if (diff == 0) return 'TODAY';
  if (diff == 1) return 'YESTERDAY';
  const dow = ['MON', 'TUE', 'WED', 'THU', 'FRI', 'SAT', 'SUN'];
  const mon = ['JAN', 'FEB', 'MAR', 'APR', 'MAY', 'JUN', 'JUL', 'AUG', 'SEP', 'OCT', 'NOV', 'DEC'];
  return '${dow[oldest.weekday - 1]} ${oldest.day} ${mon[oldest.month - 1]}';
}

String formatConfidence(double c) => c.toStringAsFixed(2);

/// `SCRIBE · GEMMA4:E4B · GEN 2`. The proposer is the server's own
/// runner-qualified string, runner prefix dropped.
String proposerLabel(BatchItem item) {
  final p = item.proposer;
  final model = p.contains('/') ? p.substring(p.indexOf('/') + 1) : p;
  return [
    'SCRIBE',
    if (model.isNotEmpty) model.toUpperCase(),
    if (item.generation != null) 'GEN ${item.generation}',
  ].join(' · ');
}

/// `DISC` in the tag, as the board abbreviates it.
String destinationTag(String destination) => destination == 'DISCUSSION' ? 'DISC' : destination;

/// Whole minutes left in a discard's undo window, never below 1 while it is
/// still open -- "0 MIN" would read as already gone.
int undoMinutesLeft(DateTime due, DateTime now) {
  final ms = due.difference(now).inMilliseconds;
  final m = (ms / 60000).ceil();
  return m < 1 ? 1 : m;
}

/// `4 MIN` / `2 HR` / `3 DAYS`; callers compose the sentence.
String relativeAge(DateTime at, DateTime now) {
  final minutes = now.difference(at).inMinutes;
  if (minutes < 1) return '<1 MIN';
  if (minutes < 60) return '$minutes MIN';
  final hours = minutes ~/ 60;
  if (hours < 24) return '$hours HR';
  final days = hours ~/ 24;
  return '$days DAY${days == 1 ? '' : 'S'}';
}
