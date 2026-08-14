/// Why a recorded milestone carries no photograph — `Docs/01` §4.4's three (SHIP-56a).
///
/// The enumeration is generated from `contracts/statuses.yaml` by `make codegen`;
/// `proof_exception_reason.gen.dart` beside this file is the Dart form, and the platform's is
/// `internal/delivery/proofexception_gen.go`.
///
/// SHIP-56a generated the vocabulary before anything needed it, so that the first screen to want
/// these three words got a list already agreeing with the service, the driver portal and
/// `ck_proofs_exception_reason` — rather than a fourth hand-written copy, which is what every one of
/// these lists started as. **Two screens now read it**: SHIP-131's exception path when a camera
/// cannot be opened, and SHIP-133's customer tracking view, which shows the reason back.
///
/// The file exists for the reason `job_status.dart` does: `<name>.gen.dart` is written by the
/// generator and imported by nothing, `<name>.dart` is written by a person and imported by
/// everything, so behaviour arriving later needs no import site to change. [ProofExceptionReasonCopy]
/// below is that behaviour, and it is why the generated file could not have owned this name.
library;

import 'package:shipper/features/delivery/proof_exception_reason.gen.dart';

export 'package:shipper/features/delivery/proof_exception_reason.gen.dart';

/// The words two different people read about one recorded reason (SHIP-131, SHIP-133).
///
/// ## Why this is hand-written beside a generated enumeration rather than in it
///
/// `ProofExceptionReason.label` is the **wire form** — `recipient_objected` — and that is right for
/// what it is. `contracts/statuses.yaml` renders a label from the document's own name for a value,
/// and `Docs/01` §4.4 names these three in snake case because they were written as a closed list for
/// a database column rather than as words for a screen. A generator cannot invent the sentence a
/// driver in the rain reads at a delivery point, and a label written into the specification would be
/// copy for three surfaces in three languages maintained in a file nobody who writes copy opens.
///
/// So the vocabulary is generated and the copy is not, which is the split `bid_status.dart` already
/// makes for `BidStatusLiveness`.
///
/// ## Two audiences, and they are not told the same thing
///
/// [driverPrompt] is what the **driver** chooses from, in the second person, at the moment they are
/// standing somewhere they cannot photograph. [customerExplanation] is what the **customer** reads
/// afterwards, in the third person and in the past tense, about a delivery that has already
/// happened.
///
/// One string for both was the obvious saving and is wrong in both directions: "The recipient does
/// not want to be photographed" is a question to a driver and a report to a customer, and a sentence
/// that serves as both serves as neither. `Docs/01` §4.4's exception path exists so that a driver is
/// never stranded; the customer's half exists so that "no photograph" does not read as "nothing was
/// recorded", which is a dispute raised over a delivery that went perfectly well.
extension ProofExceptionReasonCopy on ProofExceptionReason {
  /// What the driver taps. Australian English, second person, and short enough to read at a door.
  String get driverPrompt => switch (this) {
        ProofExceptionReason.recipientObjected => 'The recipient does not want a photograph taken',
        ProofExceptionReason.cameraUnavailable => 'The camera will not open on this phone',
        ProofExceptionReason.locationUnsafe => 'It is not safe or light enough to photograph here',

        // Never offered — see [offered]. It is here because the switch is exhaustive over the
        // enumeration, and a value added to `contracts/statuses.yaml` should fail to compile here
        // rather than reach a driver as a blank row.
        ProofExceptionReason.unknown => 'Not shown by this version',
      };

  /// What the customer reads. Third person, past tense, and it says a record was made.
  String get customerExplanation => switch (this) {
        ProofExceptionReason.recipientObjected =>
          'The person receiving the goods asked not to be photographed, so the driver recorded that '
              'instead.',
        ProofExceptionReason.cameraUnavailable =>
          'The driver’s camera could not be used, so they recorded that instead.',
        ProofExceptionReason.locationUnsafe =>
          'The delivery point was unlit or unsafe to photograph, so the driver recorded that '
              'instead.',
        ProofExceptionReason.unknown =>
          'Shipper recorded a reason this version of the app cannot name. Update the app to see it.',
      };

  /// The reasons a driver may choose from — `Docs/01` §4.4's three, and nothing else.
  ///
  /// **[ProofExceptionReason.unknown] is excluded, and it is the only exclusion.** It is this
  /// client's own value for a reason a later build wrote, not one the platform accepts: the
  /// contract's `enum` is exactly three, so sending it is a `validation_failed` naming
  /// `proof.exception_reason` — a driver told their reason is not a reason.
  ///
  /// **Derived rather than listed a second time**, so a fourth reason added to
  /// `contracts/statuses.yaml` is offered by `make codegen` and nothing else.
  ///
  /// There is deliberately no free-text option. `Docs/01` §4.4 wrote the list closed and `Docs/04`
  /// §5 gives the reason: a reason nobody can group is a moderation queue nobody can triage. A
  /// driver's own words are one field away — the milestone's `reason` is optional, 500 characters,
  /// and goes *beside* a selection rather than instead of one.
  static List<ProofExceptionReason> get offered => ProofExceptionReason.values
      .where((reason) => reason != ProofExceptionReason.unknown)
      .toList(growable: false);
}
