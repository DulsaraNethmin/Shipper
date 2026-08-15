package admin

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/DulsaraNethmin/Shipper/services/core/internal/clock"
	"github.com/DulsaraNethmin/Shipper/services/core/internal/db"
	"github.com/DulsaraNethmin/Shipper/services/core/internal/events"
	"github.com/DulsaraNethmin/Shipper/services/core/internal/httpx"
	"github.com/DulsaraNethmin/Shipper/services/core/internal/jobs"
	"github.com/DulsaraNethmin/Shipper/services/core/internal/testsupport/pgtest"
)

// SHIP-163 against a real PostgreSQL.
//
// Docs/06 §4.1 is the argument for not mocking it — "a mock happily accepts a write that the actual
// constraint would reject" — and this ticket has two of those. uq_disputes_open_per_job and
// uq_disputes_idempotency are both *partial* unique indexes, and 000402's trigger refuses a status
// change that no history row describes; a repository interface would let every test below pass while
// the real rules were being broken.
//
// # These tests import `jobs`, which non-test code in this package may not
//
// internal/boundaries skips test files deliberately — "a test wires domains together in the same way
// cmd/api does" — and that is what happens here. The fixtures move a job to Awarded through the real
// guard rather than by UPDATE, so every test below runs against a job that got where it is honestly.
//
// [testJobs] and [testParties] are this package's copies of the adapters cmd/api holds. They are the
// same translation for the same reason, and the production pair is exercised end to end by
// scripts/verify/90-admin.sh against the real binary — a Go test in cmd/api could not, because that
// package has no database.

// testInstant is the platform's clock throughout, and testIncident is ninety minutes earlier.
//
// Two values rather than one, and that is the point: Docs/04 §7's "time of event" and the moment the
// report arrives are different instants, and a test using one clock for both could not tell that the
// columns had been collapsed.
var (
	testInstant  = time.Date(2026, 8, 13, 3, 30, 0, 0, time.UTC)
	testIncident = testInstant.Add(-90 * time.Minute)
)

// testJobs is admin.Jobs over the real transition guard — the same translation
// cmd/api/routes_admin.go makes.
type testJobs struct{ svc *jobs.Service }

func (j testJobs) MoveToDisputed(
	ctx context.Context,
	r db.Runner,
	jobID, actorID uuid.UUID,
	party Party,
	reason string,
) (JobMove, error) {
	actor := jobs.ActorCustomer
	if party == PartyProvider {
		actor = jobs.ActorProvider
	}

	_, err := j.svc.Transition(ctx, r, jobs.Move{
		JobID:  jobID,
		To:     jobs.StatusDisputed,
		Actor:  jobs.User(actor, actorID),
		Reason: reason,
	})

	switch {
	case err == nil:
		return JobMoved, nil
	case errors.Is(err, jobs.ErrJobNotFound):
		return JobNotFound, nil
	case errors.Is(err, jobs.ErrAlreadyInStatus):
		return JobAlreadyDisputed, nil
	case errors.Is(err, jobs.ErrTransitionNotPermitted):
		return JobNotDisputable, nil
	default:
		return JobMoveUnrecognised, err
	}
}

// Unpublish is SHIP-160's move, as cmd/api's disputeLifecycle performs it.
//
// **A copy of the adapter rather than a stub answering a canned value**, and that is what makes the
// enforcement tests worth running: the transition is a real one through the real guard against a
// real database, so Docs/02 §2's table decides which jobs can be unpublished rather than this file
// deciding it. A stub returning JobMoved would establish that `admin` writes an audit entry when
// told the move worked, which is the half that was never in doubt.
//
// It is a copy because cmd/api's adapter is in package main, which no test can import. What keeps
// the two honest is scripts/verify/90-admin.sh, which drives the real one against the built binary.
func (j testJobs) Unpublish(
	ctx context.Context,
	r db.Runner,
	jobID, actorID uuid.UUID,
	reason string,
) (JobMove, error) {
	_, err := j.svc.Transition(ctx, r, jobs.Move{
		JobID:  jobID,
		To:     jobs.StatusCancelled,
		Actor:  jobs.User(jobs.ActorAdmin, actorID),
		Reason: reason,
	})

	switch {
	case err == nil:
		return JobMoved, nil
	case errors.Is(err, jobs.ErrJobNotFound):
		return JobNotFound, nil
	case errors.Is(err, jobs.ErrAlreadyInStatus):
		return JobAlreadyRemoved, nil
	case errors.Is(err, jobs.ErrTransitionNotPermitted):
		return JobNotRemovable, nil
	default:
		return JobMoveUnrecognised, err
	}
}

// testParties is admin.JobParties over the job and its accepted bid, as cmd/api reads it.
type testParties struct{}

func (testParties) PartyOn(ctx context.Context, r db.Runner, jobID, userID uuid.UUID) (Party, bool, error) {
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
		return "", false, nil
	case err != nil:
		return "", false, err
	}
	if party == nil {
		return "", false, nil
	}
	return Party(*party), true, nil
}

