package delivery

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/DulsaraNethmin/Shipper/services/core/internal/db"
	"github.com/DulsaraNethmin/Shipper/services/core/internal/jobs"
	"github.com/DulsaraNethmin/Shipper/services/core/internal/testsupport/pgtest"
)

// SHIP-111 against a real PostgreSQL, for the reason assignment_test.go gives and one more.
//
// **The guarantee this ticket is judged on is a unique index, and a mock does not have one.** The
// *Done when* is "records a milestone once per idempotency key", and every way that can fail —
// a retry after the cached response has expired, two retries arriving at once, a key reused for a
// different action — is a question about what PostgreSQL does with uq_milestones_idempotency. A
// repository interface would answer all three from Go and be wrong about the second.
//
// The fixtures and the ports come from assignment_test.go, which this file deliberately does not
// duplicate.

// theKey is one client-generated idempotency key.
//
// A named constant rather than a literal per test, because the tests that matter are the ones where
// two calls share one, and a typo in the second literal would make them pass by not colliding.
const theKey = "6c1f2ab4-6f2a-4f0c-8b0e-9a53c1f21b8d"

// recordMilestone runs one recording in its own transaction, which is what the handler does.
func recordMilestone(t *testing.T, pool *pgxpool.Pool, svc *Service, provider, jobID uuid.UUID, rec Recording) (Record, Outcome, error) {
	t.Helper()

	var (
		record  Record
		outcome Outcome
	)
	err := db.InTx(t.Context(), pool, func(ctx context.Context, r db.Runner) error {
		var err error
		record, outcome, err = svc.RecordMilestone(ctx, r, provider, jobID, rec)
		return err
	})
	return record, outcome, err
}

// milestoneCount is how many rows a job has, read from the table rather than from what the service
// said about it.
func milestoneCount(t *testing.T, pool *pgxpool.Pool, jobID uuid.UUID) int {
	t.Helper()

	var n int
	if err := pool.QueryRow(t.Context(),
		`SELECT count(*) FROM milestones WHERE job_id = $1`, jobID).Scan(&n); err != nil {
		t.Fatalf("counting milestones on %s: %v", jobID, err)
	}
	return n
}

// historyCount is how many transitions into a status a job has recorded.
func historyCount(t *testing.T, pool *pgxpool.Pool, jobID uuid.UUID, to jobs.Status) int {
	t.Helper()

	var n int
	if err := pool.QueryRow(t.Context(),
		`SELECT count(*) FROM job_status_history WHERE job_id = $1 AND to_status = $2`,
		jobID, string(to)).Scan(&n); err != nil {
		t.Fatalf("counting transitions on %s: %v", jobID, err)
	}
	return n
}

// enRoute is a Recording of the one milestone an Awarded job can take, with a fresh key.
func enRoute(key string) Recording {
	return Recording{Milestone: MilestoneEnRouteToPickup, Key: key}
}

// TestAProviderRecordsAMilestone is the first half of SHIP-111's acceptance criterion.
func TestAProviderRecordsAMilestone(t *testing.T) {
	pool := pgtest.DB(t)
	customer := newAccount(t, pool, "record-c@example.com", "+61400000660", "customer")
	provider := newAccount(t, pool, "record-p@example.com", "+61400000661", "provider")
	jobID := awardedJob(t, pool, customer, provider)

	record, outcome, err := recordMilestone(t, pool, newTestService(), provider, jobID,
		Recording{Milestone: MilestoneEnRouteToPickup, Reason: "  loading   now ", Key: theKey})
	if err != nil {
		t.Fatalf("RecordMilestone() = %v", err)
	}

	switch {
	case outcome != OutcomeRecorded:
		t.Errorf("outcome = %q, want recorded", outcome)
	case record.ID == uuid.Nil:
		t.Error("the milestone has no id")
	case record.Milestone != MilestoneEnRouteToPickup:
		t.Errorf("milestone = %q", record.Milestone)
	case record.Actor != ActorProvider:
		t.Errorf("actor = %q, want provider — Docs/02 §3 has no other actor able to reach this yet", record.Actor)
	case record.ActorID != provider:
		t.Errorf("actor_id = %s, want the calling provider %s", record.ActorID, provider)
	case record.Reason != "loading now":
		t.Errorf("reason = %q, want the collapsed form", record.Reason)
	case record.Key != theKey:
		t.Errorf("the row does not carry the key it was recorded under: %q", record.Key)
	}

	if got := jobStatus(t, pool, jobID); got != string(jobs.StatusEnRouteToPickup) {
		t.Errorf("the job is %q, want En route to pickup — a milestone that moves the job must move it", got)
	}
	if n := historyCount(t, pool, jobID, jobs.StatusEnRouteToPickup); n != 1 {
		t.Errorf("%d transitions into En route to pickup, want 1: the move goes through the guard "+
			"and leaves a row in the same transaction", n)
	}
}

