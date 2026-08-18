# Rate limits

**SHIP-183.** Every endpoint on the route manifest has a limit chosen here, with the reason,
including the ones deliberately left unlimited. `Docs/01` states no rate-limiting requirement
anywhere, so these numbers are **decided** rather than derived, and this document is where the
reason lives. Changing a number without changing the reason beside it is how the reasons rot.

`Docs/09`'s **SHIP-183a** applies them. This document is what it transcribes.

## 1. What already exists, measured rather than assumed

**Five of the 86 routes carry a limit today, through three different mechanisms.** This is worth
stating precisely because `Docs/11` §6 recorded the figure as *"84 of the 86 routes need a number
nobody has decided"*, which counted only the routes using `internal/ratelimit` and missed two other
mechanisms. **The true figure is 81 unlimited**, and the difference is not arithmetic — it is that a
review grepping for `ratelimit.Bucket` finds barely half of what is already there.

| Route | Mechanism | Limit | Observable? |
|---|---|---|---|
| `POST /v1/auth/login` | `internal/ratelimit` token bucket, ×2 | account 5 / 2 min; address 30 / 20 s | 429 + `Retry-After` |
| `POST /v1/admin/sessions` | `internal/ratelimit` token bucket, ×2 | account 5 / 2 min; address 20 / 20 s | 429 + `Retry-After` |
| `POST /v1/auth/request-otp` | database issue limit, in the caller's transaction | 60 s cooldown; 5 per account per hour | **silent — 202 either way** |
| `POST /v1/auth/resend-verify` | database issue limit, in the caller's transaction | 60 s cooldown; 5 per account per hour | **silent — 202 either way** |
| `POST /v1/auth/verify-phone` | attempt counter on the code | 5 wrong codes retire the live one | typed error |

**`POST /v1/auth/register` has no limit of any kind.** It inserts a `users` row and sends a
verification email, unauthenticated, unbounded. That is an account-creation flood and an outbound
email flood on one endpoint, and the reputation cost of the second lands on the sending domain
rather than on the caller. **It is the single worst gap this review found** and §5 gives it a class.

### The three mechanisms are not interchangeable, and that shapes everything below

- **A bucket** bounds a *caller*. It answers 429 with an honest `Retry-After`, and it is the right
  control when being refused discloses nothing.
- **A silent issue limit** bounds a *destination*. It must not be observable: a 429 on
  `request-otp` would tell a caller that a number has an account and has been messaged recently,
  which is exactly what the always-202 design exists to prevent. This is a domain concern, written
  inside the transaction so two racing requests cannot both see an empty window.
- **An attempt limit** bounds guesses at *one artefact* and retires it. It is what makes a
  six-digit code defensible at all.

**A route may need more than one.** `POST /v1/auth/login` already carries two buckets;
`POST /v1/auth/request-otp` should carry a bucket *and* keep its silent destination limit. So the
model below assigns exactly one **bucket class** per route — which is what SHIP-183a's gate can
hold — and names the additional mechanism where one is owed.

## 2. Why classes rather than 86 numbers

Eighty-six independent pairs of numbers is not reviewable, and it drifts the first time somebody
adds a route by copying its neighbour. **Seven classes are.** A route belongs to a class, and the
class carries the reason; what a reviewer checks on a new route is one word.

It is also the only shape in which SHIP-183a's *Done when* is achievable — *"a route registered
without a limit fails a gate rather than defaulting to unlimited"* needs a field with no usable
zero value, and `Unlimited` has to be something somebody typed.

## 3. The classes

| Class | Keyed on | Capacity | Interval | Burst | Sustained | Refill from empty | Charged |
|---|---|---|---|---|---|---|---|
| `Unlimited` | — | — | — | — | unlimited | — | never |
| `Credential` | identifier **and** address | 5 / 30 | 2 min / 20 s | 5 / 30 | 30/h / 180/h | 10 min | **failures only** |
| `Message` | address | 10 | 5 min | 10 | 12/h | 50 min | every request |
| `Upload` | subject | 30 | 2 min | 30 | 30/h | 60 min | every request |
| `Write` | subject | 60 | 5 s | 60 | 720/h | 5 min | every request |
| `Read` | subject | 300 | 1 s | 300 | 3600/h | 5 min | every request |
| `PublicRead` | address | 600 | 1 s | 600 | 3600/h | 10 min | every request |

