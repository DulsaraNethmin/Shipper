package bidding

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/DulsaraNethmin/Shipper/services/core/internal/clock"
	"github.com/DulsaraNethmin/Shipper/services/core/internal/db"
	"github.com/DulsaraNethmin/Shipper/services/core/internal/events"
	"github.com/DulsaraNethmin/Shipper/services/core/internal/fleet"
	"github.com/DulsaraNethmin/Shipper/services/core/internal/jobs"
	"github.com/DulsaraNethmin/Shipper/services/core/internal/testsupport/pgtest"
)

// The world SHIP-84's tests run in, against a real PostgreSQL.
//
// Docs/06 §4.1 forbids mocking the database here, and for this ticket that is not a preference.
// Three of the rules under test are PostgreSQL objects and nothing else:
//
//   - uq_bids_one_submitted_per_provider_per_job, a *partial* unique index, is the whole of "bid once
//     per job". A mocked repository accepts exactly the second row it exists to reject.
//   - uq_bids_idempotency plus `ON CONFLICT … DO NOTHING` is the whole of the retry guarantee, and
//     the concurrent case cannot be written against anything but a real btree.
//   - `numeric(12,2)` is what the cents round trip is a round trip *through*. In Go alone it is an
//     int64 assigned to itself.
//
// # These tests import `fleet`, which non-test code in this package may not
//
// internal/boundaries skips test files deliberately — "a test wires domains together in the same way
// cmd/api does" — and that is exactly what happens here. [Eligibility] is satisfied by the real
// `*fleet.Service`, the same value cmd/api/routes_bidding.go passes, so every eligibility assertion
// below runs against SHIP-81's actual predicate rather than against a stub that agrees with itself.
//
// That is the difference between demonstrating "a **verified, eligible** provider can bid" and
// demonstrating that a test double returns true. A stub would have made
// [TestABidIsAcceptedOnBothBiddableStatuses] pass against a filter that accepted neither status.
//
// [refusing] is the one deliberate exception, for the cases no fixture can produce.

// testInstant is the fixed clock every test here reads. Deliberately fixed rather than time.Now():
// the timing rules compare against the injected clock (Docs/10 §6.3), and a test that let the two
// drift would pass or fail on how long the suite took to reach it.
var testInstant = time.Date(2026, 8, 13, 3, 30, 0, 0, time.UTC)

var (
	// publishInstant is when the fixture's jobs become Open — before the clock, so they are already
	// live when a bid is placed.
	publishInstant = testInstant.Add(-24 * time.Hour)

	// farFuture is the deadline every fixture job carries.
	//
	// **Set explicitly rather than left to the trigger.** `jobs_open_gets_a_deadline` computes
	// expires_at from the *database's* now(), and the eligibility filter compares it against the
	// *injected* clock — so a job relying on the trigger would be eligible or not depending on how
	// far the machine's clock had drifted from [testInstant]. The same trap fleet's fixtures
	// document.
	farFuture = testInstant.Add(30 * 24 * time.Hour)
)

// newTestService is the domain wired to the real eligibility filter and the real job service.
//
// All three ports are satisfied by the packages cmd/api passes, for the reason this file's header
// gives: a stub agrees with itself. SHIP-87's customer side is the sharper case — [jobParties]
// answers "is this the job's customer" and "can this job still be awarded" out of the real `jobs`
// package, so TestOnlyAPartyToTheNegotiationCanCounter is refused by the same comparison the served
// endpoint makes. SHIP-92's is sharper again: [jobAwards] takes the real `FOR UPDATE` and runs the
// real guarded transition, so an award test that passed against a stub would prove nothing about
// whether the job actually moved.
func newTestService() *Service {
	c := clock.NewFixed(testInstant)
	return NewService(fleet.NewService(c), newTestNegotiation(c), newTestAwarding(c), c)
}

