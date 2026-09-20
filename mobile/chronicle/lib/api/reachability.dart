/// Can this device reach the Chronicle it is pointed at, and if not, what is
/// actually wrong?
///
/// This is CHRN-59's third clause — *"recovers cleanly when the tunnel is
/// down"* — and the reason it is a classification rather than a bool is the
/// epic's own warning: *"the moment recording fails because the tunnel is down
/// is the moment the operator stops trusting the app."* An app that says
/// "something went wrong" when the answer is "you are on the wrong host" or
/// "you are signed out" spends that trust just as fast.
///
/// **"Recovers cleanly" is a property of this file being re-runnable.** Nothing
/// here latches: a failed probe is a value, not a mode, so a later probe that
/// succeeds returns the app to [Reach.ok] with no restart, no re-onboarding and
/// no cached error. The test proves exactly that sequence.
///
/// The probe is `GET /healthz`, which the contract designates for this:
/// *"Load-bearing beyond liveness — CHRN-59's QR onboarding probes this to check
/// a server address before any credential exists, which is also why it keeps an
/// unprefixed path."* It needs no credential and touches no database, so it
/// answers the question asked — *is a Chronicle there* — and not a different one.
library;

import 'dart:async';

import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:http/http.dart' as http;

import 'server_url.dart';
import 'transport.dart';

enum Reach {
  /// No probe has answered yet. Distinct from a failure: showing "cannot reach
  /// the server" before asking would be a claim we have not checked.
  unknown,

  /// A Chronicle answered `/healthz`.
  ok,

  /// Nothing answered: DNS, routing, no network, connection refused. The
  /// ordinary away-from-home and no-signal case.
  unreachable,

  /// Something is there but did not answer in time. Told apart from
  /// [unreachable] because the advice differs — a timeout is worth retrying on
  /// the spot, a refused connection usually is not.
  timedOut,

  /// The address is the **tunneled, Access-gated host**. A bearer token cannot
  /// open that gate, so this is a configuration answer and not a network one:
  /// the device needs the direct host, which is what the invite QR carries.
  accessGated,

  /// A redirect we cannot attribute. Still refused — the contract declares no
  /// 3xx — but not diagnosed as Access, because we do not know that.
  unexpectedRedirect,

  /// A Chronicle answered, badly (5xx). Its problem, not the network's.
  serverError,

  /// Something answered `/healthz` and it was not a Chronicle -- a 404 from
  /// another service on that address, a captive portal, a reverse proxy with no
  /// route. Worth its own case because "cannot reach the server" sends the
  /// operator to look at their signal when the address itself is wrong.
  notChronicle,

  /// The stored address is not a URL we can build a request from. Reachable only
  /// by a hand-typed address; the QR path cannot produce it.
  malformedAddress,
}

/// The last thing a probe learned, and when.
///
/// [checkedAt] is carried because a status with no age is the same mistake
/// CLAUDE.md invariant 2 names for reference cards: *"a cache with no visible
/// staleness is a copy that lies."* The same applies to a connection banner —
/// "offline" from four hours ago is not a fact about now.
class ServerStatus {
  const ServerStatus({
    required this.reach,
    this.checkedAt,
    this.statusCode,
    this.detail,
  });

  const ServerStatus.unknown()
      : reach = Reach.unknown,
        checkedAt = null,
        statusCode = null,
        detail = null;

  final Reach reach;
  final DateTime? checkedAt;
  final int? statusCode;
  final String? detail;

  bool get isOk => reach == Reach.ok;

  /// True when retrying without changing anything could plausibly work. An
  /// Access-gated host and a malformed address cannot be fixed by waiting, and
  /// offering "retry" for them sends the operator in a circle.
  bool get isTransient =>
      reach == Reach.unreachable ||
      reach == Reach.timedOut ||
      reach == Reach.serverError;

  /// One line, in the app's voice, saying what is true.
  String get message => switch (reach) {
        Reach.unknown => 'Checking…',
        Reach.ok => 'Connected',
        Reach.unreachable => 'Cannot reach the server',
        Reach.timedOut => 'The server did not answer in time',
        Reach.accessGated =>
          'That address is behind Cloudflare Access. Scan the sign-in code '
              'again — the app needs the direct host.',
        Reach.unexpectedRedirect => 'That address redirected somewhere unexpected',
        Reach.serverError => 'The server answered with an error'
            '${statusCode == null ? '' : ' ($statusCode)'}',
        Reach.notChronicle =>
          'Something answered at that address, but it is not a Chronicle'
              '${statusCode == null ? '' : ' ($statusCode)'}',
        Reach.malformedAddress => 'That server address is not a URL',
      };
}

