// Every channel by which a string becomes perceivable to somebody looking at — or listening to —
// one frame of this application, and the closed-world assertion built on top of them.
//
// # Why this exists, and why it is a shared file rather than a fourth copy
//
// `Docs/01` §4.3 — *a customer's budget is never exposed to a provider* — is the invariant this
// repository keeps almost holding. Four waves each built a better guard and each was beaten by a
// **different mechanism**, never by a better word:
//
// | Wave | Guard | Beaten by |
// |---|---|---|
// | 10 | a closed key set over the models | a **sentence** with no field, no value and no digit |
// | 11 | a word search | the same sentence |
// | 13 | a fourteen-phrase ban-list over rendered text | a **synonym** |
// | 13 | a closed-world set over rendered `Text`/`EditableText` | `Text(…, semanticsLabel: …)` — the
//        visible screen stays **byte-identical** and only what a screen reader speaks changes |
//
// Wave 13 closed all of that on **one screen**. `grep -rlE 'SemanticsNode|semanticsLabel'` over the
// test tree matched that screen's harness and nothing else, so every other provider-facing surface
// still let a semantics label through.
//
// **The lesson recorded in `Docs/11` §3 is not "write a better list".** It is that nobody had
// enumerated the channels by which a string reaches a provider. This file is that enumeration, made
// executable, in one place three screens share — so widening it widens every guard resting on it at
// once, which is the property four one-screen guards did not have.
//
// # What is collected, channel by channel
//
// - **`RichText`** — the render-level truth. Every `Text` builds one (`widgets/text.dart`), so this
//   subsumes `Text.data` *and* `Text.rich`, which the wave-13 collector dropped: it read
//   `text.data ?? ''`, and `data` is **null** for a `textSpan` `Text`, so a rich span yielded the
//   empty string and was filtered away. A bare `RichText` was not a `Text` at all.
// - **`Text`** — collected as well as its `RichText`, belt and braces. A `Text` inside a
//   `SelectionArea` builds `_SelectableTextContainer` instead, and a guard that silently stopped
//   seeing a screen because somebody made it selectable is the failure shape this whole file exists
//   to avoid.
// - **`InlineSpan.semanticsLabel`** — `toPlainText` is called **both ways**. With
//   `includeSemanticsLabels: true` a span's label replaces its visible text; with `false` the
//   visible text survives. Reading one way loses whichever half it does not ask for.
// - **`EditableText`** — what a field is *seeded* with. Rendered as visibly as anything else and
//   invisible to every `Text` finder.
// - **The semantics tree** — what VoiceOver and TalkBack actually receive. Walked from the root
//   through `visitChildren`, so what is collected is what is announced by construction rather than
//   by enumerating cases somebody thought of.
// - **Every string-bearing field of `SemanticsData`, not four of them.** The wave-13 walk read
//   `label`, `value`, `hint` and `tooltip`. On Flutter 3.44 the class also carries `identifier`,
//   `increasedValue`, `decreasedValue`, `maxValue`, `minValue`, `linkUrl` and `controlsNodes` — all
//   of which cross to the platform accessibility bridge, and `Text` has a `semanticsIdentifier:`
//   parameter that writes straight into one of them.
// - **`customSemanticsActionIds`** — the one channel that is not a field at all.
//   `SemanticsHintOverrides(onTapHint:)` and every `CustomSemanticsAction` are converted to integer
//   **ids** before they reach `SemanticsData`, so reading more fields would never have found them.
//   `CustomSemanticsAction.getAction(id)` maps the id back to its `label` and `hint`, and TalkBack
//   speaks the hint as "double tap to <hint>".
//
// # What no widget walk can see, and is therefore recorded rather than guarded
//
// `SemanticsService.announce`, `Clipboard.setData`, `launchUrl` query strings,
// `SystemChrome.setApplicationSwitcherDescription` and push-notification copy composed server-side.
// None is reachable from a `WidgetTester` frame. The enumeration in `Docs/11` §3 names each with its
// verdict and where its guard would have to live.
//
// # Two limits that apply to everything here, and are limits rather than defects
//
// **One frame, one state.** A dialog, a dropdown menu, a snackbar or an error state is collected
// only if the test drove the screen into it first.
//
// **The viewport is a constant, not a guarantee.** All three provider lists are `ListView(children:)`
// with no `itemBuilder`, so a widget below the fold is mounted in neither tree. [tallPhone] gives a
// 2600-logical-pixel surface, which is enough for the fixtures here and is not a proof about a
// longer one.

library;

import 'dart:io';

