package main

import (
	"context"
	"fmt"
	"net/http"

	"github.com/google/uuid"

	"github.com/DulsaraNethmin/Shipper/services/core/internal/config"
	"github.com/DulsaraNethmin/Shipper/services/core/internal/db"
	"github.com/DulsaraNethmin/Shipper/services/core/internal/identity"
	"github.com/DulsaraNethmin/Shipper/services/core/internal/passwords"
	"github.com/DulsaraNethmin/Shipper/services/core/internal/platform/email"
	"github.com/DulsaraNethmin/Shipper/services/core/internal/platform/sms"
	"github.com/DulsaraNethmin/Shipper/services/core/internal/ratelimit"
)

// The identity domain's routes (SHIP-30 onwards).
//
// This file exists so that adding a domain adds a file and edits none. cmd/api/routes.go,
// manifest.go and main.go are shared surfaces (Docs/10 §9.2); a route registration that had to
// go into one of them is a line every concurrent branch also touches, and a badly resolved
// conflict there drops an endpoint with no compile error and no failing test.
//
// # Why the public routes here are public
//
// Every route that is, is how a caller obtains their own credentials — the only justification
// cmd/api's publicMutatingRoutes allow-list accepts. Registration takes a password and returns an
// account; sign-in takes a password and returns a session; the verification endpoints take a token
// or a code that was sent to the contact details being proved. None of them can require the
// credential they exist to produce.
//
// They are still behind httpx.Idempotent like every other state-changing request, and they are
// still rate limited — by their own issue rules today (SHIP-34) and by SHIP-47's token bucket
// across the whole authentication surface.
//
// # And why sign-out is not
//
// POST /v1/auth/logout is the first RequireUser route in the service (SHIP-43). The session it
// ends is the one named by the token being presented, so the credential is not merely a
// permission check — it is the whole input. See the note on Handler.Logout.
func init() {
	register(
		Route{
			Method:  http.MethodPost,
			Pattern: "/auth/register",
			Group:   GroupV1,
			Auth:    Public,
			Limit:   LimitMessage,
			Handler: func(d Deps) http.Handler { return identityHandler(d).Register() },
		},
		Route{
			// Public, and the shortest justification on the allow-list: this is the
			// endpoint that produces the credential every protected route requires
			// (SHIP-41).
			Method:  http.MethodPost,
			Pattern: "/auth/login",
			Group:   GroupV1,
			Auth:    Public,
			Limit:   LimitCredential,
			Handler: func(d Deps) http.Handler { return identityHandler(d).Login() },
		},
		Route{
			// The first route in the service that requires a credential (SHIP-43). SHIP-44
			// built the middleware; nothing had used it until now.
			Method:  http.MethodPost,
			Pattern: "/auth/logout",
			Group:   GroupV1,
			Auth:    RequireUser,
			Limit:   LimitWrite,
			Handler: func(d Deps) http.Handler { return identityHandler(d).Logout() },
		},
		Route{
			// The device list and its revoke (SHIP-46). Read-only and state-changing on
			// one resource, so the pair is a GET and a DELETE rather than two POSTs.
			Method:  http.MethodGet,
			Pattern: "/auth/sessions",
			Group:   GroupV1,
			Auth:    RequireUser,
			Limit:   LimitRead,
			Handler: func(d Deps) http.Handler { return identityHandler(d).Devices() },
		},
		Route{
			Method:  http.MethodDelete,
			Pattern: "/auth/sessions/{id}",
			Group:   GroupV1,
			Auth:    RequireUser,
			Limit:   LimitWrite,
			Handler: func(d Deps) http.Handler { return identityHandler(d).RevokeDevice() },
		},
		Route{
			Method:  http.MethodPost,
			Pattern: "/auth/verify-email",
			Group:   GroupV1,
			Auth:    Public,
			Limit:   LimitCredential,
			Handler: func(d Deps) http.Handler { return identityHandler(d).VerifyEmail() },
		},
		Route{
			Method:  http.MethodPost,
			Pattern: "/auth/resend-verify",
			Group:   GroupV1,
			Auth:    Public,
			Limit:   LimitMessage,
			Handler: func(d Deps) http.Handler { return identityHandler(d).ResendVerification() },
		},
		Route{
			Method:  http.MethodPost,
			Pattern: "/auth/request-otp",
			Group:   GroupV1,
			Auth:    Public,
			Limit:   LimitMessage,
			Handler: func(d Deps) http.Handler { return identityHandler(d).RequestOTP() },
		},
		Route{
			Method:  http.MethodPost,
			Pattern: "/auth/verify-phone",
			Group:   GroupV1,
			Auth:    Public,
			Limit:   LimitCredential,
			Handler: func(d Deps) http.Handler { return identityHandler(d).VerifyPhone() },
		},
		Route{
			// Public because it is called by exactly the client whose access token has
			// just expired (SHIP-42). Requiring one would lock that client out of the
			// endpoint that replaces it — which is the reason SHIP-44 split subject
			// resolution from subject enforcement in the first place.
			Method:  http.MethodPost,
			Pattern: "/auth/refresh",
			Group:   GroupV1,
			Auth:    Public,
			Limit:   LimitCredential,
			Handler: func(d Deps) http.Handler { return identityHandler(d).Refresh() },
		},
		Route{
			// Account deletion (SHIP-169), and the first route this domain serves that is
			// not under /auth.
			//
			// The prefix is a claim about what the resource is. Everything under /auth is
			// how a caller obtains, holds or ends a *credential*; asking to be deleted is
			// an act on the account itself, which outlives every session it ever had. It
			// is also where SHIP-170's deferral and SHIP-173's screen will read from, and
			// a person cancelling a deletion is not signing anything out.
			//
			// RequireUser, and it may be: SHIP-44 supplies the idempotency middleware's
			// scope, so a key on this route lands in idem:v1:user:<id>:<key> rather than
			// the anonymous namespace CLAUDE.md's hard gate names.
			Method:  http.MethodPost,
			Pattern: "/account/deletion",
			Group:   GroupV1,
			Auth:    RequireUser,
			Limit:   LimitWrite,
			Handler: func(d Deps) http.Handler { return identityHandler(d).RequestAccountDeletion() },
		},
	)
}

