package main

import (
	"context"
	"errors"
	"fmt"
	"net/http"

	"github.com/google/uuid"

	"github.com/DulsaraNethmin/Shipper/services/core/internal/admin"
	"github.com/DulsaraNethmin/Shipper/services/core/internal/db"
	"github.com/DulsaraNethmin/Shipper/services/core/internal/jobs"
	"github.com/DulsaraNethmin/Shipper/services/core/internal/passwords"
	"github.com/DulsaraNethmin/Shipper/services/core/internal/ratelimit"
)

// The admin domain's routes (SHIP-163 onwards).
//
// This file exists so that adding a domain adds a file and edits none. cmd/api/routes.go,
// manifest.go and main.go are shared surfaces (Docs/10 §9.2); a route registration that had to go
// into one of them is a line every concurrent branch also touches, and a badly resolved conflict
// there drops an endpoint with no compile error and no failing test.
//
// # The first route in M6, and it is not an administrative one
//
// Docs/04 §7 has three stages and the first is a party to a delivery reporting that something went
// wrong. So this route declares **RequireUser**, and deliberately not RequireAdmin: the caller is a
// customer or a provider, authenticated the ordinary way. Administrator sign-in is a separate system
// a user token can never reach (SHIP-147), the guards map in routes.go has no entry for that class,
// and a route declaring it stops the process at startup rather than being served open — which is the
// right outcome and not one to work around. SHIP-164's administrative half arrives with the
// middleware.
//
// # The route is under /jobs and the handler is in admin, and that is not a contradiction
//
// A URL is a client's map of the product, not a diagram of the packages behind it. Reporting a
// problem is something a client does *to a job*, so it lives under the job — the same reading that
// puts SHIP-106's driver and SHIP-111's milestones there. What decides which domain serves it is who
// owns the rule, and the rules here are `admin`'s: Docs/04 §7 is the intake specification, Docs/04
// §5's sixth queue is where the result lands, and Docs/04 §9's controls govern what happens next.
// SHIP-82 settled the general form of this — a route is declared where its answer is decided, not
// where its path points.
// # The other three routes are the first in the service to declare RequireAdmin (SHIP-147)
//
// They land in the same change that fills `newAdminGuard` (adminauth.go), and that is not a
// convenience — SHIP-15r's seam makes it compulsory. A class with no guard is **absent** from the
// map rather than mapped to something that refuses, so a route declaring RequireAdmin before the
// guard exists stops the process at startup naming the class. Declaring the routes and filling the
// constructor are therefore one commit or a service that will not start.
func init() {
	register(
		Route{
			Method:  http.MethodPost,
			Pattern: "/jobs/{id}/disputes",
			Group:   GroupV1,

			// RequireUser. See the file header: the complainant is a customer or a provider,
			// and the administrative stages of Docs/04 §7 are a separate ticket behind a
			// separate credential.
			Auth:    RequireUser,
			Handler: func(d Deps) http.Handler { return adminHandler(d).RaiseDispute() },
		},

		Route{
			Method:  http.MethodPost,
			Pattern: "/admin/sessions",
			Group:   GroupV1,

			// Public, and on manifest_test.go's publicMutatingRoutes allow-list because of it.
			// An endpoint that hands out a credential cannot require one; what makes it safe
			// is that it is rate limited per account and per address, which is the property
			// every entry on that list shares.
			Auth:    Public,
			Handler: func(d Deps) http.Handler { return adminHandler(d).SignIn() },
		},

		Route{
			Method:  http.MethodDelete,
			Pattern: "/admin/sessions/current",
			Group:   GroupV1,
			Auth:    RequireAdmin,
			Handler: func(d Deps) http.Handler { return adminHandler(d).SignOut() },
		},

		Route{
			Method:  http.MethodGet,
			Pattern: "/admin/me",
			Group:   GroupV1,
			Auth:    RequireAdmin,
			Handler: func(d Deps) http.Handler { return adminHandler(d).Me() },
		},

		Route{
			Method:  http.MethodGet,
			Pattern: "/admin/moderation/exceptions",
			Group:   GroupV1,

			// RequireAdmin, and `moderation.read` inside the handler (SHIP-117, SHIP-148).
			// Every role holds that permission: reading a queue is what the least-privileged
			// role exists to be able to do.
			Auth:    RequireAdmin,
			Handler: func(d Deps) http.Handler { return adminHandler(d).ExceptionQueue() },
		},

		Route{
			Method:  http.MethodGet,
			Pattern: "/admin/users",
			Group:   GroupV1,

			// RequireAdmin is the credential; `users.read` is the permission, checked in the
			// handler (SHIP-148, SHIP-151). Every role holds it — searching is what the
			// least-privileged role exists to be able to do, and Docs/01 §4.6 lists it first.
			//
			// A collection under /admin rather than /users, because the shape is the
			// administrator's view of an account and not the account holder's. There is no
			// endpoint by which a user reads another user, and this is not one arrived at
			// through a different credential.
			Auth:    RequireAdmin,
			Handler: func(d Deps) http.Handler { return adminHandler(d).SearchUsers() },
		},

		Route{
			Method:  http.MethodPost,
			Pattern: "/admin/administrators",
			Group:   GroupV1,

			// RequireAdmin is the credential; `admins.manage` is the permission, and it is
			// checked in the handler rather than declared here (SHIP-148). The manifest's
			// auth class says which *credential system* serves a route, and there is no
			// fifth class per permission — twelve permissions would be twelve classes, and
			// guardsFor is finished.
			Auth:    RequireAdmin,
			Handler: func(d Deps) http.Handler { return adminHandler(d).CreateAdministrator() },
		},
	)
}

