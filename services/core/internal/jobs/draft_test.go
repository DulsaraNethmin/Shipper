package jobs

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/DulsaraNethmin/Shipper/services/core/internal/clock"
	"github.com/DulsaraNethmin/Shipper/services/core/internal/db"
	"github.com/DulsaraNethmin/Shipper/services/core/internal/httpx"
	"github.com/DulsaraNethmin/Shipper/services/core/internal/testsupport/pgtest"
)

// SHIP-60, SHIP-61 and SHIP-62 against a real PostgreSQL.
//
// Every constraint these tests lean on is the database's — the state list, the four-digit
// postcode, the coordinate pair, the positive dimension — and Docs/06 §4.1 is the argument for
// not mocking it: "a mock happily accepts a write that the actual constraint would reject."

// fakeGeocoder is the port, controlled.
//
// A local fake rather than internal/platform/geocoding's stub, and not because the import lint
// would object — it skips test files. It is that these tests need to choose the outcome: the
// difference between "there is no such place" and "we could not ask" is the distinction the wide
// signature exists to carry, and a deterministic stub cannot be made to produce the second on
// demand.
type fakeGeocoder struct {
	// asked records every address it was given, in order, so a test can assert that an
	// address that did not change was not looked up again.
	asked []string

	// unknown are addresses reported as not found.
	unknown map[string]bool

	// err, when set, is returned instead of an answer.
	err error
}

func (g *fakeGeocoder) Lookup(_ context.Context, address string) (float64, float64, string, bool, error) {
	g.asked = append(g.asked, address)

	if g.err != nil {
		return 0, 0, "", false, g.err
	}
	if g.unknown[address] {
		return 0, 0, "", false, nil
	}
	return -33.896, 151.179, "resolved: " + address, true, nil
}

func newDraftService(t *testing.T, geo Geocoder) *Service {
	t.Helper()
	return NewService(&recordingSink{}, clock.NewFixed(testInstant), geo)
}

