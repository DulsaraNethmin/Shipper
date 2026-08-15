package fleet

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/DulsaraNethmin/Shipper/services/core/internal/db"
	"github.com/DulsaraNethmin/Shipper/services/core/internal/pagination"
)

// SHIP-81 — which open jobs a provider may bid on.
//
// Docs/01 §4.3's first line, taken literally: "Filter jobs by provider service area, vehicle
// capability, verification state, and job status." Four filters, and this file is all four of them.
//
// # The decision this ticket existed to take: one SQL predicate, in this domain
//
// The four filters read four tables owned by three domains — `provider_service_areas` and
// `vehicles` here, `users` in the shared block, and `jobs` in a domain this package may not
// import. This is the first query in the project needing more than one domain's data, and there
// were two honest shapes for it.
//
//   - **One SQL statement** naming all four tables. Correct in one round trip, pageable, and it
//     crosses no Go import boundary — but its text reaches into tables three other domains own.
//   - **Ports plus composition in Go.** This package declares in a `ports.go` what it needs of
//     `jobs`, `cmd/api` supplies the closure, and the filtering happens here. Table ownership is
//     honoured exactly; the cost is paid somewhere else.
//
// **Settled: one SQL statement, here.** Three reasons, in the order they weighed.
//
//  1. **Ports cannot page this, and an unpageable feed is not a feed.** Eligibility is a set
//     intersection: to return twenty eligible jobs, something has to know which of the open jobs
//     are eligible *before* it takes twenty. A port handing `jobs` back to this package makes the
//     filter run in Go, so the page boundary lands on the unfiltered set — one provider serving
//     one postcode would read every open job on the platform to fill a screen, and "the next
//     twenty eligible" would have no answer at all. SHIP-82 is a paginated endpoint (Docs/09), so
//     this is not a performance nicety; it is whether the next ticket can be built.
//  2. **The only port shape that *can* page puts this domain's policy inside another domain.**
//     That shape is one where `jobs` runs the filter — "open jobs in these regions that fit one of
//     these boxes" — which is the eligibility rule itself, written in the domain that does not own
//     it. `doc.go` has said since SHIP-78 that eligibility is decided here, and Docs/07 §3 requires
//     it be decided server-side in exactly one place. Splitting it across two domains gives it two.
//  3. **The Go boundary is about imports, and it is not being crossed.** CLAUDE.md's rule is that
//     domain packages do not import each other and that interfaces are declared by the consuming
//     domain; `make lint-imports` and SHIP-11's test enforce it, and this file satisfies both — it
//     imports no domain. The same document also says **do not abstract PostgreSQL** and to test
//     against a real database, and a repository interface introduced solely to keep another
//     domain's table names out of this file would be exactly that abstraction, bought with an N+1.
//
// **What is given up, and what makes it survivable.** A SELECT naming `jobs` is a coupling with no
// compiler behind it: the jobs track could rename `pickup_postcode` and nothing here would fail to
// build. Three things answer that, and none of them is a promise to be careful.
//
//   - **Every query in this file runs against the real schema in `make check`.** The tests are
//     integration tests by construction — Docs/06 §4.1 forbids mocking the database here anyway —
//     so a renamed column is a red build in CI rather than an empty feed in production.
//   - **The reach is confined to this one file, and a test enforces it.**
//     TestOnlyTheEligibilityFilterReadsAnotherDomainsTables parses the package and fails if any
//     other non-test file names `jobs` — or, from SHIP-96a, `bids`. "Which parts of `fleet` reach
//     into another domain's tables" is therefore answered by reading one file rather than by
//     grepping and hoping.
//   - **The two job statuses this file hard-codes are paired with `ck_jobs_status`** by
//     TestTheBiddableStatusesAreRealJobStatuses — the discipline Docs/10 §3.4 requires of every
//     enumeration held in two places.
//
// **What would change the answer.** A fifth domain's table joining this predicate, or a filter
// needing something no SQL expression can express — a scoring model, a call to an adapter. At that
// point the statement stops being a filter and becomes a query planner written by hand, and the
// shape to move to is a materialised eligibility projection fed by domain events. That is a real
// design with a real cost, and it is not worth paying for four tables.
//
// # Two readers, one predicate, and that is the point
//
// [eligible] is a WHERE clause and nothing else. [Service.EligibleJobs] puts a page around it and
// SHIP-82 serves that; [Service.EligibleFor] puts `EXISTS` around it for one job, which is what
// SHIP-84 needs before it accepts a bid. `doc.go` requires both — "the feed itself is filtered here
// and a bid on an ineligible job is refused here" — and two hand-written definitions of
// eligibility would drift the first time one of them was corrected. There is one.
//
// **SHIP-96a adds a second predicate and does not add a second definition of eligibility.**
// [readable] is `eligible OR the caller already holds a bid`, and it exists because reading a job
// and bidding on one are different questions that had been answered by one clause: a provider lost
// sight of a job at the moment it stopped being biddable, including by winning it. [eligible] is
// unchanged, both of its readers are unchanged, and the new clause is used by exactly one reader —
// the single-job read. Widening the filter itself would have let a stale bid buy a new offer.
//
// # This file now names a fifth table, and the header above said that was the threshold
//
// It did, and the threshold it named is the *filter* becoming "a query planner written by hand".
// [readable]'s second clause is not that: it is one `EXISTS` on `(job_id, provider_id)`, it selects
// no column of `bids`, and it takes part in no join. The three things offered in exchange for
// reaching into `jobs` all hold for it unchanged — it runs against the real schema in `make check`,
// it is confined to this one file by TestOnlyTheEligibilityFilterReadsAnotherDomainsTables, and it
// hard-codes no enumeration that a migration could rename underneath it.
//
// **The port shape was reconsidered here and rejected again, on a different argument.** The
// paging objection that ruled it out for the feed does not apply to a single job by identifier, so
// the honest comparison is one statement against two. Two means reading the job with *no*
// eligibility predicate once a port has said "yes, they bid" — which is a second definition of what
// a provider may see, living in a closure in `cmd/api`, and the thing this file exists not to have.
//
// # A design considered and rejected: a database view
//
// A view would give one definition readable from Go *and* from psql, which would have made the
// `make verify` checks exact rather than a mirror of this predicate. It was rejected because a view
// is a schema object with a dependency: PostgreSQL refuses to drop or retype a column a view reads,
// so a `fleet` migration creating one would block the jobs track's next migration through a file
// they cannot see. Trading a documentation problem for a cross-track schema lock is the wrong
// direction.

