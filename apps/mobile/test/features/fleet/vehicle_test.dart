// The vehicle model, against contracts/paths/fleet.yaml.
//
// Two things are being held here and they pull in opposite directions. Docs/07 §6 requires a client
// to tolerate a response field it has never heard of, so an additive server change needs no app
// release — and the same document requires the client not to *invent* what it was not told, so
// "absent" and "empty" stay distinguishable all the way to the screen.

import 'package:flutter_test/flutter_test.dart';
import 'package:shipper/features/fleet/vehicle.dart';

/// A vehicle exactly as `contracts/paths/fleet.yaml` documents one.
const _full = <String, Object?>{
  'id': '0198f2c1-6b40-7a11-9c3e-2f9a4d51b7e0',
  'registration': 'ABC123',
  'vehicle_type': 'van',
  'make': 'Mercedes-Benz',
  'model': 'Sprinter 314 CDI MWB',
  'max_weight_kg': 1200,
  'load_length_cm': 320,
  'load_width_cm': 175,
  'load_height_cm': 190,
  'active': true,
  'created_at': '2026-08-11T03:30:00.000Z',
  'updated_at': '2026-08-11T03:34:12.000Z',
};

/// What a vehicle added in a truck yard actually looks like: a plate, a type, and everything else
/// omitted rather than sent empty.
const _bare = <String, Object?>{
  'id': '0198f2c1-6b40-7a11-9c3e-2f9a4d51b7e1',
  'registration': 'XYZ789',
  'vehicle_type': 'ute',
  'active': true,
  'created_at': '2026-08-11T03:30:00.000Z',
  'updated_at': '2026-08-11T03:30:00.000Z',
};

