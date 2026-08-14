/// The eight bid states of `Docs/02` §4, and the two things that belong beside them (SHIP-56a).
///
/// The enumeration itself is generated. `contracts/statuses.yaml` is the source for all three
/// languages, `make codegen` produces them, and `bid_status.gen.dart` beside this file is the Dart
/// one — including the note explaining why nothing writes `countered`, which used to be written out
/// here *and* in `internal/bidding/model.go` in almost the same words. It is now one paragraph in
/// the specification, rendered into Go, Dart and TypeScript from there.
///
/// **This file is why the generated one is `.gen.dart`.** [BidStatusLiveness] and [BidParty] are
/// hand-written, and a generator that owned `bid_status.dart` outright would have deleted both the
/// next time it ran. Every consumer keeps importing the name it always imported.
library;

import 'package:json_annotation/json_annotation.dart';
import 'package:shipper/features/bidding/bid_status.gen.dart';

export 'package:shipper/features/bidding/bid_status.gen.dart';

/// Whether an offer is still standing in front of the other party.
///
/// **Presentation only, which is why it is not generated.** What may actually be done to an offer
/// is the platform's decision on every request (`Docs/07` §3): a `submitted` bid this device is
/// looking at may have been accepted, countered or expired a second ago, and the app finds out by
/// asking. The service has its own predicate, spelled `live()` and private to `internal/bidding`,
/// and the two being separate is the point rather than duplication — generating one from the other
/// would put a decision on the device.
///
/// One status, named rather than guessed at. A counter-offer is a *new row* at `submitted` and the
/// offer it displaces moves to `superseded` in the same transaction, so one status still covers
/// "the live offer in this negotiation".
extension BidStatusLiveness on BidStatus {
  bool get isLive => this == BidStatus.submitted;
}

/// Which party made an offer — `Docs/02` §4's negotiation alternates, and this says whose turn each
/// entry was (SHIP-87).
///
/// **Not derivable from position**, which is why the platform sends it: a provider may join a
/// negotiation halfway, and one job's negotiation may hold more than one chain when an offer was
/// withdrawn and replaced.
///
/// **Hand-written, because it is not a status.** It names a kind of person rather than a lifecycle
/// state, which is the line `contracts/statuses.yaml` draws for what it generates —
/// `jobs.ActorType` and `bidding.Party` are on the same side of it. Moving all three later is an
/// entry in that file and no new mechanism.
enum BidParty {
  @JsonValue('provider')
  provider,

  @JsonValue('customer')
  customer,

  /// A third kind of party this build has never heard of. See `BidStatus.unknown`.
  unknown;

  String get wireName => switch (this) {
        BidParty.provider => 'provider',
        BidParty.customer => 'customer',
        BidParty.unknown => 'unknown',
      };
}