// TestTheTwoClocksAreRecordedSeparately is the invariant SHIP-110 built the table for.
//
// The recording is dated ninety minutes before the platform hears about it, which is the ordinary
// shape of an offline delivery: a driver acts in a yard with no signal and the phone syncs later.
// Both rows the act writes must keep the driver's time and the platform's apart, and neither may
// take the other's word for the actor's.
func TestTheTwoClocksAreRecordedSeparately(t *testing.T) {
	pool := pgtest.DB(t)
	customer := newAccount(t, pool, "clocks-c@example.com", "+61400000662", "customer")
	provider := newAccount(t, pool, "clocks-p@example.com", "+61400000663", "provider")
	jobID := awardedJob(t, pool, customer, provider)

	actedAt := testInstant.Add(-90 * time.Minute)

	record, _, err := recordMilestone(t, pool, newTestService(), provider, jobID,
		Recording{Milestone: MilestoneEnRouteToPickup, RecordedAt: actedAt, Key: theKey})
	if err != nil {
		t.Fatalf("RecordMilestone() = %v", err)
	}

	if !record.ActorRecordedAt.Equal(actedAt) {
		t.Errorf("actor_recorded_at is %s, want %s — the platform does not correct the actor's "+
			"clock (Docs/02 §3.1)", record.ActorRecordedAt, actedAt)
	}
	if !record.ServerRecordedAt.After(record.ActorRecordedAt) {
		t.Errorf("server_recorded_at (%s) is not after actor_recorded_at (%s); the two clocks have "+
			"been collapsed", record.ServerRecordedAt, record.ActorRecordedAt)
	}

	// The transition the milestone caused carries the same actor claim. Two rows in two tables,
	// one act: a customer shown 06:40 by one and 08:10 by the other has been told the delivery
	// happened twice.
	var historyActorRecordedAt time.Time
	if err := pool.QueryRow(t.Context(),
		`SELECT actor_recorded_at FROM job_status_history WHERE job_id = $1 AND to_status = $2`,
		jobID, string(jobs.StatusEnRouteToPickup)).Scan(&historyActorRecordedAt); err != nil {
		t.Fatalf("reading the transition: %v", err)
	}
	if !historyActorRecordedAt.Equal(actedAt) {
		t.Errorf("the transition claims the actor acted at %s and the milestone claims %s",
			historyActorRecordedAt, actedAt)
	}
}

// TestAMilestoneWithNoTimeIsStampedFromTheInjectedClock is the online case.
//
// A client recording something while it is connected has no reason to send a time, and both columns
// then describe the same moment — but they are still two values from two clocks, which is what the
// fixed clock here makes visible.
func TestAMilestoneWithNoTimeIsStampedFromTheInjectedClock(t *testing.T) {
	pool := pgtest.DB(t)
	customer := newAccount(t, pool, "noclock-c@example.com", "+61400000664", "customer")
	provider := newAccount(t, pool, "noclock-p@example.com", "+61400000665", "provider")
	jobID := awardedJob(t, pool, customer, provider)

	record, _, err := recordMilestone(t, pool, newTestService(), provider, jobID, enRoute(theKey))
	if err != nil {
		t.Fatalf("RecordMilestone() = %v", err)
	}

	if !record.ActorRecordedAt.Equal(testInstant) {
		t.Errorf("actor_recorded_at is %s, want the service's clock at %s — an inline time.Now() "+
			"would be invisible here (Docs/10 §6.3)", record.ActorRecordedAt, testInstant)
	}
	if record.ServerRecordedAt.Equal(testInstant) {
		t.Error("server_recorded_at came from the same clock as actor_recorded_at; 000601's " +
			"trigger is what should have written it")
	}
}

// TestTheSameKeyRecordsOneMilestone is the *Done when*, and the path it takes is the one that
// matters.
//
// Both calls reach the service. That is deliberate: SHIP-15's middleware would have replayed the
// second from Redis and this function would never have run, which is exactly the protection that
// disappears when the entry expires — and a phone out of signal for a day outlives any TTL worth
// setting. What is left is the unique index, and this is what it does.
func TestTheSameKeyRecordsOneMilestone(t *testing.T) {
	pool := pgtest.DB(t)
	customer := newAccount(t, pool, "once-c@example.com", "+61400000666", "customer")
	provider := newAccount(t, pool, "once-p@example.com", "+61400000667", "provider")
	jobID := awardedJob(t, pool, customer, provider)
	svc := newTestService()

	first, outcome, err := recordMilestone(t, pool, svc, provider, jobID, enRoute(theKey))
	if err != nil || outcome != OutcomeRecorded {
		t.Fatalf("the first recording = %v (outcome = %q)", err, outcome)
	}

	second, outcome, err := recordMilestone(t, pool, svc, provider, jobID, enRoute(theKey))
	if err != nil {
		t.Fatalf("the retry = %v, want the first milestone back", err)
	}
	if outcome != OutcomeAlreadyRecorded {
		t.Errorf("the retry answered %q, want already recorded", outcome)
	}
	if second.ID != first.ID {
		t.Errorf("the retry answered with %s, want the milestone the first attempt recorded (%s)",
			second.ID, first.ID)
	}
	if !second.ServerRecordedAt.Equal(first.ServerRecordedAt) {
		t.Error("the retry answered with a different arrival time, so it is not the same row")
	}

	if n := milestoneCount(t, pool, jobID); n != 1 {
		t.Errorf("%d milestone rows, want 1 — the key recorded one thing and must record it once", n)
	}
	if n := historyCount(t, pool, jobID, jobs.StatusEnRouteToPickup); n != 1 {
		t.Errorf("%d transitions, want 1 — the retry moved the job a second time", n)
	}
}

