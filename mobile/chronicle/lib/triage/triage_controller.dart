/// The state behind the triage screen (CHRN-63): reads the batch, sends
/// decisions, folds each per-item answer back into its row.
///
/// What it holds to, all of it mirroring `web/src/views/TriageView.vue`
/// (CHRN-55):
///
///  - A ROW LEAVES THE BATCH ONLY ON `applied`. The POST answers one status per
///    item and four of the five leave the memo undecided, so each is kept
///    pending with the server's own reason ([applyResult]).
///  - A NETWORK FAILURE IS NOT A "NO". Work the server started is detached and
///    durable, and a replay answers `applied` from the recorded decision, so a
///    request that never answered leaves the row pending and says retry.
///  - A DISCARD IS HELD BACK, NOT RECALLED. `discarded` is terminal on the
///    server and there is no undo operation, so `UNDO 10 MIN` is honest only
///    one way: the decision is not sent until the window closes, the screen is
///    left, or the next batch is loaded. Kill the app inside the window and the
///    memo is simply still waiting -- the failure falls on the side of keeping
///    what somebody said.
///  - THE BATCH IS ONE SCREEN. The server hands over 25 and no total, so the
///    next memos load when this screen's are decided, and a full batch counts
///    as "at least".
///  - A TICKET IS LINKED, NEVER COPIED (CLAUDE.md invariant 2). Accepting a
///    TICKET route keeps only its key and URL, and the card under it resolves
///    the ticket's own state at render time, carrying its age.
library;

import 'dart:async';

import 'package:chronicle_api/api.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:url_launcher/url_launcher.dart';

import '../api/providers.dart';
import 'triage_rows.dart';

/// How a ticket's card reads. Colour is the widget's business; this is the
/// facts, so "unreachable says so" is testable without pixels.
class TicketCard {
  const TicketCard({
    required this.key,
    this.url,
    this.state,
    this.title,
    this.stateWord,
    this.fetchedAt,
    this.lastResolvedAt,
    this.explain,
  });

  final String key;

  /// Where tapping the card goes. Prefers the upstream's own URL, falls back to
  /// the one the accept answered with.
  final String? url;

  /// `resolved`, `broken`, `unreachable`, `unconfigured`, `unchecked`.
  final String? state;
  final String? title;

  /// The upstream's state word exactly as returned (`IN PROGRESS`), never
  /// composed here.
  final String? stateWord;
  final DateTime? fetchedAt;
  final DateTime? lastResolvedAt;
  final String? explain;
}

class TriageState {
  const TriageState({
    this.rows = const [],
    this.more = false,
    this.loading = false,
    this.loaded = false,
    this.loadError,
    this.cards = const {},
  });

  final List<TriageRow> rows;

  /// The batch came back full: more memos wait behind it.
  final bool more;
  final bool loading;
  final bool loaded;
  final String? loadError;

  /// Resolved ticket cards by ticket key.
  final Map<String, TicketCard> cards;

  TriageState copyWith({
    List<TriageRow>? rows,
    bool? more,
    bool? loading,
    bool? loaded,
    String? loadError,
    bool clearError = false,
    Map<String, TicketCard>? cards,
  }) =>
      TriageState(
        rows: rows ?? this.rows,
        more: more ?? this.more,
        loading: loading ?? this.loading,
        loaded: loaded ?? this.loaded,
        loadError: clearError ? null : (loadError ?? this.loadError),
        cards: cards ?? this.cards,
      );
}

/// Opens a URL outside the app. A provider so tests can see the tap land
/// without a platform channel.
typedef UrlOpener = Future<bool> Function(Uri url);

final urlOpenerProvider = Provider<UrlOpener>(
  (ref) => (url) => launchUrl(url, mode: LaunchMode.externalApplication),
);

/// The clock, so the discard window and card ages are testable.
final triageClockProvider = Provider<DateTime Function()>((ref) => DateTime.now);

class _Outgoing {
  const _Outgoing(this.decision, {required this.edited, this.discard = false});
  final TriageDecision decision;
  final bool edited;
  final bool discard;
  String get memoId => decision.memoId;
}

class TriageController extends Notifier<TriageState> {
  bool _refilling = false;

  @override
  TriageState build() => const TriageState();

  DateTime _now() => ref.read(triageClockProvider)();

  String _message(Object err, String fallback) {
    if (err is ApiException && err.message != null && err.message!.isNotEmpty) return err.message!;
    return fallback;
  }

  void _update(String memoId, TriageRow Function(TriageRow) fn) {
    state = state.copyWith(rows: [for (final r in state.rows) r.memoId == memoId ? fn(r) : r]);
  }

