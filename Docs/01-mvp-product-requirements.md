# Shipper — MVP Product Requirements

**Status:** Draft  
**Audience:** Product, business, operations, design, architecture  
**Goal:** Define the smallest release that proves the customer-to-provider delivery marketplace.

## 1. Outcome

Shipper enables a customer to publish a transport job, eligible providers to submit bids, the customer to award one provider, and both parties to follow the delivery through to completion.

The MVP succeeds if it establishes a repeatable flow from published job to completed delivery with reliable support visibility.

## 2. Scope

### Client surfaces

The MVP is mobile-first but not mobile-only. Requirements below apply to whichever surface serves the role:

| Surface | Roles served | Form |
|---|---|---|
| Mobile app | Customer, provider | Flutter application for iOS and Android; role chosen at signup |
| Admin panel | Administrator | Web application |
| Driver portal | Assigned driver | Job-scoped web page opened from a link; no account and no app install |

Job-scoped driver access is a **web** capability. It is not a feature of the mobile app, and an assigned driver must never be required to install anything.

### In scope

- Customer, provider, assigned-driver, and administrator roles.
- Customer job creation, publication, amendment before award, and cancellation.
- Provider onboarding, profile, vehicle management, job discovery, bids, and negotiation.
- Customer comparison, negotiation, bid acceptance, and job tracking.
- Delivery milestones and proof of delivery.
- Job-scoped driver access.
- In-app and email notifications for essential events.
- Admin search, user support, moderation, and dispute handling.
- Audit trail for critical actions.

### Out of scope

- In-platform payment, escrow, invoices, commission collection, or refunds.
- Real-time vehicle location and route optimisation.
- Ratings/reviews, subscriptions, or loyalty programmes.
- International freight, warehousing, or multi-stop jobs.
- Dangerous goods, live animals, people, or specialist regulated freight.
- Automated pricing and provider ranking by machine learning.

## 3. Users and permissions

| Role | Primary capabilities | Must not be able to |
|---|---|---|
| Customer | Manage own jobs, review bids, negotiate, award, track, open disputes | View other customers’ jobs or unrelated provider data |
| Provider | Manage profile/fleet, view eligible open jobs, bid, negotiate, update awarded jobs | View competing bid prices or alter jobs they do not own |
| Assigned driver | View and update only the assigned delivery through a restricted web link/portal, with no account and no app install | Access bids, negotiations, customer history, or other jobs |
| Administrator | Support, moderation, user restriction, dispute resolution, audit review | Change commercial records without an auditable reason |

## 4. Functional requirements

### 4.1 Customer account and job creation

The customer can:

- Register, sign in, verify email and phone, and manage profile details.
- Create a job with pickup/drop-off locations, date windows, goods description, dimensions/weight, vehicle requirement, handling notes, and optional maximum budget.
- Save a draft; publish an eligible job; edit or cancel it before award.
- View bids, provider profiles, vehicle information, and offered timing.
- Send messages and counter-offers attached to a job.
- Accept one bid and track its job status.
- Confirm delivery or raise a dispute.

**Acceptance measure:** A verified customer can publish a valid job without support assistance.

### 4.2 Provider onboarding and fleet

The provider can:

- Register as an individual or business, complete profile details, nominate service area and specialties.
- Add, edit, deactivate, and select one or more vehicles.
- Submit required declarations and verification information.
- View only jobs matching eligibility rules.
- Place, update, and withdraw a bid until it is accepted or expires.
- View and update jobs awarded to that provider.

**Acceptance measure:** A verified provider can maintain a fleet and submit a compliant bid.

### 4.3 Matching and bidding

The platform must:

- Filter jobs by provider service area, vehicle capability, verification state, and job status.
- Treat provider bid price as private from competing providers.
- Allow a customer to compare price, timing, provider profile, vehicle, and declared capability.
- Enforce one accepted bid per job.
- Close competing bids once a job is awarded.
- Record all offers, counter-offers, withdrawals, and acceptances.

**Decided:** The customer's maximum budget is **private**. Providers never see it — not as an amount, not as a band, and not as a "budget supplied" indicator. It is used only on the customer's side, to filter and sort the bids they receive.

The reasoning is asymmetry, not secrecy. If providers can see the maximum, bids converge on it, which defeats the competitive pricing that is the marketplace's purpose. That behaviour is also **not reversible**: once providers learn to bid at the ceiling, removing the signal does not remove the habit. Starting private keeps the option to introduce a signal later; starting visible destroys it.

