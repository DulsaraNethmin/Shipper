import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:shipper/core/app.dart';
import 'package:shipper/core/auth/token_store.dart';

import 'core/auth/fake_token_store.dart';

void main() {
  testWidgets('the app boots through one ProviderScope and one router', (tester) async {
    await tester.pumpWidget(
      ProviderScope(
        // Overridden on the root scope rather than by wrapping the widget in a second
        // ProviderScope, which is the mistake main.dart's comment exists to prevent: a nested
        // scope gives its subtree a private copy of every provider, and a test that passes
        // against one is not testing the app's wiring.
        //
        // The token store is substituted because the real one reaches a platform channel that
        // a widget test has no plugin behind. Which shell it lands in is app_router_test.dart's
        // subject; this test is only about the boot path reaching a shell at all.
        overrides: [tokenStoreProvider.overrideWithValue(FakeTokenStore())],
        child: const ShipperApp(),
      ),
    );
    await tester.pumpAndSettle();

    // Asserts the wiring, not the copy: a MaterialApp.router resolved routerProvider, go_router
    // evaluated the redirect against a session that had to be restored first, and built a
    // screen from the result. Every one of those failing is silent at compile time.
    expect(find.byType(MaterialApp), findsOneWidget);
    expect(find.byType(Scaffold), findsOneWidget);
    expect(find.byKey(const Key('shell-signed-out')), findsOneWidget);
  });
}
