# Shipper — Mobile Application Architecture

**Status:** Draft  
**Audience:** Architecture, mobile engineering, security, product  
**Purpose:** Define how the Flutter client is built, secured, released, and kept compatible with the platform. Sets direction without prescribing implementation detail.

This document covers only the mobile client. Platform architecture remains in `06-solution-architecture-and-roadmap.md`.

## 1. Scope of the client

One Flutter application serves **customers and providers**, with the role chosen at signup. Administration is a separate web application, and the assigned driver uses a job-scoped web portal with no account and no install.

A single app for both marketplace sides was chosen over two apps because a pilot cannot afford two store listings, two review cycles, and two release trains. The trade-off is that role-specific navigation must be genuinely separate inside the app; a customer should never see provider surfaces or the reverse.

**Flutter was chosen for existing team capability.** The material cost of that choice is that **Dart code cannot be updated over the air** — every fix ships through store review. Two consequences shape the design:

- Anything expected to change under operational pressure belongs on the server. Category lists, policy copy, validation limits, expiry windows, and feature switches are fetched, not compiled in.
- The pilot distributes through TestFlight and Play internal testing, which bypasses public review and keeps the release loop short while the product is still moving.

## 2. Application structure

Organise by **feature**, mirroring the platform domains in `06` §3, rather than by technical layer. A feature owns its screens, state, and data access; shared concerns live in a small core.

```
lib/
  core/            networking, auth, storage, error handling, offline queue
  features/
    identity/      registration, sign-in, verification, role selection
    profile/       customer and provider profiles, and verification evidence
    fleet/         vehicles and eligibility
    jobs/          creation, publication, discovery, detail
    bidding/       bids, counter-offers, negotiation, award
    delivery/      milestones, proof capture, driver assignment
    notifications/ push registration, inbox, deep-link routing
  shared/          design system, formatting, validation
```

Rules that keep this from decaying:

- Features do not import from one another. Shared behaviour moves to `core` or `shared`.
- **The list above is closed, and `apps/mobile/test/architecture_test.dart` holds `lib/features` to
  exactly it.** A folder that appeared is not a feature; a feature is a decision recorded here first.
  This is the same arrangement `internal/boundaries` gives the Go side, where a new package under
  `internal/` fails the lint until somebody classifies it.
- **Verification evidence belongs to `profile/`, capture as well as display.** `Docs/04` §3.1 puts
  the camera in the app and §3 puts the reviewing in the administrator's hands; the provider-facing
  half of that is a profile concern and not an eighth feature. Decided rather than assumed, because
  the alternative — a `verification/` feature — reads as the tidier answer right up until it
  duplicates what `profile/` is already for.
- **Reuse across features is what `core/` is for, and moving is the mechanism.** `ProofCamera`,
  `ProofImagePolicy` and `ProofStore` were written inside `delivery/` for proof of delivery and are
  wanted unchanged for verification capture, so they move to `core/` rather than being imported
  across the boundary or copied. The precedent is `ProviderOnly`, moved to `core/auth/` by SHIP-100
  for the same reason. **A second caller is the signal; the move is the answer.**
- No business rule is authoritative on the device. The app may hide, disable, or pre-validate, but the platform decides. A client-side check is a convenience, never a control.
- The API client is generated from or validated against the published API contract, so a breaking platform change fails at build time rather than in the field.

## 3. Authentication and session

This replaces the web session-cookie model entirely; there is no cookie and no CSRF concern.

| Element | Position |
|---|---|
| Access token | Short-lived, sent as a bearer token, held in memory |
| Refresh token | Longer-lived, rotated on every use, stored in iOS Keychain / Android Keystore via `flutter_secure_storage` |
| Session record | One per device, listed and individually revocable by the user and by admin |
| Reuse detection | A refresh token presented twice invalidates the whole device session |
| Biometric re-auth | Optional local unlock; a convenience over the stored token, never a substitute for it |

- Tokens must never be written to application preferences, application documents, logs, or crash reports.
- Sign-out clears secure storage, the offline queue, cached job data, and the push token registration.
- **The mobile token scheme and the driver portal's signed single-job link token are separate systems.** Neither can be exchanged for the other, and the driver token grants access to exactly one job and expires. This separation is a privacy control, not an implementation detail.

## 4. Offline behaviour

The single most important client capability, and the one most often underestimated. Drivers and providers work where there is no signal.

**Principle: the user records what happened and moves on. Syncing is the platform's problem.**

