/// The sync worker: what gets SHIP-124's queue off the handset (`Docs/07` §4, SHIP-125).
///
/// The queue's promise is that nothing the user recorded is lost. This folder's promise is the
/// other half of `Docs/07` §4's sentence — **syncing is the platform's problem** — which somebody
/// has to actually keep.
///
/// ## What is here
///
/// - `backoff.dart` — how long a failed operation waits. Base, ceiling, jitter, and what resets
///   it, with the reasoning for each number.
/// - `sync_decision.dart` — what one attempt's answer means. The retry-versus-refuse table, and
///   the three answers that are wrong in an interesting way.
/// - `operation_sender.dart` — the one method the worker needs of the network, declared by the
///   worker and implemented over `ApiClient`.
/// - `sync_signals.dart` — the triggers, and why a connectivity package is a latency
///   optimisation rather than the mechanism.
/// - `sync_alarm.dart` — the single pending wake-up.
/// - `sync_worker.dart` — the worker, and the application wiring.
///
/// ## The three clauses of the *Done when*, and where each is
///
/// **"Drains on reconnection."** `SyncWorker`'s six triggers, of which the self-scheduled one is
/// the only one guaranteed to arrive — a handset regains a route with no event at all often
/// enough that a worker depending on one stalls forever. `sync_signals.dart` argues it.
///
/// **"With exponential backoff."** `Backoff`, stored per operation through SHIP-124's
/// `release(id, nextAttemptAt:)`, which is what makes it survive a relaunch. The ceiling is the
/// number that matters and `backoff.dart` says why.
///
/// **"And per-item idempotency keys."** Nothing here mints one. The key was minted where the user
/// acted, stored in the row by SHIP-124, and put on the wire unchanged by `OperationSender` on
/// every attempt — including the attempt after a relaunch, and including the replay
/// `AuthInterceptor` makes after refreshing a token. A key regenerated per attempt would make each
/// retry a new operation and duplicate the milestone the platform had already recorded, which is
/// the exact bug idempotency exists to prevent.
///
/// ## What is deliberately not here
///
/// The pending indicator (SHIP-126), the four-hour nudge (SHIP-127), the milestone screen
/// (SHIP-129) and the reconciliation UI (SHIP-132) are separate tickets. What they consume is
/// here: `SyncWorker.snapshots` publishes a `QueueSnapshot` after every pass, `SyncWorker.record`
/// is enqueue-and-send in one call, and a refused operation is `blocked` with its reason and its
/// request id recorded.
library;
