package main

import (
	"context"
	"errors"
	"fmt"
	"net/http"

	"github.com/google/uuid"

	"github.com/DulsaraNethmin/Shipper/services/core/internal/bidding"
	"github.com/DulsaraNethmin/Shipper/services/core/internal/db"
	"github.com/DulsaraNethmin/Shipper/services/core/internal/events"
	"github.com/DulsaraNethmin/Shipper/services/core/internal/fleet"
	"github.com/DulsaraNethmin/Shipper/services/core/internal/jobs"
)

// The bidding domain's routes (SHIP-84 onwards).
//
// This file exists so that adding a domain adds a file and edits none. cmd/api/routes.go,
// manifest.go and main.go are shared surfaces (Docs/10 §9.2); a route registration that had to go
// into one of them is a line every concurrent branch also touches, and a badly resolved conflict
// there drops an endpoint with no compile error and no failing test.
//
// # The route is under /jobs and the handler is in bidding, which is now the third time
//
// A URL is a client's map of the product, not a diagram of the packages behind it. Placing a bid is
// something a provider does *to a job*, so it lives under the job — the same reading that put
// SHIP-106's driver and SHIP-111's milestones there, and the same reading that put SHIP-82's
// provider feed in routes_fleet.go. What decides which domain serves a route is who owns the rule,
// and the rules here are bidding's: one live offer per provider per job, and what an offer has to
// contain before anybody can act on it.
//
// `/v1/jobs/{id}/bids` is a collection under a job, which is what it will keep being: SHIP-102's
// customer comparison is a `GET` on this same path, and SHIP-101's provider list is a different
// resource — the caller's own bids across every job — rather than a filter on this one.
//
// # Why the route requires a user and not a role
//
// The bidder is whoever the token says is calling; there is no field in the request for naming
// somebody else, so the endpoint has no meaning without a credential. **The role is not enforced
// here**, for the reason routes_fleet.go gives about vehicles and for one more: whether this account
// may bid is not a role question at all. It is Docs/01 §4.3's four filters — service area, vehicle
// capability, verification state, job status — which `fleet` answers from the database. A customer
// presenting a valid token passes RequireUser and is refused by that filter, with the same 404 an
// ineligible provider gets.
//
// It is also what makes the route safe under SHIP-44's scoped idempotency: keys land in
// idem:v1:user:<subject>:<key> rather than the shared anonymous namespace, which is the gate
// CLAUDE.md holds authenticated state-changing endpoints behind and which SHIP-44 closed.
//
// # A bid is addressed under the job it was placed on, and that is one address rather than two
//
// SHIP-85 and SHIP-86 need a *member* of the collection above, and the alternative was a flat
// `/v1/bids/{id}`. It is rejected because SHIP-102's customer comparison is a `GET` on
// `/v1/jobs/{id}/bids` — so the collection is already under the job, and a member addressed anywhere
// else would be one resource with two URLs. That is the shape where a permission check gets added to
// one address and forgotten on the other.
//
// The cost is that both handlers compare the bid's `job_id` to the path and refuse a mismatch, which
// is a real check rather than a formality: without it the first half of the URL would be decorative
// and a client pairing a real bid with any job at all would succeed.
//
// **Withdrawal is a verb and not a `DELETE`**, for the reason `POST /v1/fleet/vehicles/{id}/deactivate`
// is one: nothing is deleted. Docs/01 §4.3 requires every withdrawal to be recorded and Docs/02 §4
// keeps the chain readable, so the row survives at `Withdrawn`. It is not a `PATCH` writing a status
// either — a bid's status is the platform's, and a request naming one is refused by the decoder.
//
// # Countering is a verb under the offer being answered, and one route serves both parties
//
// SHIP-87. Docs/02 §4 gives the two directions in two sentences that describe one act — "a customer
// counter-offer supersedes the prior provider offer. A provider counter-offer supersedes the prior
// customer offer" — so there is one endpoint and the platform works out which side the caller is on.
// Two routes would have been two authorisation rules to keep in step, and they would have differed
// the first time one of them was corrected.
//
// It names the offer being answered rather than posting to the collection, because a counter *answers
// a particular offer*: two counters against one offer are then a race the platform can see, rather
// than two independent creations that only collide at an index.
//
// **`RequireUser` and not a role, and this is where that choice pays.** The two callers are told apart
// by the database — `bids.provider_id` for the provider, `jobs.customer_id` for the customer — and a
// role claim in a token is evidence about the token rather than the fact. A customer whose token
// claims `provider` is still recognised as the customer of their own job, which is what CLAUDE.md
// means by no authorisation decision on the device.
//
// # The provider's own bids are under `/v1/fleet`, which is the one route here not under a job
//
// SHIP-101a, and it is the shape this file predicted at SHIP-84: "SHIP-101's provider list is a
// different resource — the caller's own bids across every job — rather than a filter on this one".
// `/v1/fleet` is where a provider's own things already live — their vehicles, their service area,
// their profile — and a provider asking what they have bid on is asking about their operation rather
// than about any one job.
//
// **It also sidestepped `net/http`'s routing constraint rather than working around it.** A
// four-segment `GET /v1/jobs/{id}/<literal>` panicked the mux at registration while
// `GET /v1/jobs/open/{id}` existed, so a provider list under the job tree would have had to take a
// fifth segment or a different verb. SHIP-83a has since removed that constraint by moving the feed
// to `/v1/fleet/jobs/{id}`; this resource does not belong under a job anyway, which was always the
// happier of the two reasons and is now the only one.
//
// `RequireUser` and not a role, for the fifth time. The list is scoped to the caller's own id in the
// `WHERE` clause, so a customer's answer is an empty page by construction rather than by permission —
// there is no parameter that widens it and nothing to refuse.
//
// # And the history is a `GET` under the same offer
//
// SHIP-88's "full chain remains readable". It is deliberately **not** `GET /v1/jobs/{id}/bids`, which
// this file reserved for SHIP-102's customer comparison at SHIP-84 and which is a different resource
// with a different privacy rule — every provider's offer side by side, where this is one negotiation's.
// Read-only, so the idempotency middleware lets it through untouched and it carries no key.
//
// # The award is a verb on the **job**, and the bid it accepts travels in the body
//
// SHIP-92, and it is the one route here that does not sit under an offer. Docs/09's *Done when* names
// `POST /v1/jobs/{id}/award`, and the shape follows what the act is: the job moves — exactly once,
// through the guarded transition — and the bid is what the customer selected. `withdraw` and
// `counter` are acts on an offer and leave the job where it is; this is the other kind, and putting
// it under `/bids/{bid_id}` would have made the two look alike.
//
// **`RequireUser` and not a role, for the fourth time and with the most riding on it.** Docs/02 §3
// gives the award to the customer alone, and which account that is is a column — `jobs.customer_id` —
// rather than a claim in a token. A provider presenting a token that says `customer` is refused by
// the database with the 404 a job that does not exist gets.
func init() {
	register(
		Route{
			Method:  http.MethodPost,
			Pattern: "/jobs/{id}/award",
			Group:   GroupV1,
			Auth:    RequireUser,
			Handler: func(d Deps) http.Handler { return biddingHandler(d).Award() },
		},
		Route{
			Method:  http.MethodPost,
			Pattern: "/jobs/{id}/bids",
			Group:   GroupV1,
			Auth:    RequireUser,
			Handler: func(d Deps) http.Handler { return biddingHandler(d).Place() },
		},
		Route{
			Method:  http.MethodPatch,
			Pattern: "/jobs/{id}/bids/{bid_id}",
			Group:   GroupV1,
			Auth:    RequireUser,
			Handler: func(d Deps) http.Handler { return biddingHandler(d).Revise() },
		},
		Route{
			Method:  http.MethodPost,
			Pattern: "/jobs/{id}/bids/{bid_id}/withdraw",
			Group:   GroupV1,
			Auth:    RequireUser,
			Handler: func(d Deps) http.Handler { return biddingHandler(d).Withdraw() },
		},
		Route{
			Method:  http.MethodPost,
			Pattern: "/jobs/{id}/bids/{bid_id}/counter",
			Group:   GroupV1,
			Auth:    RequireUser,
			Handler: func(d Deps) http.Handler { return biddingHandler(d).Counter() },
		},
		Route{
			Method:  http.MethodGet,
			Pattern: "/jobs/{id}/bids/{bid_id}/history",
			Group:   GroupV1,
			Auth:    RequireUser,
			Handler: func(d Deps) http.Handler { return biddingHandler(d).History() },
		},
		Route{
			Method:  http.MethodGet,
			Pattern: "/fleet/bids",
			Group:   GroupV1,
			Auth:    RequireUser,
			Handler: func(d Deps) http.Handler { return biddingHandler(d).Mine() },
		},
		Route{
			Method: http.MethodGet,

			// **`/jobs/{id}/bids` was the intended pattern, it is what Docs/09's SHIP-102a row
			// names, and it could not be served when this route was written.** Three comments
			// in this file reserved it for SHIP-102 from SHIP-84 onwards. The reservation was
			// made before SHIP-83 declared `GET /jobs/open/{id}`, and the two could not coexist:
			// `open` was a literal in the `{id}` position, so both patterns matched
			// `/v1/jobs/open/bids` with neither more specific, and Go's ServeMux panics at
			// registration rather than choosing. Measured then against Go's own mux, five ways:
			//
			//	GET /jobs/open/{id} + GET  /jobs/{id}/bids                   PANIC
			//	GET /jobs/open/{id} + GET  /jobs/{id}/offers                 PANIC — renaming does not help
			//	GET /jobs/open/{id} + GET  /jobs/{id}/bids + /jobs/open/bids PANIC — the overlap cannot be resolved
			//	GET /jobs/open/{id} + POST /jobs/{id}/bids                   ok    — why the POST exists and no GET did
			//	GET /jobs/open/{id} + GET  /jobs/{id}/bids/received          ok    — five segments do not overlap four
			//
			// The third line is the one worth recording, because it is the escape hatch
			// somebody will reach for: registering the intersection does **not** teach the mux
			// which pattern wins. Go has no such rule.
			//
			// # The constraint is gone and the path stays where it is
			//
			// Two options were open: **insert a segment**, which is SHIP-115's precedent —
			// `/jobs/{id}/delivery/detail`, `/delivery/milestones` and `/delivery/proof` are all
			// shaped by this same collision — or **move `/jobs/open/{id}`**, the structural fix
			// that frees the whole `GET /v1/jobs/{id}/<literal>` space. This route took the
			// first because the second needed `contracts/paths/fleet.yaml`,
			// `internal/fleet/http_test.go` and the fleet verify section, three files that
			// branch did not own. **SHIP-83a has since taken the second**: the feed is
			// `GET /v1/fleet/jobs/{id}`, and cmd/api/routes_jobsegment_test.go demonstrates the
			// four-segment space is free by registering one.
			//
			// **This path is not moving to `/jobs/{id}/bids` now that it could.** It is a
			// published endpoint the Flutter client already calls, and Docs/06 §5.3 is why that
			// settles it — old builds persist on devices indefinitely and Dart has no
			// over-the-air update path, so a URL that has shipped is a contract with clients
			// nobody can reach. `received` also earns its segment on the merits; see below.
			// **What SHIP-83a changes is the next ticket's options, not this one's.**
			//
			// # Why `received` rather than a shelf like delivery's
			//
			// Delivery took `/delivery/<thing>` because it had three reads to place and expected
			// more. This has one, and the collection it names is already `bids` — so a shelf
			// would read `/jobs/{id}/bids/bids`. `received` says which side of the negotiation
			// is asking, which is the whole difference between this endpoint and
			// `GET /v1/fleet/bids`: one is the offers a customer has received, the other is the
			// offers a provider has made.
			//
			// It cannot collide with a future `GET /jobs/{id}/bids/{bid_id}` either. A literal
			// is strictly more specific than a wildcard in the same position, so Go prefers this
			// and registers both — measured, not assumed.
			Pattern: "/jobs/{id}/bids/received",
			Group:   GroupV1,

			// RequireUser, and the role is not enforced here for the reason every route in this
			// file gives. **The access control is the job's ownership, decided in the domain**:
			// `bidding.Service.Offers` asks `jobs` whether this account is the job's customer
			// before it reads a single `bids` row, and a provider — including one bidding on
			// this very job — gets the byte-identical 404 a stranger and a non-existent job get.
			// That is a clause of SHIP-102a's *Done when* rather than a consequence, and it is
			// the one endpoint in this domain where a competing provider could otherwise read
			// every rival's price on a job in one request.
			Auth:    RequireUser,
			Handler: func(d Deps) http.Handler { return biddingHandler(d).Received() },
		},
	)
}