- Milestone updates and proof capture write to a **local durable queue** first, then sync. The UI confirms immediately, marked clearly as pending.
- Every queued operation carries a **client-generated idempotency key**, so retries after a dropped connection cannot duplicate records. See `02-job-lifecycle.md` §3.1.
- Each operation records **when the user acted**, distinct from when the platform accepted it. The first is what people see; the second is what audit relies on.
- Sync retries with backoff, survives app restart, and never silently drops an operation.
- Proof images queue as compressed local files and upload on reconnection, not as part of the milestone request.
- On conflict the **server always wins**. The app reconciles to platform state and must show the user what happened rather than discarding their work silently.
- Reads are cached for offline viewing of an active job. Cached job data clears on sign-out and after the job closes.

What is deliberately **not** offline: bidding, awarding, and negotiation. These are competitive, time-sensitive, and multi-party; a stale local decision is worse than an honest "you are offline."

## 5. Push notifications

Firebase Cloud Messaging fronts both platforms, including APNs delivery on iOS.

- Device tokens register after sign-in against the per-device session, and de-register on sign-out. Tokens rotate and the platform must handle rotation and stale-token cleanup.
- Every notification carries the identifiers needed to **deep-link** to the specific job, bid, or dispute it concerns.
- **Push delivery is never proof of receipt.** It is a prompt, not a channel of record. Anything that matters is also retrievable in-app and, where appropriate, by email.
- Permission is requested at a moment its value is obvious — after a first bid arrives, not on first launch. The app remains fully usable when it is declined, and offers a route to re-enable it.
- Notification content excludes addresses, goods descriptions, and full customer names, since it renders on a lock screen. See `05-policy-and-legal-review-brief.md` §4.

## 6. API versioning and the upgrade gate

Old builds persist on devices indefinitely and cannot be forced forward the way a web deployment can. This is the structural difference between shipping web and shipping mobile.

- The public API is **versioned from the first release**, and more than one version is live at any time.
- The app calls a **minimum-supported-version endpoint at launch**. Below the floor, it blocks with an update prompt linking to the store; above it, a soft prompt may encourage updating without blocking.
- Raising the floor is an operational decision with user impact and needs an owner, not a side effect of a deploy.
- A version is retired only when telemetry shows negligible traffic from builds depending on it.
- Server changes stay **backward compatible by default**: add fields, do not repurpose or remove them. Unknown fields must be tolerated by the client so additive changes need no release.

## 7. Store compliance

Treated as build work, not launch paperwork. Each item below has blocked real submissions.

- **In-app account deletion.** Required by Apple for any app offering account creation. Must reconcile with audit retention — see `05` §3.
- **Permission purpose strings** for camera and notifications, in plain language.
- **Apple privacy labels** and the **Google Play data-safety declaration**, consistent with the published privacy policy.
- **Publicly reachable privacy-policy URL**, required before any build is distributed.
- **Native functionality.** Apple Guideline 4.2 rejects apps that are effectively a wrapped website. A genuine Flutter client satisfies this; a WebView shell would not, which is a further reason the driver portal stays a separate web surface rather than being embedded.
- Age rating and encryption/export-compliance declarations, for a later public listing.

## 8. Build and release

- CI produces signed iOS and Android builds. Signing keys and store credentials live in the CI secret store, never on developer machines.
- Every build carries a unique, monotonically increasing build number and reports its version with each API call, so problems can be attributed to a specific build.
- Environments are separate and independently installable — a tester must be able to hold a staging and a production build on one device.
- Crash and adoption reporting is per version, feeding the retirement decision in §6.
- Pilot distribution is TestFlight and Play internal testing. A public store listing is a later, separate gate.

## 9. Engineering decisions — three closed, one open

Four engineering decisions were left here for Step 1 of `08-build-startup-guide.md`. Two are now closed and recorded in `10-engineering-conventions.md` §8.3, and a third — biometric unlock — was closed at SHIP-55 and is recorded below. The last turned out not to be an engineering call at all, and is annotated below with where it does get closed — an open decision with no named home is indistinguishable from one everybody forgot.

The first of the four was compound: it bundled the minimum supported OS versions with the state approach, and `10` §8.3 settles only the state half. Both halves are closed here, which is why four decisions make five rows.

| Decision | State | Recorded in, or closes at |
|---|---|---|
| State management | **Riverpod** | `10` §8.3 |
| Minimum supported iOS and Android versions | **iOS 14.0, Android API 24** | Below |
| Local persistence for the offline queue and cached reads | **Drift over SQLite** | `10` §8.3 |
| Biometric unlock in the MVP, or deferred | **Out of the MVP** | Closed at SHIP-55 — below, and `11` §3 and §9 |
| Version floor raised on a schedule or per defect | Open — an operational policy | Needs the owner §6 asks for; the mechanism is already built |

