// The client's half of the platform's TestOnlyTheOwnersResponseCarriesTheBudget.
//
// Docs/01 §4.3 and CLAUDE.md keep the customer's maximum private from providers — not as an
// amount, not as a band, and not as a "budget supplied" flag. The platform enforces that where it
// has to be enforced: SHIP-82's provider feed and SHIP-83's provider job detail get response types
// of their own, and a Go test parses the jobs package's source and fails when any struct but the
// owner's response declares a `budget` json tag.
//
// This is the same statement in Dart, and it is not redundant with the platform's. The platform
// stops the field reaching a provider's device; this stops a *widget that renders it* being reused
// on a provider screen. That is the failure this rule is realistically broken by — a shared "job
// card" taking a budget and a flag saying whether to show it, where the flag is one careless call
// site away from being wrong and nothing fails.
//
// It is a source scan rather than a type test, for the same reason the Go one is: there is no
// type-level way to say "this may only be read here", and the alternative is a convention nobody
// can check.

import 'dart:io';

import 'package:flutter_test/flutter_test.dart';

/// The files allowed to name the budget, and why each one is.
///
/// **Adding a file here is a decision about Docs/01 §4.3 and should be argued for**, not a way to
/// make this test pass. The question to answer is whether every job the file can reach belongs to
/// the person looking at it. If the answer needs a "when" in it, the answer is no.
const _allowed = <String, String>{
  // The owner's view of a job, mirroring the one response shape in the API that carries it.
  'lib/features/jobs/job.dart': 'the owner-only Job schema',
  'lib/features/jobs/job.freezed.dart': 'generated from job.dart',
  'lib/features/jobs/job.g.dart': 'generated from job.dart',

  // The customer's own list. Every job on it belongs to the person looking at it, and the widget
  // that draws the amount is private to this file.
  'lib/features/jobs/customer_job_list.dart': 'the customer’s own jobs, and _JobCard is private',

  // One of the customer's own jobs, in full (SHIP-77). `GET /v1/jobs/{id}` is owner-only and
  // answers `404` to everybody else byte-identically to a job that does not exist, so every job
  // this screen can reach belongs to the person looking at it — which is the question this list
  // exists to ask, and it needs no "when" in the answer. SHIP-83's provider job detail is a
  // separate screen reading a separate type, exactly as the platform writes a second response
  // shape rather than redacting this one.
  'lib/features/jobs/job_detail_screen.dart': 'the owner’s own job, in full',
};

/// What a reference to the budget looks like in Dart or on the wire.
final _mentions = RegExp(r'budgetCents|budget_cents');

void main() {
  test('the budget is read only where every job belongs to the person looking at it', () {
    final offences = <String>[];

    for (final file in _dartFilesUnder(Directory('lib'))) {
      final path = file.path.replaceAll(Platform.pathSeparator, '/');
      if (_allowed.containsKey(path)) continue;

      // Comments are stripped first, so a doc comment *explaining* this rule — and several of
      // them do, which is the point of writing the reasoning down — is not mistaken for a read
      // of the field. String literals are deliberately kept: a hand-built `'budget_cents'` key
      // is exactly the kind of read this looks for.
      final source = _withoutComments(file.readAsStringSync());
      if (_mentions.hasMatch(source)) offences.add(path);
    }

    expect(
      offences,
      isEmpty,
      reason: '\n\nThe customer’s budget is never exposed to a provider, in any form\n'
          '(Docs/01 §4.3, CLAUDE.md). These files name it and are not on the list:\n\n'
          '  ${offences.join('\n  ')}\n\n'
          'If the screen is genuinely the owner’s own, add it to _allowed with the reason.\n'
          'If it is shared with a provider surface, it must not read the budget at all —\n'
          'write a second widget rather than a flag, which is what the platform does with a\n'
          'second response type rather than a redaction step.\n',
    );
  });

  test('every file on the allow list still exists', () {
    // An allow list that outlives its files is an allow list that quietly permits a path
    // somebody could recreate for something else entirely.
    for (final path in _allowed.keys) {
      expect(File(path).existsSync(), isTrue, reason: '$path is on the allow list and is gone');
    }
  });
}

String _withoutComments(String source) {
  return source
      .replaceAll(RegExp(r'/\*.*?\*/', dotAll: true), '')
      .replaceAll(RegExp(r'//.*$', multiLine: true), '');
}

Iterable<File> _dartFilesUnder(Directory dir) {
  if (!dir.existsSync()) return const <File>[];
  return dir.listSync(recursive: true).whereType<File>().where((f) => f.path.endsWith('.dart'));
}
