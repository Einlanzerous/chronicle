/// The CAPTURE / QUEUE tab bar at the foot of board 1a's screens.
///
/// The design's own mockup carries a third tab, WIKI. There is no mobile
/// wiki screen -- that is E8, the web client -- so this renders the two
/// tabs that actually go somewhere rather than a destination that does
/// nothing when tapped.
library;

import 'package:flutter/material.dart';
import 'package:go_router/go_router.dart';

import '../../router/router.dart';
import '../../theme/theme.dart';
import '../../theme/tokens.dart';

class BottomCaptureQueueTabs extends StatelessWidget {
  const BottomCaptureQueueTabs({super.key, required this.current});

  /// [captureRoute] or [queueRoute].
  final String current;

  @override
  Widget build(BuildContext context) => DecoratedBox(
        decoration: const BoxDecoration(
          color: chBase,
          border: Border(top: BorderSide(color: chLine)),
        ),
        child: Row(
          children: [
            _Tab(
              label: 'CAPTURE',
              active: current == captureRoute,
              onTap: () => context.go(captureRoute),
            ),
            _Tab(
              label: 'QUEUE',
              active: current == queueRoute,
              onTap: () => context.go(queueRoute),
            ),
          ],
        ),
      );
}

class _Tab extends StatelessWidget {
  const _Tab({required this.label, required this.active, required this.onTap});

  final String label;
  final bool active;
  final VoidCallback onTap;

  @override
  Widget build(BuildContext context) => Expanded(
        child: InkWell(
          onTap: onTap,
          child: Container(
            constraints: const BoxConstraints(minHeight: minTapTarget + space2),
            padding: const EdgeInsets.symmetric(vertical: space2),
            child: Column(
              mainAxisAlignment: MainAxisAlignment.center,
              children: [
                Text(label, style: microLabel(color: active ? chSignal : chTextMeta, size: sizeXs)),
                const SizedBox(height: 5),
                Container(
                  width: 20,
                  height: 2,
                  color: active ? chSignal : Colors.transparent,
                ),
              ],
            ),
          ),
        ),
      );
}
