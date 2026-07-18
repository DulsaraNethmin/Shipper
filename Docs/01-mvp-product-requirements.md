# Shipper — MVP Product Requirements

**Status:** Draft  
**Audience:** Product, business, operations, design, architecture  
**Goal:** Define the smallest release that proves the customer-to-provider delivery marketplace.

## 1. Outcome

Shipper enables a customer to publish a transport job, eligible providers to submit bids, the customer to award one provider, and both parties to follow the delivery through to completion.

The MVP succeeds if it establishes a repeatable flow from published job to completed delivery with reliable support visibility.

## 2. Scope

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
| Assigned driver | View and update only the assigned delivery through a restricted link/portal | Access bids, negotiations, customer history, or other jobs |
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

**Decision required:** Confirm whether the customer’s maximum budget is visible to providers. Recommended: visible only as an optional range or “budget supplied,” not as an exact amount.

### 4.4 Delivery execution

The provider or assigned driver can record:

1. Driver assigned
2. En route to pickup
3. Picked up
4. In transit
5. Delivered

Delivered jobs require recipient name, delivery timestamp, and delivery note. Photo or signature proof is optional but recommended for selected jobs.

**Acceptance measure:** A customer can see the latest delivery milestone and proof of delivery for their awarded job.

### 4.5 Notifications

Essential events generate an in-app notification and, where appropriate, email:

- Account verification completed or rejected.
- Job published.
- New bid, counter-offer, withdrawal, or bid expiry.
- Bid accepted or job cancelled.
- Delivery status changes.
- Dispute opened or resolved.

SMS is deferred unless research shows email/in-app notifications are insufficient for the pilot.

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

- **Security:** Role- and job-scoped access control; encrypted data in transit and at rest; audit logs for privileged actions.
- **Reliability:** Critical job and bid actions must be durable and recoverable. A notification failure must not lose the business event.
- **Privacy:** Minimise exposure of phone numbers, addresses, and delivery details; restrict the driver portal to a single job.
- **Usability:** Job publication and bid submission should be practical on a mobile phone.
- **Performance:** Common pages should feel responsive under pilot load; exact service targets to be defined before launch.
- **Observability:** Monitor errors, job/bid transaction outcomes, notification failures, and delivery workflow delays.

## 6. MVP business rules

- Only legal, non-living goods in permitted categories may be listed.
- A customer owns a job until it is awarded; a provider owns a bid until it is accepted, withdrawn, rejected, or expired.
- A provider must be eligible and verified before bidding.
- A job can have one accepted bid only.
- A job cannot be edited in a way that changes its core requirements after an award; changes require cancellation/re-agreement or support intervention.
- The user who makes a critical action is recorded with timestamp and action reason where applicable.

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

Do not invite public users until the team has approved the job lifecycle, prohibited-goods policy, provider verification approach, support procedure, pilot geography, and terms/privacy materials.
