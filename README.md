# Shipper — MVP Plan
### Author - Nethmin Dulsara

This pack translates the initial Shipper concept into the documents needed before detailed design and implementation.

## Contents

1. [MVP product requirements](./Docs/01-mvp-product-requirements.md)
2. [Job lifecycle and status rules](./Docs/02-job-lifecycle.md)
3. [User journey maps](./Docs/03-user-journey-maps.md)
4. [Verification and moderation procedures](./Docs/04-verification-and-moderation.md)
5. [Policy and legal-review brief](./Docs/05-policy-and-legal-review-brief.md)
6. [Solution architecture and delivery roadmap](./Docs/06-solution-architecture-and-roadmap.md)
7. [Mobile application architecture](./Docs/07-mobile-architecture.md)
8. [Build startup guide](./Docs/08-build-startup-guide.md)
9. [Delivery backlog](./Docs/09-delivery-backlog.md) — 194 tickets in build order, with a [Jira import CSV](./Docs/09-delivery-backlog-jira.csv)

## Working assumptions

The documents use these provisional MVP decisions:

- Launch as an Australian road-transport marketplace, beginning with a defined pilot region.
- Support legal, non-living packaged goods only.
- Use an optional customer budget; provider bid prices are private.
- Use manual delivery milestones and proof of delivery; do not include live GPS in the MVP.
- Operate as a marketplace only in the MVP; Shipper does not collect or hold payment.
- Permit providers to bid only after baseline account and vehicle verification.
- Deliver the customer and provider experience as a single Flutter mobile application for iOS and Android, with the role chosen at signup.
- Keep administration on the web, and keep the assigned-driver portal a job-scoped web page that requires no account.
- Distribute the pilot through TestFlight and Play internal testing rather than a public store release.

## Client surfaces

The MVP is mobile-first but not mobile-only. Three client surfaces exist, and the split is deliberate:

| Surface | Users | Technology | Why |
|---|---|---|---|
| Mobile app | Customer, provider | Flutter (iOS + Android) | The marketplace is used in the field, on a phone |
| Admin panel | Administrator | Next.js web | Support and moderation are desk work needing wide screens |
| Driver portal | Assigned driver | Next.js web, link-authenticated | A subcontracted driver will not install an app for one job |

Items labelled **Decision required** need a product, business, operations, or legal owner to approve them before implementation.
