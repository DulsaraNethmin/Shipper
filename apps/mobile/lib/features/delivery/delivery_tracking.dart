import 'package:freezed_annotation/freezed_annotation.dart';

import 'package:shipper/features/delivery/milestone.dart';
import 'package:shipper/features/delivery/proof_exception_reason.dart';

part 'delivery_tracking.freezed.dart';
part 'delivery_tracking.g.dart';

/// What kind of actor recorded a milestone — the `recorded_by` enumeration in
/// `contracts/paths/delivery.yaml` (SHIP-115a).
///
/// **A kind of person, not who they are**, and the platform is explicit that it is only the kind:
/// there is no identifier here and none is served. That is the right amount for a customer, who
/// needs to know whether their delivery moved because a driver said so or because the platform
/// worked it out, and has no business with an account id either way.
///
/// **Hand-written rather than generated**, which is the line `contracts/statuses.yaml` draws: it
/// generates lifecycle *states*, and `jobs.ActorType`, `bidding.Party` and this are all on the other
/// side of it. `BidParty` is the same decision made at SHIP-87.
enum MilestoneActor {
  /// The transport provider, through the app. Everything recorded by SHIP-111's endpoint.
  @JsonValue('provider')
  provider,

  /// The assigned driver, through their job-scoped link (SHIP-120a).
  @JsonValue('driver')
  driver,

  /// An administrator, resolving something by hand (`Docs/04`).
  @JsonValue('admin')
  admin,

  /// The platform itself — a presentation change nobody performed.
  @JsonValue('system')
  system,

  /// A kind of actor this build has never heard of.
  ///
  /// Not one the platform sends: the contract enumerates exactly the four above. It exists because
  /// the alternative to decoding an unrecognised one is throwing on it, and `Docs/07` §6 is built on
  /// old builds living on devices indefinitely.
  unknown;

  /// What a customer is told about who recorded this, or `null` when there is nothing worth saying.
  ///
  /// [provider] is `null` deliberately: it is the ordinary case and by far the commonest, and
  /// "Recorded by the transport provider" under every row on the screen is noise that makes the two
  /// rows which are *not* ordinary harder to see.
  String? get attribution => switch (this) {
        MilestoneActor.provider => null,
        MilestoneActor.driver => 'Recorded by the driver',
        MilestoneActor.admin => 'Recorded by Shipper support',
        // Docs/02 §1 calls the presentation statuses the platform's own reading of a condition
        // rather than an act anybody performed, and a customer seeing a step nobody took is owed
        // that sentence rather than left to wonder who did it.
        MilestoneActor.system => 'Recorded by Shipper',
        MilestoneActor.unknown => null,
      };
}

/// One milestone the platform has recorded on a delivery — the `Milestone` schema in
/// `contracts/paths/delivery.yaml` (SHIP-115a).
///
/// ## Everything on this list is confirmed, and that is what makes SHIP-133's wording meaningful
///
/// The *Done when* is "the latest **confirmed** milestone", and the confirmation is structural
/// rather than a field to check: this is the platform's record. `accepted_at` is when the platform
/// received it, it is `required` in the contract, and a row cannot be here without one.
///
/// The contrast is `RecordMilestoneController`, which is the **provider's** side of the same
/// delivery and shows exactly what has *not* been confirmed — work sitting in SHIP-124's durable
/// queue on one handset, marked pending in a word. A customer never sees that and must not: a
/// milestone recorded in a valley an hour ago is not something to tell the person waiting for their
/// goods about.
///
/// ## The job's status is not here, and reading one out of this would be wrong
///
/// The contract says so in as many words. A milestone may deliberately move nothing — a second
/// `en_route_to_pickup` after a failed pickup attempt is kept and moves the job nowhere (SHIP-112),
/// and this endpoint is **the only place such a row can be seen at all**, because it writes no
/// `job_status_history` row and so appears in nothing derived from the status.
///
/// ## Two clocks, and the customer is shown the actor's
///
/// [recordedAt] is when the actor says they acted and [acceptedAt] is when the platform received it,
/// and they differ by however long the device was out of signal. `Docs/02` §3.1 keeps them distinct;
/// the customer is shown the first, because that is when the thing happened. [acceptedAt] is modelled
/// and deliberately not drawn — it is what support and audit reason about.
@freezed
abstract class RecordedMilestone with _$RecordedMilestone {
  const factory RecordedMilestone({
    required String id,

    @JsonKey(name: 'job_id') required String jobId,

    /// The milestone, in the platform's wire vocabulary — one of `Docs/01` §4.4's five.
    ///
    /// **A `String` rather than the `Milestone` enumeration**, which is four values and is *the
    /// milestones this app can record*. `driver_assigned` is served here and is not one of them, and
    /// a sixth could arrive without a store release. `milestoneLabel` is what names it.
    required String milestone,

    /// What kind of actor recorded it.
    @JsonKey(name: 'recorded_by', unknownEnumValue: MilestoneActor.unknown)
    MilestoneActor? recordedBy,

    /// What a person should know that the milestone itself does not say — "nobody at the gate,
    /// returning at four". Present only when one was given.
    String? reason,

    /// When the actor says they acted, in UTC. **This is what a customer is shown.**
    @JsonKey(name: 'recorded_at') String? recordedAt,

    /// When the platform received it, in UTC, on the platform's own clock.
    @JsonKey(name: 'accepted_at') String? acceptedAt,
  }) = _RecordedMilestone;

  const RecordedMilestone._();

  factory RecordedMilestone.fromJson(Map<String, dynamic> json) =>
      _$RecordedMilestoneFromJson(json);

  /// What the screen calls this milestone. See `milestone.dart` for why the four are derived and
  /// the fifth is named.
  String get label => milestoneLabel(milestone);
}

