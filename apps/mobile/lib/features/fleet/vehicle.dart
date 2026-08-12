import 'package:freezed_annotation/freezed_annotation.dart';

part 'vehicle.freezed.dart';
part 'vehicle.g.dart';

/// What kind of vehicle it is — the `VehicleType` schema in `contracts/paths/fleet.yaml`.
///
/// Lower snake case on the wire (`Docs/10` §4.7), accepted in any case on input and always
/// returned in this form.
///
/// ## This list is compiled into the build, and that is a cost worth naming out loud
///
/// `CLAUDE.md` keeps anything that changes under operational pressure server-side, because Dart
/// has no over-the-air update path. A vocabulary held in an enum here is a vocabulary that needs a
/// store release to extend.
///
/// It is here anyway, and the alternative is what makes the case. These eleven values are a
/// **contract enum** rather than operational configuration: `contracts/paths/fleet.yaml`
/// enumerates them, the platform refuses anything outside the list with `not_allowed` on
/// `vehicle_type`, and **no endpoint serves the list**. A client that did not hold it would have to
/// offer a free-text box against a closed set, and every provider who typed "Box truck
/// (refrigerated)" would be refused with no way to discover what is accepted. Holding the list is
/// what makes a picker possible at all.
///
/// What that costs is bounded, and the boundary is [unknown]: a twelfth type added server-side
/// decodes rather than throws, is shown as a type this build cannot name, and is **never
/// overwritten by an edit** — see [VehicleInput.toJson]. An old build degrades to "I cannot offer
/// that one yet" rather than to a crash or to silent data loss.
///
/// **This is not the capability vocabulary a job's `vehicle_requirement` is checked against.** The
/// contract says so itself: that list belongs to SHIP-79, describes what a provider specialises in,
/// and `Docs/11` §3 records that `vehicle_requirement` stays free text until it arrives. Nothing
/// here may be reused for it.
enum VehicleType {
  @JsonValue('motorcycle')
  motorcycle,

  @JsonValue('car')
  car,

  @JsonValue('ute')
  ute,

  @JsonValue('van')
  van,

  @JsonValue('tray_truck')
  trayTruck,

  @JsonValue('box_truck')
  boxTruck,

  @JsonValue('refrigerated_truck')
  refrigeratedTruck,

  @JsonValue('flatbed')
  flatbed,

  @JsonValue('tipper')
  tipper,

  @JsonValue('prime_mover')
  primeMover,

  @JsonValue('trailer')
  trailer,

  /// A kind of vehicle this build has never heard of.
  ///
  /// Not a value the platform sends today, and never one a client may ask for. It exists because
  /// the alternative to decoding a twelfth type is **throwing** on it, and `Docs/07` §6 is built on
  /// old builds living on devices indefinitely. Same shape, and the same reasoning, as
  /// `JobStatus.unknown` and `UserRole.unknown`.
  ///
  /// Deliberately last, so [values] keeps the contract's own order.
  unknown;

  /// The types a provider may choose.
  ///
  /// [unknown] is absent: it is something the platform's future can say, never something this
  /// client may send.
  static const selectable = <VehicleType>[
    VehicleType.motorcycle,
    VehicleType.car,
    VehicleType.ute,
    VehicleType.van,
    VehicleType.trayTruck,
    VehicleType.boxTruck,
    VehicleType.refrigeratedTruck,
    VehicleType.flatbed,
    VehicleType.tipper,
    VehicleType.primeMover,
    VehicleType.trailer,
  ];

  /// What the wire calls this type.
  String get wireName => switch (this) {
        VehicleType.motorcycle => 'motorcycle',
        VehicleType.car => 'car',
        VehicleType.ute => 'ute',
        VehicleType.van => 'van',
        VehicleType.trayTruck => 'tray_truck',
        VehicleType.boxTruck => 'box_truck',
        VehicleType.refrigeratedTruck => 'refrigerated_truck',
        VehicleType.flatbed => 'flatbed',
        VehicleType.tipper => 'tipper',
        VehicleType.primeMover => 'prime_mover',
        VehicleType.trailer => 'trailer',
        VehicleType.unknown => 'unknown',
      };

