/// Board 1b, B2 -- batch triage on the phone (CHRN-63): "Route decision,
/// variation -- the lane".
///
/// A day's memos, each with the Scribe's pick already filled in on three
/// lanes -- `TICKET` / `NOTE` / `DISC` -- and ONE primary button pinned to the
/// foot whose label is the pending outcome, `FILE 3 · DISCARD 1`. That button
/// is the commit, so the common case is zero taps per memo and one to file, and
/// a wrong proposal costs one tap on the right lane. (The ticket's "one tap per
/// memo" budget is met with room to spare; anything that needs two taps for a
/// correct proposal has failed at the thing this screen exists to do.)
///
/// What the screen draws, and why each is not decoration:
///
///  - THE SCRIBE'S PICK IS A FILL, NOT A CHOICE MADE. It is generated (tier 1,
///    CLAUDE.md invariant 1), so a pick the person has not touched is the
///    Scribe's, and a lane the person tapped is theirs and says so in the
///    detail line. Nothing is sent until FILE.
///  - A FAILED ITEM STAYS VISIBLY PENDING. Only `applied` takes a row out of
///    the batch; every other answer keeps it in place under a plain
///    `... · STILL PENDING` label with the server's reason. There is no per-row
///    RETRY because FILE is the retry: a failed row is still counted in it.
///  - AN ACCEPTED TICKET IS ONE TAP FROM OPEN. Board 1a's frame 06 card: a coral
///    left rule, the key, the ticket's title, `OPEN ↗`, under `LINKED · NOT
///    COPIED`; the upstream's state word and its age ride with it, and an
///    unreachable upstream says so rather than showing a confident stale value
///    (invariant 2).
///
/// Everything the lanes cannot say -- the title, the project, the page, the
/// whole transcript, HOLD, and the single-memo confirm an append needs -- is
/// behind a tap on the row's title, so it is one tap further for the memo that
/// wants reading and none for the memo that does not.
///
/// **The canvas draws the lanes 38 px tall; the epic's 44 px minimum tap target
/// wins**, so each lane's hit area is 44 px.
library;

import 'dart:async';

import 'package:chronicle_api/api.dart' as gen;
import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../../theme/theme.dart';
import '../../theme/tokens.dart';
import '../../triage/triage_controller.dart';
import '../../triage/triage_rows.dart';
import 'triage_editor.dart';

/// The three lanes, left to right, with the label the canvas draws.
const _lanes = [('TICKET', 'TICKET'), ('NOTE', 'NOTE'), ('DISCUSSION', 'DISC')];

/// A lane's fill when it is the pick: coral for a ticket (Switchyard's reserved
/// colour), vellum for a note, [chDiscussion] for a discussion.
Color _laneColor(String destination) => switch (destination) {
      'TICKET' => refSwitchyard,
      'NOTE' => chSignal,
      _ => chDiscussion,
    };

class TriageScreen extends ConsumerStatefulWidget {
  const TriageScreen({super.key});

  @override
  ConsumerState<TriageScreen> createState() => _TriageScreenState();
}

class _TriageScreenState extends ConsumerState<TriageScreen> {
  late final TriageController _ctl;
  Timer? _ticker;

  @override
  void initState() {
    super.initState();
    _ctl = ref.read(triageControllerProvider.notifier);
    // After the first frame: a provider may not be written during build.
    Future.microtask(_ctl.load);
    _ticker = Timer.periodic(const Duration(seconds: 15), (_) {
      unawaited(_ctl.flushDiscards(all: false));
      // The undo countdown reads the clock, so it is redrawn as time passes.
      if (mounted) setState(() {});
    });
  }

  @override
  void dispose() {
    _ticker?.cancel();
    // Leaving the screen ends the undo window.
    unawaited(_ctl.flushDiscards(all: true));
    super.dispose();
  }

