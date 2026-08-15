// The client's half of the platform's budget-privacy proofs.
//
// Docs/01 §4.3 and CLAUDE.md keep the customer's maximum private from providers — not as an
// amount, not as a band, and not as a "budget supplied" flag. The platform enforces that where it
// has to be enforced: SHIP-82's provider feed, SHIP-83's provider job detail and SHIP-84's bid
// response each get a shape of their own, and the serialised bytes of each are held to a **closed
// set of keys**, so a field arriving as `max_price` fails as surely as one arriving as
// `budget_cents`.
//
// This file is the same statement in Dart, and it is not redundant with the platform's: the
// platform stops the field reaching a provider's device, and this stops the *client* growing
// somewhere to put one.
//
// # Two guards, because one of them cannot see a rename — and that was measured, not assumed
//
// **A source scan**, below. It is a spelling check, and its job is the failure this rule is
// realistically broken by: a shared "job card" taking a budget and a flag saying whether to show
// it, where the flag is one careless call site away from being wrong and nothing fails. There is no
// type-level way to say "this may only be read here", and the alternative is a convention nobody
// can check.
//
// **A closed key set over every provider-facing model**, added at SHIP-100. Docs/11 §9 recorded the
// gap after wave 6 verified it by mutation: injecting a field called `max_price` into `OpenJob`
// **passes** the source scan, and injecting `budgetCents` fails it naming the file. Both mutations
// were re-run against this tree at SHIP-100 and both answers still held. So the source scan is kept
// for the job it does and this second guard is added for the one it cannot do — which is SHIP-83's
// own shape, brought across the wire: hold the decoded response to a closed set of keys rather than
// searching for a name.
//
// **The registry below is the mechanism, and it is deliberately fail-closed in two directions.** A
// field added to a registered type fails, whatever it is called. A *type* added to a registered file
// fails until somebody records its key set. What is left is a provider-facing model in a file
// nobody added to `_providerFacingFiles` — the same property `internal/boundaries` has on the Go
// side, and the same answer: adding one is a decision somebody records rather than something that
// happens.

import 'dart:convert';
import 'dart:io';

import 'package:flutter_test/flutter_test.dart';
import 'package:shipper/features/bidding/bid.dart';
import 'package:shipper/features/jobs/open_job.dart';

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

/// One decoded model held to a closed set of keys.
typedef _ClosedShape = ({
  /// Where the type is declared. Every `@freezed` model in this file must appear in the registry.
  String file,

  /// A round trip: decode the payload and encode it again, which is what a screen reads from.
  Map<String, dynamic> Function(Map<String, dynamic>) roundTrip,

  /// The smallest payload that decodes, so the salt below is the only interesting part of it.
  Map<String, dynamic> seed,

  /// **Every key this type may produce, and no others.** This is the whole guard: a field added to
  /// the type fails here whatever it is named, which is the axis a spelling check cannot have.
  Set<String> keys,
});

/// The models a **provider-facing** response decodes into.
///
/// A provider-facing shape is one obtained by an account that does not own the job: the feed and the
/// job detail (`GET /v1/fleet/jobs`, `GET /v1/fleet/jobs/{id}`) and a bid (`POST /v1/jobs/{id}/bids`
/// and its four siblings). The customer's own `Job` is not one of these and is on `_allowed` above.
final _providerFacing = <String, _ClosedShape>{
  'OpenJob': (
    file: 'lib/features/jobs/open_job.dart',
    roundTrip: (json) => OpenJob.fromJson(json).toJson(),
    seed: <String, dynamic>{'id': 'a', 'status': 'open'},
    keys: <String>{
      'id',
      'status',
      'pickup',
      'dropoff',
      'goods_description',
      'length_cm',
      'width_cm',
      'height_cm',
      'weight_kg',
      'vehicle_requirement',
      'handling_notes',
      'pickup_window',
      'dropoff_window',
      'expires_at',
      'created_at',
    },
  ),
  'JobRegion': (
    file: 'lib/features/jobs/open_job.dart',
    roundTrip: (json) => JobRegion.fromJson(json).toJson(),
    seed: <String, dynamic>{'suburb': 'Newtown', 'state': 'NSW', 'postcode': '2042'},
    // Three parts and no fourth. No street line and **no coordinate**: `jobs` geocodes the whole
    // address, so a pickup coordinate *is* the street line written as two numbers (SHIP-83).
    keys: <String>{'suburb', 'state', 'postcode'},
  ),
  'Bid': (
    file: 'lib/features/bidding/bid.dart',
    roundTrip: (json) => Bid.fromJson(json).toJson(),
    seed: <String, dynamic>{'id': 'a', 'job_id': 'b', 'status': 'submitted'},
    // `amount_cents` is the **provider's own price**, sent by this device. The contract says in as
    // many words that it has nothing to do with anything the customer set.
    keys: <String>{
      'id',
      'job_id',
      'status',
      'offered_by',
      'amount_cents',
      'pickup_at',
      'deliver_by',
      'message',
      'superseded_by',
      'created_at',
      'updated_at',
    },
  ),
};

