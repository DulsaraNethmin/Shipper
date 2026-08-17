// The collector's own tests.
//
// **A guard nobody has tested is a guard.** Every other file in this repository that rests on
// `perceivable` is asserting an *absence*, and an absence is exactly what a broken collector reports
// — wave 11 recorded thirteen Dart tests that passed while asserting over empty lists, and wave 13
// closed a channel whose whole signature was that the visible screen did not change.
//
// So this file asserts the collector's **presences**: one test per channel, each rendering a string
// only that channel can carry, and each failing if the channel is dropped. Together with the
// anti-vacuity group they are what makes an empty result on a real screen mean something.

import 'package:flutter/material.dart';
import 'package:flutter/rendering.dart';
import 'package:flutter_test/flutter_test.dart';

import 'disclosure.dart';

/// A minimal app with [child] in it, on a surface big enough that nothing is culled for size.
Future<void> _pump(WidgetTester tester, Widget child) async {
  tallPhone(tester, height: 800);
  await tester.pumpWidget(
    MaterialApp(home: Scaffold(body: Center(child: child))),
  );
  await tester.pumpAndSettle();
}

void main() {
  group('every channel a string can reach a person by is collected', () {
    testWidgets('a plain Text', (tester) async {
      await _pump(tester, const Text('a plain sentence'));
      expect(await perceivable(tester), contains('a plain sentence'));
    });

    testWidgets('a Text.rich, which the wave-13 collector dropped', (tester) async {
      // `text.data ?? ''` is null for a `textSpan` Text, so the previous collector yielded the empty
      // string and filtered it away. Fully visible on screen and invisible to the guard.
      await _pump(tester, const Text.rich(TextSpan(text: 'a rich sentence')));
      expect(await perceivable(tester), contains('a rich sentence'));
    });

    testWidgets('a bare RichText, which is not a Text at all', (tester) async {
      await _pump(
        tester,
        RichText(text: const TextSpan(text: 'a bare rich sentence', style: TextStyle())),
      );
      expect(await perceivable(tester), contains('a bare rich sentence'));
    });

    testWidgets('a Text.rich inside ExcludeSemantics — visible, and in neither old branch',
        (tester) async {
      // The combination is the sharpest form of the wave-13 gap: `ExcludeSemantics` empties the
      // semantics branch and `Text.rich` empties the text branch, so a screen could show this to
      // somebody with both halves of the old collector live.
      await _pump(
        tester,
        const ExcludeSemantics(child: Text.rich(TextSpan(text: 'excluded but drawn'))),
      );
      expect(await perceivable(tester), contains('excluded but drawn'));
    });

    testWidgets('the spans of a Text.rich with children', (tester) async {
      await _pump(
        tester,
        const Text.rich(
          TextSpan(
            children: <InlineSpan>[
              TextSpan(text: 'first half '),
              TextSpan(text: 'second half'),
            ],
          ),
        ),
      );
      expect(await perceivable(tester), contains('first half second half'));
    });

    testWidgets('a semanticsLabel, which leaves the visible screen byte-identical', (tester) async {
      final collected = await () async {
        await _pump(tester, const Text('what is seen', semanticsLabel: 'what is said'));
        return perceivable(tester);
      }();

      expect(collected, contains('what is seen'));
      expect(collected, contains('what is said'));
    });

    testWidgets('a span-level semanticsLabel, in both directions', (tester) async {
      // `toPlainText` is called both ways for exactly this: with `includeSemanticsLabels: true` the
      // label replaces the visible text, so reading one way loses whichever half it does not ask
      // for.
      await _pump(
        tester,
        const Text.rich(TextSpan(text: 'seen in the span', semanticsLabel: 'heard in the span')),
      );

      final collected = await perceivable(tester);
      expect(collected, contains('seen in the span'));
      expect(collected, contains('heard in the span'));
    });

    testWidgets('what a text field is seeded with', (tester) async {
      await _pump(
        tester,
        TextField(controller: TextEditingController(text: 'seeded into the box')),
      );
      expect(await perceivable(tester), contains('seeded into the box'));
    });

    testWidgets('a tooltip, which carries no visible Text', (tester) async {
      await _pump(
        tester,
        IconButton(tooltip: 'spoken as a tooltip', onPressed: () {}, icon: const Icon(Icons.send)),
      );
      expect(await perceivable(tester), contains('spoken as a tooltip'));
    });

    testWidgets('increasedValue and decreasedValue, which the wave-13 walk never read',
        (tester) async {
      await _pump(
        tester,
        Semantics(
          increasedValue: 'stepped up to here',
          decreasedValue: 'stepped down to there',
          child: const SizedBox(width: 40, height: 40),
        ),
      );

      final collected = await perceivable(tester);
      expect(collected, contains('stepped up to here'));
      expect(collected, contains('stepped down to there'));
    });

    testWidgets('an identifier, which Text has its own parameter for', (tester) async {
      await _pump(tester, const Text('visible', semanticsIdentifier: 'an-identifier'));
      expect(await perceivable(tester), contains('an-identifier'));
    });

    testWidgets('a link URL', (tester) async {
      await _pump(
        tester,
        Semantics(
          link: true,
          linkUrl: Uri.parse('https://example.test/leaked?amount=52000'),
          child: const SizedBox(width: 40, height: 40),
        ),
      );
      expect(
        await perceivable(tester),
        contains('https://example.test/leaked?amount=52000'),
      );
    });

    testWidgets('an onTapHint, which is not a field on SemanticsData at all', (tester) async {
      // The channel reading more fields would never have found: `SemanticsHintOverrides` is
      // converted to an integer **id** before it reaches `SemanticsData`, and TalkBack speaks it as
      // "double tap to <hint>". `CustomSemanticsAction.getAction` maps the id back.
      await _pump(
        tester,
        Semantics(
          onTapHint: 'see what the customer will pay',
          onTap: () {},
          child: const SizedBox(width: 40, height: 40),
        ),
      );
      expect(await perceivable(tester), contains('see what the customer will pay'));
    });

    testWidgets('a custom semantics action, by the same route', (tester) async {
      await _pump(
        tester,
        Semantics(
          customSemanticsActions: <CustomSemanticsAction, VoidCallback>{
            const CustomSemanticsAction(label: 'reveal the maximum'): () {},
          },
          child: const SizedBox(width: 40, height: 40),
        ),
      );
      expect(await perceivable(tester), contains('reveal the maximum'));
    });
  });

  group('the wave-13 correction holds: a zero-size semantics node is culled', () {
    testWidgets('Semantics(label:, child: SizedBox.shrink()) reaches nobody', (tester) async {
      // Recorded because the previous orchestrator reported this shape as a live hole and the lane
      // refuted it. A zero-size node produces **no semantics node at all**, so the sentence
      // discloses to nobody — and a collector that "caught" it would be catching something a screen
      // reader never receives, which is a different kind of wrong.
      await _pump(
        tester,
        Column(
          children: <Widget>[
            const Text('something so the frame is not empty'),
            Semantics(
              label: 'the customer has set a maximum',
              child: const SizedBox.shrink(),
            ),
          ],
        ),
      );

      expect(await perceivable(tester), isNot(contains('the customer has set a maximum')));
    });

    testWidgets('but one pixel of it does', (tester) async {
      await _pump(
        tester,
        Column(
          children: <Widget>[
            const Text('something so the frame is not empty'),
            Semantics(
              label: 'the customer has set a maximum',
              child: const SizedBox(width: 1, height: 1),
            ),
          ],
        ),
      );

      expect(await perceivable(tester), contains('the customer has set a maximum'));
    });
  });

  group('it refuses to collect nothing', () {
    testWidgets('an empty frame throws rather than returning an empty list', (tester) async {
      // The anti-vacuity property, and the reason every absence asserted anywhere else means
      // something. "Collects nothing" must never be able to read as "found nothing".
      await _pump(tester, const SizedBox(width: 10, height: 10));

      // Caught by hand rather than through `expectLater`: the matcher would have to be handed a
      // future built outside `TestAsyncUtils.guardSync`, which the framework refuses.
      Object? thrown;
      try {
        await perceivable(tester);
      } on StateError catch (error) {
        thrown = error;
      }

      expect(thrown, isA<StateError>());
    });
  });

  group('money has exactly one door, and it is provenance rather than shape', () {
    test('a shape that allow-lists a bare amount is refused', () {
      // The wave-13 hole, closed structurally rather than by remembering not to write it. Both of
      // its computed money shapes are here verbatim.
      for (final shape in <RegExp>[
        RegExp(r'^\$-?[\d,]+\.\d{2}$'),
        RegExp(r'^\d+\.\d{2}$'),
      ]) {
        expect(
          () => expectOnlyRecordedStrings(
            <String>['anything'],
            surface: 'a surface',
            copy: <String>{'anything'},
            computed: <RegExp>[shape],
          ),
          throwsArgumentError,
          reason: 'the shape ${shape.pattern} waves a bare amount through',
        );
      }
    });

    test('an amount recorded as copy is refused', () {
      expect(
        () => expectOnlyRecordedStrings(
          <String>[r'$520.00'],
          surface: 'a surface',
          copy: <String>{r'$520.00'},
        ),
        throwsArgumentError,
      );
    });

    test('a declared amount passes, in every rendering the app produces', () {
      expectOnlyRecordedStrings(
        <String>[r'$520.00', '520.00', '52000', r'$1,500.00', '1,500.00', '1500', '1,500', '150000'],
        surface: 'a surface',
        copy: const <String>{},
        amounts: const <int>{52000, 150000},
      );
    });

    test('an amount nobody declared fails, with no phrase and no label anywhere near it', () {
      // The mutation this whole file was built for: the customer's maximum as a lone chip. No
      // field, no value in a model, no digit in a sentence, no banned phrase — and it fails.
      expect(
        () => expectOnlyRecordedStrings(
          <String>['Your price', r'$450.00', r'$520.00'],
          surface: 'a surface',
          copy: <String>{'Your price'},
          amounts: const <int>{45000},
        ),
        throwsA(isA<TestFailure>()),
      );
    });

    test('a count is not an amount, and does not need declaring as one', () {
      // `my_bids_screen` draws a bare per-group count. The line is drawn at a currency symbol, a
      // two-place fraction, or three digits.
      expect(readsAsAnAmount('3'), isFalse);
      expect(readsAsAnAmount('12'), isFalse);
      expect(readsAsAnAmount('520'), isTrue);
      expect(readsAsAnAmount(r'$5.00'), isTrue);
      expect(readsAsAnAmount('5.00'), isTrue);
      expect(readsAsAnAmount('Newtown NSW 2042'), isFalse);
    });
  });

  group('the closed world', () {
    test('an unrecorded string fails', () {
      expect(
        () => expectOnlyRecordedStrings(
          <String>['recorded', 'The customer has set a maximum.'],
          surface: 'a surface',
          copy: <String>{'recorded'},
        ),
        throwsA(isA<TestFailure>()),
      );
    });

    test('framework copy is exempt by exact string and never by category', () {
      expectOnlyRecordedStrings(
        <String>['Back', 'recorded'],
        surface: 'a surface',
        copy: <String>{'recorded'},
      );
    });

    test('a party’s own words pass, and only the exact ones', () {
      expectOnlyRecordedStrings(
        <String>['Is there a lift?'],
        surface: 'a surface',
        copy: const <String>{},
        typedByAParty: <String>{'Is there a lift?'},
      );

      expect(
        () => expectOnlyRecordedStrings(
          <String>['Is there a lift, or stairs?'],
          surface: 'a surface',
          copy: const <String>{},
          typedByAParty: <String>{'Is there a lift?'},
        ),
        throwsA(isA<TestFailure>()),
      );
    });
  });

  group('the taint direction', () {
    test('a budget on the wire that reached a pixel is named, wherever it is embedded', () {
      expect(
        () => expectNoTraceOfCents(
          <String>['Up to \$1,500.00 for this job'],
          cents: 150000,
        ),
        throwsA(isA<TestFailure>()),
      );
    });

    test('and a screen that drew none of it passes', () {
      expectNoTraceOfCents(<String>['Your price', r'$450.00'], cents: 150000);
    });
  });
}