  TriageRow? row(String memoId) {
    for (final r in state.rows) {
      if (r.memoId == memoId) return r;
    }
    return null;
  }

  // -- The batch ---------------------------------------------------------------

  Future<void> load() async {
    state = state.copyWith(loading: true);
    try {
      final batch = await ref.read(triageApiProvider).getTriageBatch();
      if (!ref.mounted) return;
      if (batch == null) {
        state = state.copyWith(loading: false, loadError: 'The triage batch could not be read.');
        return;
      }
      state = state.copyWith(
        rows: mergeBatch(state.rows, batch.items),
        more: batch.items.length >= batch.limit,
        loading: false,
        loaded: true,
        clearError: true,
      );
    } on ApiException catch (e) {
      if (!ref.mounted) return;
      state = state.copyWith(loading: false, loadError: _message(e, 'The triage batch could not be read.'));
    } catch (_) {
      if (!ref.mounted) return;
      state = state.copyWith(loading: false, loadError: 'Chronicle could not be reached. Nothing was decided.');
    }
  }

  // -- Deciding ----------------------------------------------------------------

  /// ONE TAP: accept this memo as shown. The per-item path, so `confirm_edit`
  /// is set for an append or a supersede -- the tap is the confirmation.
  Future<void> accept(String memoId) {
    final r = row(memoId);
    if (r == null || rowKind(r) != RowKind.prefilled) return Future.value();
    return _send([_Outgoing(acceptDecision(r.item, single: true), edited: false)]);
  }

  /// ACCEPT ALL: every row the server says it is confident about and nobody
  /// has touched. NEVER `single`: `confirm_edit` is the per-item confirmation
  /// an append or a supersede costs, and a batch key must not set it.
  Future<void> acceptAll() {
    final batch = state.rows.where(isPrefilled).toList();
    if (batch.isEmpty) return Future.value();
    return _send([for (final r in batch) _Outgoing(acceptDecision(r.item, single: false), edited: false)]);
  }

  /// A tap on a lane. The Scribe's pick is already filled, so this is the
  /// override: tapping another lane stages that destination (sent by [fileAll],
  /// not now), tapping the proposal's own lane puts the row back to the Scribe's
  /// pick. Changing a row the server refused is a new decision, so the refusal
  /// is cleared and FILE counts it as new rather than as a retry.
  ///
  /// What the new destination needs that the proposal did not carry (a note's
  /// page, a ticket's project) is left blank, and the row says it needs input
  /// ([stagedProblem]) instead of sending a decision the server would refuse.
  void pickLane(String memoId, String lane) {
    final r = row(memoId);
    if (r == null || !isDecidable(r)) return;
    final staged = draftForLane(r, lane);
    if (staged == null) {
      _update(memoId, (x) => _changed(x.copyWith(clearDraft: true, clearNotice: true)));
    } else {
      _update(memoId, (x) => _changed(x.copyWith(draft: staged, clearNotice: true)));
    }
  }

  /// A row the person has just changed is no longer the decision the server
  /// refused (or asked for input on). A `failed` one stays failed: that was the
  /// network's doing, not the decision's, and FILE is still its retry. (FILE
  /// is an unchanged refused row's retry too; changing it only makes it new.)
  TriageRow _changed(TriageRow r) {
    final l = r.local;
    return l is Problem && l.status != ProblemStatus.failed ? r.copyWith(local: const Pending()) : r;
  }

  /// Stages the editor's draft for [fileAll]. An override is not a patch, so the
  /// draft carries every field its destination requires.
  void stage(String memoId, Draft draft) {
    final r = row(memoId);
    if (r == null || !isDecidable(r)) return;
    _update(memoId, (x) => _changed(x.copyWith(draft: draft, clearNotice: true)));
  }

  /// What FILE would do, for its label: rows it would send, rows whose last
  /// send failed or was refused and it would send again (`retry`), discards,
  /// and the refused rows it will not send as they stand, which it reports.
  ({int file, int retry, int discard, int refused}) filing() {
    var file = 0, retry = 0, discard = 0, refused = 0;
    for (final r in state.rows) {
      final l = r.local;
      final again = l is Problem && retriable(l);
      switch (filingOf(r)) {
        case Filing.file:
          again ? retry++ : file++;
        case Filing.discard:
          again ? retry++ : discard++;
        case Filing.none:
          if (l is Problem && l.status == ProblemStatus.refused) refused++;
      }
    }
    return (file: file, retry: retry, discard: discard, refused: refused);
  }

