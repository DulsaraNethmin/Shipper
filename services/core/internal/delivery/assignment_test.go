package delivery

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

// SHIP-106 against a real PostgreSQL.
//
// Docs/06 §4.1 is the argument for not mocking it — "a mock happily accepts a write that the actual
// constraint would reject" — and this ticket has two of those. uq_driver_assignments_active is a
// *partial* unique index, and 000402's trigger refuses a status change that no history row
// describes; a repository interface would let both tests below pass while the real rules were being
// broken.
//
// # These tests import `jobs`, which non-test code in this package may not
//
// internal/boundaries skips test files deliberately — "a test wires domains together in the same way
// cmd/api does" — and that is what happens here. The fixtures move a job to Awarded through the real
// guard rather than by UPDATE, so every test below runs against a job that got where it is honestly.
//
// [testJobs] and [testAwards] are this package's copies of the adapters cmd/api holds. They are the
// same translation for the same reason, and the production pair is exercised end to end by
// scripts/verify/70-delivery.sh against the real binary — a Go test in cmd/api could not, because
// that package has no database.

var testInstant = time.Date(2026, 8, 12, 3, 30, 0, 0, time.UTC)

// testJobs is delivery.Jobs over the real transition guard — the same translation
// cmd/api/routes_delivery.go makes.
type testJobs struct{ svc *jobs.Service }

func (j testJobs) MoveToDriverAssigned(ctx context.Context, r db.Runner, jobID, providerID uuid.UUID) (JobMove, error) {
	return j.move(ctx, r, jobs.Move{
		JobID: jobID,
		To:    jobs.StatusDriverAssigned,
		Actor: jobs.User(jobs.ActorProvider, providerID),
	})
}

func (j testJobs) MoveToEnRouteToPickup(ctx context.Context, r db.Runner, jobID, providerID uuid.UUID, at time.Time) (JobMove, error) {
	return j.move(ctx, r, jobs.Move{
		JobID:      jobID,
		To:         jobs.StatusEnRouteToPickup,
		Actor:      jobs.User(jobs.ActorProvider, providerID),
		RecordedAt: at,
	})
}

func (j testJobs) MoveToPickedUp(ctx context.Context, r db.Runner, jobID, providerID uuid.UUID, at time.Time) (JobMove, error) {
	return j.move(ctx, r, jobs.Move{
		JobID:      jobID,
		To:         jobs.StatusPickedUp,
		Actor:      jobs.User(jobs.ActorProvider, providerID),
		RecordedAt: at,
	})
}

func (j testJobs) MoveToInTransit(ctx context.Context, r db.Runner, jobID, providerID uuid.UUID, at time.Time) (JobMove, error) {
	return j.move(ctx, r, jobs.Move{
		JobID:      jobID,
		To:         jobs.StatusInTransit,
		Actor:      jobs.User(jobs.ActorProvider, providerID),
		RecordedAt: at,
	})
}

func (j testJobs) MoveToDelivered(ctx context.Context, r db.Runner, jobID, providerID uuid.UUID, at time.Time) (JobMove, error) {
	return j.move(ctx, r, jobs.Move{
		JobID:      jobID,
		To:         jobs.StatusDelivered,
		Actor:      jobs.User(jobs.ActorProvider, providerID),
		RecordedAt: at,
	})
}

func (j testJobs) move(ctx context.Context, r db.Runner, m jobs.Move) (JobMove, error) {
	_, err := j.svc.Transition(ctx, r, m)

	switch {
	case err == nil:
		return JobMoved, nil
	case errors.Is(err, jobs.ErrJobNotFound):
		return JobNotFound, nil
	case errors.Is(err, jobs.ErrAlreadyInStatus):
		return JobAlreadyInStatus, nil
	case errors.Is(err, jobs.ErrTransitionNotPermitted):
		return j.refusal(ctx, r, m)
	default:
		return JobMoveUnrecognised, err
	}
}

// refusal splits jobs.ErrTransitionNotPermitted into "the job has already been there" and "the job
// has not reached it yet" (SHIP-112), exactly as cmd/api/routes_delivery.go does.
//
// The duplication is the arrangement this file's header describes and not an accident: the
// production adapter lives in package main, which has no database in a Go test, so the only way to
// drive the real guard from here is to hold a copy. Both are exercised — this one by the tests
// below, and cmd/api's by scripts/verify/70-delivery.sh against the running binary — and a copy that
// drifted would show up as a late milestone absorbed by one and refused by the other.
func (j testJobs) refusal(ctx context.Context, r db.Runner, m jobs.Move) (JobMove, error) {
	history, err := j.svc.History(ctx, r, m.JobID)
	if err != nil {
		return JobMoveUnrecognised, err
	}

	for _, change := range history {
		if change.To == m.To {
			return JobAlreadyPast, nil
		}
	}
	return JobNotAssignable, nil
}

