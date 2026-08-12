package fleet

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

// SHIP-78 against a real PostgreSQL.
//
// Docs/06 §4.1 is the argument for not mocking it — "a mock happily accepts a write that the actual
// constraint would reject" — and this domain is the clearest example of that so far: the
// one-live-vehicle-per-plate rule is a *partial* unique index, and every test below that leans on it
// would pass against a repository interface while the real race lost.

var testInstant = time.Date(2026, 8, 11, 3, 30, 0, 0, time.UTC)

func newTestService() *Service { return NewService(clock.NewFixed(testInstant)) }

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

func newProvider(t *testing.T, pool *pgxpool.Pool, email, phone string) uuid.UUID {
	t.Helper()
	return newAccount(t, pool, email, phone, "provider")
}

// inTx runs fn inside a transaction, which is what every writing method after Add requires.
func inTx(t *testing.T, pool *pgxpool.Pool, fn func(ctx context.Context, r db.Runner) error) error {
	t.Helper()
	return db.InTx(t.Context(), pool, fn)
}

// ptr is the three-way distinction VehicleFields rests on, written once.
func ptr[T any](v T) *T { return &v }

// added is a vehicle in the provider's fleet, created the only way the domain creates one.
func added(t *testing.T, pool *pgxpool.Pool, provider uuid.UUID, registration string) Vehicle {
	t.Helper()

	vehicle, err := newTestService().Add(t.Context(), pool, provider, VehicleFields{
		Registration: ptr(registration),
		Type:         ptr(TypeVan),
	})
	if err != nil {
		t.Fatalf("adding %s: %v", registration, err)
	}
	return vehicle
}

// TestAProviderAddsAVehicle is the first third of SHIP-78's acceptance criterion.
func TestAProviderAddsAVehicle(t *testing.T) {
	pool := pgtest.DB(t)
	provider := newProvider(t, pool, "fleet-add@example.com", "+61400000310")

	vehicle, err := newTestService().Add(t.Context(), pool, provider, VehicleFields{
		Registration: ptr("  abc 123 "),
		Type:         ptr(VehicleType(" Van ")),
		Make:         ptr("Mercedes-Benz"),
		Model:        ptr("Sprinter  314"),
		MaxWeightKg:  ptr(1200.5),
		LengthCm:     ptr(320),
	})
	if err != nil {
		t.Fatalf("Add() = %v", err)
	}

	switch {
	case vehicle.ID == uuid.Nil:
		t.Error("the vehicle has no id")
	case vehicle.ProviderID != provider:
		t.Errorf("owned by %s, want %s", vehicle.ProviderID, provider)
	case !vehicle.Active():
		t.Error("a vehicle was added out of service")
	}

	// Normalisation is checked on the way out because it is what the *column* holds: a plate
	// stored as typed would let one truck be recorded twice under two spellings.
	if vehicle.Registration != "ABC123" {
		t.Errorf("registration = %q, want ABC123 — upper case, spaces removed", vehicle.Registration)
	}
	if vehicle.Type != TypeVan {
		t.Errorf("type = %q, want van", vehicle.Type)
	}
	if vehicle.Model != "Sprinter 314" {
		t.Errorf("model = %q, want the whitespace collapsed", vehicle.Model)
	}
	if vehicle.Capacity.MaxWeightKg != 1200.5 || vehicle.Capacity.LengthCm != 320 {
		t.Errorf("capacity = %+v, want the stated weight and length", vehicle.Capacity)
	}

	// A capacity nobody stated is absent rather than zero, which is what the columns' own
	// constraints make unambiguous.
	if vehicle.Capacity.WidthCm != 0 {
		t.Errorf("width = %d, want it unstated", vehicle.Capacity.WidthCm)
	}
}

// TestOnlyAProviderMayKeepAFleet is read from users.role rather than from a token claim.
//
// 000300 has no CHECK for it — a foreign key cannot see another table's column — so this is the
// enforcement, and it has to be tested where the row is written.
func TestOnlyAProviderMayKeepAFleet(t *testing.T) {
	pool := pgtest.DB(t)
	customer := newAccount(t, pool, "fleet-customer@example.com", "+61400000311", "customer")

	_, err := newTestService().Add(t.Context(), pool, customer, VehicleFields{
		Registration: ptr("CUS111"),
		Type:         ptr(TypeUte),
	})
	if !errors.Is(err, ErrNotProvider) {
		t.Fatalf("Add() = %v, want ErrNotProvider", err)
	}

	var count int
	if err := pool.QueryRow(t.Context(),
		`SELECT count(*) FROM vehicles WHERE provider_id = $1`, customer).Scan(&count); err != nil {
		t.Fatalf("counting: %v", err)
	}
	if count != 0 {
		t.Errorf("a customer has %d vehicles", count)
	}
}