// newTestNegotiation is [Negotiation] over the real `jobs` service, as cmd/api wires it.
//
// A second copy of `negotiatedJobs` from cmd/api/routes_bidding.go, deliberately: the composition
// root is not importable from here, and the whole point of the port is that this package names
// neither type. What keeps the two honest is that both are held to the same interface and both are
// exercised — this one by the tests below, that one by scripts/verify/61-bidding.sh against the real
// binary.
//
// The sink is the real outbox writer, as cmd/api passes; the geocoder is nil because it is only
// reached when an address is resolved. Neither is exercised — `jobs.Service.Job` is a read that emits
// nothing — but `jobs.NewService` refuses a nil sink outright, which is the right refusal and is why
// this passes one rather than a stub.
func newTestNegotiation(c clock.Clock) jobParties {
	return jobParties{jobs: jobs.NewService(events.NewOutbox(), c, nil)}
}

type jobParties struct{ jobs *jobs.Service }

func (p jobParties) CustomerOf(ctx context.Context, r db.Runner, userID, jobID uuid.UUID) (bool, error) {
	_, err := p.jobs.Job(ctx, r, userID, jobID)
	return p.answer(err)
}

func (p jobParties) AwardableBy(ctx context.Context, r db.Runner, customerID, jobID uuid.UUID) (bool, error) {
	job, err := p.jobs.Job(ctx, r, customerID, jobID)
	if owned, err := p.answer(err); err != nil || !owned {
		return false, err
	}
	return jobs.Permitted(job.Status, jobs.StatusAwarded), nil
}

func (jobParties) answer(err error) (bool, error) {
	switch {
	case err == nil:
		return true, nil
	case errors.Is(err, jobs.ErrNotJobOwner), errors.Is(err, jobs.ErrJobNotFound):
		return false, nil
	default:
		return false, err
	}
}

var _ Negotiation = jobParties{}

// newTestAwarding is [Awarding] over the real `jobs` service, as cmd/api wires it (SHIP-92).
//
// A second copy of `awardableJobs` from cmd/api/routes_bidding.go, for the reason [newTestNegotiation]
// is a second copy of `negotiatedJobs`: the composition root is not importable from here, and the
// whole point of the port is that this package names neither type.
//
// **The copy matters more here than it did for the counter's port**, and it is worth saying why
// rather than leaving it to be assumed. This one *writes*: it takes the `FOR UPDATE` that starts the
// lock ordering and it runs the guarded transition that ends it. A stub answering `JobAwardable` and
// `JobAwarded` would make every award test below pass against a service that never locked anything
// and never moved a job — which is precisely the failure `make verify` could not catch either, since
// it drives the same binary from outside.
func newTestAwarding(c clock.Clock) jobAwards {
	return jobAwards{jobs: jobs.NewService(events.NewOutbox(), c, nil)}
}

type jobAwards struct{ jobs *jobs.Service }

func (a jobAwards) LockForAward(
	ctx context.Context,
	r db.Runner,
	customerID, jobID uuid.UUID,
) (JobAward, error) {
	const q = `SELECT customer_id, status FROM jobs WHERE id = $1 FOR UPDATE`

	var (
		owner  uuid.UUID
		status jobs.Status
	)
	switch err := r.QueryRow(ctx, q, jobID).Scan(&owner, &status); {
	case errors.Is(err, db.ErrNoRows):
		return JobAwardNoSuchJob, nil
	case err != nil:
		return JobAwardUnrecognised, err
	}

	if owner != customerID {
		return JobAwardNoSuchJob, nil
	}
	if !jobs.Permitted(status, jobs.StatusAwarded) {
		return JobAwardNotPermitted, nil
	}
	return JobAwardable, nil
}

