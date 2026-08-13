package bidding

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/DulsaraNethmin/Shipper/services/core/internal/clock"
	"github.com/DulsaraNethmin/Shipper/services/core/internal/db"
	"github.com/DulsaraNethmin/Shipper/services/core/internal/fleet"
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

// newTestService is the domain wired to the real eligibility filter.
func newTestService() *Service {
	return NewService(fleet.NewService(clock.NewFixed(testInstant)), clock.NewFixed(testInstant))
}

// refusing is [Eligibility] answering "no" to everything.
//
// The one test double here, and it earns its place: [TestAnEligibilityFailureIsNotARefusal] needs
// the port to *fail* rather than to decline, which no arrangement of rows can produce. Everything
// else goes through the real filter.
type refusing struct{ err error }

func (r refusing) EligibleFor(context.Context, db.Runner, uuid.UUID, uuid.UUID) (bool, error) {
	return false, r.err
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

// row is the stored bid, read straight out of the table rather than off whatever the service said
// about itself.
//
// Every assertion about what a revision or a withdrawal *wrote* goes through this: a method returning
// the value it meant to store would satisfy a test comparing against its own return value even if the
// `UPDATE` had touched nothing.
func (m market) row(t *testing.T, bid uuid.UUID) storedBid {
	t.Helper()

	var (
		s         storedBid
		message   *string
		key       *string
		pickupAt  *time.Time
		deliverBy *time.Time
	)
	if err := m.pool.QueryRow(t.Context(), `
		SELECT status, amount, pickup_at, deliver_by, message, idempotency_key, updated_at
		FROM bids WHERE id = $1`, bid).
		Scan(&s.status, &s.amount, &pickupAt, &deliverBy, &message, &key, &s.updatedAt); err != nil {
		t.Fatalf("reading bid %s: %v", bid, err)
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
	status    string
	amount    float64
	pickupAt  time.Time
	deliverBy time.Time
	message   string
	key       string
	updatedAt time.Time
}

// setStatus moves a bid directly, for the statuses no endpoint can reach yet.
//
// Accepted is SHIP-92's, Rejected is SHIP-93's, Expired is SHIP-89's and Superseded is SHIP-88's, so a
// test that needs one of them writes it. Unlike a job, a bid's status has no trigger guarding it
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

	if err := db.InTx(t.Context(), pool, func(ctx context.Context, r db.Runner) error {
		entry, err := uuid.NewV7()
		if err != nil {
			return err
		}
		if _, err := r.Exec(ctx, `
			INSERT INTO job_status_history
				(id, job_id, from_status, to_status, actor_type, actor_id, actor_recorded_at)
			VALUES ($1, $2, $3, $4, 'customer', $5, $6)`,
			entry, job, from, to, actor, publishInstant); err != nil {
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