// adminHandler builds the domain's handler from what Deps already carries.
//
// Nothing admin needs is missing from Deps, which is the test Docs/10 §9.2 sets for whether a domain
// has been written the way the shared-surface rules ask: the job service, the outbox writer and the
// two ports are all pure functions of the pool, the clock and the configuration, so no field had to
// be added to a shared struct and no line to its literal in main.go.
//
// It panics for the same reason jobsHandler and deliveryHandler do: it runs during attach, at
// startup, and every failure it can report is a wiring mistake that will still be there after a
// restart. The pool is deliberately not checked — it may be nil because the database was unreachable
// at startup, which is a transient condition the service is built to survive, and the handlers
// answer 503 for as long as it lasts.
// newJobService is routes_delivery.go's, and it is shared rather than copied deliberately: it is
// already written as "a job service for a domain that needs to move a job but is not `jobs`", and a
// second constructor differing only in which file it sits in is a second place for the geocoder
// decision to be made differently.
// The argon2id profile is d.Config.Identity.Argon2, and that is deliberate rather than borrowed.
// SHIP-15r moved argon2id into `internal/passwords` precisely so that a second domain needing to
// hash a password would not be the reason for a second implementation, and a second *cost knob*
// would be the same mistake one level up: two profiles that agree by comment until somebody raises
// one. There is one platform password cost and this is where it is configured. **The field's name
// is now narrower than its meaning** — renaming it is a `internal/config` change and therefore a
// shared-surface request rather than something this ticket takes on its own.
func adminHandler(d Deps) *admin.Handler {
	svc := admin.NewService(disputeLifecycle{jobs: newJobService(d)}, jobPartiesLookup{}, d.Clock)

	hasher, err := passwords.NewHasher(passwords.Argon2Profile{
		MemoryKiB:   d.Config.Identity.Argon2.MemoryKiB,
		Iterations:  d.Config.Identity.Argon2.Iterations,
		Parallelism: d.Config.Identity.Argon2.Parallelism,
	})
	if err != nil {
		panic("cmd/api: admin password hasher: " + err.Error())
	}

	// The same prefix identity's limiter uses, so both live in one keyspace a person can scan;
	// the *keys* are prefixed `admin-signin:` by the domain, so an administrator's failures and a
	// user's failures against the same address are separate allowances.
	limiter, err := ratelimit.New(d.Redis, "rl:v1:", d.Clock)
	if err != nil {
		panic("cmd/api: admin rate limiter: " + err.Error())
	}

	// SHIP-150. Built from d.Clock rather than from a clock of its own, which is the whole of
	// Docs/11 §9's "one row, one clock": an audit entry has to sort against the session row and the
	// status-history row it describes, and both of those take this clock.
	auditor, err := admin.NewAuditor(d.Clock)
	if err != nil {
		panic("cmd/api: admin audit writer: " + err.Error())
	}

	creds, err := admin.NewCredentials(d.Pool, hasher, limiter, d.Clock, auditor)
	if err != nil {
		panic("cmd/api: admin credentials: " + err.Error())
	}

	moderation, err := admin.NewModeration(exceptionQueueLookup{}, d.Pool)
	if err != nil {
		panic("cmd/api: admin moderation: " + err.Error())
	}

	// SHIP-151. It takes the pool alone: the search reads `users`, which is a shared table this
	// domain may read directly, and there is no port to supply. See internal/admin/postgres_users.go
	// for why that is not the arrangement jobPartiesLookup and exceptionQueueLookup use.
	users, err := admin.NewUsers(d.Pool)
	if err != nil {
		panic("cmd/api: admin account search: " + err.Error())
	}

	handler, err := admin.NewHandler(svc, creds, moderation, users, d.Pool, d.Logger)
	if err != nil {
		panic("cmd/api: admin handler: " + err.Error())
	}
	return handler
}

