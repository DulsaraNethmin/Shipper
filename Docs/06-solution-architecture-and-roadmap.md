# Shipper — Solution Architecture and Delivery Roadmap

**Status:** Draft  
**Purpose:** Set the architectural direction without prescribing implementation-level design.

## 1. Architecture principles

- Start simple: a modular core platform, not a large set of microservices.
- Make job, bid, and delivery records authoritative and auditable.
- Separate customer/provider experience from administrator experience.
- Keep asynchronous work—notifications, analytics, future integrations—away from critical job transactions.
- Design security and operational visibility into the first release.
- Extract independent services only when scale, team ownership, or operational evidence justifies it.

## 2. Logical architecture

| Layer | Responsibility | Preferred technology |
|---|---|---|
| Customer/provider experience | Responsive marketplace interface | Next.js |
| Administrator experience | Privileged support and moderation interface | Next.js |
| Experience API / BFF | Session-aware page data, read composition, caching | Next.js |
| Core platform | Authoritative business rules and domain workflows | Go |
| System of record | Users, jobs, bids, messages, delivery, audit records | PostgreSQL |
| Cache | Short-lived reads, sessions, rate limits | Redis |
| Event backbone | Notifications, reporting, integrations, background processing | Kafka |
| Observability | Logs, metrics, traces, alerts | Datadog |
| Cloud foundation | Managed runtime, network, storage, security | AWS |

## 3. Platform domains

The core platform should have clear ownership boundaries:

- Identity and access
- Customer/provider profiles
- Vehicle fleet and eligibility
- Jobs
- Bidding and negotiation
- Delivery execution and proof
- Notifications
- Administration, disputes, and audit

At MVP scale, these domains may share one deployable Go application and one PostgreSQL database while maintaining clear internal boundaries. This allows later extraction without premature operational cost.

## 4. Data and integration rules

- PostgreSQL is the source of truth for business decisions.
- Redis may accelerate reads but must not be the only copy of a job, bid, or status.
- Every critical state change emits a durable domain event for notifications, audit, and reporting.
- External services must not be able to alter the core job lifecycle directly.
- Files such as proof-of-delivery images and verification documents live in private object storage; the database retains metadata and access controls.
- Maps/geocoding, email/SMS, identity checks, and payments are replaceable integrations behind domain-owned interfaces.

## 5. Security and operations baseline

- Separate development, staging, and production environments.
- Private data services with least-privilege service access.
- Role-based and job-scoped authorisation.
- Secrets managed outside application code.
- Encrypted connections and encrypted storage.
- Backups, restore tests, deployment rollback, and incident alerting.
- Datadog dashboards for errors, job/bid success, notifications, delayed jobs, and suspicious activity.

## 6. Delivery roadmap

### Phase 0 — Discovery and operating model

Approve product scope, lifecycle, business model, verification policy, prohibited-goods policy, pilot geography, and legal-review outputs.

### Phase 1 — Marketplace MVP

Deliver:

- Customer and provider onboarding.
- Provider profile and vehicle management.
- Job creation, publication, discovery, bidding, negotiation, and award.
- Manual delivery milestones, proof of delivery, and job-scoped driver portal.
- Essential notifications.
- Admin support, moderation, and audit capabilities.

**Exit criterion:** Pilot users can complete real delivery jobs with support visibility and no unresolved critical safety/control gap.

### Phase 2 — Trust and operational maturity

Deliver:

- Enhanced verification and document expiry management.
- Reviews and reputation.
- Improved disputes, reporting, and risk controls.
- Better job matching and provider discovery.
- Pilot expansion based on completion and retention metrics.

### Phase 3 — Commercial scale

Evaluate and introduce only where commercially justified:

- Payments, commissions, invoicing, and refunds.
- Live tracking and ETA.
- Recurring jobs and business accounts.
- Analytics-driven matching/pricing insight.
- Additional regions, vehicle classes, and integrations.

## 7. Key architecture decisions to close

| Decision | Recommended position |
|---|---|
| Initial service shape | Modular Go platform with clear domain boundaries |
| Front-end structure | Shared marketplace app for customer/provider roles; separate admin portal |
| Payment ownership | No payment processing in MVP |
| Tracking | Manual milestones and proof of delivery first |
| Real-time requirements | Notifications are asynchronous; authoritative status is transactional |
| Search/matching | Database-backed filtering first; dedicated search only when evidence requires it |
| Provider verification | Baseline eligibility before bidding; enhanced controls in Phase 2 |

## 8. Delivery governance

- Product owns scope, user experience, KPI definition, and prioritisation.
- Operations owns verification, support, moderation, and pilot feedback.
- Legal/privacy advisers approve public-facing policy and compliance positions.
- Architecture owns non-functional requirements, security baseline, resilience, and technical decision records.
- Business leadership owns geography, revenue model, risk appetite, and launch approval.