// TestAddingRequiresARegistrationAndAType reports both at once.
//
// Docs/10 §4.6: a provider filling in a form should be shown every field that needs attention in
// one answer rather than discovering them one request at a time.
func TestAddingRequiresARegistrationAndAType(t *testing.T) {
	pool := pgtest.DB(t)
	provider := newProvider(t, pool, "fleet-required@example.com", "+61400000312")

	_, err := newTestService().Add(t.Context(), pool, provider, VehicleFields{Make: ptr("Isuzu")})

	var apiErr *httpx.Error
	if !errors.As(err, &apiErr) {
		t.Fatalf("Add() = %v, want an error in the contract's shape", err)
	}
	if apiErr.Status != http.StatusUnprocessableEntity {
		t.Errorf("status = %d, want 422", apiErr.Status)
	}

	named := map[string]bool{}
	for _, problem := range apiErr.Details {
		named[problem.Field] = true
	}
	for _, field := range []string{"registration", "vehicle_type"} {
		if !named[field] {
			t.Errorf("no detail names %s; got %v", field, apiErr.Details)
		}
	}
}

// TestAnUnknownVehicleTypeIsAFieldError keeps the refusal legible.
//
// The database would refuse it too, and that is the point of checking here as well: a provider
// handed ck_vehicles_type learns nothing they can act on.
func TestAnUnknownVehicleTypeIsAFieldError(t *testing.T) {
	pool := pgtest.DB(t)
	provider := newProvider(t, pool, "fleet-badtype@example.com", "+61400000313")

	_, err := newTestService().Add(t.Context(), pool, provider, VehicleFields{
		Registration: ptr("BAD111"),
		Type:         ptr(VehicleType("hovercraft")),
	})

	var apiErr *httpx.Error
	if !errors.As(err, &apiErr) {
		t.Fatalf("Add() = %v, want an error in the contract's shape", err)
	}
	if len(apiErr.Details) != 1 || apiErr.Details[0].Field != "vehicle_type" {
		t.Errorf("details = %v, want one naming vehicle_type", apiErr.Details)
	}
	if strings.Contains(apiErr.Details[0].Message, "van") {
		t.Error("the message enumerates the valid types, which is a second copy of the list")
	}
}

// TestOneLiveVehiclePerPlate is the index reported as a domain error.
func TestOneLiveVehiclePerPlate(t *testing.T) {
	pool := pgtest.DB(t)
	provider := newProvider(t, pool, "fleet-plate@example.com", "+61400000314")

	added(t, pool, provider, "SAME11")

	// The same plate written the other way round, which is exactly what normalisation exists to
	// catch: without it this would be a second live row for one truck.
	_, err := newTestService().Add(t.Context(), pool, provider, VehicleFields{
		Registration: ptr("same 11"),
		Type:         ptr(TypeUte),
	})
	if !errors.Is(err, ErrDuplicateRegistration) {
		t.Fatalf("Add() = %v, want ErrDuplicateRegistration", err)
	}
}

// TestAProviderEditsTheirVehicle is the second third of the acceptance criterion.
func TestAProviderEditsTheirVehicle(t *testing.T) {
	pool := pgtest.DB(t)
	provider := newProvider(t, pool, "fleet-edit@example.com", "+61400000315")
	svc := newTestService()

	vehicle, err := svc.Add(t.Context(), pool, provider, VehicleFields{
		Registration: ptr("EDT111"),
		Type:         ptr(TypeVan),
		Make:         ptr("Isuzu"),
		MaxWeightKg:  ptr(4500.0),
	})
	if err != nil {
		t.Fatalf("Add() = %v", err)
	}

	var edited Vehicle
	err = inTx(t, pool, func(ctx context.Context, r db.Runner) error {
		var err error
		edited, err = svc.Update(ctx, r, provider, vehicle.ID, VehicleFields{
			Type:        ptr(TypeBoxTruck),
			MaxWeightKg: ptr(0.0),
			WidthCm:     ptr(175),
		})
		return err
	})
	if err != nil {
		t.Fatalf("Update() = %v", err)
	}

	switch {
	case edited.Type != TypeBoxTruck:
		t.Errorf("type = %q, want box_truck", edited.Type)
	case edited.Capacity.WidthCm != 175:
		t.Errorf("width = %d, want 175", edited.Capacity.WidthCm)

	// Absent means unchanged and empty means cleared, and they are not the same request:
	// without the second a provider could state a capacity and never take it back.
	case edited.Make != "Isuzu":
		t.Errorf("make = %q, want it untouched", edited.Make)
	case edited.Capacity.MaxWeightKg != 0:
		t.Errorf("max weight = %v, want it cleared", edited.Capacity.MaxWeightKg)
	}
}

// TestAnEditCannotBlankTheIdentifyingFields keeps a row describable.
func TestAnEditCannotBlankTheIdentifyingFields(t *testing.T) {
	pool := pgtest.DB(t)
	provider := newProvider(t, pool, "fleet-blank@example.com", "+61400000316")
	svc := newTestService()
	vehicle := added(t, pool, provider, "BLK111")

	err := inTx(t, pool, func(ctx context.Context, r db.Runner) error {
		_, err := svc.Update(ctx, r, provider, vehicle.ID, VehicleFields{Registration: ptr("   ")})
		return err
	})

	var apiErr *httpx.Error
	if !errors.As(err, &apiErr) {
		t.Fatalf("Update() = %v, want an error in the contract's shape", err)
	}
	if len(apiErr.Details) != 1 || apiErr.Details[0].Field != "registration" {
		t.Errorf("details = %v, want one naming registration", apiErr.Details)
	}
}

