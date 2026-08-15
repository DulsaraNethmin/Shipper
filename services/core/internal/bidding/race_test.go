package bidding

import (
	"context"
	"errors"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/DulsaraNethmin/Shipper/services/core/internal/clock"
	"github.com/DulsaraNethmin/Shipper/services/core/internal/db"
	"github.com/DulsaraNethmin/Shipper/services/core/internal/events"
)

// SHIP-95 — the award under concurrency, written from the documents rather than from the code.
//
// Docs/08 names four races and this file runs all four:
//
//  1. two rapid award attempts on the same job — [TestASecondAwardBlocksOnTheFirstAndIsThenRefused]
//     and [TestConcurrentAwardsOfDifferentOffersLeaveOneAcceptedBid];
//  2. a provider withdrawing a bid at the moment it is being accepted —
//     [TestAWithdrawalCommittedMidAwardIsNotOverwritten] and its mirror
//     [TestAnAwardInFlightRefusesTheWithdrawalRacingIt];
//  3. an expiring bid racing an acceptance — [TestAnExpiryCommittedMidAwardIsNotOverwritten] and
//     [TestAnAwardedOfferIsOutOfAnExpirySweepsReach];
//  4. a retry arriving after the original award succeeded —
//     [TestARetriedAwardIsAnsweredFromTheAcceptedOffer] sequentially and
//     [TestConcurrentAwardsOfOneOfferAllAnswerWithIt] concurrently.
//
// Docs/08's standard is the reason the file is shaped the way it is: "write concurrency tests that
// actually run these races. A duplicate award means two providers both believe they have the job — a
// marketplace-credibility failure, not a bug report."
//
// # Every race here is observed, not hoped for
//
// **A race test that passes because it never raced is worse than no test**, because it is counted.
// Two goroutines started at the same moment prove nothing on their own: the machine is free to run
// one to completion before the other reaches its first statement, and the assertion at the end then
// holds for the same reason it would hold sequentially.
//
// So every deterministic test below drives the interleaving by hand and then *asks PostgreSQL*
// whether it happened. One transaction is opened by the test and held, the racing call is started in
// a goroutine, and [market.waitUntilBlockedBy] polls `pg_blocking_pids` until the server reports that
// the second transaction is waiting on the first's locks. Only then is the held transaction
// released. If the racing call ever finishes without having been blocked, the wait fails the test
// naming what did not happen — which is the failure mode the free-running variants cannot detect
// about themselves.
//
// `pg_stat_activity` is filtered to `current_database()`, which is exact here rather than
// approximate: `pgtest.DB` clones a database per test (Docs/10 §7.1), so the only backends in it are
// this test's own.
//
// # And every race needs two real transactions on a real database
//
// Docs/06 §4.1 and CLAUDE.md both forbid abstracting PostgreSQL here, and this file is the sharpest
// case in the repository for why. Every mechanism it exercises is the database's: `FOR UPDATE`
// ordering, a conditional `UPDATE` matching no rows because another transaction committed underneath
// it, `uq_bids_one_accepted_per_job` serialising two writers on one btree entry, and READ COMMITTED
// re-evaluating a predicate after the transaction it was waiting on commits. A mocked repository has
// none of them, and would accept every write this file exists to refuse.
//
// # Two facts about the platform that shape what is testable where
//
// **A concurrent race cannot be run through the idempotency middleware.** Two requests arriving under
// one key never both reach the service — the second is refused `idempotency_request_in_progress`
// (SHIP-15) — so a race at the service layer has to be driven below it, exactly as SHIP-111's
// [delivery.TestConcurrentRetriesRecordOneMilestone] is. That is not a workaround: it is the case the
// middleware cannot cover, which is two API instances, a Redis failover, or a retry whose cached
// entry has expired on both sides.
//
// **So race 4 is two different properties.** At the HTTP layer a retry is *sequential* and is
// answered by the middleware's stored response — SHIP-94's, demonstrated over the wire by
// `scripts/verify/61-bidding.sh`. Below it a retry is *concurrent* and is answered by the accepted
// offer itself, and that is the half this file owns. The two are marked as such in the tests'
// own comments so a reader is never guessing which layer is being talked about.

// --- driving one race by hand -------------------------------------------------------------------

// attempt is one call the test starts in the background and then waits on.
//
// The fields are written by the goroutine before `done` is closed and read by the test after
// receiving from it, which is the happens-before `-race` checks for and the reason no mutex appears
// here.
type attempt struct {
	bid  Bid
	err  error
	done chan struct{}
}

// awardInBackground starts one award in a transaction of its own.
//
// Its own transaction rather than the caller's, because that is the whole subject: two awards in one
// transaction are not a race, they are a sequence, and the guarantee under test is what the platform
// does when two *clients* arrive at once.
func (m market) awardInBackground(t *testing.T, customer, job, bid uuid.UUID) *attempt {
	t.Helper()

	a := &attempt{done: make(chan struct{})}
	go func() {
		defer close(a.done)
		a.err = db.InTx(t.Context(), m.pool, func(ctx context.Context, r db.Runner) error {
			var err error
			a.bid, err = m.svc.AwardBid(ctx, r, customer, job, bid)
			return err
		})
	}()
	return a
}

// withdrawInBackground starts one withdrawal in a transaction of its own, for the direction of race
// 2 where the award is the one already in flight.
func (m market) withdrawInBackground(t *testing.T, provider, job, bid uuid.UUID) *attempt {
	t.Helper()

	a := &attempt{done: make(chan struct{})}
	go func() {
		defer close(a.done)
		a.err = db.InTx(t.Context(), m.pool, func(ctx context.Context, r db.Runner) error {
			var err error
			a.bid, err = m.svc.WithdrawBid(ctx, r, provider, job, bid)
			return err
		})
	}()
	return a
}

// wait blocks until the attempt has finished, and fails rather than hanging if it never does.
//
// The timeout is generous and is not a timing assumption: by the time it is called the transaction
// this attempt was waiting on has been released, so the only way to reach it is a lock that was
// never going to be granted. A deadlock is reported by PostgreSQL in about a second and arrives as
// an error instead, which is the more useful failure of the two.
func (a *attempt) wait(t *testing.T, what string) {
	t.Helper()

	select {
	case <-a.done:
	case <-time.After(30 * time.Second):
		t.Fatalf("%s never finished — it is still waiting for a lock nothing is going to release", what)
	}
}

// finished reports whether the attempt is already over, without blocking.
func (a *attempt) finished() bool {
	select {
	case <-a.done:
		return true
	default:
		return false
	}
}

// hold opens a transaction the test drives by hand.
//
// [db.InTx] owns the commit, which is exactly what a race cannot have: the interleaving under test
// is "this transaction is still open while that one runs", and a closure that commits when it
// returns has no moment in the middle to hand over.
//
// The rollback in cleanup is the safety net rather than the mechanism. A test that means to commit
// says so; this is what stops a failed assertion leaving a lock held while the rest of the package
// runs.
func (m market) hold(t *testing.T) pgx.Tx {
	t.Helper()

	tx, err := m.pool.Begin(t.Context())
	if err != nil {
		t.Fatalf("opening a transaction for the test to drive: %v", err)
	}
	t.Cleanup(func() { _ = tx.Rollback(context.WithoutCancel(t.Context())) })
	return tx
}