  /// The type as a provider reads it, in the words Australian road transport uses.
  String get label => switch (this) {
        VehicleType.motorcycle => 'Motorcycle',
        VehicleType.car => 'Car',
        VehicleType.ute => 'Ute',
        VehicleType.van => 'Van',
        VehicleType.trayTruck => 'Tray truck',
        VehicleType.boxTruck => 'Box truck',
        VehicleType.refrigeratedTruck => 'Refrigerated truck',
        VehicleType.flatbed => 'Flatbed',
        VehicleType.tipper => 'Tipper',
        VehicleType.primeMover => 'Prime mover',
        VehicleType.trailer => 'Trailer',

        // Deliberately not "Unknown", which reads as a fault in the record. The vehicle is fine and
        // this build is old — the same distinction the shell draws for an account type it does not
        // recognise, and the same one `JobStatus.unknown` draws for a status.
        VehicleType.unknown => 'Not named by this version',
      };
}

/// A vehicle as **its owning provider** sees it — the `Vehicle` schema in
/// `contracts/paths/fleet.yaml`.
///
/// One type for every operation that answers with a vehicle: add, edit, read, list, deactivate and
/// reactivate all return the same shape, so a client parses one thing whatever it did to obtain it.
///
/// ## There is no customer view of a vehicle here, and there must not be one
///
/// `Docs/01` §4.3 lets a customer compare "provider profile, vehicle, and declared capability"
/// beside the bids on their job, and that shape arrives with SHIP-96 as a schema of its own — not
/// this one reached by another route. The platform took the same decision on its own side, for the
/// reason the budget rule already taught this codebase: one shape with a redaction step somebody
/// has to remember is the arrangement a privacy rule is hardest to keep.
///
/// ## Why almost everything is nullable, and why [active] is not
///
/// A vehicle added in a truck yard carries a plate and a type and often nothing else, and the
/// platform **omits** what is empty rather than sending `""` and `0` — so a client can tell "not
/// stated" from "stated as nothing". Beyond that, `Docs/07` §6 makes a required field a decode that
/// throws, so only what the app genuinely branches on is required.
///
/// [active] is required precisely because it is the field every fleet screen branches on. A boolean
/// defaulted on absence is wrong in both directions: default it true and a retired truck looks
/// available, default it false and a working one disappears. The contract guarantees it is always
/// present, which makes throwing the honest answer if it ever is not.
@freezed
abstract class Vehicle with _$Vehicle {
  const factory Vehicle({
    required String id,

    /// The plate, upper case with its spaces removed — the platform's stored form, not what was
    /// typed. `abc 123` and `ABC123` are one vehicle rather than two.
    required String registration,

    @JsonKey(name: 'vehicle_type', unknownEnumValue: VehicleType.unknown)
    required VehicleType vehicleType,

    /// Whether the vehicle is in service. See the note on this class.
    required bool active,

    String? make,
    String? model,

    /// The most it can carry, in **kilograms**. `null` means the provider has not stated one, which
    /// is a different thing from a capacity of zero.
    @JsonKey(name: 'max_weight_kg') double? maxWeightKg,

    /// The **load space**, in centimetres — not the vehicle's own length. A customer's 190 cm sofa
    /// has to fit inside, not alongside.
    @JsonKey(name: 'load_length_cm') int? loadLengthCm,
    @JsonKey(name: 'load_width_cm') int? loadWidthCm,
    @JsonKey(name: 'load_height_cm') int? loadHeightCm,

    /// When the provider took it out of service. **Absent while it is in service**, which is what
    /// lets a screen show "off the road since 3 Aug 2026" without a second field meaning the same
    /// thing as [active].
    @JsonKey(name: 'deactivated_at') String? deactivatedAt,

    @JsonKey(name: 'created_at') String? createdAt,
    @JsonKey(name: 'updated_at') String? updatedAt,
  }) = _Vehicle;

  const Vehicle._();

  factory Vehicle.fromJson(Map<String, dynamic> json) => _$VehicleFromJson(json);

  /// Whether the provider has stated anything about what this vehicle can carry.
  ///
  /// A vehicle with no capacity at all is ordinary — `Docs/01` §4.2's measure is that a provider can
  /// maintain a fleet, and somebody standing in a truck yard has the plate to hand and may not know
  /// the load height. A capacity heading with nothing under it is worse than no heading.
  bool get hasCapacity =>
      maxWeightKg != null ||
      loadLengthCm != null ||
      loadWidthCm != null ||
      loadHeightCm != null;

  /// The load space as a person reads it — `320 × 175 × 190 cm` — or `null` when none was stated.
  ///
  /// Partial is legitimate and is shown as far as it goes: a provider may know the length of a tray
  /// and not its height. Centimetres, because `CLAUDE.md` fixes the units and the contract carries
  /// them.
  String? get loadSpace {
    final parts = <String>[
      if (loadLengthCm case final length?) '$length',
      if (loadWidthCm case final width?) '$width',
      if (loadHeightCm case final height?) '$height',
    ];
    return parts.isEmpty ? null : '${parts.join(' × ')} cm';
  }
}

