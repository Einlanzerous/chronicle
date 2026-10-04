/// Board 1a, B2 -- batch triage on the phone (CHRN-63).
///
/// A day's memos, each pre-filled with the Scribe's proposal. The interaction
/// budget is ONE TAP per memo in the common case: a pre-filled row carries its
/// own ACCEPT, and ACCEPT ALL takes every one the server is confident about.
/// Anything that needs two taps for a correct proposal has failed at the thing
/// this screen exists to do.
///
/// What the screen draws, and why each is not decoration:
///
///  - THE PROPOSAL IS THE ONLY STEEL. It is generated (tier 1, CLAUDE.md
///    invariant 1); a person's decision beside it is vellum.
///  - A FAILED ITEM STAYS VISIBLY PENDING. Only `applied` takes a row out of
///    the batch; every other answer keeps it in place under a plain
///    `... · STILL PENDING` label with the server's own reason, and a retry.
///  - AN ACCEPTED TICKET IS ONE TAP FROM OPEN. The card under it is coral
///    (Switchyard's reserved colour), says `LINKED · NOT COPIED`, carries the
///    upstream's state word and its age, and says so when the upstream is
///    unreachable rather than showing a confident stale value (invariant 2).
library;

import 'dart:async';

import 'package:chronicle_api/api.dart' as gen;
import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:go_router/go_router.dart';