// TestConcurrentRetriesRecordOneMilestone is the same guarantee under a race.
//
// Eight requests with one key, each in its own transaction, all reaching the service at once. The
// middleware would let one through and refuse the rest with `idempotency_request_in_progress`, so
// this is what is left when it cannot: two API instances, a Redis failover, a cache miss on both
// sides of a retry.
//
// The check-then-write this could have been written as — read the key, insert if absent — passes
// this test roughly never. What makes it correct is that the losers resolve on a btree.
func TestConcurrentRetriesRecordOneMilestone(t *testing.T) {
	pool := pgtest.DB(t)
	customer := newAccount(t, pool, "race-c@example.com", "+61400000668", "customer")
	provider := newAccount(t, pool, "race-p@example.com", "+61400000669", "provider")
	jobID := awardedJob(t, pool, customer, provider)
	svc := newTestService()

	const attempts = 8

	var (
		wg      sync.WaitGroup
		mu      sync.Mutex
		ids     = map[uuid.UUID]int{}
		writers int
		failed  []error
	)

	wg.Add(attempts)
	for range attempts {
		go func() {
			defer wg.Done()

			var (
				record  Record
				outcome Outcome
			)
			err := db.InTx(t.Context(), pool, func(ctx context.Context, r db.Runner) error {
				var err error
				record, outcome, err = svc.RecordMilestone(ctx, r, provider, jobID, enRoute(theKey))
				return err
			})

			mu.Lock()
			defer mu.Unlock()
			if err != nil {
				failed = append(failed, err)
				return
			}
			ids[record.ID]++
			if outcome == OutcomeRecorded {
				writers++
			}
		}()
	}
	wg.Wait()

	for _, err := range failed {
		t.Errorf("a concurrent retry failed: %v", err)
	}
	if writers != 1 {
		t.Errorf("%d of %d concurrent retries believe they recorded the milestone, want exactly 1",
			writers, attempts)
	}
	if len(ids) != 1 {
		t.Errorf("the concurrent retries answered with %d different milestones, want 1: %v", len(ids), ids)
	}

	if n := milestoneCount(t, pool, jobID); n != 1 {
		t.Errorf("%d milestone rows after %d concurrent retries, want 1", n, attempts)
	}
	if n := historyCount(t, pool, jobID, jobs.StatusEnRouteToPickup); n != 1 {
		t.Errorf("%d transitions after %d concurrent retries, want 1", n, attempts)
	}
}

// TestTheIndexRefusesADuplicateKeyWithNoServiceInvolved is the guarantee at its source.
//
// The service is bypassed and the row is written by hand, past every check in front of it. If this
// insert ever succeeds, every test above is testing Go rather than the platform — which is the
// failure mode Docs/06 §4.1 gives for mocking the database, reached by a different route.
func TestTheIndexRefusesADuplicateKeyWithNoServiceInvolved(t *testing.T) {
	pool := pgtest.DB(t)
	customer := newAccount(t, pool, "index-c@example.com", "+61400000670", "customer")
	provider := newAccount(t, pool, "index-p@example.com", "+61400000671", "provider")
	jobID := awardedJob(t, pool, customer, provider)

	insert := func() error {
		_, err := pool.Exec(t.Context(), `
			INSERT INTO milestones
				(id, job_id, milestone, actor_type, actor_id, idempotency_key, actor_recorded_at)
			VALUES ($1, $2, 'Picked up', 'provider', $3, $4, now())`,
			uuid.Must(uuid.NewV7()), jobID, provider, theKey)
		return err
	}

	if err := insert(); err != nil {
		t.Fatalf("the first direct insert: %v", err)
	}
	if err := insert(); !db.IsUniqueViolation(err, "uq_milestones_idempotency") {
		t.Fatalf("a second row under the same key was accepted (%v); nothing above this is a "+
			"guarantee if the index does not hold", err)
	}

	// The same key on a different job is a different action and must still be allowed: a client
	// that reuses one value across two deliveries has made two requests that both deserve to
	// succeed.
	other := awardedJob(t, pool, customer, provider)
	if _, err := pool.Exec(t.Context(), `
		INSERT INTO milestones
			(id, job_id, milestone, actor_type, actor_id, idempotency_key, actor_recorded_at)
		VALUES ($1, $2, 'Picked up', 'provider', $3, $4, now())`,
		uuid.Must(uuid.NewV7()), other, provider, theKey); err != nil {
		t.Errorf("the key is scoped too widely: it refused a milestone on another job (%v)", err)
	}
}