// biddingHandler builds the domain's handler from what Deps already carries.
//
// Nothing bidding needs is missing from Deps: the clock and the pool, which is the same short list
// fleet needs. That is the test Docs/10 §9.2 sets for whether a domain has been written the way the
// shared-surface rules ask — no field had to be added to a shared struct and no line to its literal
// in main.go.
//
// It panics for the same reason jobsHandler does: it runs during attach, at startup, and every
// failure it can report is a wiring mistake that will still be there after a restart. The pool is
// deliberately not checked — it may be nil because the database was unreachable at startup, which is
// a transient condition the service is built to survive, and the handlers answer 503 for as long as
// it lasts.
//
// # This is where two domains meet, and the meeting costs one argument
//
// `bidding` needs to know whether a provider may bid on a job, and that is `fleet`'s answer:
// SHIP-81's eligibility filter is one SQL predicate over four tables, and its own header names
// SHIP-84 as the reader it built `EligibleFor` for. `bidding` declares what it needs in its own
// ports.go and imports no domain; `fleet` knows nothing about `bidding`; the two are joined here,
// which is the only place they may be (Docs/06 §4.1, CLAUDE.md).
//
// **The eligibility port needed no adapter type, and that is worth noticing rather than assuming.**
// routes_delivery.go has two — `jobLifecycle` and `acceptedBids` — because a transition has four
// outcomes that cannot be carried across without naming a `jobs` sentinel. That port answers a bool,
// which has no vocabulary to translate, so `*fleet.Service` satisfies it structurally and the wiring
// is one argument and the assertion below.
//
// The other two do need types, and for two different reasons: `negotiatedJobs` translates `jobs`'
// sentinels into bools (SHIP-87), and `awardableJobs` carries an outcome and takes a lock (SHIP-92).
//
// A second `fleet.Service` rather than the one `fleetHandler` builds, for the reason
// `newJobService` builds a second `jobs.Service`: both are pure functions of the clock, neither
// holds state, and sharing one would be a dependency between two route files that today know nothing
// about each other.
func biddingHandler(d Deps) *bidding.Handler {
	svc := bidding.NewService(
		events.NewOutbox(),
		fleet.NewService(d.Clock),
		negotiatedJobs{jobs: newJobService(d)},
		awardableJobs{jobs: newJobService(d)},
		presentedJobs{jobs: newJobService(d)},
		offerableVehicles{fleet: fleet.NewService(d.Clock)},
		offerorDirectory{fleet: fleet.NewService(d.Clock)},
		d.Clock,
	)

	handler, err := bidding.NewHandler(svc, d.Pool, d.Logger)
	if err != nil {
		panic("cmd/api: bidding handler: " + err.Error())
	}
	return handler
}