Revisit only if the evidence demands it. The metrics that would justify reopening this are **time to first bid** and **share of jobs receiving at least one bid** (`§7`). If providers are bidding far off-target because they cannot judge the job, the fix is more likely better job detail — dimensions, access constraints, handling notes — than exposing the budget.

### 4.4 Delivery execution

The provider or assigned driver can record:

1. Driver assigned
2. En route to pickup
3. Picked up
4. In transit
5. Delivered

Delivered jobs require recipient name, delivery timestamp, delivery note, and photo proof.

Milestone updates must succeed without a network connection. Pickup bays, warehouses, and rural routes routinely have no usable signal, and a driver cannot be asked to stand still until a request completes. Updates queue on the device and sync when connectivity returns; the recorded time is the time the driver acted, not the time the server received it.

#### Photo proof is mandatory

**Decided:** A job cannot be recorded as Delivered without photo proof, except through the exception path below.

The MVP carries no live GPS (`§2`), which means proof of delivery is the *only* evidence that the job happened as claimed. It is doing the trust work that tracking would otherwise share. A native camera also removes the friction argument that originally justified making proof optional — capture is now two taps.

**The exception path is part of the same feature and must be built with it, not after.** Proof can legitimately be impossible:

- The recipient objects to being photographed.
- The camera permission is denied or the hardware is unavailable.
- The delivery point is unlit or unsafe to photograph.

In these cases the driver selects a reason and proceeds. The job completes, the missing proof is recorded with its reason, and the job is flagged into the moderation queue (`04` §5). What must never happen is a driver standing at a delivery point unable to finish the job — that converts a UI constraint into an operational failure and a support call.

~~**Decision required:** whether a job completed via the exception path requires customer confirmation before it can auto-complete, rather than expiring into Completed under the 72-hour rule. Owner: operations.~~ **Decided on 14 August 2026 (X-6): it does not.** A job delivered through this path auto-completes on `02` §6.1's ordinary 72-hour rule, which is where the decision and its reasoning are recorded. The short form is that SHIP-117 now queues every exception-completed job for moderation, so a person reviews it either way — blocking auto-completion would add no review and would only strand the job when the customer never acts. SHIP-119 implements the rule.

**Acceptance measure:** A customer can see the latest delivery milestone and proof of delivery for their awarded job; a driver can record a full delivery with no connectivity and have it appear correctly once back in range; and a driver with a denied camera permission can still complete a job.

### 4.5 Notifications

Essential events generate a notification through the channels appropriate to the recipient:

- Account verification completed or rejected.
- Job published.
- New bid, counter-offer, withdrawal, or bid expiry.
- Bid accepted or job cancelled.
- Delivery status changes.
- Dispute opened or resolved.

**Push is the primary channel** for customers and providers, with email retained for records and for anything the user may need to retrieve later. A push notification must deep-link to the specific job it concerns, and delivery of a push must never be treated as confirmation that the user saw it — the platform state remains authoritative.

Push requires an explicit permission grant that the user may decline or later revoke. The app must remain usable without it, and must not request the permission on first launch before the user understands what it is for.

The assigned driver has no app and therefore receives no push.

**Decided:** the **provider forwards the job-scoped portal link to their driver themselves**. Shipper does not send it by SMS in the MVP. The provider already has a working channel to their own driver, and adding an SMS integration and a per-message cost to a pilot buys little.

**However, the driver's name and mobile number are captured at assignment regardless.** They are needed for support escalation when a delivery goes wrong — which is precisely when nobody wants to be routing questions through the provider — and capturing them now means enabling direct SMS in Phase 2 is a configuration change rather than a schema migration and a re-consent exercise.

### 4.6 Administration and support

Administrators can:

- Search users, jobs, bids, and disputes.
- Review account verification status.
- Remove or unpublish policy-breaching jobs.
- Restrict or suspend accounts with a recorded reason.
- Add internal support notes.
- Resolve disputes using a documented outcome.
- View an immutable history of important actions.

## 5. Non-functional requirements

### 5.1 Carried forward