import 'package:flutter/material.dart';
// `rendering.dart` re-exports `semantics.dart`, which is where `SemanticsNode`, `SemanticsData` and
// `CustomSemanticsAction` live. Importing both would be an `unnecessary_import`, which this
// project's analyzer treats as fatal.
import 'package:flutter/rendering.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:shipper/shared/formatting/money.dart';

/// A phone-shaped surface, and a tall one, so that a list is mounted rather than culled.
///
/// See the note about the viewport at the top of this file: this raises the fold, it does not remove
/// it. A fixture long enough to overflow 2600 is a fixture whose tail is outside every guard here.
void tallPhone(WidgetTester tester, {double height = 2600}) {
  tester.view.physicalSize = Size(800, height);
  tester.view.devicePixelRatio = 1;
  addTearDown(tester.view.reset);
}

/// Every string this frame puts in front of somebody, across every channel a widget test can reach.
///
/// Merged nodes are split on newlines: a `Card` merges its descendants into one node whose label is
/// theirs joined by `\n`, so the **fragments** are what get checked. Otherwise one long
/// concatenation would match nothing recorded and would have to be waved through or listed whole.
///
/// Empty strings are dropped — an empty composer is not a sentence.
///
/// ## It turns semantics on itself, and both anti-vacuity checks are deliberate
///
/// A widget test builds **no semantics tree at all** unless a `SemanticsHandle` is held, so the
/// quiet failure available here is a collector that reads an empty tree, finds nothing to object to,
/// and reports success. "Collects some labels" is the shape that reads as a fix. Wave 11 recorded
/// thirteen Dart tests that passed while asserting over empty lists; this must never become the
/// fourteenth.
///
/// So it **throws** in two cases rather than returning nothing: when the semantics root is null, and
/// when the whole collection is empty. Neither can be reached by a screen that is drawing anything
/// at all, and both are what "found nothing" would otherwise be indistinguishable from.
///
/// The handle is enabled, pumped and disposed **inside** this call rather than left to the caller.
/// Leaving it out is worse in two ways: a test that forgets it gets exactly that silent pass, and
/// `addTearDown` is too late to dispose one — the framework verifies no handle is outstanding
/// *before* tear-downs run. The `pump` is not optional either: `ensureSemantics` marks the tree as
/// needing semantics and the tree is built on the **next** frame.
Future<List<String>> perceivable(WidgetTester tester) async {
  final handle = tester.ensureSemantics();
  try {
    await tester.pump();

    final collected = <String>[
      ..._rendered(tester),
      ..._seeded(tester),
      ..._announced(tester),
    ].expand((data) => data.split('\n')).map((data) => data.trim()).where((d) => d.isNotEmpty);

    final strings = collected.toList();
    if (strings.isEmpty) {
      throw StateError(
        'Nothing was collected from a frame that is supposed to be showing a screen. An empty '
        'collection here would silently pass every assertion resting on it, so it is refused '
        'rather than returned.',
      );
    }

    return strings;
  } finally {
    handle.dispose();
  }
}

/// [perceivable], lower-cased and joined — the open-world half, for a ban-list.
///
/// Kept because a ban-list fails **fast and readably** and is worth having beside the closed-world
/// assertion, not instead of it. On its own it is the rung wave 13 watched lose to a synonym.
Future<String> perceivableText(WidgetTester tester) async =>
    (await perceivable(tester)).map((data) => data.toLowerCase()).join('\n');

/// Everything drawn as text, from the render level rather than from the widget that built it.
List<String> _rendered(WidgetTester tester) {
  final strings = <String>[];

  for (final rich in tester.widgetList<RichText>(find.byType(RichText))) {
    strings
      ..add(rich.text.toPlainText(includeSemanticsLabels: false))
      ..add(rich.text.toPlainText());
  }

  for (final text in tester.widgetList<Text>(find.byType(Text))) {
    strings.add(text.data ?? '');
    strings.add(text.semanticsLabel ?? '');
    if (text.textSpan case final span?) {
      strings
        ..add(span.toPlainText(includeSemanticsLabels: false))
        ..add(span.toPlainText());
    }
  }

  return strings;
}

/// What every text field currently holds.
List<String> _seeded(WidgetTester tester) => tester
    .widgetList<EditableText>(find.byType(EditableText))
    .map((field) => field.controller.text)
    .toList();

