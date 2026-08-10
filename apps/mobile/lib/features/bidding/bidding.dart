/// Bidding — bids, counter-offers, negotiation, award (`Docs/07` §2).
///
/// Deliberately **not** offline-capable. `Docs/07` §4 excludes bidding, awarding and
/// negotiation from the durable queue: they are competitive, time-sensitive and multi-party,
/// and a stale local decision is worse than an honest "you are offline".
///
/// Empty until M3.
library;