  @override
  Widget build(BuildContext context) {
    final s = ref.watch(triageControllerProvider);
    final now = ref.watch(triageClockProvider)();
    final counts = countRows(s.rows);
    final plan = _ctl.filing();

    return Scaffold(
      body: SafeArea(
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            Container(
              height: 52,
              padding: const EdgeInsets.symmetric(horizontal: space4),
              decoration: const BoxDecoration(border: Border(bottom: BorderSide(color: chRaised))),
              child: Row(
                children: [
                  Expanded(
                    child: Column(
                      mainAxisAlignment: MainAxisAlignment.center,
                      crossAxisAlignment: CrossAxisAlignment.start,
                      children: [
                        const Text(
                          'Evening triage',
                          style: TextStyle(fontSize: 14, fontWeight: FontWeight.w600, color: chText),
                        ),
                        if (s.loaded)
                          Text(
                            key: const ValueKey('header-counts'),
                            _subline(counts, s.more),
                            style: microLabel(size: sizeXxs),
                          ),
                      ],
                    ),
                  ),
                  InkWell(
                    key: const ValueKey('accept-all'),
                    onTap: counts.prefilled == 0 ? null : _ctl.acceptAll,
                    child: ConstrainedBox(
                      constraints: const BoxConstraints(minHeight: minTapTarget, minWidth: minTapTarget),
                      child: Center(
                        child: Text(
                          'ACCEPT ALL',
                          style: microLabel(color: counts.prefilled == 0 ? chTextDim : chSignal, size: 10),
                        ),
                      ),
                    ),
                  ),
                ],
              ),
            ),
            Expanded(child: _Body(state: s, now: now)),
            Container(
              padding: const EdgeInsets.fromLTRB(space4, 14, space4, 14),
              decoration: const BoxDecoration(border: Border(top: BorderSide(color: chRaised))),
              child: SizedBox(
                width: double.infinity,
                height: 52,
                child: FilledButton(
                  key: const ValueKey('file'),
                  onPressed: plan.file + plan.retry + plan.discard == 0 ? null : _ctl.fileAll,
                  style: FilledButton.styleFrom(
                    backgroundColor: chSignal,
                    foregroundColor: chBase,
                    shape: const RoundedRectangleBorder(),
                  ),
                  child: FittedBox(
                    fit: BoxFit.scaleDown,
                    child: Text(
                      fileLabel(plan.file, plan.discard, retry: plan.retry, refused: plan.refused),
                      style: const TextStyle(
                        fontFamily: fontMono,
                        fontSize: 11.5,
                        fontWeight: FontWeight.w600,
                        letterSpacing: 1.5,
                      ),
                    ),
                  ),
                ),
              ),
            ),
          ],
        ),
      ),
    );
  }

  /// `4 MEMOS · SCRIBE PRE-FILLED`, plus how many did not land. [more] is the
  /// batch cap talking: the server hands over one screen (25) and no total.
  static String _subline(TriageCounts c, bool more) {
    final noun = c.waiting == 1 && !more ? 'MEMO' : 'MEMOS';
    return '${c.waiting}${more ? '+' : ''} $noun · SCRIBE PRE-FILLED'
        '${c.failed > 0 ? ' · ${c.failed} FAILED' : ''}';
  }
}

class _Body extends ConsumerWidget {
  const _Body({required this.state, required this.now});

  final TriageState state;
  final DateTime now;

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    if (state.loadError != null && state.rows.isEmpty) {
      return Padding(
        padding: const EdgeInsets.all(space3),
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            Text(state.loadError!, key: const ValueKey('load-error'), style: const TextStyle(color: chText)),
            const SizedBox(height: space2),
            OutlinedButton(
              onPressed: ref.read(triageControllerProvider.notifier).load,
              style: OutlinedButton.styleFrom(minimumSize: const Size(minTapTarget, minTapTarget)),
              child: const Text('Try again'),
            ),
          ],
        ),
      );
    }
    if (!state.loaded) {
      return Center(child: Text('Reading the batch…', style: monoMeta()));
    }
    if (state.rows.isEmpty) {
      return Center(child: Text('NOTHING WAITING', key: const ValueKey('empty'), style: microLabel()));
    }
    return RefreshIndicator(
      onRefresh: ref.read(triageControllerProvider.notifier).load,
      child: ListView.builder(
        itemCount: state.rows.length,
        itemBuilder: (context, i) => _RowCard(row: state.rows[i], state: state, now: now),
      ),
    );
  }
}

class _RowCard extends ConsumerWidget {
  const _RowCard({required this.row, required this.state, required this.now});

  final TriageRow row;
  final TriageState state;
  final DateTime now;

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final ctl = ref.read(triageControllerProvider.notifier);
    final kind = rowKind(row);
    final local = row.local;
    final item = row.item;
    final id = row.memoId;
    final p = item.proposal;
    final pick = pickOf(row);
    final decidable = isDecidable(row) && local is! Sending;
    final discarding = filingOf(row) == Filing.discard;
    final stuck = _isStuck(row, kind);
    final missing = stagedProblem(row);