// disputeLifecycle implements admin.Jobs over the guarded transition (SHIP-57).
//
// The same seam routes_delivery.go describes working as intended: `admin` names what it needs in its
// own ports.go, `jobs` knows nothing about `admin`, and the two are joined here by a type that
// translates one vocabulary into the other. Go satisfies the interface structurally, so neither
// package imports the other.
//
// # The translation is of errors into outcomes, and of a party into an actor
//
// admin cannot call errors.Is against jobs' sentinels — that is an import — so the refusals come
// back as an admin.JobMove instead. Everything jobs treats as a refusal becomes an outcome with a
// nil error; everything else stays an error, because a failing database is not an answer.
//
// The second translation is admin.Party into jobs.ActorType. Both packages need a word for "the
// customer" and neither may use the other's, so the mapping lives here — the composition root, where
// both vocabularies are visible. **This is the only place in the service that knows the two lists
// correspond.**
type disputeLifecycle struct {
	jobs *jobs.Service
}

// MoveToDisputed runs Docs/02 §2's `Awarded through Delivered → Disputed` through the one guarded
// function, inside the caller's transaction.
//
// The actor is the complainant, recorded as the side of the job they were on — which is what Docs/02
// §1 names as the primary actors for this status ("a customer, provider, or admin has raised an
// unresolved issue") and what job_status_history records.
//
// The reason is the dispute's category. Docs/01 §3 requires one only of an administrator, so this is
// not obligatory — it is here because the alternative is a job_status_history row saying a job froze
// and not saying why, when the answer was already in hand.
//
// RecordedAt is left zero, so jobs.Transition uses the platform's clock for both. That is correct
// here: the request is made online, by a person at a screen, so there is only one clock for the act
// of freezing the job. When the *incident* happened is a different fact and is recorded on the
// dispute, where it is not mistaken for the time somebody reported it.
func (l disputeLifecycle) MoveToDisputed(
	ctx context.Context,
	r db.Runner,
	jobID, actorID uuid.UUID,
	party admin.Party,
	reason string,
) (admin.JobMove, error) {
	actor, err := actorFor(party)
	if err != nil {
		return admin.JobMoveUnrecognised, err
	}

	_, err = l.jobs.Transition(ctx, r, jobs.Move{
		JobID:  jobID,
		To:     jobs.StatusDisputed,
		Actor:  jobs.User(actor, actorID),
		Reason: reason,
	})

	switch {
	case err == nil:
		return admin.JobMoved, nil
	case errors.Is(err, jobs.ErrJobNotFound):
		return admin.JobNotFound, nil
	case errors.Is(err, jobs.ErrAlreadyInStatus):
		return admin.JobAlreadyDisputed, nil
	case errors.Is(err, jobs.ErrTransitionNotPermitted):
		return admin.JobNotDisputable, nil
	default:
		return admin.JobMoveUnrecognised, err
	}
}