// TestAnEditThatNamesNoFieldIsRefused, rather than answering 200 with an unchanged vehicle.
func TestAnEditThatNamesNoFieldIsRefused(t *testing.T) {
	pool := pgtest.DB(t)
	provider := newProvider(t, pool, "fleet-noop@example.com", "+61400000317")
	svc := newTestService()
	vehicle := added(t, pool, provider, "NOP111")

	err := inTx(t, pool, func(ctx context.Context, r db.Runner) error {
		_, err := svc.Update(ctx, r, provider, vehicle.ID, VehicleFields{})
		return err
	})
	if !errors.Is(err, ErrNothingToUpdate) {
		t.Fatalf("Update() = %v, want ErrNothingToUpdate", err)
	}
}

// TestAStrangerCannotReachAnothersVehicle covers all four operations at once.
//
// The rule is per operation rather than per domain: a fleet is discoverable through whichever route
// forgets the check, so a test that only covered the edit would leave three ways in.
func TestAStrangerCannotReachAnothersVehicle(t *testing.T) {
	pool := pgtest.DB(t)
	owner := newProvider(t, pool, "fleet-owner@example.com", "+61400000318")
	stranger := newProvider(t, pool, "fleet-stranger@example.com", "+61400000319")
	svc := newTestService()

	vehicle := added(t, pool, owner, "OWN111")

	t.Run("read", func(t *testing.T) {
		_, err := svc.Vehicle(t.Context(), pool, stranger, vehicle.ID)
		if !errors.Is(err, ErrNotVehicleOwner) {
			t.Errorf("Vehicle() = %v, want ErrNotVehicleOwner", err)
		}
	})

	t.Run("edit", func(t *testing.T) {
		err := inTx(t, pool, func(ctx context.Context, r db.Runner) error {
			_, err := svc.Update(ctx, r, stranger, vehicle.ID, VehicleFields{Make: ptr("Hino")})
			return err
		})
		if !errors.Is(err, ErrNotVehicleOwner) {
			t.Errorf("Update() = %v, want ErrNotVehicleOwner", err)
		}
	})

	t.Run("deactivate", func(t *testing.T) {
		err := inTx(t, pool, func(ctx context.Context, r db.Runner) error {
			_, err := svc.Deactivate(ctx, r, stranger, vehicle.ID)
			return err
		})
		if !errors.Is(err, ErrNotVehicleOwner) {
			t.Errorf("Deactivate() = %v, want ErrNotVehicleOwner", err)
		}
	})

	t.Run("reactivate", func(t *testing.T) {
		err := inTx(t, pool, func(ctx context.Context, r db.Runner) error {
			_, err := svc.Reactivate(ctx, r, stranger, vehicle.ID)
			return err
		})
		if !errors.Is(err, ErrNotVehicleOwner) {
			t.Errorf("Reactivate() = %v, want ErrNotVehicleOwner", err)
		}
	})

	// The refusals are refusals, not rollbacks that happened to work.
	unchanged, err := svc.Vehicle(t.Context(), pool, owner, vehicle.ID)
	if err != nil {
		t.Fatalf("reading it back: %v", err)
	}
	if unchanged.Make != "" || !unchanged.Active() {
		t.Errorf("the stranger reached the row: %+v", unchanged)
	}
}

// TestDeactivationKeepsTheRow is the last third of the acceptance criterion, and the rule this
// domain's doc.go states.
func TestDeactivationKeepsTheRow(t *testing.T) {
	pool := pgtest.DB(t)
	provider := newProvider(t, pool, "fleet-deact@example.com", "+61400000320")
	svc := newTestService()
	vehicle := added(t, pool, provider, "DEA111")

	var deactivated Vehicle
	err := inTx(t, pool, func(ctx context.Context, r db.Runner) error {
		var err error
		deactivated, err = svc.Deactivate(ctx, r, provider, vehicle.ID)
		return err
	})
	if err != nil {
		t.Fatalf("Deactivate() = %v", err)
	}

	if deactivated.Active() {
		t.Error("the vehicle is still in service")
	}
	if !deactivated.DeactivatedAt.Equal(testInstant) {
		t.Errorf("deactivated at %s, want the service's clock reading %s",
			deactivated.DeactivatedAt, testInstant)
	}

	// The row survives, which is the whole difference between deactivating and deleting: it is
	// named by the bid that won a job and by the delivery that followed.
	var count int
	if err := pool.QueryRow(t.Context(),
		`SELECT count(*) FROM vehicles WHERE id = $1`, vehicle.ID).Scan(&count); err != nil {
		t.Fatalf("counting: %v", err)
	}
	if count != 1 {
		t.Fatalf("the vehicle row is gone")
	}

	// Deactivating again is absorbed rather than refused: the caller asked for an outcome that
	// holds, which is what makes a retry with a fresh idempotency key safe.
	err = inTx(t, pool, func(ctx context.Context, r db.Runner) error {
		again, err := svc.Deactivate(ctx, r, provider, vehicle.ID)
		if err == nil && !again.DeactivatedAt.Equal(deactivated.DeactivatedAt) {
			t.Errorf("the second deactivation moved the timestamp to %s", again.DeactivatedAt)
		}
		return err
	})
	if err != nil {
		t.Fatalf("deactivating twice = %v", err)
	}
}