// testAwards is delivery.Awards over the accepted bid, as cmd/api reads it.
type testAwards struct{}

func (testAwards) AwardedProvider(ctx context.Context, r db.Runner, jobID uuid.UUID) (uuid.UUID, bool, error) {
	const q = `SELECT provider_id FROM bids WHERE job_id = $1 AND status = 'Accepted'`

	var providerID uuid.UUID
	err := r.QueryRow(ctx, q, jobID).Scan(&providerID)
	switch {
	case errors.Is(err, db.ErrNoRows):
		return uuid.Nil, false, nil
	case err != nil:
		return uuid.Nil, false, err
	}
	return providerID, true, nil
}

// staticJobs answers with one outcome, for the cases no fixture can produce.
//
// Every move answers identically, which is what makes it useful: a test using it is asking what the
// *service* does with an outcome, not which move produced it.
type staticJobs struct {
	move JobMove
	err  error
}

func (s staticJobs) MoveToDriverAssigned(context.Context, db.Runner, uuid.UUID, uuid.UUID) (JobMove, error) {
	return s.move, s.err
}

func (s staticJobs) MoveToEnRouteToPickup(context.Context, db.Runner, uuid.UUID, uuid.UUID, time.Time) (JobMove, error) {
	return s.move, s.err
}

func (s staticJobs) MoveToPickedUp(context.Context, db.Runner, uuid.UUID, uuid.UUID, time.Time) (JobMove, error) {
	return s.move, s.err
}

func (s staticJobs) MoveToInTransit(context.Context, db.Runner, uuid.UUID, uuid.UUID, time.Time) (JobMove, error) {
	return s.move, s.err
}

func (s staticJobs) MoveToDelivered(context.Context, db.Runner, uuid.UUID, uuid.UUID, time.Time) (JobMove, error) {
	return s.move, s.err
}

// testClock is the clock every service below is built with.
//
// Fixed rather than real, and it is doing work: [testInstant] is what a milestone recorded with no
// actor-supplied time is stamped with, so a test can tell that value apart from the
// server_recorded_at the database writes from its own clock. Two real clocks would agree to within a
// millisecond and prove nothing.
func testClock() *clock.Fixed { return clock.NewFixed(testInstant) }

func newTestService() *Service {
	return newTestServiceWith(testJobs{svc: jobs.NewService(events.NewOutbox(), testClock(), nil)})
}

// newTestServiceWith is the same service over a different job lifecycle, which is what the tests
// covering outcomes no fixture can produce need.
//
// One constructor rather than four literals, so that the driver token issuer SHIP-107 added arrives
// in every one of them: a test built without one panics, which is the right answer and a tedious one
// to rediscover four times. SHIP-114's upload signer and policy arrive the same way, and SHIP-115's
// object reader and ownership lookup after them.
func newTestServiceWith(lifecycle Jobs) *Service {
	return NewService(lifecycle, testAwards{}, testJobOwners(), testDriverIssuer(testClock()),
		&recordingUploads{}, newRecordingObjects(), testUploadPolicy(), testClock())
}

// testJobOwners is delivery.JobOwners over jobs.Service.Job, as cmd/api reads it.
//
// A copy of jobCustomers for the reason testJobs is a copy of jobLifecycle: the production adapter
// lives in package main, which has no database in a Go test. It goes through `jobs` rather than
// running its own SELECT, so that "owns the job" means here exactly what it means there.
func testJobOwners() JobOwners {
	return testOwners{svc: jobs.NewService(events.NewOutbox(), testClock(), nil)}
}

type testOwners struct {
	svc *jobs.Service
}

