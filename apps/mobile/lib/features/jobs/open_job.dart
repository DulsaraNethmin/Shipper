import 'package:freezed_annotation/freezed_annotation.dart';

import 'package:shipper/features/jobs/job.dart';
import 'package:shipper/features/jobs/job_status.dart';

part 'open_job.freezed.dart';
part 'open_job.g.dart';

/// A job as **a provider deciding whether to bid** sees it — the `OpenJob` schema in
/// `contracts/paths/fleet.yaml` (SHIP-82, SHIP-83).
///
/// One type for both operations that answer with one: the feed and the single job. The platform
/// answers both with the same shape deliberately, so a client parses one thing whatever it did to
/// obtain the job.
///
/// ## This is a second type rather than [Job] with fields hidden, and that is the whole design
///
/// `Docs/01` §4.3 keeps the customer's maximum private from providers — **not as an amount, not as
/// a band, and not as a "budget supplied" flag**. The platform enforces that by writing a separate
/// response shape with no field a budget could go in, rather than one shape and a redaction step
/// somebody has to remember; `TestTheProviderResponseCarriesNoBudgetInAnyForm` holds the serialised
/// response to a **closed set of keys**, so a budget renamed `max_price` fails too.
///
/// This type is the client's half of that decision. There is no budget field here, there is no
/// placeholder for one, and no screen reading this type may draw an affordance implying a customer
/// stated a maximum. A key the platform does not send is a key `json_serializable` never reads, so
/// a budget arriving by accident cannot reach a widget — but the rule is the absence of the field,
/// not the accident of the decoder.
///
/// `test/features/jobs/budget_stays_on_the_customer_side_test.dart` fails if this file, or any
/// screen reading it, names the budget at all.
///
/// ## Three things the platform withholds, which a client must not invent
///
/// **No street line.** A provider prices a job on the locality, the distance and the state; the
/// doorstep is needed by whoever drives to it, which is after an award. See [JobRegion].
///
/// **No coordinate.** `jobs` geocodes the whole address, so a pickup coordinate *is* the street
/// line written as two numbers — a shape that withheld `line` and carried `coordinate` would have
/// kept the letter of the decision and broken it entirely. [JobRegion] therefore has no
/// [Coordinate] and must never grow one.
///
/// **No customer.** `Docs/01` §4.3 lets a *customer* compare provider profiles once bids arrive;
/// nothing gives the reverse before an award.
///
/// ## Why almost everything is nullable
///
/// The platform **omits** what the customer has not stated rather than sending `""` and `0`, so a
/// client can tell "not stated" from "stated as nothing". Beyond that, `Docs/07` §6 makes a
/// required field a decode that throws: only what this app genuinely branches on is required, so a
/// field the platform stops sending cannot crash a build already on a phone.
///
/// [status] is required because the feed branches on it — a job with bids on it is one a provider
/// is bidding against company, and that is worth knowing before they price it.
@freezed
abstract class OpenJob with _$OpenJob {
  const factory OpenJob({
    required String id,

    /// `open` or `negotiating`, and nothing else can reach here.
    ///
    /// **Never a settable field** (`Docs/02` §2, `CLAUDE.md`). It is read here and set nowhere.
    @JsonKey(unknownEnumValue: JobStatus.unknown) required JobStatus status,

    /// Where the goods are collected, at the grain a provider is given it.
    ///
    /// Present for every job this endpoint can return — the eligibility filter matches a declared
    /// region against the pickup state or postcode, so a job with neither cannot be in the feed —
    /// and nullable anyway, because `Docs/07` §6 does not make a client crash on the platform's
    /// future.
    JobRegion? pickup,

    /// Where they are delivered. **Legitimately absent**: a customer may publish before they have
    /// both ends of the trip.
    JobRegion? dropoff,

    @JsonKey(name: 'goods_description') String? goodsDescription,
    @JsonKey(name: 'length_cm') int? lengthCm,
    @JsonKey(name: 'width_cm') int? widthCm,
    @JsonKey(name: 'height_cm') int? heightCm,

    /// Kilograms (`CLAUDE.md` fixes the units, and the contract carries them).
    @JsonKey(name: 'weight_kg') double? weightKg,

    /// What the customer says the job needs, in their own words.
    ///
    /// **Free text, and deliberately not an enum.** `Docs/11` §3 records that it stays free text
    /// until SHIP-79's capability vocabulary arrives, and `VehicleType` is explicitly not that
    /// vocabulary. A client that matched it against a compiled-in list would be inventing an
    /// eligibility rule the platform does not have.
    @JsonKey(name: 'vehicle_requirement') String? vehicleRequirement,

    @JsonKey(name: 'handling_notes') String? handlingNotes,
    @JsonKey(name: 'pickup_window') JobTimeWindow? pickupWindow,
    @JsonKey(name: 'dropoff_window') JobTimeWindow? dropoffWindow,

    /// When the job stops being offered (SHIP-68), so a provider deciding whether to price it
    /// today knows whether it will still be there tomorrow.
    @JsonKey(name: 'expires_at') String? expiresAt,

    @JsonKey(name: 'created_at') String? createdAt,
  }) = _OpenJob;