// identityHandler builds the domain's handler from what Deps already carries.
//
// # Why this is built here rather than added to Deps
//
// Deps and its literal in main.go are the shared surfaces wave 2's three tracks would otherwise
// each grow a field on, and TestDepsCarriesExactlyWhatIsDeclared exists to make that a decision
// with a name attached. Nothing identity needs is missing: a hasher, a service and a handler are
// all pure functions of the pool, the clock and the configuration.
//
// # Why it panics
//
// It runs during attach, at startup, from a route's Handler function. Every failure it can
// report is a configuration or wiring mistake that will still be there after a restart — an
// argon2 profile outside the range this package will run, a nil clock. A service that came up
// serving registration with no password hasher would accept a password and store something
// nobody can verify against, which is worse than not starting.
//
// The pool is deliberately *not* checked. It may be nil because the database was unreachable at
// startup, which is a transient condition the service is built to survive (see the note on
// Deps); the handlers answer 503 for as long as it lasts.
func identityHandler(d Deps) *identity.Handler {
	hasher, err := passwords.NewHasher(passwords.Argon2Profile{
		MemoryKiB:   d.Config.Passwords.Argon2.MemoryKiB,
		Iterations:  d.Config.Passwords.Argon2.Iterations,
		Parallelism: d.Config.Passwords.Argon2.Parallelism,
	})
	if err != nil {
		panic("cmd/api: identity password hasher: " + err.Error())
	}

	// The keyset and the issuer are built here rather than carried on Deps, which is the
	// pattern the note on Deps describes: both are pure functions of the configuration, so
	// neither is a reason to grow the shared struct. newRouter builds a *verifier* over the
	// same keys and passes it to the middleware — two objects over one keyset, because issuing
	// needs the active key and a TTL while verifying needs the whole set and no TTL.
	keys, err := identity.NewKeyset(d.Config.Identity.AccessTokenKeys, d.Config.Identity.AccessTokenActiveKID)
	if err != nil {
		panic("cmd/api: identity keyset: " + err.Error())
	}

	issuer, err := identity.NewAccessTokenIssuer(keys, d.Config.Identity.AccessTokenTTL, d.Clock)
	if err != nil {
		panic("cmd/api: identity access token issuer: " + err.Error())
	}

	// SHIP-47's token bucket, over the same Redis the idempotency store uses. d.Redis may be
	// nil — the process starts with an unreachable cache deliberately — and the limiter then
	// refuses every attempt, which is the fail-closed direction internal/ratelimit argues for.
	// The prefix keeps these keys distinguishable from the idempotency store's in one database.
	limiter, err := ratelimit.New(d.Redis, "rl:v1:", d.Clock)
	if err != nil {
		panic("cmd/api: identity rate limiter: " + err.Error())
	}

	svc, err := identity.NewService(d.Pool, hasher, issuer, limiter,
		newEmailSender(d.Config), newSMSSender(d.Config), activeJobLookup{}, d.Clock)
	if err != nil {
		panic("cmd/api: identity service: " + err.Error())
	}

	handler, err := identity.NewHandler(svc, d.Logger)
	if err != nil {
		panic("cmd/api: identity handler: " + err.Error())
	}
	return handler
}