// TestAVehicleCanComeBackUnlessItsPlateIsTaken is the collision only reactivation can hit.
func TestAVehicleCanComeBackUnlessItsPlateIsTaken(t *testing.T) {
	pool := pgtest.DB(t)
	provider := newProvider(t, pool, "fleet-react@example.com", "+61400000321")
	svc := newTestService()

	original := added(t, pool, provider, "RET111")
	if err := inTx(t, pool, func(ctx context.Context, r db.Runner) error {
		_, err := svc.Deactivate(ctx, r, provider, original.ID)
		return err
	}); err != nil {
		t.Fatalf("Deactivate() = %v", err)
	}

	t.Run("it comes back while nothing holds the plate", func(t *testing.T) {
		var back Vehicle
		err := inTx(t, pool, func(ctx context.Context, r db.Runner) error {
			var err error
			back, err = svc.Reactivate(ctx, r, provider, original.ID)
			return err
		})
		if err != nil {
			t.Fatalf("Reactivate() = %v", err)
		}
		if !back.Active() {
			t.Error("the vehicle did not return to service")
		}
	})

	t.Run("and is refused when a replacement holds it", func(t *testing.T) {
		if err := inTx(t, pool, func(ctx context.Context, r db.Runner) error {
			_, err := svc.Deactivate(ctx, r, provider, original.ID)
			return err
		}); err != nil {
			t.Fatalf("Deactivate() = %v", err)
		}
		added(t, pool, provider, "RET111")

		err := inTx(t, pool, func(ctx context.Context, r db.Runner) error {
			_, err := svc.Reactivate(ctx, r, provider, original.ID)
			return err
		})
		if !errors.Is(err, ErrDuplicateRegistration) {
			t.Fatalf("Reactivate() = %v, want ErrDuplicateRegistration", err)
		}
	})
}

// TestADeactivatedVehicleCanStillBeEdited, and editing it does not bring it back.
//
// Refusing the edit would push a provider towards creating a second row for the same vehicle, which
// is the duplication deactivation exists to avoid.
func TestADeactivatedVehicleCanStillBeEdited(t *testing.T) {
	pool := pgtest.DB(t)
	provider := newProvider(t, pool, "fleet-editoff@example.com", "+61400000322")
	svc := newTestService()
	vehicle := added(t, pool, provider, "OFF111")

	if err := inTx(t, pool, func(ctx context.Context, r db.Runner) error {
		_, err := svc.Deactivate(ctx, r, provider, vehicle.ID)
		return err
	}); err != nil {
		t.Fatalf("Deactivate() = %v", err)
	}

	var edited Vehicle
	err := inTx(t, pool, func(ctx context.Context, r db.Runner) error {
		var err error
		edited, err = svc.Update(ctx, r, provider, vehicle.ID, VehicleFields{Model: ptr("NPR 45-155")})
		return err
	})
	if err != nil {
		t.Fatalf("Update() = %v", err)
	}
	if edited.Model != "NPR 45-155" {
		t.Errorf("model = %q, want the edit applied", edited.Model)
	}
	if edited.Active() {
		t.Error("an edit brought the vehicle back into service, which only Reactivate may do")
	}
}

// TestAWriteOutsideATransactionIsRefused, rather than silently losing its row lock.
//
// No trigger enforces this, unlike jobs' transition guard — which is exactly why it is checked. An
// edit outside a transaction releases the FOR UPDATE the instant the SELECT returns, and two
// concurrent edits then interleave with nothing to report afterwards.
func TestAWriteOutsideATransactionIsRefused(t *testing.T) {
	pool := pgtest.DB(t)
	provider := newProvider(t, pool, "fleet-notx@example.com", "+61400000323")
	svc := newTestService()
	vehicle := added(t, pool, provider, "NTX111")

	if _, err := svc.Update(t.Context(), pool, provider, vehicle.ID, VehicleFields{Make: ptr("Fuso")}); !errors.Is(err, ErrNotInTransaction) {
		t.Errorf("Update() = %v, want ErrNotInTransaction", err)
	}
	if _, err := svc.Deactivate(t.Context(), pool, provider, vehicle.ID); !errors.Is(err, ErrNotInTransaction) {
		t.Errorf("Deactivate() = %v, want ErrNotInTransaction", err)
	}
	if _, err := svc.Reactivate(t.Context(), pool, provider, vehicle.ID); !errors.Is(err, ErrNotInTransaction) {
		t.Errorf("Reactivate() = %v, want ErrNotInTransaction", err)
	}
}