Sustained rate is one unit per interval; burst is the capacity; refill-from-empty is capacity ×
interval. Those three are not independent, which is the arithmetic §6 turns on.

**`Unlimited` — one route, `GET /health`.** A load balancer and a container orchestrator poll it,
and throttling it takes a *healthy* instance out of rotation — a limiter converted into an outage.
It is `operational` rather than `/v1`, carries no caller data, and discloses nothing. This is the
only route where unlimited is the right answer, and the reason is that the caller is the
infrastructure.

**`Credential` — routes that test a secret.** SHIP-47's figures, reused verbatim rather than
re-derived, because they were argued once and the argument holds: five is far past mistyping and
far short of useful guessing, and thirty per address sits above what an office or a carrier's NAT
looks like when several people get their passwords wrong. Charging **failures only** is what makes
this a control on guessing rather than a cap on how often somebody may sign in.

**The two existing address buckets disagree and this unifies them at 30.** `internal/identity`
uses 30 and `internal/admin` uses 20, and the tighter admin figure is the wrong direction. The
per-*account* bucket is the anti-guessing control and is identical at five in both; the address
bucket exists to bound somebody working through a list of accounts, and there are very few
administrators to work through. What the admin surface does have is an office sharing one address,
which argues for the looser figure rather than the tighter one. SHIP-183a changes the admin
constant to 30.

> **The per-identifier bucket applies only where a failed attempt leaves an identifier to key on.**
> On `verify-phone` it is the phone number and on `login` the email address. On
> `POST /v1/auth/verify-email` and `POST /v1/auth/refresh` the request carries only a
> high-entropy token, and a bucket keyed on a value that is different every attempt counts
> nothing. Those two routes take the **address bucket alone**, deliberately, and SHIP-183a must
> not silently key them on something that looks like an identifier.

**`Message` — routes that cause an email or an SMS.** The existing per-destination limits stop one
victim being flooded. They do **not** stop one caller walking a list of addresses, which burns the
sending reputation across thousands of destinations and enumerates accounts at the same time. The
address bucket is what bounds that, and it can be a visible 429 because refusing an address
discloses nothing about any account. Ten in a burst and one back every five minutes is far past
anybody registering, and far under what a list-walker needs.

**`Upload` — routes that mint a pre-signed URL.** The cost is bytes in the object store, and a
bucket refill does not give them back. Thirty covers a provider's four verification documents and a
job's proof set in one burst, with thirty an hour sustained after that.

**`Write` — authenticated state change.** Sixty in a burst is unreachable by a person and trivially
reachable by a retry loop, which is exactly the line this class is drawn on: it turns a runaway
client into a 429 instead of a load event. **It is not a control on consequence.** Account
deletion, awarding a bid and approving a suspension are all `Write`, and a bucket would not make
any of them safer — doing one of those once is the whole harm, and authorisation and the two-person
rule are what stand in front of them. A rate limit bounds volume; it has never bounded consequence.

**`Read` — authenticated read.** Three hundred absorbs a cold start where a client opens several
screens at once; 3600 an hour is well past any legitimate client and well under what makes scraping
worthwhile.

**`PublicRead` — unauthenticated read.** `GET /v1/app/policy` and `GET /v1/app/minimum-version` are
hit by **every** application launch, and the key is a network address, so one corporate NAT is one
caller. Deliberately the loosest class for that reason. The responses are static configuration, and
the honest control for volumetric abuse of static content is an edge cache rather than an
application bucket — which is a deployment decision, not this document's.

## 4. What every class shares

