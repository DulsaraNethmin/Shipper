package main

import (
	"context"
	"io"
	"log/slog"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/DulsaraNethmin/Shipper/services/core/internal/bidding"
	"github.com/DulsaraNethmin/Shipper/services/core/internal/clock"
	"github.com/DulsaraNethmin/Shipper/services/core/internal/db"
	"github.com/DulsaraNethmin/Shipper/services/core/internal/events"
	"github.com/DulsaraNethmin/Shipper/services/core/internal/jobs"
	"github.com/DulsaraNethmin/Shipper/services/core/internal/testsupport/pgtest"
)

// SHIP-89 — "bids expire on their own terms", demonstrated as a pass of the real registered task
// against a real database.
//
// The terms themselves are internal/bidding's and are tested there: which offers are due, what an
// expiry writes, what it emits, and every closed status the sweep must leave alone. **What is
// tested here is the half that only exists as a running process** — that the registration is in
// the manifest, that the claim is a claim, and that two workers divide the due offers rather than
// duplicating them.
//
// The clock is injected for tasks_jobs_test.go's reason: the claim judges against `clock.Now()`,
// so advancing a `clock.Fixed` is the whole of "the collection time passed" and no test waits for
// one.

// bidExpiryTask builds the registered task exactly as main would, over the given clock.
//
// Through `tasks()` rather than by calling [expireBids] directly, for [registeredTask]'s reason:
// the registration in `init` is the thing a merge can drop, and a test that reached past it would
// keep passing after the task had stopped being registered.
func bidExpiryTask(t *testing.T, pool *pgxpool.Pool, at clock.Clock) Task {
	t.Helper()
	return registeredTask(t, pool, at, "bid-expiry")
}

// TestTheBidExpiryTaskIsDeclaredSensibly is the declaration rather than the work.
func TestTheBidExpiryTaskIsDeclaredSensibly(t *testing.T) {
	pool := pgtest.DB(t)
	task := bidExpiryTask(t, pool, clock.System{})

	switch {
	case task.Every <= 0:
		t.Errorf("%s has no interval, so it would never run twice", task.Name)
	case task.timeout() >= task.Every:
		t.Errorf("%s: the timeout %s is not shorter than the interval %s",
			task.Name, task.timeout(), task.Every)
	case task.Close != nil:
		// It owns nothing: the pass runs queries inside the caller's transaction. A Close
		// here would mean a resource somebody added without saying why (SHIP-15g).
		t.Errorf("%s declares a Close, but it owns no long-lived resource", task.Name)
	}
}

// TestTheBidExpiryClaimIsAClaim is cmd/worker's own rule applied to the fourth task's query.
//
// [checkClaim] refuses a query without `FOR UPDATE SKIP LOCKED`, and it refuses it at the first
// pass rather than on the first busy day — but only for a query somebody actually runs through
// [ClaimIDs]. Asserted directly as well, so that a domain constant edited into something that is
// no longer a claim is reported by a named test rather than by whichever pass happened to run.
func TestTheBidExpiryClaimIsAClaim(t *testing.T) {
	if err := checkClaim(bidding.ExpiryClaim); err != nil {
		t.Errorf("bidding.ExpiryClaim is not a claim: %v", err)
	}
}

// TestAPassExpiresTheOffersWhoseCollectionTimeHasPassed is SHIP-89's *Done when* through the
// registered task.
//
// The two offers it must leave alone are each a distinct defect if taken: an offer collecting
// tomorrow is a live offer the customer can still accept, and an offer that has already closed
// carries the record of *how* it closed, which `Expired` would overwrite.
func TestAPassExpiresTheOffersWhoseCollectionTimeHasPassed(t *testing.T) {
	pool := pgtest.DB(t)
	m := newBidMarket(t, pool, "worker-bid-expiry", "+61400000890")

	past := m.offerAt(t, time.Now().UTC().Add(-time.Hour))
	future := m.offerAt(t, time.Now().UTC().Add(48*time.Hour))

	closed := m.offerAt(t, time.Now().UTC().Add(-time.Hour))
	m.setStatus(t, closed, "Withdrawn")

	if claimed := runPass(t, pool, bidExpiryTask(t, pool, clock.System{})); claimed != 1 {
		t.Fatalf("the pass claimed %d offers, want the one whose collection time has passed", claimed)
	}

	if got := m.statusOf(t, past); got != "Expired" {
		t.Errorf("the offer past its collection time is %s, want Expired", got)
	}
	if got := m.statusOf(t, future); got != "Submitted" {
		t.Errorf("an offer collecting in two days became %s", got)
	}
	if got := m.statusOf(t, closed); got != "Withdrawn" {
		t.Errorf("a withdrawn offer became %s; the sweep overwrote how it ended", got)
	}

	// And the event, in the same transaction as the status (Docs/10 §6.1). A consumer that
	// never hears about an expiry cannot tell either party the offer has gone.
	var emitted int
	if err := pool.QueryRow(t.Context(),
		`SELECT count(*) FROM outbox WHERE aggregate_id = $1 AND event_type = $2`,
		past, bidding.EventBidExpired).Scan(&emitted); err != nil {
		t.Fatalf("counting the events: %v", err)
	}
	if emitted != 1 {
		t.Errorf("the expired offer has %d %s events, want one", emitted, bidding.EventBidExpired)
	}
}