// backendPID is the server-side process running this transaction, which is what the other side of a
// race is identified by.
//
// Read from the transaction rather than from the pool: `pg_backend_pid()` answers for the connection
// it is asked on, and the pool would answer for whichever connection it happened to hand out.
func backendPID(t *testing.T, r db.Runner) int32 {
	t.Helper()

	var pid int32
	if err := r.QueryRow(t.Context(), `SELECT pg_backend_pid()`).Scan(&pid); err != nil {
		t.Fatalf("reading the backend pid: %v", err)
	}
	return pid
}

// blockedBy counts the backends in this test's own database waiting on locks held by pid.
//
// `current_database()` is what makes this exact rather than indicative. Every worktree shares one
// PostgreSQL cluster (CLAUDE.md), and `pg_stat_activity` is cluster-wide — but `pgtest.DB` clones a
// database per test, so a backend in this one belongs to this test and to nothing else.
func (m market) blockedBy(t *testing.T, pid int32) int {
	t.Helper()

	var n int
	if err := m.pool.QueryRow(t.Context(), `
		SELECT count(*) FROM pg_stat_activity
		 WHERE datname = current_database() AND $1 = ANY(pg_blocking_pids(pid))`, pid).Scan(&n); err != nil {
		t.Fatalf("asking PostgreSQL who is blocked: %v", err)
	}
	return n
}

// waitUntilBlockedBy blocks until the server reports that the attempt is waiting on pid's locks.
//
// **This is the assertion that the race happened**, and it is deliberately a hard failure rather
// than a log line. Two goroutines started together prove nothing: if the second ran to completion
// before the first reached its first statement, every assertion afterwards would hold for the same
// reason it holds sequentially, and the test would report a guarantee it had not exercised.
//
// So there are two ways out of this loop and only one of them continues the test. Either the
// database says the attempt is waiting — the interleaving is real and the caller may now release the
// transaction it is holding — or the attempt finished without ever waiting, which is the finding.
func (m market) waitUntilBlockedBy(t *testing.T, pid int32, a *attempt, what string) {
	t.Helper()

	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		if m.blockedBy(t, pid) > 0 {
			return
		}
		if a.finished() {
			t.Fatalf("%s ran to completion without ever waiting on the transaction the test was "+
				"holding — the two never raced, and nothing serialises them (it answered: %v)",
				what, a.err)
		}
		time.Sleep(2 * time.Millisecond)
	}
	t.Fatalf("%s never reached the lock the test was holding within 20s", what)
}

// bidLockIsFree reports whether the bid row can be taken `FOR UPDATE` right now.
//
// Used to ask a question no assertion about outcomes can ask: *which row an in-flight award has
// already taken*. A short `lock_timeout` rather than `NOWAIT` because the two report the same
// SQLSTATE and a timeout also tolerates a lock granted and released in the instant between.
func (m market) bidLockIsFree(t *testing.T, bid uuid.UUID) bool {
	t.Helper()

	tx, err := m.pool.Begin(t.Context())
	if err != nil {
		t.Fatalf("opening the probe transaction: %v", err)
	}
	defer func() { _ = tx.Rollback(context.WithoutCancel(t.Context())) }()

	if _, err := tx.Exec(t.Context(), `SET LOCAL lock_timeout = '750ms'`); err != nil {
		t.Fatalf("setting the probe's lock timeout: %v", err)
	}

	var id uuid.UUID
	err = tx.QueryRow(t.Context(), `SELECT id FROM bids WHERE id = $1 FOR UPDATE`, bid).Scan(&id)

	var pgErr *pgconn.PgError
	switch {
	case err == nil:
		return true
	case errors.As(err, &pgErr) && pgErr.Code == "55P03": // lock_not_available
		return false
	default:
		t.Fatalf("probing the lock on bid %s: %v", bid, err)
		return false
	}
}

// awardOutcome is everything the platform is allowed to have written, read off the tables.
//
// Read out of the database rather than assembled from what the calls returned, for the reason
// [market.statuses] gives: the rejection sweep writes rows no caller ever named, and a duplicate
// award writes a row every caller would report as its own success. Only the tables can say how many
// there are.
//
// The count of moves to 'Awarded' is separate from the status on purpose. A job reads 'Awarded'
// after one transition and after two, and 000402's trigger means a transition without a history row
// is impossible — so the history is what distinguishes "awarded once" from "awarded twice", and the
// status alone cannot.
func (m market) awardOutcome(t *testing.T, job uuid.UUID) (status string, accepted, moves int) {
	t.Helper()

	if err := m.pool.QueryRow(t.Context(), `
		SELECT (SELECT status FROM jobs WHERE id = $1),
		       (SELECT count(*) FROM bids WHERE job_id = $1 AND status = 'Accepted'),
		       (SELECT count(*) FROM job_status_history WHERE job_id = $1 AND to_status = 'Awarded')`,
		job).Scan(&status, &accepted, &moves); err != nil {
		t.Fatalf("reading what became of job %s: %v", job, err)
	}
	return status, accepted, moves
}

// requireOneAward is the invariant every test in this file ends on.
//
// Docs/01 §6 and Docs/02 §3 in one assertion: exactly one accepted bid per job, and the job moved to
// 'Awarded' exactly once. It is written here rather than repeated per test because a race is only as
// good as the thing it checks afterwards, and eight copies of this would be eight opportunities for
// one of them to check less.
func (m market) requireOneAward(t *testing.T, job uuid.UUID) {
	t.Helper()

	status, accepted, moves := m.awardOutcome(t, job)
	if status != "Awarded" {
		t.Errorf("the job reads %q, want Awarded", status)
	}
	if accepted != 1 {
		t.Errorf("%d bids on the job are Accepted, want exactly 1 — two providers each believing they "+
			"have the job is the failure Docs/08 calls a marketplace-credibility failure", accepted)
	}
	if moves != 1 {
		t.Errorf("the job was moved to Awarded %d times, want exactly 1", moves)
	}
}

// requireNotAwarded is the same invariant from the other side, for the races the award loses.
func (m market) requireNotAwarded(t *testing.T, job uuid.UUID) {
	t.Helper()

	status, accepted, moves := m.awardOutcome(t, job)
	if status == "Awarded" || moves != 0 {
		t.Errorf("the job reads %q after %d moves to Awarded, want a job that was never awarded",
			status, moves)
	}
	if accepted != 0 {
		t.Errorf("%d bids on the job are Accepted, want none — the offer stopped being live before it "+
			"was accepted", accepted)
	}
}

// --- race 1: two rapid award attempts on the same job --------------------------------------------