// actorFor maps the party a complainant was on to the actor kind job_status_history records.
//
// A switch rather than a cast, even though the strings are identical today. They are two closed
// lists owned by two packages — admin.Parties has two values and jobs.ActorTypes has five — and a
// cast would compile for ever after either list changed. This is the one place the correspondence is
// asserted, so it is the one place it can be wrong, and a value with no case is an error rather than
// a silently mistyped actor.
func actorFor(party admin.Party) (jobs.ActorType, error) {
	switch party {
	case admin.PartyCustomer:
		return jobs.ActorCustomer, nil
	case admin.PartyProvider:
		return jobs.ActorProvider, nil
	default:
		return "", fmt.Errorf("cmd/api: %q is not a party a transition can be attributed to", party)
	}
}

// jobPartiesLookup implements admin.JobParties by reading the job and its accepted bid.
//
// # Why this query is here and not in a domain
//
// It spans `jobs` and `bidding`, and `admin` may import neither. The composition root is where a
// dependency between domains is visible to anyone reading how the service is wired, rather than
// buried in `admin/postgres.go` where `jobs` and `bids` would read as tables admin owns.
//
// It is the same call routes_delivery.go's acceptedBids makes, and it will move for the same reason:
// when `bidding` grows a store at SHIP-92, the accepted-bid half of this statement becomes a method
// on it. The customer half belongs to `jobs`, and the join between them belongs here regardless.
//
// # 'Accepted' is the awarded provider, and the index is why that is safe to assume
//
// uq_bids_one_accepted_per_job is partial on `status = 'Accepted'`, so the LEFT JOIN cannot multiply
// rows however many bids a job carries (SHIP-80, SHIP-91). Docs/02 §3 — "awarding a job atomically
// marks one bid accepted and all others closed" — is the statement it enforces.
type jobPartiesLookup struct{}

// PartyOn is which side of the job this account is on, if either.
//
// No lock. The transition that follows in the same transaction takes the job row FOR UPDATE and
// re-reads its status, so a job that moved in the instant between the two statements is refused
// there rather than here.
//
// A job that does not exist and a job the caller has nothing to do with are the same answer, and
// admin turns both into one 404. See admin.JobParties.
//
// **The customer is checked before the provider, and on a job where one account is both, customer
// wins.** Nothing in the platform makes that possible today — ck_users_role fixes the role at
// registration and SHIP-45's trigger keeps it fixed — so the ordering is a decision recorded rather
// than one relied on.
func (jobPartiesLookup) PartyOn(
	ctx context.Context,
	r db.Runner,
	jobID, userID uuid.UUID,
) (admin.Party, bool, error) {
	const q = `
		SELECT CASE
		           WHEN j.customer_id = $2 THEN 'customer'
		           WHEN b.provider_id = $2 THEN 'provider'
		       END
		FROM jobs j
		LEFT JOIN bids b ON b.job_id = j.id AND b.status = 'Accepted'
		WHERE j.id = $1`

	var party *string
	err := r.QueryRow(ctx, q, jobID, userID).Scan(&party)
	switch {
	case errors.Is(err, db.ErrNoRows):
		// No such job. Deliberately the same answer as "not your job" below.
		return "", false, nil
	case err != nil:
		return "", false, fmt.Errorf("cmd/api: reading who is party to %s: %w", jobID, err)
	}
	if party == nil {
		return "", false, nil
	}
	return admin.Party(*party), true, nil
}

// Compile-time proof that the two adapters satisfy the ports admin declared, which is the only place
// in the build where that can be established — admin names neither type and neither type names
// admin, so nothing else links them.
var (
	_ admin.Jobs           = disputeLifecycle{}
	_ admin.JobParties     = jobPartiesLookup{}
	_ admin.ExceptionQueue = exceptionQueueLookup{}
)