// negotiatedJobs implements bidding.Negotiation over the customer's own view of a job (SHIP-87).
//
// The second seam this file joins, and the first that needed a type. `fleet`'s answer is a bool with
// no vocabulary to translate, so `*fleet.Service` satisfies its port structurally; `jobs` answers with
// a `Job` and two sentinels, and turning those into two bools is exactly the translation
// routes_delivery.go's `jobLifecycle` does for a transition. Neither package names the other.
//
// # Both methods go through jobs.Service.Job, which is the ownership check itself
//
// That method reads the row and compares `customer_id` in Go, raising `jobs.ErrNotJobOwner` for
// somebody else's job and `jobs.ErrJobNotFound` for nobody's. **Both become `false` here**, which is
// the same collapse `bidding` makes on the wire and `fleet` makes for eligibility: distinguishing them
// would take a second query whose only product is the knowledge that some identifier exists.
//
// Anything else stays an error, because a failing database is not an answer.
type negotiatedJobs struct {
	jobs *jobs.Service
}

// CustomerOf reports whether this account owns this job, whatever its status.
//
// Status is deliberately not consulted. SHIP-88 keeps a negotiation readable to its parties after the
// job is awarded, cancelled or completed — a record is at its most useful once the work is over.
func (n negotiatedJobs) CustomerOf(ctx context.Context, r db.Runner, userID, jobID uuid.UUID) (bool, error) {
	_, err := n.jobs.Job(ctx, r, userID, jobID)
	return n.answer(err)
}