// newEmailSender picks the email implementation for this environment (SHIP-32, SHIP-31).
//
// This is the composition root doing the one thing only it can: identity declares what it needs
// of a sender in its own ports.go and imports nothing from internal/platform/email, and the
// adapter knows nothing about identity. Go satisfies the interface structurally, and the two
// meet here (Docs/06 §4.1).
//
// email.UseConsole owns the rule rather than a switch written here, and it leans towards the
// console for anything it does not recognise. Choosing wrongly towards the console costs a
// developer a puzzled minute; choosing wrongly towards the provider sends real email from a
// machine that should never have had the credential.
//
// A staging or production deployment with no provider configured stops the process, and that is
// the correct direction. The alternative is a service that registers accounts, reports success,
// and silently sends no verification message — so every account it creates is one nobody can
// finish setting up, discovered a day later by the people who signed up.
func newEmailSender(cfg *config.Config) identity.EmailSender {
	if email.UseConsole(cfg.Env) {
		return email.NewConsole()
	}

	sender, err := email.NewProvider(email.Options{
		BaseURL: cfg.Email.ProviderBaseURL,
		APIKey:  cfg.Email.ProviderAPIKey,
		Sender:  cfg.Email.Sender,
	})
	if err != nil {
		panic("cmd/api: email provider: " + err.Error() +
			" — set EMAIL_PROVIDER_BASE_URL, EMAIL_PROVIDER_API_KEY and EMAIL_SENDER, or run " +
			"with SHIPPER_ENV=development to log messages to the console instead")
	}
	return sender
}

// newSMSSender picks the SMS implementation for this environment (SHIP-35, SHIP-34).
//
// The same shape as newEmailSender and the same reasoning, with one difference in emphasis:
// sms.UseConsole leaning towards the console matters more here, because a message costs money
// per send and wakes a real handset belonging to whoever last used that number for testing.
func newSMSSender(cfg *config.Config) identity.SMSSender {
	if sms.UseConsole(cfg.Env) {
		return sms.NewConsole()
	}

	sender, err := sms.NewProvider(sms.Options{
		BaseURL: cfg.SMS.ProviderBaseURL,
		APIKey:  cfg.SMS.ProviderAPIKey,
		Sender:  cfg.SMS.Sender,
	})
	if err != nil {
		panic("cmd/api: sms provider: " + err.Error() +
			" — set SMS_PROVIDER_BASE_URL, SMS_PROVIDER_API_KEY and SMS_SENDER, or run with " +
			"SHIPPER_ENV=development to log messages to the console instead")
	}
	return sender
}