- **Fail closed.** `internal/ratelimit` reports `ErrUnavailable` rather than allowing, and the
  package note argues it: a limiter that fails open is one an attacker disables by making Redis
  unreachable. On the authentication surface the status is 503 rather than 429, because *"this is
  temporarily unavailable"* is true and *"you have done too much"* is not.
- **429 releases the idempotency key.** `httpx.Idempotent` already treats 429 like a 5xx and does
  not record it, so a throttled client that retries with the same key is not answered from a
  cached refusal. That is existing behaviour and SHIP-183a must not disturb it.
- **`Retry-After` is the time until the next token**, not until a window rolls over. The token
  bucket is what makes that figure honest, and it is why SHIP-47 chose one.

## 5. The assignment

All 86 routes on `services/core/cmd/api/routes_golden.txt`, generated from the manifest rather than
listed by hand. Counts: `Unlimited` 1, `Credential` 5, `Message` 3, `Upload` 3, `Write` 36,
`Read` 35, `PublicRead` 3.

| Method | Path | Auth | Class |
|---|---|---|---|
| GET | `/health` | public | `Unlimited` |
| POST | `/v1/account/deletion` | user | `Write` |
| POST | `/v1/admin/administrators` | admin | `Write` |
| GET | `/v1/admin/audit` | admin | `Read` |
| GET | `/v1/admin/disputes` | admin | `Read` |
| GET | `/v1/admin/disputes/{id}` | admin | `Read` |
| POST | `/v1/admin/disputes/{id}/resolution` | admin | `Write` |
| GET | `/v1/admin/jobs` | admin | `Read` |
| GET | `/v1/admin/jobs/{id}` | admin | `Read` |
| POST | `/v1/admin/jobs/{id}/unpublish` | admin | `Write` |
| GET | `/v1/admin/me` | admin | `Read` |
| GET | `/v1/admin/moderation/cancellations` | admin | `Read` |
| GET | `/v1/admin/moderation/exceptions` | admin | `Read` |
| GET | `/v1/admin/moderation/expiring-documents` | admin | `Read` |
| GET | `/v1/admin/notes` | admin | `Read` |
| POST | `/v1/admin/notes` | admin | `Write` |
| POST | `/v1/admin/sessions` | public | `Credential` |
| DELETE | `/v1/admin/sessions/current` | admin | `Write` |
| GET | `/v1/admin/suspensions` | admin | `Read` |
| POST | `/v1/admin/suspensions/{id}/approval` | admin | `Write` |
| GET | `/v1/admin/users` | admin | `Read` |
| POST | `/v1/admin/users/{id}/standing` | admin | `Write` |
| POST | `/v1/admin/users/{id}/suspension` | admin | `Write` |
| GET | `/v1/admin/verifications` | admin | `Read` |
| POST | `/v1/admin/verifications/{id}/decision` | admin | `Write` |
| GET | `/v1/admin/verifications/{id}/documents` | admin | `Read` |
| GET | `/v1/app/minimum-version` | public | `PublicRead` |
| GET | `/v1/app/policy` | public | `PublicRead` |
| POST | `/v1/auth/login` | public | `Credential` |
| POST | `/v1/auth/logout` | user | `Write` |
| POST | `/v1/auth/refresh` | public | `Credential` — address bucket only |
| POST | `/v1/auth/register` | public | `Message` |
| POST | `/v1/auth/request-otp` | public | `Message` + the existing silent destination limit |
| POST | `/v1/auth/resend-verify` | public | `Message` + the existing silent destination limit |
| GET | `/v1/auth/sessions` | user | `Read` |
| DELETE | `/v1/auth/sessions/{id}` | user | `Write` |
| POST | `/v1/auth/verify-email` | public | `Credential` — address bucket only |
| POST | `/v1/auth/verify-phone` | public | `Credential` + the existing attempt limit |
| GET | `/v1/driver/jobs/{id}` | driver-token | `Read` |
| GET | `/v1/driver/jobs/{id}/milestones` | driver-token | `Read` |
| POST | `/v1/driver/jobs/{id}/milestones` | driver-token | `Write` |
| POST | `/v1/driver/jobs/{id}/proof-uploads` | driver-token | `Upload` |
| GET | `/v1/fleet/bids` | user | `Read` |
| GET | `/v1/fleet/jobs` | user | `Read` |
| GET | `/v1/fleet/jobs/{id}` | user | `Read` |
| GET | `/v1/fleet/profile` | user | `Read` |
| PATCH | `/v1/fleet/profile` | user | `Write` |
| GET | `/v1/fleet/vehicles` | user | `Read` |
| POST | `/v1/fleet/vehicles` | user | `Write` |
| GET | `/v1/fleet/vehicles/{id}` | user | `Read` |
| PATCH | `/v1/fleet/vehicles/{id}` | user | `Write` |
| POST | `/v1/fleet/vehicles/{id}/deactivate` | user | `Write` |
| POST | `/v1/fleet/vehicles/{id}/reactivate` | user | `Write` |
| GET | `/v1/jobs` | user | `Read` |
| POST | `/v1/jobs` | user | `Write` |
| GET | `/v1/jobs/{id}` | user | `Read` |
| PATCH | `/v1/jobs/{id}` | user | `Write` |
| POST | `/v1/jobs/{id}/award` | user | `Write` |
| POST | `/v1/jobs/{id}/bids` | user | `Write` |
| GET | `/v1/jobs/{id}/bids/received` | user | `Read` |
| PATCH | `/v1/jobs/{id}/bids/{bid_id}` | user | `Write` |
| POST | `/v1/jobs/{id}/bids/{bid_id}/counter` | user | `Write` |
| GET | `/v1/jobs/{id}/bids/{bid_id}/history` | user | `Read` |
| GET | `/v1/jobs/{id}/bids/{bid_id}/messages` | user | `Read` |
| POST | `/v1/jobs/{id}/bids/{bid_id}/messages` | user | `Write` |
| POST | `/v1/jobs/{id}/bids/{bid_id}/withdraw` | user | `Write` |
| POST | `/v1/jobs/{id}/cancel` | user | `Write` |
| GET | `/v1/jobs/{id}/delivery/detail` | user | `Read` |
| GET | `/v1/jobs/{id}/delivery/milestones` | user | `Read` |
| GET | `/v1/jobs/{id}/delivery/proof` | user | `Read` |
| POST | `/v1/jobs/{id}/disputes` | user | `Write` |
| POST | `/v1/jobs/{id}/driver` | user | `Write` |
| POST | `/v1/jobs/{id}/driver/link` | user | `Write` |
| POST | `/v1/jobs/{id}/extend` | user | `Write` |
| GET | `/v1/jobs/{id}/history` | user | `Read` |
| POST | `/v1/jobs/{id}/milestones` | user | `Write` |
| POST | `/v1/jobs/{id}/proof-uploads` | user | `Upload` |
| POST | `/v1/notifications/device-tokens` | user | `Write` |
| DELETE | `/v1/notifications/device-tokens/current` | user | `Write` |
| GET | `/v1/notifications/preferences` | user | `Read` |
| PUT | `/v1/notifications/preferences` | user | `Write` |
| GET | `/v1/provider/verification` | user | `Read` |
| GET | `/v1/provider/verification/documents` | user | `Read` |
| POST | `/v1/provider/verification/documents` | user | `Write` |
| POST | `/v1/provider/verification/documents/uploads` | user | `Upload` |
| GET | `/v1/{$}` | public | `PublicRead` |

