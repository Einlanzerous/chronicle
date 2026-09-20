import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../../api/reachability.dart';
import '../../api/server_url.dart';
import '../../auth/auth_controller.dart';
import '../../theme/theme.dart';
import '../../theme/tokens.dart';

/// What a signed-in device shows today: who it is, where it is pointed, and
/// whether that address is answering.
///
/// **This is deliberately not the capture screen.** One-tap capture is CHRN-60,
/// the confirm is CHRN-62 and batch triage is CHRN-63 — all three are
/// `review_mode: decision` or have boards of their own, and CHRN-59's job is the
/// skeleton underneath them. What this screen does own is the honest answer to
/// "is the server reachable", because that is the clause CHRN-59 has to
/// demonstrate.
class HomeScreen extends ConsumerWidget {
  const HomeScreen({super.key});

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final me = ref.watch(meProvider);
    final status = ref.watch(reachabilityProvider);
    final server = ref.watch(serverUrlProvider);

    return Scaffold(
      body: SafeArea(
        child: RefreshIndicator(
          onRefresh: () => ref.read(reachabilityProvider.notifier).check(),
          child: ListView(
            padding: const EdgeInsets.all(space4),
            children: [
              Text('CHRONICLE', style: microLabel(color: chSignal, size: sizeSm)),
              const SizedBox(height: space4),

              _StatusCard(status: status),
              const SizedBox(height: space4),

              Text('THIS DEVICE', style: microLabel()),
              const SizedBox(height: space2),
              me.when(
                data: (user) => Text(
                  user?.displayName ?? 'Signed out',
                  style: const TextStyle(fontSize: sizeMd, color: chText),
                ),
                loading: () => Text('Checking…', style: monoMeta()),
                // A failure here is the connection, and the card above already
                // says so in terms that distinguish the cases. Repeating a raw
                // exception would add noise, not information.
                error: (_, _) => Text('Not confirmed', style: monoMeta()),
              ),
              const SizedBox(height: space1),
              Text(server, style: monoMeta(size: sizeXs)),

              const SizedBox(height: space4),
              Text('CAPTURE', style: microLabel()),
              const SizedBox(height: space2),
              const Text(
                'Recording is not built yet.',
                style: TextStyle(fontSize: sizeBody, color: chText2),
              ),

              const SizedBox(height: space4),
              TextButton(
                onPressed: () async {
                  final revoked =
                      await ref.read(authControllerProvider.notifier).signOut();
                  if (!context.mounted) return;
                  ScaffoldMessenger.of(context).showSnackBar(
                    SnackBar(
                      content: Text(
                        revoked
                            ? 'Signed out.'
                            : 'Signed out on this device. The server was not '
                                'reached, so revoke this device from the device '
                                'list when you are back online.',
                      ),
                    ),
                  );
                },
                child: const Text('Sign out'),
              ),
            ],
          ),
        ),
      ),
    );
  }
}

class _StatusCard extends ConsumerWidget {
  const _StatusCard({required this.status});

  final ServerStatus status;

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    // Steel is tier 1's colour and the reserved pair belongs to Switchyard and
    // Amber, so a connection state gets the resolved green when it is fine and
    // plain text when it is not. Nothing here invents a colour.
    final tint = status.isOk ? chResolved : chTextMeta;

    return Container(
      padding: const EdgeInsets.all(space3),
      decoration: BoxDecoration(
        color: chRaised,
        borderRadius: BorderRadius.circular(space1),
        border: Border.all(color: chLine),
      ),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          Row(
            children: [
              Container(
                width: space1,
                height: space1,
                decoration: BoxDecoration(color: tint, shape: BoxShape.circle),
              ),
              const SizedBox(width: space2),
              Expanded(
                child: Text(
                  status.message,
                  style: const TextStyle(fontSize: sizeBody, color: chText),
                ),
              ),
            ],
          ),
          if (status.checkedAt != null) ...[
            const SizedBox(height: space1),
            // The age is shown rather than implied. CLAUDE.md invariant 2's rule
            // for reference cards -- "a cache with no visible staleness is a copy
            // that lies" -- is the same rule for a connection banner: "offline"
            // with no timestamp is not a claim about now.
            Text(
              'CHECKED ${_clock(status.checkedAt!)}',
              style: microLabel(size: sizeXxs),
            ),
          ],
          if (status.isTransient) ...[
            const SizedBox(height: space2),
            TextButton(
              onPressed: () => ref.read(reachabilityProvider.notifier).check(),
              child: const Text('Try again'),
            ),
          ],
        ],
      ),
    );
  }

  /// Local wall-clock time, which is what the rest of Chronicle renders.
  static String _clock(DateTime at) {
    final local = at.toLocal();
    final h = local.hour.toString().padLeft(2, '0');
    final m = local.minute.toString().padLeft(2, '0');
    return '$h:$m';
  }
}
