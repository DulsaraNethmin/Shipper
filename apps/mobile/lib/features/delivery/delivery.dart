/// Delivery — milestones, proof capture, driver assignment (`Docs/07` §2).
///
/// The feature that most needs the offline queue in `core/queue`: `Docs/07` §4 has the user
/// record what happened and move on, with syncing left to the platform. Delivered requires
/// photo proof or a recorded exception reason, never neither (`Docs/01` §4.4), so a denied
/// camera permission must reach the exception path rather than a dead end.
///
/// ## Two halves of one delivery, and they are not the same screen
///
/// This feature now holds **both parties' views of the same job**, and the difference between them
/// is the difference between a claim and a record:
///
/// | | Who | What it shows | Where it comes from |
/// |---|---|---|---|
/// | `delivery_screen.dart` | the awarded **provider** | what this handset has recorded, including what has **not** been sent | SHIP-124's durable queue, on the device |
/// | `tracking_screen.dart` | the **customer** | what the platform holds, and nothing else | `GET /v1/jobs/{id}/delivery/…` |
///
/// **A customer must never be shown the first.** A milestone recorded in a valley an hour ago and
/// still sitting in a queue is not a fact about their delivery, and the whole of `Docs/02` §3.1's
/// pending-state machinery exists so that the person who recorded it knows the difference. The
/// customer's side needs no such marking because everything it can see is confirmed by construction.
///
/// ## What is here (SHIP-129, SHIP-130, SHIP-133)
///
/// - `milestone.dart` — the four milestones this app can record, in the platform's own wire
///   vocabulary, why `driver_assigned` is not one of them, and `milestoneLabel` for naming the five
///   that can be *read*.
/// - `record_milestone_controller.dart` — recording one against the queue, and deriving what has
///   and has not reached the platform from what the queue still holds.
/// - `delivery_screen.dart` — the buttons and the log, with pending marked in a word, an icon and
///   a sentence rather than in a colour.
/// - `capture_proof_controller.dart`, `proof_capture_screen.dart` — the photograph, taken by this
///   app's own camera, compressed on the device, and queued as a file. **The camera, the compressor
///   and the store are no longer here**: SHIP-81c gave them a second caller in `features/profile/`,
///   and `Docs/07` §2 answers a second caller with a move rather than a cross-feature import, so
///   they are `core/capture/` and this feature names the `proof/` folder it writes into.
/// - `delivery_tracking.dart` — the three read shapes: a recorded milestone, a piece of evidence,
///   and who is driving.
/// - `delivery_repository.dart` — the read shelf, and why every path on it has five segments.
/// - `tracking_controller.dart`, `tracking_screen.dart` — the customer's view, and why nothing it
///   reads is ever cached.
///
/// ## What is deliberately not here
///
/// **No transition logic of any kind.** `Docs/02` §3.1 puts every transition on the platform, and
/// this feature holds no copy of `Docs/02` §2's table, does not know what status a job is in, and
/// never decides that one milestone must follow another. Recording is a claim about something that
/// happened; what follows from it is the platform's answer.
///
/// **No recipient name and no delivery note.** `Docs/01` §4.4 requires a delivered job to carry both
/// alongside its proof and `Docs/02` §3 repeats it, and **no column holds either** — `Docs/11` §4
/// carries SHIP-118 as partly done for exactly this. The customer's tracking view does not model
/// them, draw them, or leave a space where they would go, because a screen implying a field the
/// platform cannot supply is worse than one that is honestly short of it.
///
/// **No reconciliation screen.** A refused operation is quarantined by the worker and shown on the
/// provider's side only as "needs attention"; SHIP-132 is the screen that says what it lost to and
/// is the only way one ever leaves the queue.
library;