// TestASecondAwardBlocksOnTheFirstAndIsThenRefused runs Docs/08's first race with the interleaving
// pinned rather than hoped for.
//
// One award is run inside a transaction the test holds open. A second award, of a *different*
// provider's offer on the same job, is started in the background and is observed waiting on the
// first before the first is allowed to commit. That is the window a duplicate award would come
// through, and it is held open deliberately for as long as the test needs.
//
// **The loser is told `conflict`, not `bidding_bid_closed`**, and contracts/paths/bidding.yaml is
// explicit about why: the award closes every other offer, so "that offer is closed" and "you have
// already awarded this job" become the same fact about every bid on the job, and only the second
// tells the client where to look.
func TestASecondAwardBlocksOnTheFirstAndIsThenRefused(t *testing.T) {
	m := newMarket(t)

	mine, _, err := m.place(t, m.provider, m.job, offer("key-race1-mine"))
	if err != nil {
		t.Fatalf("placing the first offer: %v", err)
	}
	rival := m.rival(t, 1)
	theirs, _, err := m.place(t, rival, m.job, offer("key-race1-theirs"))
	if err != nil {
		t.Fatalf("placing the rival's offer: %v", err)
	}

	// The first award, complete but uncommitted. Everything it locked, it still holds.
	first := m.hold(t)
	firstPID := backendPID(t, first)
	if _, err := m.svc.AwardBid(t.Context(), first, m.customer, m.job, mine.ID); err != nil {
		t.Fatalf("the first award: %v", err)
	}

	second := m.awardInBackground(t, m.customer, m.job, theirs.ID)
	m.waitUntilBlockedBy(t, firstPID, second, "the second award")

	if err := first.Commit(t.Context()); err != nil {
		t.Fatalf("committing the first award: %v", err)
	}
	second.wait(t, "the second award")

	if !errors.Is(second.err, ErrJobNotAwardable) {
		t.Fatalf("the second award = %v, want ErrJobNotAwardable — the job has been awarded and the "+
			"client needs to be sent to the job rather than to the offer", second.err)
	}

	m.requireOneAward(t, m.job)
	found := m.statuses(t, m.job)
	if got := found[mine.ID]; got != string(StatusAccepted) {
		t.Errorf("the offer that won reads %q, want Accepted", got)
	}
	if got := found[theirs.ID]; got != string(StatusRejected) {
		t.Errorf("the offer that lost reads %q, want Rejected — SHIP-93 closes it in the winner's "+
			"own transaction", got)
	}
}

// TestConcurrentAwardsOfDifferentOffersLeaveOneAcceptedBid is the same race with nothing sequenced.
//
// Six customers' worth of impatience: six offers from six providers on one job, six awards released
// together, no transaction held by the test and no interleaving chosen by it. This is the shape
// Docs/08 asks for — "tests that actually run these races" — and the deterministic test above is
// what stops it passing vacuously, because between them they cover both "the window is closed when I
// hold it open" and "the window is closed when the scheduler chooses".
//
// **Contention is measured rather than assumed.** An observer watches `pg_blocking_pids` throughout
// and the test fails if it never sees one attempt waiting on another: six awards that happened to
// run one after another would satisfy every assertion below while proving nothing.
//
// The racers get a pool of their own, sized to them. On the shared pool a seventh goroutine can be
// left waiting for a connection rather than for a lock, which turns a race into a queue and is
// precisely the vacuous pass this test is built to notice.
func TestConcurrentAwardsOfDifferentOffersLeaveOneAcceptedBid(t *testing.T) {
	m := newMarket(t)

	const racers = 6

	providers := make([]uuid.UUID, 0, racers)
	providers = append(providers, m.provider)
	for i := 1; i < racers; i++ {
		providers = append(providers, m.rival(t, 100+i))
	}

	offers := make([]uuid.UUID, 0, racers)
	for i, p := range providers {
		bid, _, err := m.place(t, p, m.job, offer(fmt.Sprintf("key-race1n-%d", i)))
		if err != nil {
			t.Fatalf("placing offer %d: %v", i, err)
		}
		offers = append(offers, bid.ID)
	}

	pool := racePool(t, m.pool, racers)

	watching, contended := m.watchForContention(t)

	var (
		ready      sync.WaitGroup
		wg         sync.WaitGroup
		start      = make(chan struct{})
		mu         sync.Mutex
		won        []uuid.UUID
		refused    int
		unexpected []error
	)
	ready.Add(racers)
	wg.Add(racers)
	for _, bid := range offers {
		go func() {
			defer wg.Done()

			ready.Done()
			<-start

			var accepted Bid
			err := db.InTx(t.Context(), pool, func(ctx context.Context, r db.Runner) error {
				var err error
				accepted, err = m.svc.AwardBid(ctx, r, m.customer, m.job, bid)
				return err
			})

			mu.Lock()
			defer mu.Unlock()
			switch {
			case err == nil:
				won = append(won, accepted.ID)
			case errors.Is(err, ErrJobNotAwardable):
				refused++
			default:
				unexpected = append(unexpected, err)
			}
		}()
	}

	ready.Wait()
	close(start)
	wg.Wait()
	close(watching)

	for _, err := range unexpected {
		t.Errorf("a concurrent award failed with something other than a legible refusal: %v", err)
	}
	if len(won) != 1 {
		t.Errorf("%d of %d concurrent awards succeeded, want exactly 1", len(won), racers)
	}
	if refused != racers-1 {
		t.Errorf("%d awards were refused with ErrJobNotAwardable, want %d", refused, racers-1)
	}
	if contended.Load() == 0 {
		t.Error("no attempt was ever seen waiting on another — the awards did not overlap, so this " +
			"test proved nothing about concurrency")
	}

	m.requireOneAward(t, m.job)

	found := m.statuses(t, m.job)
	rejected := 0
	for _, status := range found {
		if status == string(StatusRejected) {
			rejected++
		}
	}
	if rejected != racers-1 {
		t.Errorf("%d offers were closed by the award, want %d — every other live offer on an awarded "+
			"job is Rejected in the winner's own transaction (Docs/02 §3)", rejected, racers-1)
	}
	if len(won) == 1 && found[won[0]] != string(StatusAccepted) {
		t.Errorf("the winning award answered with %s, which the table reads as %q", won[0], found[won[0]])
	}
}

// racePool is a second pool on the same database, sized to the racers that will use it.
//
// pgxpool's default is one connection per CPU, which is a limit a race can hit without saying so: a
// goroutine waiting for a connection is not waiting for a lock, and the test would then be measuring
// the pool rather than the award.
func racePool(t *testing.T, from *pgxpool.Pool, n int32) *pgxpool.Pool {
	t.Helper()

	cfg, err := pgxpool.ParseConfig(from.Config().ConnString())
	if err != nil {
		t.Fatalf("reading the test pool's configuration: %v", err)
	}
	cfg.MaxConns = n
	cfg.MinConns = n

	pool, err := pgxpool.NewWithConfig(t.Context(), cfg)
	if err != nil {
		t.Fatalf("opening a pool for %d racers: %v", n, err)
	}
	t.Cleanup(pool.Close)
	return pool
}

// watchForContention polls until it is told to stop, counting the moments at which one backend in
// this database was waiting on another.
//
// It is the free-running tests' answer to the question the deterministic ones answer with
// [market.waitUntilBlockedBy]: did this actually race? Its own connection comes from the original
// pool rather than the racers', so watching cannot itself starve the thing it is watching.
func (m market) watchForContention(t *testing.T) (chan<- struct{}, *atomic.Int64) {
	t.Helper()

	stop := make(chan struct{})
	seen := &atomic.Int64{}

	go func() {
		for {
			select {
			case <-stop:
				return
			default:
			}

			var n int64
			if err := m.pool.QueryRow(t.Context(), `
				SELECT count(*) FROM pg_stat_activity
				 WHERE datname = current_database() AND cardinality(pg_blocking_pids(pid)) > 0`).
				Scan(&n); err == nil && n > seen.Load() {
				seen.Store(n)
			}
			time.Sleep(time.Millisecond)
		}
	}()

	t.Cleanup(func() {
		select {
		case <-stop:
		default:
			close(stop)
		}
	})
	return stop, seen
}