func newProvider(t *testing.T, pool *pgxpool.Pool, email, phone string) uuid.UUID {
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

func text(s string) *string      { return &s }
func number(n int) *int          { return &n }
func decimal(f float64) *float64 { return &f }

func sydney() *Address {
	return &Address{Line: "12 Smith Street", Suburb: "Newtown", State: StateNSW, Postcode: "2042"}
}

func melbourne() *Address {
	return &Address{Line: "40 Bourke Street", Suburb: "Melbourne", State: StateVIC, Postcode: "3000"}
}

// TestCreateDraftIsOwnedByTheCallerAndStartsAsADraft is SHIP-61's acceptance criterion.
func TestCreateDraftIsOwnedByTheCallerAndStartsAsADraft(t *testing.T) {
	pool := pgtest.DB(t)
	svc := newDraftService(t, &fakeGeocoder{})

	customer := newCustomer(t, pool, "creates@example.com", "+61400000601")

	job, err := svc.CreateDraft(t.Context(), pool, customer, DraftFields{})
	if err != nil {
		t.Fatalf("creating a draft: %v", err)
	}

	if job.CustomerID != customer {
		t.Errorf("the job is owned by %s, want %s", job.CustomerID, customer)
	}
	if job.Status != StatusDraft {
		t.Errorf("the job is %s, want Draft", job.Status)
	}
	if got := statusOf(t, pool, job.ID); got != StatusDraft {
		t.Errorf("the stored job is %s, want Draft", got)
	}

	// An entirely empty body is legitimate: Docs/01 §4.1 saves drafts, and the app captures
	// a job over several steps. It must not have invented anything.
	if !job.Pickup.Address.IsZero() || !job.Dropoff.Address.IsZero() {
		t.Errorf("an empty draft came back with an address: %#v / %#v", job.Pickup, job.Dropoff)
	}
	if job.CreatedAt.IsZero() || job.UpdatedAt.IsZero() {
		t.Error("the job has no timestamps")
	}
}

// TestCreateDraftNormalisesAndResolvesBothAddresses is SHIP-60's acceptance criterion.
func TestCreateDraftNormalisesAndResolvesBothAddresses(t *testing.T) {
	pool := pgtest.DB(t)
	geo := &fakeGeocoder{}
	svc := newDraftService(t, geo)

	customer := newCustomer(t, pool, "addresses@example.com", "+61400000602")

	job, err := svc.CreateDraft(t.Context(), pool, customer, DraftFields{
		// Typed the way a person types: extra spaces, the spelled-out state, a postcode
		// with a stray space in it.
		Pickup: &Address{
			Line: "  12   Smith Street ", Suburb: " Newtown ",
			State: "new south wales", Postcode: " 20 42 ",
		},
		Dropoff: melbourne(),
	})
	if err != nil {
		t.Fatalf("creating a draft: %v", err)
	}

	want := *sydney()
	if job.Pickup.Address != want {
		t.Errorf("pickup = %#v, want %#v", job.Pickup.Address, want)
	}

	for _, l := range []struct {
		name     string
		location Location
	}{{"pickup", job.Pickup}, {"dropoff", job.Dropoff}} {
		lat, lng, resolved := l.location.Coordinate()
		if !resolved {
			t.Errorf("%s was not resolved", l.name)
			continue
		}
		if lat != -33.896 || lng != 151.179 {
			t.Errorf("%s resolved to (%v, %v)", l.name, lat, lng)
		}
		if !strings.HasPrefix(l.location.Formatted, "resolved: ") {
			t.Errorf("%s kept no formatted address: %q", l.name, l.location.Formatted)
		}
	}

	// The geocoder is asked about the normalised address, not the raw one. Asking about the
	// raw one would make the same address typed twice resolve to two different answers with
	// any provider that is not whitespace-insensitive.
	if len(geo.asked) != 2 {
		t.Fatalf("the geocoder was asked %d times, want 2: %v", len(geo.asked), geo.asked)
	}
	if want := "12 Smith Street, Newtown NSW 2042"; geo.asked[0] != want {
		t.Errorf("the geocoder was asked %q, want %q", geo.asked[0], want)
	}

	// And it is all there after a round trip, not merely in the value that was returned.
	stored := reread(t, pool, job.ID)
	if stored.Pickup != job.Pickup || stored.Dropoff != job.Dropoff {
		t.Errorf("the stored job differs from the returned one:\n stored %#v\n returned %#v",
			stored.Pickup, job.Pickup)
	}
}

// TestAFailedLookupDoesNotFailTheJob is SHIP-59a's rule, held at its first consumer.
func TestAFailedLookupDoesNotFailTheJob(t *testing.T) {
	pool := pgtest.DB(t)
	customer := newCustomer(t, pool, "unresolved@example.com", "+61400000603")

	cases := map[string]Geocoder{
		// The provider answered and did not recognise the address — a legitimate rural
		// address the vendor has never heard of.
		"not found": &fakeGeocoder{unknown: map[string]bool{
			"12 Smith Street, Newtown NSW 2042": true,
		}},

		// The lookup did not complete: an unreachable provider, a refused credential.
		"the lookup failed": &fakeGeocoder{err: errors.New("the provider is unreachable")},

		// No provider configured at all, which is what staging and production have today.
		"no geocoder": nil,
	}

	for name, geo := range cases {
		t.Run(name, func(t *testing.T) {
			job, err := newDraftService(t, geo).CreateDraft(t.Context(), pool, customer,
				DraftFields{Pickup: sydney()})
			if err != nil {
				t.Fatalf("creating a draft: %v", err)
			}

			if job.Pickup.Address != *sydney() {
				t.Errorf("the address was not stored as typed: %#v", job.Pickup.Address)
			}
			if _, _, resolved := job.Pickup.Coordinate(); resolved {
				t.Error("an address that did not resolve came back with a coordinate")
			}
			if job.Status != StatusDraft {
				t.Errorf("the job is %s, want Draft", job.Status)
			}
		})
	}
}

// 000400 says the customer-account rule is enforced where the draft is created, because a foreign
// key cannot see another table's column. This is that enforcement.
func TestCreateDraftRefusesAnAccountThatIsNotACustomer(t *testing.T) {
	pool := pgtest.DB(t)
	svc := newDraftService(t, &fakeGeocoder{})

	provider := newProvider(t, pool, "bidder@example.com", "+61400000604")

	if _, err := svc.CreateDraft(t.Context(), pool, provider, DraftFields{}); !errors.Is(err, ErrNotCustomer) {
		t.Fatalf("a provider created a job: %v", err)
	}

	// And an account that does not exist at all, which is what a token outliving its user
	// looks like.
	if _, err := svc.CreateDraft(t.Context(), pool, uuid.Must(uuid.NewV7()), DraftFields{}); !errors.Is(err, ErrNotCustomer) {
		t.Fatalf("a job was created for an account that does not exist: %v", err)
	}
}

// Validation answers in the error contract's own shape, with one entry per offending field, so a
// customer filling in a form is shown everything that needs attention at once.
func TestCreateDraftReportsEveryOffendingFieldAtOnce(t *testing.T) {
	pool := pgtest.DB(t)
	svc := newDraftService(t, &fakeGeocoder{})
	customer := newCustomer(t, pool, "invalid@example.com", "+61400000605")

	_, err := svc.CreateDraft(t.Context(), pool, customer, DraftFields{
		Pickup:   &Address{Line: "12 Smith Street", Suburb: "Newtown", State: "Westeros", Postcode: "20"},
		LengthCm: number(-5),
		WeightKg: decimal(500_000),
	})

	var apiErr *httpx.Error
	if !errors.As(err, &apiErr) {
		t.Fatalf("err = %v, want a validation error in the contract's shape", err)
	}
	if apiErr.Status != http.StatusUnprocessableEntity {
		t.Errorf("status = %d, want 422", apiErr.Status)
	}

	got := offendingFields(apiErr.Details)
	want := []string{"length_cm", "pickup.postcode", "pickup.state", "weight_kg"}
	if len(got) != len(want) {
		t.Fatalf("fields = %v, want %v", got, want)
	}
	for i := range got {
		if got[i] != want[i] {
			t.Fatalf("fields = %v, want %v", got, want)
		}
	}

	// Nothing was written. A validation failure that had already created a row would leave
	// the customer with a draft they did not ask for.
	var jobs int
	if err := pool.QueryRow(t.Context(),
		`SELECT count(*) FROM jobs WHERE customer_id = $1`, customer).Scan(&jobs); err != nil {
		t.Fatalf("counting jobs: %v", err)
	}
	if jobs != 0 {
		t.Errorf("%d jobs were created by a request that failed validation", jobs)
	}
}

// Everything a draft can hold survives a round trip through the columns, including the values the
// database stores as NULL.
func TestEveryDraftFieldSurvivesARoundTrip(t *testing.T) {
	pool := pgtest.DB(t)
	svc := newDraftService(t, &fakeGeocoder{})

	customer := newCustomer(t, pool, "roundtrip@example.com", "+61400000614")

	pickupWindow := TimeWindow{
		Start: time.Date(2026, 8, 14, 9, 0, 0, 0, time.UTC),
		End:   time.Date(2026, 8, 15, 17, 0, 0, 0, time.UTC),
	}
	dropoffWindow := TimeWindow{End: time.Date(2026, 8, 16, 12, 0, 0, 0, time.UTC)}

	created, err := svc.CreateDraft(t.Context(), pool, customer, DraftFields{
		Pickup:             sydney(),
		Dropoff:            melbourne(),
		GoodsDescription:   text("Two-seater sofa, wrapped, no legs attached"),
		LengthCm:           number(190),
		WidthCm:            number(90),
		HeightCm:           number(85),
		WeightKg:           decimal(45.5),
		VehicleRequirement: text("Ute with a tailgate lifter"),
		HandlingNotes:      text("Second-floor walk-up, no lift.\nBuzzer 4B."),
		PickupWindow:       &pickupWindow,
		DropoffWindow:      &dropoffWindow,
	})
	if err != nil {
		t.Fatalf("creating a draft: %v", err)
	}

	stored := reread(t, pool, created.ID)

	switch {
	case stored.GoodsDescription != created.GoodsDescription:
		t.Errorf("goods description = %q", stored.GoodsDescription)
	case stored.Dimensions != Dimensions{LengthCm: 190, WidthCm: 90, HeightCm: 85}:
		t.Errorf("dimensions = %#v", stored.Dimensions)
	case stored.WeightKg != 45.5:
		t.Errorf("weight = %v", stored.WeightKg)
	case stored.VehicleRequirement != "Ute with a tailgate lifter":
		t.Errorf("vehicle requirement = %q", stored.VehicleRequirement)
	// Trimmed, not collapsed: a handling note is a paragraph a driver reads, and running
	// its lines together would join three separate instructions.
	case !strings.Contains(stored.HandlingNotes, "\n"):
		t.Errorf("the handling notes lost their line break: %q", stored.HandlingNotes)
	case !stored.PickupWindow.Start.Equal(pickupWindow.Start):
		t.Errorf("pickup window start = %v", stored.PickupWindow.Start)
	case !stored.DropoffWindow.End.Equal(dropoffWindow.End):
		t.Errorf("dropoff window end = %v", stored.DropoffWindow.End)
	// A window with only one end is a half-open window and not a missing one.
	case !stored.DropoffWindow.Start.IsZero():
		t.Errorf("dropoff window start = %v, want absent", stored.DropoffWindow.Start)
	}
}

// reread reads a job back through the store, so a test asserts what the columns hold rather than
// what the method returned.
func reread(t *testing.T, pool *pgxpool.Pool, id uuid.UUID) Job {
	t.Helper()

	var job Job
	if err := db.InTx(t.Context(), pool, func(ctx context.Context, r db.Runner) error {
		var err error
		job, err = postgresStore{}.lockJob(ctx, r, id)
		return err
	}); err != nil {
		t.Fatalf("re-reading %s: %v", id, err)
	}
	return job
}