/// The files the registry covers. A `@freezed` model in one of these and not in `_providerFacing`
/// fails the structural test below.
Set<String> get _providerFacingFiles =>
    _providerFacing.values.map((shape) => shape.file).toSet();

/// A customer's maximum, arriving under seven names and three values.
///
/// **Names rather than one name**, because that is the whole finding: a search catches
/// `budget_cents` and misses `max_price`. **Values that appear nowhere else**, so the second
/// assertion — that no rendering of the number survives the round trip — cannot pass by accident.
const _salt = <String, dynamic>{
  'budget_cents': 8675309,
  'max_price': 8675309,
  'budget': 86753,
  'customer_maximum_cents': 8675309,
  'ceiling_cents': 8675309,
  'willing_to_pay': 8675309,
  'reserve': 424242,
};

void main() {
  group('the source names the budget only where every job is the reader’s own', () {
    test('no other file mentions it', () {
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
  });

  group('every provider-facing model is held to a closed set of keys', () {
    // The guard the source scan cannot be. SHIP-83 put it this way on the platform and gave the
    // reason: a rule expressed as a list of forbidden names cannot say "no budget", because
    // `max_price` is a budget and does not contain the word.
    for (final entry in _providerFacing.entries) {
      final name = entry.key;
      final shape = entry.value;

      test('$name produces exactly the keys the contract names', () {
        final round = shape.roundTrip(<String, dynamic>{...shape.seed, ..._salt});

        expect(
          round.keys.toSet(),
          shape.keys,
          reason: '\n\n$name gained or lost a key (${shape.file}).\n\n'
              'A field added to a provider-facing model is a decision about Docs/01 §4.3,\n'
              'whatever it is called — `max_price` is a budget and does not contain the word.\n'
              'If the platform genuinely sends it, add it to the set in this file **and** say\n'
              'why. If it is a budget under another name, it does not belong on this side of\n'
              'the wire at all.\n',
        );
      });

      test('$name lets no rendering of the customer’s maximum through', () {
        // The value, not only the name. A key could be renamed to something innocuous and still
        // carry the number, which is why SHIP-83 searched the body as well as the key set.
        final encoded = jsonEncode(shape.roundTrip(<String, dynamic>{...shape.seed, ..._salt}));

        for (final value in _salt.values.toSet()) {
          expect(
            encoded,
            isNot(contains('$value')),
            reason: '$name carried $value through a round trip (${shape.file})',
          );
        }
      });

      test('$name names no key a maximum could be read out of', () {
        // Belt and braces beside the closed set above, and the cheaper failure to read: a key
        // called `budget_band` fails the set as well, and this says why in one line.
        for (final key in shape.keys) {
          expect(
            key,
            isNot(anyOf(contains('budget'), contains('price'), contains('maximum'))),
            reason: '$name declares `$key` (${shape.file})',
          );
        }
      });
    }

    test('every model declared in a provider-facing file is registered', () {
      // **This is what makes the guard structural rather than remembered.** A new type in one of
      // these files fails until somebody records its key set, which is the same fail-closed
      // arrangement `internal/boundaries` gives a ninth Go package.
      //
      // What it cannot reach is a provider-facing model in a file nobody added to the registry.
      // That is recorded rather than argued away: adding one is a decision, and the decision is
      // made here.
      final missing = <String>[];

      for (final path in _providerFacingFiles) {
        final source = _withoutComments(File(path).readAsStringSync());
        final declared = RegExp(r'abstract class (\w+) with _\$\1').allMatches(source);

        for (final match in declared) {
          final name = match.group(1)!;
          if (!_providerFacing.containsKey(name)) missing.add('$name in $path');
        }
      }

      expect(
        missing,
        isEmpty,
        reason: '\n\nA provider-facing model is not held to a closed key set:\n\n'
            '  ${missing.join('\n  ')}\n\n'
            'Add it to _providerFacing with the keys the contract names. That is what stops\n'
            'the next type carrying a budget under a name nobody thought to search for.\n',
      );
    });

    test('every registered file still exists', () {
      for (final path in _providerFacingFiles) {
        expect(File(path).existsSync(), isTrue, reason: '$path is registered and is gone');
      }
    });
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
