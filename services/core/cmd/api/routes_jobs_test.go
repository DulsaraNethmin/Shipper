package main

import (
	"regexp"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/DulsaraNethmin/Shipper/services/core/internal/testsupport/pgtest"
)

// SHIP-65a's port implementation, against real rows.
//
// # Why this is here and not in internal/jobs
//
// [jobBidders] is the half of SHIP-65a that `jobs` cannot hold: it names `bids`, which belongs to
// `bidding`, and a domain may not import another domain. So the domain's own tests answer through a
// [jobs.Bidders] they control — which proves the *rule* and can prove nothing about the statement
// behind it — and this proves the statement. `acceptedBids` in routes_delivery.go sits in exactly
// the same position and is the precedent.
//
// The two failures this catches and the domain tests cannot: a predicate that silently narrows to
// one bid status, and a query whose arguments are the wrong way round. The second is the one worth
// naming — `HasBidOn(ctx, r, jobID, providerID)` and `WHERE job_id = $1 AND provider_id = $2` are
// two orderings that have to agree, and both are UUIDs, so nothing but a row notices.

// bidStatuses is Docs/02 §4's eight, and every one of them is a bid a provider holds.
//
// Paired with `ck_bids_status` below rather than trusted, per Docs/10 §3.4: this list lives in a
// migration `bidding` owns, in that domain's Go, and here, and a ninth value added there must fail
// something rather than quietly leave this test checking seven eighths of the question.
var bidStatuses = []string{
	"Draft",
	"Submitted",
	"Countered",
	"Accepted",
	"Rejected",
	"Withdrawn",
	"Expired",
	"Superseded",
}

// quotedBidStrings pulls the string literals out of a constraint definition.
var quotedBidStrings = regexp.MustCompile(`'([^']*)'`)

func bidStatusesInTheConstraint(t *testing.T, pool *pgxpool.Pool) []string {
	t.Helper()

	var definition string
	if err := pool.QueryRow(t.Context(),
		`SELECT pg_get_constraintdef(oid) FROM pg_constraint WHERE conname = 'ck_bids_status'`,
	).Scan(&definition); err != nil {
		t.Fatalf("reading ck_bids_status: %v", err)
	}

	var out []string
	for _, match := range quotedBidStrings.FindAllStringSubmatch(definition, -1) {
		out = append(out, match[1])
	}
	return out
}

// newBidFixtureCustomer, newBidFixtureProvider and newBidFixtureJob are the smallest rows that
// satisfy the foreign keys. Written here rather than reached for from another package because test
// helpers do not cross a package boundary in Go.
func newBidFixtureUser(t *testing.T, pool *pgxpool.Pool, email, phone, role string) uuid.UUID {
	t.Helper()

	id, err := uuid.NewV7()
	if err != nil {
		t.Fatalf("generating an id: %v", err)
	}
	if _, err := pool.Exec(t.Context(),
		`INSERT INTO users (id, email, phone, password_hash, role) VALUES ($1, $2, $3, 'x', $4)`,
		id, email, phone, role); err != nil {
		t.Fatalf("inserting %s: %v", email, err)
	}
	return id
}

func newBidFixtureJob(t *testing.T, pool *pgxpool.Pool, customer uuid.UUID) uuid.UUID {
	t.Helper()

	id, err := uuid.NewV7()
	if err != nil {
		t.Fatalf("generating an id: %v", err)
	}
	if _, err := pool.Exec(t.Context(),
		`INSERT INTO jobs (id, customer_id) VALUES ($1, $2)`, id, customer); err != nil {
		t.Fatalf("inserting a job: %v", err)
	}
	return id
}

