# apps/driver-portal — Next.js job-scoped driver portal

A responsive web portal that an assigned driver opens from a link. **No account, no signup,
no app install** (`Docs/06` §2). The provider forwards the link; there is no SMS integration
in the MVP (`Docs/08` Step 0).

## Not yet scaffolded

This directory is a placeholder created by **SHIP-1**. The Next.js project arrives in
**SHIP-23**, and the portal itself in **SHIP-120**…**SHIP-123**.

## The invariant that defines this surface

The driver's **job-scoped token** and the mobile **auth token** are separate systems.
Neither can be exchanged for the other. The driver token is signed, time-limited, and grants
access to **exactly one job** and nothing else (`Docs/06` §5.2, SHIP-107, SHIP-108).

A driver-portal session must never be able to reach another job, a user profile, or any
account surface — regardless of what is typed into the address bar.

## Design constraints

- Used one-handed, outdoors, on a phone browser, often on poor signal. Large touch targets
  (SHIP-121).
- Photo proof is captured through the browser camera and uploaded **directly** to private
  object storage via a short-lived pre-signed URL — never proxied through the API
  (SHIP-122, `Docs/06` §5.2).
- The portal becomes read-only once the delivery is completed (SHIP-123).
