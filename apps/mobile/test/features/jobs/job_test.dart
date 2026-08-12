// The `Job` schema, read against contracts/paths/jobs.yaml.
//
// A model test rather than a screen test, because what these check is decoding: the fields whose
// absence means something, the enum that must not throw on a value it has never seen, and the
// coordinate that is absent rather than zeroed. Every one of them is silent at compile time and
// only shows up on a device.

import 'package:flutter_test/flutter_test.dart';
import 'package:shipper/features/jobs/job.dart';
import 'package:shipper/features/jobs/job_status.dart';

/// The `Job` example from the contract, filled out.
const _job = <String, dynamic>{
  'id': '0198f2c1-6b40-7a11-9c3e-2f9a4d51b7e0',
  'status': 'open',
  'pickup': <String, dynamic>{
    'line': '12 Smith Street',
    'suburb': 'Newtown',
    'state': 'NSW',
    'postcode': '2042',
    'coordinate': <String, dynamic>{
      'latitude': -33.896,
      'longitude': 151.179,
      'formatted': '12 Smith Street, Newtown NSW 2042',
    },
  },
  'dropoff': <String, dynamic>{
    'line': 'Lot 14 Boundary Road',
    'suburb': 'Coonabarabran',
    'state': 'NSW',
    'postcode': '2357',
  },
  'goods_description': 'Two-seater sofa, wrapped, no legs attached',
  'length_cm': 190,
  'width_cm': 90,
  'height_cm': 85,
  'weight_kg': 45.5,
  'vehicle_requirement': 'Ute with a tailgate lifter',
  'handling_notes': 'Second-floor walk-up, no lift.',
  'pickup_window': <String, dynamic>{'start': '2026-08-14T09:00:00.000Z'},
  'budget_cents': 150000,
  'expires_at': '2026-08-25T03:30:00.000Z',
  'created_at': '2026-08-11T03:30:00.000Z',
  'updated_at': '2026-08-11T03:34:12.000Z',
};

