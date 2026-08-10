/// The durable local operation queue (`Docs/07` §4, SHIP-124).
///
/// **Structure only. There is no queue here yet**, and SHIP-17 deliberately did not build
/// one — this folder exists so that when SHIP-124 arrives it has an obvious home and no
/// argument about where.
///
/// The decision that shapes it is already made: **Drift over SQLite**, in `Docs/10` §8.3 and
/// `Docs/07` §9. The reason is transactional rather than about storage. An operation, its
/// client-generated idempotency key, the time the user acted, and the local path to its proof
/// image either all commit or none of them do, and the sync worker has to mark an item in
/// flight and recover cleanly when the process dies mid-upload. A key-value store cannot
/// express that, and the first time it half-writes an entry the client has quietly dropped
/// precisely what `Docs/07` §4 promises it will not.
///
/// The Drift dependency is not in `pubspec.yaml` yet. It pulls a native SQLite library into
/// both platform builds, and adding that ahead of the code that uses it buys nothing —
/// `flutter analyze` cannot tell an unused dependency from a missing one. It arrives with
/// SHIP-124.
library;
