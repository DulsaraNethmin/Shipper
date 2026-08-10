# apps/driver-portal — Next.js job-scoped driver portal

A responsive web portal that an assigned driver opens from a link. **No account, no signup,
no app install** (`Docs/06` §2). The provider forwards the link; there is no SMS integration
in the MVP (`Docs/08` Step 0).

## Scaffolded, and nothing more than that

**SHIP-23** landed the Next.js project: App Router, TypeScript in strict mode, Tailwind with
shadcn/ui, inside the pnpm workspace at the repository root (`Docs/10` §8.4). The portal
itself is **SHIP-120**…**SHIP-123**.

```
make web-dev app=driver-portal    run it at http://localhost:3002
make web-build                    build every web application
make web-check                    lint, type-check and build
```

Two routes exist, both static and both inert:

| Route | What it is |
|---|---|
| `/` | What this surface is, for anyone who arrives without a link. There is no way in from here — the link is the only entry |
| `/job` | The placeholder delivery page: milestones, proof, and the two things it will never show |

**The placeholder route carries no token, deliberately.** The real page is reached at a
token-bearing URL and renders only after the platform has validated it (SHIP-108, SHIP-120).
A scaffold route that accepted a token would be a shape inviting somebody to render a job
beside a token this application never checked — and authorisation is the platform's
decision, never the client's (`Docs/07` §3).

No web font is fetched and nothing is loaded from the API. This is the surface most likely
to be opened on a phone with two bars of signal.

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
