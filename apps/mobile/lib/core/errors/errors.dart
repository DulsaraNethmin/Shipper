/// Failure handling (`Docs/07` §2).
///
/// The platform returns one error shape with a machine-readable `code` and the request ID
/// (SHIP-12, and `services/core/README.md` for the shape). **Clients branch on `code`, never
/// on `message`** — the message is copy and may be reworded server-side without a release,
/// which is the point of it living there.
///
/// The request ID travels with the failure so a support conversation can name one. It is
/// shown to the user only where it helps them get help.
///
/// The transport-level mapping lives in `core/api`. What belongs here is what the rest of the
/// app sees: a small set of failures a screen can actually respond to.
///
/// Empty until M1 gives a screen something to respond to.
library;