// --- race 2: a withdrawal at the moment of acceptance ---------------------------------------------

// TestAWithdrawalCommittedMidAwardIsNotOverwritten runs Docs/08's second race.
//
// A provider withdraws their offer while the customer is awarding it. The withdrawal is run first
// and held uncommitted, so the award meets a row that is `Submitted` in the last committed version
// of the table and `Withdrawn` in the one about to be committed — which is the only interleaving in
// which the two disagree, and therefore the only one worth running.
//
// **This is the race that says whether accepting is conditional on the offer still being live.** No
// constraint can express it: `ck_bids_superseded_is_not_live` catches an offer displaced by a
// counter and `ck_bids_only_a_providers_offer_is_accepted` catches the customer's own, but a
// withdrawal leaves `superseded_by` NULL and `offered_by` unchanged, so nothing in the schema stands
// between an unconditional write and a bid marked Accepted after its owner took it back. Docs/11
// §3's SHIP-88 entry says exactly this: "check that the bid it is accepting is `Submitted`" is the
// one rule application logic still has to keep.
//
// The consequence if it is not kept is not a stale row. The provider has been told their offer is
// withdrawn, the customer has been told the job is theirs, and the two find out at a pickup address.
//
// # The rule turns out to be kept twice, and mutation is how that was established
//
// Worth recording, because it changes what this test is evidence of. The platform holds the property
// with a locking re-read of the bid *and* a compare-and-set on the accept, and either one alone is
// sufficient — so removing just one is invisible to every test in the repository including this one.
// Removing the compare-and-set changes nothing anywhere; removing the lock leaves this test failing
// with the compare-and-set's own message, which is neither of this domain's sentinels and would reach
// a client as `internal_error`; removing both leaves the award **succeeding** on a withdrawn offer,
// which is the failure this test exists for and which nothing else in the package notices.
//
// So the honest claim is not "this test pins one line". It is that the *property* is pinned: whatever
// combination of guards a later change leaves standing, an offer that stopped being live during the
// award must not end up Accepted, and if none of them is left this test says so.
func TestAWithdrawalCommittedMidAwardIsNotOverwritten(t *testing.T) {
	m := newMarket(t)

	bid, _, err := m.place(t, m.provider, m.job, offer("key-race2-withdraw"))
	if err != nil {
		t.Fatalf("placing the offer: %v", err)
	}

	// The withdrawal, complete but uncommitted.
	taken := m.hold(t)
	takenPID := backendPID(t, taken)
	if _, err := m.svc.WithdrawBid(t.Context(), taken, m.provider, m.job, bid.ID); err != nil {
		t.Fatalf("withdrawing the offer: %v", err)
	}

	award := m.awardInBackground(t, m.customer, m.job, bid.ID)
	m.waitUntilBlockedBy(t, takenPID, award, "the award")

	if err := taken.Commit(t.Context()); err != nil {
		t.Fatalf("committing the withdrawal: %v", err)
	}
	award.wait(t, "the award")

	if !errors.Is(award.err, ErrBidClosed) {
		t.Fatalf("the award = %v, want ErrBidClosed — the offer stopped being live while the award "+
			"was waiting for it, and an offer that is not live must not be accepted", award.err)
	}
	if got := m.row(t, bid.ID).status; got != string(StatusWithdrawn) {
		t.Errorf("the offer reads %q, want Withdrawn — the provider took it back and the award wrote "+
			"over them", got)
	}
	m.requireNotAwarded(t, m.job)
}

// TestAnAwardInFlightRefusesTheWithdrawalRacingIt is the same race with the winner swapped.
//
// The award is held uncommitted and the withdrawal arrives behind it. Docs/01 §4.2 bounds a
// withdrawal at "until it is accepted", so the provider is refused — and refused with
// [ErrBidAccepted] rather than [ErrBidClosed], because the two lead a client to different screens:
// one is an offer that is over and the other is a job they have just won. Docs/02 §6.2 then makes
// stepping away from it a provider cancellation with consequences rather than a withdrawal.
//
// Running both directions is not symmetry for its own sake. A platform that resolved this race by
// letting whichever transaction committed second win would pass the test above and fail this one.
func TestAnAwardInFlightRefusesTheWithdrawalRacingIt(t *testing.T) {
	m := newMarket(t)

	bid, _, err := m.place(t, m.provider, m.job, offer("key-race2-award"))
	if err != nil {
		t.Fatalf("placing the offer: %v", err)
	}

	// The award, complete but uncommitted.
	awarding := m.hold(t)
	awardingPID := backendPID(t, awarding)
	if _, err := m.svc.AwardBid(t.Context(), awarding, m.customer, m.job, bid.ID); err != nil {
		t.Fatalf("the award: %v", err)
	}

	withdrawal := m.withdrawInBackground(t, m.provider, m.job, bid.ID)
	m.waitUntilBlockedBy(t, awardingPID, withdrawal, "the withdrawal")

	if err := awarding.Commit(t.Context()); err != nil {
		t.Fatalf("committing the award: %v", err)
	}
	withdrawal.wait(t, "the withdrawal")

	if !errors.Is(withdrawal.err, ErrBidAccepted) {
		t.Fatalf("the withdrawal = %v, want ErrBidAccepted — the offer was awarded while the "+
			"withdrawal waited, and the provider needs to be shown the job rather than an error",
			withdrawal.err)
	}
	if got := m.row(t, bid.ID).status; got != string(StatusAccepted) {
		t.Errorf("the offer reads %q, want Accepted — the withdrawal wrote over a committed award", got)
	}
	m.requireOneAward(t, m.job)
}

// --- race 3: an expiring bid racing an acceptance -------------------------------------------------