    final body = Container(
      key: ValueKey('row-$id'),
      padding: const EdgeInsets.symmetric(horizontal: space4, vertical: 15),
      decoration: BoxDecoration(
        border: Border(
          bottom: const BorderSide(color: chRaised),
          // The grey left rule is the board's mark for a decision that did not
          // finish; nothing else uses it.
          left: stuck ? const BorderSide(color: chTextMeta, width: 3) : BorderSide.none,
        ),
      ),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          Row(
            crossAxisAlignment: CrossAxisAlignment.baseline,
            textBaseline: TextBaseline.alphabetic,
            children: [
              Text(formatClock(item.capturedAt, now), style: monoMeta(color: chTextMeta, size: sizeXs)),
              const SizedBox(width: 9),
              Text(formatDuration(item.durationMs), style: monoMeta(color: chTextDim, size: sizeXxs + 0.5)),
            ],
          ),
          // The title, and the one way to everything the lanes do not say: the
          // whole transcript, HOLD, the title and project, and the single-memo
          // confirm an append needs.
          InkWell(
            key: ValueKey('open-$id'),
            onTap: decidable ? () => _openConfirm(context, ctl) : null,
            child: ConstrainedBox(
              constraints: const BoxConstraints(minHeight: minTapTarget),
              child: Align(
                alignment: Alignment.centerLeft,
                child: Text(
                  _title(),
                  maxLines: 3,
                  overflow: TextOverflow.ellipsis,
                  style: TextStyle(
                    fontFamily: fontSerif,
                    fontSize: 16.5,
                    height: 1.42,
                    color: discarding ? chText2 : chText,
                  ),
                ),
              ),
            ),
          ),
          if (_showsLanes(kind, local, discarding)) ...[
            _LaneRow(
              memoId: id,
              pick: pick,
              enabled: decidable,
              onPick: (lane) => ctl.pickLane(id, lane),
            ),
            if (_detail(pick, p, missing) case final line?)
              Padding(
                padding: const EdgeInsets.only(top: 9),
                child: Text(line, key: ValueKey('detail-$id'), style: monoMeta(color: chTextMeta, size: sizeXxs + 0.5)),
              ),
          ] else if (discarding && local is! Discarding)
            // A discard is shown as one: bordered, with the Scribe's reason, in
            // place of the lanes. Override is the row's own tap.
            Container(
              key: ValueKey('discard-strip-$id'),
              height: 38,
              margin: const EdgeInsets.only(top: 4),
              padding: const EdgeInsets.symmetric(horizontal: 12),
              decoration: BoxDecoration(border: Border.all(color: chLine)),
              child: Row(
                children: [
                  Text('DISCARD', style: microLabel(color: chTextMeta, size: sizeXxs + 0.5)),
                  const SizedBox(width: 11),
                  Expanded(
                    child: Text(
                      row.draft != null ? 'your choice' : (p?.reason ?? ''),
                      textAlign: TextAlign.right,
                      overflow: TextOverflow.ellipsis,
                      style: monoMeta(color: chTextDim, size: sizeXxs + 0.5),
                    ),
                  ),
                ],
              ),
            ),
          if (local is Problem) _problemLine(local),
          if (row.notice != null)
            Padding(
              padding: const EdgeInsets.only(top: space1),
              child: Text(row.notice!, key: ValueKey('notice-$id'), style: microLabel(color: chText2, size: sizeXxs)),
            ),
          if (local is Held) ...[
            _stateLine('HELD · NOT NOW${local.reason.isEmpty ? '' : ' · ${local.reason}'}'),
            _textAction('RELEASE', 'release', () => ctl.release(id)),
          ],
          if (local is Discarding) ...[
            // Not sent yet, so it WILL discard; `DISCARDED` is for the server's `applied`.
            _stateLine('DISCARDS IN ${undoMinutesLeft(local.due, now)} MIN'),
            _textAction('UNDO', 'undo', () => ctl.undoDiscard(id)),
          ],
          if (local is Discarded) _stateLine('DISCARDED'),
          if (kind == RowKind.linkInFlight) _stateLine('DECIDED · WAITING FOR SWITCHYARD'),
          if (kind == RowKind.linkUnresolved) _stateLine('DECIDED · LINK NOT FOUND YET · STILL PENDING'),
          if (kind == RowKind.linkAmbiguous) _stateLine('DECIDED · MORE THAN ONE TICKET MATCHES · STILL PENDING'),
          if (item.link?.state == 'refused' && local is! Accepted)
            _stateLine('SWITCHYARD REFUSED · ${item.link?.refusedReason ?? 'CHANGE THE DECISION'}'),
          if (local is Accepted) _acceptedBlock(local),
          if (kind == RowKind.sending) _stateLine('SENDING…'),
        ],
      ),
    );

    // A discard reads as set aside: the row dims, the way the canvas draws it.
    return discarding || local is Discarding || local is Discarded ? Opacity(opacity: .55, child: body) : body;
  }

  /// A memo's title: the proposal's, else the transcript's own first words.
  String _title() {
    final t = (row.draft?.title.isNotEmpty ?? false) ? row.draft!.title : row.item.proposal?.title;
    return (t != null && t.isNotEmpty) ? t : row.item.excerpt;
  }

  /// Lanes are for a row that is still waiting on a decision; a discard has its
  /// strip instead, and a row that has been decided is past choosing.
  bool _showsLanes(RowKind kind, LocalState local, bool discarding) {
    if (discarding) return false;
    if (local is Accepted || local is Held || local is Discarding || local is Discarded) return false;
    return kind == RowKind.prefilled || kind == RowKind.needsInput || kind == RowKind.discardProposed;
  }

  /// The mono line under the lanes: where the pick goes, or what it still needs.
  String? _detail(String? pick, gen.Proposal? p, String? missing) {
    if (pick == null) return row.item.error ?? 'NO PROPOSAL · TAP THE TITLE TO ADD DETAIL';
    final d = row.draft;
    final mine = d != null;
    final String where;
    switch (pick) {
      case 'TICKET':
        final key = mine ? d.projectKey : (p?.projectKey ?? '');
        final type = mine ? d.ticketType : (p?.ticketType ?? 'task');
        where = '→ ${key.isEmpty ? 'no project' : key} · $type';
      case 'NOTE':
        final path = mine ? d.pagePath : (p?.pagePath ?? '');
        final verb = mine ? d.verb : (p?.verb?.value ?? 'create');
        where = '→ ${path.isEmpty ? 'no page' : path.split('/').join(' / ')}${verb == 'create' ? '' : ' · $verb'}';
      default:
        where = '→ open question';
    }
    final line = mine ? '$where · YOUR CHOICE' : where;
    // Where it would go, then what it still needs: `→ no project · task` is the
    // fact, and the second line is why FILE is not taking the row.
    return missing == null ? line : '$line\nNEEDS INPUT · $missing TAP THE TITLE TO ADD IT';
  }

  Widget _stateLine(String text) => Padding(
        padding: const EdgeInsets.only(top: space1),
        child: Text(text, key: ValueKey('state-${row.memoId}'), style: microLabel(color: chTextMeta, size: sizeXxs)),
      );

  Widget _textAction(String label, String keyName, VoidCallback onTap) => InkWell(
        key: ValueKey('$keyName-${row.memoId}'),
        onTap: onTap,
        child: ConstrainedBox(
          constraints: const BoxConstraints(minHeight: minTapTarget, minWidth: minTapTarget),
          child: Align(
            alignment: Alignment.centerLeft,
            child: Text(label, style: microLabel(color: chSignal, size: sizeXs)),
          ),
        ),
      );

  Widget _problemLine(Problem p) {
    final label = switch (p.status) {
      ProblemStatus.failed => 'FAILED · STILL PENDING',
      ProblemStatus.refused => 'REFUSED · STILL PENDING',
      ProblemStatus.needsInput => 'NEEDS INPUT · STILL PENDING',
      ProblemStatus.stale => 'PROPOSAL CHANGED · STILL PENDING',
    };
    return Padding(
      padding: const EdgeInsets.only(top: space1),
      child: Column(
        key: ValueKey('problem-${row.memoId}'),
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          Text(label, style: microLabel(color: chText, size: sizeXxs)),
          const SizedBox(height: 3),
          Text(p.reason, style: const TextStyle(fontSize: sizeBody, color: chText2)),
          if (p.raw != null)
            Text('TAP THE TITLE FOR THE SERVER\'S OWN WORDS', style: microLabel(color: chTextDim, size: sizeXxs)),
        ],
      ),
    );
  }

  Widget _acceptedBlock(Accepted a) {
    final r = a.result;
    final head = switch (r.destination ?? '') {
      'TICKET' => 'TICKET CREATED',
      'NOTE' => 'NOTE CREATED${r.noteRef == null ? '' : ' · ${r.noteRef}'}',
      'DISCUSSION' => 'DISCUSSION OPENED${r.discussionRef == null ? '' : ' · ${r.discussionRef}'}',
      _ => 'FILED',
    };
    final key = r.ticketKey;
    return Padding(
      padding: const EdgeInsets.only(top: space1),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          Text(
            a.edited ? '$head · EDITED' : head,
            key: ValueKey('state-${row.memoId}'),
            style: microLabel(color: chResolved, size: sizeXxs),
          ),
          if (key != null && key.isNotEmpty) ...[
            const SizedBox(height: space1),
            _TicketCardView(
              card: state.cards[key] ?? TicketCard(key: key, url: r.ticketUrl, state: 'unchecked'),
              now: now,
            ),
          ],
        ],
      ),
    );
  }

  Future<void> _edit(BuildContext context, TriageController ctl) async {
    final draft = await showTriageEditor(
      context,
      initial: row.draft ?? draftFor(row.item),
      excerpt: row.item.excerpt,
      projectKeys: {
        for (final r in state.rows)
          if (r.item.proposal?.projectKey case final k? when k.isNotEmpty) k,
      }.toList()
        ..sort(),
    );
    if (draft != null) ctl.stage(row.memoId, draft);
  }

  Future<void> _openConfirm(BuildContext context, TriageController ctl) async {
    final choice = await showModalBottomSheet<_ConfirmChoice>(
      context: context,
      isScrollControlled: true,
      backgroundColor: chBase,
      builder: (_) => _ConfirmSheet(row: row),
    );
    if (!context.mounted) return;
    switch (choice) {
      case _ConfirmChoice.accept:
        unawaited(ctl.accept(row.memoId));
      case _ConfirmChoice.edit:
        await _edit(context, ctl);
      case _ConfirmChoice.hold:
        unawaited(ctl.hold(row.memoId));
      case _ConfirmChoice.discard:
        ctl.stage(row.memoId, const Draft(destination: 'DISCARD'));
      case null:
        break;
    }
  }

  /// The grey left rule the board gives a decision that did not finish.
  static bool _isStuck(TriageRow row, RowKind kind) {
    if (kind == RowKind.linkUnresolved || kind == RowKind.linkAmbiguous || kind == RowKind.linkInFlight) {
      return true;
    }
    if (row.item.link?.state == 'refused') return true;
    final l = row.local;
    return l is Problem && (l.status == ProblemStatus.failed || l.status == ProblemStatus.refused);
  }
}