// TestAPassBeforeTheCollectionTimeTakesNothing is what stops the sweep being "expire everything".
//
// Without it the task above would pass against an implementation that claimed every live offer it
// found, which is the mutation this file exists to catch: an hour before the collection time,
// nothing is due.
func TestAPassBeforeTheCollectionTimeTakesNothing(t *testing.T) {
	pool := pgtest.DB(t)
	m := newBidMarket(t, pool, "worker-bid-not-yet", "+61400000891")

	collects := time.Now().UTC().Add(6 * time.Hour)
	bid := m.offerAt(t, collects)

	early := clock.NewFixed(collects.Add(-time.Hour))
	if claimed := runPass(t, pool, bidExpiryTask(t, pool, early)); claimed != 0 {
		t.Fatalf("a pass an hour early claimed %d offers, want none", claimed)
	}
	if got := m.statusOf(t, bid); got != "Submitted" {
		t.Fatalf("the offer is %s an hour before it collects, want Submitted", got)
	}

	late := clock.NewFixed(collects.Add(time.Minute))
	if claimed := runPass(t, pool, bidExpiryTask(t, pool, late)); claimed != 1 {
		t.Errorf("a pass a minute late claimed no offer, want the one past its collection time")
	}
	if got := m.statusOf(t, bid); got != "Expired" {
		t.Errorf("the offer is %s a minute after it collects, want Expired", got)
	}
}

// TestTwoWorkersExpireEachOfferExactlyOnce is the property a rolling deployment depends on.
//
// Two workers at once is the normal state during a deployment and nothing coordinates them: the
// claim's `FOR UPDATE SKIP LOCKED` hands the second whatever the first did not take. The failure
// it rules out is quiet in both directions — duplicated work is two expiry events for one offer,
// and a missing `SKIP LOCKED` is two workers doing the work of one, slowly.
//
// Counted in the outbox rather than in the two return values, because the event is what a provider
// and a customer would actually receive.
func TestTwoWorkersExpireEachOfferExactlyOnce(t *testing.T) {
	pool := pgtest.DB(t)
	m := newBidMarket(t, pool, "worker-bid-concurrent", "+61400000892")

	const due = 6
	past := time.Now().UTC().Add(-time.Hour)
	for range due {
		m.offerAt(t, past)
	}

	var wg sync.WaitGroup
	claimed := make([]int, 2)
	for worker := range claimed {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			claimed[n] = runPass(t, pool, bidExpiryTask(t, pool, clock.System{}))
		}(worker)
	}
	wg.Wait()

	if total := claimed[0] + claimed[1]; total != due {
		t.Errorf("two workers claimed %d offers between them (%v), want %d", total, claimed, due)
	}

	var emitted, offers int
	if err := pool.QueryRow(t.Context(),
		`SELECT count(*), count(DISTINCT aggregate_id) FROM outbox WHERE event_type = $1`,
		bidding.EventBidExpired).Scan(&emitted, &offers); err != nil {
		t.Fatalf("counting the expiries: %v", err)
	}
	if emitted != due || offers != due {
		t.Errorf("%d expiry events across %d offers, want %d of each — an offer expired twice "+
			"is two notifications for one event", emitted, offers, due)
	}
}

