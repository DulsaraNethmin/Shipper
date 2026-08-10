# apps/admin — Next.js administration panel

The privileged support and moderation interface (`Docs/06` §2). Separate from the customer
and provider experience by design.

## Scaffolded, and nothing more than that

**SHIP-22** landed the Next.js project: App Router, TypeScript in strict mode, Tailwind with
shadcn/ui, inside the pnpm workspace at the repository root (`Docs/10` §8.4). The substance
of the panel is milestone **M6** (SHIP-147…SHIP-166).

```
make web-dev app=admin     run it at http://localhost:3001
make web-build             build every web application
make web-check             lint, type-check and build
```

What exists is a **placeholder authenticated shell**: the chrome an administrator will see
around every screen, with the M6 sections listed and inert, and no data of any kind.

**There is no sign-in form, deliberately.** Administrator authentication is SHIP-147 and is
a separate system from user authentication — a placeholder login is a hole somebody
eventually wires to the wrong verifier. The shell says who is signed in (nobody) and leaves
it there.

No screen reads from the API and no component takes marketplace data as a prop. That is
also deliberate: a table has to decide which columns exist, and the ticket that first
decides should be one that has read `Docs/01` §4.3 on what a budget is.

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