// AwardableBy reports whether this customer's job could still be awarded.
//
// # `jobs.Permitted(status, Awarded)` rather than a list of statuses, and that is the whole point of
// this method
//
// Docs/02 §2's transition table is the authority, `jobs` holds it in exactly one place and exports the
// question, and `Open` and `Negotiating` are the two statuses that can reach `Awarded`. Writing those
// two strings here instead would have put a **third** copy of that list in the service — after `jobs`'
// own table and `fleet.biddableStatuses` — and a third copy is the one nobody remembers to correct.
//
// It also states the rule in the form the product actually has it. A customer countering is not asking
// whether the job is biddable; they are asking whether this negotiation can still end in an award. The
// two select the same statuses today and they are different questions, and this one is the one whose
// answer follows Docs/02 §2 automatically if that document ever changes.
func (n negotiatedJobs) AwardableBy(
	ctx context.Context,
	r db.Runner,
	customerID, jobID uuid.UUID,
) (bool, error) {
	job, err := n.jobs.Job(ctx, r, customerID, jobID)
	if owned, err := n.answer(err); err != nil || !owned {
		return false, err
	}
	return jobs.Permitted(job.Status, jobs.StatusAwarded), nil
}

// answer is the translation, in one place: what `jobs` treats as a refusal becomes `false` with a nil
// error, and everything else stays an error.
//
// Written once rather than twice for the reason `jobLifecycle.move` is written once — a third method
// added later must not be able to treat `jobs.ErrNotJobOwner` differently from these two, and that
// drift would be invisible: both methods would still compile, still pass their own tests, and answer
// a stranger with a refusal on one path and a 500 on the other.
func (negotiatedJobs) answer(err error) (bool, error) {
	switch {
	case err == nil:
		return true, nil
	case errors.Is(err, jobs.ErrNotJobOwner), errors.Is(err, jobs.ErrJobNotFound):
		return false, nil
	default:
		return false, fmt.Errorf("cmd/api: reading a job for a counter-offer: %w", err)
	}
}

// awardableJobs implements bidding.Awarding, which is the port the award writes through (SHIP-92).
//
// # Why the lock is a statement here rather than a method on jobs.Service
//
// `jobs` has `lockJob` and it is unexported, reachable only from its own `Transition`. Exporting it
// would mean editing another domain from a branch that owns `bidding`, and it would widen that
// domain's surface for one caller. So the statement is written here, in the composition root — the
// same call `acceptedBids` in routes_delivery.go makes in the opposite direction, with the same
// reasoning recorded there: this is where a dependency between two domains is *visible to anybody
// reading how the service is wired*, rather than buried in a domain's postgres.go where it would read
// as a table that domain owns.
//
// **The trade is deliberate and it is not free.** Two statements now know that a job has a
// `customer_id` and a `status`: this one, and `jobs`' own store. What keeps them honest is that
// nothing here interprets the status — `jobs.Permitted` is asked, which is Docs/02 §2's table in the
// one place that holds it — and that the transition itself still goes through the guarded function
// below. When `jobs` grows an exported lock for a caller in another domain, this method becomes a
// call to it and nothing else changes.
//
// # It takes the lock even to refuse
//
// A read that answered "awardable" and let go would be answering about a job that can be cancelled,
// disputed or awarded to somebody else before `bidding` writes anything. Docs/11 §3's SHIP-88 entry
// puts this first in the ordering for that reason: it is the outermost lock, and the only one shared
// with the status guard and `job_status_history`.
type awardableJobs struct {
	jobs *jobs.Service
}

// LockForAward holds the job row and reports whether this customer may still award it.
//
// The two refusals are deliberately different values rather than one. "Not yours or no such job"
// leads a client to a 404 and "the job has moved on" to a 409 naming what became of it, and a bool
// would have made `bidding` choose one answer for both — see bidding.JobAward.
//
// `customer_id` is compared here rather than folded into the `WHERE` clause, which is the same choice
// `jobs.Service.Job` makes and for the same reason: a query scoped to the caller that returns nothing
// cannot say whether the job was somebody else's or nobody's, and only one of those is a caller
// probing for identifiers. Both are one answer on the wire; keeping them apart costs a comparison and
// buys a fact a test can assert.
func (a awardableJobs) LockForAward(
	ctx context.Context,
	r db.Runner,
	customerID, jobID uuid.UUID,
) (bidding.JobAward, error) {
	const q = `SELECT customer_id, status FROM jobs WHERE id = $1 FOR UPDATE`

	var (
		owner  uuid.UUID
		status jobs.Status
	)
	switch err := r.QueryRow(ctx, q, jobID).Scan(&owner, &status); {
	case errors.Is(err, db.ErrNoRows):
		return bidding.JobAwardNoSuchJob, nil
	case err != nil:
		return bidding.JobAwardUnrecognised, fmt.Errorf("cmd/api: holding %s for award: %w", jobID, err)
	}

	if owner != customerID {
		return bidding.JobAwardNoSuchJob, nil
	}

	// jobs.Permitted rather than a list of statuses written here. Docs/02 §2's table is authoritative
	// and `jobs` holds it in exactly one place; the two statuses that can reach 'Awarded' today are
	// Open and Negotiating, and writing them out would be a copy that stops agreeing with the
	// document the first time somebody corrects one of them. negotiatedJobs.AwardableBy makes the
	// same call for the counter's version of this question.
	if !jobs.Permitted(status, jobs.StatusAwarded) {
		return bidding.JobAwardNotPermitted, nil
	}
	return bidding.JobAwardable, nil
}

