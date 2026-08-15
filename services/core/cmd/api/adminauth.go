package main

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/DulsaraNethmin/Shipper/services/core/internal/admin"
	"github.com/DulsaraNethmin/Shipper/services/core/internal/clock"
	"github.com/DulsaraNethmin/Shipper/services/core/internal/config"
	"github.com/DulsaraNethmin/Shipper/services/core/internal/httpx"
)

// The composition root for an administrator's session (SHIP-147), written before the ticket.
//
// # This file is the seam, and driverauth.go is the worked example it copies
//
// `cmd/api/routes.go`, `manifest.go` and `main.go` are shared surfaces (Docs/10 §9.2), so the admin
// track cannot map its own auth class: RequireAdmin is declared in manifest.go because the manifest
// has to be able to express it, and it is mapped in guardsFor, which is a file a domain branch may
// not edit. That made SHIP-147 unbuildable from a domain branch for six consecutive waves — §8 has
// carried it as the last live gate in that section since SHIP-108 cleared the driver's.
//
// SHIP-15m proved the shape on the other guard and SHIP-108 filled it without editing one shared
// file. This is the same seam for the same reason, and SHIP-147 fills the body below.
//
// # Absent is not the same as refusing
//
// Returning nil leaves RequireAdmin **out** of the guard map, and attach panics at startup for any
// route declaring it. That is deliberate, and guardsFor argues it in full: a class mapped to
// something that refuses everything is a route answering 401 forever, indistinguishable to every
// client from an expired credential. A process that will not start is the loudest available way of
// saying "this auth class is not implemented yet", and it cannot reach production.
//
// So `internal/admin` may declare a RequireAdmin route in routes_admin.go **only in the change that
// fills this function**. Declaring one before that is a `make run` that dies at startup naming the
// class, which is the seam working rather than a defect.
//
// # What SHIP-147 must not do
//
// **It must not produce an [authctx.Subject].** The rule is CLAUDE.md's, written for the driver
// token and true here for the same reason: an administrator session and a mobile session are
// separate systems, and resolving one into the type the other uses is an exchange one helper
// function later. `internal/identity` refuses any role but the two `ck_users_role` permits
// (ErrInvalidRole names this ticket), so the platform already declines to issue a user token to an
// administrator; this is the other direction.
//
// **The grant belongs to internal/admin**, not to internal/authctx and not to internal/httpx.
// Infrastructure imports neither a domain nor an adapter (SHIP-15c), and every domain imports
// httpx — so one import of `admin` from inside it welds all eight to admin through an edge that is
// in no domain's own files. delivery's driverauth.go is the precedent: the grant type is exported,
// its context key and accessor are not, and "delivery is the only domain that serves a driver-token
// route" is therefore a fact about the build rather than a rule somebody follows.
//
// # Why three parameters, all unused
//
// The same argument driverauth.go makes, and it has been paid once already: SHIP-15m gave
// newDriverTokenGuard a keyset and a clock it did not use, SHIP-108 used both, and main.go was not
// in that ticket's diff. A signature that changes when the body is filled puts the shared file back
// in the diff, which is the whole of what this seam is for.
//
// The three are what an administrator session can plausibly be built from. cfg carries whatever
// SHIP-147 adds to internal/config — signing material if the session is a token, a lifetime either
// way. clk is how any expiry is decided, and the injectable clock is what makes it testable.
//
// **pool is the one that is not on driverauth.go's list, and it is here because the two credentials
// are unlike each other.** A driver token is verified from key material alone: it is signed, it
// names its one job, and nothing is read to check it. An administrator session is far more likely
// to be a row — Docs/04 §4 wants sessions an administrator can be signed out of, and CLAUDE.md's
// audit invariant wants the acting administrator identified on every entry, neither of which a
// stateless token gives you without a second mechanism. So the pool is offered rather than
// withheld, and SHIP-147 may ignore it.
//
// **It may be nil**, and a guard built here must survive that. main.go warns and starts when
// PostgreSQL is unreachable, because a rolling deployment during a failover must not take every
// instance down at once — so the closure holds a pool that may be nil for a while and answers 503
// while it is, exactly as every handler built from Deps.Pool already does.
//
// # Why an error rather than a panic
//
// Also the same as newAccessTokenAuthenticator and newDriverTokenGuard: a keyset or a configuration
// value that cannot be read is a fault that will still be there after every restart, and a service
// that came up unable to verify any administrator session would answer 401 to every administrator
// while reporting itself healthy. It refuses to start instead, beside the other configuration
// failures.
//
// # What it does now (SHIP-147), and which of the three parameters it turned out to need
//
// Two statements: build the session verifier over the pool and the clock, and hand back the
// middleware `internal/admin` declares. Nothing about an administrator session is decided in this
// package — not the credential's shape, not its lifetime, not what a verified session grants, and
// not what a refusal answers with. That all lives in internal/admin, beside the sign-in that has to
// agree with it.
//
// **cfg is unused, and that is the design rather than an omission.** The paragraph above offered it
// for "signing material if the session is a token, a lifetime either way", and SHIP-147 chose
// neither: the credential is an opaque random value stored hashed in `admin_sessions`, so there is
// no key material to configure, and the two lifetimes are constants in `internal/admin` for the
// reason SHIP-39's TTL and SHIP-47's limits are — `internal/config` is a shared surface (Docs/10
// §9.2) and four tracks were open. **This ticket asked nothing of internal/config.**
//
// The parameter stays because removing it would put main.go back in the diff, which is the one
// thing this seam exists to prevent. It is what a lifetime or a key would arrive through if either
// is ever wanted.
//
// **The seam held.** `cmd/api/routes.go`, `manifest.go`, `main.go` and `Deps` are untouched by
// SHIP-147: filling this body is what maps RequireAdmin, and `routes_admin.go` — the admin track's
// own file — is what declares the first routes on it.
//
// # What SHIP-147b added, and why it is a second field rather than a second parameter
//
// An administrator's idempotency key landed in `idem:v1:anonymous:<key>`, because
// `httpx.SubjectScope` reads an `authctx.Subject` and an administrator deliberately produces none.
// The guard below cannot fix that: it runs **per route, inside** `httpx.Idempotent`, and the scope
// has been computed before it ever sees the request. Docs/11 §9 named the mechanism that does —
// a second group-wide resolver, outside `Idempotent`, which `httpx.ResolvePrincipal` now is — and
// this file is where the closure over `internal/admin` is supplied.
//
// So this constructor returns **two** things, and it returns them in one struct rather than
// growing `newRouter` a sixth parameter. That is not tidiness. `newRouter` has fifteen call sites
// across five test files, and `main.go` is a shared surface: bundling means the return type and
// the parameter type change together, `main.go` compiles untouched, and every test still says
// `testAdminGuard()`. SHIP-108's measured cost for the *other* seam was fifteen mechanical edits
// in a diff that also contained a token verifier, and Docs/11 §9 records that as the thing this
// arrangement exists to avoid.
type adminAuth struct {
	// Guard enforces the RequireAdmin auth class, per route, from the manifest.
	Guard Guard

	// Scope names the administrator an idempotency key belongs to, group-wide.
	//
	// **It resolves the session a second time**, and that is a real cost stated rather than
	// hidden: the guard resolves it too, so an administrative write does two indexed reads of
	// `admin_sessions` instead of one. The alternative is for `internal/admin` to export a way
	// of putting a grant on its own context key so the guard could reuse this one — which
	// would export exactly the thing adminauth.go keeps unexported so that no other package
	// can hold an administrator grant. One extra read on the writes only (the resolver is
	// lazy — see httpx.ResolvePrincipal) is the cheaper side of that trade.
	//
	// `Authenticator.slide` is granularity-limited, so the pair produces at most one write.
	Scope httpx.PrincipalResolver
}