// TestAnExpiryCommittedMidAwardIsNotOverwritten runs Docs/08's third race.
//
// SHIP-89 has no writer yet, so the sweep is the statement a sweep is: one conditional `UPDATE`
// moving every offer that is still `Submitted` and past its own terms to `Expired`. Writing it out
// here rather than calling something is the honest arrangement — the race is against a *transaction*
// that closes the offer, and which package eventually opens that transaction changes nothing about
// what the award has to survive.
//
// It is deliberately not a copy of the withdrawal test with a different status. A withdrawal is a
// person acting on their own offer and reaches the platform through an endpoint; an expiry is a
// background sweep that acts on offers nobody is looking at, and Docs/02 §4 lists 'Expired' beside
// 'Withdrawn' as two ways of leaving the same predicate. An award that checked "not withdrawn"
// instead of "still live" would pass the test above and fail this one.
func TestAnExpiryCommittedMidAwardIsNotOverwritten(t *testing.T) {
	m := newMarket(t)

	bid, _, err := m.place(t, m.provider, m.job, offer("key-race3-expire"))
	if err != nil {
		t.Fatalf("placing the offer: %v", err)
	}

	// The expiry sweep, run and held uncommitted.
	sweep := m.hold(t)
	sweepPID := backendPID(t, sweep)
	tag, err := sweep.Exec(t.Context(),
		`UPDATE bids SET status = 'Expired' WHERE id = $1 AND status = 'Submitted'`, bid.ID)
	if err != nil {
		t.Fatalf("expiring the offer: %v", err)
	}
	if tag.RowsAffected() != 1 {
		t.Fatalf("the sweep expired %d offers, want 1 — the fixture is wrong, not the award",
			tag.RowsAffected())
	}

	award := m.awardInBackground(t, m.customer, m.job, bid.ID)
	m.waitUntilBlockedBy(t, sweepPID, award, "the award")

	if err := sweep.Commit(t.Context()); err != nil {
		t.Fatalf("committing the sweep: %v", err)
	}
	award.wait(t, "the award")

	if !errors.Is(award.err, ErrBidClosed) {
		t.Fatalf("the award = %v, want ErrBidClosed — the offer ran out on its own terms while the "+
			"award was waiting for it", award.err)
	}
	if got := m.row(t, bid.ID).status; got != string(StatusExpired) {
		t.Errorf("the offer reads %q, want Expired — the award wrote over an offer that had run out", got)
	}
	m.requireNotAwarded(t, m.job)
}

// TestAnAwardedOfferIsOutOfAnExpirySweepsReach is the third race in the order it will usually happen.
//
// The award commits, and only then does the sweep run. Nothing needs to block for this one, which is
// the point: SHIP-89 will be a worker rather than a request, and the guarantee it needs is not about
// locking at all — it is that after an award there is nothing on the job for a sweep to find.
//
// **That is a property of what the award writes rather than of what the sweep checks**, and it is
// worth pinning here rather than in SHIP-89: the accepted offer leaves the `Submitted` predicate by
// being accepted, and every other offer leaves it by being rejected in the same transaction (SHIP-93).
// A sweep written against Docs/02 §4 therefore cannot expire an awarded job's offers however late it
// runs, and cannot un-award anything by arriving after the fact.
func TestAnAwardedOfferIsOutOfAnExpirySweepsReach(t *testing.T) {
	m := newMarket(t)

	mine, _, err := m.place(t, m.provider, m.job, offer("key-race3-after-mine"))
	if err != nil {
		t.Fatalf("placing the offer: %v", err)
	}
	rival := m.rival(t, 2)
	if _, _, err := m.place(t, rival, m.job, offer("key-race3-after-theirs")); err != nil {
		t.Fatalf("placing the rival's offer: %v", err)
	}

	if _, err := m.award(t, m.customer, m.job, mine.ID); err != nil {
		t.Fatalf("the award: %v", err)
	}

	tag, err := m.pool.Exec(t.Context(),
		`UPDATE bids SET status = 'Expired' WHERE job_id = $1 AND status = 'Submitted'`, m.job)
	if err != nil {
		t.Fatalf("running the sweep after the award: %v", err)
	}
	if tag.RowsAffected() != 0 {
		t.Errorf("the sweep found %d live offers on an awarded job, want none — an award closes every "+
			"offer on the job, so there is nothing left for a sweep to expire", tag.RowsAffected())
	}
	if got := m.row(t, mine.ID).status; got != string(StatusAccepted) {
		t.Errorf("the accepted offer reads %q after the sweep ran, want Accepted", got)
	}
	m.requireOneAward(t, m.job)
}

// --- race 4: a retry arriving after the original award succeeded ----------------------------------

// TestARetriedAwardIsAnsweredFromTheAcceptedOffer is race 4 in its *sequential* form, at the service
// layer.
//
// contracts/paths/bidding.yaml publishes it as the row a client should design against: "the same
// key, much later, or a **fresh** key → the accepted offer itself → `200` with the same bid, and
// nothing further recorded". The guarantee deliberately does not depend on the key, because a phone
// that lost its connection, was restarted and generated a fresh key for the same intent must not be
// told its award failed when it succeeded.
//
// **The HTTP layer's sequential retry is a different mechanism and is not this test.** There the
// middleware replays the stored response byte for byte while its entry lives (SHIP-15, SHIP-94), and
// `scripts/verify/61-bidding.sh` demonstrates both over the wire. What is asserted here is what
// answers when that cache cannot: the row.
func TestARetriedAwardIsAnsweredFromTheAcceptedOffer(t *testing.T) {
	m := newMarket(t)

	bid, _, err := m.place(t, m.provider, m.job, offer("key-race4-seq"))
	if err != nil {
		t.Fatalf("placing the offer: %v", err)
	}

	first, err := m.award(t, m.customer, m.job, bid.ID)
	if err != nil {
		t.Fatalf("the first award: %v", err)
	}

	retried, err := m.award(t, m.customer, m.job, bid.ID)
	if err != nil {
		t.Fatalf("the retry = %v, want the accepted offer back", err)
	}
	if retried.ID != first.ID {
		t.Errorf("the retry answered with %s, want the offer the award accepted (%s)", retried.ID, first.ID)
	}
	if retried.Status != StatusAccepted {
		t.Errorf("the retry answered with an offer reading %q, want Accepted", retried.Status)
	}
	m.requireOneAward(t, m.job)
}

// TestConcurrentAwardsOfOneOfferAllAnswerWithIt is race 4 in its *concurrent* form, which is the one
// the middleware cannot cover.
//
// Six awards of the same offer released together. Under one idempotency key these never both reach
// the service — SHIP-15 refuses the second with `idempotency_request_in_progress` — so this is what
// is left when it cannot: two API instances, a Redis failover, or a retry whose cached entry has
// expired on both sides. Exactly the case SHIP-111's
// [delivery.TestConcurrentRetriesRecordOneMilestone] covers for a milestone.
//
// **Every one of them succeeds, and that is the assertion rather than a tolerance.** A retry of an
// award is not a second award: the accepted offer is the record of the request, and a client told
// `conflict` because it asked twice for something that had already happened would reasonably show
// its user a failure. So all six answer with the same offer, one transition is recorded, and one row
// is Accepted.
func TestConcurrentAwardsOfOneOfferAllAnswerWithIt(t *testing.T) {
	m := newMarket(t)

	const racers = 6

	bid, _, err := m.place(t, m.provider, m.job, offer("key-race4-concurrent"))
	if err != nil {
		t.Fatalf("placing the offer: %v", err)
	}

	pool := racePool(t, m.pool, racers)
	watching, contended := m.watchForContention(t)

	var (
		ready  sync.WaitGroup
		wg     sync.WaitGroup
		start  = make(chan struct{})
		mu     sync.Mutex
		ids    = map[uuid.UUID]int{}
		failed []error
	)
	ready.Add(racers)
	wg.Add(racers)
	for range racers {
		go func() {
			defer wg.Done()

			ready.Done()
			<-start

			var accepted Bid
			err := db.InTx(t.Context(), pool, func(ctx context.Context, r db.Runner) error {
				var err error
				accepted, err = m.svc.AwardBid(ctx, r, m.customer, m.job, bid.ID)
				return err
			})

			mu.Lock()
			defer mu.Unlock()
			if err != nil {
				failed = append(failed, err)
				return
			}
			ids[accepted.ID]++
		}()
	}

	ready.Wait()
	close(start)
	wg.Wait()
	close(watching)

	for _, err := range failed {
		t.Errorf("a concurrent retry of one award was refused: %v — an award is idempotent by state, "+
			"and the accepted offer is what answers a retry the middleware cannot", err)
	}
	if len(ids) != 1 {
		t.Errorf("the concurrent retries answered with %d different offers, want 1: %v", len(ids), ids)
	}
	if n := ids[bid.ID]; n != racers && len(failed) == 0 {
		t.Errorf("%d of %d retries answered with the offer that was awarded, want all of them", n, racers)
	}
	if contended.Load() == 0 {
		t.Error("no attempt was ever seen waiting on another — the retries did not overlap, so this " +
			"test proved nothing about concurrency")
	}
	m.requireOneAward(t, m.job)
}

