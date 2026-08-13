package main

import (
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/DulsaraNethmin/Shipper/services/core/internal/clock"
	"github.com/DulsaraNethmin/Shipper/services/core/internal/config"
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
// # What happens today
//
// It returns nil, nil. Nothing in `cmd/api` declares RequireAdmin — routes_admin.go's only route is
// `POST /v1/jobs/{id}/disputes`, which is RequireUser because the caller is a party to the job — so
// nothing is unserved by that, and a route that started declaring the class would stop the process
// rather than be served open.
func newAdminGuard(cfg *config.Config, pool *pgxpool.Pool, clk clock.Clock) (Guard, error) {
	// SHIP-147: build the administrator session verifier and return the Guard that enforces it.
	// Until then the class is unenforced and therefore unserved.
	_, _, _ = cfg, pool, clk
	return nil, nil
}