// MoveToAwarded runs Docs/02 §2's `Open / Negotiating → Awarded` through the one guarded function,
// inside the caller's transaction.
//
// The actor is the **customer**, which is Docs/02 §3's first transition control — "only the customer
// can award a job" — and what `job_status_history` records. No reason: Docs/01 §3 requires one only
// of an administrator.
//
// RecordedAt is left zero, so jobs.Transition uses the platform's clock for both. An award is made
// online, by a customer looking at the screen, so there is only one clock — unlike a milestone, where
// routes_delivery.go carries the actor's separately.
//
// The translation is of errors into outcomes, exactly as `jobLifecycle.move` does it: everything
// `jobs` treats as a refusal becomes an outcome with a nil error, and everything else stays an error
// because a failing database is not an answer.
func (a awardableJobs) MoveToAwarded(
	ctx context.Context,
	r db.Runner,
	jobID, customerID uuid.UUID,
) (bidding.JobAward, error) {
	_, err := a.jobs.Transition(ctx, r, jobs.Move{
		JobID: jobID,
		To:    jobs.StatusAwarded,
		Actor: jobs.User(jobs.ActorCustomer, customerID),
	})

	switch {
	case err == nil:
		return bidding.JobAwarded, nil
	case errors.Is(err, jobs.ErrJobNotFound):
		return bidding.JobAwardNoSuchJob, nil
	case errors.Is(err, jobs.ErrAlreadyInStatus), errors.Is(err, jobs.ErrTransitionNotPermitted):
		// One outcome for both, unlike delivery's port, and the difference is what the caller does
		// with it. A milestone arriving late is a record worth keeping (SHIP-112), so `delivery` has
		// to tell "already been there" from "not there yet". An award has no such case: a job already
		// at 'Awarded' and a job that can never reach it are both "this award did not happen", and
		// the accepted bid the customer is holding is what tells them which.
		return bidding.JobAwardNotPermitted, nil
	default:
		return bidding.JobAwardUnrecognised, err
	}
}

// presentedJobs implements bidding.Presentation, which is Docs/02 §1's Negotiating status (SHIP-90).
//
// The fourth seam this file joins, and the third that needed a type. It is `awardableJobs` in
// shape — a lock, a reading of Docs/02 §2's table, and the guarded transition — with one difference
// that is the whole of the ticket's concurrency story: see [presentedJobs.LeaveNegotiation].
//
// **The actor is the platform in both directions.** Docs/02 §1 calls Negotiating "a useful
// presentation status" in as many words, and these moves are the platform's reading of a condition
// in `bids` rather than an act anybody performed: a provider placing an offer asked for their offer
// to exist, not for the job to move, and `job_status_history` records who acted rather than who
// caused. `jobs.System()` is the same actor SHIP-68's expiry sweep records, and each move carries a
// reason for the same reason that one does — it is read by support and shown in the customer's
// status timeline.
type presentedJobs struct {
	jobs *jobs.Service
}

// The two reasons, in the shape jobs.ExpiryReason has: fixed strings rather than formatted ones, so
// that support can search for them and a customer's timeline reads the same on every job.
const (
	negotiationOpenedReason = "An offer was made on the job (Docs/02 §2)."
	negotiationEndedReason  = "The last live offer on the job was withdrawn or expired (Docs/02 §2)."
)

// EnterNegotiation runs `Open → Negotiating`, taking the job row for the rest of the transaction.
//
// An ordinary blocking lock, because `bidding` calls it before it has touched a `bids` row — which
// is what keeps Docs/11 §3's `jobs` → `bids` ordering in one direction. `jobs.Service.Transition`
// takes the lock itself, so there is no separate statement here.
//
// The translation is of errors into outcomes, exactly as `jobLifecycle.move` and
// `awardableJobs.MoveToAwarded` do it: everything `jobs` treats as a refusal becomes an outcome
// with a nil error, and everything else stays an error because a failing database is not an answer.
func (p presentedJobs) EnterNegotiation(
	ctx context.Context,
	r db.Runner,
	jobID uuid.UUID,
) (bidding.JobPresentation, error) {
	return p.move(p.jobs.Transition(ctx, r, jobs.Move{
		JobID:  jobID,
		To:     jobs.StatusNegotiating,
		Actor:  jobs.System(),
		Reason: negotiationOpenedReason,
	}))
}