/// The evidence for one recorded milestone — the `Proof` schema in `contracts/paths/delivery.yaml`
/// (SHIP-115, SHIP-116).
///
/// ## Two shapes, and [exceptionReason] is how you tell them apart
///
/// A photograph carries [downloadUrl] and [downloadExpiresAt]. A reasoned exception carries neither
/// — there is no object, so there is nothing to sign a URL for — and carries [exceptionReason]
/// instead.
///
/// **Branch on [exceptionReason], never on a missing [downloadUrl].** Both work today and only the
/// first is right: the second reads the absence of a *credential* as a fact about the delivery, and
/// a URL can be absent because it expired, because the store was unreachable, or because a later
/// build stopped sending one.
///
/// ## [downloadUrl] is a credential, and this type is why it is not stored
///
/// There is no public path to a proof photograph anywhere in this platform — it identifies an
/// address and a recipient. What arrives is a URL signed for this caller *after* the request checked
/// who they are, and **anybody holding it can fetch the image until [downloadExpiresAt]; nothing can
/// revoke one**. So it is rendered and never persisted: nothing in this client writes a proof
/// response to disk, and `TrackingController` holds one in memory for the life of a screen and
/// re-reads rather than caching. The URLs are minted per request, so asking again is the supported
/// way to get fresh ones.
///
/// ## What is not modelled
///
/// `object_key`, `content_type` and `content_length`. All three are served, all three are for
/// support and for a client reconciling a list, and no screen shows them. `Docs/07` §6 ignores
/// unknown fields, so not modelling them costs nothing and keeps the shape to what is drawn.
@freezed
abstract class DeliveryProof with _$DeliveryProof {
  const factory DeliveryProof({
    required String id,

    @JsonKey(name: 'job_id') required String jobId,

    /// The milestone this evidence is for. On this job by construction.
    @JsonKey(name: 'milestone_id') String? milestoneId,

    /// Which milestone was recorded, in the wire vocabulary.
    required String milestone,

    /// A signed URL at the object store. **A credential** — see the note on this class.
    @JsonKey(name: 'download_url') String? downloadUrl,

    /// When [downloadUrl] stops working.
    ///
    /// **Read rather than assumed**: it is configuration and may be shortened without notice, which
    /// is why nothing in this client compiles in a lifetime.
    @JsonKey(name: 'download_expires_at') String? downloadExpiresAt,

    /// Why this milestone has no photograph, and absent when it has one.
    @JsonKey(name: 'exception_reason', unknownEnumValue: ProofExceptionReason.unknown)
    ProofExceptionReason? exceptionReason,

    /// The **actor's** clock, carried through from the milestone. What to show a customer.
    @JsonKey(name: 'recorded_at') String? recordedAt,

    /// When the platform recorded it. What support reasons about.
    @JsonKey(name: 'accepted_at') String? acceptedAt,
  }) = _DeliveryProof;

  const DeliveryProof._();

  factory DeliveryProof.fromJson(Map<String, dynamic> json) => _$DeliveryProofFromJson(json);

  /// Whether this record is a reasoned exception rather than a photograph.
  ///
  /// `Docs/01` §4.4 makes the two the same feature: delivered requires photo proof **or** a recorded
  /// exception reason, and never neither. An exception is evidence rather than the absence of it.
  bool get isException => exceptionReason != null;

  /// What the screen calls the milestone this is evidence for.
  String get label => milestoneLabel(milestone);
}

/// Who is carrying a delivery — the `DeliveryDetailView` schema in `contracts/paths/delivery.yaml`
/// (SHIP-115a).
///
/// ## A job with no driver yet is a `200`, not a `404`
///
/// [driverAssigned] is `false` and the other fields are absent. An awarded job nobody has been put
/// on is an ordinary state and both parties are entitled to see it, and **a draft job is the same
/// answer**: `Service.partyTo` makes the job's owner a party whatever its status, so a customer
/// reading this on a job they have not published yet gets an empty assignment rather than a refusal.
/// Branch on [driverAssigned], never on a missing [driverName].
///
/// ## `driver_mobile` is deliberately not modelled
///
/// The platform sends it **to the provider and not to the customer**, and blanks it in the service
/// rather than leaving the handler to omit it — "a handler that never receives a number cannot
/// render one" (SHIP-115a). A driver has no account and no way to consent, and `Docs/01` §4 asks the
/// platform to minimise how far a phone number travels.
///
/// This type is read by a **customer's** screen, so the field is one that can never arrive. Modelling
/// it would put a null on a customer surface that a later screen could draw the day something else
/// started populating it — which is the "one shape with a flag" arrangement the platform declined
/// twice, once for the budget and once for this. The provider's own view of a driver, when it is
/// built, is a second type.
@freezed
abstract class DeliveryDriver with _$DeliveryDriver {
  const factory DeliveryDriver({
    @JsonKey(name: 'job_id') required String jobId,

    /// Whether anybody is driving the job yet. When `false`, nothing else is present, and that is a
    /// complete answer rather than a missing one.
    @JsonKey(name: 'driver_assigned') required bool driverAssigned,

    /// The assignment record. A driver has no account, so this is their identity for the length of
    /// one job — it is what their milestones are attributed to.
    @JsonKey(name: 'assignment_id') String? assignmentId,

    /// The name the transport provider wrote down.
    @JsonKey(name: 'driver_name') String? driverName,

    /// When the driver was put on the job, on the platform's clock, in UTC.
    @JsonKey(name: 'assigned_at') String? assignedAt,
  }) = _DeliveryDriver;

  const DeliveryDriver._();

  factory DeliveryDriver.fromJson(Map<String, dynamic> json) => _$DeliveryDriverFromJson(json);
}