**`POST /v1/jobs/{id}/driver/link` is `Write` rather than `Message` on purpose.** It emits an
event; the outbound message is the notifications worker's, asynchronously. The per-destination
control for it therefore belongs in `internal/notifications` and not in a bucket on this route.

**The driver-token classes key on the job, not on a person.** A driver token grants access to
exactly one job, so the subject is the assignment — which means one job's driver cannot spend
another's allowance, and a leaked link is bounded to the job it was issued for.

## 6. The question `Docs/11` §9 parked on this row

> *A per-account limit is a lockout somebody else can trigger.* The bucket keys on the submitted
> address whether or not it has an account — it must, or never being throttled would itself
> disclose that an address is unknown. So a caller can spend somebody else's allowance. §9 named
> two ways out: a per-account limit counting **distinct addresses**, or one a **successful sign-in
> clears**.

**Decided: take "clears on success" now; hold "count distinct addresses" behind a named trigger.**

**"Clears on success" is worth exactly one thing, and it is quantifiable.** The bucket must still
be consulted *before* the password is checked — the tempting alternative, honouring a correct
password even while throttled, **destroys the limit entirely**: every wrong guess answers 429 and
the right one answers 200, so the attacker guesses indefinitely and the 429/200 split tells them
when they have won. With the check kept in front, a success can only clear a bucket that had a
token left. So the benefit is that the worst-case lockout falls from **capacity × interval to
interval — ten minutes to two** — because the victim needs one token rather than a full refill.
That is a real reduction for a few lines, and it does not disclose anything: a caller who just
signed in successfully already knows the account exists.