// activeJobLookup implements identity.ActiveJobs over `jobs` and `bids` (SHIP-170).
//
// # Why the query is here rather than in the domain
//
// It reads `jobs`, which is `internal/jobs`', and `bids`, which is `internal/bidding`'s, and
// `internal/identity` may import neither — the boundary lint refuses both directions. The
// composition root is where a dependency between domains is visible to somebody reading how the
// service is wired, rather than buried in `internal/identity/postgres.go` where `jobs` and `bids`
// would read as tables identity owns. jobPartiesLookup in routes_admin.go is the same arrangement
// for the same reason, and this statement is that one's join with a status predicate added.
//
// **`internal/jobs` and `internal/bidding` are untouched by this ticket**, which is the property
// that made SHIP-170 buildable in a wave where neither package is anybody's: the deferral is a read
// of rows those domains already write, and nothing about creating, awarding or moving a job
// changes.
type activeJobLookup struct{}

// activeJobStatuses is Docs/02 §1's six committed statuses, in the document's own order.
//
// **Written out rather than expressed as a complement, and that is the same decision
// overduePickupStatuses records.** "Not Draft, Open, Negotiating, Completed, Cancelled or Disputed"
// would be six exclusions doing the work of six inclusions and would silently absorb a thirteenth
// status added to Docs/02 §1 later — into the *deferring* set, which is the direction that quietly
// stops people being deleted. Naming them means a new status is deliberately absent until somebody
// decides it belongs.
//
// The range is Docs/05 §3.1's own — "a request made between Awarded and Delivered" — and it is
// inclusive at both ends. `Delivered` is in it because the delivery is not finished until the job
// is `Completed` (Docs/02 §6.1's 72-hour auto-complete, or the customer confirming): a job sitting
// at `Delivered` can still move to `Disputed`, and erasing a party at that moment is precisely what
// strands the counterparty. `Disputed` is deliberately *not* in it — a dispute is not a delivery in
// flight, it is one that has stopped, and SHIP-164 is what ends it.
//
// A single-line constant, so that TestTheActiveStatusesAreTheOnesTheLifecycleDeclares can read this
// file and resolve the concatenation by name.
const activeJobStatuses = `('Awarded', 'Driver assigned', 'En route to pickup', 'Picked up', 'In transit', 'Delivered')`

// HasActiveJob reports whether this account is party to a job in flight, on either side.
//
// # Both parties, and the OR is the whole ticket
//
// Docs/05 §3.1: "Erasing a party mid-delivery would strand the counterparty." A *party* is the
// customer whose goods are moving (`jobs.customer_id`) or the provider whose offer was accepted
// (`bids.provider_id` where the bid is `Accepted`), and the deletion endpoint is `RequireUser` with
// no role predicate, so both halves of the marketplace reach it. A query that checked only the
// customer would pass every customer-side assertion and delete a driver mid-delivery.
//
// `uq_bids_one_accepted_per_job` is partial on `status = 'Accepted'`, so the LEFT JOIN cannot
// multiply rows however many bids a job carries (SHIP-80, SHIP-91) — the same property
// jobPartiesLookup relies on. `EXISTS` rather than a count: the question is yes or no, and the
// planner can stop at the first row.
//
// No lock and no ordering. The answer is a snapshot, and identity's own note on the port says why
// that is enough: the state is re-read every time the request is touched rather than recorded once.
func (activeJobLookup) HasActiveJob(ctx context.Context, r db.Runner, userID uuid.UUID) (bool, error) {
	const q = `
		SELECT EXISTS (
		       SELECT 1
		         FROM jobs j
		         LEFT JOIN bids b ON b.job_id = j.id AND b.status = 'Accepted'
		        WHERE j.status IN ` + activeJobStatuses + `
		          AND (j.customer_id = $1 OR b.provider_id = $1))`

	var active bool
	if err := r.QueryRow(ctx, q, userID).Scan(&active); err != nil {
		return false, fmt.Errorf("cmd/api: reading whether %s is carrying a delivery: %w", userID, err)
	}
	return active, nil
}

// Compile-time proof that the adapter satisfies the port identity declared, which is the only place
// in the build where that can be established — identity names no type here and this type names no
// domain but identity, so nothing else links them.
var _ identity.ActiveJobs = activeJobLookup{}