// biddableStatuses are the job statuses that accept a bid.
//
// **Two, not one, and Docs/02 is explicit about why.** §1 defines Negotiating as "one or more
// active bids or counter-offers exist; **job remains open to eligible bids**", and adds the
// sentence that settles it: "'Negotiating' is a useful presentation status. Technically, the job
// remains available for eligible bids unless the customer closes it or awards a bid."
//
// So a filter accepting only 'Open' would be wrong — and wrong in the way hardest to see, because
// nothing can reach 'Negotiating' until SHIP-90 lands. The defect would surface months from now as
// jobs vanishing from every provider's feed the instant somebody bid on them, which reads as a
// bidding bug rather than as this line.
//
// It is a second copy of two strings `jobs` also holds, for the reason [ServiceAreaStates] is a
// second copy of eight: domains do not import each other. What keeps the copies honest is the same
// thing — TestTheBiddableStatusesAreRealJobStatuses reads `ck_jobs_status` out of `pg_constraint`
// and holds this list to it in both directions.
var biddableStatuses = []string{"Open", "Negotiating"}

// eligible is the whole of Docs/01 §4.3's four filters, as one WHERE clause.
//
// It names `j` and two parameters and nothing else, so both readers below can wrap it:
//
//	$1  the provider asking
//	$2  now, from the domain's injectable clock rather than from the database's now()
//
// **Every one of the four filters is opt-in, and that is one rule rather than four coincidences.**
// SHIP-79 settled it for the service area — "an empty declaration matches nothing, not everything",
// or the provider who has not finished onboarding becomes the widest-reaching provider on the
// platform — and `EXISTS` gives the same answer for the rest by construction: no account row, no
// declared region, no vehicle in service, no jobs. A provider who has declared nothing sees
// nothing.
//
// # 1. Job status
//
// [biddableStatuses], plus the deadline and the job's own customer.
//
// **The deadline is not a fifth filter.** SHIP-68's sweep runs on a ticker, so between a job's
// `expires_at` passing and the worker reaching it the row still says 'Open'. A feed trusting the
// column alone would offer work nobody may bid on, and the provider would discover that by being
// refused after pricing it.
//
// **Excluding the job's own customer is 000500's, handed to this ticket by name**: "there is no
// CHECK that the provider is not the job's own customer … it is a comparison across two tables and
// belongs with SHIP-81's eligibility filter." It is unreachable today — `users.role` is immutable
// (000005) and `jobs` refuses a non-customer — and it costs one line to be right if either changes.
//
// # 2. Verification state
//
// Docs/04 §4 gives verification five outcomes — Pending, Verified, Restricted, Rejected, Suspended
// — and 000002's own comment says where they will live: "Provider verification state is separate
// and lives with profiles". **`internal/profiles` is empty and migration block 200–299 is unused**,
// so those five states do not exist yet and this filter cannot read them.
//
// What it checks instead is the part of Docs/04 §3's baseline that *does* exist, which is the
// automated row of that table: email and phone verified, on a provider account in good standing.
// `identity.User.CanPublish` is the customer-side twin and Docs/04 §2 is its authority; this is
// §3's. Three positions worth stating rather than leaving to be re-derived:
//
//   - **`status = 'active'`, so 'restricted' is excluded as well as 'suspended'.** §4 makes
//     Restricted "limited access pending clarification", and §1's first principle is "do not allow
//     a provider to bid until baseline checks are complete". An account nobody has finished
//     clarifying does not bid, and the conservative direction is also the reversible one.
//   - **`role = 'provider'` is read from the column, not from the token's claim** — the claim is
//     evidence about the token and the column is the fact, the reading [ErrNotProvider] already
//     takes.
//   - **This filter is deliberately incomplete, and the ticket completing it is named.** The
//     document-review half of Docs/04 §3 — licence, registration, insurance, ABN — arrives with
//     SHIP-152…154. **That work adds its clause to this predicate.** It must not add a second
//     eligibility check elsewhere, or there will be two answers to who may bid. Docs/11 §3 records
//     this as the seam.
//
// # 3. Service area
//
// Set membership against the pickup, which is the shape SHIP-79 settled: no coordinate, no radius,
// no distance arithmetic anywhere in this domain. Note what NULL does here, and that it is wanted —
// `a.area = j.pickup_state` is NULL rather than true for a job with no address, so an incomplete
// job matches nobody.
//
// # 4. Vehicle capability
//
// **A vehicle is required, and a number that is missing never excludes.** Those sound
// contradictory and are not. Having *no* vehicle in service excludes a provider outright — the
// opt-in rule, and Docs/04 §3 requires vehicle details "before bidding with a vehicle". But a
// stated capacity is optional on both sides: 000300 lets a provider add a truck with a plate and
// nothing else, and 000404 lets a customer publish without measuring the sofa. So a comparison is
// made only where both sides supplied a number, and the filter excludes only on a *known*
// mismatch. Treating "not stated" as "does not fit" would empty the feed of every job whose
// customer left a field blank, which is most of them.
//
// **Dimensions are compared axis to axis, and rotation is not modelled.** A 200 cm item does not
// fit a 180 cm load bay lengthways, even though it would lie across a 210 cm one diagonally.
// Modelling that means guessing how the goods will be packed, which is the provider's decision at
// the tailgate rather than a filter's — and the two errors are not symmetrical: a job wrongly
// hidden costs a customer a bid, a job wrongly shown costs a provider one screen. Both sides typed
// their numbers as length, width and height, and comparing them the way both sides meant them is
// the honest reading. What would reopen it is providers reporting jobs they could have taken and
// never saw.
//
// `vehicle_requirement` is deliberately matched against nothing. It is free text — "ute with a
// tailgate lifter" — and 000404 says so: the column "stays text", and the capability vocabulary is
// [Specialty], which is a declaration about a *business* rather than a fact about a vehicle.
// Matching free text against an enumeration would hide jobs on a spelling.
const eligible = `
	-- 1. job status
	j.status IN ('Open', 'Negotiating')
	AND (j.expires_at IS NULL OR j.expires_at > $2)
	AND j.customer_id <> $1

	-- 2. verification state
	AND EXISTS (
		SELECT 1 FROM users u
		WHERE u.id                = $1
		  AND u.role              = 'provider'
		  AND u.status            = 'active'
		  AND u.email_verified_at IS NOT NULL
		  AND u.phone_verified_at IS NOT NULL)

	-- 3. service area
	AND EXISTS (
		SELECT 1 FROM provider_service_areas a
		WHERE a.provider_id = $1
		  AND ((a.scope = 'state'    AND a.area = j.pickup_state)
		    OR (a.scope = 'postcode' AND a.area = j.pickup_postcode)))

	-- 4. vehicle capability
	AND EXISTS (
		SELECT 1 FROM vehicles v
		WHERE v.provider_id    = $1
		  AND v.deactivated_at IS NULL
		  AND (j.weight_kg IS NULL OR v.max_weight_kg  IS NULL OR v.max_weight_kg  >= j.weight_kg)
		  AND (j.length_cm IS NULL OR v.load_length_cm IS NULL OR v.load_length_cm >= j.length_cm)
		  AND (j.width_cm  IS NULL OR v.load_width_cm  IS NULL OR v.load_width_cm  >= j.width_cm)
		  AND (j.height_cm IS NULL OR v.load_height_cm IS NULL OR v.load_height_cm >= j.height_cm))`