/// The three lanes. The pick is filled in its destination's colour and the
/// others sit dim on [chLaneOff]; a tap on another lane is the override.
/// Each lane is at least [minTapTarget] tall -- the canvas draws 38 px, and the
/// epic's 44 px floor wins.
class _LaneRow extends StatelessWidget {
  const _LaneRow({required this.memoId, required this.pick, required this.enabled, required this.onPick});

  final String memoId;
  final String? pick;
  final bool enabled;
  final void Function(String lane) onPick;

  @override
  Widget build(BuildContext context) => Padding(
        padding: const EdgeInsets.only(top: 4),
        child: Row(
          children: [
            for (var i = 0; i < _lanes.length; i++) ...[
              if (i > 0) const SizedBox(width: 1),
              Expanded(
                child: Semantics(
                  button: true,
                  selected: pick == _lanes[i].$1,
                  label: _lanes[i].$1,
                  child: InkWell(
                    key: ValueKey('lane-$memoId-${_lanes[i].$1}'),
                    onTap: enabled ? () => onPick(_lanes[i].$1) : null,
                    child: Container(
                      height: minTapTarget,
                      alignment: Alignment.center,
                      color: pick == _lanes[i].$1 ? _laneColor(_lanes[i].$1) : chLaneOff,
                      child: Text(
                        _lanes[i].$2,
                        style: microLabel(
                          color: pick == _lanes[i].$1 ? chBase : chTextMeta,
                          size: 10,
                        ),
                      ),
                    ),
                  ),
                ),
              ),
            ],
          ],
        ),
      );
}

