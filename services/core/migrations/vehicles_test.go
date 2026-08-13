package migrations_test

import (
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/DulsaraNethmin/Shipper/services/core/internal/fleet"
	"github.com/DulsaraNethmin/Shipper/services/core/internal/testsupport/pgtest"
)

// SHIP-78's table, checked where its guarantees live.
//
// Every assertion here is about a constraint or an index, so all of them run against a real
// PostgreSQL — Docs/06 §4.1: "a mock happily accepts a write that the actual constraint would
// reject." The partial unique index below is the clearest case in the repository so far: an
// application-level check would pass every one of these tests and still lose the race it exists to
// win.

// newVehicle inserts one vehicle, in service, and returns its id.
func newVehicle(t *testing.T, pool *pgxpool.Pool, provider uuid.UUID, registration string) uuid.UUID {
	t.Helper()

	id, err := uuid.NewV7()
	if err != nil {
		t.Fatalf("generating an id: %v", err)
	}
	if _, err := pool.Exec(t.Context(),
		`INSERT INTO vehicles (id, provider_id, registration, vehicle_type) VALUES ($1, $2, $3, 'van')`,
		id, provider, registration); err != nil {
		t.Fatalf("inserting %s for %s: %v", registration, provider, err)
	}
	return id
}

// TestVehicleTypeConstraintMatchesTheGoConstants is Docs/10 §3.4's pairing, in both directions.
//
// One direction alone is not enough: a type the database accepts and Go does not know is a row
// nothing can render, and a type Go has and the database refuses is a form that fails on
// submission.
func TestVehicleTypeConstraintMatchesTheGoConstants(t *testing.T) {
	pool := pgtest.DB(t)

	inGo := map[string]bool{}
	for _, kind := range fleet.VehicleTypes {
		inGo[string(kind)] = true
	}
	if len(inGo) != len(fleet.VehicleTypes) {
		t.Errorf("fleet.VehicleTypes contains a duplicate: %d constants, %d distinct values",
			len(fleet.VehicleTypes), len(inGo))
	}

	var definition string
	err := pool.QueryRow(t.Context(),
		`SELECT pg_get_constraintdef(oid) FROM pg_constraint WHERE conname = $1`,
		"ck_vehicles_type").Scan(&definition)
	if err != nil {
		t.Fatalf("reading ck_vehicles_type: %v", err)
	}

	inSQL := map[string]bool{}
	for _, match := range quotedLiteral.FindAllStringSubmatch(definition, -1) {
		inSQL[match[1]] = true
	}

	for kind := range inGo {
		if !inSQL[kind] {
			t.Errorf("%q is a fleet.VehicleType and ck_vehicles_type refuses it", kind)
		}
	}
	for kind := range inSQL {
		if !inGo[kind] {
			t.Errorf("ck_vehicles_type accepts %q and no Go constant names it", kind)
		}
	}
}

// TestAnUnknownVehicleTypeIsRefused proves the CHECK is real rather than decorative.
func TestAnUnknownVehicleTypeIsRefused(t *testing.T) {
	pool := pgtest.DB(t)
	provider := newUser(t, pool, "fleet-type@example.com", "+61400000300", "provider")

	id, _ := uuid.NewV7()
	_, err := pool.Exec(t.Context(),
		`INSERT INTO vehicles (id, provider_id, registration, vehicle_type) VALUES ($1, $2, 'AAA111', $3)`,
		id, provider, "hovercraft")
	if err == nil {
		t.Fatal("'hovercraft' was accepted as a vehicle type")
	}
	if !strings.Contains(err.Error(), "ck_vehicles_type") {
		t.Errorf("expected ck_vehicles_type to refuse it, got: %v", err)
	}
}

