/// Fixtures for CHRN-63: a scripted [TriageApi] and [ReferencesApi], and batch
/// items shaped like the server's. No network, no queue, no platform channel.
library;

import 'dart:io';

import 'package:chronicle_api/api.dart' as gen;

gen.BatchItem item(
  String id, {
  String dest = 'TICKET',
  String? verb,
  bool pre = true,
  String status = 'valid',
  String? title,
  DateTime? capturedAt,
  bool withProposal = true,
  bool complete = true,
  gen.LinkState? link,
}) {
  final destination = switch (dest) {
    'NOTE' => gen.ProposalDestinationEnum.NOTE,
    'DISCUSSION' => gen.ProposalDestinationEnum.DISCUSSION,
    'DISCARD' => gen.ProposalDestinationEnum.DISCARD,
    _ => gen.ProposalDestinationEnum.TICKET,
  };
  return gen.BatchItem(
    memoId: id,
    capturedAt: capturedAt ?? DateTime.utc(2026, 10, 3, 18, 5),
    durationMs: 41000,
    excerpt: 'Transcript for $id: remember to do the thing.',
    proposer: 'ollama/gemma4:e4b',
    generation: 1,
    status: status,
    preAcceptable: pre,
    link: link,
    proposal: withProposal
        ? gen.Proposal(
            generated: gen.Generated(
              tier: gen.GeneratedTierEnum.GeneratedTierOne,
              source_: gen.GeneratedSource_Enum.chronicle,
              regenerable: true,
              notice: 'generated',
            ),
            destination: destination,
            confidence: 0.93,
            reason: 'A clear to-do.',
            title: title ?? 'Title $id',
            nearestPage: null,
            projectKey: dest == 'TICKET' && complete ? 'CHRN' : null,
            ticketType: dest == 'TICKET' ? 'task' : null,
            description: dest == 'TICKET' ? 'Do the thing.' : null,
            pagePath: dest == 'NOTE' && complete ? 'estate/notes' : null,
            body: dest == 'NOTE' ? 'Body.' : null,
            verb: verb == null
                ? null
                : gen.ProposalVerbEnum.values.firstWhere((v) => v.value == verb),
            targetNote: verb == 'append' || verb == 'supersede' ? 'CHR-0001' : null,
            openingPost: dest == 'DISCUSSION' ? 'Discuss it.' : null,
          )
        : null,
  );
}

gen.TriageResult applied(String id, {String dest = 'TICKET', String? ticketKey}) => gen.TriageResult(
      memoId: id,
      status: 'applied',
      destination: dest,
      ticketKey: dest == 'TICKET' ? (ticketKey ?? 'CHRN-900') : null,
      ticketUrl: dest == 'TICKET' ? 'https://switchyard.example.com/t/${ticketKey ?? 'CHRN-900'}' : null,
      noteRef: dest == 'NOTE' ? 'CHR-0311' : null,
    );

/// A [gen.TriageApi] whose answers the test scripts. [answer] decides one
/// decision's result; the default lands everything.
class FakeTriageApi extends gen.TriageApi {
  FakeTriageApi(this.batch) : super(gen.ApiClient());

  List<gen.BatchItem> batch;
  int limit = 25;
  bool unreachable = false;
  gen.TriageResult Function(gen.TriageDecision d) answer = (d) => applied(d.memoId);

  /// Every POST /triage/accept body, in order.
  final requests = <List<gen.TriageDecision>>[];
  final held = <String>[];
  final released = <String>[];
  int batchReads = 0;

  @override
  Future<gen.TriageBatch?> getTriageBatch({int? limit, Future<void>? abortTrigger}) async {
    batchReads++;
    if (unreachable) throw const SocketException('offline');
    return gen.TriageBatch(items: List.of(batch), limit: this.limit);
  }

  @override
  Future<gen.TriageResults?> acceptTriage(gen.AcceptRequest acceptRequest, {Future<void>? abortTrigger}) async {
    requests.add(List.of(acceptRequest.items));
    if (unreachable) throw const SocketException('offline');
    return gen.TriageResults(results: [for (final d in acceptRequest.items) answer(d)]);
  }

  @override
  Future<gen.DeferredItem?> holdMemo(gen.HoldRequest holdRequest, {Future<void>? abortTrigger}) async {
    held.add(holdRequest.memoId);
    return null;
  }

  @override
  Future<void> releaseMemo(gen.ReleaseRequest releaseRequest, {Future<void>? abortTrigger}) async {
    released.add(releaseRequest.memoId);
  }
}

class FakeReferencesApi extends gen.ReferencesApi {
  FakeReferencesApi() : super(gen.ApiClient());

  bool unreachable = false;

  /// When the upstream was last read, as the card's age is computed from it.
  DateTime fetchedAt = DateTime.utc(2026, 10, 3, 19, 56);

  @override
  Future<gen.ResolveResponse?> resolveReferences(gen.ResolveRequest resolveRequest, {Future<void>? abortTrigger}) async {
    if (unreachable) throw const SocketException('offline');
    return gen.ResolveResponse(resolutions: [
      for (final d in resolveRequest.references)
        gen.Resolution(
          token: d.token,
          state: gen.ResolutionStateEnum.resolved,
          upstream: gen.ResolutionUpstream(
            key: d.token,
            displayName: 'IN PROGRESS',
            title: 'A ticket the memo made',
            url: 'https://switchyard.example.com/t/${d.token}',
          ),
          fetchedAt: fetchedAt,
        ),
    ]);
  }
}
