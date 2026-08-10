import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import 'package:shipper/core/routing/app_router.dart';
import 'package:shipper/shared/design_system/app_theme.dart';

/// The application widget.
///
/// A [ConsumerWidget] because the router is itself a provider: `Docs/07` §2 keeps
/// role-specific navigation genuinely separate inside one app, which means the route set
/// depends on session state rather than being a constant. Reading it here is what lets the
/// whole navigation graph change when the session does, without a second [MaterialApp].
class ShipperApp extends ConsumerWidget {
  const ShipperApp({super.key});

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final router = ref.watch(routerProvider);

    return MaterialApp.router(
      title: 'Shipper',
      theme: AppTheme.light,
      darkTheme: AppTheme.dark,
      routerConfig: router,
    );
  }
}
