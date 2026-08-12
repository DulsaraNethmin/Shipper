import 'package:freezed_annotation/freezed_annotation.dart';

import 'package:shipper/features/jobs/job_status.dart';

part 'job.freezed.dart';
part 'job.g.dart';

/// A job as **its owner** sees it — the `Job` schema in `contracts/paths/jobs.yaml`.
///
/// One type for every operation that answers with a job: create, edit, read and list all return
/// the same shape, so a client parses one thing whatever it did to obtain it.
///
/// ## The budget is on this type because this type is the customer's own view
///
/// `Docs/01` §4.3 keeps the customer's maximum private from providers — not as an amount, not as
/// a band, and not as a "budget supplied" flag. Every endpoint that answers with this shape is
/// owner-only, which is what makes [budgetCents] safe here and nowhere else.
///
/// **There is no provider view of a job yet, and when SHIP-82 and SHIP-83 bring one it is a
/// separate type rather than this one with a field hidden.** The platform took that decision on
/// its own side for the same reason: a shape with a redaction step somebody has to remember is
/// the arrangement this rule is hardest to keep. `budget_stays_on_the_customer_side_test.dart` is
/// this client's version of the Go service's `TestOnlyTheOwnersResponseCarriesTheBudget` — it
/// fails when [budgetCents] is read anywhere outside the customer's own screens.
///
/// ## Why almost everything is nullable
///
/// A draft is mostly empty for most of its life, and the platform **omits** what is empty rather
/// than sending `""` and `0` — so a client can tell "not filled in" from "filled in with
/// nothing". Beyond that, `Docs/07` §6 makes a required field a decode that throws: only the two
/// fields this app actually branches on are required, so a field the platform stops sending
/// cannot crash an installed build. Same rule, and the same reasoning, as `Account`.
@freezed
abstract class Job with _$Job {
  const factory Job({
    required String id,

    /// **Never a settable field** (`Docs/02` §2, `CLAUDE.md`). It is read here and set nowhere:
    /// no request body in this API has a `status`, and one that arrived would be refused as an
    /// unknown field rather than quietly ignored.
    @JsonKey(unknownEnumValue: JobStatus.unknown) required JobStatus status,

    /// Where the goods are collected, once the customer has given an address (SHIP-71).
    JobLocation? pickup,

    /// Where they are delivered (SHIP-71).
    JobLocation? dropoff,

    @JsonKey(name: 'goods_description') String? goodsDescription,
    @JsonKey(name: 'length_cm') int? lengthCm,
    @JsonKey(name: 'width_cm') int? widthCm,
    @JsonKey(name: 'height_cm') int? heightCm,
    @JsonKey(name: 'weight_kg') double? weightKg,
    @JsonKey(name: 'vehicle_requirement') String? vehicleRequirement,
    @JsonKey(name: 'handling_notes') String? handlingNotes,
    @JsonKey(name: 'pickup_window') JobTimeWindow? pickupWindow,
    @JsonKey(name: 'dropoff_window') JobTimeWindow? dropoffWindow,

    /// The customer's own maximum, in **cents**, AUD (SHIP-67).
    ///
    /// Minor units as a whole number because money is never a float (`Docs/10` §3.3), and the
    /// name carries the unit because a field called `budget` holding `150000` is one somebody
    /// eventually reads as dollars.
    ///
    /// `null` means no budget was supplied, which is a different thing from a budget of zero —
    /// that distinction is the reason the platform omits the field rather than sending `0`.
    ///
    /// **Read it only on a customer surface.** See the note on this class.
    @JsonKey(name: 'budget_cents') int? budgetCents,

    /// When the job stops being offered (SHIP-68).
    ///
    /// Absent while the job is a draft: the clock starts at publication, and a draft may be saved
    /// and returned to indefinitely (`Docs/02` §6.3).
    @JsonKey(name: 'expires_at') String? expiresAt,

    @JsonKey(name: 'created_at') String? createdAt,
    @JsonKey(name: 'updated_at') String? updatedAt,
  }) = _Job;

  factory Job.fromJson(Map<String, dynamic> json) => _$JobFromJson(json);
}