// LeaveNegotiation runs `Negotiating → Open`, and **does not wait for the job row**.
//
// `FOR UPDATE SKIP LOCKED` first, and the transition only if it was obtained. `bidding` calls this
// once the offer has closed, which is after a `bids` row is locked — and in the expiry sweep the
// bid was locked by the *claim*, before the domain was reached at all. A blocking lock would be
// `bids` → `jobs` against the award's `jobs` → `bids`, and the first deadlock would be between a
// sweep and an award. A lock attempt that cannot wait cannot deadlock.
//
// **Skipping is also the right answer rather than merely the safe one.** Whatever holds the row is
// making a real change — an award, a cancellation, an expiry, a publication, an extension — and a
// presentation change must not overwrite or delay it. bidding.Presentation carries the cost that
// follows: under contention a job can sit at Negotiating with no live offer until the next thing
// happens to it.
//
// The status is read under the lock as well, so that a job somebody moved between the caller's
// count and this statement is answered rather than transitioned. That read costs nothing — the row
// is already in hand — and it is what makes [bidding.JobPresentationClosed] a real answer here
// rather than a translation of a refusal.
func (p presentedJobs) LeaveNegotiation(
	ctx context.Context,
	r db.Runner,
	jobID uuid.UUID,
) (bidding.JobPresentation, error) {
	const q = `SELECT status FROM jobs WHERE id = $1 FOR UPDATE SKIP LOCKED`

	var status jobs.Status
	switch err := r.QueryRow(ctx, q, jobID).Scan(&status); {
	case errors.Is(err, db.ErrNoRows):
		// Either there is no such job or somebody is holding it. `SKIP LOCKED` cannot tell the
		// two apart in one statement, and a second statement to find out would be a read whose
		// only product is the knowledge that a row exists — which is the disclosure every port
		// in this file declines to make. Held is the answer that says "no change was
		// attempted", which is true of both.
		return bidding.JobPresentationHeld, nil
	case err != nil:
		return bidding.JobPresentationUnrecognised,
			fmt.Errorf("cmd/api: holding %s to leave Negotiating: %w", jobID, err)
	}

	if !jobs.Permitted(status, jobs.StatusOpen) {
		// Docs/02 §2's table rather than a comparison against 'Negotiating' written here. The
		// row that matters is `Negotiating → Open`; `Awarded / Driver assigned → Open` is also
		// in the table and is SHIP-116's provider cancellation, which reaches this transition
		// through its own path and never through a closing offer.
		if status == jobs.StatusOpen {
			return bidding.JobPresentationAlreadyThere, nil
		}
		return bidding.JobPresentationClosed, nil
	}

	return p.move(p.jobs.Transition(ctx, r, jobs.Move{
		JobID:  jobID,
		To:     jobs.StatusOpen,
		Actor:  jobs.System(),
		Reason: negotiationEndedReason,
	}))
}

// move is the translation, in one place, so that a third presentation row added later cannot treat
// `jobs.ErrAlreadyInStatus` differently from these two.
func (presentedJobs) move(_ jobs.Job, err error) (bidding.JobPresentation, error) {
	switch {
	case err == nil:
		return bidding.JobPresentationMoved, nil
	case errors.Is(err, jobs.ErrAlreadyInStatus):
		return bidding.JobPresentationAlreadyThere, nil
	case errors.Is(err, jobs.ErrTransitionNotPermitted), errors.Is(err, jobs.ErrJobNotFound):
		// One outcome for both, which is the collapse every port here makes: a job that cannot
		// take this move and a job that does not exist are one answer, and telling them apart
		// would disclose that somebody else's job exists.
		return bidding.JobPresentationClosed, nil
	default:
		return bidding.JobPresentationUnrecognised, err
	}
}

// Compile-time proof that the four adapters satisfy the ports bidding declared.
//
// This is the only place in the build where that can be established — `bidding` names neither `fleet`
// nor `jobs`, and neither names `bidding`, so nothing else links them. If any of them ever part
// company, these lines are what say so, at compile time and in the file whose job it is to know about
// all three.
var (
	_ bidding.Eligibility  = (*fleet.Service)(nil)
	_ bidding.Negotiation  = negotiatedJobs{}
	_ bidding.Awarding     = awardableJobs{}
	_ bidding.Presentation = presentedJobs{}
	_ bidding.Vehicles     = offerableVehicles{}
	_ bidding.Directory    = offerorDirectory{}
)

// offerableVehicles implements bidding.Vehicles over the provider's own fleet (SHIP-102a).
//
// The port migration 000501 named and declined to write — "validating it needs a second `fleet`
// fact … and therefore a second port" — joined here, which is the only place `bidding` and `fleet`
// may meet. It needs a type where eligibility did not, and the reason is the reverse of the usual
// one: eligibility answers a bool already, and this has to *narrow* a `fleet.Vehicle` and two
// sentinels down to one.
//
// **`fleet.Service.Vehicle` is scoped by provider, which is what makes the collapse safe.** A
// vehicle belonging to another provider raises the same `ErrVehicleNotFound` a vehicle that does not
// exist raises, so this adapter never learns the difference and could not disclose it if it wanted
// to. `contracts/paths/fleet.yaml` makes the same collapse on `GET /v1/fleet/vehicles/{id}` and says
// why: which vehicles a competitor runs is commercial information they never published.
type offerableVehicles struct {
	fleet *fleet.Service
}