// readable is what a provider may *read* about a job, which is deliberately wider than what they
// may bid on (SHIP-96a).
//
// [eligible], **or** the caller already holds a bid on this job. Two clauses and the same two
// parameters, so this substitutes for [eligible] wherever a whole job is read rather than filtered.
//
// # Why the read had to widen, and why bidding did not
//
// SHIP-96a's row closes two gaps that turn out to be one gap seen from both ends of a bid.
//
//   - **The provider who wins a job loses their view of it by winning.** [eligible] matches
//     `status IN ('Open','Negotiating')`, so the instant the award commits, the endpoint the
//     provider was reading the pickup address and the customer's handling notes from answers 404 —
//     and SHIP-129's milestone screen can show a job identifier with no address, no goods and no
//     pickup window. Three lanes recorded that independently in wave 7.
//   - **Every provider who lost it loses theirs in the same transaction.** "View the job" works
//     while an offer is live and stops when somebody else's is accepted, which is the moment a
//     provider most wants to look at what they bid on.
//
// One clause answers both, because both audiences are the same thing: *a provider with a
// relationship to this job that is not eligibility*. There is no third audience — a provider who
// never bid and is not eligible is a stranger, and gets what a stranger gets.
//
// **Bidding is not widened with it and must not be.** [Service.EligibleFor] still reads [eligible]
// alone, so holding an expired bid does not let a provider bid again on a job that has closed. The
// two questions are genuinely different — "may I look at this" and "may I offer on this" — and
// SHIP-96a's row only asks the first.
//
// # The bid clause names no status, and that is the *Done when* read literally
//
// "A provider holding **any** bid on a job, **live or closed**, and the provider awarded it … for
// as long as the bid or the award exists." So Submitted, Countered, Accepted, Rejected, Withdrawn,
// Expired and Superseded all grant the read, and so does a Draft — a status no endpoint can
// currently produce, and one this clause would have to name specially to exclude, which is a rule
// nobody has asked for.
//
// **The award needs no clause of its own.** SHIP-92 records it by moving the winning bid to
// 'Accepted' — there is no `awarded_provider_id` column anywhere in `jobs` — so the awarded
// provider is a provider holding a bid, and a second clause would be a second way to say the same
// thing that a later schema change could make disagree with the first.
//
// **`offered_by` is not filtered either.** A customer's counter-offer (000502) is a row in `bids`
// carrying the same `provider_id` as the negotiation it belongs to, so it is evidence of the same
// relationship whichever party wrote it.
//
// Nothing here selects from `bids` — it is an `EXISTS` — so the disclosure boundary is still
// [eligibleJobColumns] and still one list. A bid tells this predicate that the caller is a party
// and tells the response nothing.
const readable = `(` + eligible + `)

	OR EXISTS (
		SELECT 1 FROM bids b
		WHERE b.job_id      = j.id
		  AND b.provider_id = $1)`