// TestOneLiveVehiclePerPlate is the partial unique index, and the reason it is partial.
//
// The three cases are the whole design:
//
//  1. a second *live* row on one plate is refused, because that is one truck recorded twice;
//  2. a plate that has been retired can be used again, because a truck sold and a replacement
//     given the same personalised plate is ordinary;
//  3. two providers may both hold a plate, because that is a real-world dispute for verification
//     to settle (Docs/04) rather than something a constraint should answer by refusing whichever
//     of the two typed second.
func TestOneLiveVehiclePerPlate(t *testing.T) {
	pool := pgtest.DB(t)
	provider := newUser(t, pool, "fleet-dup@example.com", "+61400000301", "provider")
	other := newUser(t, pool, "fleet-dup-two@example.com", "+61400000302", "provider")

	first := newVehicle(t, pool, provider, "DUP111")

	t.Run("a second live row on the same plate is refused", func(t *testing.T) {
		id, _ := uuid.NewV7()
		_, err := pool.Exec(t.Context(),
			`INSERT INTO vehicles (id, provider_id, registration, vehicle_type) VALUES ($1, $2, 'DUP111', 'ute')`,
			id, provider)
		if err == nil {
			t.Fatal("one truck was recorded twice in one fleet")
		}
		if !strings.Contains(err.Error(), "uq_vehicles_provider_registration") {
			t.Errorf("expected the partial unique index to refuse it, got: %v", err)
		}
	})

	t.Run("another provider may hold the same plate", func(t *testing.T) {
		// Not a defect: a sold vehicle whose previous owner never deactivated it produces
		// exactly this, and adjudicating it is verification's job rather than a constraint's.
		newVehicle(t, pool, other, "DUP111")
	})

	t.Run("a retired plate may be used again", func(t *testing.T) {
		if _, err := pool.Exec(t.Context(),
			`UPDATE vehicles SET deactivated_at = now() WHERE id = $1`, first); err != nil {
			t.Fatalf("deactivating: %v", err)
		}
		newVehicle(t, pool, provider, "DUP111")

		// And the retired one cannot come back while the replacement holds the plate, which is
		// the collision fleet.Reactivate reports as fleet_duplicate_registration.
		_, err := pool.Exec(t.Context(),
			`UPDATE vehicles SET deactivated_at = NULL WHERE id = $1`, first)
		if err == nil {
			t.Fatal("both rows were in service on one plate at once")
		}
		if !strings.Contains(err.Error(), "uq_vehicles_provider_registration") {
			t.Errorf("expected the partial unique index to refuse it, got: %v", err)
		}
	})
}

// TestVehicleCapacityMustBePositive covers the four coherence constraints together.
//
// They are constraints rather than validation because a zero-length load space and a negative
// capacity are not values an operator would ever want to permit. The *upper* bounds are the
// validator's, because "the largest load this marketplace carries" is a number operations changes
// (Docs/06 §5.3) and a CHECK constraint is a migration.
func TestVehicleCapacityMustBePositive(t *testing.T) {
	pool := pgtest.DB(t)
	provider := newUser(t, pool, "fleet-capacity@example.com", "+61400000303", "provider")

	cases := map[string]struct {
		column     string
		value      any
		constraint string
	}{
		"a weightless vehicle":     {"max_weight_kg", 0, "ck_vehicles_max_weight_kg"},
		"a negative load length":   {"load_length_cm", -1, "ck_vehicles_load_length_cm"},
		"a load space of no width": {"load_width_cm", 0, "ck_vehicles_load_width_cm"},
		"a negative load height":   {"load_height_cm", -400, "ck_vehicles_load_height_cm"},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			id, _ := uuid.NewV7()
			_, err := pool.Exec(t.Context(),
				`INSERT INTO vehicles (id, provider_id, registration, vehicle_type, `+tc.column+`)
				 VALUES ($1, $2, $3, 'van', $4)`,
				// One plate for every case, which is safe because none of them inserts.
				id, provider, "CAP111", tc.value)
			if err == nil {
				t.Fatalf("%s = %v was accepted", tc.column, tc.value)
			}
			if !strings.Contains(err.Error(), tc.constraint) {
				t.Errorf("expected %s to refuse it, got: %v", tc.constraint, err)
			}
		})
	}
}

// TestAVehicleNeedsARegistration is the one field a row cannot exist without.
func TestAVehicleNeedsARegistration(t *testing.T) {
	pool := pgtest.DB(t)
	provider := newUser(t, pool, "fleet-blank@example.com", "+61400000304", "provider")

	id, _ := uuid.NewV7()
	_, err := pool.Exec(t.Context(),
		`INSERT INTO vehicles (id, provider_id, registration, vehicle_type) VALUES ($1, $2, '   ', 'van')`,
		id, provider)
	if err == nil {
		t.Fatal("a vehicle was created with a blank registration, identifying nothing")
	}
	if !strings.Contains(err.Error(), "ck_vehicles_registration") {
		t.Errorf("expected ck_vehicles_registration to refuse it, got: %v", err)
	}
}

// TestAProviderWithVehiclesCannotBeDeleted is Docs/10 §3.3's ON DELETE RESTRICT.
//
// SHIP-171 pseudonymises an account rather than removing it, so nothing should be deleting a user
// row at all — and a cascade here would destroy the commercial record Docs/05 §3.1 requires
// retaining.
func TestAProviderWithVehiclesCannotBeDeleted(t *testing.T) {
	pool := pgtest.DB(t)
	provider := newUser(t, pool, "fleet-restrict@example.com", "+61400000305", "provider")
	newVehicle(t, pool, provider, "RES111")

	if _, err := pool.Exec(t.Context(), `DELETE FROM users WHERE id = $1`, provider); err == nil {
		t.Fatal("a provider with a fleet was deleted, taking the vehicles with them")
	}
}
