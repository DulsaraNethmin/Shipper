# apps/admin — Next.js administration panel

The privileged support and moderation interface (`Docs/06` §2). Separate from the customer
and provider experience by design.

## Not yet scaffolded

This directory is a placeholder created by **SHIP-1**. The Next.js project arrives in
**SHIP-22**, and the substance of the panel is milestone **M6** (SHIP-147…SHIP-166).

## Things that are already decided

- Admin authentication is **independent of user authentication**. An admin surface can never
  be reached with a user token (SHIP-147).
- Permissions are granular and default to the minimum (SHIP-148).
- Every privileged mutation writes an append-only audit entry. Ordinary administrators
  cannot delete audit entries (SHIP-149, SHIP-150).
- Verification evidence renders through short-lived signed URLs and every view is
  access-logged (SHIP-155).

## Note on the API contract

The admin panel consumes the same versioned Go API as the mobile app and driver portal. It
keeps its own Next.js server-side data access for privileged screens, but that is an
application detail — **not** a shared platform tier. There is no BFF (`Docs/06` §2.1).