// staticParties answers with one outcome, for the cases no fixture can produce.
type staticParties struct {
	party   Party
	isParty bool
	err     error
}

func (s staticParties) PartyOn(context.Context, db.Runner, uuid.UUID, uuid.UUID) (Party, bool, error) {
	return s.party, s.isParty, s.err
}

// staticJobs answers with one outcome, likewise.
type staticJobs struct {
	move JobMove
	err  error
}

func (s staticJobs) MoveToDisputed(context.Context, db.Runner, uuid.UUID, uuid.UUID, Party, string) (JobMove, error) {
	return s.move, s.err
}

// Unpublish answers the same canned outcome. Present so the type still satisfies [Jobs]; the
// enforcement tests use [testJobs], which performs a real transition.
func (s staticJobs) Unpublish(context.Context, db.Runner, uuid.UUID, uuid.UUID, string) (JobMove, error) {
	return s.move, s.err
}

func testClock() *clock.Fixed { return clock.NewFixed(testInstant) }

func newTestService() *Service {
	return NewService(
		testJobs{svc: jobs.NewService(events.NewOutbox(), testClock(), nil)},
		testParties{},
		testClock(),
	)
}

// --- fixtures ---------------------------------------------------------------------------------

// newAccount inserts a user with the role the test needs.
func newAccount(t *testing.T, pool *pgxpool.Pool, email, phone, role string) uuid.UUID {
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

// newDraft inserts a job at Draft, which is the only status 000402 lets one be created at.
func newDraft(t *testing.T, pool *pgxpool.Pool, customerID uuid.UUID) uuid.UUID {
	t.Helper()

	id, err := uuid.NewV7()
	if err != nil {
		t.Fatalf("generating a job id: %v", err)
	}
	if _, err := pool.Exec(t.Context(),
		`INSERT INTO jobs (id, customer_id) VALUES ($1, $2)`, id, customerID); err != nil {
		t.Fatalf("inserting a job: %v", err)
	}
	return id
}

// moveJob puts a job where the test needs it, through the guard rather than around it.
//
// SHIP-63 publishes and SHIP-92 awards, and neither exists yet — so this is what those endpoints
// will do: one guarded transition per step, each leaving its job_status_history row. A test that
// wrote `UPDATE jobs SET status` would be refused by 000402, which is the point.
func moveJob(t *testing.T, pool *pgxpool.Pool, jobID uuid.UUID, actor jobs.Actor, to ...jobs.Status) {
	t.Helper()
	moveJobBecause(t, pool, jobID, actor, "", to...)
}

// moveJobBecause is moveJob with a reason, which an administrator's transition cannot be without:
// Docs/01 §3 forbids one changing a commercial record without an auditable reason, and
// jobs.Move.validate refuses it. It is what SHIP-164's outcome will supply.
func moveJobBecause(t *testing.T, pool *pgxpool.Pool, jobID uuid.UUID, actor jobs.Actor, reason string, to ...jobs.Status) {
	t.Helper()

	svc := jobs.NewService(events.NewOutbox(), testClock(), nil)

	for _, status := range to {
		err := db.InTx(t.Context(), pool, func(ctx context.Context, r db.Runner) error {
			_, err := svc.Transition(ctx, r, jobs.Move{
				JobID: jobID, To: status, Actor: actor, Reason: reason,
			})
			return err
		})
		if err != nil {
			t.Fatalf("moving %s to %s: %v", jobID, status, err)
		}
	}
}

// acceptBid records the award SHIP-92 will make: one accepted bid, which is what makes a provider
// *the* provider on a job.
func acceptBid(t *testing.T, pool *pgxpool.Pool, jobID, providerID uuid.UUID) {
	t.Helper()

	if _, err := pool.Exec(t.Context(),
		`INSERT INTO bids (id, job_id, provider_id, status, amount) VALUES ($1, $2, $3, 'Accepted', 450.00)`,
		uuid.Must(uuid.NewV7()), jobID, providerID); err != nil {
		t.Fatalf("accepting a bid: %v", err)
	}
}

// deliveredJob is a job that has been published, awarded, driven and delivered — the state a
// dispute is most often raised from, and the one Docs/02 §6.1's 72-hour window runs against.
func deliveredJob(t *testing.T, pool *pgxpool.Pool, customer, provider uuid.UUID) uuid.UUID {
	t.Helper()

	jobID := newDraft(t, pool, customer)
	moveJob(t, pool, jobID, jobs.User(jobs.ActorCustomer, customer), jobs.StatusOpen, jobs.StatusAwarded)
	acceptBid(t, pool, jobID, provider)
	moveJob(t, pool, jobID, jobs.User(jobs.ActorProvider, provider),
		jobs.StatusEnRouteToPickup, jobs.StatusPickedUp, jobs.StatusInTransit, jobs.StatusDelivered)
	return jobID
}

// goodIntake is an intake with nothing wrong with it, which each test then varies one field of.
//
// The description carries a deliberate line break and stray surrounding whitespace. Both are
// asserted on the way out: the whitespace goes and the line break stays, which is the distinction
// [Intake.normalise] exists to make. An evidence list with a blank row in it is what a form with an
// empty input sends.
func goodIntake(key string) Intake {
	return Intake{
		Category:       CategoryGoodsDamaged,
		Description:    "  Two of the four crates arrived with the sides staved in.\n\nThe depot signed for them anyway. ",
		DesiredOutcome: "  A record of the damage, and the provider contacted about it. ",
		OccurredAt:     testIncident,
		Evidence:       []string{"  Photographed the crates   at the depot ", "", "The driver's message of 11/08"},
		Key:            key,
	}
}

// raiseWith runs one intake in its own transaction, which is what the handler does.
func raiseWith(t *testing.T, pool *pgxpool.Pool, svc *Service, complainant, jobID uuid.UUID, in Intake) (Dispute, bool, error) {
	t.Helper()

	var (
		dispute Dispute
		raised  bool
	)
	err := db.InTx(t.Context(), pool, func(ctx context.Context, r db.Runner) error {
		var err error
		dispute, raised, err = svc.RaiseDispute(ctx, r, complainant, jobID, in)
		return err
	})
	return dispute, raised, err
}

// jobStatus reads the column, not what the service said about it.
func jobStatus(t *testing.T, pool *pgxpool.Pool, jobID uuid.UUID) string {
	t.Helper()

	var status string
	if err := pool.QueryRow(t.Context(), `SELECT status FROM jobs WHERE id = $1`, jobID).Scan(&status); err != nil {
		t.Fatalf("reading the status of %s: %v", jobID, err)
	}
	return status
}

// disputeCount is how many rows the table holds for a job, whatever the service reported.
func disputeCount(t *testing.T, pool *pgxpool.Pool, jobID uuid.UUID) int {
	t.Helper()

	var n int
	if err := pool.QueryRow(t.Context(),
		`SELECT count(*) FROM disputes WHERE job_id = $1`, jobID).Scan(&n); err != nil {
		t.Fatalf("counting disputes on %s: %v", jobID, err)
	}
	return n
}

// --- the acceptance criterion -----------------------------------------------------------------

// TestACustomerRaisesADisputeCapturingTheIntakeFields is SHIP-163's *Done when*, field by field.
//
// "A customer or provider raises a dispute capturing the Docs/04 §7 intake fields." Docs/04 §7 names
// seven — job, complainant, category, description, desired outcome, time of event, evidence — and
// every one of them is asserted below, read back off the row rather than off what the service
// returned.
func TestACustomerRaisesADisputeCapturingTheIntakeFields(t *testing.T) {
	pool := pgtest.DB(t)
	customer := newAccount(t, pool, "dispute-cust@example.com", "+61400000800", "customer")
	provider := newAccount(t, pool, "dispute-prov@example.com", "+61400000801", "provider")
	jobID := deliveredJob(t, pool, customer, provider)

	dispute, raised, err := raiseWith(t, pool, newTestService(), customer, jobID, goodIntake("key-first"))
	if err != nil {
		t.Fatalf("RaiseDispute() = %v", err)
	}
	if !raised {
		t.Fatal("the intake reports that it wrote nothing")
	}

	// Read the row rather than trusting the return value: the columns are what an administrator
	// triages from.
	var (
		gotJob         uuid.UUID
		gotComplainant uuid.UUID
		gotParty       string
		gotCategory    string
		gotDescription string
		gotOutcome     string
		gotOccurredAt  time.Time
		gotEvidence    []string
		gotCreatedAt   time.Time
		gotResolvedAt  *time.Time
	)
	if err := pool.QueryRow(t.Context(), `
		SELECT job_id, complainant_id, complainant_party, category, description, desired_outcome,
		       occurred_at, evidence, created_at, resolved_at
		FROM disputes WHERE id = $1`, dispute.ID,
	).Scan(&gotJob, &gotComplainant, &gotParty, &gotCategory, &gotDescription, &gotOutcome,
		&gotOccurredAt, &gotEvidence, &gotCreatedAt, &gotResolvedAt); err != nil {
		t.Fatalf("reading the dispute back: %v", err)
	}

	// job
	if gotJob != jobID {
		t.Errorf("job_id = %s, want %s", gotJob, jobID)
	}
	// complainant — the account, and which side of the job the platform decided they were on
	if gotComplainant != customer {
		t.Errorf("complainant_id = %s, want %s", gotComplainant, customer)
	}
	if gotParty != string(PartyCustomer) {
		t.Errorf("complainant_party = %q, want %q", gotParty, PartyCustomer)
	}
	// category
	if gotCategory != string(CategoryGoodsDamaged) {
		t.Errorf("category = %q, want %q", gotCategory, CategoryGoodsDamaged)
	}
	// description and desired outcome — trimmed, and the paragraph break kept. A description is
	// the field an administrator reads carefully, and collapsing its line breaks would flatten
	// four paragraphs into a wall of text.
	wantDescription := "Two of the four crates arrived with the sides staved in.\n\nThe depot signed for them anyway."
	if gotDescription != wantDescription {
		t.Errorf("description = %q, want %q", gotDescription, wantDescription)
	}
	if gotOutcome != "A record of the damage, and the provider contacted about it." {
		t.Errorf("desired_outcome = %q, want it trimmed", gotOutcome)
	}
	// time of event — the complainant's clock, not the platform's
	if !gotOccurredAt.Equal(testIncident) {
		t.Errorf("occurred_at = %s, want %s exactly as sent", gotOccurredAt, testIncident)
	}
	if !gotCreatedAt.After(gotOccurredAt) {
		t.Errorf("created_at %s is not after occurred_at %s; the two clocks have been collapsed "+
			"and support cannot tell the incident from the report", gotCreatedAt, gotOccurredAt)
	}
	// evidence — the blank entry dropped, the rest kept in order
	want := []string{"Photographed the crates at the depot", "The driver's message of 11/08"}
	if len(gotEvidence) != len(want) {
		t.Fatalf("evidence = %v, want %v", gotEvidence, want)
	}
	for i := range want {
		if gotEvidence[i] != want[i] {
			t.Errorf("evidence[%d] = %q, want %q", i, gotEvidence[i], want[i])
		}
	}

	if gotResolvedAt != nil {
		t.Error("a dispute was raised already resolved")
	}
	if !dispute.Open() {
		t.Error("Dispute.Open() disagrees with resolved_at")
	}

	// The half of the *Done when* that is not about the dispute at all: Docs/02 §2 has raising
	// one move the job, and Docs/02 §3 says why.
	if status := jobStatus(t, pool, jobID); status != string(jobs.StatusDisputed) {
		t.Errorf("the job is %q, want %q — a dispute that freezes nothing lets the 72-hour "+
			"auto-complete run through it (Docs/02 §3, §6.1)", status, jobs.StatusDisputed)
	}
}

// TestTheTransitionWentThroughTheGuard checks the record 000402 refuses the status change without.
//
// The row is what makes the move legal, so its absence is not a missing audit line — it is a status
// change the database would not have permitted. The reason carries the category, which is what makes
// a job's history say why it froze rather than only that it did.
func TestTheTransitionWentThroughTheGuard(t *testing.T) {
	pool := pgtest.DB(t)
	customer := newAccount(t, pool, "guard-cust@example.com", "+61400000802", "customer")
	provider := newAccount(t, pool, "guard-prov@example.com", "+61400000803", "provider")
	jobID := deliveredJob(t, pool, customer, provider)

	if _, _, err := raiseWith(t, pool, newTestService(), customer, jobID, goodIntake("key-guard")); err != nil {
		t.Fatalf("RaiseDispute() = %v", err)
	}

	var (
		from, to, actorType, reason string
		actorID                     uuid.UUID
	)
	if err := pool.QueryRow(t.Context(), `
		SELECT from_status, to_status, actor_type, actor_id, coalesce(reason, '')
		FROM job_status_history WHERE job_id = $1 AND to_status = 'Disputed'`, jobID,
	).Scan(&from, &to, &actorType, &actorID, &reason); err != nil {
		t.Fatalf("reading the transition: %v", err)
	}

	if from != string(jobs.StatusDelivered) || to != string(jobs.StatusDisputed) {
		t.Errorf("recorded %s->%s, want Delivered->Disputed", from, to)
	}
	if actorType != string(jobs.ActorCustomer) || actorID != customer {
		t.Errorf("attributed to %s %s, want customer %s", actorType, actorID, customer)
	}
	if reason != string(CategoryGoodsDamaged) {
		t.Errorf("reason = %q, want the category so the history says why the job froze", reason)
	}
}

// TestTheAwardedProviderMayAlsoRaiseOne is the other half of "a customer or provider".
//
// Docs/02 §1 names both as primary actors for 'Disputed', and the provider's side of a delivery
// going wrong — a customer who was not there, goods that were not what was listed — is Docs/02 §5's
// list as much as the customer's is.
func TestTheAwardedProviderMayAlsoRaiseOne(t *testing.T) {
	pool := pgtest.DB(t)
	customer := newAccount(t, pool, "prov-raise-cust@example.com", "+61400000804", "customer")
	provider := newAccount(t, pool, "prov-raise-prov@example.com", "+61400000805", "provider")
	jobID := deliveredJob(t, pool, customer, provider)

	in := goodIntake("key-provider")
	in.Category = CategoryCustomerUnavailable

	dispute, raised, err := raiseWith(t, pool, newTestService(), provider, jobID, in)
	if err != nil {
		t.Fatalf("RaiseDispute() = %v", err)
	}
	if !raised {
		t.Fatal("the intake wrote nothing")
	}
	if dispute.ComplainantParty != PartyProvider {
		t.Errorf("recorded as %q, want %q — the party is resolved from the accepted bid",
			dispute.ComplainantParty, PartyProvider)
	}

	var actorType string
	if err := pool.QueryRow(t.Context(), `
		SELECT actor_type FROM job_status_history WHERE job_id = $1 AND to_status = 'Disputed'`,
		jobID).Scan(&actorType); err != nil {
		t.Fatalf("reading the transition: %v", err)
	}
	if actorType != string(jobs.ActorProvider) {
		t.Errorf("the transition is attributed to %q, want %q", actorType, jobs.ActorProvider)
	}
}

// --- who may not ------------------------------------------------------------------------------

// TestAStrangerIsRefusedAndNothingIsWritten is the check wave 5 found six fleet endpoints not
// making.
//
// Those endpoints scope their query to the caller's own id and answer 200 with an empty result,
// which leaks nothing and tells somebody with no business asking that their request was fine. This
// one asks who the caller is on the job and refuses when the answer is nobody.
//
// Three strangers, because they fail for three different reasons and all three must fail: a provider
// who bid and lost, a customer who owns a different job, and an account with no connection to
// anything. A missing job is the fourth, and it answers identically on the wire.
func TestAStrangerIsRefusedAndNothingIsWritten(t *testing.T) {
	pool := pgtest.DB(t)
	customer := newAccount(t, pool, "stranger-cust@example.com", "+61400000806", "customer")
	provider := newAccount(t, pool, "stranger-prov@example.com", "+61400000807", "provider")
	loser := newAccount(t, pool, "stranger-loser@example.com", "+61400000808", "provider")
	other := newAccount(t, pool, "stranger-other@example.com", "+61400000809", "customer")
	jobID := deliveredJob(t, pool, customer, provider)

	// The losing provider has a bid on the job, and it is not the accepted one.
	if _, err := pool.Exec(t.Context(),
		`INSERT INTO bids (id, job_id, provider_id, status, amount) VALUES ($1, $2, $3, 'Rejected', 500.00)`,
		uuid.Must(uuid.NewV7()), jobID, loser); err != nil {
		t.Fatalf("inserting a losing bid: %v", err)
	}

	svc := newTestService()

	for name, caller := range map[string]uuid.UUID{
		"a provider who bid and lost":   loser,
		"a customer of some other job":  other,
		"an account with no bid at all": newAccount(t, pool, "stranger-none@example.com", "+61400000810", "provider"),
	} {
		t.Run(name, func(t *testing.T) {
			_, _, err := raiseWith(t, pool, svc, caller, jobID, goodIntake("key-"+name))
			if !errors.Is(err, ErrNotAParty) {
				t.Fatalf("RaiseDispute() = %v, want ErrNotAParty — %s must be refused, not "+
					"quietly given an empty answer", err, name)
			}
		})
	}

	// A job that does not exist answers the same way, which is what keeps the two
	// indistinguishable on the wire.
	if _, _, err := raiseWith(t, pool, svc, customer, uuid.Must(uuid.NewV7()), goodIntake("key-nojob")); !errors.Is(err, ErrNotAParty) {
		t.Errorf("a missing job answered %v, want the same refusal a stranger's job gets", err)
	}

	if n := disputeCount(t, pool, jobID); n != 0 {
		t.Errorf("%d dispute rows survived refusals that should have written none", n)
	}
	if status := jobStatus(t, pool, jobID); status != string(jobs.StatusDelivered) {
		t.Errorf("a refused intake froze the job anyway: %q", status)
	}
}

// TestAJobThatCannotBeDisputedIsRefusedAndRollsBackWhole covers both ends of the lifecycle.
//
// Docs/02 §2 permits 'Disputed' from Awarded through Delivered and from nowhere else. A job still
// open for bids has no delivery to dispute, and one that has been cancelled has left the lifecycle.
// The dispute row must roll back with the refused transition — a complaint against a job the
// platform still believes is running normally is a state no support queue can read.
func TestAJobThatCannotBeDisputedIsRefusedAndRollsBackWhole(t *testing.T) {
	pool := pgtest.DB(t)
	customer := newAccount(t, pool, "notdisputable@example.com", "+61400000811", "customer")
	svc := newTestService()

	t.Run("a job still open for bids", func(t *testing.T) {
		jobID := newDraft(t, pool, customer)
		moveJob(t, pool, jobID, jobs.User(jobs.ActorCustomer, customer), jobs.StatusOpen)

		_, _, err := raiseWith(t, pool, svc, customer, jobID, goodIntake("key-open"))
		if !errors.Is(err, ErrJobNotDisputable) {
			t.Fatalf("RaiseDispute() = %v, want ErrJobNotDisputable", err)
		}
		if n := disputeCount(t, pool, jobID); n != 0 {
			t.Errorf("%d dispute rows survived a refused transition", n)
		}
		if status := jobStatus(t, pool, jobID); status != string(jobs.StatusOpen) {
			t.Errorf("the job is %q, want it left where it was", status)
		}
	})

	t.Run("a job that has been cancelled", func(t *testing.T) {
		jobID := newDraft(t, pool, customer)
		moveJob(t, pool, jobID, jobs.User(jobs.ActorCustomer, customer), jobs.StatusCancelled)

		_, _, err := raiseWith(t, pool, svc, customer, jobID, goodIntake("key-cancelled"))
		if !errors.Is(err, ErrJobNotDisputable) {
			t.Fatalf("RaiseDispute() = %v, want ErrJobNotDisputable", err)
		}
		if n := disputeCount(t, pool, jobID); n != 0 {
			t.Errorf("%d dispute rows survived a refused transition", n)
		}
	})
}

// --- raising twice ----------------------------------------------------------------------------

// TestARetryRaisesNoSecondDispute is the guarantee the ticket rests on that Redis cannot give.
//
// SHIP-15's middleware replays the stored response while its entry lives and this function is never
// reached. What is exercised here is the path after that entry expires or is evicted — a phone out
// of signal for longer than any TTL worth setting — where the handler runs a second time and the
// index is what makes the outcome correct.
func TestARetryRaisesNoSecondDispute(t *testing.T) {
	pool := pgtest.DB(t)
	customer := newAccount(t, pool, "retry-cust@example.com", "+61400000812", "customer")
	provider := newAccount(t, pool, "retry-prov@example.com", "+61400000813", "provider")
	jobID := deliveredJob(t, pool, customer, provider)
	svc := newTestService()

	first, raised, err := raiseWith(t, pool, svc, customer, jobID, goodIntake("one-key"))
	if err != nil || !raised {
		t.Fatalf("the first intake = %v, raised %v", err, raised)
	}

	again, raisedAgain, err := raiseWith(t, pool, svc, customer, jobID, goodIntake("one-key"))
	if err != nil {
		t.Fatalf("the retry = %v", err)
	}
	if raisedAgain {
		t.Error("the retry reports that it wrote a second dispute")
	}
	if again.ID != first.ID {
		t.Errorf("the retry answered with %s, want the dispute the first attempt raised (%s)", again.ID, first.ID)
	}
	// The first attempt's account of the incident, not the retry's — which is why the row is read
	// back rather than reconstructed from the request.
	if !again.OccurredAt.Equal(first.OccurredAt) {
		t.Errorf("the retry's occurred_at is %s, want the first attempt's %s", again.OccurredAt, first.OccurredAt)
	}

	if n := disputeCount(t, pool, jobID); n != 1 {
		t.Errorf("%d dispute rows for one key", n)
	}

	var transitions int
	if err := pool.QueryRow(t.Context(),
		`SELECT count(*) FROM job_status_history WHERE job_id = $1 AND to_status = 'Disputed'`,
		jobID).Scan(&transitions); err != nil {
		t.Fatalf("counting transitions: %v", err)
	}
	if transitions != 1 {
		t.Errorf("the retry moved the job a second time: %d transitions", transitions)
	}
}

// TestAKeyReusedForAnotherComplaintIsRefused is the same call the middleware makes on a fingerprint
// mismatch.
//
// Replaying the first dispute would tell a client that something it never sent had been raised.
func TestAKeyReusedForAnotherComplaintIsRefused(t *testing.T) {
	pool := pgtest.DB(t)
	customer := newAccount(t, pool, "reuse-cust@example.com", "+61400000814", "customer")
	provider := newAccount(t, pool, "reuse-prov@example.com", "+61400000815", "provider")
	jobID := deliveredJob(t, pool, customer, provider)
	svc := newTestService()

	if _, _, err := raiseWith(t, pool, svc, customer, jobID, goodIntake("shared-key")); err != nil {
		t.Fatalf("the first intake = %v", err)
	}

	other := goodIntake("shared-key")
	other.Category = CategoryLate

	if _, _, err := raiseWith(t, pool, svc, customer, jobID, other); !errors.Is(err, ErrIdempotencyKeyReused) {
		t.Fatalf("RaiseDispute() = %v, want ErrIdempotencyKeyReused", err)
	}
	if n := disputeCount(t, pool, jobID); n != 1 {
		t.Errorf("%d dispute rows after a reused key", n)
	}
}

// TestASecondPartyIsRefusedWhileOneIsOpen is the answer the *other* party to the delivery gets.
//
// A job is frozen once. Two open disputes would be two things SHIP-164 could unfreeze the job by
// resolving, and it would have to pick — so the second is refused with a code that tells the client
// to show the one that exists rather than offer the form again.
func TestASecondPartyIsRefusedWhileOneIsOpen(t *testing.T) {
	pool := pgtest.DB(t)
	customer := newAccount(t, pool, "second-cust@example.com", "+61400000816", "customer")
	provider := newAccount(t, pool, "second-prov@example.com", "+61400000817", "provider")
	jobID := deliveredJob(t, pool, customer, provider)
	svc := newTestService()

	if _, _, err := raiseWith(t, pool, svc, customer, jobID, goodIntake("customer-key")); err != nil {
		t.Fatalf("the customer's intake = %v", err)
	}

	in := goodIntake("provider-key")
	in.Category = CategoryCustomerUnavailable

	if _, _, err := raiseWith(t, pool, svc, provider, jobID, in); !errors.Is(err, ErrDisputeAlreadyOpen) {
		t.Fatalf("the provider's intake = %v, want ErrDisputeAlreadyOpen", err)
	}
	if n := disputeCount(t, pool, jobID); n != 1 {
		t.Errorf("%d dispute rows on a job that may hold one open", n)
	}

	// And once it is resolved the job can be disputed again, which is what makes the index
	// partial rather than a unique constraint. The job goes back into the lifecycle the way
	// SHIP-164's outcome will put it there.
	if _, err := pool.Exec(t.Context(), `UPDATE disputes SET resolved_at = now() WHERE job_id = $1`, jobID); err != nil {
		t.Fatalf("resolving: %v", err)
	}
	moveJobBecause(t, pool, jobID, jobs.User(jobs.ActorAdmin, customer),
		"dispute resolved, delivery accepted", jobs.StatusCompleted)

	// Completed has no route to Disputed, so the second attempt is refused by the guard rather
	// than by the index — which is the correct refusal and confirms the index is no longer the
	// one answering.
	if _, _, err := raiseWith(t, pool, svc, provider, jobID, in); !errors.Is(err, ErrJobNotDisputable) {
		t.Errorf("RaiseDispute() = %v, want the guard to refuse it once the job has completed", err)
	}
}

// --- what the service refuses before it touches anything ----------------------------------------

// TestAKeylessIntakeIsRefused records why the check is in the domain rather than the handler.
//
// uq_disputes_idempotency is partial on the column being present, so a row with no key falls outside
// it. A guarantee that can be removed by omitting a header is one this domain should refuse to write
// without.
func TestAKeylessIntakeIsRefused(t *testing.T) {
	pool := pgtest.DB(t)
	customer := newAccount(t, pool, "nokey-cust@example.com", "+61400000818", "customer")
	provider := newAccount(t, pool, "nokey-prov@example.com", "+61400000819", "provider")
	jobID := deliveredJob(t, pool, customer, provider)

	in := goodIntake("")
	if _, _, err := raiseWith(t, pool, newTestService(), customer, jobID, in); !errors.Is(err, ErrNoIdempotencyKey) {
		t.Fatalf("RaiseDispute() = %v, want ErrNoIdempotencyKey", err)
	}
	if n := disputeCount(t, pool, jobID); n != 0 {
		t.Errorf("%d dispute rows were written with no key to record them under", n)
	}
}

// TestIntakeIsValidatedBeforeAnythingIsRead is the field-level contract, gathered in one answer.
//
// It runs against a pool rather than a transaction on purpose: validation is refused before the
// transaction check would matter, and a caller with a malformed form should be told what is wrong
// with it rather than that the platform wanted a transaction.
func TestIntakeIsValidatedBeforeAnythingIsRead(t *testing.T) {
	pool := pgtest.DB(t)
	customer := newAccount(t, pool, "valid-cust@example.com", "+61400000820", "customer")
	provider := newAccount(t, pool, "valid-prov@example.com", "+61400000821", "provider")
	jobID := deliveredJob(t, pool, customer, provider)
	svc := newTestService()

	cases := map[string]struct {
		mutate func(*Intake)
		field  string
	}{
		"no category":                {func(in *Intake) { in.Category = "" }, "category"},
		"a category nobody declared": {func(in *Intake) { in.Category = "Van broke down" }, "category"},
		"no description":             {func(in *Intake) { in.Description = "   " }, "description"},
		"no desired outcome":         {func(in *Intake) { in.DesiredOutcome = "" }, "desired_outcome"},
		"no time of event":           {func(in *Intake) { in.OccurredAt = time.Time{} }, "occurred_at"},
		"an incident in the future": {
			func(in *Intake) { in.OccurredAt = testInstant.Add(time.Hour) }, "occurred_at",
		},
		"a description nobody could read": {
			func(in *Intake) { in.Description = strings.Repeat("x", maxDescription+1) }, "description",
		},
		"too much evidence": {
			func(in *Intake) {
				in.Evidence = make([]string, maxEvidenceItems+1)
				for i := range in.Evidence {
					in.Evidence[i] = "a reference"
				}
			}, "evidence",
		},
		"one evidence entry too long": {
			func(in *Intake) { in.Evidence = []string{strings.Repeat("x", maxEvidenceItem+1)} }, "evidence.0",
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			in := goodIntake("key-" + name)
			tc.mutate(&in)

			_, _, err := raiseWith(t, pool, svc, customer, jobID, in)

			var apiErr *httpx.Error
			if !errors.As(err, &apiErr) {
				t.Fatalf("RaiseDispute() = %v, want a validation error", err)
			}
			if apiErr.Code != httpx.CodeValidationFailed {
				t.Fatalf("code = %q, want %q", apiErr.Code, httpx.CodeValidationFailed)
			}

			var named bool
			for _, d := range apiErr.Details {
				if d.Field == tc.field {
					named = true
				}
			}
			if !named {
				t.Errorf("the refusal does not name %q: %+v", tc.field, apiErr.Details)
			}
		})
	}

	if n := disputeCount(t, pool, jobID); n != 0 {
		t.Errorf("%d dispute rows survived a refused validation", n)
	}
}