class _ProposalBlock extends StatelessWidget {
  const _ProposalBlock({required this.item, required this.proposal});

  final gen.BatchItem item;
  final gen.Proposal proposal;

  @override
  Widget build(BuildContext context) {
    final p = proposal;
    final reason = p.reason.trim().isNotEmpty
        ? p.reason
        : (p.nearestPage == null ? '' : 'Nearest existing page is ${p.nearestPage!.split('/').join(' / ')}');
    return Container(
      key: ValueKey('proposal-${item.memoId}'),
      margin: const EdgeInsets.only(top: space1),
      padding: const EdgeInsets.all(space2),
      decoration: BoxDecoration(
        border: Border.all(color: chGenerated.withValues(alpha: 0.4)),
        borderRadius: BorderRadius.circular(space1),
      ),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          Row(
            children: [
              Text(destinationTag(p.destination.value), style: microLabel(color: chGenerated, size: sizeXs)),
              if (p.verb != null) ...[
                const SizedBox(width: space1),
                Text(p.verb!.value.toUpperCase(), style: microLabel(size: sizeXxs)),
              ],
              const Spacer(),
              Text(formatConfidence(p.confidence), style: monoMeta(color: chGenerated, size: sizeXs)),
            ],
          ),
          if ((p.title ?? '').isNotEmpty) ...[
            const SizedBox(height: 3),
            Text(p.title!, style: const TextStyle(fontSize: sizeMd, color: chText)),
          ],
          if (p.destination == gen.ProposalDestinationEnum.TICKET && (p.projectKey ?? '').isNotEmpty)
            Text('${p.projectKey} · ${p.ticketType ?? 'task'}', style: monoMeta(size: sizeXs)),
          if (reason.isNotEmpty) ...[
            const SizedBox(height: 3),
            Text(reason, style: const TextStyle(fontSize: sizeBody, color: chText2)),
          ],
          const SizedBox(height: space1),
          Text(proposerLabel(item), style: microLabel(color: chGenerated, size: sizeXxs)),
          for (final c in item.clearedFields)
            Padding(
              padding: const EdgeInsets.only(top: 3),
              child: Text('CLEARED ${c.field.toUpperCase()} · ${c.reason}', style: microLabel(size: sizeXxs)),
            ),
        ],
      ),
    );
  }
}