// TestAKeyThatRecordedSomethingElseIsRefused.
//
// The database's version of the middleware's fingerprint check. Replaying the first milestone would
// tell a client that something it never sent had been recorded, which is worse than a refusal it can
// act on.
func TestAKeyThatRecordedSomethingElseIsRefused(t *testing.T) {
	pool := pgtest.DB(t)
	customer := newAccount(t, pool, "reuse-c@example.com", "+61400000672", "customer")
	provider := newAccount(t, pool, "reuse-p@example.com", "+61400000673", "provider")
	jobID := awardedJob(t, pool, customer, provider)
	svc := newTestService()

	if _, _, err := recordMilestone(t, pool, svc, provider, jobID, enRoute(theKey)); err != nil {
		t.Fatalf("the first recording: %v", err)
	}

	_, _, err := recordMilestone(t, pool, svc, provider, jobID,
		Recording{Milestone: MilestonePickedUp, Key: theKey})
	if !errors.Is(err, ErrIdempotencyKeyReused) {
		t.Fatalf("RecordMilestone(same key, other milestone) = %v, want ErrIdempotencyKeyReused", err)
	}
	if n := milestoneCount(t, pool, jobID); n != 1 {
		t.Errorf("%d milestone rows, want 1: the refused recording was written anyway", n)
	}
}

// TestAMilestoneWithNoKeyIsRefused.
//
// Unreachable through the served route — the middleware refuses first — and checked because
// uq_milestones_idempotency is partial: a row with no key is outside it, and "once per key" would be
// quietly absent rather than broken.
func TestAMilestoneWithNoKeyIsRefused(t *testing.T) {
	pool := pgtest.DB(t)
	customer := newAccount(t, pool, "nokey-c@example.com", "+61400000674", "customer")
	provider := newAccount(t, pool, "nokey-p@example.com", "+61400000675", "provider")
	jobID := awardedJob(t, pool, customer, provider)

	_, _, err := recordMilestone(t, pool, newTestService(), provider, jobID, enRoute(""))
	if !errors.Is(err, ErrNoIdempotencyKey) {
		t.Fatalf("RecordMilestone(no key) = %v, want ErrNoIdempotencyKey", err)
	}
	if n := milestoneCount(t, pool, jobID); n != 0 {
		t.Errorf("%d milestone rows were written with no key to record them against", n)
	}
}

// TestOnlyTheAwardedProviderMayRecordAMilestone is the other half of the *Done when*.
//
// Docs/02 §3 permits "the awarded provider, their assigned driver, or an administrator acting with
// an audit reason", and only the first of those can present a credential today. **The customer is
// refused like any other stranger**, which is the case worth writing down: they own the job, they
// are shown the milestones, and confirming a delivery is still not recording one.
func TestOnlyTheAwardedProviderMayRecordAMilestone(t *testing.T) {
	pool := pgtest.DB(t)
	customer := newAccount(t, pool, "actor-c@example.com", "+61400000676", "customer")
	provider := newAccount(t, pool, "actor-p@example.com", "+61400000677", "provider")
	stranger := newAccount(t, pool, "actor-x@example.com", "+61400000678", "provider")
	jobID := awardedJob(t, pool, customer, provider)
	svc := newTestService()

	for _, tc := range []struct {
		name   string
		caller uuid.UUID
		job    uuid.UUID
		want   error
	}{
		{name: "another provider", caller: stranger, job: jobID, want: ErrNotAwardedProvider},
		{name: "the job's own customer", caller: customer, job: jobID, want: ErrNotAwardedProvider},
		{name: "a job that does not exist", caller: provider, job: uuid.Must(uuid.NewV7()), want: ErrJobNotFound},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, _, err := recordMilestone(t, pool, svc, tc.caller, tc.job, enRoute(theKey+tc.name))
			if !errors.Is(err, tc.want) {
				t.Fatalf("RecordMilestone() = %v, want %v", err, tc.want)
			}
		})
	}

	if n := milestoneCount(t, pool, jobID); n != 0 {
		t.Errorf("%d milestones were recorded by callers with no right to record any", n)
	}
	if got := jobStatus(t, pool, jobID); got != string(jobs.StatusAwarded) {
		t.Errorf("the job is %q, want Awarded — a refused recording moved it", got)
	}
}