- **Security:** Role- and job-scoped access control; encrypted data in transit and at rest; audit logs for privileged actions. Mobile credentials live in the platform keystore, and no authorisation decision is made on the device.
- **Reliability:** Critical job and bid actions must be durable and recoverable. A notification failure must not lose the business event.
- **Privacy:** Minimise exposure of phone numbers, addresses, and delivery details; restrict the driver portal to a single job.
- **Usability:** Job publication and bid submission are designed for a phone held one-handed in the field. This is now the baseline the product is built to, not an accommodation.
- **Performance:** Common screens should feel responsive under pilot load on a mid-range Android device over a mobile network, not only on a modern phone over Wi-Fi. Exact service targets to be defined before launch.
- **Observability:** Monitor errors, job/bid transaction outcomes, notification failures, and delivery workflow delays. Client-side crash and adoption reporting is needed per app version, since problems can now be confined to one build.

### 5.2 New requirements introduced by the mobile client

These have no equivalent in a web product and are easy to underestimate.

- **Offline tolerance.** Delivery milestones and proof capture must work with no connection and sync later. Every state-changing request carries a client-generated idempotency key so retries cannot duplicate records.
- **Forward compatibility.** Old builds stay installed on devices indefinitely and cannot be forced forward the way a web deployment can. The public API must be versioned, and the app must check a minimum-supported-version endpoint at launch and block with an update prompt when it falls below the floor.
- **Store compliance.** Permission purpose strings for camera and notifications, Apple privacy labels, the Google Play data-safety declaration, a publicly reachable privacy policy, and **in-app account deletion**. Account deletion is a hard Apple requirement for any app offering account creation, not a nice-to-have, and it must be reconciled with the audit-retention position in the policy brief.
- **Payload economy.** Assume metered mobile data. Compress images on the device before upload, upload directly to object storage rather than through the API, and avoid re-fetching unchanged job data on every screen.
- **Permission resilience.** Camera and notification permissions may be denied or revoked at any time. Every affected flow needs a working degraded path and a clear explanation, never a dead end.

## 6. MVP business rules

- Only legal, non-living goods in permitted categories may be listed.
- A customer owns a job until it is awarded; a provider owns a bid until it is accepted, withdrawn, rejected, or expired.
- A provider must be eligible and verified before bidding.
- A job can have one accepted bid only.
- A job cannot be edited in a way that changes its core requirements after an award; changes require cancellation/re-agreement or support intervention.
- The user who makes a critical action is recorded with timestamp and action reason where applicable.
- **A customer's maximum budget is never disclosed to a provider**, in any form or through any channel. This is enforced by the platform, not by the client.
- **A job cannot reach Delivered without either photo proof or a recorded exception reason.**
- **A job returned to Open after a provider cancellation is editable again**, since it is once more an unawarded job the customer owns.

## 7. Success measures

| Metric | Why it matters |
|---|---|
| Published jobs per week | Customer demand |
| Share of jobs with one or more bids | Provider supply and matching quality |
| Time to first bid | Marketplace responsiveness |
| Award rate | Customer confidence and useful supply |
| Completion rate | Delivery reliability |
| Cancellation and dispute rate | Operational quality and policy fit |
| Repeat customer/provider activity | Marketplace value |

## 8. Release gate

### Pilot geography

**Decided:** the pilot covers **one metropolitan area plus a radius of approximately 150 km**, within a single state.

The radius matters more than it looks. Intra-city work alone would put Shipper against established couriers in a crowded segment; extending to regional runs captures the trips where independent operators actually make margin, and where a customer's problem is genuinely unsolved — pricing a furniture move to a regional town today means ringing around. That matches the small-transport-operator framing the product was conceived around.

Staying inside one state avoids cross-border variation in transport rules during a pilot that has no capacity to absorb it.

**Decision required:** which metropolitan area. Owner: business. Needed before provider recruitment begins, not before build starts — at pilot scale supply is recruited by hand, and the practical constraint is where the team can meet operators in person.

### Gate conditions

Do not invite public users until the team has approved the job lifecycle, prohibited-goods policy, provider verification approach, support procedure, pilot geography, and terms/privacy materials.

The mobile client adds prerequisites that must be met before the first build reaches a pilot user:

- Apple Developer Program and Google Play Console enrolment complete.
- Signed builds distributable through TestFlight and Play internal testing.
- In-app account deletion working, and the forced-upgrade gate in place.
- Privacy policy published at a public URL, with Apple privacy labels and the Play data-safety declaration submitted.

Pilot distribution deliberately avoids public store review. A later public listing is a separate gate requiring store assets, age rating, and export-compliance declarations.