// Usable reports whether this provider may offer this vehicle, right now.
//
// Three refusals to one `false`: no such vehicle, another provider's, and one out of service.
// Anything else stays an error, because a failing database is not an answer — the reading every
// adapter in this file takes.
func (v offerableVehicles) Usable(
	ctx context.Context,
	r db.Runner,
	providerID, vehicleID uuid.UUID,
) (bool, error) {
	vehicle, err := v.fleet.Vehicle(ctx, r, providerID, vehicleID)
	switch {
	case errors.Is(err, fleet.ErrVehicleNotFound), errors.Is(err, fleet.ErrNotVehicleOwner):
		// **Both sentinels, and the pair is why this is a `switch` rather than one comparison.**
		// `fleet` keeps them apart in Go deliberately — "a test asserting 'the stranger was
		// refused' should fail if the vehicle silently stopped existing instead" — and
		// indistinguishable on the wire, which is the collapse this adapter is making. Matching
		// only the first would answer 500 for a competitor's vehicle: a refusal turned into an
		// outage by an omission the compiler cannot see.
		return false, nil
	case err != nil:
		return false, fmt.Errorf("cmd/api: reading %s's vehicle %s: %w", providerID, vehicleID, err)
	}

	// In service as well as owned. Docs/01 §4.2 makes deactivation the act that stops a vehicle
	// carrying new work, and an offer is new work. `fleet.Vehicle.Active` is that domain's own
	// reading of `deactivated_at` rather than a second comparison written here.
	return vehicle.Active(), nil
}

// offerorDirectory implements bidding.Directory for the customer's comparison screen (SHIP-102a).
//
// # Why these two statements are here and not in a domain
//
// `users` is `identity`'s table and `vehicles` is `fleet`'s. `bidding` may import neither, and
// reading them from `bidding/postgres.go` would be the import's effect without the import's
// visibility — a second place that knows what `deactivated_at` and `email_verified_at` mean, in a
// file whose header says it is the `bids` table in SQL. This is `acceptedBids`' arrangement pointing
// a third way, and it is the argument bidding/ports.go makes about locking a `jobs` row.
//
// **`fleet.Service.Vehicle` is deliberately not used here even though `offerableVehicles` uses it.**
// That method reads one vehicle scoped by its owner, which is the right shape for authorising one
// write and the wrong shape for rendering a page: a page of a hundred offers would be a hundred
// round trips. `fleet` has no batch read and adding one is an edit to a package this branch does not
// own, so the statement is here — where a cross-domain read is allowed to be visible — and the
// wave's handover records `fleet.Service.VehiclesByID` as the method that would replace it.
//
// # Two statements for the whole page, whatever is on it
//
// One over `users` and one over `vehicles`, each with an `= ANY($1)` over the identifiers the page
// named. That is the point of [bidding.Directory] taking a map rather than answering one offer at a
// time.
//
// # Nothing it selects could carry a budget, and nothing it selects is a declaration
//
// `users` is read for two verification timestamps, the account's standing and its creation date;
// `vehicles` for the type, make, model and the four capacity columns. `provider_service_areas` and
// `provider_specialties` are not read at all — SHIP-102a's *Done when* forbids both, and the way to
// be sure of that is not to have a statement that could return them. `jobs` is not read either, so
// there is no budget in reach of this file.
type offerorDirectory struct{ fleet *fleet.Service }

// Describe reads the providers and vehicles one page of offers named.
//
// An offer whose provider or vehicle has no row is simply absent from the maps, which
// [bidding.Service.Offers] renders with what it has. A page of offers must not fail because one of
// them names something unreadable — and with `ON DELETE RESTRICT` on both references (000300,
// 000504) the case is very nearly unreachable anyway.
func (d offerorDirectory) Describe(
	ctx context.Context,
	r db.Runner,
	offers map[uuid.UUID]bidding.Offeror,
) (map[uuid.UUID]bidding.OfferorDetail, error) {
	providerIDs := make([]uuid.UUID, 0, len(offers))
	vehicleIDs := make([]uuid.UUID, 0, len(offers))
	seenProvider := make(map[uuid.UUID]bool, len(offers))
	seenVehicle := make(map[uuid.UUID]bool, len(offers))

	for _, offeror := range offers {
		if !seenProvider[offeror.ProviderID] {
			seenProvider[offeror.ProviderID] = true
			providerIDs = append(providerIDs, offeror.ProviderID)
		}
		if offeror.VehicleID != uuid.Nil && !seenVehicle[offeror.VehicleID] {
			seenVehicle[offeror.VehicleID] = true
			vehicleIDs = append(vehicleIDs, offeror.VehicleID)
		}
	}

	providers, err := d.describeProviders(ctx, r, providerIDs)
	if err != nil {
		return nil, err
	}
	vehicles, err := describeVehicles(ctx, r, vehicleIDs)
	if err != nil {
		return nil, err
	}

	described := make(map[uuid.UUID]bidding.OfferorDetail, len(offers))
	for bidID, offeror := range offers {
		detail := bidding.OfferorDetail{Provider: providers[offeror.ProviderID]}
		if vehicle, found := vehicles[offeror.VehicleID]; found {
			detail.Vehicle, detail.HasVehicle = vehicle, true
		}
		described[bidID] = detail
	}
	return described, nil
}

