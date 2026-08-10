/// Delivery — milestones, proof capture, driver assignment (`Docs/07` §2).
///
/// The feature that most needs the offline queue in `core/queue`: `Docs/07` §4 has the user
/// record what happened and move on, with syncing left to the platform. Delivered requires
/// photo proof or a recorded exception reason, never neither (`Docs/01` §4.4), so a denied
/// camera permission must reach the exception path rather than a dead end.
///
/// Empty until M4.
library;
