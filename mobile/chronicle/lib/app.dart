import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import 'router/router.dart';
import 'theme/theme.dart';

class ChronicleApp extends ConsumerWidget {
  const ChronicleApp({super.key});

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    return MaterialApp.router(
      title: 'Chronicle',
      debugShowCheckedModeBanner: false,
      theme: chronicleTheme(),
      routerConfig: ref.watch(routerProvider),
    );
  }
}
