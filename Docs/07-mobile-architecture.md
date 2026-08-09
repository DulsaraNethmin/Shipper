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
    profile/       customer and provider profiles
    fleet/         vehicles and eligibility
    jobs/          creation, publication, discovery, detail
    bidding/       bids, counter-offers, negotiation, award
    delivery/      milestones, proof capture, driver assignment
    notifications/ push registration, inbox, deep-link routing
  shared/          design system, formatting, validation
```

Rules that keep this from decaying:

- Features do not import from one another. Shared behaviour moves to `core` or `shared`.
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

## 9. Decisions required

Engineering decisions, to be closed during Step 1 of `08-build-startup-guide.md`:

- State management approach and the minimum supported iOS and Android versions.
- Local persistence technology for the offline queue and cached reads.
- Whether biometric unlock is offered in the MVP or deferred.
- Whether the app version floor is raised on a schedule or only in response to a specific defect.

Product decisions already settled that constrain this client:

- **Unsynced milestones** escalate on a tiered schedule — in-app indicator immediately, provider nudge at 4 hours, operations alert at 24 hours (`02` §3.1). The app owns the first two.
- **Photo proof is mandatory** with a reasoned exception path (`01` §4.4). The camera flow must handle a denied permission without dead-ending, which makes §4's offline queue and the exception path the same piece of work.
- **The customer's budget is never sent to a provider's device** (`01` §4.3). It must not appear in any provider-facing API response, including ones the app does not currently render.