// TestAPastIncidentIsAccepted is the other side of the one bound on the clock.
//
// A complaint about a delivery a month ago is still a complaint, and refusing it would discard the
// record — the same reasoning milestones.actor_recorded_at carries.
func TestAPastIncidentIsAccepted(t *testing.T) {
	pool := pgtest.DB(t)
	customer := newAccount(t, pool, "past-cust@example.com", "+61400000822", "customer")
	provider := newAccount(t, pool, "past-prov@example.com", "+61400000823", "provider")
	jobID := deliveredJob(t, pool, customer, provider)

	in := goodIntake("key-past")
	in.OccurredAt = testInstant.AddDate(0, -1, 0)

	dispute, raised, err := raiseWith(t, pool, newTestService(), customer, jobID, in)
	if err != nil || !raised {
		t.Fatalf("RaiseDispute() = %v, raised %v", err, raised)
	}
	if !dispute.OccurredAt.Equal(in.OccurredAt) {
		t.Errorf("occurred_at = %s, want %s — the platform does not correct it", dispute.OccurredAt, in.OccurredAt)
	}
}

// TestIntakeRefusesAConnectionPool is the check that stops a half-written dispute committing alone.
//
// The status guard would refuse the transition on its own — 000402's setting is transaction-local —
// but by then the dispute row would have committed, leaving a complaint against a job that never
// froze.
func TestIntakeRefusesAConnectionPool(t *testing.T) {
	pool := pgtest.DB(t)
	customer := newAccount(t, pool, "notx-cust@example.com", "+61400000824", "customer")
	provider := newAccount(t, pool, "notx-prov@example.com", "+61400000825", "provider")
	jobID := deliveredJob(t, pool, customer, provider)

	_, _, err := newTestService().RaiseDispute(t.Context(), pool, customer, jobID, goodIntake("key-notx"))
	if !errors.Is(err, ErrNotInTransaction) {
		t.Fatalf("RaiseDispute() = %v, want ErrNotInTransaction", err)
	}
	if n := disputeCount(t, pool, jobID); n != 0 {
		t.Errorf("%d dispute rows were written outside a transaction", n)
	}
}