  /// FILE: the commit for the lanes as they stand. Everything [filingOf] takes
  /// goes in one request -- accepted as shown, or as the person's override -- and
  /// the discards enter their undo window. NEVER `single`: `confirm_edit` is the
  /// per-item confirmation an append or a supersede costs, so a row that needs
  /// it comes back `refused` and stays pending, to be confirmed on its own.
  Future<void> fileAll() async {
    final out = <_Outgoing>[];
    final discards = <String>[];
    for (final r in state.rows) {
      switch (filingOf(r)) {
        case Filing.file:
          final d = r.draft;
          out.add(d == null
              ? _Outgoing(acceptDecision(r.item, single: false), edited: false)
              : _Outgoing(editDecision(r.item, d), edited: true));
        case Filing.discard:
          // A discard whose send already failed has had its window: filing it
          // again sends it now, rather than holding it another ten minutes.
          if (r.local is Problem) {
            out.add(_Outgoing(discardDecision(r.item), edited: false, discard: true));
          } else {
            discards.add(r.memoId);
          }
        case Filing.none:
          break;
      }
    }
    discards.forEach(discard);
    await _send(out);
  }

  Future<void> _send(List<_Outgoing> out) async {
    if (out.isEmpty) return;
    for (final o in out) {
      _update(o.memoId, (r) => r.copyWith(local: const Sending()));
    }

    var anyStale = false;
    try {
      final res = await ref.read(triageApiProvider).acceptTriage(
            AcceptRequest(items: [for (final o in out) o.decision]),
          );
      if (!ref.mounted) return;
      if (res == null) {
        for (final o in out) {
          _update(o.memoId, (r) => failRow(r, 'The decision was not accepted.'));
        }
      } else {
        // One result per item, each carrying its memo_id -- matched by id so a
        // short or reordered answer cannot land a status on the wrong row.
        final byMemo = {for (final r in res.results) r.memoId: r};
        final now = _now();
        for (final o in out) {
          final result = byMemo[o.memoId];
          _update(o.memoId, (r) {
            if (result == null) {
              return failRow(r, 'The server did not answer for this memo. It is still pending.');
            }
            if (result.status == 'stale') anyStale = true;
            if (o.discard && result.status == 'applied') {
              return r.copyWith(clearNotice: true, local: const Discarded());
            }
            final next = applyResult(r, result, edited: o.edited, now: now);
            return next.local is Accepted ? next.copyWith(clearDraft: true) : next;
          });
          if (result != null && result.status == 'applied') unawaited(_resolveTicket(result));
        }
      }
    } on ApiException catch (e) {
      if (!ref.mounted) return;
      final reason = _message(e, 'The decision was not accepted.');
      for (final o in out) {
        _update(o.memoId, (r) => failRow(r, reason));
      }
    } catch (_) {
      if (!ref.mounted) return;
      // A network failure says nothing about whether the server started the
      // work: a replay answers `applied` from the recorded decision.
      for (final o in out) {
        _update(
          o.memoId,
          (r) => failRow(r, 'Chronicle could not be reached. Retry — a decision that did land answers as landed.'),
        );
      }
    }

    // A stale item must be re-shown, not decided blind.
    if (anyStale) {
      await load();
    } else {
      await _refillIfDone();
    }
  }

  // -- Hold and release --------------------------------------------------------

  Future<void> hold(String memoId, {String? reason}) async {
    final r = row(memoId);
    if (r == null || !isDecidable(r)) return;
    final before = r.local;
    _update(memoId, (x) => x.copyWith(local: const Sending()));
    try {
      final item = await ref.read(triageApiProvider).holdMemo(HoldRequest(memoId: memoId, reason: reason));
      if (!ref.mounted) return;
      _update(memoId, (x) => x.copyWith(local: Held(item?.reason ?? reason ?? '')));
      await _refillIfDone();
    } catch (e) {
      if (!ref.mounted) return;
      // Not a failed DECISION: the row goes back to what it was, and says so.
      final why = e is ApiException ? _message(e, 'the hold was not recorded') : 'CHRONICLE UNREACHABLE';
      _update(memoId, (x) => x.copyWith(local: before, notice: 'HOLD NOT RECORDED · $why'));
    }
  }

  Future<void> release(String memoId) async {
    final r = row(memoId);
    if (r == null || r.local is! Held) return;
    try {
      await ref.read(triageApiProvider).releaseMemo(ReleaseRequest(memoId: memoId));
    } catch (e) {
      if (!ref.mounted) return;
      final why = e is ApiException ? _message(e, 'still held') : 'CHRONICLE UNREACHABLE';
      _update(memoId, (x) => x.copyWith(notice: 'NOT RELEASED · $why'));
      return;
    }
    if (!ref.mounted) return;
    _update(memoId, (x) => x.copyWith(clearNotice: true, local: const Pending()));
  }

  // -- Discard, with its undo window -------------------------------------------