/// An address, and where the platform resolved it to.
///
/// The four parts are what a form collects and what the platform stores. The street line stays
/// freeform on both sides: unit numbers, level numbers, lot numbers, PO boxes and roadside mail
/// boxes are all legitimate first lines of an Australian address.
@freezed
abstract class JobLocation with _$JobLocation {
  const factory JobLocation({
    @Default('') String line,
    @Default('') String suburb,

    /// The state or territory, as the **upper-case abbreviation** — `NSW`, `VIC`.
    ///
    /// A `String` and deliberately not an enum. `Docs/10` §4.7 records this as the one exception
    /// to lower snake case on the wire, taken because a client *prints* the state rather than
    /// branching on it — and an enum here would be a second list of the eight states to keep in
    /// step with the platform's, for no behaviour that depends on it. Input is accepted in any
    /// case and as the spelled-out name, so nothing needs normalising on the device either.
    @Default('') String state,

    /// Four digits, leading zero kept — `0800` is Darwin, and an `int` would lose it.
    @Default('') String postcode,

    /// Where the address resolved to, or `null` when it did not (SHIP-59a, SHIP-60).
    ///
    /// **A missing coordinate is an ordinary outcome and never an error.** The platform stores
    /// the address exactly as typed and the job proceeds; a client that treated this as a failure
    /// would block a rural delivery the marketplace exists to carry.
    Coordinate? coordinate,
  }) = _JobLocation;

  const JobLocation._();

  factory JobLocation.fromJson(Map<String, dynamic> json) => _$JobLocationFromJson(json);

  /// Whether the platform matched this address to a place.
  ///
  /// A test on the presence of the object, never on the numbers: (0, 0) is a real point in the
  /// Gulf of Guinea, so "we looked and found it" is a claim two floats cannot make.
  bool get resolved => coordinate != null;

  /// Whether the customer has supplied anything at all.
  bool get isEmpty =>
      line.isEmpty && suburb.isEmpty && state.isEmpty && postcode.isEmpty;

  /// The address the way a person writes it on an envelope: `12 Smith Street, Newtown NSW 2042`.
  String get oneLine {
    final tail = [suburb, state, postcode].where((p) => p.isNotEmpty).join(' ');
    return [line, tail].where((p) => p.isNotEmpty).join(', ');
  }
}

/// Where an address resolved to.
@freezed
abstract class Coordinate with _$Coordinate {
  const factory Coordinate({
    @Default(0.0) double latitude,
    @Default(0.0) double longitude,

    /// The geocoder's own rendering of the place it matched, which is not always what the
    /// customer typed.
    ///
    /// This is the field worth showing back: it answers "did the platform understand the address
    /// I gave it", which is a different and more useful question than a pair of numbers. Omitted
    /// when the provider supplied none.
    String? formatted,
  }) = _Coordinate;

  factory Coordinate.fromJson(Map<String, dynamic> json) => _$CoordinateFromJson(json);
}

/// When something may happen — a window rather than an instant, because road transport is not
/// scheduled to the minute (SHIP-73 collects it).
@freezed
abstract class JobTimeWindow with _$JobTimeWindow {
  const factory JobTimeWindow({String? start, String? end}) = _JobTimeWindow;

  factory JobTimeWindow.fromJson(Map<String, dynamic> json) => _$JobTimeWindowFromJson(json);
}

/// An address on its way **to** the platform, as the customer typed it (SHIP-71).
///
/// Separate from [JobLocation] rather than reusing it, and the difference is the point: a
/// [JobLocation] carries what the platform made of an address, and a client that could construct
/// one would be a client that could claim a coordinate. This carries the four parts and nothing
/// else.
///
/// **Nothing here validates and nothing here normalises.** `Docs/07` §2 puts the rules on the
/// platform, and the platform answers `validation_failed` with one `details` entry per offending
/// field in the shape a form can render. Two normalisers that disagree is how a client ends up
/// unable to reproduce its own address.
class AddressInput {
  const AddressInput({
    this.line = '',
    this.suburb = '',
    this.state = '',
    this.postcode = '',
  });

  /// What a customer returning to a saved draft starts from.
  factory AddressInput.from(JobLocation? location) {
    if (location == null) return const AddressInput();
    return AddressInput(
      line: location.line,
      suburb: location.suburb,
      state: location.state,
      postcode: location.postcode,
    );
  }

  final String line;
  final String suburb;
  final String state;
  final String postcode;

  /// The four parts, in the shape `contracts/paths/jobs.yaml` calls `Address`.
  ///
  /// All four are always sent, including when they are empty. The platform replaces an address as
  /// a whole rather than merging it part by part — merging would let an edit produce an address
  /// made of two different places — so an entirely empty object is how a customer *clears* one,
  /// and a partly filled one is refused with a `required` entry per missing part.
  Map<String, Object?> toJson() => <String, Object?>{
        'line': line,
        'suburb': suburb,
        'state': state,
        'postcode': postcode,
      };
}