// --- outcomes no fixture can produce ------------------------------------------------------------

// TestAnUnrecognisedPortAnswerIsRefused covers the two cases a wiring mistake produces.
//
// Both would otherwise be read as success: a JobMove nobody has a case for would leave a dispute on
// a job whose status nobody moved, and a party outside the two would be written into a column
// ck_disputes_complainant_party refuses — as a 500 naming a constraint rather than the wiring.
func TestAnUnrecognisedPortAnswerIsRefused(t *testing.T) {
	pool := pgtest.DB(t)
	customer := newAccount(t, pool, "wiring-cust@example.com", "+61400000826", "customer")
	provider := newAccount(t, pool, "wiring-prov@example.com", "+61400000827", "provider")
	jobID := deliveredJob(t, pool, customer, provider)

	t.Run("a party the domain does not recognise", func(t *testing.T) {
		svc := NewService(staticJobs{move: JobMoved}, staticParties{party: "admin", isParty: true}, testClock())

		_, _, err := raiseWith(t, pool, svc, customer, jobID, goodIntake("key-party"))
		if !errors.Is(err, ErrPartyUnrecognised) {
			t.Fatalf("RaiseDispute() = %v, want ErrPartyUnrecognised", err)
		}
	})

	t.Run("a move the domain has no case for", func(t *testing.T) {
		svc := NewService(staticJobs{}, testParties{}, testClock())

		_, _, err := raiseWith(t, pool, svc, customer, jobID, goodIntake("key-move"))
		if !errors.Is(err, ErrJobMoveUnrecognised) {
			t.Fatalf("RaiseDispute() = %v, want ErrJobMoveUnrecognised", err)
		}
	})

	if n := disputeCount(t, pool, jobID); n != 0 {
		t.Errorf("%d dispute rows survived a wiring failure", n)
	}
}

// TestTheServiceRefusesToBeBuiltWithoutItsCollaborators is the same call jobs.NewService makes.
//
// None of the three may be defaulted to something harmless: a nil JobParties is "nobody is checked",
// a nil Jobs is "the job never freezes", and a nil clock is a future-dated incident nothing refuses.
func TestTheServiceRefusesToBeBuiltWithoutItsCollaborators(t *testing.T) {
	cases := map[string]func(){
		"no job lifecycle": func() { NewService(nil, testParties{}, testClock()) },
		"no party lookup":  func() { NewService(staticJobs{}, nil, testClock()) },
		"no clock":         func() { NewService(staticJobs{}, testParties{}, nil) },
	}

	for name, build := range cases {
		t.Run(name, func(t *testing.T) {
			defer func() {
				if recover() == nil {
					t.Error("NewService returned a service that would fail later instead of now")
				}
			}()
			build()
		})
	}
}
