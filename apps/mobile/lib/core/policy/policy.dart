/// The operational numbers this build applies on the device (SHIP-167a).
///
/// `CLAUDE.md`: *"Anything expected to change under operational pressure lives server-side —
/// category lists, validation limits, policy copy, feature switches. Flutter has no over-the-air
/// update path for Dart code."* Two numbers already in this application are exactly that, and
/// neither could obey the rule when it was written:
///
/// - **SHIP-127's four-hour unsynced-nudge threshold** (`core/sync/unsynced_nudge.dart`).
/// - **SHIP-130's proof compression budget** (`core/capture/captured_image.dart`), which
///   SHIP-81c gave a second reader in `features/profile/`.
///
/// Both files say so in their own words, and both give the same reason for compiling the value in
/// anyway: **each fires on a handset that by assumption has no connection.** The nudge is about
/// work that has not reached the platform; the compression happens before an upload the device
/// cannot yet make. That is the premise of the two features rather than an oversight in them, so an
/// endpoint read at the moment of use could never have worked.
///
/// ## What does work, and the one clause worth reading twice
///
/// An endpoint the app reads **while it still has signal** and keeps. `GET /v1/app/policy` is that
/// endpoint, deliberately shaped like `GET /v1/app/minimum-version` (SHIP-167) — unauthenticated,
/// changed by configuration rather than by a release — rather than inventing a second convention.
///
/// The rule this folder implements, in the backlog's own words: *"the app caches the last response
/// and applies it with no connection, **falling back to a compiled default only when it has never
/// had one**."* Three cases and they are not two:
///
/// | Situation | What applies |
/// |---|---|
/// | The platform answered | What it said, and it is written to the cache |
/// | No connection, and this device has been online before | **The cached answer** |
/// | No connection, and this device has *never* been online | The compiled default |
///
/// **A build that fell back to the compiled default whenever it was offline would be wrong**, and
/// would look right in review: the offline path would pass a test, the numbers would be plausible,
/// and the endpoint would silently have no effect on the devices it exists for — because the
/// devices it exists for are the ones that are offline when the value is used. [resolveAppPolicy]
/// is the whole of that decision, in one expression, so it can be read and mutated as one.
///
/// ## What is here
///
/// - `app_policy.dart` — the model, the compiled default, and the repository over the client that
///   carries no credential.
/// - `app_policy_cache.dart` — where the last answer is kept, and why it is a file rather than a
///   row in the queue's database.
/// - `app_policy_controller.dart` — [resolveAppPolicy], the providers, and the seam that keeps
///   every widget test from making a request.
library;

export 'package:shipper/core/policy/app_policy.dart';
export 'package:shipper/core/policy/app_policy_cache.dart';
export 'package:shipper/core/policy/app_policy_controller.dart';