/// A vehicle on its way **to** the platform, as the provider filled the form in.
///
/// Separate from [Vehicle] rather than reusing it, and the difference is the point: a [Vehicle]
/// carries what the platform made of a vehicle — its normalised plate, whether it is in service,
/// when it came off the road — and a client that could construct one would be a client that could
/// claim those. This carries what a form collects and nothing else.
///
/// **There is no `active` here and there will not be one.** A vehicle leaves and re-enters service
/// through its own endpoints; the contract's request schema is `additionalProperties: false`, so a
/// client that sent `"active": false` is told the field does not exist rather than having it quietly
/// ignored — the failure worth preventing, because a provider who believes they took a truck off the
/// road and did not will keep receiving work for it.
///
/// **Nothing here validates and nothing here normalises.** `Docs/07` §2 puts the rules on the
/// platform: it upper-cases the plate, strips its spaces, collapses whitespace in the make, and
/// answers `validation_failed` with one `details` entry per offending field in the shape a form can
/// render. Two normalisers that disagree is how a provider ends up unable to find the vehicle they
/// have just added.
class VehicleInput {
  const VehicleInput({
    this.registration = '',
    this.vehicleType,
    this.make = '',
    this.model = '',
    this.maxWeightKg = 0,
    this.loadLengthCm = 0,
    this.loadWidthCm = 0,
    this.loadHeightCm = 0,
  });

  /// What an edit of [vehicle] starts from.
  ///
  /// The zeros and empty strings are what the platform means by "not stated", which is what makes
  /// the form round-trip: a field the provider has never filled in comes back empty, and one they
  /// empty deliberately is sent as the clearing value.
  factory VehicleInput.from(Vehicle vehicle) {
    return VehicleInput(
      registration: vehicle.registration,
      vehicleType: vehicle.vehicleType,
      make: vehicle.make ?? '',
      model: vehicle.model ?? '',
      maxWeightKg: vehicle.maxWeightKg ?? 0,
      loadLengthCm: vehicle.loadLengthCm ?? 0,
      loadWidthCm: vehicle.loadWidthCm ?? 0,
      loadHeightCm: vehicle.loadHeightCm ?? 0,
    );
  }

  final String registration;

  /// `null` until the provider has chosen one. See [toJson] for what that means on the wire.
  final VehicleType? vehicleType;

  final String make;
  final String model;

  /// Kilograms. `0` is "not stated", and sending it is how a provider clears a capacity they stated
  /// before.
  final double maxWeightKg;

  /// Centimetres of **load space**. `0` clears.
  final int loadLengthCm;
  final int loadWidthCm;
  final int loadHeightCm;

  /// The body of both writing operations — the `VehicleInput` schema in
  /// `contracts/paths/fleet.yaml`.
  ///
  /// ## Every field the form holds is sent, every time, and that is not a `PUT`
  ///
  /// The contract distinguishes three things: a field omitted is left alone, an empty value clears
  /// it, and a value sets it. A form that omitted what the provider had emptied would give them no
  /// way to take back a load height they once stated — the exact case the contract calls out — so
  /// what is on the form is what is sent, empties included.
  ///
  /// That is deliberately **not** the hazard `PATCH` exists to avoid. The contract's warning is
  /// about a client that has not been updated for a *new* field silently clearing it, and a field
  /// this build has never heard of is never named here, so it can never be cleared. What is sent is
  /// exactly what this build can show the provider, which is the only thing it can honestly claim to
  /// have asked them about.
  ///
  /// ## An unrecognised type is omitted rather than echoed back
  ///
  /// [vehicleType] is left out when it is `null` — nothing chosen yet, which the platform answers
  /// with `required` on a create — and when it is [VehicleType.unknown]. The second is the one worth
  /// stating: an old build editing a vehicle whose type was added after it shipped cannot name that
  /// type, and omitting the field is `PATCH`'s own way of saying "leave it as it was". Sending
  /// `"unknown"` would be refused, and — worse if it were ever accepted — would overwrite a correct
  /// record with this build's ignorance.
  Map<String, Object?> toJson() {
    final type = vehicleType;

    return <String, Object?>{
      'registration': registration,
      if (type != null && type != VehicleType.unknown) 'vehicle_type': type.wireName,
      'make': make,
      'model': model,
      'max_weight_kg': maxWeightKg,
      'load_length_cm': loadLengthCm,
      'load_width_cm': loadWidthCm,
      'load_height_cm': loadHeightCm,
    };
  }
}