/// Probes one base URL. Separate from the controller so a test can drive it
/// directly with a stubbed client, and so one probe can be run against an
/// address that is not the stored one.
///
/// Note it is NOT used to vet an address before storing it, and cannot be:
/// [AuthController.signIn] stores the address first because the probe reads the
/// stored value and the generated client is built from it. The consequence is
/// small and worth knowing — a scanned host that turns out to be Access-gated is
/// persisted even though the app refused it, and the next scan overwrites it.
Future<ServerStatus> probeServer(
  String baseUrl, {
  http.Client? client,
  Duration timeout = const Duration(seconds: 6),
}) async {
  final normalized = normalizeServerUrl(baseUrl);
  final Uri uri;
  try {
    final parsed = Uri.parse('$normalized/healthz');
    if (!parsed.hasScheme || parsed.host.isEmpty) {
      return ServerStatus(reach: Reach.malformedAddress, checkedAt: DateTime.now());
    }
    uri = parsed;
  } on FormatException catch (e) {
    return ServerStatus(
      reach: Reach.malformedAddress,
      checkedAt: DateTime.now(),
      detail: e.message,
    );
  }

  // A probe wants the transport's redirect refusal and its deadline, and wants
  // no credential: /healthz takes none, and onboarding runs this before one
  // exists.
  final probe = ChronicleClient(inner: client, timeout: timeout);
  try {
    final response = await probe.get(uri);
    if (response.statusCode == 200) {
      return ServerStatus(reach: Reach.ok, checkedAt: DateTime.now());
    }
    return ServerStatus(
      reach:
          response.statusCode >= 500 ? Reach.serverError : Reach.notChronicle,
      checkedAt: DateTime.now(),
      statusCode: response.statusCode,
    );
  } on NotChronicleException catch (e) {
    return ServerStatus(
      reach: e.isAccessGated ? Reach.accessGated : Reach.unexpectedRedirect,
      checkedAt: DateTime.now(),
      statusCode: e.statusCode,
      detail: e.location,
    );
  } on TimeoutException {
    return ServerStatus(reach: Reach.timedOut, checkedAt: DateTime.now());
  } catch (e) {
    // Everything left is "nothing answered": a SocketException behind
    // http.ClientException, a DNS failure, a dropped connection. Naming
    // dart:io's types here would buy no accuracy — the advice is identical.
    return ServerStatus(
      reach: Reach.unreachable,
      checkedAt: DateTime.now(),
      detail: e.toString(),
    );
  } finally {
    // Only closes a client this call created. An injected one belongs to its
    // caller -- which is a test, since probeClientProvider gives the app null
    // and the app therefore makes one per probe. Closing a client the caller
    // still holds would break the next stubbed request.
    probe.close();
  }
}

/// The client every probe goes through. Null means "make one per probe", which
/// is what the app does; a test overrides it to drive the controller's own state
/// transitions rather than only [probeServer]'s return value.
final probeClientProvider = Provider<http.Client?>((ref) => null);

/// The app's live view of the server it is pointed at.
class ReachabilityController extends Notifier<ServerStatus> {
  /// Bumped per probe, captured before the await, and compared after: the
  /// answer to the LATEST probe wins, rather than whichever probe happens to
  /// finish last. This is the same guard, for the same reason, that
  /// `NoteView.vue` puts on its loaders (CHRN-110) -- and without it the
  /// failure is reachable with no unusual input:
  ///
  /// `main.dart` fires a probe at startup without awaiting it, and an
  /// unreachable server makes that probe run for its full 6 s deadline. Inside
  /// that window the operator pulls to refresh or taps Try again, the network is
  /// back, and the banner correctly says Connected at half a second. Then the
  /// startup probe times out and — with no guard — overwrites it with "did not
  /// answer in time", carrying a NEWER `checkedAt` because the timestamp is
  /// stamped at completion. The banner would flip back to an error with no user
  /// action, on the one screen the recovery clause is demonstrated on.
  int _seq = 0;

  @override
  ServerStatus build() => const ServerStatus.unknown();

  /// Re-probe. Safe to call repeatedly and concurrently: the answer to the most
  /// recently STARTED probe is the one that reaches [state].
  Future<ServerStatus> check() async {
    final seq = ++_seq;
    final base = ref.read(serverUrlProvider);
    if (base.isEmpty) {
      final status = ServerStatus(
        reach: Reach.malformedAddress,
        checkedAt: DateTime.now(),
        detail: 'no server address configured',
      );
      if (seq == _seq) state = status;
      return status;
    }
    final status =
        await probeServer(base, client: ref.read(probeClientProvider));

    // Superseded: a later probe has started, so this answer is about a moment
    // that has passed and must not become the app's state. It is still RETURNED
    // — the caller asked for this probe specifically, and AuthController.signIn
    // needs its own verdict about the address it just set.
    if (seq == _seq) state = status;
    return status;
  }
}

final reachabilityProvider =
    NotifierProvider<ReachabilityController, ServerStatus>(
  ReachabilityController.new,
);