// TestAFleetListIsTheProvidersOwn, newest first, filterable and paged.
func TestAFleetListIsTheProvidersOwn(t *testing.T) {
	pool := pgtest.DB(t)
	provider := newProvider(t, pool, "fleet-list@example.com", "+61400000324")
	other := newProvider(t, pool, "fleet-list-two@example.com", "+61400000325")
	svc := newTestService()

	first := added(t, pool, provider, "LST111")
	added(t, pool, provider, "LST222")
	third := added(t, pool, provider, "LST333")
	added(t, pool, other, "OTH111")

	if err := inTx(t, pool, func(ctx context.Context, r db.Runner) error {
		_, err := svc.Deactivate(ctx, r, provider, first.ID)
		return err
	}); err != nil {
		t.Fatalf("Deactivate() = %v", err)
	}

	t.Run("only their own, newest first", func(t *testing.T) {
		page, err := svc.Vehicles(t.Context(), pool, provider, VehicleQuery{})
		if err != nil {
			t.Fatalf("Vehicles() = %v", err)
		}
		if len(page.Vehicles) != 3 {
			t.Fatalf("%d vehicles, want the provider's three", len(page.Vehicles))
		}
		if page.Vehicles[0].ID != third.ID {
			t.Errorf("the list does not start at the newest")
		}
		for _, v := range page.Vehicles {
			if v.ProviderID != provider {
				t.Fatalf("the list carries %s's vehicle", v.ProviderID)
			}
		}
	})

	t.Run("the default includes retired vehicles", func(t *testing.T) {
		page, _ := svc.Vehicles(t.Context(), pool, provider, VehicleQuery{})
		for _, v := range page.Vehicles {
			if v.ID == first.ID {
				return
			}
		}
		t.Error("a retired vehicle is missing from the unfiltered list, so it cannot be brought back")
	})

	t.Run("active=true narrows it", func(t *testing.T) {
		page, err := svc.Vehicles(t.Context(), pool, provider, VehicleQuery{Active: ptr(true)})
		if err != nil {
			t.Fatalf("Vehicles() = %v", err)
		}
		if len(page.Vehicles) != 2 {
			t.Fatalf("%d in service, want 2", len(page.Vehicles))
		}
		for _, v := range page.Vehicles {
			if !v.Active() {
				t.Errorf("%s is out of service and in the active list", v.Registration)
			}
		}
	})

	t.Run("active=false narrows it the other way", func(t *testing.T) {
		page, err := svc.Vehicles(t.Context(), pool, provider, VehicleQuery{Active: ptr(false)})
		if err != nil {
			t.Fatalf("Vehicles() = %v", err)
		}
		if len(page.Vehicles) != 1 || page.Vehicles[0].ID != first.ID {
			t.Errorf("the retired list is %+v, want just the deactivated one", page.Vehicles)
		}
	})

	t.Run("paging reaches every vehicle exactly once", func(t *testing.T) {
		seen := map[uuid.UUID]int{}
		cursor := VehicleCursor{}

		for pages := 0; ; pages++ {
			if pages > 5 {
				t.Fatal("paging did not terminate; the cursor is not advancing")
			}
			page, err := svc.Vehicles(t.Context(), pool, provider,
				VehicleQuery{Limit: 1, After: cursor})
			if err != nil {
				t.Fatalf("Vehicles() = %v", err)
			}
			for _, v := range page.Vehicles {
				seen[v.ID]++
			}
			if !page.HasMore {
				break
			}
			cursor = page.Next
		}

		if len(seen) != 3 {
			t.Errorf("paged over %d vehicles, want 3", len(seen))
		}
		for id, times := range seen {
			if times != 1 {
				t.Errorf("%s appeared %d times", id, times)
			}
		}
	})
}

// --- SHIP-79: the provider's declaration -------------------------------------------------------

// declared runs one declaration inside a transaction, which is what Declare requires.
func declared(t *testing.T, pool *pgxpool.Pool, provider uuid.UUID, f ProfileFields) (Profile, error) {
	t.Helper()

	var profile Profile
	err := inTx(t, pool, func(ctx context.Context, r db.Runner) error {
		var err error
		profile, err = newTestService().Declare(ctx, r, provider, f)
		return err
	})
	return profile, err
}

// states, postcodes and specialties read as the wire does: the two lists of a service area, split.
func states(p Profile) []string    { return areasAt(p, ScopeState) }
func postcodes(p Profile) []string { return areasAt(p, ScopePostcode) }

func areasAt(p Profile, scope AreaScope) []string {
	var out []string
	for _, area := range p.Areas {
		if area.Scope == scope {
			out = append(out, area.Area)
		}
	}
	return out
}

func sameStrings(got, want []string) bool {
	if len(got) != len(want) {
		return false
	}
	for i := range got {
		if got[i] != want[i] {
			return false
		}
	}
	return true
}

