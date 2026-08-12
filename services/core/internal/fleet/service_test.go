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