**State management is Riverpod**, with `go_router` for routing, `freezed` and `json_serializable` for models, and `dio` for transport. `10` §8.3 carries that list and is where it changes; restating it here would only create a second copy to disagree with.

**The minimum supported versions are iOS 14.0 and Android API 24 (Android 7.0).** The Android floor is the one that needed an argument. §3 puts the refresh token in the Keystore, and `flutter_secure_storage` reaches it by wrapping the stored value under a key the Keystore holds, which requires API 23 — below that it falls back to something weaker. So API 21 and 22, which Flutter itself still supports, cannot hold the token the way §3 requires. Taking Flutter's own floor would have bought reach at the price of the single storage guarantee this document makes, and a token store that is only sometimes hardware-backed is not a guarantee. API 24 clears that boundary rather than sitting on it. iOS 14.0 costs very little by comparison — that install base moves forward on its own.

**That paragraph named `EncryptedSharedPreferences` until SHIP-48, and by then it was wrong.** The package deprecated it — Google deprecated the Jetpack Security library behind it — in favour of its own Keystore-wrapped AES-GCM and RSA-OAEP ciphers. The requirement is still API 23 and the floor is still API 24, so **the decision did not move**; only the sentence justifying it had stopped being true. Corrected rather than left, because a floor whose stated reason no longer exists is a floor somebody reopens.

**Local persistence is Drift over SQLite.** §4 calls the durable queue the single most important client capability, and SHIP-124 requires that a queued operation is never silently dropped. That is a transactional requirement rather than a storage one: an operation, its client-generated idempotency key, the time the user acted, and the local path to its proof image either all commit or none of them do, and the sync worker has to mark an item in flight and recover cleanly when the process dies mid-upload. A key-value store — `shared_preferences`, or Hive — is simpler and cannot express any of that. The first time it half-writes a queue entry, the client has quietly dropped precisely what §4 promises it will not.

**Biometric unlock is out of the MVP, and that was decided at SHIP-55.** §3 fixed its position long ago and that has not moved — an optional local unlock, a convenience over the stored token and never a substitute for it. What was open was the scope question, and this paragraph said it closed "at SHIP-48 onwards". It closed at SHIP-55 instead, because an optional local unlock is a gate on a sign-in screen and SHIP-48 built the token store without one. The reasoning, in the order it weighed: the refresh token is already behind `first_unlock_this_device` on iOS and a Keystore-wrapped key on Android, so the device passcode gates it, and what biometric unlock adds is a second gate in front of an app on an **already unlocked** handset; the cost is asymmetric, because `IOSOptions.accessControlFlags` is nearly free while `AndroidOptions.biometric()` requires API 28 against this document's floor of API 24, and "only sometimes biometric" is the same half-guarantee the floor argument above rejects; and an optional control needs somewhere to turn it off, which no settings surface offers before SHIP-173. `11` §3 and §9 carry the full reasoning.

**Three things would reopen it**, named so this is a decision rather than a shrug. The Android floor rising to API 28 for another reason — SHIP-24 and SHIP-26 touch the Android build configuration and already carry the `flutter_secure_storage` 11 / `compileSdk` question, and if the floor moves there the Android half becomes free. A settings surface existing, SHIP-173's in-app account screen being the first. Or the pilot holding something that makes an unlocked handset a real exposure — payment details, which the MVP holds none of, or a customer address history on a shared device.

**The version-floor policy stays open, and it is operational rather than engineering.** SHIP-167 is built and configuration-driven — `MIN_SUPPORTED_IOS_BUILD` and `MIN_SUPPORTED_ANDROID_BUILD` — so raising the floor is already a configuration change rather than a release. What is missing is not a mechanism but the owner §6 asks for: who decides that a floor rises, on what evidence, and whether that happens on a cadence or only when a specific defect forces it. Blocking a build is a support event before it is anything else, so the call belongs with whoever carries the pilot's support load.

Product decisions already settled that constrain this client:

- **Unsynced milestones** escalate on a tiered schedule — in-app indicator immediately, provider nudge at 4 hours, operations alert at 24 hours (`02` §3.1). The app owns the first two.
- **Photo proof is mandatory** with a reasoned exception path (`01` §4.4). The camera flow must handle a denied permission without dead-ending, which makes §4's offline queue and the exception path the same piece of work.
- **The customer's budget is never sent to a provider's device** (`01` §4.3). It must not appear in any provider-facing API response, including ones the app does not currently render.