/// The accepted ticket, as board 1a frame 06's card: `LINKED · NOT COPIED`, a
/// coral left rule (Switchyard's reserved colour, on nothing else here), the
/// key, the ticket's title and `OPEN ↗`. The card is a LINK, so the whole of it
/// is the tap that opens the ticket. A cache with no visible age is a copy that
/// lies, so the age is drawn, and an unreachable upstream says so instead of
/// showing a confident stale state.
class _TicketCardView extends ConsumerWidget {
  const _TicketCardView({required this.card, required this.now});

  final TicketCard card;
  final DateTime now;

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final hasUrl = card.url != null && card.url!.isNotEmpty;
    final String status;
    switch (card.state) {
      case 'resolved':
        status = card.stateWord ?? 'RESOLVED';
      case 'broken':
        status = 'BROKEN · ${card.explain ?? 'THE LINK DOES NOT RESOLVE'}';
      case 'unreachable':
        status = 'SWITCHYARD UNREACHABLE'
            '${card.lastResolvedAt == null ? '' : ' · LAST REACHED ${relativeAge(card.lastResolvedAt!, now)} AGO'}';
      case 'unconfigured':
        status = 'SWITCHYARD NOT CONFIGURED';
      default:
        status = 'NOT CHECKED';
    }
    final age = card.state == 'resolved' && card.fetchedAt != null
        ? ' · AS OF ${relativeAge(card.fetchedAt!, now)} AGO'
        : '';