// TestDeliveredWithNeitherProofNorExceptionIsRefused is SHIP-118's *Done when*, and it is the test
// CLAUDE.md's invariant rests on: never Delivered with neither photo proof nor a recorded
// exception.
//
// **It is the mutation target for this ticket.** Removing the condition in
// [Service.RecordMilestone] leaves this failing — and leaves it failing in a specific way worth
// knowing about, because 000605's deferred trigger then refuses the same row at COMMIT. The rule
// survives; what is lost is a `delivery_proof_required` a client can act on, in exchange for a
// constraint name in a 500 (Docs/10 §4.6).
func TestDeliveredWithNeitherProofNorExceptionIsRefused(t *testing.T) {
	pool := pgtest.DB(t)
	customer := newAccount(t, pool, "proof-c@example.com", "+61400000679", "customer")
	provider := newAccount(t, pool, "proof-p@example.com", "+61400000680", "provider")
	jobID := awardedJob(t, pool, customer, provider)

	moveJob(t, pool, jobID, jobs.User(jobs.ActorProvider, provider),
		jobs.StatusEnRouteToPickup, jobs.StatusPickedUp, jobs.StatusInTransit)

	_, _, err := recordMilestone(t, pool, newTestService(), provider, jobID,
		Recording{Milestone: MilestoneDelivered, Key: theKey, RecipientName: "R. Chen", DeliveryNote: "Left with reception"})
	if !errors.Is(err, ErrProofRequired) {
		t.Fatalf("RecordMilestone(delivered) = %v, want ErrProofRequired", err)
	}

	if got := jobStatus(t, pool, jobID); got != string(jobs.StatusInTransit) {
		t.Errorf("the job is %q, want In transit — a job reached Delivered with no proof", got)
	}
	if n := milestoneCount(t, pool, jobID); n != 0 {
		t.Errorf("%d milestone rows survived a refused delivery", n)
	}
}

// TestARepeatedMilestoneIsRecordedAndMovesNothing is Docs/02 §5's failed pickup attempt.
//
// A driver reaches the pickup, finds nobody there, and records 'En route to pickup' again on the way
// back. Two keys, two intents, two rows — 000601 has no uniqueness on (job_id, milestone) precisely
// so that it can — and one transition, because the job was already there.
func TestARepeatedMilestoneIsRecordedAndMovesNothing(t *testing.T) {
	pool := pgtest.DB(t)
	customer := newAccount(t, pool, "repeat-c@example.com", "+61400000681", "customer")
	provider := newAccount(t, pool, "repeat-p@example.com", "+61400000682", "provider")
	jobID := awardedJob(t, pool, customer, provider)
	svc := newTestService()

	first, _, err := recordMilestone(t, pool, svc, provider, jobID, enRoute(theKey+"-1"))
	if err != nil {
		t.Fatalf("the first recording: %v", err)
	}

	second, outcome, err := recordMilestone(t, pool, svc, provider, jobID,
		Recording{Milestone: MilestoneEnRouteToPickup, Reason: "nobody at the gate", Key: theKey + "-2"})
	if err != nil {
		t.Fatalf("recording the same milestone again: %v", err)
	}
	if outcome != OutcomeRecorded || second.ID == first.ID {
		t.Errorf("a second attempt under a new key answered %q; it is a second record, not a retry "+
			"and not a late arrival — the job is standing exactly where this milestone says", outcome)
	}

	if n := milestoneCount(t, pool, jobID); n != 2 {
		t.Errorf("%d milestone rows, want 2 — a repeat is a record, not a duplicate", n)
	}
	if n := historyCount(t, pool, jobID, jobs.StatusEnRouteToPickup); n != 1 {
		t.Errorf("%d transitions into En route to pickup, want 1 — the job was already there", n)
	}
}

