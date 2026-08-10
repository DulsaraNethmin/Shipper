import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:shipper/core/app.dart';
import 'package:shipper/core/health/health_repository.dart';
import 'package:shipper/core/health/health_status.dart';

void main() {
  testWidgets('the app boots through one ProviderScope and one router', (tester) async {
    await tester.pumpWidget(
      ProviderScope(
        // Overridden on the root scope rather than by wrapping the widget in a second
        // ProviderScope, which is the mistake main.dart's comment exists to prevent: a nested
        // scope gives its subtree a private copy of every provider, and a test that passes
        // against one is not testing the app's wiring.
        overrides: [
          healthProvider.overrideWith(
            (ref) => const HealthStatus(status: 'ok', version: 'v0.0.0-test'),
          ),
        ],
        child: const ShipperApp(),
      ),
    );
    await tester.pumpAndSettle();

    // Asserts the wiring, not the copy: a MaterialApp.router reached its initial route, which
    // means the scope resolved routerProvider and go_router built a screen from it. Every one
    // of those failing is silent at compile time.
    expect(find.byType(MaterialApp), findsOneWidget);
    expect(find.byType(Scaffold), findsOneWidget);
    expect(find.text('v0.0.0-test'), findsOneWidget);
  });
}