// Region is where one end of a job is, as a provider sees it.
//
// **Three parts, and the street line is not one of them**, which is a decision rather than an
// oversight. No document takes a position on when a provider learns the exact door, so this takes
// the reversible direction — the argument Docs/01 §4.3 makes about the budget, applied to an
// address. A provider pricing a job needs the locality, the distance and the state; the doorstep is
// needed by whoever drives to it, which is after the award. Disclosing it later is easy and
// withdrawing it later is not. SHIP-83 is where the provider's *detail* view confirms or reopens
// this.
//
// Distinct from [ServiceArea], which is a region a provider has *declared*. This is a place a job
// is at.
type Region struct {
	Suburb   string
	State    string
	Postcode string
}

// Window is a period a job's pickup or delivery has to happen in. Either end may be the zero time,
// which is how a customer says they only care about the other one.
type Window struct {
	Start time.Time
	End   time.Time
}

// EligibleJob is one job a provider may read, in the shape the feed shows it.
//
// **Named after the feed that defines the shape rather than after every caller that serves it.**
// SHIP-96a widened the single-job read past eligibility — a provider reads a job they have bid on
// for as long as the bid exists — and this type deliberately did not gain a second form for that.
// One shape is what makes the budget rule keepable: two would be two places a field could be added
// and two responses a test would have to know to check.
//
// **There is no budget field and there never may be**, here or in anything else this package
// serialises. Docs/01 §4.3 and CLAUDE.md both make it an invariant, and SHIP-67's source-parsing
// guard cannot see this package — TestNoProviderFacingShapeCarriesTheBudget is this package's copy
// of it, and it exists because this is the first provider-facing shape anywhere outside
// `internal/jobs`.
//
// Nor is there a customer. Docs/01 §4.3 lets a *customer* compare provider profiles once bids
// arrive; nothing gives the reverse before an award, and a feed carrying it would be an identity
// disclosure nobody asked for.
type EligibleJob struct {
	ID uuid.UUID

	// Status is any of the twelve in Docs/02 §1, stored form, and reaches the wire through
	// [wireStatus].
	//
	// **It was one of [biddableStatuses] and nothing else until SHIP-96a**, because nothing else
	// could satisfy the filter. [readable] admits a job the caller has bid on whatever became of
	// it, so `Awarded`, `In transit` and `Cancelled` all arrive here now. That is the field the
	// widened read is *for*: a provider looking at a job after the award wants to know what
	// happened to it, and the status is the only part of this shape that says.
	Status string

	Pickup  Region
	Dropoff Region

	GoodsDescription string
	LengthCm         int
	WidthCm          int
	HeightCm         int
	WeightKg         float64

	// VehicleRequirement and HandlingNotes are what the customer believes the job needs, in their
	// own words. Docs/01 §4.3 names them among the detail that improves a bid — "the fix is more
	// likely better job detail — dimensions, access constraints, handling notes — than exposing
	// the budget" — so they are carried in full rather than summarised.
	VehicleRequirement string
	HandlingNotes      string

	PickupWindow  Window
	DropoffWindow Window

	// ExpiresAt is when the job stops being offered (SHIP-68). Carried because a provider deciding
	// whether to price a job today wants to know it will still be there tomorrow.
	ExpiresAt time.Time

	CreatedAt time.Time
}