void main() {
  group('decoding', () {
    test('reads every field the contract documents', () {
      final vehicle = Vehicle.fromJson(_full);

      expect(vehicle.id, '0198f2c1-6b40-7a11-9c3e-2f9a4d51b7e0');
      expect(vehicle.registration, 'ABC123');
      expect(vehicle.vehicleType, VehicleType.van);
      expect(vehicle.active, isTrue);
      expect(vehicle.make, 'Mercedes-Benz');
      expect(vehicle.model, 'Sprinter 314 CDI MWB');
      expect(vehicle.maxWeightKg, 1200);
      expect(vehicle.loadLengthCm, 320);
      expect(vehicle.loadWidthCm, 175);
      expect(vehicle.loadHeightCm, 190);
      expect(vehicle.deactivatedAt, isNull);
    });

    test('an omitted optional field is null, not zero and not empty', () {
      // The distinction the platform omits fields to preserve: "not stated" is a different thing
      // from "stated as nothing", and a client that defaulted these would show a provider a load
      // space of 0 × 0 × 0 cm for a truck they simply have not measured.
      final vehicle = Vehicle.fromJson(_bare);

      expect(vehicle.make, isNull);
      expect(vehicle.model, isNull);
      expect(vehicle.maxWeightKg, isNull);
      expect(vehicle.loadSpace, isNull);
      expect(vehicle.hasCapacity, isFalse);
    });

    test('a vehicle off the road carries the day it came off it', () {
      final vehicle = Vehicle.fromJson(<String, Object?>{
        ..._bare,
        'active': false,
        'deactivated_at': '2026-08-11T03:34:12.000Z',
      });

      expect(vehicle.active, isFalse);
      expect(vehicle.deactivatedAt, '2026-08-11T03:34:12.000Z');
    });

    test('a field this build has never heard of is ignored rather than fatal', () {
      // Docs/07 §6. An additive server change must not break a build already on a phone.
      final vehicle = Vehicle.fromJson(<String, Object?>{..._full, 'tail_lift': true});

      expect(vehicle.registration, 'ABC123');
    });

    test('a vehicle type added after this build shipped decodes as unknown', () {
      // The alternative is throwing, which on a phone with no over-the-air fix is a fleet screen
      // that cannot be opened again until the store approves a release.
      final vehicle = Vehicle.fromJson(<String, Object?>{..._full, 'vehicle_type': 'road_train'});

      expect(vehicle.vehicleType, VehicleType.unknown);
      expect(vehicle.vehicleType.label, isNot(contains('Unknown')));
    });

    test('the load space is shown as far as it goes', () {
      final partial = Vehicle.fromJson(<String, Object?>{..._bare, 'load_length_cm': 320});

      expect(partial.loadSpace, '320 cm');
      expect(Vehicle.fromJson(_full).loadSpace, '320 × 175 × 190 cm');
    });
  });

  group('the type vocabulary', () {
    test('every selectable type has a wire name the contract enumerates', () {
      // The eleven values in contracts/paths/fleet.yaml, in the contract's own order.
      expect(
        VehicleType.selectable.map((t) => t.wireName),
        <String>[
          'motorcycle',
          'car',
          'ute',
          'van',
          'tray_truck',
          'box_truck',
          'refrigerated_truck',
          'flatbed',
          'tipper',
          'prime_mover',
          'trailer',
        ],
      );
    });

    test('unknown is never offered as a choice', () {
      // It is something the platform's future can say and never something this client may ask for.
      expect(VehicleType.selectable, isNot(contains(VehicleType.unknown)));
      expect(VehicleType.values.last, VehicleType.unknown);
    });
  });

  group('the body of a write', () {
    test('names every field the form holds, so an emptied box clears its value', () {
      // The contract's middle row: absent leaves a value alone and empty clears it. A form that
      // omitted what somebody had emptied would give them no way to take back a load height they
      // once stated.
      const emptied = VehicleInput(registration: 'ABC123', vehicleType: VehicleType.van);

      expect(emptied.toJson(), <String, Object?>{
        'registration': 'ABC123',
        'vehicle_type': 'van',
        'make': '',
        'model': '',
        'max_weight_kg': 0.0,
        'load_length_cm': 0,
        'load_width_cm': 0,
        'load_height_cm': 0,
      });
    });

    test('sends the contract\'s field names exactly', () {
      // The platform refuses unknown fields — httpx.DecodeJSON is strict — so a key name is not a
      // detail. `load_length` rather than `load_length_cm` is a 400 from a build already installed.
      const input = VehicleInput(
        registration: 'ABC 123',
        vehicleType: VehicleType.boxTruck,
        make: 'Isuzu',
        model: 'NPR 45-155',
        maxWeightKg: 4500,
        loadLengthCm: 620,
        loadWidthCm: 240,
        loadHeightCm: 230,
      );

      expect(input.toJson().keys, <String>[
        'registration',
        'vehicle_type',
        'make',
        'model',
        'max_weight_kg',
        'load_length_cm',
        'load_width_cm',
        'load_height_cm',
      ]);
      // Sent as typed. The platform upper-cases the plate and strips its spaces; a second
      // normaliser here is how a provider ends up unable to find the vehicle they just added.
      expect(input.toJson()['registration'], 'ABC 123');
      expect(input.toJson()['vehicle_type'], 'box_truck');
    });

    test('omits the type when nothing has been chosen', () {
      // Which the platform answers with `required` on a create — its rule to state, not this
      // client's to pre-empt with a guess.
      expect(const VehicleInput(registration: 'ABC123').toJson().containsKey('vehicle_type'), isFalse);
    });

    test('omits a type this build cannot name, rather than overwriting it', () {
      // An old build editing a vehicle whose type was added after it shipped. Omitting the field is
      // PATCH's own way of saying "leave it as it was"; sending "unknown" would be refused, and if
      // it were ever accepted would replace a correct record with this build's ignorance.
      const input = VehicleInput(
        registration: 'ABC123',
        vehicleType: VehicleType.unknown,
        make: 'Kenworth',
      );

      expect(input.toJson().containsKey('vehicle_type'), isFalse);
      expect(input.toJson()['make'], 'Kenworth');
    });

    test('an edit starts from what the platform stored', () {
      final vehicle = Vehicle.fromJson(_full);
      final input = VehicleInput.from(vehicle);

      expect(input.registration, 'ABC123');
      expect(input.vehicleType, VehicleType.van);
      expect(input.maxWeightKg, 1200);
      expect(input.loadHeightCm, 190);
    });

    test('a field the platform never stored starts empty rather than absent', () {
      // Which is what makes the form round-trip: the zeros and empty strings are exactly what the
      // platform means by "not stated", so re-sending them changes nothing.
      final input = VehicleInput.from(Vehicle.fromJson(_bare));

      expect(input.make, '');
      expect(input.maxWeightKg, 0);
      expect(input.loadLengthCm, 0);
    });

    test('there is no way to send an active flag', () {
      // A vehicle leaves and re-enters service through its own endpoints. The contract's request
      // schema is additionalProperties:false, so a client that sent one would be told the field
      // does not exist — which is better than the alternative, but better still is not having one.
      const input = VehicleInput(registration: 'ABC123', vehicleType: VehicleType.van);

      expect(input.toJson().containsKey('active'), isFalse);
      expect(input.toJson().containsKey('deactivated_at'), isFalse);
    });
  });
}