func (a jobAwards) MoveToAwarded(
	ctx context.Context,
	r db.Runner,
	jobID, customerID uuid.UUID,
) (JobAward, error) {
	_, err := a.jobs.Transition(ctx, r, jobs.Move{
		JobID: jobID,
		To:    jobs.StatusAwarded,
		Actor: jobs.User(jobs.ActorCustomer, customerID),
	})

	switch {
	case err == nil:
		return JobAwarded, nil
	case errors.Is(err, jobs.ErrJobNotFound):
		return JobAwardNoSuchJob, nil
	case errors.Is(err, jobs.ErrAlreadyInStatus), errors.Is(err, jobs.ErrTransitionNotPermitted):
		return JobAwardNotPermitted, nil
	default:
		return JobAwardUnrecognised, err
	}
}

var _ Awarding = jobAwards{}

// refusing is [Eligibility] answering "no" to everything.
//
// The one test double here, and it earns its place: [TestAnEligibilityFailureIsNotARefusal] needs
// the port to *fail* rather than to decline, which no arrangement of rows can produce. Everything
// else goes through the real filter.
type refusing struct{ err error }

func (r refusing) EligibleFor(context.Context, db.Runner, uuid.UUID, uuid.UUID) (bool, error) {
	return false, r.err
}

// brokenNegotiation is [Negotiation] failing rather than declining, which is [refusing]'s twin and
// earns its place for the same reason.
//
// A port that *fails* is a condition no arrangement of rows produces, and the failure must not be
// reported as a refusal: a caller told "no such bid" because the database was unreachable would go
// looking for a bid that is there.
type brokenNegotiation struct{ err error }

func (b brokenNegotiation) CustomerOf(context.Context, db.Runner, uuid.UUID, uuid.UUID) (bool, error) {
	return false, b.err
}

func (b brokenNegotiation) AwardableBy(context.Context, db.Runner, uuid.UUID, uuid.UUID) (bool, error) {
	return false, b.err
}

// brokenAwarding is [Awarding] failing rather than answering, which is [brokenNegotiation]'s twin
// and earns its place for a reason of its own (SHIP-92).
//
// The other two stubs exist because a port that *fails* is a condition no arrangement of rows
// produces. This one has a second job: [JobAwardUnrecognised] is what an adapter with a missing case
// returns, and [Service.AwardBid] has to refuse it rather than read it as permission to award. That
// value cannot be produced by the real adapter at all — every branch of it returns something else —
// so a stub is the only way to demonstrate that the refusal exists.
// The two halves are separate fields because the interesting cases are asymmetric: a lock that
// answers and a transition that then does not is the shape [Service.AwardBid]'s last branch exists
// for, and one field could not express it.
type brokenAwarding struct {
	lock    JobAward
	lockErr error
	move    JobAward
	moveErr error
}

func (b brokenAwarding) LockForAward(context.Context, db.Runner, uuid.UUID, uuid.UUID) (JobAward, error) {
	return b.lock, b.lockErr
}

func (b brokenAwarding) MoveToAwarded(context.Context, db.Runner, uuid.UUID, uuid.UUID) (JobAward, error) {
	return b.move, b.moveErr
}

// market is a verified provider who can bid, a customer, and one published job they can bid on.
//
// Every filter accepts at the start. Each test then breaks exactly one thing, which is what makes a
// failure legible: if the happy path and one exclusion both fail, the fixture is wrong, and if only
// the exclusion fails, the rule is.
type market struct {
	pool     *pgxpool.Pool
	svc      *Service
	provider uuid.UUID
	customer uuid.UUID
	job      uuid.UUID
}

// newMarket builds the eligible case: a verified provider serving Victoria with a van in service,
// and an Open job picking up in Richmond that the van can carry.
func newMarket(t *testing.T) market {
	t.Helper()

	pool := pgtest.DB(t)
	m := market{
		pool:     pool,
		svc:      newTestService(),
		provider: newVerifiedProvider(t, pool, "bid-provider@example.com", "+61400000840"),
		customer: newCustomer(t, pool, "bid-customer@example.com", "+61400000841"),
	}

	declare(t, pool, m.provider, "VIC")
	addVehicle(t, pool, m.provider, "BID001")
	m.job = m.publish(t)
	return m
}