// JobCursor is a position in the eligible-jobs feed.
//
// Two fields for the reason [VehicleCursor] has two: `created_at` is not unique — a customer
// publishing several jobs in one sitting writes rows in the same millisecond — so a cursor that
// could not break the tie would repeat or skip a job at exactly the page boundary.
//
// This is the position, not its encoding. `internal/pagination` owns turning one into an opaque
// string and SHIP-82 does that at the edge; nothing here invents a second encoding.
type JobCursor struct {
	CreatedAt time.Time
	ID        uuid.UUID
}

// IsZero reports whether the query starts at the newest eligible job.
func (c JobCursor) IsZero() bool { return c.ID == uuid.Nil && c.CreatedAt.IsZero() }

// EligibilityQuery is what a provider is asking of the marketplace.
//
// The zero value is a valid query: the newest page of everything they are eligible for. There are
// deliberately no *narrowing* fields on it — no state, no vehicle, no goods type. Eligibility is
// what the platform decides; a provider narrowing their own feed further is SHIP-99's client-side
// business, and a filter parameter here would be a second place for the platform's answer to be
// argued with.
type EligibilityQuery struct {
	// Limit is how many jobs to return. Zero means the default, and anything above the maximum is
	// clamped rather than refused.
	Limit int

	// After is the position to continue from. The zero value starts at the newest.
	After JobCursor
}

// EligibleJobPage is one page of the feed.
type EligibleJobPage struct {
	Jobs []EligibleJob

	// Next is where the following page starts, and is the zero cursor on the last page.
	Next JobCursor

	// HasMore says whether asking again would return anything, carried rather than inferred from
	// Next — "no cursor" is also what the first request looks like.
	HasMore bool
}