void main() {
  group('decoding', () {
    test('reads the contract example', () {
      final job = Job.fromJson(_job);

      expect(job.id, '0198f2c1-6b40-7a11-9c3e-2f9a4d51b7e0');
      expect(job.status, JobStatus.open);
      expect(job.pickup?.suburb, 'Newtown');
      expect(job.weightKg, 45.5);
      expect(job.budgetCents, 150000);
      expect(job.expiresAt, '2026-08-25T03:30:00.000Z');
      expect(job.pickupWindow?.start, '2026-08-14T09:00:00.000Z');
      expect(job.dropoffWindow, isNull);
    });

    test('an empty draft decodes, because the platform omits what is empty', () {
      // The response to `POST /v1/jobs` with an empty body. Everything optional is *absent*
      // rather than sent as `""` and `0`, which is what lets a client tell "not filled in" from
      // "filled in with nothing" — and is why almost every field on Job is nullable.
      final job = Job.fromJson(<String, dynamic>{
        'id': '0198f2c1-6b40-7a11-9c3e-2f9a4d51b7e0',
        'status': 'draft',
        'created_at': '2026-08-11T03:30:00.000Z',
        'updated_at': '2026-08-11T03:30:00.000Z',
      });

      expect(job.status, JobStatus.draft);
      expect(job.pickup, isNull);
      expect(job.budgetCents, isNull, reason: 'no budget is not a budget of zero');
      expect(job.expiresAt, isNull, reason: 'a draft has no deadline; the clock starts at publish');
    });

    test('a field this build has never seen is ignored rather than fatal', () {
      // Docs/07 §6: the platform adds fields, and old builds live on devices indefinitely. A
      // client that refused what it did not recognise would turn every additive change into a
      // release.
      final job = Job.fromJson(<String, dynamic>{
        ..._job,
        'insurance_required': true,
        'pickup': <String, dynamic>{...(_job['pickup']! as Map<String, dynamic>), 'what3words': 'a.b.c'},
      });

      expect(job.id, '0198f2c1-6b40-7a11-9c3e-2f9a4d51b7e0');
      expect(job.pickup?.line, '12 Smith Street');
    });

    test('a thirteenth status decodes to unknown rather than throwing', () {
      // The alternative to tolerating it is crashing every installed build the day a status is
      // added, with no over-the-air fix. Same reasoning, and the same shape, as UserRole.unknown.
      final job = Job.fromJson(<String, dynamic>{..._job, 'status': 'impounded'});

      expect(job.status, JobStatus.unknown);
      expect(job.status.label, isNot(contains('Unknown')));
    });

    test('a whole-number weight arrives as a double', () {
      // `weight_kg` is a JSON number, and 45 is as valid as 45.5. A cast rather than a conversion
      // here would be a runtime failure for exactly the customers who typed a round number.
      final job = Job.fromJson(<String, dynamic>{..._job, 'weight_kg': 45});

      expect(job.weightKg, 45.0);
    });
  });

  group('a location the platform could not place', () {
    test('is resolved: false with no coordinate, and is not an error', () {
      final job = Job.fromJson(_job);

      expect(job.pickup!.resolved, isTrue);
      expect(job.pickup!.coordinate!.formatted, '12 Smith Street, Newtown NSW 2042');

      expect(job.dropoff!.resolved, isFalse);
      expect(job.dropoff!.coordinate, isNull);
      expect(job.dropoff!.line, 'Lot 14 Boundary Road',
          reason: 'the address is stored exactly as typed (SHIP-59a)');
    });

    test('resolution is the presence of the object, never a test on the numbers', () {
      // (0, 0) is a real point in the Gulf of Guinea. A client that inferred "unresolved" from
      // two zeroes would call a legitimate coordinate a failure, and vice versa.
      final atOrigin = JobLocation.fromJson(<String, dynamic>{
        'line': 'Somewhere',
        'suburb': 'Nowhere',
        'state': 'NSW',
        'postcode': '2000',
        'coordinate': <String, dynamic>{'latitude': 0, 'longitude': 0},
      });

      expect(atOrigin.resolved, isTrue);
    });
  });

  group('display', () {
    test('an address reads the way it is written on an envelope', () {
      expect(
        Job.fromJson(_job).pickup!.oneLine,
        '12 Smith Street, Newtown NSW 2042',
      );
    });

    test('a partly filled address still reads without stray punctuation', () {
      const suburbOnly = JobLocation(suburb: 'Newtown');
      expect(suburbOnly.oneLine, 'Newtown');
      expect(const JobLocation().oneLine, isEmpty);
      expect(const JobLocation().isEmpty, isTrue);
    });

    test('the state is printed as it arrived and is never lower-cased', () {
      // Docs/10 §4.7's one exception. The platform always answers with the upper-case
      // abbreviation and a client prints it; anything else would be this client re-deciding a
      // value it does not own.
      expect(Job.fromJson(_job).pickup!.state, 'NSW');
    });
  });

  group('AddressInput', () {
    test('always carries all four parts, so an emptied form clears the address', () {
      // The platform replaces an address as a whole rather than merging it part by part, so an
      // empty object means "there is no pickup address now". Omitting the fields would mean
      // "leave whatever is there alone", which is not what an emptied form means.
      expect(
        const AddressInput().toJson(),
        <String, Object?>{'line': '', 'suburb': '', 'state': '', 'postcode': ''},
      );
    });

    test('starts from a saved location without carrying its coordinate', () {
      final from = AddressInput.from(Job.fromJson(_job).pickup);

      expect(from.toJson(), <String, Object?>{
        'line': '12 Smith Street',
        'suburb': 'Newtown',
        'state': 'NSW',
        'postcode': '2042',
      });
    });
  });

  group('JobStatus', () {
    test('the twelve wire forms are the contract and are written out here', () {
      // Not derived, deliberately: these strings are published, and a build already branching on
      // `driver_assigned` cannot have it renamed underneath it. This is the Dart half of the same
      // pin the Go service keeps in TestStatusWireFormsAreStableAndDistinct, and both are
      // replaced by SHIP-56a's generator.
      expect(
        JobStatus.values.where((s) => s != JobStatus.unknown).map((s) => s.wireName).toList(),
        <String>[
          'draft',
          'open',
          'negotiating',
          'awarded',
          'driver_assigned',
          'en_route_to_pickup',
          'picked_up',
          'in_transit',
          'delivered',
          'completed',
          'cancelled',
          'disputed',
        ],
      );
    });

    test('the labels are the exact names in Docs/02 §1', () {
      expect(JobStatus.draft.label, 'Draft');
      expect(JobStatus.driverAssigned.label, 'Driver assigned');
      expect(JobStatus.enRouteToPickup.label, 'En route to pickup');
      expect(JobStatus.inTransit.label, 'In transit');
      expect(JobStatus.cancelled.label, 'Cancelled');
    });

    test('unknown is last, so values stays in Docs/02 §1 order for a screen that groups by it',
        () {
      expect(JobStatus.values.last, JobStatus.unknown);
      expect(JobStatus.values.first, JobStatus.draft);
    });
  });
}
