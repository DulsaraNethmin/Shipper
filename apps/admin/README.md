# apps/admin — Next.js administration panel

The privileged support and moderation interface (`Docs/06` §2). Separate from the customer
and provider experience by design.

## Where it is

**SHIP-22** landed the Next.js project: App Router, TypeScript in strict mode, Tailwind with
shadcn/ui, inside the pnpm workspace at the repository root (`Docs/10` §8.4).

**SHIP-188a** put a door on it. An administrator signs in, the panel holds the session, and the
shell renders who they are and what their role holds. The screens behind it are SHIP-188b (user and
job search), SHIP-188c (job detail and its audit trail) and SHIP-188d (the verification queue and
its decision) — the three `Docs/01` §8's demonstration walks through.

```
make web-dev app=admin     run it at http://localhost:3001
make web-check             lint, test, build, type-check — what CI runs
```

It needs the platform running (`make run`) and at least one administrator. There is no endpoint
that creates the first one — that would be an authenticated administrator endpoint with nobody to
authenticate — so it comes from an `INSERT`; `scripts/verify/90-admin.sh` has the fixture and the
development password hash it is written with.

`SHIPPER_API_BASE_URL` says where the platform is, read on the server per request. It defaults to
`http://localhost:8080`, which is what `make run` serves in a primary tree — **a worktree with its
own `HTTP_PORT` has to set it**, and so does every deployment (SHIP-189).

## How the session is held, and why it is not the driver portal's answer

The panel's token is set as an **httpOnly cookie by a route handler**. It is in no response body,
no browser storage, and no script-readable cookie — `lib/surface.test.ts` holds all three as
properties of the tree rather than as habits.

The driver portal keeps its token in `sessionStorage` and bans cookies outright. That is the right
answer there and the wrong one here, and the difference is the asset: a driver's token grants **one
delivery** and is already in the URL they were sent, while an administrator's session reaches every
user's contact details, every job, the verification evidence and the append-only audit trail. A
credential JavaScript can read is a credential any injected script can read.

**The cost of a cookie is that the browser attaches it to requests this origin did not make**, which
is cross-site request forgery. It is paid twice: `SameSite=Strict`, and an `Origin` check in every
mutating route handler (`lib/origin.ts`). `Secure` is set for every host that is not recognisably
local, so a deployment cannot lose it by forgetting to configure something.

**Signing out ends the session at the platform**, not just here — `DELETE
/v1/admin/sessions/current` deletes the row, and `internal/admin/adminauth.go` re-reads that row on
every request, so there is no window in which the credential still works. The cookie is cleared only
when the platform confirms the session is over; a failed sign-out leaves an administrator signed in
and says so, rather than telling them they are out of a session that is still live.

## The shape of the outbound hop

The Go service serves no CORS headers, so a browser cannot call it directly from this origin — a
preflight `OPTIONS` reaches nothing that answers one. Each upstream call therefore goes through a
server-side file on this origin, and there is **one file per endpoint, each naming one literal path**:

| File | Reaches |
|---|---|
| `app/api/admin/sessions/route.ts` | `POST /v1/admin/sessions` |
| `app/api/admin/sessions/current/route.ts` | `DELETE /v1/admin/sessions/current` |
| `lib/administrator.ts` | `GET /v1/admin/me` |

**There must never be a shared `forward(path, …)` helper here.** One `fetch` whose destination is an
argument is a general, credential-forwarding front door however narrow its callers are today, and
the door this one would open is onto twenty-four privileged endpoints. `lib/surface.test.ts` fails
if any file names more than one upstream path, so the table above is checked rather than documented.

This is **not** a BFF (`Docs/06` §2.1). `Docs/10` §8.4 allows a web surface its own server-side data
access as an application detail; each of these is one method, one path, no logic and no vocabulary
of its own.

## Things that are already decided

- Admin authentication is **independent of user authentication**. An admin surface can never be
  reached with a user token (SHIP-147).
- Permissions are granular and default to the minimum (SHIP-148). The panel reads them to hide and
  disable and **never to decide** — `Docs/07` §3. Every check that matters is the platform's, on
  every request, out of the session row.
- Every privileged mutation writes an append-only audit entry. Ordinary administrators cannot delete
  one (SHIP-149, SHIP-150).
- Verification evidence renders through short-lived signed URLs and every view is access-logged
  (SHIP-155). The panel keeps neither a URL nor an object key (SHIP-188d).
- A customer's budget appears on no admin screen (`Docs/01` §4.3). It is held server-side by
  `TestTheAdminJobShapesCarryNothingPrivate`; the screens are the second place to check.

## Tests

`node --test` over `lib/`, which needs no framework, no build and no install — Node 22 strips the
types itself. `make web-check` runs them.

**Until SHIP-188a there was no `test` script and `pnpm -r run test` silently skipped this package**
— a test nobody runs is worse than one that does not exist, because it is counted. `mk/web.mk`
records the same position.

| File | Holds |
|---|---|
| `lib/surface.test.ts` | The credential is unreachable from JavaScript; each file names one endpoint; every route handler checks the origin |
| `lib/sessions.test.ts` | What the route handlers actually put on the wire, with the platform stood in for |
| `lib/cookie.test.ts` | The attributes of the `Set-Cookie` value itself, and the origin comparison |