// publish writes a job the fixture provider is eligible for and moves it to Open.
//
// It carries a budget, and that is not decoration: every test asserting a provider never sees the
// customer's maximum would pass forever against a job that has none.
func (m market) publish(t *testing.T) uuid.UUID {
	t.Helper()

	id, err := uuid.NewV7()
	if err != nil {
		t.Fatalf("generating a job id: %v", err)
	}

	exec(t, m.pool, `
		INSERT INTO jobs (id, customer_id, pickup_line, pickup_suburb, pickup_state, pickup_postcode,
		                  dropoff_suburb, dropoff_state, dropoff_postcode,
		                  goods_description, weight_kg, length_cm, width_cm, height_cm, budget)
		VALUES ($1, $2, '5 Church Street', 'Richmond', 'VIC', '3121',
		        'Melbourne', 'VIC', '3000',
		        'Two-seater sofa', 80, 190, 90, 80, 4321.99)`, id, m.customer)

	transition(t, m.pool, id, m.customer, "Draft", "Open")
	exec(t, m.pool, `UPDATE jobs SET expires_at = $2 WHERE id = $1`, id, farFuture)
	return id
}

// offer is a complete, valid offer with the key the caller names.
//
// One helper rather than a literal per test, so that a test breaking one field is visibly breaking
// one field.
func offer(key string) Offer {
	return Offer{
		AmountCents: 45000,
		PickupAt:    testInstant.Add(48 * time.Hour),
		DeliverBy:   testInstant.Add(56 * time.Hour),
		Message:     "Can collect from the loading dock.",
		Key:         key,
	}
}

// place runs one placement in a transaction, which is what [Service.PlaceBid] requires.
func (m market) place(t *testing.T, provider, job uuid.UUID, o Offer) (Bid, bool, error) {
	t.Helper()

	var (
		bid     Bid
		created bool
	)
	err := db.InTx(t.Context(), m.pool, func(ctx context.Context, r db.Runner) error {
		var err error
		bid, created, err = m.svc.PlaceBid(ctx, r, provider, job, o)
		return err
	})
	return bid, created, err
}

// revise runs one revision in a transaction, which is what [Service.ReviseBid] requires.
func (m market) revise(t *testing.T, provider, job, bid uuid.UUID, rev Revision) (Bid, error) {
	t.Helper()

	var revised Bid
	err := db.InTx(t.Context(), m.pool, func(ctx context.Context, r db.Runner) error {
		var err error
		revised, err = m.svc.ReviseBid(ctx, r, provider, job, bid, rev)
		return err
	})
	return revised, err
}

// withdraw runs one withdrawal in a transaction, which is what [Service.WithdrawBid] requires.
func (m market) withdraw(t *testing.T, provider, job, bid uuid.UUID) (Bid, error) {
	t.Helper()

	var withdrawn Bid
	err := db.InTx(t.Context(), m.pool, func(ctx context.Context, r db.Runner) error {
		var err error
		withdrawn, err = m.svc.WithdrawBid(ctx, r, provider, job, bid)
		return err
	})
	return withdrawn, err
}

// counter runs one counter-offer in a transaction, which is what [Service.CounterOffer] requires.
func (m market) counter(t *testing.T, caller, job, bid uuid.UUID, c Counter) (Bid, bool, error) {
	t.Helper()

	var (
		made    Bid
		created bool
	)
	err := db.InTx(t.Context(), m.pool, func(ctx context.Context, r db.Runner) error {
		var err error
		made, created, err = m.svc.CounterOffer(ctx, r, caller, job, bid, c)
		return err
	})
	return made, created, err
}

