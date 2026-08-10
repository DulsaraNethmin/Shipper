/// Networking (`Docs/07` §2).
///
/// One client, on `dio` (`Docs/10` §8.3), reaching the versioned public API directly — there
/// is no BFF tier (`Docs/06` §2.1). Product endpoints sit under `/v1`; operational ones such
/// as `/health` do not (SHIP-13).
///
/// Two rules shape everything in this folder:
///
/// - **Unknown response fields are tolerated** (`Docs/07` §6). Old builds live on devices
///   indefinitely, so the server adds fields rather than repurposing them, and a client that
///   rejects what it does not recognise turns every additive change into a release.
/// - **Every state-changing request carries an `Idempotency-Key`** and is refused without one
///   (SHIP-15, `CLAUDE.md`). Mobile clients retry after dropped connections; retries must not
///   duplicate bids, milestones or proof.
///
/// The client is structured so `Docs/10` §8.1's generated client can replace the transport at
/// SHIP-17a without the rest of the app noticing.
library;