// EligibleJobs is the feed: every job this provider may bid on, newest first (SHIP-81).
//
// The page shape is [Service.Vehicles]', including the read of one extra row to answer "is there
// another page" without a second query and without counting the whole set.
//
// A provider who is not verified, has declared no service area, or has no vehicle in service gets
// an empty page rather than an error, and that is deliberate. None of the three is a *failure* of
// the request — they are the truthful answer to it — and SHIP-82 has a screen for "nothing here
// yet" that it does not have for a 403. What the client does about it, which is send them to
// onboarding, it can decide from an empty feed and the profile it already holds.
func (s *Service) EligibleJobs(ctx context.Context, r db.Runner, providerID uuid.UUID,
	q EligibilityQuery) (EligibleJobPage, error) {

	if providerID == uuid.Nil {
		return EligibleJobPage{}, fmt.Errorf("fleet: an eligibility query names no provider: %w", ErrNotProvider)
	}

	limit := q.Limit
	switch {
	case limit <= 0:
		limit = pagination.DefaultLimit
	case limit > pagination.MaxLimit:
		limit = pagination.MaxLimit
	}

	found, err := s.store.eligibleJobs(ctx, r, providerID, s.clock.Now(), q.After, limit+1)
	if err != nil {
		return EligibleJobPage{}, err
	}

	page := EligibleJobPage{Jobs: found}
	if len(found) > limit {
		page.Jobs = found[:limit]

		last := page.Jobs[limit-1]
		page.Next = JobCursor{CreatedAt: last.CreatedAt, ID: last.ID}
		page.HasMore = true
	}
	return page, nil
}

// ProviderJobFor is one job as this provider may read it, by identifier (SHIP-83, widened by
// SHIP-96a).
//
// **It authorises and reads in one statement rather than two.** SHIP-83 could have asked
// [Service.EligibleFor] and then read the job, and that shape was rejected for two reasons that
// both matter:
//
//   - **A second SELECT is a second definition of what a provider may see.** The read would need
//     its own WHERE, and the only honest one is a predicate this file already holds — so the choice
//     is between naming it twice and naming it once. `doc.go` requires eligibility decided in one
//     place.
//   - **Two statements can disagree with each other.** Between the check and the read the customer
//     can cancel the job, and the detail view would then serve a job nobody may bid on. One
//     statement cannot straddle that.
//
// # It reads [readable] and not [eligible], which is SHIP-96a's whole change
//
// Until SHIP-96a this was the third reader of [eligible], and a provider therefore lost sight of a
// job at the moment it stopped being biddable — **including by winning it.** [readable] adds the
// bid the caller already holds, so the view survives the award, the rejection and the expiry. See
// that constant for the reasoning; nothing about the row that comes back changes, because the
// column list does not.
//
// [Service.EligibleFor] is unchanged and is still what SHIP-84 asks before it writes a bid: a
// caller deciding whether to permit something wants a boolean, not a row, and *bidding* is still
// governed by eligibility alone. TestTheThreeReadersOfTheFilterAgree holds all three to each other
// for a caller who holds no bid — which is every case in that file — and
// TestTheProviderReadIsTheFeedPlusTheCallersOwnBids is where the divergence itself is pinned:
// strictly more, and only through the bid clause.
//
// **A job this provider may not read is [ErrJobNotOffered], and so is a job that does not exist.**
// The two are one sentinel rather than two, because distinguishing them would need a second query
// whose only purpose is to disclose which job identifiers exist — the reasoning
// [ErrVehicleNotFound] and [ErrNotVehicleOwner] take on the wire, taken here in the domain as well
// because there is nothing this domain could truthfully say about a job it may not read.
func (s *Service) ProviderJobFor(ctx context.Context, r db.Runner, providerID, jobID uuid.UUID) (EligibleJob, error) {
	if providerID == uuid.Nil {
		return EligibleJob{}, fmt.Errorf("fleet: a job request names no provider: %w", ErrNotProvider)
	}
	if jobID == uuid.Nil {
		return EligibleJob{}, ErrJobNotOffered
	}
	return s.store.providerJob(ctx, r, providerID, s.clock.Now(), jobID)
}