// TestAProviderDeclaresAServiceAreaAndSpecialties is SHIP-79's acceptance criterion: declared,
// stored, and read back.
//
// Everything supplied here is in a form a person would type rather than the form that is stored,
// because normalising is what stops one region being recorded as two — the same argument the
// registration plate makes, applied to a set.
func TestAProviderDeclaresAServiceAreaAndSpecialties(t *testing.T) {
	pool := pgtest.DB(t)
	provider := newProvider(t, pool, "declare@example.com", "+61400000350")

	profile, err := declared(t, pool, provider, ProfileFields{
		States:      ptr([]string{"Victoria", " nsw ", "VIC"}),
		Postcodes:   ptr([]string{"3 000", "0800", "3000"}),
		Specialties: ptr([]Specialty{" Refrigerated ", SpecialtyGeneralFreight, "refrigerated"}),
	})
	if err != nil {
		t.Fatalf("Declare() = %v", err)
	}

	// States first and each group ascending, whatever order they were sent in.
	if got := states(profile); !sameStrings(got, []string{"NSW", "VIC"}) {
		t.Errorf("states = %v, want [NSW VIC] — spelled out, mixed case and duplicated on the way in", got)
	}
	if got := postcodes(profile); !sameStrings(got, []string{"0800", "3000"}) {
		t.Errorf("postcodes = %v, want [0800 3000] — the space removed and the repeat collapsed", got)
	}

	// Specialties come back in the platform's own order rather than the order they were sent, so
	// two providers holding the same specialties present them identically.
	if len(profile.Specialties) != 2 ||
		profile.Specialties[0] != SpecialtyGeneralFreight ||
		profile.Specialties[1] != SpecialtyRefrigerated {
		t.Errorf("specialties = %v, want [general_freight refrigerated]", profile.Specialties)
	}

	// The rows, not the answer the service gave about itself.
	var rows int
	if err := pool.QueryRow(t.Context(),
		`SELECT count(*) FROM provider_service_areas WHERE provider_id = $1`, provider).Scan(&rows); err != nil {
		t.Fatalf("counting the stored areas: %v", err)
	}
	if rows != 4 {
		t.Errorf("%d stored areas, want 4 — two states and two postcodes", rows)
	}
}

// TestAnUndeclaredProfileIsEmptyRatherThanMissing.
//
// There is no parent row, so "I have not nominated a service area yet" has the same shape as any
// other answer. A client opening the screen for the first time gets empty lists rather than an
// error it has to read as "not yet".
func TestAnUndeclaredProfileIsEmptyRatherThanMissing(t *testing.T) {
	pool := pgtest.DB(t)
	provider := newProvider(t, pool, "declare-empty@example.com", "+61400000351")

	profile, err := newTestService().Profile(t.Context(), pool, provider)
	if err != nil {
		t.Fatalf("Profile() = %v", err)
	}
	if len(profile.Areas) != 0 || len(profile.Specialties) != 0 {
		t.Errorf("an undeclared profile is %+v, want empty", profile)
	}
	if profile.ProviderID != provider {
		t.Errorf("provider = %s, want %s", profile.ProviderID, provider)
	}

	// And it matches nothing. Eligibility is opt-in: the alternative would make the provider who
	// has not finished onboarding the widest-reaching provider on the platform.
	if profile.Serves("NSW", "2000") {
		t.Error("an undeclared provider serves New South Wales; an empty declaration must match nothing")
	}
}

// TestAnUnnamedListIsLeftAloneAndAnEmptyOneClearsIt is the three-state rule, which is the whole of
// how a partial declaration works.
func TestAnUnnamedListIsLeftAloneAndAnEmptyOneClearsIt(t *testing.T) {
	pool := pgtest.DB(t)
	provider := newProvider(t, pool, "declare-partial@example.com", "+61400000352")

	if _, err := declared(t, pool, provider, ProfileFields{
		States:      ptr([]string{"NSW"}),
		Postcodes:   ptr([]string{"3000"}),
		Specialties: ptr([]Specialty{SpecialtyLivestock}),
	}); err != nil {
		t.Fatalf("the first declaration: %v", err)
	}

	// Naming only the specialties leaves both halves of the service area exactly as they were.
	profile, err := declared(t, pool, provider, ProfileFields{
		Specialties: ptr([]Specialty{SpecialtyCourier}),
	})
	if err != nil {
		t.Fatalf("the second declaration: %v", err)
	}
	if !sameStrings(states(profile), []string{"NSW"}) || !sameStrings(postcodes(profile), []string{"3000"}) {
		t.Errorf("naming only the specialties changed the service area: %+v", profile.Areas)
	}
	if len(profile.Specialties) != 1 || profile.Specialties[0] != SpecialtyCourier {
		t.Errorf("specialties = %v, want [courier] — a named list is replaced, not merged", profile.Specialties)
	}

	// The empty list is the other half, and without it a provider who withdrew from every single
	// postcode could not say so.
	profile, err = declared(t, pool, provider, ProfileFields{Postcodes: ptr([]string{})})
	if err != nil {
		t.Fatalf("clearing the postcodes: %v", err)
	}
	if len(postcodes(profile)) != 0 {
		t.Errorf("postcodes = %v, want none — an empty list clears", postcodes(profile))
	}
	if !sameStrings(states(profile), []string{"NSW"}) {
		t.Errorf("clearing the postcodes disturbed the states: %v", states(profile))
	}
}

// TestARedeclaredEntryKeepsTheDateItWasFirstDeclared.
//
// The store amends the set rather than rewriting it, so an entry that survives a change is the same
// entry. It matters because created_at is the only record of when a provider took on a region, and
// Docs/04 §3 makes the service area something a verification decision is made against.
func TestARedeclaredEntryKeepsTheDateItWasFirstDeclared(t *testing.T) {
	pool := pgtest.DB(t)
	provider := newProvider(t, pool, "declare-stable@example.com", "+61400000353")

	if _, err := declared(t, pool, provider, ProfileFields{States: ptr([]string{"NSW"})}); err != nil {
		t.Fatalf("the first declaration: %v", err)
	}

	first := declaredAt(t, pool, provider, "NSW")

	if _, err := declared(t, pool, provider, ProfileFields{States: ptr([]string{"NSW", "VIC"})}); err != nil {
		t.Fatalf("the second declaration: %v", err)
	}

	if again := declaredAt(t, pool, provider, "NSW"); !again.Equal(first) {
		t.Errorf("NSW was rewritten: first declared %s, now %s", first, again)
	}
}