  const OpenJob._();

  factory OpenJob.fromJson(Map<String, dynamic> json) => _$OpenJobFromJson(json);

  /// Whether the customer has said anything about the size or weight of the goods.
  ///
  /// A job with neither is ordinary — the wizard collects the addresses first — and a heading with
  /// nothing under it is worse than no heading.
  bool get hasMeasurements =>
      weightKg != null || lengthCm != null || widthCm != null || heightCm != null;

  /// The goods as a person reads them — `320 × 175 × 190 cm` — or `null` when no dimension was
  /// stated.
  ///
  /// Partial is legitimate and is shown as far as it goes: a customer may know the length of a
  /// sofa and not its height. Centimetres, because `CLAUDE.md` fixes the units.
  String? get dimensions {
    final parts = <String>[
      if (lengthCm case final length?) '$length',
      if (widthCm case final width?) '$width',
      if (heightCm case final height?) '$height',
    ];
    return parts.isEmpty ? null : '${parts.join(' × ')} cm';
  }

  /// The pickup state, upper case, or `''` when the platform sent no pickup at all.
  ///
  /// Read by the feed's own narrowing (`open_jobs_filter.dart`), which is why it is here rather
  /// than at the call site: "which state is this job picked up in" is a property of the job.
  String get pickupState => pickup?.state ?? '';
}

/// One end of a job, at the grain a provider is given it — the `Region` schema in
/// `contracts/paths/fleet.yaml`.
///
/// **Three parts and no fourth.** There is no street line and no coordinate, and the omission is
/// the decision rather than an oversight: see the note on [OpenJob]. This type must never grow
/// either, and a provider screen that wants an exact address is asking for the shape SHIP-93
/// onwards gives the *awarded* provider, at a different moment.
///
/// Separate from [JobLocation], which is the customer's own view of their own address and carries
/// both. Two types rather than one with a redaction step, for the reason the budget already taught
/// this codebase.
@freezed
abstract class JobRegion with _$JobRegion {
  const factory JobRegion({
    @Default('') String suburb,

    /// The state or territory, as the **upper-case abbreviation** — `NSW`, `VIC`.
    ///
    /// A `String` and deliberately not an enum, for the reason [JobLocation.state] gives: a client
    /// prints it rather than branching on it, and an enum would be a second list of the eight
    /// states to keep in step for no behaviour that depends on it. The feed's own narrowing groups
    /// by the values it was actually sent rather than by a list compiled in here, which is the
    /// same decision seen from the other side.
    @Default('') String state,

    /// Four digits, leading zero kept — `0800` is Darwin, and an `int` would lose it.
    @Default('') String postcode,
  }) = _JobRegion;

  const JobRegion._();

  factory JobRegion.fromJson(Map<String, dynamic> json) => _$JobRegionFromJson(json);

  /// Whether the platform sent anything at all for this end of the job.
  bool get isEmpty => suburb.isEmpty && state.isEmpty && postcode.isEmpty;

  /// The region the way a person says it: `Newtown NSW 2042`.
  String get oneLine => [suburb, state, postcode].where((p) => p.isNotEmpty).join(' ');
}