import '../../theme/theme.dart';
import '../../theme/tokens.dart';
import '../../triage/triage_controller.dart';
import '../../triage/triage_rows.dart';
import 'triage_editor.dart';

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
    _ticker = Timer.periodic(const Duration(seconds: 15), (_) => unawaited(_ctl.flushDiscards(all: false)));
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
    final day = batchDayLabel(s.rows.map((r) => r.item.capturedAt), now);

    return Scaffold(
      body: SafeArea(
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            Padding(
              padding: const EdgeInsets.fromLTRB(space3, space3, space3, 0),
              child: Row(
                children: [
                  Expanded(
                    child: Text(
                      'EVENING TRIAGE${day == null ? '' : ' · $day'}',
                      style: microLabel(color: chSignal, size: sizeSm),
                    ),
                  ),
                  TextButton(
                    onPressed: () => context.go('/'),
                    style: TextButton.styleFrom(minimumSize: const Size(minTapTarget, minTapTarget)),
                    child: const Text('Home'),
                  ),
                ],
              ),
            ),
            if (s.loaded)
              Padding(
                key: const ValueKey('header-counts'),
                padding: const EdgeInsets.symmetric(horizontal: space3),
                child: Text(headerCounts(counts, more: s.more), style: microLabel(size: sizeXs)),
              ),
            const SizedBox(height: space2),
            Padding(
              padding: const EdgeInsets.symmetric(horizontal: space3),
              child: SizedBox(
                width: double.infinity,
                child: FilledButton(
                  key: const ValueKey('accept-all'),
                  onPressed: counts.prefilled == 0 ? null : _ctl.acceptAll,
                  style: FilledButton.styleFrom(minimumSize: const Size.fromHeight(minTapTarget)),
                  child: Text('ACCEPT ALL PRE-FILLED · ${counts.prefilled}'),
                ),
              ),
            ),
            const SizedBox(height: space1),
            Expanded(child: _Body(state: s, now: now)),
            if (s.loaded)
              Container(
                key: const ValueKey('footer-counts'),
                width: double.infinity,
                padding: const EdgeInsets.all(space3),
                decoration: const BoxDecoration(border: Border(top: BorderSide(color: chLine))),
                child: Text(footerCounts(counts, more: s.more), style: microLabel(size: sizeXxs)),
              ),
          ],
        ),
      ),
    );
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
      child: ListView.separated(
        padding: const EdgeInsets.all(space3),
        itemCount: state.rows.length,
        separatorBuilder: (_, _) => const SizedBox(height: space2),
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
    final stuck = _isStuck(row, kind);
    final id = row.memoId;
    final p = item.proposal;

    return Container(
      key: ValueKey('row-$id'),
      decoration: BoxDecoration(
        color: chRaised,
        // No radius: Flutter cannot round a border whose sides differ, and the
        // grey left rule on a stuck row is the whole point of the difference.
        border: Border(
          left: BorderSide(color: stuck ? chTextMeta : chLine, width: stuck ? 3 : 1),
          top: const BorderSide(color: chLine),
          right: const BorderSide(color: chLine),
          bottom: const BorderSide(color: chLine),
        ),
      ),
      padding: const EdgeInsets.all(space3),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          Text(
            '${formatClock(item.capturedAt, now)} · ${formatDuration(item.durationMs)}',
            style: monoMeta(size: sizeXs),
          ),
          const SizedBox(height: space1),
          // Tapping the excerpt opens the one-off confirm: the whole transcript
          // and the proposal in full, for the memo that wants reading first.
          InkWell(
            key: ValueKey('open-$id'),
            onTap: isDecidable(row) ? () => _openConfirm(context, ctl) : null,
            child: ConstrainedBox(
              constraints: const BoxConstraints(minHeight: minTapTarget),
              child: Align(
                alignment: Alignment.centerLeft,
                child: Text(
                  item.excerpt,
                  maxLines: 3,
                  overflow: TextOverflow.ellipsis,
                  style: const TextStyle(fontSize: sizeMd, color: chText),
                ),
              ),
            ),
          ),
          if (p != null && (kind == RowKind.prefilled || kind == RowKind.discardProposed || kind == RowKind.needsInput))
            _ProposalBlock(item: item, proposal: p),
          if (kind == RowKind.needsInput && p == null)
            Padding(
              padding: const EdgeInsets.only(top: space1),
              child: Text(
                item.error ?? 'NO PROPOSAL · NEEDS YOUR INPUT',
                style: microLabel(size: sizeXxs),
              ),
            ),
          if (local is Problem) _problemLine(local),
          if (row.notice != null)
            Padding(
              padding: const EdgeInsets.only(top: space1),
              child: Text(row.notice!, key: ValueKey('notice-$id'), style: microLabel(color: chText2, size: sizeXxs)),
            ),
          if (local is Held)
            _stateLine('HELD · NOT NOW${local.reason.isEmpty ? '' : ' · ${local.reason}'}'),
          if (local is Discarding)
            _stateLine('DISCARDED · UNDO ${undoMinutesLeft(local.due, now)} MIN'),
          if (local is Discarded) _stateLine('DISCARDED'),
          if (kind == RowKind.linkInFlight) _stateLine('DECIDED · WAITING FOR SWITCHYARD'),
          if (kind == RowKind.linkUnresolved) _stateLine('DECIDED · LINK NOT FOUND YET · STILL PENDING', stuck: true),
          if (kind == RowKind.linkAmbiguous) _stateLine('DECIDED · MORE THAN ONE TICKET MATCHES · STILL PENDING', stuck: true),
          if (item.link?.state == 'refused' && local is! Accepted)
            _stateLine('SWITCHYARD REFUSED · ${item.link?.refusedReason ?? 'CHANGE THE DECISION'}', stuck: true),
          if (local is Accepted) _acceptedBlock(local, ref),
          if (kind == RowKind.sending) _stateLine('SENDING…'),
          _actions(context, ctl, kind),
        ],
      ),
    );
  }

  Widget _stateLine(String text, {bool stuck = false}) => Padding(
        padding: const EdgeInsets.only(top: space1),
        child: Text(
          text,
          key: ValueKey('state-${row.memoId}'),
          style: microLabel(color: stuck ? chText2 : chTextMeta, size: sizeXxs),
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
        ],
      ),
    );
  }

  Widget _acceptedBlock(Accepted a, WidgetRef ref) {
    final r = a.result;
    final dest = r.destination ?? '';
    final head = switch (dest) {
      'TICKET' => 'ACCEPTED · TICKET',
      'NOTE' => 'ACCEPTED · NOTE${r.noteRef == null ? '' : ' · ${r.noteRef}'}',
      'DISCUSSION' => 'ACCEPTED · DISCUSSION${r.discussionRef == null ? '' : ' · ${r.discussionRef}'}',
      _ => 'ACCEPTED',
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

  Widget _actions(BuildContext context, TriageController ctl, RowKind kind) {
    final id = row.memoId;
    final local = row.local;
    Widget btn(String label, String keyName, VoidCallback? onTap, {bool primary = false}) => Expanded(
          child: Padding(
            padding: const EdgeInsets.only(right: space1),
            child: primary
                ? FilledButton(
                    key: ValueKey('$keyName-$id'),
                    onPressed: onTap,
                    style: FilledButton.styleFrom(minimumSize: const Size(minTapTarget, minTapTarget)),
                    child: Text(label),
                  )
                : OutlinedButton(
                    key: ValueKey('$keyName-$id'),
                    onPressed: onTap,
                    style: OutlinedButton.styleFrom(minimumSize: const Size(minTapTarget, minTapTarget)),
                    child: Text(label),
                  ),
          ),
        );

    final children = <Widget>[];
    if (local is Held) {
      children.add(btn('RELEASE', 'release', () => ctl.release(id)));
    } else if (local is Discarding) {
      children.add(btn('UNDO', 'undo', () => ctl.undoDiscard(id)));
    } else if (local is Problem && local.status == ProblemStatus.failed && row.draft == null && kind == RowKind.prefilled) {
      // A transient failure of an accept-as-shown is retried as it was.
      children.add(btn('RETRY', 'accept', () => ctl.accept(id), primary: true));
      children.add(btn('EDIT', 'edit', () => _edit(context, ctl)));
    } else if (kind == RowKind.prefilled) {
      children.add(btn('ACCEPT', 'accept', () => ctl.accept(id), primary: true));
      children.add(btn('EDIT', 'edit', () => _edit(context, ctl)));
      children.add(btn('HOLD', 'hold', () => ctl.hold(id)));
    } else if (kind == RowKind.needsInput) {
      children.add(btn('EDIT', 'edit', () => _edit(context, ctl), primary: true));
      children.add(btn('HOLD', 'hold', () => ctl.hold(id)));
      children.add(btn('DISCARD', 'discard', () => ctl.discard(id)));
    } else if (kind == RowKind.discardProposed) {
      children.add(btn('DISCARD', 'discard', () => ctl.discard(id), primary: true));
      children.add(btn('EDIT', 'edit', () => _edit(context, ctl)));
      children.add(btn('HOLD', 'hold', () => ctl.hold(id)));
    }
    if (children.isEmpty) return const SizedBox.shrink();
    return Padding(
      padding: const EdgeInsets.only(top: space2),
      child: Row(children: children),
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
    if (draft != null) unawaited(ctl.edit(row.memoId, draft));
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
        ctl.discard(row.memoId);
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
              Text(
                destinationTag(p.destination.value),
                style: microLabel(color: chGenerated, size: sizeXs),
              ),
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

/// The accepted ticket, as a live card. Coral is Switchyard's reserved colour
/// and appears on nothing else here; the card is a LINK, so the whole of it is
/// the tap that opens the ticket. A cache with no visible age is a copy that
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
        ? 'AS OF ${relativeAge(card.fetchedAt!, now)} AGO'
        : null;

    return Material(
      color: Colors.transparent,
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
          constraints: const BoxConstraints(minHeight: minTapTarget),
          padding: const EdgeInsets.all(space2),
          decoration: BoxDecoration(
            border: Border.all(color: refSwitchyard),
            borderRadius: BorderRadius.circular(space1),
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
                      style: microLabel(color: refSwitchyard, size: sizeXs),
                    ),
                    if ((card.title ?? '').isNotEmpty)
                      Text(card.title!, style: const TextStyle(fontSize: sizeBody, color: chText)),
                    Text(
                      'LINKED · NOT COPIED${age == null ? '' : ' · $age'}',
                      style: microLabel(size: sizeXxs),
                    ),
                  ],
                ),
              ),
              if (hasUrl) const Icon(Icons.arrow_outward, color: refSwitchyard, size: 18),
            ],
          ),
        ),
      ),
    );
  }
}

enum _ConfirmChoice { accept, edit, hold, discard }

/// The single-note confirm: the one-off path for a memo that wants reading
/// before it is decided. The whole transcript and the proposal in full, with
/// the same four verbs the row has.
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
            if (editsExistingNote(p))
              Padding(
                padding: const EdgeInsets.only(top: space1),
                child: Text(
                  '${p!.verb!.value.toUpperCase()} WRITES INTO A NOTE SOMEBODY ALREADY WROTE. ACCEPTING HERE CONFIRMS IT.',
                  style: microLabel(color: chText2, size: sizeXxs),
                ),
              ),
            const SizedBox(height: space3),
            if (kind == RowKind.prefilled) action('ACCEPT', 'accept', _ConfirmChoice.accept, primary: true),
            action('EDIT', 'edit', _ConfirmChoice.edit, primary: kind != RowKind.prefilled),
            action('HOLD', 'hold', _ConfirmChoice.hold),
            action('DISCARD', 'discard', _ConfirmChoice.discard),
          ],
        ),
      ),
    );
  }
}