// --- race 5: an expiry sweep and an award contending for one job row (SHIP-95a) -------------------

// sweepClaim runs [ExpiryClaim] inside a transaction the test drives by hand, and returns what it
// locked.
//
// The claim rather than a hand-written `SELECT`, because the whole subject is what the sweep is
// **holding** when it reaches the job: `FOR UPDATE SKIP LOCKED` on `bids` is what the worker takes,
// and a test that took its own lock would be proving something about its own SQL.
func sweepClaim(t *testing.T, r db.Runner, at time.Time) []uuid.UUID {
	t.Helper()

	rows, err := r.Query(t.Context(), ExpiryClaim, at, ExpiryBatch)
	if err != nil {
		t.Fatalf("claiming the due offers: %v", err)
	}
	defer rows.Close()

	var due []uuid.UUID
	for rows.Next() {
		var id uuid.UUID
		if err := rows.Scan(&id); err != nil {
			t.Fatalf("scanning a claimed offer: %v", err)
		}
		due = append(due, id)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("claiming the due offers: %v", err)
	}
	return due
}

// liveOffers is how many offers on a job either party could still act on.
//
// Read through [postgresStore.liveOffers] rather than through a `WHERE` written here, because that
// is the predicate [Service.leaveNegotiationIfEmpty] decides on and TestTheLivePredicateIsOneRule
// holds it to [Status.live]. A second copy would be a third opinion about what "live" means, and the
// assertion below is precisely that the sweep left none.
func (m market) liveOffers(t *testing.T, job uuid.UUID) int {
	t.Helper()

	live, err := m.svc.store.liveOffers(t.Context(), m.pool, job)
	if err != nil {
		t.Fatalf("counting the live offers on %s: %v", job, err)
	}
	return live
}

// deadlocked reports whether PostgreSQL killed this transaction to break a cycle.
//
// SQLSTATE 40P01. Named rather than matched on the message, and asserted separately from the
// outcome each side is supposed to reach, because a deadlock is reported as an *error* — and a test
// that accepted "one of them failed" as its pass condition would accept the deadlock as correct
// behaviour. That is the trap [TestTheJobIsTakenBeforeTheBid] records about the award's own
// ordering, and it applies here for the same reason.
func deadlocked(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "40P01"
}

// TestAnExpirySweepDoesNotQueueBehindAnAwardHoldingTheJob is SHIP-95a, and it is the one race
// Docs/08's four do not cover: **a sweep and an award contending for one `jobs` row.**
//
// # What is being held when the two meet, which is the whole of it
//
// Docs/11 §3's SHIP-88 entry records the ordering this domain keeps — `jobs` → `bids`, one
// direction, nothing coming back — and [Presentation.LeaveNegotiation] is the one call that cannot
// obey it. By the time `bidding` reaches it the offer has already closed, and in the expiry sweep
// the `bids` rows were locked by the *claim*, before this package was entered at all. So the sweep
// arrives at the job holding bids, and the award arrives at the bid holding the job. That is a
// cycle, and `FOR UPDATE SKIP LOCKED` is the only thing that keeps it from closing.
//
// # The interleaving is driven by hand and then confirmed by the server
//
// The sweep claims both offers and is held. The award is started in the background, takes the job
// row — which nothing else holds — and then blocks on the first offer, which the sweep has. The test
// waits on `pg_blocking_pids` until PostgreSQL says so, exactly as SHIP-95's own tests do; only then
// does the sweep expire what it claimed and reach the job.
//
// **The contention that is asserted is the award waiting on the sweep, and that is deliberate rather
// than second best.** With `SKIP LOCKED` in place the sweep never waits for anything, so there is no
// second block for the server to report — the absence of it *is* the property. What the wait proves
// is that the two transactions genuinely overlapped, which is the half a free-running test cannot
// establish about itself.
//
// # What fails, with `SKIP LOCKED` removed
//
// The sweep queues behind the award's `jobs` row, the award is already queued behind the sweep's
// `bids` row, and PostgreSQL breaks the cycle by killing one of them with SQLSTATE 40P01 after
// `deadlock_timeout`. Which one it kills is the server's choice, so both sides are asserted: the
// sweep must succeed, and the award must be refused with [ErrBidClosed] rather than with a deadlock.
// Whichever backend dies, one of those two assertions is the one that reports it.
//
// # And the cost the port documents is asserted rather than left as prose
//
// [Presentation.LeaveNegotiation] states it plainly: "under contention a job can sit at Negotiating
// with no live offer until the next thing happens to it." That is the outcome here, and it is
// checked — a job at Negotiating with nothing live is the *correct* end state of this race, and a
// later change that made the sweep wait for the row would produce a tidier job status and a
// deadlock.
func TestAnExpirySweepDoesNotQueueBehindAnAwardHoldingTheJob(t *testing.T) {
	m := newMarket(t)

	mine, _, err := m.place(t, m.provider, m.job, offer("key-race5-mine"))
	if err != nil {
		t.Fatalf("placing the offer the award will take: %v", err)
	}
	rival := m.rival(t, 5)
	theirs, _, err := m.place(t, rival, m.job, offer("key-race5-theirs"))
	if err != nil {
		t.Fatalf("placing the rival's offer: %v", err)
	}
	if status, _ := m.jobStatus(t, m.job); status != "Negotiating" {
		t.Fatalf("the placements left the job at %q, so this race is not the one being run", status)
	}

	// The sweep, holding both offers and nothing else. Judged at [pastCollection], which is after
	// both offers' own collection time — so the claim takes both and the job is left with nothing
	// live once they are expired, which is what carries the sweep as far as the job row.
	sweep := m.hold(t)
	sweepPID := backendPID(t, sweep)

	due := sweepClaim(t, sweep, pastCollection)
	if len(due) != 2 {
		t.Fatalf("the claim took %d offers, want both — the fixture is wrong, not the sweep", len(due))
	}

	// The award, which takes the job row first and is then stopped at the offer the sweep holds.
	award := m.awardInBackground(t, m.customer, m.job, mine.ID)
	m.waitUntilBlockedBy(t, sweepPID, award, "the award")

	// Now the sweep walks on to the job, holding two `bids` rows, while the award holds the job.
	// This is the moment the whole ticket is about.
	expirer := NewService(events.NewOutbox(), nil, nil, nil,
		newTestPresentation(clock.NewFixed(pastCollection)), nil, nil, clock.NewFixed(pastCollection))

	var sweepErr error
	for _, id := range due {
		if _, sweepErr = expirer.Expire(t.Context(), sweep, id); sweepErr != nil {
			break
		}
	}

	if deadlocked(sweepErr) {
		t.Fatalf("the sweep was killed to break a deadlock: %v — LeaveNegotiation waited for a `jobs` "+
			"row while holding `bids` rows, which is the `bids` → `jobs` ordering the award's "+
			"`jobs` → `bids` closes into a cycle", sweepErr)
	}
	if sweepErr != nil {
		t.Fatalf("the sweep failed while an award held the job: %v — a presentation change must not "+
			"fail because a real change is in flight", sweepErr)
	}
	// The award must still be waiting. Two very different things can have ended it early, and the
	// deadlock is checked first because it is the one the mutation produces: with `SKIP LOCKED`
	// removed, PostgreSQL breaks the cycle by killing a backend, and it killed *this* one in the run
	// that established this test. Reporting that as "the two never overlapped" would be exactly
	// backwards — they overlapped so hard the server had to intervene.
	if award.finished() {
		if deadlocked(award.err) {
			t.Fatalf("the award was killed to break a deadlock: %v — the sweep waited for the `jobs` "+
				"row while holding the `bids` rows its claim took, which is `bids` → `jobs` against "+
				"the award's `jobs` → `bids`. That is the cycle `FOR UPDATE SKIP LOCKED` in "+
				"LeaveNegotiation exists to keep open", award.err)
		}
		t.Fatalf("the award finished while the sweep still held the offers it had claimed, so the two "+
			"never overlapped and this test proved nothing (it answered: %v)", award.err)
	}

	if err := sweep.Commit(t.Context()); err != nil {
		t.Fatalf("committing the sweep: %v", err)
	}
	award.wait(t, "the award")

	if deadlocked(award.err) {
		t.Fatalf("the award was killed to break a deadlock: %v — see the sweep's own message; "+
			"PostgreSQL chooses which side of the cycle dies, and this is the other one", award.err)
	}
	if !errors.Is(award.err, ErrBidClosed) {
		t.Fatalf("the award = %v, want ErrBidClosed — the offer ran out on its own terms while the "+
			"award was waiting for it", award.err)
	}

	found := m.statuses(t, m.job)
	for id, status := range found {
		if status != string(StatusExpired) {
			t.Errorf("offer %s reads %q after the sweep, want Expired", id, status)
		}
	}
	if got := found[theirs.ID]; got != string(StatusExpired) {
		t.Errorf("the rival's offer reads %q, want Expired", got)
	}
	m.requireNotAwarded(t, m.job)

	// The cost [Presentation.LeaveNegotiation] documents, asserted. The job is left at Negotiating
	// with nothing live on it, because the presentation change was skipped rather than queued —
	// which is what "biddable by everybody and awardable by nobody" means and is not a defect.
	status, changes := m.jobStatus(t, m.job)
	if status != "Negotiating" {
		t.Errorf("the job reads %q, want Negotiating — the sweep skipped the row somebody else was "+
			"holding, so nothing moved it back to Open", status)
	}
	if changes != 2 {
		t.Errorf("the job has %d transitions, want two: publication and Negotiating. A third would "+
			"mean the sweep waited for the job row and got it", changes)
	}
	if live := m.liveOffers(t, m.job); live != 0 {
		t.Errorf("%d offers on the job are still live, want none", live)
	}
}

