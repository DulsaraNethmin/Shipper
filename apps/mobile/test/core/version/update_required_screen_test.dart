// SHIP-168 — the prompt, in both of the shapes the platform can put it in.
//
// The *Done when* says "blocks with an update prompt linking to the store", and the platform is
// explicitly allowed to send no link — `IOS_STORE_URL` and `ANDROID_STORE_URL` both default to
// empty and `internal/config/config.go` records that as a decision. So there are two shapes, the
// pilot ships the second, and the second is the one that can go wrong quietly: a screen with a
// headline and nothing under it reads as a screen that failed to load.

import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:shipper/core/version/update_required_screen.dart';

void main() {
  Future<List<Uri>> pumpScreen(
    WidgetTester tester, {
    required String? storeUrl,
    Future<bool> Function(Uri)? open,
  }) async {
    final opened = <Uri>[];
    await tester.pumpWidget(
      MaterialApp(
        home: UpdateRequiredScreen(
          storeUrl: storeUrl,
          openStore: (destination) async {
            opened.add(destination);
            return open == null ? true : await open(destination);
          },
        ),
      ),
    );
    await tester.pumpAndSettle();
    return opened;
  }

  group('with a link', () {
    testWidgets('offers a button, and it goes where the platform said', (tester) async {
      final opened = await pumpScreen(tester, storeUrl: 'https://apps.apple.com/app/id123');

      expect(find.byKey(const Key('update-required-title')), findsOneWidget);
      expect(find.byKey(const Key('update-open-store')), findsOneWidget);
      expect(find.byKey(const Key('update-no-store-link')), findsNothing);

      await tester.tap(find.byKey(const Key('update-open-store')));
      await tester.pumpAndSettle();

      expect(opened, [Uri.parse('https://apps.apple.com/app/id123')]);
    });

    testWidgets('a link that does not open falls back to saying where to go', (tester) async {
      // `launchUrl` returns false when nothing on the device handles the destination. A button
      // that appears to do nothing is worse than no button at all, on a screen with no way off it.
      await pumpScreen(
        tester,
        storeUrl: 'https://apps.apple.com/app/id123',
        open: (_) async => false,
      );

      await tester.tap(find.byKey(const Key('update-open-store')));
      await tester.pumpAndSettle();

      expect(find.byKey(const Key('update-no-store-link')), findsOneWidget);
      expect(find.byKey(const Key('update-store-url')), findsOneWidget);
      expect(find.text('https://apps.apple.com/app/id123'), findsOneWidget);
    });

    testWidgets('a link that throws does the same, rather than becoming an error', (tester) async {
      await pumpScreen(
        tester,
        storeUrl: 'https://apps.apple.com/app/id123',
        open: (_) async => throw StateError('no activity found'),
      );

      await tester.tap(find.byKey(const Key('update-open-store')));
      await tester.pumpAndSettle();

      expect(tester.takeException(), isNull);
      expect(find.byKey(const Key('update-no-store-link')), findsOneWidget);
    });
  });

  group('without a link, which is what the pilot ships', () {
    testWidgets('still says what to do, and cannot be read as a screen that failed', (tester) async {
      await pumpScreen(tester, storeUrl: null);

      // The same headline and the same explanation as the shape with a button. What differs is
      // only the last element.
      expect(find.byKey(const Key('update-required-title')), findsOneWidget);
      expect(find.byKey(const Key('update-required-body')), findsOneWidget);
      expect(find.byKey(const Key('update-no-store-link')), findsOneWidget);

      // The three things that would make it look broken rather than deliberate.
      expect(find.byType(CircularProgressIndicator), findsNothing);
      expect(find.byKey(const Key('update-open-store')), findsNothing);
      expect(find.byKey(const Key('update-store-url')), findsNothing);
    });

    testWidgets('the instruction is true wherever the build came from', (tester) async {
      // Pilot distribution is TestFlight and Play internal testing (Docs/01 §8) and a public
      // listing comes later, so the sentence names none of them and stays correct through all
      // three. It is asserted verbatim because copy is the whole deliverable in this shape.
      await pumpScreen(tester, storeUrl: null);

      expect(
        find.text(
          'Open the app store you installed Shipper from and install the latest version.',
        ),
        findsOneWidget,
      );
    });

    testWidgets('an empty store_url is treated as no link', (tester) async {
      await pumpScreen(tester, storeUrl: '');

      expect(find.byKey(const Key('update-open-store')), findsNothing);
      expect(find.byKey(const Key('update-no-store-link')), findsOneWidget);
    });

    testWidgets('a destination with no scheme is not a link', (tester) async {
      // `launchUrl` would refuse it, leaving a button that does nothing. Better to show the shape
      // that at least tells somebody what to do.
      await pumpScreen(tester, storeUrl: 'apps.apple.com/app/id123');

      expect(find.byKey(const Key('update-open-store')), findsNothing);
      expect(find.byKey(const Key('update-no-store-link')), findsOneWidget);
    });
  });
}
