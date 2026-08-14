/// Why a recorded milestone carries no photograph — `Docs/01` §4.4's three (SHIP-56a).
///
/// The enumeration is generated from `contracts/statuses.yaml` by `make codegen`;
/// `proof_exception_reason.gen.dart` beside this file is the Dart form, and the platform's is
/// `internal/delivery/proofexception_gen.go`.
///
/// **Nothing imports it yet, and that is deliberate rather than an oversight.** SHIP-131 is the
/// ticket that offers the exception path when a camera permission is denied, and it is the first
/// screen that needs these three words. Generating them now means that screen gets a vocabulary
/// already agreeing with the service, the driver portal and `ck_proofs_exception_reason` — rather
/// than a fourth hand-written copy, which is what every one of these lists started as.
///
/// The file exists for the reason `job_status.dart` does: `<name>.gen.dart` is written by the
/// generator and imported by nothing, `<name>.dart` is written by a person and imported by
/// everything, so behaviour arriving later needs no import site to change.
library;

export 'package:shipper/features/delivery/proof_exception_reason.gen.dart';
