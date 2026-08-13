// SHIP-82, SHIP-83 — the shape a provider is given a job in, decoded.
//
// Held against contracts/paths/fleet.yaml's `OpenJob` and `Region`, because the key names are the
// contract: a client reading `pickup_suburb` where the platform sends `pickup.suburb` shows an
// empty card with no error anywhere, on a build already on somebody's phone.
//
// # The budget half of this file is the point of it
//
// Docs/01 §4.3 and CLAUDE.md keep the customer's maximum away from a provider in every form. The
// platform proves the field never leaves the service (TestTheProviderResponseCarriesNoBudgetInAny
// Form holds the serialised response to a closed set of keys). This proves the other half: that a
// budget arriving anyway — renamed, added by a future version, or injected by anything between the
// two — has nowhere in this type to land.

import 'package:flutter_test/flutter_test.dart';
import 'package:shipper/features/jobs/job_status.dart';
import 'package:shipper/features/jobs/open_job.dart';

/// A job exactly as `GET /v1/jobs/open` sends one.
const _wire = <String, dynamic>{
  'id': '0198f2c1-6b40-7a11-9c3e-2f9a4d51b7e0',
  'status': 'open',
  'pickup': <String, dynamic>{'suburb': 'Newtown', 'state': 'NSW', 'postcode': '2042'},
  'dropoff': <String, dynamic>{'suburb': 'Geelong', 'state': 'VIC', 'postcode': '3220'},
  'goods_description': 'Three-seat sofa, wrapped',
  'length_cm': 190,
  'width_cm': 90,
  'height_cm': 85,
  'weight_kg': 65.0,
  'vehicle_requirement': 'Van or larger',
  'handling_notes': 'Second floor, no lift',
  'pickup_window': <String, dynamic>{'start': '2026-08-20T23:00:00.000Z'},
  'expires_at': '2026-08-25T03:30:00.000Z',
  'created_at': '2026-08-11T03:30:00.000Z',
};