// exceptionQueueLookup implements admin.ExceptionQueue over `delivery`'s evidence tables (SHIP-117).
//
// # Why the query is here rather than in a domain
//
// It reads `proofs` and `milestones`, which are `internal/delivery`'s, and `jobs`, which is
// `internal/jobs`'. `admin` may import neither, and the boundary lint refuses both. The composition
// root is where a dependency between domains is visible to somebody reading how the service is
// wired, rather than buried in `admin/postgres.go` where `proofs` would read as a table admin owns.
// jobPartiesLookup above is the same arrangement for the same reason.
//
// **`internal/delivery` is untouched by this ticket**, which is the property that made SHIP-117
// buildable at all in a wave where another track owns that package. The queue is a read of rows that
// domain already writes, and nothing about recording an exception changes.
//
// # The vocabulary is not translated, and that is deliberate
//
// `exception_reason`, `milestone` and `jobs.status` come back as the strings the database holds.
// Two of them are Docs/02's own values and the third is generated from `contracts/statuses.yaml`
// (SHIP-56a), so a translation here would be a third copy of a list whose whole point is that there
// is one. `delivery.ProofExceptionReason.Wire()` is the identity function on the stored form, and
// Docs/10 §4.7's mapping for the other two belongs to the endpoints that publish them.
type exceptionQueueLookup struct{}

// ExceptionsAwaitingReview reads one page of the queue, oldest first.
//
// # The ordering is total, and the cursor is why
//
// `(p.created_at, p.id)` rather than `p.created_at` alone: two deliveries recorded in the same
// millisecond would otherwise make a single-column cursor either skip an entry or repeat one, and a
// moderation queue that can hide a row is worse than one that shows it twice.
//
// `p.created_at` rather than `m.actor_recorded_at`, and that is the same decision Docs/02 §3.1
// records: the actor's clock is the driver's handset, which syncs late and can be wrong, and a queue
// ordered by it could be reordered by a device — putting an entry behind rows that arrived after it.
// The platform's clock is what a support target is measured against (Docs/04 §8).
//
// # Why the join does not filter on the milestone kind
//
// Docs/01 §4.4 is about delivery, and `proofs` does not restrict itself to one milestone — so
// filtering to 'Delivered' here would silently drop an exception recorded against a pickup the day
// somebody allows one. The milestone is *reported* instead, and a moderator sees which claim the
// exception stands behind. `idx_proofs_exception` is partial on `exception_reason IS NOT NULL`,
// which is the predicate that makes this cheap; the ordering is a sort over the few rows it selects.
func (exceptionQueueLookup) ExceptionsAwaitingReview(
	ctx context.Context,
	r db.Runner,
	q admin.QueueQuery,
) ([]admin.ExceptionEntry, error) {
	const query = `
		SELECT p.id, p.job_id, m.milestone, p.exception_reason,
		       coalesce(m.reason, ''), p.created_at, j.status
		FROM proofs p
		JOIN milestones m ON m.id = p.milestone_id
		JOIN jobs j       ON j.id = p.job_id
		WHERE p.exception_reason IS NOT NULL
		  AND ($1::timestamptz IS NULL OR (p.created_at, p.id) > ($1, $2))
		ORDER BY p.created_at, p.id
		LIMIT $3`

	// A nil rather than a zero time for the first page: `> (NULL, …)` is NULL rather than true,
	// so the predicate has to be skipped rather than satisfied, and the `$1 IS NULL` guard above
	// is what does it. Passing the zero time would work today and would stop working the first
	// time somebody backdated a fixture.
	var (
		after   any
		afterID any
	)
	if !q.After.Zero() {
		after, afterID = q.After.RecordedAt, q.After.ProofID
	}

	rows, err := r.Query(ctx, query, after, afterID, q.Limit)
	if err != nil {
		return nil, fmt.Errorf("cmd/api: reading the delivery-exception queue: %w", err)
	}
	defer rows.Close()

	var out []admin.ExceptionEntry
	for rows.Next() {
		var e admin.ExceptionEntry
		if err := rows.Scan(&e.ProofID, &e.JobID, &e.Milestone, &e.Reason,
			&e.Note, &e.RecordedAt, &e.JobStatus); err != nil {
			return nil, fmt.Errorf("cmd/api: reading a delivery-exception entry: %w", err)
		}
		out = append(out, e)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("cmd/api: reading the delivery-exception queue: %w", err)
	}
	return out, nil
}
