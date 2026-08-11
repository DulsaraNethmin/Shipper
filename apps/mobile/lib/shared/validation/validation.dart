/// Field-level validation for forms (`Docs/07` §2).
///
/// Every rule here is a **convenience**. `Docs/07` §2 is explicit: the app may pre-validate,
/// and the platform decides. A field this package accepts may still be rejected server-side,
/// and the form has to render that rejection rather than assume it cannot happen.
///
/// Limits that change under operational pressure — lengths, ranges, category lists — are
/// fetched, not compiled in. Dart has no over-the-air update path (`Docs/07` §1), so a
/// hard-coded limit is a store release away from being corrected.
///
/// `validators.dart` holds the signup checks (SHIP-51, SHIP-53, SHIP-54) and its own header
/// carries the one place that rule is knowingly bent, and what stops the bend from mattering.
library;