// describeProviders is the closed customer-facing summary of each provider, from `users`.
//
// **The verification predicate is `fleet`'s, copied deliberately rather than abstracted.**
// `fleet/eligibility.go`'s second filter is `u.status = 'active' AND u.email_verified_at IS NOT NULL
// AND u.phone_verified_at IS NOT NULL`, and 000002's own comment says the five states of Docs/04 §4
// will live in `profiles` when that domain is written. Two copies of one predicate is one more than
// this repository wants, and the alternative today is worse: a port into `fleet` for a fact `fleet`
// derives from `identity`'s table, to answer a question neither domain asked. Docs/11 §3 records
// SHIP-153…SHIP-159 as what collapses both copies into `profiles`.
func (d offerorDirectory) describeProviders(
	ctx context.Context,
	r db.Runner,
	ids []uuid.UUID,
) (map[uuid.UUID]bidding.ProviderSummary, error) {
	summaries := make(map[uuid.UUID]bidding.ProviderSummary, len(ids))
	if len(ids) == 0 {
		return summaries, nil
	}

	// Who each provider trades as, from `fleet` rather than from a statement here (SHIP-79a).
	//
	// **This is the one part of this file that is not a raw SQL statement, and the asymmetry is
	// deliberate.** `users` is read here because the fact wanted — email and phone confirmed on an
	// account in good standing — is a predicate `identity` does not expose and `fleet` copies. The
	// public profile is different: `fleet.PublicProfile` *is* the closed set of what a customer may
	// be shown, defined in the domain that owns the declaration, and reading `provider_profiles`
	// here instead would put a second opinion about that set in a file that is not the one SHIP-79a
	// asks to be kept in step. `fleet.Service.PublicProfiles` is the batch read this file's earlier
	// note recorded as missing, written by SHIP-79a because a customer-facing page finally needed
	// one.
	//
	// It returns [fleet.PublicProfile] and nothing else, so `provider_service_areas` and
	// `provider_specialties` are as far out of reach here as they were when nothing read `fleet` at
	// all — which is the property the header above claims and this must not weaken.
	if d.fleet == nil {
		return nil, fmt.Errorf("cmd/api: no fleet service is wired, so %d providers cannot be described", len(ids))
	}
	public, err := d.fleet.PublicProfiles(ctx, r, ids)
	if err != nil {
		return nil, err
	}

	const q = `
		SELECT id,
		       (status = 'active'
		            AND email_verified_at IS NOT NULL
		            AND phone_verified_at IS NOT NULL) AS verified,
		       created_at
		FROM users
		WHERE id = ANY($1)`

	rows, err := r.Query(ctx, q, ids)
	if err != nil {
		return nil, fmt.Errorf("cmd/api: describing %d providers: %w", len(ids), err)
	}
	defer rows.Close()

	for rows.Next() {
		var summary bidding.ProviderSummary
		if err := rows.Scan(&summary.ID, &summary.Verified, &summary.MemberSince); err != nil {
			return nil, fmt.Errorf("cmd/api: describing one of %d providers: %w", len(ids), err)
		}

		// A provider who has not declared a profile is absent from the map and leaves both
		// fields empty, which is what the client renders as "not stated". The offer stays on the
		// screen either way: an unfinished profile is not a reason to hide a price.
		if declared, found := public[summary.ID]; found {
			summary.DisplayName = declared.DisplayName
			summary.OperatesAs = declared.OperatesAs.String()
		}
		summaries[summary.ID] = summary
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("cmd/api: describing %d providers: %w", len(ids), err)
	}
	return summaries, nil
}

// describeVehicles is the customer-facing description of each vehicle, from `vehicles`.
//
// **`registration` is not selected**, which is the same reason it is not a field on
// bidding.VehicleSummary: a plate identifies a vehicle in the physical world and a comparison screen
// is choosing between offers rather than meeting a driver. Not selecting it is stronger than not
// rendering it.
//
// **`deactivated_at` is not read either, and that is deliberate rather than an omission.** An offer
// made with a vehicle that has since left service still stands on it — 000504's `ON DELETE RESTRICT`
// and bidding.Vehicles' doc comment are the same sentence — so a customer comparing last week's
// offers has to see the vehicle they were made with. The check that a vehicle is in service belongs
// where the offer is *written*, which is `offerableVehicles` above.
func describeVehicles(
	ctx context.Context,
	r db.Runner,
	ids []uuid.UUID,
) (map[uuid.UUID]bidding.VehicleSummary, error) {
	summaries := make(map[uuid.UUID]bidding.VehicleSummary, len(ids))
	if len(ids) == 0 {
		return summaries, nil
	}

	const q = `
		SELECT id, vehicle_type, COALESCE(make, ''), COALESCE(model, ''),
		       COALESCE(max_weight_kg, 0), COALESCE(load_length_cm, 0),
		       COALESCE(load_width_cm, 0), COALESCE(load_height_cm, 0)
		FROM vehicles
		WHERE id = ANY($1)`

	rows, err := r.Query(ctx, q, ids)
	if err != nil {
		return nil, fmt.Errorf("cmd/api: describing %d vehicles: %w", len(ids), err)
	}
	defer rows.Close()

	for rows.Next() {
		var summary bidding.VehicleSummary
		if err := rows.Scan(
			&summary.ID, &summary.Type, &summary.Make, &summary.Model,
			&summary.MaxWeightKg, &summary.LengthCm, &summary.WidthCm, &summary.HeightCm,
		); err != nil {
			return nil, fmt.Errorf("cmd/api: describing one of %d vehicles: %w", len(ids), err)
		}
		summaries[summary.ID] = summary
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("cmd/api: describing %d vehicles: %w", len(ids), err)
	}
	return summaries, nil
}