// TestAMilestoneTheJobHasMovedPastIsAbsorbedAsHistory is SHIP-112's acceptance criterion, and it is
// SHIP-111's boundary test inverted.
//
// **This test used to assert the refusal.** It was called
// TestAMilestoneTheJobHasMovedPastIsRefusedForNow, it named SHIP-112 in its own comment, and its
// milestone count was there to prove that the transaction which had to leave nothing behind did
// leave nothing behind. That count is still here and it is the assertion that changed sides: the row
// is now what must survive, because Docs/02 §3.1 says "the platform accepts the historical fact
// without moving the job backwards" and a discarded record is the defect this ticket exists to
// prevent.
//
// Both halves of the *Done when* are below, and the second is three assertions rather than one.
// "Without moving the job backwards" means the status is untouched, **no job_status_history row was
// written**, and the transition the job made honestly an hour ago is still the only one it has.
func TestAMilestoneTheJobHasMovedPastIsAbsorbedAsHistory(t *testing.T) {
	pool := pgtest.DB(t)
	customer := newAccount(t, pool, "late-c@example.com", "+61400000683", "customer")
	provider := newAccount(t, pool, "late-p@example.com", "+61400000684", "provider")
	jobID := awardedJob(t, pool, customer, provider)

	// The job has reached the pickup and set off. The driver's queued 'En route to pickup',
	// recorded an hour ago in a yard with no signal, arrives now.
	moveJob(t, pool, jobID, jobs.User(jobs.ActorProvider, provider),
		jobs.StatusEnRouteToPickup, jobs.StatusPickedUp)

	actedAt := testInstant.Add(-time.Hour)

	record, outcome, err := recordMilestone(t, pool, newTestService(), provider, jobID,
		Recording{Milestone: MilestoneEnRouteToPickup, RecordedAt: actedAt, Key: theKey})
	if err != nil {
		t.Fatalf("RecordMilestone(late) = %v, want it absorbed rather than refused (Docs/02 §3.1)", err)
	}
	if outcome != OutcomeAbsorbed {
		t.Errorf("outcome = %q, want absorbed", outcome)
	}

	// Recorded as history — the first half, and the half a client would never see if the
	// transaction had rolled back the way SHIP-111 left it.
	if n := milestoneCount(t, pool, jobID); n != 1 {
		t.Errorf("%d milestone rows, want 1 — a late milestone is kept, not discarded", n)
	}
	if !record.ActorRecordedAt.Equal(actedAt) {
		t.Errorf("actor_recorded_at = %s, want %s — the actor's clock is the truth about when a "+
			"late milestone happened, and the platform does not correct it", record.ActorRecordedAt, actedAt)
	}
	if !record.ServerRecordedAt.After(record.ActorRecordedAt) {
		t.Error("server_recorded_at is not after actor_recorded_at; absorption is the one feature " +
			"that consumes the pair SHIP-110 built, and it has been collapsed")
	}

	// The row is readable afterwards, and it is the row the service described. Read back through
	// the pool because what the service returned is not evidence that anything committed.
	var storedMilestone string
	var storedActorRecordedAt time.Time
	if err := pool.QueryRow(t.Context(),
		`SELECT milestone, actor_recorded_at FROM milestones WHERE id = $1`, record.ID).
		Scan(&storedMilestone, &storedActorRecordedAt); err != nil {
		t.Fatalf("reading the absorbed milestone back: %v", err)
	}
	if storedMilestone != string(MilestoneEnRouteToPickup) || !storedActorRecordedAt.Equal(actedAt) {
		t.Errorf("the committed row is %q at %s, want %q at %s",
			storedMilestone, storedActorRecordedAt, MilestoneEnRouteToPickup, actedAt)
	}

	// Without moving the job backwards — the other half.
	if got := jobStatus(t, pool, jobID); got != string(jobs.StatusPickedUp) {
		t.Errorf("the job is %q, want Picked up — a late milestone moved it backwards", got)
	}
	if n := historyCount(t, pool, jobID, jobs.StatusEnRouteToPickup); n != 1 {
		t.Errorf("%d transitions into En route to pickup, want the 1 the job really made: an "+
			"absorbed milestone writes no job_status_history row, because no status changed", n)
	}
	if n := historyCount(t, pool, jobID, jobs.StatusPickedUp); n != 1 {
		t.Errorf("%d transitions into Picked up, want 1 — absorbing rewrote the job's history", n)
	}
}

// TestAnAbsorbedMilestoneIsStillRecordedOncePerKey — the retry path over an absorbed row.
//
// A phone that syncs a late milestone and then loses the response retries with the same key. It must
// get the same row back and record nothing further, exactly as a milestone that moved the job does:
// absorption must not open a second way to write two rows under one key.
func TestAnAbsorbedMilestoneIsStillRecordedOncePerKey(t *testing.T) {
	pool := pgtest.DB(t)
	customer := newAccount(t, pool, "lateretry-c@example.com", "+61400000691", "customer")
	provider := newAccount(t, pool, "lateretry-p@example.com", "+61400000692", "provider")
	jobID := awardedJob(t, pool, customer, provider)
	svc := newTestService()

	moveJob(t, pool, jobID, jobs.User(jobs.ActorProvider, provider),
		jobs.StatusEnRouteToPickup, jobs.StatusPickedUp, jobs.StatusInTransit)

	late := Recording{
		Milestone:  MilestonePickedUp,
		RecordedAt: testInstant.Add(-2 * time.Hour),
		Key:        theKey,
	}

	first, outcome, err := recordMilestone(t, pool, svc, provider, jobID, late)
	if err != nil || outcome != OutcomeAbsorbed {
		t.Fatalf("the late recording = %v (outcome = %q), want it absorbed", err, outcome)
	}

	second, outcome, err := recordMilestone(t, pool, svc, provider, jobID, late)
	if err != nil {
		t.Fatalf("the retry of a late recording = %v", err)
	}
	if outcome != OutcomeAlreadyRecorded {
		t.Errorf("the retry answered %q, want already recorded", outcome)
	}
	if second.ID != first.ID {
		t.Errorf("the retry answered with %s, want the absorbed milestone %s", second.ID, first.ID)
	}

	if n := milestoneCount(t, pool, jobID); n != 1 {
		t.Errorf("%d milestone rows, want 1 — one key records one thing, absorbed or not", n)
	}
	if got := jobStatus(t, pool, jobID); got != string(jobs.StatusInTransit) {
		t.Errorf("the job is %q, want In transit", got)
	}
	if n := historyCount(t, pool, jobID, jobs.StatusPickedUp); n != 1 {
		t.Errorf("%d transitions into Picked up, want the 1 the job really made", n)
	}
}