    return Column(
      crossAxisAlignment: CrossAxisAlignment.start,
      children: [
        Text('LINKED · NOT COPIED$age', style: microLabel(size: sizeXxs)),
        const SizedBox(height: space1),
        Material(
          color: chLaneOff,
          child: InkWell(
            key: ValueKey('open-ticket-${card.key}'),
            onTap: hasUrl
                ? () async {
                    final opened = await ref.read(triageControllerProvider.notifier).openTicket(card.key);
                    if (!opened && context.mounted) {
                      ScaffoldMessenger.of(context).showSnackBar(
                        const SnackBar(content: Text('Could not open the ticket. Its key is on the card.')),
                      );
                    }
                  }
                : null,
            child: Container(
              constraints: const BoxConstraints(minHeight: 50),
              padding: const EdgeInsets.symmetric(horizontal: 13, vertical: space1),
              decoration: const BoxDecoration(
                border: Border(
                  top: BorderSide(color: chLine),
                  right: BorderSide(color: chLine),
                  bottom: BorderSide(color: chLine),
                  left: BorderSide(color: refSwitchyard, width: 2),
                ),
              ),
              child: Row(
                children: [
                  Expanded(
                    child: Column(
                      crossAxisAlignment: CrossAxisAlignment.start,
                      children: [
                        Text(
                          '${card.key} · $status',
                          key: ValueKey('ticket-status-${card.key}'),
                          style: monoMeta(color: refSwitchyard, size: 11),
                        ),
                        if ((card.title ?? '').isNotEmpty)
                          Text(
                            card.title!,
                            overflow: TextOverflow.ellipsis,
                            style: const TextStyle(fontSize: 13, color: chText2),
                          ),
                      ],
                    ),
                  ),
                  if (hasUrl)
                    Text('OPEN ↗', style: microLabel(color: chSignal, size: 10.5)),
                ],
              ),
            ),
          ),
        ),
      ],
    );
  }
}

enum _ConfirmChoice { accept, edit, hold, discard }

/// The single-note confirm: the one-off path for a memo that wants reading
/// before it is decided. The whole transcript and the proposal in full. ACCEPT
/// here sends this memo on its own, now, and is the only path that carries the
/// per-item confirmation an append or a supersede costs; EDIT and DISCARD stage
/// the choice for FILE like a lane tap does.
class _ConfirmSheet extends StatelessWidget {
  const _ConfirmSheet({required this.row});

  final TriageRow row;

  @override
  Widget build(BuildContext context) {
    final item = row.item;
    final p = item.proposal;
    final kind = rowKind(row);
    final id = row.memoId;
    Widget action(String label, String keyName, _ConfirmChoice c, {bool primary = false}) => Padding(
          padding: const EdgeInsets.only(bottom: space1),
          child: SizedBox(
            width: double.infinity,
            child: primary
                ? FilledButton(
                    key: ValueKey('confirm-$keyName-$id'),
                    onPressed: () => Navigator.of(context).pop(c),
                    style: FilledButton.styleFrom(minimumSize: const Size.fromHeight(minTapTarget)),
                    child: Text(label),
                  )
                : OutlinedButton(
                    key: ValueKey('confirm-$keyName-$id'),
                    onPressed: () => Navigator.of(context).pop(c),
                    style: OutlinedButton.styleFrom(minimumSize: const Size.fromHeight(minTapTarget)),
                    child: Text(label),
                  ),
          ),
        );

    return SafeArea(
      child: SingleChildScrollView(
        padding: const EdgeInsets.all(space3),
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            Text('CONFIRM ONE MEMO', style: microLabel(color: chSignal, size: sizeSm)),
            const SizedBox(height: space2),
            Text(item.excerpt, key: ValueKey('confirm-excerpt-$id'), style: const TextStyle(fontSize: sizeMd, color: chText)),
            if (p != null) _ProposalBlock(item: item, proposal: p),
            if (row.local case Problem(:final raw?))
              Padding(
                padding: const EdgeInsets.only(top: space2),
                child: Column(
                  crossAxisAlignment: CrossAxisAlignment.start,
                  children: [
                    Text('WHAT THE SERVER SAID', style: microLabel(size: sizeXxs)),
                    const SizedBox(height: 3),
                    SelectableText(raw, key: ValueKey('confirm-raw-$id'), style: monoMeta(size: sizeXxs + 0.5)),
                  ],
                ),
              ),
            if (editsExistingNote(p))
              Padding(
                padding: const EdgeInsets.only(top: space1),
                child: Text(
                  '${p!.verb!.value.toUpperCase()} WRITES INTO A NOTE SOMEBODY ALREADY WROTE. ACCEPTING HERE CONFIRMS IT.',
                  style: microLabel(color: chText2, size: sizeXxs),
                ),
              ),
            const SizedBox(height: space3),
            if (kind == RowKind.prefilled)
              action('ACCEPT AS SHOWN', 'accept', _ConfirmChoice.accept, primary: true),
            action('EDIT', 'edit', _ConfirmChoice.edit, primary: kind != RowKind.prefilled),
            action('HOLD · NOT NOW', 'hold', _ConfirmChoice.hold),
            action('DISCARD', 'discard', _ConfirmChoice.discard),
          ],
        ),
      ),
    );
  }
}
