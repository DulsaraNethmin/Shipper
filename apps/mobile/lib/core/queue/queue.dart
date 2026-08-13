/// The durable local operation queue (`Docs/07` §4, SHIP-124).
///
/// `Docs/07` §4 calls this the single most important client capability and the one most often
/// underestimated. Its principle is one sentence — **the user records what happened and moves
/// on; syncing is the platform's problem** — and its hard requirement is one more: an operation
/// is **never silently dropped**.
///
/// ## What is here
///
/// - `queued_operation.dart` — what a queued operation is, the kinds there are, and the two
///   shapes a stored row can be read back as.
/// - `queue_database.dart` — the Drift table and database. One table, `queued_operations`.
/// - `operation_queue.dart` — the queue itself: enqueue, the snapshot a counter and a
///   reconciliation screen read, and the claim/release/complete seam the sync worker drains
///   through.
///
/// ## What is deliberately not here
///
/// **The sync worker is SHIP-125, in `core/sync`.** Nothing in this folder decides when to send,
/// how long to wait after a failure, or what a particular platform refusal means. What it does is
/// make those decisions safe to take: every attempt reuses the idempotency key minted when the
/// user acted, a claim that never came back is recovered rather than stranded, and an item nothing
/// can be done with is quarantined where it stays visible instead of vanishing.
///
/// ## Storage: Drift over SQLite
///
/// Decided before this ticket, in `Docs/10` §8.3 and `Docs/07` §9, and the reason is
/// transactional rather than about storage. An operation, its idempotency key, the time the user
/// acted and the local path to its proof image either all commit or none of them do; and the
/// worker has to mark an item in flight and recover cleanly when the process dies mid-upload. A
/// key-value store cannot express either, and the first time it half-writes an entry the client
/// has dropped exactly what `Docs/07` §4 promises it will not.
///
/// ## Sign-out
///
/// `Docs/07` §3 clears the queue at sign-out. [OperationQueue.clear] is that call and reports how
/// many operations it discarded, so a bulk removal is still something a person can be told about.
/// SHIP-125 wires it: `syncWorkerProvider` watches the session and clears the queue when it
/// becomes signed out, rather than `SessionController.signOut` calling in here. See `Docs/11` §3,
/// SHIP-125.
library;