func declaredAt(t *testing.T, pool *pgxpool.Pool, provider uuid.UUID, area string) time.Time {
	t.Helper()

	var at time.Time
	err := pool.QueryRow(t.Context(),
		`SELECT created_at FROM provider_service_areas WHERE provider_id = $1 AND area = $2`,
		provider, area).Scan(&at)
	if err != nil {
		t.Fatalf("reading created_at for %s: %v", area, err)
	}
	return at
}

// TestOnlyAProviderDeclaresAServiceArea, read from users.role rather than from a token's claim.
//
// The read is deliberately not refused: a customer's declaration is empty because they have never
// made one, and answering 403 to a read that discloses nothing would make the client special-case a
// screen it never shows.
func TestOnlyAProviderDeclaresAServiceArea(t *testing.T) {
	pool := pgtest.DB(t)
	customer := newAccount(t, pool, "declare-customer@example.com", "+61400000354", "customer")

	_, err := declared(t, pool, customer, ProfileFields{States: ptr([]string{"NSW"})})
	if !errors.Is(err, ErrNotProvider) {
		t.Errorf("Declare() by a customer = %v, want ErrNotProvider", err)
	}

	profile, err := newTestService().Profile(t.Context(), pool, customer)
	if err != nil {
		t.Errorf("Profile() for a customer = %v, want an empty profile", err)
	}
	if len(profile.Areas) != 0 {
		t.Errorf("a customer has a service area: %+v", profile.Areas)
	}
}

// TestADeclarationThatNamesNoListIsRefused, for the reason an edit to a vehicle that changes
// nothing is: answering with the unchanged declaration would tell a client its edit was applied.
func TestADeclarationThatNamesNoListIsRefused(t *testing.T) {
	pool := pgtest.DB(t)
	provider := newProvider(t, pool, "declare-nothing@example.com", "+61400000355")

	if _, err := declared(t, pool, provider, ProfileFields{}); !errors.Is(err, ErrNothingToUpdate) {
		t.Errorf("Declare() with no list = %v, want ErrNothingToUpdate", err)
	}
}

// TestADeclarationOutsideATransactionIsRefused.
//
// Replacing a set is a delete and an insert that must be one decision, and lockProfile's advisory
// lock is released the moment the statement returns when there is no transaction to hold it.
func TestADeclarationOutsideATransactionIsRefused(t *testing.T) {
	pool := pgtest.DB(t)
	provider := newProvider(t, pool, "declare-notx@example.com", "+61400000356")

	_, err := newTestService().Declare(t.Context(), pool, provider,
		ProfileFields{States: ptr([]string{"NSW"})})
	if !errors.Is(err, ErrNotInTransaction) {
		t.Errorf("Declare() on the pool = %v, want ErrNotInTransaction", err)
	}
}

// TestEveryBadEntryIsReportedByPosition is Docs/10 §4.6 applied to a set.
//
// The position is what makes the answer usable: the client renders each entry as its own control,
// and an error naming only the list leaves the provider to work out which of forty postcodes is the
// one it means.
func TestEveryBadEntryIsReportedByPosition(t *testing.T) {
	pool := pgtest.DB(t)
	provider := newProvider(t, pool, "declare-invalid@example.com", "+61400000357")

	_, err := declared(t, pool, provider, ProfileFields{
		States:      ptr([]string{"NSW", "Zealand"}),
		Postcodes:   ptr([]string{"3000", "3A00", "800"}),
		Specialties: ptr([]Specialty{SpecialtyOversized, "hovercraft"}),
	})

	var apiErr *httpx.Error
	if !errors.As(err, &apiErr) {
		t.Fatalf("Declare() = %v, want a validation failure", err)
	}
	if apiErr.Status != http.StatusUnprocessableEntity {
		t.Errorf("status = %d, want 422", apiErr.Status)
	}

	named := map[string]bool{}
	for _, detail := range apiErr.Details {
		named[detail.Field] = true
	}
	for _, want := range []string{
		"service_area.states.1",
		"service_area.postcodes.1",
		"service_area.postcodes.2",
		"specialties.1",
	} {
		if !named[want] {
			t.Errorf("no detail names %s; got %v", want, named)
		}
	}
	if named["service_area.states.0"] || named["specialties.0"] {
		t.Errorf("a valid entry was reported: %v", named)
	}

	// Nothing was written. A declaration is replaced whole, so a request that fails validation
	// must leave the previous one exactly as it was — including when there was none.
	profile, err := newTestService().Profile(t.Context(), pool, provider)
	if err != nil {
		t.Fatalf("Profile() = %v", err)
	}
	if len(profile.Areas) != 0 || len(profile.Specialties) != 0 {
		t.Errorf("a refused declaration reached the database: %+v", profile)
	}
}

