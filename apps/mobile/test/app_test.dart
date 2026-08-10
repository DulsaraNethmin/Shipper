import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:shipper/core/app.dart';

void main() {
  testWidgets('the app boots through one ProviderScope and one router', (tester) async {
    await tester.pumpWidget(const ProviderScope(child: ShipperApp()));
    await tester.pumpAndSettle();

    // Not a test of the placeholder's copy, which SHIP-19 replaces. It asserts the wiring:
    // a MaterialApp.router reached its initial route, which means the ProviderScope resolved
    // routerProvider and go_router built a screen from it. Every one of those failing is
    // silent at compile time.
    expect(find.byType(MaterialApp), findsOneWidget);
    expect(find.byType(Scaffold), findsOneWidget);
  });
}