// award runs one award in a transaction, which is what [Service.AwardBid] requires (SHIP-92).
//
// The customer is a parameter rather than the fixture's, because "only the customer may award" is
// itself a rule under test — a helper that always supplied the right one could not exercise it.
func (m market) award(t *testing.T, customer, job, bid uuid.UUID) (Bid, error) {
	t.Helper()

	var accepted Bid
	err := db.InTx(t.Context(), m.pool, func(ctx context.Context, r db.Runner) error {
		var err error
		accepted, err = m.svc.AwardBid(ctx, r, customer, job, bid)
		return err
	})
	return accepted, err
}

// rival is a second eligible provider, built from one number (SHIP-93).
//
// The sweep is not a sweep against one competitor, and building three by hand is three copies of the
// same four statements where the only interesting difference is which offer wins. `n` is the whole of
// an account's identity here — an address, a mobile in this package's own block and a registration —
// so a test names a rival by a number and cannot collide with another test's by accident.
func (m market) rival(t *testing.T, n int) uuid.UUID {
	t.Helper()

	id := newVerifiedProvider(t, m.pool,
		fmt.Sprintf("bid-rival-%d@example.com", n), fmt.Sprintf("+6140000%04d", n))
	declare(t, m.pool, id, "VIC")
	addVehicle(t, m.pool, id, fmt.Sprintf("RIV%03d", n%1000))
	return id
}

// statuses is what every bid on one job reads, keyed by identifier.
//
// Read out of the table rather than assembled from what each call returned, for the reason [market.row]
// is: **the sweep writes rows no caller ever named**, so a test built from return values could not see
// what it did — which is the whole of what SHIP-93 has to demonstrate.
func (m market) statuses(t *testing.T, job uuid.UUID) map[uuid.UUID]string {
	t.Helper()

	rows, err := m.pool.Query(t.Context(),
		`SELECT id, status FROM bids WHERE job_id = $1`, job)
	if err != nil {
		t.Fatalf("reading the bids on %s: %v", job, err)
	}
	defer rows.Close()

	found := map[uuid.UUID]string{}
	for rows.Next() {
		var (
			id     uuid.UUID
			status string
		)
		if err := rows.Scan(&id, &status); err != nil {
			t.Fatalf("scanning a bid on %s: %v", job, err)
		}
		found[id] = status
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("reading the bids on %s: %v", job, err)
	}
	return found
}

// jobStatus is the job's status and how many transitions have been recorded against it.
//
// Read together, and out of the tables rather than off anything the service said about itself: an
// award that moved the status without leaving a history row would be a transition that bypassed the
// guard, and 000402's trigger makes that impossible — so a test that checked only the status could
// not tell a real transition from one that never happened.
func (m market) jobStatus(t *testing.T, job uuid.UUID) (string, int) {
	t.Helper()

	var (
		status  string
		changes int
	)
	if err := m.pool.QueryRow(t.Context(), `
		SELECT j.status, (SELECT count(*) FROM job_status_history WHERE job_id = j.id)
		FROM jobs j WHERE j.id = $1`, job).Scan(&status, &changes); err != nil {
		t.Fatalf("reading job %s: %v", job, err)
	}
	return status, changes
}

// chain reads one negotiation's history, on the pool rather than in a transaction — which is what
// [Service.Chain] is built for and what the handler passes it.
func (m market) chain(t *testing.T, caller, job, bid uuid.UUID) ([]Bid, bool, error) {
	t.Helper()
	return m.svc.Chain(t.Context(), m.pool, caller, job, bid)
}

// counterOf is a counter-offer changing the price alone, which is the ordinary shape.
//
// Price alone deliberately: it is the case 000501 named when it removed `ck_bids_offer_has_timing`
// ("a customer countering on *price alone*"), so the fixture exercises the inheritance rather than
// hiding it behind a fully stated offer.
func counterOf(cents int64, key string) Counter {
	return Counter{AmountCents: ptr(cents), Key: key}
}

