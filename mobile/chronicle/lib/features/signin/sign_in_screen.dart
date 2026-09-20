import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../../auth/auth_controller.dart';
import '../../auth/sign_in_link.dart';
import '../../theme/theme.dart';
import '../../theme/tokens.dart';
import 'scan_screen.dart';

/// The front door. Scan the code, or paste the link for a device whose camera
/// cannot — CHRN-106 shows the token beside the QR for exactly that case.
class SignInScreen extends ConsumerStatefulWidget {
  const SignInScreen({super.key});

  @override
  ConsumerState<SignInScreen> createState() => _SignInScreenState();
}

class _SignInScreenState extends ConsumerState<SignInScreen> {
  final _pasted = TextEditingController();
  bool _busy = false;
  String? _error;

  @override
  void dispose() {
    _pasted.dispose();
    super.dispose();
  }

  Future<void> _scan() async {
    final link = await Navigator.of(context).push<SignInLink>(
      MaterialPageRoute(builder: (_) => const ScanScreen()),
    );
    if (link == null || !mounted) return;
    await _redeem(link);
  }

  Future<void> _paste() async {
    final link = parseSignInLink(_pasted.text);
    if (link == null) {
      setState(() => _error = const SignInResult.failed(
            SignInFailure.notASignInLink,
          ).message);
      return;
    }
    await _redeem(link);
  }

  Future<void> _redeem(SignInLink link) async {
    setState(() {
      _busy = true;
      _error = null;
    });
    final result = await ref.read(authControllerProvider.notifier).signIn(
          baseUrl: link.baseUrl,
          inviteToken: link.token,
        );
    if (!mounted) return;
    setState(() {
      _busy = false;
      _error = result.ok ? null : result.message;
    });
    // On success the router's redirect takes over: a held token is what makes
    // the home route reachable, so there is no navigation to do here.
  }

  @override
  Widget build(BuildContext context) {
    return Scaffold(
      body: SafeArea(
        child: Center(
          child: SingleChildScrollView(
            padding: const EdgeInsets.all(space4),
            child: ConstrainedBox(
              // The canvas's frame is 412 wide; this keeps the column honest on
              // a tablet rather than stretching a phone layout across it.
              constraints: const BoxConstraints(maxWidth: 412),
              child: Column(
                crossAxisAlignment: CrossAxisAlignment.stretch,
                children: [
                  Text('CHRONICLE', style: microLabel(color: chSignal, size: sizeSm)),
                  const SizedBox(height: space2),
                  const Text(
                    'Add this device',
                    style: TextStyle(fontSize: 26, color: chText),
                  ),
                  const SizedBox(height: space2),
                  const Text(
                    'Scan the sign-in code from Account → Add device on a '
                    'device that is already signed in. The code carries both '
                    'the address and the invite.',
                    style: TextStyle(fontSize: sizeBase, color: chText2),
                  ),
                  const SizedBox(height: space4),
                  FilledButton.icon(
                    onPressed: _busy ? null : _scan,
                    icon: const Icon(Icons.qr_code_scanner),
                    label: const Text('Scan sign-in code'),
                  ),
                  const SizedBox(height: space4),
                  Text('OR PASTE THE LINK', style: microLabel()),
                  const SizedBox(height: space2),
                  TextField(
                    controller: _pasted,
                    enabled: !_busy,
                    autocorrect: false,
                    enableSuggestions: false,
                    keyboardType: TextInputType.url,
                    style: monoMeta(color: chText, size: sizeBody),
                    decoration: const InputDecoration(
                      hintText: 'https://…/sign-in?token=…',
                    ),
                    onSubmitted: (_) => _busy ? null : _paste(),
                  ),
                  const SizedBox(height: space2),
                  TextButton(
                    onPressed: _busy ? null : _paste,
                    child: const Text('Use this link'),
                  ),
                  if (_busy) ...[
                    const SizedBox(height: space4),
                    const Center(child: CircularProgressIndicator()),
                  ],
                  if (_error != null) ...[
                    const SizedBox(height: space4),
                    // Not coral and not gold: CLAUDE.md invariant 2 reserves
                    // those two for Switchyard and Amber references anywhere in
                    // the estate, and a sign-in error is neither.
                    Container(
                      padding: const EdgeInsets.all(space3),
                      decoration: BoxDecoration(
                        color: chRaised,
                        borderRadius: BorderRadius.circular(space1),
                        border: Border.all(color: chLine),
                      ),
                      child: Text(
                        _error!,
                        style: const TextStyle(fontSize: sizeBody, color: chText),
                      ),
                    ),
                  ],
                ],
              ),
            ),
          ),
        ),
      ),
    );
  }
}