func newAdminGuard(cfg *config.Config, pool *pgxpool.Pool, clk clock.Clock) (adminAuth, error) {
	// cfg is deliberately unused. See the note above; an administrator session has no signing
	// key and no configured lifetime, and the parameter is kept so that main.go stays out of the
	// next diff that needs one.
	_ = cfg

	verifier, err := admin.NewAuthenticator(pool, clk)
	if err != nil {
		return adminAuth{}, fmt.Errorf("administrator session verifier: %w", err)
	}

	return adminAuth{
		// admin.RequireAdmin returns a func(http.Handler) http.Handler, which is Guard's
		// underlying type — so no conversion and, more to the point, no second declaration of
		// the middleware shape in a package every domain can see.
		Guard: admin.RequireAdmin(verifier),

		// The scope is the **administrator**, not the session, which is the same choice
		// httpx.SubjectScope makes for a user: one person signed in twice has performed one
		// action, and idempotency answers whether the action has happened. A session
		// identifier would make one administrator's second browser a different caller.
		//
		// An error is "not an administrator credential" and nothing more — httpx tries the
		// next resolver and then falls back to the anonymous scope. It is deliberately not
		// distinguished here: a lapsed session, a revoked one and a user's access token all
		// mean the same thing to a namespace, and the *route* is where a refusal belongs.
		Scope: func(ctx context.Context, credential string) (httpx.Principal, error) {
			grant, err := verifier.Resolve(ctx, credential)
			if err != nil {
				return httpx.Principal{}, err
			}
			return httpx.Principal{
				Kind: httpx.PrincipalAdmin,
				ID:   grant.Administrator.ID.String(),
			}, nil
		},
	}, nil
}
