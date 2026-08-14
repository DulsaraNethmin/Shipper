/// The twelve job states of `Docs/02` §1 (SHIP-56a).
///
/// The enumeration itself is generated. `contracts/statuses.yaml` is the source for all three
/// languages, `make codegen` produces them, and `job_status.gen.dart` beside this file is the Dart
/// one — `Docs/10` §8.2 named that arrangement long before there was a generator, and the comment
/// this file used to carry promised it.
///
/// ## Why this file still exists rather than being the generated one
///
/// **So there is somewhere for behaviour to go.** A generator that owned the file every screen
/// imports would delete anything hand-written beside the enumeration the next time it ran, which is
/// exactly what happened to `bid_status.dart`'s `isLive` and `BidParty` in the first draft of
/// SHIP-56a. The convention is now uniform across the client: `<name>.gen.dart` is written by the
/// generator and imported by nothing, `<name>.dart` is written by a person and imported by
/// everything. Nothing that already imported this file had to change.
///
/// There is no behaviour here today, and that is the honest state of it — the app reads a status,
/// shows it, and decides nothing from it. `Docs/02` §2 and `CLAUDE.md` put every transition behind
/// one guarded server-side function, and `job_actions.dart` asks the platform what is permitted
/// rather than working it out from the value.
library;

export 'package:shipper/features/jobs/job_status.gen.dart';