// TestAnExpirySweepReachesTheJobWhenNobodyIsHoldingIt is the other half of the same statement, and
// it is what stops the test above passing for the wrong reason.
//
// `FOR UPDATE SKIP LOCKED` returning no row is indistinguishable, in one statement, from a job that
// does not exist — [presentedJobs.LeaveNegotiation] says so and answers [JobPresentationHeld] for
// both. So a `LeaveNegotiation` that had stopped working altogether, or a claim that never reached
// the job, would satisfy every assertion above: nothing moved, and nothing was meant to.
//
// This runs the identical sweep against the identical fixture with **nobody holding the job**, and
// requires the move. Between the two, "skipped" is told apart from "broken".
func TestAnExpirySweepReachesTheJobWhenNobodyIsHoldingIt(t *testing.T) {
	m := newMarket(t)

	if _, _, err := m.place(t, m.provider, m.job, offer("key-race5-uncontended-mine")); err != nil {
		t.Fatalf("placing the offer: %v", err)
	}
	rival := m.rival(t, 6)
	if _, _, err := m.place(t, rival, m.job, offer("key-race5-uncontended-theirs")); err != nil {
		t.Fatalf("placing the rival's offer: %v", err)
	}

	claimed, err := m.sweepAt(t, pastCollection)
	if err != nil {
		t.Fatalf("the sweep failed: %v", err)
	}
	if claimed != 2 {
		t.Fatalf("the sweep claimed %d offers, want both", claimed)
	}

	status, changes := m.jobStatus(t, m.job)
	if status != "Open" {
		t.Errorf("the job reads %q with nothing holding it, want Open — the sweep is meant to reach "+
			"the job row when it is free, and a LeaveNegotiation that never reaches it at all would "+
			"look exactly like the contended case", status)
	}
	if changes != 3 {
		t.Errorf("the job has %d transitions, want three: publication, Negotiating, and back", changes)
	}
}

// TestEveryLeaveNegotiationTakesTheJobRowWithoutWaiting pins the statement in all three copies of it.
//
// # Why a source guard sits beside a race, rather than instead of one
//
// The race above runs against this package's own [jobPresentation], because neither composition root
// is importable from here — `cmd/api` and `cmd/worker` are `package main`, and the port exists
// precisely so that this package names neither type. So the race proves the *property* against a
// faithful copy, and this proves the copies have not parted company.
//
// **Wave 10's finding applies and is the reason this is not the whole test**: a pairing guard is a
// text guard, and a test that reads a constant does not test the query that interpolates it. Neither
// half is sufficient. Together they answer SHIP-95a's *Done when* — "removing `SKIP LOCKED` from
// `LeaveNegotiation` fails the test rather than passing `make check`" — for every `LeaveNegotiation`
// there is, which is what the sentence says.
//
// The fixture is in the list rather than exempt from it. It is the one the race actually drives, so
// a copy that quietly dropped the clause there would take the race down with it silently.
func TestEveryLeaveNegotiationTakesTheJobRowWithoutWaiting(t *testing.T) {
	for _, file := range []string{
		"../../cmd/api/routes_bidding.go",
		"../../cmd/worker/tasks_bidding.go",
		"fixtures_test.go",
	} {
		t.Run(path.Base(path.Dir(file))+"/"+path.Base(file), func(t *testing.T) {
			source, err := os.ReadFile(file)
			if err != nil {
				t.Fatalf("reading %s: %v", file, err)
			}

			fset := token.NewFileSet()
			parsed, err := parser.ParseFile(fset, file, source, 0)
			if err != nil {
				t.Fatalf("parsing %s: %v", file, err)
			}

			var body string
			for _, decl := range parsed.Decls {
				fn, isFunc := decl.(*ast.FuncDecl)
				if !isFunc || fn.Name.Name != "LeaveNegotiation" || fn.Body == nil {
					continue
				}
				body = string(source[fn.Body.Pos()-1 : fn.Body.End()-1])
			}
			if body == "" {
				t.Fatalf("%s declares no LeaveNegotiation — the guard is now looking at the wrong "+
					"file, which is a worse failure than the one it was written for", file)
			}

			if !strings.Contains(body, "FOR UPDATE SKIP LOCKED") {
				t.Errorf("%s's LeaveNegotiation does not take the job row with `FOR UPDATE SKIP "+
					"LOCKED`. It is called holding `bids` rows, so a blocking lock there is "+
					"`bids` → `jobs` against the award's `jobs` → `bids`, and the first deadlock "+
					"is between an expiry sweep and an award — see "+
					"TestAnExpirySweepDoesNotQueueBehindAnAwardHoldingTheJob", file)
			}
		})
	}
}

