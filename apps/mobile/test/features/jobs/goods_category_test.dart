// The goods catalogue as the platform serves it, checked against contracts/paths/jobs.yaml
// (SHIP-58).

import 'package:flutter_test/flutter_test.dart';
import 'package:shipper/features/jobs/goods_category.dart';

/// The catalogue exactly as the endpoint answers, keys and all.
const _served = <String, Object?>{
  'categories': <Object?>[
    <String, Object?>{
      'code': 'general_freight',
      'label': 'General freight',
      'description': 'Palletised or boxed goods needing no special handling.',
      'carried': true,
      'provisional': true,
    },
    <String, Object?>{
      'code': 'dangerous_goods',
      'label': 'Dangerous goods',
      'carried': false,
      'provisional': true,
    },
  ],
};

void main() {
  group('decoding the catalogue', () {
    test('reads the five fields the contract names', () {
      final catalogue = GoodsCatalogue.fromJson(_served);
      final first = catalogue.categories.first;

      expect(first.code, 'general_freight');
      expect(first.label, 'General freight');
      expect(first.description, 'Palletised or boxed goods needing no special handling.');
      expect(first.carried, isTrue);
      expect(first.provisional, isTrue);
    });

    test('an entry with no description is an ordinary entry', () {
      // The contract omits `description` when the catalogue gives none, rather than sending "".
      final catalogue = GoodsCatalogue.fromJson(_served);

      expect(catalogue.categories.last.description, isNull);
      expect(catalogue.categories.last.label, 'Dangerous goods');
    });

    test('an empty catalogue decodes rather than throwing', () {
      // A deployment configured with nothing carried is a deployment the platform refuses to
      // start, so this should not happen — but a client that threw on it would turn a server
      // misconfiguration into a crash on somebody's phone.
      expect(GoodsCatalogue.fromJson(<String, Object?>{}).categories, isEmpty);
    });

    test('a missing `carried` is a decode failure, not an assumed yes', () {
      // The contract marks it required and says why: "a missing field read as 'assume yes' would
      // offer a customer dangerous goods". This type takes the contract at its word — the failure
      // is contained to one form, which shows a banner and a retry, and that is a better outcome
      // than a silent wrong answer either way. `Job` takes the opposite decision, because it is
      // decoded on every screen in the app.
      expect(
        () => GoodsCatalogue.fromJson(<String, Object?>{
          'categories': <Object?>[
            <String, Object?>{'code': 'x', 'label': 'X', 'provisional': true},
          ],
        }),
        throwsA(isA<TypeError>()),
      );
    });
  });

  group('looking a code up', () {
    test('finds the entry a job names', () {
      final catalogue = GoodsCatalogue.fromJson(_served);

      expect(catalogue.byCode('dangerous_goods')?.label, 'Dangerous goods');
    });

    test('a code the catalogue no longer serves is null rather than an error', () {
      // A draft saved last week can name a category withdrawn since, and the customer has to be
      // able to see their own job.
      final catalogue = GoodsCatalogue.fromJson(_served);

      expect(catalogue.byCode('asbestos'), isNull);
      expect(catalogue.byCode(''), isNull);
      expect(catalogue.byCode(null), isNull);
    });
  });

  group('what the app may say about a category', () {
    test('refuses only what the catalogue says it does not carry', () {
      final catalogue = GoodsCatalogue.fromJson(_served);

      expect(catalogue.refuses('dangerous_goods'), isTrue);
      expect(catalogue.refuses('general_freight'), isFalse);
    });

    test('a code it has never heard of is not refused here', () {
      // This cannot tell "withdrawn from the list" from "never existed", and guessing either way
      // would be an authorisation decision on the device (`Docs/07` §3). The app shows what it
      // knows and lets POST /v1/jobs/{id}/publish decide.
      final catalogue = GoodsCatalogue.fromJson(_served);

      expect(catalogue.refuses('asbestos'), isFalse);
      expect(catalogue.refuses(null), isFalse);
    });
  });
}