// row is the stored bid, read straight out of the table rather than off whatever the service said
// about itself.
//
// Every assertion about what a revision or a withdrawal *wrote* goes through this: a method returning
// the value it meant to store would satisfy a test comparing against its own return value even if the
// `UPDATE` had touched nothing.
func (m market) row(t *testing.T, bid uuid.UUID) storedBid {
	t.Helper()

	var (
		s            storedBid
		message      *string
		key          *string
		pickupAt     *time.Time
		deliverBy    *time.Time
		supersededBy *uuid.UUID
	)
	if err := m.pool.QueryRow(t.Context(), `
		SELECT status, offered_by, amount, pickup_at, deliver_by, message, idempotency_key,
		       superseded_by, updated_at
		FROM bids WHERE id = $1`, bid).
		Scan(&s.status, &s.offeredBy, &s.amount, &pickupAt, &deliverBy, &message, &key,
			&supersededBy, &s.updatedAt); err != nil {
		t.Fatalf("reading bid %s: %v", bid, err)
	}
	if supersededBy != nil {
		s.supersededBy = *supersededBy
	}
	if pickupAt != nil {
		s.pickupAt = *pickupAt
	}
	if deliverBy != nil {
		s.deliverBy = *deliverBy
	}
	if message != nil {
		s.message = *message
	}
	if key != nil {
		s.key = *key
	}
	return s
}

// storedBid is one row of the `bids` table, as the tests read it.
type storedBid struct {
	status       string
	offeredBy    string
	amount       float64
	pickupAt     time.Time
	deliverBy    time.Time
	message      string
	key          string
	supersededBy uuid.UUID
	updatedAt    time.Time
}

// setStatus moves a bid directly, for the statuses no endpoint can reach yet.
//
// Expired is SHIP-89's and has no writer yet. Accepted, Rejected and Superseded all have one now —
// the award, its sweep and a counter — and a test still writes them directly when what it needs is a
// *starting* state rather than the act that produces one. Unlike a job, a bid's status has no trigger
// guarding it
// (000500 says why), so this is the same statement the platform itself would run — not a back door
// around a guard.
func (m market) setStatus(t *testing.T, bid uuid.UUID, status Status) {
	t.Helper()
	exec(t, m.pool, `UPDATE bids SET status = $2 WHERE id = $1`, bid, string(status))
}

// ptr is a pointer to a literal, which [Revision]'s optional fields need at every call site.
func ptr[T any](v T) *T { return &v }

// bids is how many rows this provider has on this job, whatever their status.
func (m market) bids(t *testing.T, provider, job uuid.UUID) int {
	t.Helper()

	var n int
	if err := m.pool.QueryRow(t.Context(),
		`SELECT count(*) FROM bids WHERE provider_id = $1 AND job_id = $2`, provider, job).Scan(&n); err != nil {
		t.Fatalf("counting bids: %v", err)
	}
	return n
}

// --- accounts and the fleet behind them ---------------------------------------------------------

func newAccount(t *testing.T, pool *pgxpool.Pool, email, phone, role string) uuid.UUID {
	t.Helper()

	id, err := uuid.NewV7()
	if err != nil {
		t.Fatalf("generating an id: %v", err)
	}
	exec(t, pool,
		`INSERT INTO users (id, email, phone, password_hash, role) VALUES ($1, $2, $3, 'x', $4)`,
		id, email, phone, role)
	return id
}

// newVerifiedProvider is a provider who has met Docs/04 §3's automated baseline.
//
// Kept separate from a bare provider on purpose: every eligibility test here would pass against a
// filter that ignored verification if the shared helper quietly verified everybody.
func newVerifiedProvider(t *testing.T, pool *pgxpool.Pool, email, phone string) uuid.UUID {
	t.Helper()

	id := newAccount(t, pool, email, phone, "provider")
	exec(t, pool, `UPDATE users SET email_verified_at = now(), phone_verified_at = now() WHERE id = $1`, id)
	return id
}