// TestAMilestoneTheDeliveryHasNotReachedIsRefused is the other side of SHIP-112's line, and the
// reason the sentinel it used to share with the late case still exists.
//
// A premature milestone is not a historical fact that arrived out of order — it is a claim about
// something that has not happened. Refusing it costs the client a retry and nothing else, because
// the identical request succeeds as soon as the delivery reaches that point; absorbing it would
// answer "recorded" for a move that then never happens. The second half of this test is that
// recoverability, which is what makes the refusal defensible rather than merely conservative.
func TestAMilestoneTheDeliveryHasNotReachedIsRefused(t *testing.T) {
	pool := pgtest.DB(t)
	customer := newAccount(t, pool, "early-c@example.com", "+61400000693", "customer")
	provider := newAccount(t, pool, "early-p@example.com", "+61400000694", "provider")
	jobID := awardedJob(t, pool, customer, provider)
	svc := newTestService()

	moveJob(t, pool, jobID, jobs.User(jobs.ActorProvider, provider), jobs.StatusEnRouteToPickup)

	early := Recording{Milestone: MilestoneInTransit, Key: theKey}

	_, outcome, err := recordMilestone(t, pool, svc, provider, jobID, early)
	if !errors.Is(err, ErrMilestoneNotPermitted) {
		t.Fatalf("RecordMilestone(premature) = %v (outcome = %q), want ErrMilestoneNotPermitted", err, outcome)
	}
	if n := milestoneCount(t, pool, jobID); n != 0 {
		t.Errorf("%d milestone rows survived a refusal; only a late milestone is kept", n)
	}
	if got := jobStatus(t, pool, jobID); got != string(jobs.StatusEnRouteToPickup) {
		t.Errorf("the job is %q, want En route to pickup", got)
	}

	// The delivery catches up, and the same recording — same key, same milestone — now stands.
	if _, _, err := recordMilestone(t, pool, svc, provider, jobID,
		Recording{Milestone: MilestonePickedUp, Key: theKey + "-pickup"}); err != nil {
		t.Fatalf("recording the pickup: %v", err)
	}

	_, outcome, err = recordMilestone(t, pool, svc, provider, jobID, early)
	if err != nil {
		t.Fatalf("the retried premature recording = %v, want it to succeed now the job has "+
			"reached the point it describes", err)
	}
	if outcome != OutcomeRecorded {
		t.Errorf("outcome = %q, want recorded — a premature milestone is refused, not lost", outcome)
	}
	if got := jobStatus(t, pool, jobID); got != string(jobs.StatusInTransit) {
		t.Errorf("the job is %q, want In transit", got)
	}
}

// TestAbsorptionCannotReachDelivered holds CLAUDE.md's invariant against the one ticket that could
// plausibly have widened it.
//
// SHIP-112 makes a refused move keep its row, and 'Delivered' is refused for a different reason
// entirely — there is neither photo proof nor a recorded exception, and neither can be captured
// until SHIP-114…SHIP-116. That check sits in front of the insert rather than in the switch that
// absorbs, so a 'Delivered' never reaches the milestone table at all. This drives it from a job that
// has *already been Delivered*, which is the one state where "the job has moved past it" is true and
// absorption would otherwise apply.
func TestAbsorptionCannotReachDelivered(t *testing.T) {
	pool := pgtest.DB(t)
	customer := newAccount(t, pool, "deliv-c@example.com", "+61400000695", "customer")
	provider := newAccount(t, pool, "deliv-p@example.com", "+61400000696", "provider")
	jobID := awardedJob(t, pool, customer, provider)

	moveJob(t, pool, jobID, jobs.User(jobs.ActorProvider, provider),
		jobs.StatusEnRouteToPickup, jobs.StatusPickedUp, jobs.StatusInTransit,
		jobs.StatusDelivered, jobs.StatusCompleted)

	_, _, err := recordMilestone(t, pool, newTestService(), provider, jobID,
		Recording{Milestone: MilestoneDelivered, RecordedAt: testInstant.Add(-time.Hour), Key: theKey, RecipientName: "R. Chen", DeliveryNote: "Left with reception"})
	if !errors.Is(err, ErrProofRequired) {
		t.Fatalf("RecordMilestone(late delivered) = %v, want ErrProofRequired — absorption must "+
			"not become a second way to record a delivery with nothing behind it", err)
	}
	if n := milestoneCount(t, pool, jobID); n != 0 {
		t.Errorf("%d milestone rows, want 0 — a delivered milestone was absorbed with neither "+
			"photo proof nor a recorded exception", n)
	}
}