**It does not solve the problem**, and saying so is the honest version. Under sustained attack the
victim gets a two-minute window every two minutes instead of a ten-minute cycle. Better, not fixed.

**"Count distinct addresses" does solve it**, and it is declined for now on cost rather than on
merit. It needs a per-account **set** of addresses in Redis rather than a counter, and the size of
that set is chosen by the attacker — so the control against a distributed lockout is itself a
memory-pressure surface, which wants a bound and an eviction policy and its own reasoning. That is
a ticket, not a line.

**The trigger to build it is named** rather than left to judgement: the first credible report of an
account locked out by somebody else, **or** pilot telemetry showing account buckets emptied from
more than three distinct addresses. Either one means the distributed case has stopped being
theoretical, and at that point the set is worth its cost.

## 7. Whether limits belong in configuration

`internal/identity`'s constant block names this ticket as the one to decide it.

**Decided: the numbers stay in code, and configuration gets a global lever rather than per-route
overrides.**

Eighty-six routes is 172 environment variables, and `deploy/.env.example` documents every variable
this service reads. Nobody maintains 172 of them correctly. Worse, a per-route override is a number
that can drift from the reason written beside it here with nothing to notice — which is the exact
failure this document exists to prevent, reintroduced through the back door.

What an incident actually wants is not one route different from the rest. It is **"everything
tighter, now"** while something is being attacked, or **"looser, now"** because a real customer's
NAT is being throttled. Both are global.

**Two scalars, because one cannot do it.** Capacity governs the burst and interval governs the
sustained rate, and they are independent: scaling capacity alone moves what a cold start can absorb
and leaves the long-run rate untouched. So the lever is a burst scale and a rate scale, both
floating point, both defaulting to 1.0, applied to every class. **The exact shape is SHIP-183a's to
build** — this document owns the decision that it is global rather than per-route, and that the
default must be the numbers in §3.

**The revisit trigger:** the first incident that genuinely wants one route different from the rest.
At that point the answer is a per-**class** override map, not a per-route one — seven values a
person can hold in their head, against eighty-six they cannot.

## 8. The gate SHIP-183a inherits

**Eleven of the 86 routes key on a network address, and none of their limits can be trusted behind
a load balancer until the trusted-proxy configuration exists.** `Docs/11` §9 records that
`X-Forwarded-For` is deliberately unread and parks the decision on "the deployment work". This
review turns that from a note into a countable dependency.

The five `Credential`, three `Message` and three `PublicRead` routes are the eleven. With
`X-Forwarded-For` unread behind a proxy, every request presents the balancer's address, so all
eleven share one bucket per class and the first deployment throttles the entire world at the first
caller. Applying these limits **before** a trusted-proxy hop count or CIDR allow-list exists takes
the blast radius of that gap from the two routes that have it today to eleven.