func (o testOwners) IsCustomer(ctx context.Context, r db.Runner, jobID, userID uuid.UUID) (bool, error) {
	_, err := o.svc.Job(ctx, r, userID, jobID)
	switch {
	case err == nil:
		return true, nil
	case errors.Is(err, jobs.ErrNotJobOwner), errors.Is(err, jobs.ErrJobNotFound):
		return false, nil
	default:
		return false, err
	}
}

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

	svc := jobs.NewService(events.NewOutbox(), clock.NewFixed(testInstant), nil)

	for _, status := range to {
		err := db.InTx(t.Context(), pool, func(ctx context.Context, r db.Runner) error {
			_, err := svc.Transition(ctx, r, jobs.Move{JobID: jobID, To: status, Actor: actor})
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

// awardedJob is a job that has been published and awarded to provider — the state SHIP-106 starts
// from.
func awardedJob(t *testing.T, pool *pgxpool.Pool, customer, provider uuid.UUID) uuid.UUID {
	t.Helper()

	jobID := newDraft(t, pool, customer)
	moveJob(t, pool, jobID, jobs.User(jobs.ActorCustomer, customer), jobs.StatusOpen, jobs.StatusAwarded)
	acceptBid(t, pool, jobID, provider)
	return jobID
}

// assign runs one assignment in its own transaction, which is what the handler does.
//
// It drops the driver token, because most of the tests below are about the assignment and the
// status move. [assignGranting] is the same call with the token kept, and token_test.go is where the
// tests that care about it live.
func assign(t *testing.T, pool *pgxpool.Pool, svc *Service, provider, jobID uuid.UUID, n Nomination) (Assignment, bool, error) {
	t.Helper()

	assignment, _, created, err := assignGranting(t, pool, svc, provider, jobID, n)
	return assignment, created, err
}

// assignGranting runs one assignment and keeps the job-scoped token it minted (SHIP-107).
func assignGranting(
	t *testing.T,
	pool *pgxpool.Pool,
	svc *Service,
	provider, jobID uuid.UUID,
	n Nomination,
) (Assignment, DriverToken, bool, error) {
	t.Helper()

	var (
		assignment Assignment
		token      DriverToken
		created    bool
	)
	err := db.InTx(t.Context(), pool, func(ctx context.Context, r db.Runner) error {
		var err error
		assignment, token, created, err = svc.AssignDriver(ctx, r, provider, jobID, n)
		return err
	})
	return assignment, token, created, err
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

// TestAProviderNominatesADriver is the first half of SHIP-106's acceptance criterion.
func TestAProviderNominatesADriver(t *testing.T) {
	pool := pgtest.DB(t)
	customer := newAccount(t, pool, "assign-cust@example.com", "+61400000600", "customer")
	provider := newAccount(t, pool, "assign-prov@example.com", "+61400000601", "provider")
	jobID := awardedJob(t, pool, customer, provider)

	assignment, created, err := assign(t, pool, newTestService(), provider, jobID, Nomination{
		DriverName:   "  Sam   Patel ",
		DriverMobile: "0412 345 678",
	})
	if err != nil {
		t.Fatalf("AssignDriver() = %v", err)
	}

	switch {
	case !created:
		t.Error("the assignment reports that it wrote nothing")
	case assignment.ID == uuid.Nil:
		t.Error("the assignment has no id")
	case assignment.JobID != jobID:
		t.Errorf("assigned to %s, want %s", assignment.JobID, jobID)
	case !assignment.Live():
		t.Error("the driver was assigned already unassigned")
	}

	// Normalisation is checked on the way out because it is what the column holds: a number
	// stored as typed is a number a second spelling of one handset can hide behind.
	if assignment.DriverName != "Sam Patel" {
		t.Errorf("driver_name = %q, want the whitespace collapsed", assignment.DriverName)
	}
	if assignment.DriverMobile != "+61412345678" {
		t.Errorf("driver_mobile = %q, want E.164", assignment.DriverMobile)
	}

	// The half of the Done when that is not about the driver at all.
	if status := jobStatus(t, pool, jobID); status != string(jobs.StatusDriverAssigned) {
		t.Errorf("the job is %q, want %q", status, jobs.StatusDriverAssigned)
	}
}

// TestTheTransitionWentThroughTheGuard checks the record 000402 refuses the status change without.
//
// A job that reached 'Driver assigned' with no history row would mean the guard had been bypassed —
// which the trigger makes impossible, and which is exactly why it is worth asserting that the row
// says what it should: the provider did it, not the customer and not the platform.
func TestTheTransitionWentThroughTheGuard(t *testing.T) {
	pool := pgtest.DB(t)
	customer := newAccount(t, pool, "assign-guard-c@example.com", "+61400000602", "customer")
	provider := newAccount(t, pool, "assign-guard-p@example.com", "+61400000603", "provider")
	jobID := awardedJob(t, pool, customer, provider)

	if _, _, err := assign(t, pool, newTestService(), provider, jobID, Nomination{
		DriverName:   "Sam Patel",
		DriverMobile: "+61412345678",
	}); err != nil {
		t.Fatalf("AssignDriver() = %v", err)
	}

	var (
		from, to, actorType string
		actorID             uuid.UUID
	)
	if err := pool.QueryRow(t.Context(), `
		SELECT from_status, to_status, actor_type, actor_id
		FROM job_status_history
		WHERE job_id = $1 AND to_status = 'Driver assigned'`, jobID).
		Scan(&from, &to, &actorType, &actorID); err != nil {
		t.Fatalf("reading the transition: %v", err)
	}

	switch {
	case from != string(jobs.StatusAwarded):
		t.Errorf("moved from %q, want Awarded", from)
	case actorType != string(jobs.ActorProvider):
		t.Errorf("recorded against %q, want provider", actorType)
	case actorID != provider:
		t.Errorf("recorded against %s, want the provider %s", actorID, provider)
	}
}

// TestAProviderSelfAssigns is the other half of the acceptance criterion.
//
// The mobile is not in the request at all: it comes from the account, which is the number the
// platform verified rather than one the caller asserted.
func TestAProviderSelfAssigns(t *testing.T) {
	pool := pgtest.DB(t)
	customer := newAccount(t, pool, "self-cust@example.com", "+61400000604", "customer")
	provider := newAccount(t, pool, "self-prov@example.com", "+61400000605", "provider")
	jobID := awardedJob(t, pool, customer, provider)

	assignment, created, err := assign(t, pool, newTestService(), provider, jobID, Nomination{
		DriverName: "Ravi Chandra",
		Self:       true,
	})
	if err != nil {
		t.Fatalf("AssignDriver(self) = %v", err)
	}
	if !created {
		t.Error("the self-assignment wrote nothing")
	}
	if assignment.DriverMobile != "+61400000605" {
		t.Errorf("driver_mobile = %q, want the provider's own number from their account", assignment.DriverMobile)
	}
	if status := jobStatus(t, pool, jobID); status != string(jobs.StatusDriverAssigned) {
		t.Errorf("the job is %q, want %q", status, jobs.StatusDriverAssigned)
	}
}

// TestSelfAssignmentRefusesASuppliedMobile keeps the two paths apart.
//
// Neither field can win silently: a provider who sent both has two numbers in mind, and guessing
// which would send SHIP-107's link to whichever one lost.
func TestSelfAssignmentRefusesASuppliedMobile(t *testing.T) {
	pool := pgtest.DB(t)
	customer := newAccount(t, pool, "self-both-c@example.com", "+61400000606", "customer")
	provider := newAccount(t, pool, "self-both-p@example.com", "+61400000607", "provider")
	jobID := awardedJob(t, pool, customer, provider)

	_, _, err := assign(t, pool, newTestService(), provider, jobID, Nomination{
		DriverName:   "Ravi Chandra",
		DriverMobile: "+61412345678",
		Self:         true,
	})

	var apiErr *httpx.Error
	if !errors.As(err, &apiErr) {
		t.Fatalf("AssignDriver() = %v, want a validation failure", err)
	}
	if apiErr.Status != 422 {
		t.Errorf("status = %d, want 422", apiErr.Status)
	}
}

// TestOnlyTheAwardedProviderMayAssign is the authorisation decision, and it is the platform's.
//
// The second provider holds a perfectly good credential. What they do not hold is the accepted bid.
func TestOnlyTheAwardedProviderMayAssign(t *testing.T) {
	pool := pgtest.DB(t)
	customer := newAccount(t, pool, "stranger-c@example.com", "+61400000608", "customer")
	provider := newAccount(t, pool, "stranger-p@example.com", "+61400000609", "provider")
	stranger := newAccount(t, pool, "stranger-x@example.com", "+61400000610", "provider")
	jobID := awardedJob(t, pool, customer, provider)

	_, _, err := assign(t, pool, newTestService(), stranger, jobID, Nomination{
		DriverName:   "Sam Patel",
		DriverMobile: "+61412345678",
	})
	if !errors.Is(err, ErrNotAwardedProvider) {
		t.Fatalf("AssignDriver() = %v, want ErrNotAwardedProvider", err)
	}

	// The refusal is not merely reported: nothing was written on the way to it.
	if status := jobStatus(t, pool, jobID); status != string(jobs.StatusAwarded) {
		t.Errorf("the job is %q — a stranger moved it", status)
	}
	if _, live, err := newTestService().Driver(t.Context(), pool, jobID); err != nil || live {
		t.Errorf("a driver was recorded for a stranger's request (live = %v, err = %v)", live, err)
	}
}

// TestAJobNobodyWasAwardedIsNotFound is the other side of the same 404.
//
// An Open job exists and has no accepted bid. Telling the caller that it exists would disclose
// somebody else's job, so the answer is the one a missing job gets.
func TestAJobNobodyWasAwardedIsNotFound(t *testing.T) {
	pool := pgtest.DB(t)
	customer := newAccount(t, pool, "unawarded-c@example.com", "+61400000611", "customer")
	provider := newAccount(t, pool, "unawarded-p@example.com", "+61400000612", "provider")

	jobID := newDraft(t, pool, customer)
	moveJob(t, pool, jobID, jobs.User(jobs.ActorCustomer, customer), jobs.StatusOpen)

	_, _, err := assign(t, pool, newTestService(), provider, jobID, Nomination{
		DriverName:   "Sam Patel",
		DriverMobile: "+61412345678",
	})
	if !errors.Is(err, ErrJobNotFound) {
		t.Fatalf("AssignDriver() = %v, want ErrJobNotFound", err)
	}

	_, _, err = assign(t, pool, newTestService(), provider, uuid.Must(uuid.NewV7()), Nomination{
		DriverName:   "Sam Patel",
		DriverMobile: "+61412345678",
	})
	if !errors.Is(err, ErrJobNotFound) {
		t.Fatalf("AssignDriver(no such job) = %v, want ErrJobNotFound", err)
	}
}

// TestAJobThatHasSetOffCannotTakeADriver is Docs/02 §2 refusing the move, not this domain.
//
// 'Driver assigned' is skippable — Awarded → En route to pickup is permitted — and there is no way
// back. A provider who drove off and then tried to nominate somebody is told so.
func TestAJobThatHasSetOffCannotTakeADriver(t *testing.T) {
	pool := pgtest.DB(t)
	customer := newAccount(t, pool, "enroute-c@example.com", "+61400000613", "customer")
	provider := newAccount(t, pool, "enroute-p@example.com", "+61400000614", "provider")
	jobID := awardedJob(t, pool, customer, provider)

	moveJob(t, pool, jobID, jobs.User(jobs.ActorProvider, provider), jobs.StatusEnRouteToPickup)

	_, _, err := assign(t, pool, newTestService(), provider, jobID, Nomination{
		DriverName:   "Sam Patel",
		DriverMobile: "+61412345678",
	})
	if !errors.Is(err, ErrJobNotAssignable) {
		t.Fatalf("AssignDriver() = %v, want ErrJobNotAssignable", err)
	}

	// The assignment row is inserted before the transition is attempted, so this also proves
	// the whole thing rolled back rather than leaving a driver on a job that never moved.
	if _, live, err := newTestService().Driver(t.Context(), pool, jobID); err != nil || live {
		t.Errorf("a driver survived a refused transition (live = %v, err = %v)", live, err)
	}
}

// TestTheSameDriverNominatedTwiceIsAbsorbed is the retry that does not reuse its key.
func TestTheSameDriverNominatedTwiceIsAbsorbed(t *testing.T) {
	pool := pgtest.DB(t)
	customer := newAccount(t, pool, "twice-c@example.com", "+61400000615", "customer")
	provider := newAccount(t, pool, "twice-p@example.com", "+61400000616", "provider")
	jobID := awardedJob(t, pool, customer, provider)

	first, created, err := assign(t, pool, newTestService(), provider, jobID, Nomination{
		DriverName:   "Sam Patel",
		DriverMobile: "0412 345 678",
	})
	if err != nil || !created {
		t.Fatalf("the first nomination: %v (created = %v)", err, created)
	}

	// The same driver, written differently. Normalisation is what makes this the same request.
	second, created, err := assign(t, pool, newTestService(), provider, jobID, Nomination{
		DriverName:   "Sam  Patel ",
		DriverMobile: "+61 412 345 678",
	})
	if err != nil {
		t.Fatalf("the second nomination: %v", err)
	}
	if created {
		t.Error("the repeat wrote a second assignment")
	}
	if second.ID != first.ID {
		t.Errorf("the repeat answered with %s, want the existing %s", second.ID, first.ID)
	}

	var rows int
	if err := pool.QueryRow(t.Context(),
		`SELECT count(*) FROM driver_assignments WHERE job_id = $1`, jobID).Scan(&rows); err != nil {
		t.Fatalf("counting assignments: %v", err)
	}
	if rows != 1 {
		t.Errorf("%d assignment rows, want 1", rows)
	}

	// One transition, not two: the second call found the job already at 'Driver assigned' and
	// wrote no history row.
	var transitions int
	if err := pool.QueryRow(t.Context(),
		`SELECT count(*) FROM job_status_history WHERE job_id = $1 AND to_status = 'Driver assigned'`,
		jobID).Scan(&transitions); err != nil {
		t.Fatalf("counting transitions: %v", err)
	}
	if transitions != 1 {
		t.Errorf("%d transitions into Driver assigned, want 1", transitions)
	}
}

// TestADifferentDriverIsRefused holds the line SHIP-106 draws around replacement.
func TestADifferentDriverIsRefused(t *testing.T) {
	pool := pgtest.DB(t)
	customer := newAccount(t, pool, "replace-c@example.com", "+61400000617", "customer")
	provider := newAccount(t, pool, "replace-p@example.com", "+61400000618", "provider")
	jobID := awardedJob(t, pool, customer, provider)

	if _, _, err := assign(t, pool, newTestService(), provider, jobID, Nomination{
		DriverName:   "Sam Patel",
		DriverMobile: "+61412345678",
	}); err != nil {
		t.Fatalf("the first nomination: %v", err)
	}

	_, _, err := assign(t, pool, newTestService(), provider, jobID, Nomination{
		DriverName:   "Ravi Chandra",
		DriverMobile: "+61412000999",
	})
	if !errors.Is(err, ErrDriverAlreadyAssigned) {
		t.Fatalf("AssignDriver() = %v, want ErrDriverAlreadyAssigned", err)
	}

	driver, live, err := newTestService().Driver(t.Context(), pool, jobID)
	if err != nil || !live {
		t.Fatalf("reading the driver: %v (live = %v)", err, live)
	}
	if driver.DriverName != "Sam Patel" {
		t.Errorf("the driver is now %q — the refusal changed the job", driver.DriverName)
	}
}

// TestTheIndexRefusesASecondLiveDriver checks the mechanism rather than the check in front of it.
//
// The service reads the live assignment first and would have refused this. The index is what makes
// the rule true when two requests race and both read nothing, so it is tested directly: an INSERT
// that the check never saw.
func TestTheIndexRefusesASecondLiveDriver(t *testing.T) {
	pool := pgtest.DB(t)
	customer := newAccount(t, pool, "index-c@example.com", "+61400000619", "customer")
	provider := newAccount(t, pool, "index-p@example.com", "+61400000620", "provider")
	jobID := awardedJob(t, pool, customer, provider)

	if _, _, err := assign(t, pool, newTestService(), provider, jobID, Nomination{
		DriverName:   "Sam Patel",
		DriverMobile: "+61412345678",
	}); err != nil {
		t.Fatalf("the first nomination: %v", err)
	}

	err := db.InTx(t.Context(), pool, func(ctx context.Context, r db.Runner) error {
		_, err := postgresStore{}.insert(ctx, r, Assignment{
			ID:           uuid.Must(uuid.NewV7()),
			JobID:        jobID,
			DriverName:   "Ravi Chandra",
			DriverMobile: "+61412000999",
		})
		return err
	})
	if !errors.Is(err, ErrDriverAlreadyAssigned) {
		t.Fatalf("insert() = %v, want ErrDriverAlreadyAssigned from uq_driver_assignments_active", err)
	}
}

// TestAssignmentOutsideATransactionIsRefused.
//
// The pool satisfies db.Runner and is not a transaction. Without this check the assignment would
// commit on its own and 000402 would then refuse the status change, leaving a driver on a job that
// never moved — the one outcome this endpoint must not be able to produce.
func TestAssignmentOutsideATransactionIsRefused(t *testing.T) {
	pool := pgtest.DB(t)
	customer := newAccount(t, pool, "notx-c@example.com", "+61400000621", "customer")
	provider := newAccount(t, pool, "notx-p@example.com", "+61400000622", "provider")
	jobID := awardedJob(t, pool, customer, provider)

	_, _, _, err := newTestService().AssignDriver(t.Context(), pool, provider, jobID, Nomination{
		DriverName:   "Sam Patel",
		DriverMobile: "+61412345678",
	})
	if !errors.Is(err, ErrNotInTransaction) {
		t.Fatalf("AssignDriver(pool) = %v, want ErrNotInTransaction", err)
	}
}

// TestAnUnrecognisedOutcomeIsAFailure covers the zero value of JobMove.
//
// It is the answer a half-written adapter gives, and reading it as success would leave an assignment
// on a job whose status nobody moved. No fixture can produce it, so the port is stubbed.
func TestAnUnrecognisedOutcomeIsAFailure(t *testing.T) {
	pool := pgtest.DB(t)
	customer := newAccount(t, pool, "unknown-c@example.com", "+61400000623", "customer")
	provider := newAccount(t, pool, "unknown-p@example.com", "+61400000624", "provider")
	jobID := awardedJob(t, pool, customer, provider)

	svc := newTestServiceWith(staticJobs{move: JobMoveUnrecognised})

	_, _, err := assign(t, pool, svc, provider, jobID, Nomination{
		DriverName:   "Sam Patel",
		DriverMobile: "+61412345678",
	})
	if !errors.Is(err, ErrJobMoveUnrecognised) {
		t.Fatalf("AssignDriver() = %v, want ErrJobMoveUnrecognised", err)
	}
}

// TestAJobAlreadyDriverAssignedKeepsTheNewDriver is the absorption Docs/02 §3.1 asks for.
//
// The job is where the caller wanted it and no assignment was live — the state a stood-down driver
// leaves behind. The new driver stands and nothing is refused.
func TestAJobAlreadyDriverAssignedKeepsTheNewDriver(t *testing.T) {
	pool := pgtest.DB(t)
	customer := newAccount(t, pool, "already-c@example.com", "+61400000625", "customer")
	provider := newAccount(t, pool, "already-p@example.com", "+61400000626", "provider")
	jobID := awardedJob(t, pool, customer, provider)

	svc := newTestServiceWith(staticJobs{move: JobAlreadyInStatus})

	assignment, created, err := assign(t, pool, svc, provider, jobID, Nomination{
		DriverName:   "Sam Patel",
		DriverMobile: "+61412345678",
	})
	if err != nil || !created {
		t.Fatalf("AssignDriver() = %v (created = %v), want the assignment to stand", err, created)
	}
	if assignment.DriverName != "Sam Patel" {
		t.Errorf("driver_name = %q", assignment.DriverName)
	}
}

// TestAJobPastDriverAssignedIsRefused is SHIP-112's new outcome arriving on the assignment path.
//
// **An assignment is not absorbed the way a late milestone is**, and this is the test that says so.
// A milestone is a claim about something that already happened, so keeping it costs nothing; an
// assignment is an instruction about who drives the job *now*, and a driver_assignments row written
// for a job that has already gone would name a live driver on a delivery somebody else is carrying.
//
// The port is stubbed because no fixture can produce this. Reaching it needs a job that has been at
// 'Driver assigned' and has no live assignment, and nothing writes `unassigned_at` until SHIP-109 —
// the same reason TestAJobAlreadyDriverAssignedKeepsTheNewDriver stubs its outcome. A job that set
// off without ever being 'Driver assigned' is JobNotAssignable and is covered by
// TestAJobThatHasSetOffCannotTakeADriver, against the real guard.
//
// The second assertion is wave 5's invariant, which SHIP-112 must not have loosened: the assignment
// row is inserted before the transition is attempted, so a refusal has to take it with it.
func TestAJobPastDriverAssignedIsRefused(t *testing.T) {
	pool := pgtest.DB(t)
	customer := newAccount(t, pool, "past-c@example.com", "+61400000627", "customer")
	provider := newAccount(t, pool, "past-p@example.com", "+61400000628", "provider")
	jobID := awardedJob(t, pool, customer, provider)

	svc := newTestServiceWith(staticJobs{move: JobAlreadyPast})

	_, _, err := assign(t, pool, svc, provider, jobID, Nomination{
		DriverName:   "Sam Patel",
		DriverMobile: "+61412345678",
	})
	if !errors.Is(err, ErrJobNotAssignable) {
		t.Fatalf("AssignDriver() = %v, want ErrJobNotAssignable — a job past 'Driver assigned' "+
			"has nothing historical to keep, so there is nothing to absorb", err)
	}

	var rows int
	if err := pool.QueryRow(t.Context(),
		`SELECT count(*) FROM driver_assignments WHERE job_id = $1`, jobID).Scan(&rows); err != nil {
		t.Fatalf("counting assignments: %v", err)
	}
	if rows != 0 {
		t.Errorf("%d assignment rows survived the refusal; absorption must not have turned a "+
			"rollback into a partial write", rows)
	}
}

// collaborators is everything [NewService] insists on, for the test below.
type collaborators struct {
	jobs    Jobs
	awards  Awards
	owners  JobOwners
	tokens  *DriverTokenIssuer
	uploads ProofUploads
	objects ProofObjects
	policy  UploadPolicy
	clk     clock.Clock
}

// working is a set that builds a service, which every case below then removes exactly one thing
// from.
//
// **Written as a base plus one removal rather than eight literals**, which is a change SHIP-115 made
// while adding two more collaborators. Eight full literals is where a case that is missing *two*
// things still passes, because a panic proves only that something was wrong; starting from a set
// that is known to work makes each case a statement about one field.
func workingCollaborators() collaborators {
	return collaborators{
		jobs:    staticJobs{move: JobMoved},
		awards:  testAwards{},
		owners:  testJobOwners(),
		tokens:  testDriverIssuer(testClock()),
		uploads: &recordingUploads{},
		objects: newRecordingObjects(),
		policy:  testUploadPolicy(),
		clk:     testClock(),
	}
}

// TestNewServiceRefusesAMissingCollaborator.
//
// Every one is a load-bearing rule rather than a convenience: without Awards nobody is checked,
// without Jobs the job never moves, without JobOwners a customer cannot be told from a stranger
// asking after their own delivery (SHIP-115), without a token issuer an assignment produces no link
// for the driver (SHIP-107), without an upload signer or a usable policy proof cannot be captured
// at all (SHIP-114), without an object reader the platform can only take the client's word that a
// photograph exists (SHIP-115), and without a clock a milestone recorded with no actor-supplied
// time has nothing to be stamped from. A service that started without any of them would fail
// silently, in production, at the first request.
func TestNewServiceRefusesAMissingCollaborator(t *testing.T) {
	// The base itself must build, or every case below would "pass" for the wrong reason.
	base := workingCollaborators()
	NewService(base.jobs, base.awards, base.owners, base.tokens,
		base.uploads, base.objects, base.policy, base.clk)

	for _, tc := range []struct {
		name   string
		remove func(*collaborators)
	}{
		{"no job lifecycle", func(c *collaborators) { c.jobs = nil }},
		{"no award lookup", func(c *collaborators) { c.awards = nil }},
		{"no job ownership lookup", func(c *collaborators) { c.owners = nil }},
		{"no driver token issuer", func(c *collaborators) { c.tokens = nil }},
		{"nowhere to put proof", func(c *collaborators) { c.uploads = nil }},
		{"no way to read an object back", func(c *collaborators) { c.objects = nil }},
		{"no size limit", func(c *collaborators) { c.policy.MaxBytes = 0 }},
		{"no accepted content types", func(c *collaborators) { c.policy.AcceptedContentTypes = nil }},
		{"no upload lifetime", func(c *collaborators) { c.policy.UploadTTL = 0 }},
		{"no download lifetime", func(c *collaborators) { c.policy.DownloadTTL = 0 }},
		{"no clock", func(c *collaborators) { c.clk = nil }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			defer func() {
				if recover() == nil {
					t.Error("NewService returned a service that cannot work")
				}
			}()

			c := workingCollaborators()
			tc.remove(&c)
			NewService(c.jobs, c.awards, c.owners, c.tokens, c.uploads, c.objects, c.policy, c.clk)
		})
	}
}

// TestNominationValidation covers what a client can get wrong before anything is read.
func TestNominationValidation(t *testing.T) {
	for _, tc := range []struct {
		name  string
		given Nomination
		field string
	}{
		{name: "no name", given: Nomination{DriverMobile: "+61412345678"}, field: "driver_name"},
		{name: "whitespace name", given: Nomination{DriverName: "\t ", DriverMobile: "+61412345678"}, field: "driver_name"},
		{name: "name too long", given: Nomination{DriverName: strings.Repeat("a", maxDriverName+1), DriverMobile: "+61412345678"}, field: "driver_name"},
		{name: "no mobile", given: Nomination{DriverName: "Sam Patel"}, field: "driver_mobile"},
		{name: "letters in the mobile", given: Nomination{DriverName: "Sam Patel", DriverMobile: "0412 34a 678"}, field: "driver_mobile"},
		{name: "too short", given: Nomination{DriverName: "Sam Patel", DriverMobile: "+61412"}, field: "driver_mobile"},
		{name: "unrecognised trunk prefix", given: Nomination{DriverName: "Sam Patel", DriverMobile: "+0412345678"}, field: "driver_mobile"},
		{name: "self with a mobile", given: Nomination{DriverName: "Sam Patel", DriverMobile: "+61412345678", Self: true}, field: "driver_mobile"},
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

// TestAustralianNumbersAreNormalised pins the shapes a person actually types.
func TestAustralianNumbersAreNormalised(t *testing.T) {
	for _, tc := range []struct{ given, want string }{
		{"0412 345 678", "+61412345678"},
		{"(04) 1234 5678", "+61412345678"},
		{"+61 412 345 678", "+61412345678"},
		{"61412345678", "+61412345678"},
		{"+44 20 7946 0958", "+442079460958"},
	} {
		if got := normalisePhone(tc.given); got != tc.want {
			t.Errorf("normalisePhone(%q) = %q, want %q", tc.given, got, tc.want)
		}
	}
}