// --- the lock, and the order it is taken in -------------------------------------------------------

// TestAnAwardHoldsTheJobRowUntilItCommits is the property no assertion about outcomes can reach.
//
// Docs/11 §3's SHIP-88 entry opens the award's lock ordering with "`jobs` first —
// `SELECT … FROM jobs WHERE id = $1 FOR UPDATE`. It is the outermost lock and the only one shared
// with the status guard and `job_status_history`." The port's own documentation says why it is a
// lock rather than a read: "a read that reported 'this job can be awarded' and then let go would be
// answering about a job that can be cancelled, disputed or awarded to somebody else before the
// caller writes anything."
//
// **Every outcome-based test in this package survives that lock being removed**, and this is why. A
// second award of a *different* offer would still be refused, because the write takes the
// `uq_bids_one_accepted_per_job` btree entry and the second writer blocks there and is then told the
// answer — a different mechanism reaching the same result by luck of what happens to be indexed. So
// the lock has to be observed directly: the test holds the job row from outside and asserts that an
// award cannot get past it.
//
// The award is expected to *block*, which is the assertion. If it runs to completion while another
// transaction holds the job row, the job is not held for the duration of the award at all, and every
// invariant that rests on it rests instead on whatever the schema happens to catch.
//
// # What this test proves is that the job row serialises an award, not which statement takes it
//
// The distinction is worth stating because a mutation found the edge. Deleting `FOR UPDATE` from the
// outermost read leaves this test **passing**: the transition at step 5 goes through `jobs`' guarded
// function, which locks the job row itself, so the award still cannot commit past a held row. What
// changes is *when* — the bid has been read, accepted and swept by then, and the ordering has quietly
// become `bids` → `jobs`.
//
// That is [TestTheJobIsTakenBeforeTheBid]'s question, and the two are deliberately separate rather
// than one test with two assertions. This one says an award cannot slip past a job somebody else is
// holding, which is what stops two of them interleaving. That one says the job is taken before
// anything on `bids` is, which is what stops the ordering closing into a cycle.
func TestAnAwardHoldsTheJobRowUntilItCommits(t *testing.T) {
	m := newMarket(t)

	bid, _, err := m.place(t, m.provider, m.job, offer("key-lock-job"))
	if err != nil {
		t.Fatalf("placing the offer: %v", err)
	}

	// Somebody else holding the job row, touching no bid.
	holder := m.hold(t)
	holderPID := backendPID(t, holder)
	var held uuid.UUID
	if err := holder.QueryRow(t.Context(),
		`SELECT id FROM jobs WHERE id = $1 FOR UPDATE`, m.job).Scan(&held); err != nil {
		t.Fatalf("taking the job row: %v", err)
	}

	award := m.awardInBackground(t, m.customer, m.job, bid.ID)
	m.waitUntilBlockedBy(t, holderPID, award, "the award")

	if err := holder.Rollback(t.Context()); err != nil {
		t.Fatalf("releasing the job row: %v", err)
	}
	award.wait(t, "the award")

	if award.err != nil {
		t.Fatalf("the award, once the job row was released = %v, want it to succeed", award.err)
	}
	m.requireOneAward(t, m.job)
}

// TestTheJobIsTakenBeforeTheBid is the ordering itself, and it is asserted rather than inferred.
//
// Docs/11 §3's SHIP-88 entry numbers the five steps and the first two are `jobs` then the bid, with
// the reason given plainly: "everything else in `bidding` locks only its own `bids` rows, by
// identifier, and never locks a `jobs` row. That is what makes the ordering acyclic — `jobs` →
// `bids` in one direction, and nothing going the other way."
//
// # Why this is an ordering test rather than a deadlock test
//
// Swapping the two produces a deadlock, and it is a real one rather than a predicted one: with the
// bid taken first, two awards of two different offers on one job each hold a bid and then contend for
// the job, and the winner's rejection sweep (step 4) then waits for the bid the loser is holding.
// **Mutating the award to take the bid first was tried, and PostgreSQL killed five of six racers with
// SQLSTATE 40P01** — reported by [TestConcurrentAwardsOfDifferentOffersLeaveOneAcceptedBid], which is
// where a swap surfaces as damage rather than as a diagnosis.
//
// **Asserting on that deadlock would still be the weaker test**, for two reasons. It needs both
// racers to reach their bid locks before either reaches the job, which is a scheduling coincidence
// rather than something a test can arrange; and a deadlock is reported as an *error*, so a suite that
// accepted "one of them failed" as the pass condition would accept the deadlock as correct behaviour.
//
// So the order is observed directly instead. While an award is held at the job row it has not yet
// been granted, the bid it is about to accept must still be free — and if it is not, the award took
// the two in the order that closes the cycle. The probe is one `FOR UPDATE` under a short
// `lock_timeout`, which is the only question that distinguishes the two orderings from outside.
func TestTheJobIsTakenBeforeTheBid(t *testing.T) {
	m := newMarket(t)

	bid, _, err := m.place(t, m.provider, m.job, offer("key-lock-order"))
	if err != nil {
		t.Fatalf("placing the offer: %v", err)
	}

	holder := m.hold(t)
	holderPID := backendPID(t, holder)
	var held uuid.UUID
	if err := holder.QueryRow(t.Context(),
		`SELECT id FROM jobs WHERE id = $1 FOR UPDATE`, m.job).Scan(&held); err != nil {
		t.Fatalf("taking the job row: %v", err)
	}

	award := m.awardInBackground(t, m.customer, m.job, bid.ID)
	m.waitUntilBlockedBy(t, holderPID, award, "the award")

	if !m.bidLockIsFree(t, bid.ID) {
		t.Error("the award was holding the bid row while it waited for the job row, so it takes the " +
			"two in the order Docs/11 §3 says closes the cycle — `bids` then `jobs`, against a " +
			"counter and a withdrawal that lock only `bids`. The first deadlock is between two " +
			"awards on one job: each holds a bid, each waits for the job, and the winner's " +
			"rejection sweep then waits for the loser's bid")
	}

	if err := holder.Rollback(t.Context()); err != nil {
		t.Fatalf("releasing the job row: %v", err)
	}
	award.wait(t, "the award")

	if award.err != nil {
		t.Fatalf("the award, once the job row was released = %v, want it to succeed", award.err)
	}
	m.requireOneAward(t, m.job)
}