// EligibleFor answers the same question about one job (SHIP-81).
//
// The second reader of [eligible], and the reason that constant is a WHERE clause rather than a
// whole statement. `doc.go` requires that "a bid on an ineligible job is refused here", so SHIP-84
// asks this before it writes a bid; sharing the predicate with the feed is what stops the endpoint
// that shows a job and the endpoint that accepts a bid on it from disagreeing about it.
//
// A job that does not exist is `false` rather than an error, for the reason
// [postgresStore.isProvider] gives about a missing account: the caller is deciding whether to
// permit something, and "no" is a truthful and complete answer to "may I bid on a job that is not
// there". Distinguishing the two would also disclose which job identifiers exist.
func (s *Service) EligibleFor(ctx context.Context, r db.Runner, providerID, jobID uuid.UUID) (bool, error) {
	if providerID == uuid.Nil || jobID == uuid.Nil {
		return false, nil
	}
	return s.store.jobIsEligible(ctx, r, providerID, s.clock.Now(), jobID)
}

// --- the store ---------------------------------------------------------------------------------

// eligibleJobColumns is every column of an [EligibleJob], in the order [scanEligibleJob] reads
// them.
//
// **This list is the disclosure boundary, and it is an allow-list by construction.** `jobs` carries
// a `budget` column three lines from `weight_kg` in the same table; naming columns rather than
// selecting the row is what makes admitting it a deliberate edit to this constant rather than a
// field that arrives with somebody else's schema change. `SELECT *` here would be a defect the day
// `jobs` adds a column.
//
// The nullable text and numeric columns are coalesced in SQL rather than scanned into pointers, the
// trade [vehicleColumns] already takes: the domain's representation of "not stated" is the zero
// value, and 000404's constraints make zero a value those columns cannot hold. The five timestamps
// are the exception, because PostgreSQL's NULL has no representation in time.Time.
const eligibleJobColumns = `
	j.id, j.status,
	COALESCE(j.pickup_suburb, ''),  COALESCE(j.pickup_state, ''),  COALESCE(j.pickup_postcode, ''),
	COALESCE(j.dropoff_suburb, ''), COALESCE(j.dropoff_state, ''), COALESCE(j.dropoff_postcode, ''),
	COALESCE(j.goods_description, ''),
	COALESCE(j.length_cm, 0), COALESCE(j.width_cm, 0), COALESCE(j.height_cm, 0),
	COALESCE(j.weight_kg, 0),
	COALESCE(j.vehicle_requirement, ''), COALESCE(j.handling_notes, ''),
	j.pickup_window_start, j.pickup_window_end,
	j.dropoff_window_start, j.dropoff_window_end,
	j.expires_at, j.created_at`

// scanEligibleJob reads one row of [eligibleJobColumns].
//
// One function rather than a copy per call site, for the reason [scanVehicle] is one: a column
// added to the list and not here is a scan mismatch at the first call, which is the failure worth
// having.
func scanEligibleJob(row pgx.Row) (EligibleJob, error) {
	var (
		job         EligibleJob
		pickupFrom  *time.Time
		pickupTo    *time.Time
		dropoffFrom *time.Time
		dropoffTo   *time.Time
		expiresAt   *time.Time
	)

	if err := row.Scan(
		&job.ID, &job.Status,
		&job.Pickup.Suburb, &job.Pickup.State, &job.Pickup.Postcode,
		&job.Dropoff.Suburb, &job.Dropoff.State, &job.Dropoff.Postcode,
		&job.GoodsDescription,
		&job.LengthCm, &job.WidthCm, &job.HeightCm,
		&job.WeightKg,
		&job.VehicleRequirement, &job.HandlingNotes,
		&pickupFrom, &pickupTo,
		&dropoffFrom, &dropoffTo,
		&expiresAt, &job.CreatedAt,
	); err != nil {
		return EligibleJob{}, err
	}

	job.PickupWindow = Window{Start: at(pickupFrom), End: at(pickupTo)}
	job.DropoffWindow = Window{Start: at(dropoffFrom), End: at(dropoffTo)}
	job.ExpiresAt = at(expiresAt)
	return job, nil
}

// at is the NULL conversion for a timestamp, running the direction [nullText] does not.
func at(t *time.Time) time.Time {
	if t == nil {
		return time.Time{}
	}
	return *t
}

