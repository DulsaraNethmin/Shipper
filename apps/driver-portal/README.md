# apps/driver-portal — Next.js job-scoped driver portal

A responsive web portal that an assigned driver opens from a link. **No account, no signup,
no app install** (`Docs/06` §2). The provider forwards the link; there is no SMS integration
in the MVP (`Docs/08` Step 0).

## Routes

| Route | What it is |
|---|---|
| `/` | What this surface is, for anyone who arrives without a link. There is no way in from here — the link is the only entry |
| `/j/<job-id>#<token>` | **The delivery a link opens (SHIP-120)** |
| `/api/driver/jobs/<job-id>` | This application's own forward to `GET /v1/driver/jobs/{id}`, and to nothing else |

```
make web-dev app=driver-portal    run it at http://localhost:3002
make web-check                    lint, test, build and type-check every web application
```

## The link

```
https://<portal>/j/<job-id>#<token>
```

**The job identifier and the token are both in it, and that is the mechanism rather than
redundancy.** The token already names the job it grants — that claim is inside the signature, which
is what makes it single-job (SHIP-107) — and `GET /v1/driver/jobs/{id}` compares the job in the path
against the job in the token, a comparison SHIP-108 put in the auth class so that no handler can
skip it.

A client that decoded `job_id` out of the token to build the path would make that comparison
compare the token with itself: it would pass for ever, on any grant, however widely issued. So the
identifier reaches this portal independently of the credential, and **nothing here parses a JWT**.
The one claim a page might want — when the link stops working — is served as `link_expires_at`.

**The token is in the fragment**, which is the one part of a URL that is never sent to any server:
not in a request line, not in an access log, not in a proxy, not in a `Referer`. On arrival it is
moved into `sessionStorage` and the fragment is stripped from the address bar, so a screenshot or a
glance over the shoulder shows a job identifier the platform will not serve to anyone else.

`sessionStorage` rather than `localStorage`, which would leave a working seven-day credential on a
shared phone, and rather than a cookie, which would be attached to requests that have nothing to do
with the delivery. Closing the tab loses the token, and that is the intended trade: **the message
the link arrived in is the durable store**, and it is the only copy that should survive. The
reasoning in full is in `lib/link.ts`, and `Docs/11` §3's SHIP-120 entry records why.

## Why there is a route handler in front of the API

`app/api/driver/jobs/[jobId]/route.ts` forwards to `GET /v1/driver/jobs/{id}` and to nothing else.

**It is not a BFF tier.** `CLAUDE.md` is explicit that the Go platform owns the versioned public API
directly, and `Docs/10` §8.4 already allows a web surface its own server-side data access as an
application detail. This is one route, one method, one upstream path, no logic and no state: the
platform's status and its body, request id included, are what the browser receives.

It exists because **the service serves no CORS headers**, so a browser cannot send an
credential header to it cross-origin. It also has a property worth keeping if that changes: the
API's location is a **server** environment variable read per request rather than a `NEXT_PUBLIC_`
value inlined into a bundle at build time.

The narrowness is the security property. A `rewrites()` entry would have been three lines and would
have made this origin a credential-forwarding front door to the whole platform.

| Variable | Default | What |
|---|---|---|
| `SHIPPER_API_BASE_URL` | `http://localhost:8080` | Where the Go API is. Set it per worktree — `make run` in a worktree serves on that tree's `HTTP_PORT` |

Put it in `apps/driver-portal/.env.local`, which is git-ignored, or export it before `make web-dev`.

## Tests

```
pnpm --filter ./apps/driver-portal test
```

`node --test` over `lib/`. **There is no test framework in this application's dependencies and that
is deliberate**: Node 22 strips TypeScript types itself, so twenty-five tests cost no dependency, no
lockfile change and no build step. `tsconfig.json` sets `allowImportingTsExtensions` so a test can
import `./link.ts` by its real name.

**`make web-check` runs them**, second in `web-lint web-test web-build web-typecheck` — before the
build rather than after the type-check, because they cost a tenth of a second and depend on nothing
the build produces.

Two are worth knowing about.

`lib/one-job.test.ts` holds the property the whole surface rests on: **what leaves this portal names
the job in the URL, never the job in the token**. It opens a link whose path names one job and whose
credential grants another, and asserts on the requests that actually go out — through the route
handler, to a stand-in platform that performs SHIP-108's comparison itself. Deriving the identifier
from the token, anywhere in that chain, fails it. That mutation passed every test this application
had when SHIP-120 shipped, which is why the test exists; `Docs/11` §3 records it.

`lib/surface.test.ts` is the other. **The driver's token goes to one endpoint, and adding a second
place it could go is a failing test rather than a review comment**: the set of files that may make a
request, name a credential, or hold one is closed, and the test names the file when it grows. It
reads code with the comments stripped, because every doc comment here discusses the credential
header and `localStorage` at length. It reads *source*, which is a weaker kind of guard — the reason
`one-job.test.ts` asks what went out on the wire instead.

## The invariant that defines this surface

The driver's **job-scoped token** and the mobile **auth token** are separate systems.
Neither can be exchanged for the other. The driver token is signed, time-limited, and grants
access to **exactly one job** and nothing else (`Docs/06` §5.2, SHIP-107, SHIP-108).

A driver-portal session must never be able to reach another job, a user profile, or any
account surface — regardless of what is typed into the address bar. **The platform enforces that
and this portal does not**: a job the token does not cover is a `404` rendered honestly, never a
filter over a list and never a page that discloses some other job exists. Authorisation is the
platform's decision (`Docs/07` §3).

## What the page shows, and what it does not

Five fields, because five is what `GET /v1/driver/jobs/{id}` serves: the job, the assignment, the
driver's name, when they were assigned, and when the link stops working. **Every one is rendered.**
The endpoint's response is the platform's answer to what a driver may see, so a page that dropped a
field would take that decision back off the platform and a page that added one would invent it.

Absent, each for a reason recorded on the platform side: the driver's own mobile number, the job's
status, and **anything about money — ever**. The customer's budget is never exposed to a provider or
to their driver, in any form; neither are bids or negotiations.

**Absent and not yet decided: the pickup and drop-off, the goods, and who to call.** `Docs/03` §3
puts them in the driver's Prepare stage and the endpoint does not serve them. The page says so
rather than leaving a blank card. See `Docs/11` §3's SHIP-120 entry.

## Design constraints

- Used one-handed, outdoors, on a phone browser, often on poor signal. Large touch targets
  (SHIP-121).
- Photo proof is captured through the browser camera and uploaded **directly** to private object
  storage via a short-lived pre-signed URL — never proxied through the API (SHIP-122,
  `Docs/06` §5.2).
- The portal becomes read-only once the delivery is completed (SHIP-123).
- No web font is fetched. This is the surface most likely to be opened on a phone with two bars of
  signal.