  /// Holds a discard for [discardUndo]. Nothing is sent yet, so the row says it
  /// WILL discard and when -- never that it has: `DISCARDED` is for a discard the
  /// server answered `applied` ([Discarded]). The choice stays on the row as a
  /// DISCARD draft, so a send that fails leaves the row a discard still pending
  /// and not a proposal for something else.
  void discard(String memoId) {
    final r = row(memoId);
    if (r == null || !isDecidable(r)) return;
    _update(
      memoId,
      (x) => x.copyWith(
        clearNotice: true,
        draft: const Draft(destination: 'DISCARD'),
        local: Discarding(_now().add(discardUndo)),
      ),
    );
    unawaited(_refillIfDone());
  }

  void undoDiscard(String memoId) {
    final r = row(memoId);
    if (r == null || r.local is! Discarding) return;
    _update(memoId, (x) => x.copyWith(clearDraft: true, local: const Pending()));
  }

  /// Sends the discards whose window has closed -- or all of them (leaving the
  /// screen ends the window: there is nowhere left to undo it from). A send that
  /// fails -- the network, a refusal -- leaves the row failed and still pending
  /// ([_send]); it is not dropped, and it stays on the controller, which outlives
  /// the screen, so the row is there when the screen is opened again.
  Future<void> flushDiscards({required bool all}) {
    // The screen calls this from its own dispose, which can be the container's
    // too (a test tearing down, an app being torn out): nothing to send then.
    if (!ref.mounted) return Future.value();
    final now = _now();
    final due = state.rows.where((r) {
      final l = r.local;
      return l is Discarding && (all || !l.due.isAfter(now));
    });
    return _send([for (final r in due) _Outgoing(discardDecision(r.item), edited: false, discard: true)]);
  }

  /// When every row here is decided and the batch came back full, the next
  /// memos are waiting behind it. Open discards go first: they still occupy the
  /// server's 25, so the next batch would otherwise be the same one.
  Future<void> _refillIfDone() async {
    if (_refilling || !state.more) return;
    if (state.rows.any(isDecidable) || state.rows.any((r) => r.local is Sending)) return;
    _refilling = true;
    try {
      await flushDiscards(all: true);
      if (!ref.mounted) return;
      await load();
    } finally {
      _refilling = false;
    }
  }

  // -- Tickets: linked, never copied -------------------------------------------

  /// Resolves the ticket an accepted decision made, for the card under its row.
  /// Only the key and URL are kept from the accept; the state is the upstream's.
  Future<void> _resolveTicket(TriageResult result) async {
    final key = result.ticketKey;
    if (key == null || key.isEmpty) return;
    final m = RegExp(r'^([A-Z][A-Z0-9]*)-(\d+)$').firstMatch(key);
    var card = TicketCard(key: key, url: result.ticketUrl, state: 'unchecked');
    _putCard(card);
    if (m == null) return;
    try {
      final res = await ref.read(referencesApiProvider).resolveReferences(ResolveRequest(references: [
        ReferenceDescriptor(
          system: ReferenceDescriptorSystemEnum.switchyard,
          token: key,
          key: m.group(1),
          number: int.parse(m.group(2)!),
        ),
      ]));
      if (!ref.mounted) return;
      final hit = res?.resolutions.where((r) => r.token == key).firstOrNull;
      if (hit == null) return;
      final up = hit.upstream;
      card = TicketCard(
        key: up?.key ?? key,
        url: up?.url ?? result.ticketUrl,
        state: hit.state.value,
        title: up?.title,
        stateWord: up?.displayName ?? up?.outcome,
        fetchedAt: hit.fetchedAt,
        lastResolvedAt: hit.lastResolvedAt,
        explain: hit.explain,
      );
      _putCard(card, as_: key);
    } catch (_) {
      if (!ref.mounted) return;
      // The key and URL the accept answered with still work; what is gone is
      // the upstream's state, and the card says so rather than guessing one.
      _putCard(TicketCard(key: key, url: result.ticketUrl, state: 'unreachable'));
    }
  }

  void _putCard(TicketCard card, {String? as_}) {
    state = state.copyWith(cards: {...state.cards, as_ ?? card.key: card});
  }

  /// Opens the ticket in Switchyard. False when there is nowhere to go.
  Future<bool> openTicket(String ticketKey) async {
    final url = state.cards[ticketKey]?.url;
    final uri = url == null ? null : Uri.tryParse(url);
    if (uri == null || !(uri.scheme == 'https' || uri.scheme == 'http')) return false;
    return ref.read(urlOpenerProvider)(uri);
  }
}

final triageControllerProvider =
    NotifierProvider<TriageController, TriageState>(TriageController.new);