// TestADeliveryRunsThroughItsMilestones walks the three this endpoint can record.
//
// One job, three keys, three rows, three transitions. It is here because each move is a separate
// method on the port and a wrongly wired one would pass every single-milestone test above.
func TestADeliveryRunsThroughItsMilestones(t *testing.T) {
	pool := pgtest.DB(t)
	customer := newAccount(t, pool, "run-c@example.com", "+61400000685", "customer")
	provider := newAccount(t, pool, "run-p@example.com", "+61400000686", "provider")
	jobID := awardedJob(t, pool, customer, provider)
	svc := newTestService()

	for i, step := range []struct {
		milestone Milestone
		status    jobs.Status
	}{
		{MilestoneEnRouteToPickup, jobs.StatusEnRouteToPickup},
		{MilestonePickedUp, jobs.StatusPickedUp},
		{MilestoneInTransit, jobs.StatusInTransit},
	} {
		record, outcome, err := recordMilestone(t, pool, svc, provider, jobID,
			Recording{Milestone: step.milestone, Key: theKey + string(rune('a'+i))})
		if err != nil || outcome != OutcomeRecorded {
			t.Fatalf("recording %s: %v (outcome = %q)", step.milestone, err, outcome)
		}
		if record.Milestone != step.milestone {
			t.Fatalf("recorded %q, want %q", record.Milestone, step.milestone)
		}
		if got := jobStatus(t, pool, jobID); got != string(step.status) {
			t.Fatalf("after %s the job is %q, want %q", step.milestone, got, step.status)
		}
	}

	if n := milestoneCount(t, pool, jobID); n != 3 {
		t.Errorf("%d milestone rows after three recordings", n)
	}
}

// TestRecordMilestoneRefusesAPool.
//
// The milestone and the transition are one act in two tables. Handed a pool, the first would commit
// on its own and leave a delivery whose timeline and status disagree.
func TestRecordMilestoneRefusesAPool(t *testing.T) {
	pool := pgtest.DB(t)
	customer := newAccount(t, pool, "pool-c@example.com", "+61400000687", "customer")
	provider := newAccount(t, pool, "pool-p@example.com", "+61400000688", "provider")
	jobID := awardedJob(t, pool, customer, provider)

	_, _, err := newTestService().RecordMilestone(t.Context(), pool, provider, jobID, enRoute(theKey))
	if !errors.Is(err, ErrNotInTransaction) {
		t.Fatalf("RecordMilestone(pool) = %v, want ErrNotInTransaction", err)
	}
}

// TestAnUnrecognisedOutcomeFailsARecording is the zero value of JobMove, on this path too.
func TestAnUnrecognisedOutcomeFailsARecording(t *testing.T) {
	pool := pgtest.DB(t)
	customer := newAccount(t, pool, "unknown-mc@example.com", "+61400000689", "customer")
	provider := newAccount(t, pool, "unknown-mp@example.com", "+61400000690", "provider")
	jobID := awardedJob(t, pool, customer, provider)

	svc := newTestServiceWith(staticJobs{move: JobMoveUnrecognised})

	_, _, err := recordMilestone(t, pool, svc, provider, jobID, enRoute(theKey))
	if !errors.Is(err, ErrJobMoveUnrecognised) {
		t.Fatalf("RecordMilestone() = %v, want ErrJobMoveUnrecognised", err)
	}
	if n := milestoneCount(t, pool, jobID); n != 0 {
		t.Errorf("%d milestone rows survived an unrecognised outcome", n)
	}
}

// TestRecordingValidation covers what a client can get wrong before anything is read.
func TestRecordingValidation(t *testing.T) {
	for _, tc := range []struct {
		name  string
		given Recording
		field string
	}{
		{name: "no milestone", given: Recording{Key: theKey}, field: "milestone"},
		{name: "not a milestone", given: Recording{Milestone: "Unloaded", Key: theKey}, field: "milestone"},
		{
			name:  "driver assigned has an endpoint of its own",
			given: Recording{Milestone: MilestoneDriverAssigned, Key: theKey},
			field: "milestone",
		},
		{
			name:  "a reason longer than the column should hold",
			given: Recording{Milestone: MilestonePickedUp, Reason: strings.Repeat("a", maxMilestoneReason+1), Key: theKey},
			field: "reason",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			problems := tc.given.normalise().problems()
			if !problems.Any() {
				t.Fatalf("%#v was accepted", tc.given)
			}
			for _, f := range problems.Fields() {
				if f.Field == tc.field {
					return
				}
			}
			t.Errorf("problems are %v, want one about %s", problems.Fields(), tc.field)
		})
	}
}