void main() {
  group('decoding', () {
    test('reads every field the contract sends', () {
      final job = OpenJob.fromJson(_wire);

      expect(job.id, '0198f2c1-6b40-7a11-9c3e-2f9a4d51b7e0');
      expect(job.status, JobStatus.open);
      expect(job.pickup?.suburb, 'Newtown');
      expect(job.pickup?.state, 'NSW');
      expect(job.pickup?.postcode, '2042');
      expect(job.dropoff?.suburb, 'Geelong');
      expect(job.goodsDescription, 'Three-seat sofa, wrapped');
      expect(job.lengthCm, 190);
      expect(job.weightKg, 65.0);
      expect(job.vehicleRequirement, 'Van or larger');
      expect(job.handlingNotes, 'Second floor, no lift');
      expect(job.pickupWindow?.start, '2026-08-20T23:00:00.000Z');
      expect(job.expiresAt, '2026-08-25T03:30:00.000Z');
    });

    test('a job with two regions and nothing else decodes', () {
      // The ordinary case rather than a degenerate one: the wizard collects the addresses first,
      // so a job can be open to bids before its owner has described the goods. The platform omits
      // every field they have not filled in, and a client that required one would show a provider
      // nothing at all.
      final job = OpenJob.fromJson(<String, dynamic>{
        'id': 'a',
        'status': 'negotiating',
        'pickup': <String, dynamic>{'state': 'NT'},
        'created_at': '2026-08-11T03:30:00.000Z',
      });

      expect(job.status, JobStatus.negotiating);
      expect(job.pickup?.state, 'NT');
      expect(job.dropoff, isNull);
      expect(job.goodsDescription, isNull);
      expect(job.hasMeasurements, isFalse);
      expect(job.dimensions, isNull);
    });

    test('a status this build has never heard of decodes rather than throwing', () {
      // Docs/07 §6 is built on old builds living on devices indefinitely. A thirteenth status must
      // not make a job vanish from a provider's feed, and must not crash the app that cannot name
      // it — there is no over-the-air fix for either.
      final job = OpenJob.fromJson(<String, dynamic>{'id': 'a', 'status': 'auctioning'});

      expect(job.status, JobStatus.unknown);
    });

    test('a field this build has never heard of is ignored', () {
      // Docs/07 §6: server changes stay backward compatible by adding fields, and the client
      // tolerates them so an additive change needs no release.
      final job = OpenJob.fromJson(<String, dynamic>{
        ..._wire,
        'insurance_required': true,
        'distance_km': 812,
      });

      expect(job.id, '0198f2c1-6b40-7a11-9c3e-2f9a4d51b7e0');
    });
  });

  group('the budget has nowhere to land', () {
    test('a response carrying one decodes without it, under any name', () {
      // Not a search for the word. SHIP-83 found that a search catches `budget_cents` and misses
      // `max_price`, so the platform holds its response to a closed set of keys; this holds the
      // client's type to the same standard from the other direction — whatever the key is called,
      // there is no field for it and `toJson` cannot produce one.
      final job = OpenJob.fromJson(<String, dynamic>{
        ..._wire,
        'budget_cents': 150000,
        'max_price': 150000,
        'budget': 1500,
        'customer_maximum_cents': 150000,
      });

      final round = job.toJson();

      for (final key in round.keys) {
        expect(
          key,
          isNot(anyOf(
            contains('budget'),
            contains('price'),
            contains('maximum'),
            contains('amount'),
            contains('cents'),
          )),
          reason: 'OpenJob must carry no field a customer’s maximum could be read out of '
              '(Docs/01 §4.3). `$key` is one.',
        );
      }

      expect(round.values, isNot(contains(150000)));
      expect(round.values, isNot(contains(1500)));
    });

    test('every key the type does produce is one the contract names', () {
      // The closed-key-set assertion, on the client's side. A field added here later shows up as a
      // failing test naming it, which is the only mechanism that catches "somebody added a budget
      // and called it something else".
      expect(
        OpenJob.fromJson(_wire).toJson().keys.toSet(),
        <String>{
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
      );
    });
  });

  group('the region a provider is given', () {
    test('carries three parts and no street line', () {
      // SHIP-83 confirmed the decision for the single-job view and widened it: the coordinate is
      // withheld too, because `jobs` geocodes the whole address, so a pickup coordinate *is* the
      // street line written as two numbers. A client type with either field would be a client
      // asking for something the platform has decided not to send.
      final region = JobRegion.fromJson(<String, dynamic>{
        'suburb': 'Newtown',
        'state': 'NSW',
        'postcode': '2042',
        'line': '12 Smith Street',
        'coordinate': <String, dynamic>{'latitude': -33.9, 'longitude': 151.2},
      });

      expect(region.toJson().keys.toSet(), <String>{'suburb', 'state', 'postcode'});
      expect(region.oneLine, 'Newtown NSW 2042');
    });

    test('a region the platform sent nothing for is empty rather than blank text', () {
      expect(const JobRegion().isEmpty, isTrue);
      expect(const JobRegion(state: 'QLD').isEmpty, isFalse);
      expect(const JobRegion(state: 'QLD').oneLine, 'QLD');
    });

    test('a leading zero in a postcode survives', () {
      // 0800 is Darwin, and an int would lose it.
      expect(
        JobRegion.fromJson(<String, dynamic>{'postcode': '0800'}).postcode,
        '0800',
      );
    });
  });

  group('what a card reads off a job', () {
    test('dimensions read as a person writes them', () {
      expect(OpenJob.fromJson(_wire).dimensions, '190 × 90 × 85 cm');
    });

    test('a partly stated size is shown as far as it goes', () {
      // A customer may know the length of a sofa and not its height. Showing what they said is
      // better than showing nothing, and better than inventing a zero.
      final job = OpenJob.fromJson(<String, dynamic>{'id': 'a', 'status': 'open', 'length_cm': 190});

      expect(job.dimensions, '190 cm');
      expect(job.hasMeasurements, isTrue);
    });

    test('the pickup state is empty rather than null when there is no pickup', () {
      // The narrowing reads this for every job, and a job with no pickup must not become a chip
      // labelled with nothing.
      expect(OpenJob.fromJson(<String, dynamic>{'id': 'a', 'status': 'open'}).pickupState, '');
    });
  });
}
