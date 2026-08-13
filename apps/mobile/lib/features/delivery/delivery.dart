/// Delivery — milestones, proof capture, driver assignment (`Docs/07` §2).
///
/// The feature that most needs the offline queue in `core/queue`: `Docs/07` §4 has the user
/// record what happened and move on, with syncing left to the platform. Delivered requires
/// photo proof or a recorded exception reason, never neither (`Docs/01` §4.4), so a denied
/// camera permission must reach the exception path rather than a dead end.
///
/// ## What is here (SHIP-129)
///
/// - `milestone.dart` — the four milestones this app can record, in the platform's own wire
///   vocabulary, and why `driver_assigned` is not one of them.
/// - `record_milestone_controller.dart` — recording one against the queue, and deriving what has
///   and has not reached the platform from what the queue still holds.
/// - `delivery_screen.dart` — the buttons and the log, with pending marked in a word, an icon and
///   a sentence rather than in a colour.
///
/// ## What is deliberately not here
///
/// **No transition logic of any kind.** `Docs/02` §3.1 puts every transition on the platform, and
/// this feature holds no copy of `Docs/02` §2's table, does not know what status a job is in, and
/// never decides that one milestone must follow another. Recording is a claim about something that
/// happened; what follows from it is the platform's answer.
///
/// **No proof capture.** SHIP-130 compresses and queues a photograph as a separate operation —
/// `Docs/07` §4 is explicit that an image uploads on reconnection rather than as part of the
/// milestone request — and SHIP-131 is the exception path when the camera is refused. Until both
/// exist, `delivered` is a milestone this app names and does not offer.
///
/// **No reconciliation screen.** A refused operation is quarantined by the worker and shown here
/// only as "needs attention"; SHIP-132 is the screen that says what it lost to and is the only way
/// one ever leaves the queue.
library;
