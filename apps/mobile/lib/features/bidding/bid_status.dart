import 'package:json_annotation/json_annotation.dart';

/// One of the eight bid states in `Docs/02` §4, as `contracts/paths/bidding.yaml` publishes them.
///
/// **Lower snake case on the wire**, exactly as `JobStatus` is, and for the reason `Docs/10` §4.7
/// gives: the stored form may have capitals and spaces, and what a client branches on may not.
///
/// ## This is the Dart copy, and it is temporary by design
///
/// `Docs/10` §8.2 makes `contracts/statuses.yaml` the source for all three languages at SHIP-56a.
/// That file does not exist yet, so `job_status.dart` is the Dart copy of the twelve job states and
/// this is the Dart copy of the eight bid ones. **When SHIP-56a lands it replaces both**, and the
/// wire strings below are the contract it has to reproduce.
///
/// ## `countered` is in the list and nothing writes it
///
/// `Docs/02` §4 names both `Countered` — "an offer answered with a different price or timing" — and
/// `Superseded` — "an offer displaced by a counter from either party". Read carefully those are the
/// same transition described from its two ends, and the platform chose one: `internal/bidding`
/// declares `StatusCountered`, holds it in the closed set `ck_bids_status` is checked against, and
/// carries a comment saying it is deliberately never written. `Docs/11` §9 records the open question
/// and gives it to SHIP-96.
///
/// **It is here for the same reason it is there**: the enum is the published vocabulary, and a value
/// omitted from this list would decode as [unknown] the day somebody starts writing it. It is not
/// here because anything expects it.
enum BidStatus {
  /// Prepared and not yet offered. Nothing this client can obtain — there is no endpoint that
  /// creates one and none that returns one — and in the list because the contract enumerates it.
  @JsonValue('draft')
  draft,

  /// Live, and awaiting the other party. What placing an offer produces, and where revising it
  /// leaves the offer.
  @JsonValue('submitted')
  submitted,

  /// See the note on this enum. The platform writes [superseded] instead.
  @JsonValue('countered')
  countered,

  /// The customer took this offer. Exactly one accepted bid per job, enforced by a partial unique
  /// index rather than by application logic (`CLAUDE.md`).
  @JsonValue('accepted')
  accepted,

  @JsonValue('rejected')
  rejected,

  /// Taken back by the party who made it, before it was accepted.
  @JsonValue('withdrawn')
  withdrawn,

  @JsonValue('expired')
  expired,

  /// Displaced by a counter-offer from the other party (SHIP-87, SHIP-88). It can be neither
  /// changed nor answered afterwards, only read.
  @JsonValue('superseded')
  superseded,

  /// A status this build has never heard of.
  ///
  /// Not one the platform sends: the contract enumerates exactly the eight above. It exists because
  /// the alternative to decoding a ninth is **throwing** on it, and `Docs/07` §6 is built on old
  /// builds living on devices indefinitely. Same reasoning, and the same shape, as
  /// `JobStatus.unknown` and `UserRole.unknown`.
  ///
  /// Deliberately last, so [values] stays in the contract's own order.
  unknown;

  /// What the wire calls this status.
  String get wireName => switch (this) {
        BidStatus.draft => 'draft',
        BidStatus.submitted => 'submitted',
        BidStatus.countered => 'countered',
        BidStatus.accepted => 'accepted',
        BidStatus.rejected => 'rejected',
        BidStatus.withdrawn => 'withdrawn',
        BidStatus.expired => 'expired',
        BidStatus.superseded => 'superseded',
        BidStatus.unknown => 'unknown',
      };

  /// The status as a person reads it — the exact name from `Docs/02` §4 (`CLAUDE.md`), because a
  /// provider reading "Superseded" in the app and support reading it in an audit entry have to be
  /// reading about the same thing.
  String get label => switch (this) {
        BidStatus.draft => 'Draft',
        BidStatus.submitted => 'Submitted',
        BidStatus.countered => 'Countered',
        BidStatus.accepted => 'Accepted',
        BidStatus.rejected => 'Rejected',
        BidStatus.withdrawn => 'Withdrawn',
        BidStatus.expired => 'Expired',
        BidStatus.superseded => 'Superseded',

        // Deliberately not "Unknown", which reads as a fault. The offer is fine; this build is old,
        // and saying which is the difference between an update and a support call.
        BidStatus.unknown => 'Not shown by this version',
      };

  /// Whether this offer is still standing in front of the other party.
  ///
  /// **Presentation only.** What may actually be done to an offer is the platform's decision on
  /// every request (`Docs/07` §3): a `submitted` bid this device is looking at may have been
  /// accepted, countered or expired a second ago, and the app finds out by asking.
  bool get isLive => this == BidStatus.submitted;
}

/// Which party made an offer — `Docs/02` §4's negotiation alternates, and this says whose turn each
/// entry was (SHIP-87).
///
/// **Not derivable from position**, which is why the platform sends it: a provider may join a
/// negotiation halfway, and one job's negotiation may hold more than one chain when an offer was
/// withdrawn and replaced.
enum BidParty {
  @JsonValue('provider')
  provider,

  @JsonValue('customer')
  customer,

  /// A third kind of party this build has never heard of. See [BidStatus.unknown].
  unknown;

  String get wireName => switch (this) {
        BidParty.provider => 'provider',
        BidParty.customer => 'customer',
        BidParty.unknown => 'unknown',
      };
}
