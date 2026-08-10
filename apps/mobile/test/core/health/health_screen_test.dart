import 'dart:async';

import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:shipper/core/api/api_client.dart';
import 'package:shipper/core/api/api_environment.dart';
import 'package:shipper/core/errors/api_failure.dart';
import 'package:shipper/core/health/health_repository.dart';
import 'package:shipper/core/health/health_screen.dart';
import 'package:shipper/core/health/health_status.dart';

Widget _screen({
  required FutureOr<HealthStatus> Function(Ref ref) health,
  ApiEnvironment environment = ApiEnvironment.local,
}) {
  return ProviderScope(
    overrides: [
      apiEnvironmentProvider.overrideWithValue(environment),
      healthProvider.overrideWith(health),
    ],
    child: const MaterialApp(home: HealthScreen()),
  );
}

void main() {
  testWidgets('shows the version the platform reported', (tester) async {
    await tester.pumpWidget(
      _screen(
        health: (ref) =>
            const HealthStatus(status: 'ok', version: 'v0.1.0', commit: 'c218f8b'),
      ),
    );
    await tester.pumpAndSettle();

    expect(find.byKey(const Key('health-version')), findsOneWidget);
    expect(find.text('v0.1.0'), findsOneWidget);
  });

  testWidgets('names the address it is talking to', (tester) async {
    // The most likely reason this screen fails is that it is pointed somewhere there is no
    // API, and a version that will not load says nothing about which address did not answer.
    await tester.pumpWidget(
      _screen(health: (ref) => const HealthStatus(status: 'ok', version: 'v0.1.0')),
    );
    await tester.pumpAndSettle();

    expect(find.byKey(const Key('health-base-url')), findsOneWidget);
    expect(find.text(ApiEnvironment.local.baseUrl(isAndroid: false)), findsOneWidget);
  });

  testWidgets("shows a failure in the platform's own words, with a way back", (tester) async {
    await tester.pumpWidget(
      _screen(health: (ref) => Future<HealthStatus>.error(const ApiUnreachable())),
    );
    await tester.pumpAndSettle();

    expect(find.byKey(const Key('health-error')), findsOneWidget);
    expect(find.text('Try again'), findsOneWidget);
    expect(find.byKey(const Key('health-version')), findsNothing);
  });

  testWidgets('shows progress before an answer arrives', (tester) async {
    final pending = Completer<HealthStatus>();
    addTearDown(() => pending.complete(const HealthStatus(status: 'ok', version: 'v')));

    await tester.pumpWidget(_screen(health: (ref) => pending.future));
    await tester.pump();

    expect(find.byType(CircularProgressIndicator), findsOneWidget);
  });
}