// eligibleJobs reads the feed, newest first, from a keyset position.
//
// The statement is one fixed shape with the cursor written as `$3 IS NULL OR …` rather than
// appended when it applies, for the reason [postgresStore.vehiclesFor] gives: a built statement
// renumbers its parameters as clauses come and go, and a clause dropped from the SQL but not from
// the argument list shifts every value after it silently.
//
// The ordering is `created_at DESC, id DESC` and the keyset is a **row comparison** rather than
// `created_at <= $3 AND id < $4` — the second is wrong for every row whose timestamp is strictly
// older, and it is wrong quietly, by dropping them.
//
// There is no index on `jobs` for this ordering yet, and that is a recorded gap rather than an
// oversight: the index belongs to block 400–499, and the note at the foot of migration 000302 says
// why a fleet migration must not create it.
func (postgresStore) eligibleJobs(ctx context.Context, r db.Runner, providerID uuid.UUID,
	now time.Time, after JobCursor, limit int) ([]EligibleJob, error) {

	const q = `
		SELECT ` + eligibleJobColumns + `
		FROM jobs j
		WHERE ` + eligible + `
		  AND ($3::timestamptz IS NULL OR (j.created_at, j.id) < ($3, $4))
		ORDER BY j.created_at DESC, j.id DESC
		LIMIT $5`

	// The two cursor parameters go NULL together: a position is both fields or neither, which is
	// what JobCursor.IsZero says and what the row comparison needs to be true of.
	var since, sinceID any
	if !after.IsZero() {
		since, sinceID = after.CreatedAt.UTC(), after.ID
	}

	rows, err := r.Query(ctx, q, providerID, now.UTC(), since, sinceID, limit)
	if err != nil {
		return nil, fmt.Errorf("fleet: read the jobs %s is eligible for: %w", providerID, err)
	}
	defer rows.Close()

	var out []EligibleJob
	for rows.Next() {
		// pgx.Rows satisfies pgx.Row, so the one scanner serves this read and the single-row one
		// alike — which is what stops a column being added to the list and to only one of them.
		job, err := scanEligibleJob(rows)
		if err != nil {
			return nil, fmt.Errorf("fleet: scanning the jobs %s is eligible for: %w", providerID, err)
		}
		out = append(out, job)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("fleet: reading the jobs %s is eligible for: %w", providerID, err)
	}
	return out, nil
}

// providerJob reads one job by identifier, as the calling provider may see it (SHIP-83, widened by
// SHIP-96a).
//
// **The same column list the feed uses**, with `j.id = $3` added — so this view can show nothing
// the feed could not have shown, including the budget neither of them selects. A separate column
// list here would be a second disclosure boundary, and the second one is always the one nobody
// remembers to check. SHIP-96a widened *who may ask* and deliberately did not touch *what comes
// back*: the answer for an awarded job is the answer for an open one, minus nothing and plus
// nothing.
//
// The predicate is [readable] rather than [eligible], which is the whole of the widening. The
// parentheses around it in that constant are load-bearing — `AND eligible OR bid` would bind the
// `j.id = $3` to the first branch only, and every provider holding a bid on *any* job would read
// *any* job.
//
// No rows is [ErrJobNotOffered] rather than an error about the database, because "no such job" and
// "not yours to see" are the same answer and neither is a failure of the request.
func (postgresStore) providerJob(ctx context.Context, r db.Runner, providerID uuid.UUID,
	now time.Time, jobID uuid.UUID) (EligibleJob, error) {

	const q = `
		SELECT ` + eligibleJobColumns + `
		FROM jobs j
		WHERE j.id = $3
		  AND (` + readable + `)`

	job, err := scanEligibleJob(r.QueryRow(ctx, q, providerID, now.UTC(), jobID))
	switch {
	case errors.Is(err, db.ErrNoRows):
		return EligibleJob{}, ErrJobNotOffered
	case err != nil:
		return EligibleJob{}, fmt.Errorf("fleet: read job %s for provider %s: %w", jobID, providerID, err)
	}
	return job, nil
}

// jobIsEligible answers [eligible] for one job.
//
// `SELECT EXISTS (…)` rather than reading the row and seeing whether anything came back, because
// the answer is a boolean and a statement returning one always returns exactly one row — so there
// is no "no rows" case to confuse with "not eligible", and PostgreSQL stops at the first match.
func (postgresStore) jobIsEligible(ctx context.Context, r db.Runner, providerID uuid.UUID,
	now time.Time, jobID uuid.UUID) (bool, error) {

	const q = `
		SELECT EXISTS (
			SELECT 1 FROM jobs j
			WHERE j.id = $3
			  AND ` + eligible + `)`

	var permitted bool
	if err := r.QueryRow(ctx, q, providerID, now.UTC(), jobID).Scan(&permitted); err != nil {
		return false, fmt.Errorf("fleet: decide whether %s may bid on %s: %w", providerID, jobID, err)
	}
	return permitted, nil
}