// TestALongerListThanThereAreStatesIsOneErrorRatherThanThousands.
//
// The count is checked before the entries are, so a client with a runaway list gets one message it
// can act on instead of a response body of field errors.
func TestALongerListThanThereAreStatesIsOneErrorRatherThanThousands(t *testing.T) {
	pool := pgtest.DB(t)
	provider := newProvider(t, pool, "declare-toomany@example.com", "+61400000358")

	tooMany := make([]string, 0, len(ServiceAreaStates)+1)
	for i := range len(ServiceAreaStates) + 1 {
		tooMany = append(tooMany, string(rune('a'+i)))
	}

	_, err := declared(t, pool, provider, ProfileFields{States: ptr(tooMany)})

	var apiErr *httpx.Error
	if !errors.As(err, &apiErr) {
		t.Fatalf("Declare() = %v, want a validation failure", err)
	}
	if len(apiErr.Details) != 1 || apiErr.Details[0].Field != "service_area.states" {
		t.Errorf("details = %+v, want one error naming the list itself", apiErr.Details)
	}
}

// TestServiceAreaMembershipIsASetQuery is the "queryable" half of SHIP-79's Done when, and it is
// the shape SHIP-81 will filter with.
//
// It is a test rather than an exported query method because SHIP-81 owns the eligibility filter and
// this ticket adds none. What it demonstrates is that the decision holds: a job's state and postcode
// answer the question with set membership and no distance arithmetic anywhere.
func TestServiceAreaMembershipIsASetQuery(t *testing.T) {
	pool := pgtest.DB(t)
	provider := newProvider(t, pool, "declare-serves@example.com", "+61400000359")

	profile, err := declared(t, pool, provider, ProfileFields{
		States:    ptr([]string{"TAS"}),
		Postcodes: ptr([]string{"3000"}),
	})
	if err != nil {
		t.Fatalf("Declare() = %v", err)
	}

	for _, c := range []struct {
		state, postcode string
		want            bool
		why             string
	}{
		{"TAS", "7000", true, "a whole state covers a postcode inside it"},
		{"VIC", "3000", true, "a single postcode is covered without the state being"},
		{"VIC", "3121", false, "a postcode in a state the provider does not serve"},
		{"NSW", "2000", false, "neither grain matches"},
	} {
		if got := profile.Serves(c.state, c.postcode); got != c.want {
			t.Errorf("Serves(%q, %q) = %v, want %v — %s", c.state, c.postcode, got, c.want, c.why)
		}
	}

	// The same question asked of the database, which is where SHIP-81 will ask it: one index
	// lookup per provider, and no coordinate involved.
	var serves bool
	err = pool.QueryRow(t.Context(), `
		SELECT EXISTS (
			SELECT 1 FROM provider_service_areas
			WHERE provider_id = $1
			  AND ((scope = 'state' AND area = $2) OR (scope = 'postcode' AND area = $3))
		)`, provider, "VIC", "3000").Scan(&serves)
	if err != nil {
		t.Fatalf("the membership query: %v", err)
	}
	if !serves {
		t.Error("the membership query does not find a postcode the provider declared")
	}
}

// TestTwoDevicesDeclaringAtOnceDoNotProduceTheUnion is what the advisory lock exists for.
//
// In READ COMMITTED and without it, each transaction's DELETE cannot see the other's uncommitted
// INSERT, so both survive and the declaration becomes a set neither client asked for — a lost
// update with nothing to report afterwards. This fails with [postgresStore.lockProfile] removed.
func TestTwoDevicesDeclaringAtOnceDoNotProduceTheUnion(t *testing.T) {
	pool := pgtest.DB(t)
	provider := newProvider(t, pool, "declare-race@example.com", "+61400000360")

	first, err := pool.Begin(t.Context())
	if err != nil {
		t.Fatalf("opening the first transaction: %v", err)
	}
	defer first.Rollback(t.Context())

	if _, err := newTestService().Declare(t.Context(), first, provider,
		ProfileFields{States: ptr([]string{"NSW"})}); err != nil {
		t.Fatalf("the first declaration: %v", err)
	}

	second := make(chan error, 1)
	started := make(chan struct{})
	go func() {
		close(started)
		second <- func() error {
			return db.InTx(context.Background(), pool, func(ctx context.Context, r db.Runner) error {
				_, err := newTestService().Declare(ctx, r, provider,
					ProfileFields{States: ptr([]string{"VIC"})})
				return err
			})
		}()
	}()

	// Long enough for the second transaction to reach the lock and block on it. If it has not,
	// this test passes for the wrong reason rather than failing for a wrong one — which is the
	// safe direction for a timing-dependent check to be wrong in.
	<-started
	time.Sleep(200 * time.Millisecond)

	if err := first.Commit(t.Context()); err != nil {
		t.Fatalf("committing the first declaration: %v", err)
	}
	if err := <-second; err != nil {
		t.Fatalf("the second declaration: %v", err)
	}

	profile, err := newTestService().Profile(t.Context(), pool, provider)
	if err != nil {
		t.Fatalf("Profile() = %v", err)
	}
	if !sameStrings(states(profile), []string{"VIC"}) {
		t.Errorf("states = %v, want [VIC] — the later declaration replaces the earlier, and the "+
			"union of the two is what an unlocked read-modify-write produces", states(profile))
	}
}