/// Every string the accessibility layer would hand to the platform, from the whole semantics tree.
List<String> _announced(WidgetTester tester) {
  final root = _semanticsRoot(tester);
  if (root == null) {
    throw StateError(
      'Semantics are enabled and the tree is empty. Something has changed about how it is built, '
      'and an empty collection here would silently pass every assertion resting on it.',
    );
  }

  final spoken = <String>[];

  void walk(SemanticsNode node) {
    final data = node.getSemanticsData();

    spoken.addAll(<String>[
      data.label,
      data.value,
      data.hint,
      data.tooltip,
      // Announced by both screen readers when a value can be stepped, and absent from the wave-13
      // walk.
      data.increasedValue,
      data.decreasedValue,
      // `Text(semanticsIdentifier:)` writes here, and it crosses to `resource-id` on Android and to
      // `accessibilityIdentifier` on iOS.
      data.identifier,
      data.maxValue ?? '',
      data.minValue ?? '',
      data.linkUrl?.toString() ?? '',
      ...?data.controlsNodes,
    ]);

    // The channel that is not a field. `SemanticsHintOverrides(onTapHint:)` and every
    // `CustomSemanticsAction` are converted to ids before they reach `SemanticsData`, so no amount
    // of reading more fields would have found them. The registry maps them back.
    for (final id in data.customSemanticsActionIds ?? const <int>[]) {
      final action = CustomSemanticsAction.getAction(id);
      spoken.addAll(<String>[action?.label ?? '', action?.hint ?? '']);
    }

    node.visitChildren((child) {
      walk(child);
      return true;
    });
  }

  walk(root);
  return spoken;
}

/// The root of the semantics tree, found from the root pipeline owner.
///
/// `RendererBinding.pipelineOwner` is the one-liner and is **deprecated**, which this repository's
/// analyzer treats as fatal. The replacement is the owner *tree*: the root owner delegates the render
/// tree to a child, so the semantics owner is found by descending rather than by reading a property.
SemanticsNode? _semanticsRoot(WidgetTester tester) {
  SemanticsNode? found;

  void visit(PipelineOwner owner) {
    found ??= owner.semanticsOwner?.rootSemanticsNode;
    owner.visitChildren(visit);
  }

  visit(tester.binding.rootPipelineOwner);
  return found;
}

// ---------------------------------------------------------------------------------------------
// The assertion
// ---------------------------------------------------------------------------------------------

/// Strings **Flutter** puts on a screen, which no file of ours can be asked to contain.
///
/// A named list of exact strings, and deliberately **not** an exemption for a category. Exempting
/// "tooltips", or "semantics labels", or "anything the framework might contribute", is precisely the
/// move that let a semantics label through in the first place — a category exemption is a hole
/// shaped like whatever is put inside it.
const frameworkCopy = <String>{
  // `AppBar`'s automatic back button takes both from `MaterialLocalizations`.
  'Back',
  // `Scrollable` announces itself on Android.
  'Scrollable',
  // `RefreshIndicator`'s semantics, from `MaterialLocalizations.refreshIndicatorSemanticLabel`.
  'Refresh',
};

/// What a money amount looks like once it has been rendered, in every form this application
/// produces one.
///
/// `$520.00`, `520.00` (the counter form's own box has no symbol), `$1,500.00`, `1,500`, and the raw
/// cents. Anchored at both ends: an unanchored pattern would match a price *inside* a longer
/// sentence, and a longer sentence is a different failure that the closed-world check below owns.
final _moneyShaped = RegExp(r'^-?\$?[\d,]*\d(?:\.\d{1,2})?$');

/// Whether [value] is an amount rather than a small integer somebody is counting with.
///
/// A bare `3` is a group count; a bare `520` is a price. The line is drawn at a currency symbol, a
/// two-place fraction, or three digits — which is the narrowest rule that keeps `my_bids_screen`'s
/// per-group counts out and every rendering of a plausible budget in.
bool readsAsAnAmount(String value) {
  if (!_moneyShaped.hasMatch(value)) return false;
  return value.contains(r'$') ||
      value.contains('.') ||
      value.replaceAll(RegExp(r'[^\d]'), '').length >= 3;
}

/// Every way this application can render [cents].
Set<String> renderingsOfCents(int cents) {
  final aud = audFromCents(cents);
  final unsigned = aud.replaceFirst(r'$', '');

  return <String>{
    aud,
    unsigned,
    unsigned.replaceAll(',', ''),
    if (cents % 100 == 0) ...<String>{
      unsigned.substring(0, unsigned.length - 3),
      unsigned.substring(0, unsigned.length - 3).replaceAll(',', ''),
    },
    '$cents',
  };
}

/// The amounts a loose shape would have waved through, used to refuse one.
const _moneyProbes = <String>[r'$520.00', '520.00', '52000', r'$1,500.00', '1,500', '1500'];