// TestAFailedBidPassLeavesEveryClaimedOfferLive is what makes a crashed worker harmless.
//
// The pass is one transaction, so a failure part way through releases every claimed row unexpired
// rather than leaving some offers closed and others not.
func TestAFailedBidPassLeavesEveryClaimedOfferLive(t *testing.T) {
	pool := pgtest.DB(t)
	m := newBidMarket(t, pool, "worker-bid-rollback", "+61400000893")

	bid := m.offerAt(t, time.Now().UTC().Add(-time.Hour))

	scheduler, err := NewScheduler(pool, slog.New(slog.NewTextHandler(io.Discard, nil)), nil)
	if err != nil {
		t.Fatalf("building the scheduler: %v", err)
	}

	task := bidExpiryTask(t, pool, clock.System{})
	failing := task
	failing.Run = func(ctx context.Context, r db.Runner) (int, error) {
		if _, err := task.Run(ctx, r); err != nil {
			return 0, err
		}
		return 0, context.Canceled // spelling:ok — standard library sentinel, not our word
	}

	if _, err := scheduler.RunOnce(t.Context(), failing); err == nil {
		t.Fatal("the pass reported success")
	}
	if got := m.statusOf(t, bid); got != "Submitted" {
		t.Errorf("the offer is %s after a failed pass, want Submitted — a claim that is not "+
			"committed is a claim that never happened", got)
	}
	var emitted int
	if err := pool.QueryRow(t.Context(),
		`SELECT count(*) FROM outbox WHERE aggregate_id = $1`, bid).Scan(&emitted); err != nil {
		t.Fatalf("counting the events: %v", err)
	}
	if emitted != 0 {
		t.Errorf("a rolled-back pass left %d event(s) in the outbox", emitted)
	}
}

// --- the fixture ---------------------------------------------------------------------------

// bidMarket is a customer, a provider and one Open job, with offers written straight into `bids`.
//
// **Written with SQL rather than through bidding.Service.PlaceBid**, and that is deliberate rather
// than lazy. A placement goes through SHIP-81's eligibility filter, which wants a service area, a
// vehicle and a verified account — three fixtures that have nothing to do with what this file
// tests, and which internal/bidding's own suite already builds. What a sweep needs is rows in the
// state the sweep reads, and `bids` has no trigger guarding its status (000500 says why), so this
// is the same statement the platform itself runs.
type bidMarket struct {
	pool     *pgxpool.Pool
	customer uuid.UUID
	provider uuid.UUID
	job      uuid.UUID
}

func newBidMarket(t *testing.T, pool *pgxpool.Pool, name, phone string) bidMarket {
	t.Helper()

	m := bidMarket{pool: pool}
	m.customer = newJobCustomer(t, pool, name+"-customer@example.com", phone)
	m.provider = newBidProvider(t, pool, name+"-provider@example.com", phone+"1")

	// Published through the guard, with [publishedJob], rather than inserted as Open: 000402
	// refuses a status write that did not come through jobs.Service.Transition, and a fixture
	// that went round it would be arranging a state the platform cannot produce. No pickup
	// window, so 000406 gives it the fourteen-day backstop — which also leaves it well clear of
	// the two job sweeps that run in the same binary.
	m.job = publishedJob(t, pool, jobs.NewService(events.NewOutbox(), clock.System{}, nil),
		m.customer, nil)
	return m
}

func newBidProvider(t *testing.T, pool *pgxpool.Pool, email, phone string) uuid.UUID {
	t.Helper()

	id, err := uuid.NewV7()
	if err != nil {
		t.Fatalf("generating an id: %v", err)
	}
	if _, err := pool.Exec(t.Context(),
		`INSERT INTO users (id, email, phone, password_hash, role) VALUES ($1, $2, $3, 'x', 'provider')`,
		id, email, phone); err != nil {
		t.Fatalf("inserting %s: %v", email, err)
	}
	return id
}

// offerAt is one live offer committing to collect at the given instant.
//
// Each offer belongs to its own provider, because `uq_bids_one_submitted_per_provider_per_job`
// permits one live offer per provider per job and the concurrency test wants six at once.
func (m bidMarket) offerAt(t *testing.T, collects time.Time) uuid.UUID {
	t.Helper()

	bid, err := uuid.NewV7()
	if err != nil {
		t.Fatalf("generating a bid id: %v", err)
	}
	provider := newBidProvider(t, m.pool,
		bid.String()+"@example.com", "+614"+bid.String()[24:])

	if _, err := m.pool.Exec(t.Context(), `
		INSERT INTO bids (id, job_id, provider_id, status, amount, pickup_at, deliver_by)
		VALUES ($1, $2, $3, 'Submitted', 450.00, $4, $5)`,
		bid, m.job, provider, collects, collects.Add(8*time.Hour)); err != nil {
		t.Fatalf("inserting an offer: %v", err)
	}
	return bid
}

func (m bidMarket) setStatus(t *testing.T, bid uuid.UUID, status string) {
	t.Helper()

	if _, err := m.pool.Exec(t.Context(),
		`UPDATE bids SET status = $2 WHERE id = $1`, bid, status); err != nil {
		t.Fatalf("setting %s to %s: %v", bid, status, err)
	}
}

func (m bidMarket) statusOf(t *testing.T, bid uuid.UUID) string {
	t.Helper()

	var status string
	if err := m.pool.QueryRow(t.Context(),
		`SELECT status FROM bids WHERE id = $1`, bid).Scan(&status); err != nil {
		t.Fatalf("reading the status of %s: %v", bid, err)
	}
	return status
}