**The other 75 routes are unaffected by the proxy**, because a user id, an administrator id and a
driver token's job all survive one untouched. **That is the scheduling consequence worth carrying
forward:** SHIP-183a can ship those limits immediately and must hold the eleven behind the
deployment work. Splitting it that way is a smaller change and a safer one, and it is the reason
this section exists rather than a sentence in §3.

> **Corrected by SHIP-183a: 74 of those 75 take a bucket, not all 75.** The count is 86 − 11, which
> sweeps `GET /health` in with them — and `/health` is `Unlimited`, so it is keyed on nothing and
> enforces its class by having none. The sentence above is right about the proxy and off by one
> about the work. **SHIP-183a's *Done when* inherited the same figure**, so it reads "each of the 75
> routes" against 74 that carry a limiter; §5's table is what was transcribed and it is the
> authority. Worth stating rather than quietly fixing, because the next person to recount will get
> 74 and needs to know which of the two figures was wrong.

## 9. What SHIP-183a decided, which this document left open

Three questions were named here as SHIP-183a's to answer. They are recorded here rather than only in
the code, because the next change to any of them is a change to this document's reasoning.

**One bucket per class per caller, not one per route.** §3's figures are budgets for a *kind* of
work, and this document already said so where it justified `Upload`: thirty "covers a provider's four
verification documents **and** a job's proof set in one burst", which are two routes sharing one
allowance. Per-route buckets would silently multiply every figure by the number of routes in its
class — 36 × 60 writes in a burst rather than 60 — and §7's conclusion that an override belongs at
the class rather than the route is the same judgement reached from the other end. The key is
`route:<class>:<caller>`.

**The caller is `httpx.SubjectScope`, which is narrower than §5's wording.** §5 says the driver
classes key on "the job" and the rest on "the subject". An account holder is keyed on the account,
exactly as written. The other two credential systems are keyed on the **credential**, because
`internal/admin`'s and `internal/delivery`'s grant accessors are unexported — deliberately, so that
nothing outside those domains can read a grant at all — and exporting them for a rate-limit key would
undo a separation `CLAUDE.md` names as an invariant.

That is strictly tighter for the driver: one token is one job, so two links for one job are two
buckets rather than one, and a leaked link is still bounded to the job it was issued for. **It
loosens exactly one thing and the cost is worth naming rather than burying — an administrator who
signs in again gets a fresh allowance**, because a new session is a new credential. That bypass is
self-limiting: a sign-in costs an argon2id derivation and is itself `Credential`-classed, so it is a
slower way to spend an allowance than waiting for the refill.

**The limiter runs inside the auth guard, and this is the load-bearing one.** These classes key on
the authenticated caller, and there is no caller until the guard has run. Wrapped the other way
round, every unauthenticated request keys on the same value — `route:<class>:anonymous` — and the
first attacker to empty that bucket refuses every anonymous request to every route in the class. A
limiter converted into a denial of service is worse than no limiter. The consequence to state rather
than discover: **traffic the guard refuses is not counted at all**, and bounding that is what §8's
address-keyed classes are for.

**The lever is two floating-point scalars, `RATE_LIMIT_BURST_SCALE` and `RATE_LIMIT_RATE_SCALE`**,
both defaulting to 1.0, both bounded 0.01–100, applied to every class. Neither can reach zero: a
class keeps a floor of one token and one millisecond, so these are a dial and not an off switch.

## 10. Revisit triggers, collected

| Trigger | What changes |
|---|---|
| An account reported locked out by a third party, or buckets emptied from >3 distinct addresses | Build the distinct-address per-account limit (§6) |
| An incident wanting one route different from the rest | A per-**class** override map, never per-route (§7) |
| The trusted-proxy configuration landing | The eleven address-keyed routes become enforceable (§8) |
| The first route whose cost is neither a caller, a destination nor an artefact | A fourth mechanism, and this document gains a section rather than a class |