/// Every string [perceived] on a surface must be one of five things, and anything else fails.
///
/// **Closed-world on purpose, and this is the whole mechanism.** A ban-list has to anticipate the
/// wording and loses to a paraphrase; this loses to nothing, because it fails on whatever it was not
/// told about. Adding a provider-visible string becomes a decision somebody recorded rather than
/// something that happened.
///
/// The five:
///
/// - [copy] — sentences, labels and buttons a person decided a provider may be shown.
/// - [framework] — [frameworkCopy] by default. Flutter's own contributions, by exact string.
/// - [typedByAParty] — the **exact** values this fixture typed. A customer who names their own limit
///   in a message has disclosed it themselves. Exact values and never a licence for free text.
/// - [amounts] — money, **declared in cents with a provenance rather than matched by shape.**
/// - [computed] — shapes for values the screen derives rather than writes: an instant, a count.
///
/// ## The shape-allow-list hole, and how this closes it
///
/// The wave-13 allow-set carried `RegExp(r'^\$-?[\d,]+\.\d{2}$')` and `RegExp(r'^\d+\.\d{2}$')`
/// among its computed shapes, so **any bare unlabelled amount was allow-listed by shape.** A
/// customer's budget rendered as a lone `$520.00` chip passed the closed-world assertion *and* every
/// phrase in the ban-list beside it: the guard could not tell the provider's own price from the
/// customer's maximum, because as strings they are the same kind of thing.
///
/// Money therefore does not go through [computed] here. It goes through [amounts], which is a set of
/// **cent values the test can say where it got** — the provider's own bid, the offer on the table —
/// expanded into every rendering this application produces. An amount from anywhere else is not in
/// the set and fails, whatever its shape.
///
/// Two argument checks keep the door shut rather than merely leaving it closed, because the hole was
/// re-openable by anybody writing a plausible-looking regex:
///
/// - a [computed] shape that matches any of [_moneyProbes] is **refused** with an `ArgumentError`
///   naming [amounts];
/// - a [copy] literal that [readsAsAnAmount] is refused the same way.
///
/// So there is exactly one door for money on a provider surface, and going through it means stating
/// where the number came from.
void expectOnlyRecordedStrings(
  List<String> perceived, {
  required String surface,
  required Set<String> copy,
  Set<String> typedByAParty = const <String>{},
  Set<int> amounts = const <int>{},
  List<RegExp> computed = const <RegExp>[],
  Set<String> framework = frameworkCopy,
}) {
  for (final shape in computed) {
    final waved = _moneyProbes.where(shape.hasMatch).toList();
    if (waved.isNotEmpty) {
      throw ArgumentError.value(
        shape.pattern,
        'computed',
        '\n\nThis shape allow-lists a bare money amount — it matches ${waved.join(', ')}.\n\n'
            'That is the hole wave 13 left open and this helper exists to close: a shape cannot\n'
            'tell the provider’s own price from the customer’s maximum, because as strings they\n'
            'are the same kind of thing. Declare the amounts in cents through `amounts:` instead,\n'
            'where each one has to come from somewhere the test can name.\n',
      );
    }
  }

  final smuggled = copy.where(readsAsAnAmount).toList();
  if (smuggled.isNotEmpty) {
    throw ArgumentError.value(
      smuggled.join(', '),
      'copy',
      '\n\nAn amount is recorded as copy. Amounts go through `amounts:` in cents, so that the\n'
          'test states where the number came from — Docs/01 §4.3 is about provenance and not\n'
          'about wording.\n',
    );
  }

  final allowedAmounts = amounts.expand(renderingsOfCents).toSet();

  // **Before the closed-world check, so that a leaked amount is reported as a leak** rather than as
  // an unrecorded string somebody would be tempted to paste into `copy`.
  final unaccounted = perceived
      .where(readsAsAnAmount)
      .where((data) => !allowedAmounts.contains(data))
      .where((data) => !typedByAParty.contains(data))
      .toSet();

  expect(
    unaccounted,
    isEmpty,
    reason: '\n\n$surface renders an amount of money that no declared value accounts for:\n\n'
        '  ${unaccounted.map((s) => '“$s”').join('\n  ')}\n\n'
        'Docs/01 §4.3: a customer’s budget is never exposed to a provider — not as an amount,\n'
        'not as a band, and not as a “budget supplied” indicator. A bare unlabelled amount is\n'
        'the form that carries no field, no label and no phrase, so it is the form every\n'
        'previous guard let through.\n\n'
        'If this is the provider’s own price, or an offer on the table, add its **cent value**\n'
        'to `amounts:` — which means saying in the test where the number came from. If it came\n'
        'from the customer’s maximum, in any wording at all, it is a product decision about\n'
        'Docs/01 §4.3 and belongs in the document before it belongs in a widget.\n',
  );

  final unrecorded = perceived
      .where((data) => !copy.contains(data))
      .where((data) => !framework.contains(data))
      .where((data) => !typedByAParty.contains(data))
      .where((data) => !allowedAmounts.contains(data))
      .where((data) => !computed.any((shape) => shape.hasMatch(data)))
      .toSet();

  expect(
    unrecorded,
    isEmpty,
    reason: '\n\n$surface puts a string in front of a **provider** that nothing in this file\n'
        'records:\n\n'
        '  ${unrecorded.map((s) => '“$s”').join('\n  ')}\n\n'
        'This assertion is closed-world on purpose. Docs/01 §4.3 forbids the customer’s maximum\n'
        'as an amount, as a band, **and as a “budget supplied” indicator** — and the third form\n'
        'is a sentence, which carries no field, no value and no digit. A list of banned phrases\n'
        'loses to a paraphrase; this one loses to nothing, because it fails on anything it was\n'
        'not told about.\n\n'
        'If the string above is legitimate copy, add it to this surface’s `copy` set — that is\n'
        'the decision being recorded, and it is a decision about what a provider may be told.\n'
        'If it is a value the screen computes, add its **shape** to `computed`, tightly enough\n'
        'that prose cannot match it — and note that a money shape is refused outright. If it is\n'
        'something a party typed, it belongs in `typedByAParty`, which is the fixture’s own\n'
        'strings and not a licence.\n\n'
        'If it relates an offer to what the customer can spend, in any wording at all, it is a\n'
        'product decision about Docs/01 §4.3 and belongs in the document before it belongs in a\n'
        'widget.\n',
  );
}