// TestTheBidderLookupAnswersForEveryBidStatus is the *Done when*'s "a provider holding a bid",
// taken at its word.
//
// **Any bid, at any status.** A provider whose offer was rejected, superseded or withdrawn priced
// this job, and what became of it is the answer to the only question they have about it. A
// predicate that filtered on `status = 'Accepted'` would pass every test in internal/jobs and lock
// seven eighths of the readers out of the endpoint.
func TestTheBidderLookupAnswersForEveryBidStatus(t *testing.T) {
	pool := pgtest.DB(t)

	// The list this test walks is the constraint's list, so a ninth status fails here rather
	// than being silently untested.
	accepted := bidStatusesInTheConstraint(t, pool)
	if len(accepted) != len(bidStatuses) {
		t.Fatalf("ck_bids_status accepts %v and this test walks %v — Docs/02 §4's eight have "+
			"changed and the pairing is what should have told you", accepted, bidStatuses)
	}
	for _, status := range bidStatuses {
		if !containsBidStatus(accepted, status) {
			t.Fatalf("this test bids at %q, which ck_bids_status does not accept: %v",
				status, accepted)
		}
	}

	customer := newBidFixtureUser(t, pool, "bidlookup-c@example.com", "+61400000670", "customer")
	job := newBidFixtureJob(t, pool, customer)

	lookup := jobBidders{}

	for i, status := range bidStatuses {
		provider := newBidFixtureUser(t, pool,
			"bidlookup-p"+status+"@example.com", "+6140000068"+string(rune('0'+i)), "provider")

		// Before the bid exists the same call must answer false. Asserting both sides with one
		// provider is what makes this a test of the row rather than of the fixture.
		before, err := lookup.HasBidOn(t.Context(), pool, job, provider)
		if err != nil {
			t.Fatalf("%s: reading before the bid: %v", status, err)
		}
		if before {
			t.Fatalf("%s: the lookup found a bid before one was placed", status)
		}

		id, err := uuid.NewV7()
		if err != nil {
			t.Fatalf("generating an id: %v", err)
		}
		// ck_bids_offer_has_an_amount lets only a Draft carry no amount.
		if status == "Draft" {
			_, err = pool.Exec(t.Context(),
				`INSERT INTO bids (id, job_id, provider_id, status) VALUES ($1, $2, $3, $4)`,
				id, job, provider, status)
		} else {
			_, err = pool.Exec(t.Context(),
				`INSERT INTO bids (id, job_id, provider_id, status, amount, pickup_at, deliver_by)
				 VALUES ($1, $2, $3, $4, 185.00, now() + interval '2 days', now() + interval '3 days')`,
				id, job, provider, status)
		}
		if err != nil {
			t.Fatalf("%s: placing the bid: %v", status, err)
		}

		held, err := lookup.HasBidOn(t.Context(), pool, job, provider)
		if err != nil {
			t.Fatalf("%s: reading after the bid: %v", status, err)
		}
		if !held {
			t.Errorf("a %s bid does not count as holding a bid.\n"+
				"  SHIP-65a serves a job's history to any provider who bid on it, at any "+
				"status — a rejected or withdrawn bidder priced this job and is entitled to "+
				"know what became of it.", status)
		}
	}
}

// TestTheBidderLookupIsScopedToOneJobAndOneProvider is the argument-order guard.
//
// Two jobs and two providers, cross-checked. A query with `job_id = $2 AND provider_id = $1` would
// answer correctly for the one pair where they coincide and wrongly for every other, and both
// arguments are UUIDs so nothing in the type system notices.
func TestTheBidderLookupIsScopedToOneJobAndOneProvider(t *testing.T) {
	pool := pgtest.DB(t)

	customer := newBidFixtureUser(t, pool, "bidscope-c@example.com", "+61400000690", "customer")
	first := newBidFixtureJob(t, pool, customer)
	second := newBidFixtureJob(t, pool, customer)

	bidder := newBidFixtureUser(t, pool, "bidscope-p1@example.com", "+61400000691", "provider")
	onlooker := newBidFixtureUser(t, pool, "bidscope-p2@example.com", "+61400000692", "provider")

	id, err := uuid.NewV7()
	if err != nil {
		t.Fatalf("generating an id: %v", err)
	}
	if _, err := pool.Exec(t.Context(),
		`INSERT INTO bids (id, job_id, provider_id, status, amount, pickup_at, deliver_by)
		 VALUES ($1, $2, $3, 'Submitted', 185.00,
		         now() + interval '2 days', now() + interval '3 days')`, id, first, bidder); err != nil {
		t.Fatalf("placing the bid: %v", err)
	}

	lookup := jobBidders{}

	for _, c := range []struct {
		name     string
		job      uuid.UUID
		provider uuid.UUID
		want     bool
	}{
		{"the pair that bid", first, bidder, true},
		{"the same provider on another job", second, bidder, false},
		{"another provider on the job that was bid on", first, onlooker, false},
		{"neither", second, onlooker, false},
	} {
		t.Run(c.name, func(t *testing.T) {
			got, err := lookup.HasBidOn(t.Context(), pool, c.job, c.provider)
			if err != nil {
				t.Fatalf("reading: %v", err)
			}
			if got != c.want {
				t.Errorf("HasBidOn(%s, %s) = %v, want %v", c.job, c.provider, got, c.want)
			}
		})
	}
}

// TestTheBidderLookupAnswersFalseForAJobThatDoesNotExist records the answer rather than leaving it
// to be discovered.
//
// It is false rather than an error, and the caller is what makes that safe: jobs.Service.HistoryFor
// reads the job row before it asks, so a missing job has already become a 404 by then and the two
// cases cannot be confused.
func TestTheBidderLookupAnswersFalseForAJobThatDoesNotExist(t *testing.T) {
	pool := pgtest.DB(t)

	held, err := jobBidders{}.HasBidOn(t.Context(), pool, uuid.New(), uuid.New())
	if err != nil {
		t.Fatalf("reading: %v", err)
	}
	if held {
		t.Error("the lookup found a bid on a job that does not exist")
	}
}

func containsBidStatus(haystack []string, needle string) bool {
	for _, value := range haystack {
		if value == needle {
			return true
		}
	}
	return false
}