func newCustomer(t *testing.T, pool *pgxpool.Pool, email, phone string) uuid.UUID {
	t.Helper()
	return newAccount(t, pool, email, phone, "customer")
}

// declare gives a provider a service area through fleet's own service rather than by writing rows.
func declare(t *testing.T, pool *pgxpool.Pool, provider uuid.UUID, states ...string) {
	t.Helper()

	list := states
	if err := db.InTx(t.Context(), pool, func(ctx context.Context, r db.Runner) error {
		_, err := fleet.NewService(clock.NewFixed(testInstant)).
			Declare(ctx, r, provider, fleet.ProfileFields{States: &list})
		return err
	}); err != nil {
		t.Fatalf("declaring %v for %s: %v", states, provider, err)
	}
}

// addVehicle puts a van in the provider's fleet, big enough for the fixture job.
func addVehicle(t *testing.T, pool *pgxpool.Pool, provider uuid.UUID, registration string) {
	t.Helper()

	kind := fleet.TypeVan
	weight, length, width, height := 1200.0, 300, 160, 180
	if _, err := fleet.NewService(clock.NewFixed(testInstant)).Add(t.Context(), pool, provider,
		fleet.VehicleFields{
			Registration: &registration,
			Type:         &kind,
			MaxWeightKg:  &weight,
			LengthCm:     &length,
			WidthCm:      &width,
			HeightCm:     &height,
		}); err != nil {
		t.Fatalf("adding %s to %s: %v", registration, provider, err)
	}
}

// --- moving a job the way the platform does -----------------------------------------------------

// transition moves a job the only way 000402 permits: a job_status_history row written in the same
// transaction and named by `shipper.job_status_transition`.
//
// This package's non-test code cannot import `jobs` to use the guard's Go side. What a test can do
// is satisfy the same trigger the guard satisfies — so these fixtures move jobs exactly as the
// platform does, rather than through a back door the platform does not have.
func transition(t *testing.T, pool *pgxpool.Pool, job, actor uuid.UUID, from, to string) {
	t.Helper()
	transitionBy(t, pool, "customer", job, actor, from, to)
}

// transitionBy is [transition] with the actor's kind named (SHIP-94).
//
// Every move in this package's fixtures was a customer's until an award had to be followed by the
// delivery *starting*, and `Awarded → En route to pickup` is the provider's. A history row attributing
// it to the customer would be a fixture that lies about who acted — which matters here more than it
// looks, because the row is the whole of what 000402's trigger reads.
func transitionBy(t *testing.T, pool *pgxpool.Pool, actorType string, job, actor uuid.UUID, from, to string) {
	t.Helper()

	if err := db.InTx(t.Context(), pool, func(ctx context.Context, r db.Runner) error {
		entry, err := uuid.NewV7()
		if err != nil {
			return err
		}
		if _, err := r.Exec(ctx, `
			INSERT INTO job_status_history
				(id, job_id, from_status, to_status, actor_type, actor_id, actor_recorded_at)
			VALUES ($1, $2, $3, $4, $7, $5, $6)`,
			entry, job, from, to, actor, publishInstant, actorType); err != nil {
			return err
		}
		if _, err := r.Exec(ctx,
			`SELECT set_config('shipper.job_status_transition', $1::text, true)`, entry); err != nil {
			return err
		}
		_, err = r.Exec(ctx, `UPDATE jobs SET status = $2 WHERE id = $1`, job, to)
		return err
	}); err != nil {
		t.Fatalf("moving %s from %s to %s: %v", job, from, to, err)
	}
}

// exec runs one statement and fails the test rather than returning an error, because every caller
// here is arranging a world rather than exercising one.
func exec(t *testing.T, pool *pgxpool.Pool, sql string, args ...any) {
	t.Helper()

	if _, err := pool.Exec(t.Context(), sql, args...); err != nil {
		t.Fatalf("%s: %v", strings.Join(strings.Fields(sql), " "), err)
	}
}
