/// Profile — customer and provider profiles, and verification evidence (`Docs/07` §2).
///
/// A provider's public profile is what a customer judges a bid by, and what verification
/// (`Docs/04`) attaches to. **Verification state is displayed here and decided nowhere near here**:
/// `Docs/04` §4's five outcomes are an administrator's, `profiles.Service.Decide` is exported and
/// deliberately reachable from no route a provider can call, and nothing in this folder has an
/// opinion about whether anybody may bid.
///
/// ## What is here (SHIP-81c, SHIP-81d)
///
/// The provider-facing half of `Docs/04` §3.1 — evidence capture. §2 assigns it here in as many
/// words, and says why the tidier-looking alternative was refused: *"the alternative — a
/// `verification/` feature — reads as the tidier answer right up until it duplicates what
/// `profile/` is already for."* `architecture_test.dart` holds `lib/features` to §2's closed list of
/// seven, so this is enforced rather than remembered.
///
/// - `verification_document.dart` — the four kinds `Docs/04` §3 collects, in the platform's wire
///   vocabulary, and what a submitted one reads back as.
/// - `verification_repository.dart` — the presign, the PUT and the submission as one operation, and
///   why it does **not** go through `core/queue`.
/// - `verification_documents_controller.dart` — compressing one photograph, sending it, and what a
///   retry retries.
/// - `verification_documents_screen.dart` — the four rows, and why there is no progress bar.
/// - `capture_document_screen.dart` — the camera, and what is offered when it will not open.
/// - `document_file_source.dart` — the file-upload fallback `Docs/04` §3.1 requires, and the
///   argument for the package it depends on (SHIP-81d).
///
/// ## What is deliberately not here
///
/// **The camera, the compressor and the store.** They are `core/capture/`, because
/// `features/delivery/` needs them too and features do not import one another. SHIP-81c moved them
/// on the precedent `ProviderOnly` set at SHIP-100 — *"a second caller is the signal; the move is
/// the answer"* — rather than copying three files or reaching across a boundary.
///
/// **Any rendering of a submitted image.** `GET /v1/provider/verification/documents` answers a
/// freshly signed `download_url` with a lifetime of minutes and the contract says not to cache one,
/// so `VerificationDocument` does not model it. The screen that reviews these images is the
/// administrator's (SHIP-155), on the administrator's own credential.
///
/// **The profile itself.** A provider's service area and specialties are `GET` and
/// `PATCH /v1/fleet/profile` (SHIP-79), which are not served yet; a customer profile has no
/// endpoint at all. Both are empty until the tickets that build them.
library;