/// No rendering of [cents] appears anywhere in [perceived], as a whole string or inside one.
///
/// **The other direction, and it needs a different instrument.** [expectOnlyRecordedStrings] fails on
/// anything it was not told about, which is the guard against a disclosure nobody anticipated. This
/// one starts from a number the fixture deliberately put on the wire and proves it did not come out
/// the other end — including embedded in a sentence, where the closed-world check would report an
/// unrecorded string and say nothing about *why* it was unrecorded.
///
/// Use both. A screen that only has the first can be made to pass by pasting the leak into `copy`;
/// a screen that only has the second is guarded against the value in the fixture and against
/// nothing else.
void expectNoTraceOfCents(List<String> perceived, {required int cents, String? surface}) {
  final renderings = renderingsOfCents(cents);

  final found = <String>{};
  for (final data in perceived) {
    for (final rendering in renderings) {
      if (data.contains(rendering)) found.add('$rendering  (in “$data”)');
    }
  }

  expect(
    found,
    isEmpty,
    reason: '\n\nThe customer’s maximum reached ${surface ?? 'a provider surface'}:\n\n'
        '  ${found.join('\n  ')}\n\n'
        'Docs/01 §4.3, and CLAUDE.md’s invariant list: not as an amount, not as a band, and not\n'
        'as a “budget supplied” flag, through any endpoint or response — and a screen is the last\n'
        'place it can escape.\n',
  );
}

/// Every string in [recorded] still appears somewhere in the [sources] it claims to come from.
///
/// **The staleness direction.** An allow-list that outlives its copy quietly permits a sentence
/// somebody could reintroduce under a wording that was retired for a reason: a reworded line has to
/// be re-recorded rather than inherited, and a recorded string matching nothing is a slot the
/// closed-world assertion above would wave through.
///
/// Adjacent Dart string literals are joined before searching, because source wraps a long sentence
/// across two quoted parts and the rendered string has no such seam.
void expectEveryRecordedStringStillExists(
  Set<String> recorded, {
  required List<String> sources,
  required String surface,
  Set<String> exempt = frameworkCopy,
}) {
  final joined = sources
      .map((path) => File(path).readAsStringSync())
      .join('\n')
      .replaceAll(RegExp(r"'\s*\n\s*'"), '');

  final stale = recorded.where((copy) => !exempt.contains(copy) && !joined.contains(copy)).toList();

  expect(
    stale,
    isEmpty,
    reason: '\n\nThese strings are recorded as copy a provider may be shown on $surface, and no\n'
        'longer appear in its source:\n\n'
        '  ${stale.map((s) => '“$s”').join('\n  ')}\n\n'
        'Delete them, or correct them to what the screen now says. A recorded string that\n'
        'matches nothing is a slot the closed-world assertion would wave through.\n',
  );
}
