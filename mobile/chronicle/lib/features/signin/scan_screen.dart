import 'package:flutter/material.dart';
import 'package:mobile_scanner/mobile_scanner.dart';

import '../../auth/sign_in_link.dart';
import '../../theme/theme.dart';
import '../../theme/tokens.dart';

/// Scan the sign-in code another signed-in device is showing.
///
/// CHRN-106 renders it in **Account → Add device**, and the code carries both
/// halves this device needs: the invite and the address to redeem it against. On
/// a fresh install that is the only way the app learns where Chronicle is.
///
/// Pops with the parsed [SignInLink], or null if the operator backs out. This
/// screen owns the camera and nothing else; redeeming is the caller's business.
class ScanScreen extends StatefulWidget {
  const ScanScreen({super.key});

  @override
  State<ScanScreen> createState() => _ScanScreenState();
}

class _ScanScreenState extends State<ScanScreen> {
  final MobileScannerController _controller = MobileScannerController(
    formats: const [BarcodeFormat.qrCode],
    detectionSpeed: DetectionSpeed.noDuplicates,
  );

  // A QR resolves in one frame and the scanner keeps firing; without this the
  // screen pops several times off a single glance at the code.
  bool _handled = false;

  @override
  void dispose() {
    _controller.dispose();
    super.dispose();
  }

  void _onDetect(BarcodeCapture capture) {
    if (_handled) return;
    for (final barcode in capture.barcodes) {
      final link = parseSignInLink(barcode.rawValue ?? '');
      if (link != null) {
        _handled = true;
        Navigator.of(context).pop(link);
        return;
      }
    }
  }

  @override
  Widget build(BuildContext context) {
    return Scaffold(
      backgroundColor: chBase,
      body: Stack(
        children: [
          Positioned.fill(
            child: MobileScanner(controller: _controller, onDetect: _onDetect),
          ),
          SafeArea(
            child: Padding(
              padding: const EdgeInsets.all(space2),
              child: Row(
                children: [
                  IconButton(
                    onPressed: () => Navigator.of(context).maybePop(),
                    icon: const Icon(Icons.arrow_back, color: chText),
                    tooltip: 'Back',
                    // The epic's floor, and the reason it is stated on a plain
                    // IconButton: Material's default is 48 but a themed icon
                    // button can come out smaller.
                    constraints: const BoxConstraints(
                      minWidth: minTapTarget,
                      minHeight: minTapTarget,
                    ),
                  ),
                  const SizedBox(width: space2),
                  Text('SCAN SIGN-IN CODE', style: microLabel(color: chText)),
                ],
              ),
            ),
          ),
          Align(
            alignment: Alignment.bottomCenter,
            child: SafeArea(
              child: Padding(
                padding: const EdgeInsets.all(space4),
                child: Text(
                  'Account → Add device, on a device that is already signed in.',
                  textAlign: TextAlign.center,
                  style: monoMeta(color: chText2),
                ),
              ),
            ),
          ),
        ],
      ),
    );
  }
}
