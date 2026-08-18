/// `Docs/04` §3's four collected documents, and what the platform says about one it holds
/// (SHIP-81c).
///
/// **Hand-written rather than generated**, following `milestone.dart` and `proof_exception_reason`:
/// the set is four values that `Docs/04` §3 fixed, the wire spelling is
/// `contracts/paths/profiles.yaml`'s `DocumentKind` enum, and what a provider reads on the screen is
/// copy that belongs in the layer that draws it.
library;

/// One of the four kinds of evidence `Docs/04` §3 collects from a provider.
///
/// **Closed, and in the platform's own order.** `contracts/paths/profiles.yaml` enumerates
/// `[licence, registration, insurance, abn_evidence]` and `422` names `kind` for anything else, so
/// a fifth value here would be a screen offering something the platform refuses.
enum VerificationDocumentKind {
  /// The provider's driver licence.
  licence('licence'),

  /// The vehicle's registration certificate.
  registration('registration'),

  /// The insurance certificate.
  insurance('insurance'),

  /// Evidence of the ABN — a registration extract, or a tax invoice showing it.
  abnEvidence('abn_evidence');

  const VerificationDocumentKind(this.wire);

  /// What goes in `kind` on `POST /v1/provider/verification/documents`.
  ///
  /// **Australian spelling, and it is the wire's rather than this client's preference**:
  /// `licence` the noun, which the contract states in as many words and `scripts/check-spelling.sh`
  /// holds this repository to independently (`Docs/10` §9.3).
  final String wire;

  /// The kind [wire] names, or `null` if this build has never heard of it.
  ///
  /// `null` rather than a throw or a default, for the reason `OperationKind.byName` gives: a value
  /// this build does not recognise is a platform that has moved ahead of this handset, and a screen
  /// that silently relabelled somebody's insurance certificate as a licence would be worse than one
  /// that quietly declines to draw a row it cannot name.
  static VerificationDocumentKind? fromWire(String wire) {
    for (final kind in VerificationDocumentKind.values) {
      if (kind.wire == wire) return kind;
    }
    return null;
  }
}

/// What a provider is asked for, per kind.
///
/// Copy, so it lives beside the screen that draws it rather than on the enum — the same split
/// `ProofExceptionReasonCopy` makes, and for the same reason: the wire vocabulary is a contract and
/// the words are not.
extension VerificationDocumentKindCopy on VerificationDocumentKind {
  /// The heading a provider reads.
  String get label => switch (this) {
        VerificationDocumentKind.licence => 'Driver licence',
        VerificationDocumentKind.registration => 'Vehicle registration',
        VerificationDocumentKind.insurance => 'Insurance certificate',
        VerificationDocumentKind.abnEvidence => 'ABN evidence',
      };

  /// One sentence saying what a legible photograph of it looks like.
  ///
  /// `Docs/04` §3 has an administrator reviewing each image by eye for *"correct document type,
  /// legible, not visibly expired, name matching the account"*. Every re-photograph is a round trip
  /// through a person, so saying what will be looked at is the cheapest thing this screen can do.
  String get guidance => switch (this) {
        VerificationDocumentKind.licence =>
          'Photograph the front of your current driver licence. Your name and the expiry date '
              'both need to be readable.',
        VerificationDocumentKind.registration =>
          'Photograph the registration certificate for the vehicle you will be driving.',
        VerificationDocumentKind.insurance =>
          'Photograph your current insurance certificate. The cover and the dates both need to be '
              'readable.',
        VerificationDocumentKind.abnEvidence =>
          'Photograph an ABN registration extract, or a tax invoice showing your ABN.',
      };
}

/// One document the platform holds, as `GET /v1/provider/verification/documents` returns it.
///
/// **Hand-written `fromJson` rather than `json_serializable`**, following `Vehicle` and `Job` in
/// having a generated pair — except here there is nothing to generate a *writer* for: this client
/// never sends a `Document`, it sends a `DocumentSubmission`, which is two fields and a method
/// argument rather than a type.
///
/// ## What is deliberately not modelled
///
/// **`download_url` and `download_expires_at`.** They are credentials with a lifetime of minutes,
/// and the contract says *"do not cache them, do not log them, and re-read the list rather than
/// holding one"*. A field on a model held by a controller is a cache, so there is no field. Nothing
/// on this branch renders a submitted image back to the provider — the administrator's viewer is
/// SHIP-155's, on the administrator's credential — and the screen that would is the one that should
/// re-read the list and use the URL immediately.
///
/// **`expires_at`.** See `VerificationRepository.submit` for why nothing here collects one.
final class VerificationDocument {
  const VerificationDocument({
    required this.id,
    required this.kind,
    required this.submittedAt,
  });

  /// The submission's identifier.
  final String id;

  /// Which of the four this is, or `null` when the platform named a kind this build does not have.
  final VerificationDocumentKind? kind;

  /// When the platform recorded the submission, in UTC.
  final DateTime submittedAt;

  static VerificationDocument fromJson(Map<String, dynamic> json) {
    return VerificationDocument(
      id: json['id'] as String,
      kind: VerificationDocumentKind.fromWire(json['kind'] as String? ?? ''),
      // `toLocal()` is deliberately not applied. Nothing on this screen renders the instant; what
      // it is read for is "is there one of these yet", and a date rendered in the wrong zone is a
      // trap `Docs/11` §3 has recorded firing twice. Whoever draws it uses `shared/formatting`.
      submittedAt: DateTime.parse(json['submitted_at'] as String),
    );
  }
}
