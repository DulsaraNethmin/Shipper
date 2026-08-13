package migrations_test

import (
	"regexp"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/DulsaraNethmin/Shipper/services/core/internal/fleet"
	"github.com/DulsaraNethmin/Shipper/services/core/internal/testsupport/pgtest"
)

// SHIP-79's two tables, checked where their guarantees live.
//
// Every assertion here is about a constraint or an index, so all of them run against a real
// PostgreSQL (Docs/06 §4.1). The one that matters most is the scope-and-area check: it is what makes
// "an entry names one grain and never two" structural, and an application-level version of it would
// pass every test in this file while a support query typed at a psql prompt wrote the row it exists
// to refuse.

// arrayLiterals pulls the members out of the single `ARRAY[...]` in a constraint definition.
//
// quotedLiteral (jobs_test.go) on its own is not enough for ck_provider_service_areas_scope_and_area,
// because that constraint also carries 'state', 'postcode' and a postcode pattern — comparing all of
// its literals against fleet.ServiceAreaStates would report three failures that are not defects.
var arrayLiterals = regexp.MustCompile(`ARRAY\[([^\]]*)\]`)

func constraintMembers(t *testing.T, pool *pgxpool.Pool, name string) map[string]bool {
	t.Helper()

	var definition string
	err := pool.QueryRow(t.Context(),
		`SELECT pg_get_constraintdef(oid) FROM pg_constraint WHERE conname = $1`, name).Scan(&definition)
	if err != nil {
		t.Fatalf("reading %s: %v", name, err)
	}

	array := arrayLiterals.FindStringSubmatch(definition)
	if array == nil {
		t.Fatalf("%s has no ARRAY[...] to read a list out of: %s", name, definition)
	}

	members := map[string]bool{}
	for _, match := range quotedLiteral.FindAllStringSubmatch(array[1], -1) {
		members[match[1]] = true
	}
	return members
}

// newProviderAccount is a provider, which is the only account these tables reference.
func newProviderAccount(t *testing.T, pool *pgxpool.Pool, email, phone string) uuid.UUID {
	t.Helper()
	return newUser(t, pool, email, phone, "provider")
}

func declareArea(t *testing.T, pool *pgxpool.Pool, provider uuid.UUID, scope, area string) error {
	t.Helper()

	id, err := uuid.NewV7()
	if err != nil {
		t.Fatalf("generating an id: %v", err)
	}
	_, err = pool.Exec(t.Context(),
		`INSERT INTO provider_service_areas (id, provider_id, scope, area) VALUES ($1, $2, $3, $4)`,
		id, provider, scope, area)
	return err
}

// TestSpecialtyConstraintMatchesTheGoConstants is Docs/10 §3.4's pairing, in both directions.
//
// One direction alone is not enough: a specialty the database accepts and Go does not know is a row
// nothing can render, and one Go has and the database refuses is a form that fails on submission.
func TestSpecialtyConstraintMatchesTheGoConstants(t *testing.T) {
	pool := pgtest.DB(t)

	inGo := map[string]bool{}
	for _, specialty := range fleet.Specialties {
		inGo[string(specialty)] = true
	}
	if len(inGo) != len(fleet.Specialties) {
		t.Errorf("fleet.Specialties contains a duplicate: %d constants, %d distinct values",
			len(fleet.Specialties), len(inGo))
	}

	inSQL := constraintMembers(t, pool, "ck_provider_specialties_specialty")

	for specialty := range inGo {
		if !inSQL[specialty] {
			t.Errorf("%q is a fleet.Specialty and ck_provider_specialties_specialty refuses it", specialty)
		}
	}
	for specialty := range inSQL {
		if !inGo[specialty] {
			t.Errorf("ck_provider_specialties_specialty accepts %q and no Go constant names it", specialty)
		}
	}
}

// TestServiceAreaStatesMatchTheGoConstants is the same pairing for the eight states.
//
// fleet.ServiceAreaStates is deliberately a second copy of a list `jobs` also holds — the boundary
// rule forbids the import, and eight strings are cheaper duplicated than registered as shared
// infrastructure. This test is what keeps *this* copy honest; jobs' own test keeps the other.
func TestServiceAreaStatesMatchTheGoConstants(t *testing.T) {
	pool := pgtest.DB(t)

	inGo := map[string]bool{}
	for _, state := range fleet.ServiceAreaStates {
		inGo[state] = true
	}

	inSQL := constraintMembers(t, pool, "ck_provider_service_areas_scope_and_area")

	for state := range inGo {
		if !inSQL[state] {
			t.Errorf("%q is in fleet.ServiceAreaStates and the constraint refuses it", state)
		}
	}
	for state := range inSQL {
		if !inGo[state] {
			t.Errorf("the constraint accepts %q as a state and no Go constant names it", state)
		}
	}
}

// TestAServiceAreaNamesOneGrain is the decision SHIP-79 took, held by the database rather than by
// the writer remembering it.
//
// An entry is a whole state or one postcode and never a postcode qualified by a state, because
// Docs/11 §3 records SHIP-60's deliberate refusal to validate a postcode against its state — the
// allocations have exceptions and move. A row that carried both could disagree with itself.
func TestAServiceAreaNamesOneGrain(t *testing.T) {
	pool := pgtest.DB(t)
	provider := newProviderAccount(t, pool, "area-grain@example.com", "+61400000390")

	t.Run("a state at the state scope is accepted", func(t *testing.T) {
		if err := declareArea(t, pool, provider, "state", "VIC"); err != nil {
			t.Fatalf("declaring VIC: %v", err)
		}
	})

	t.Run("a postcode at the postcode scope is accepted", func(t *testing.T) {
		if err := declareArea(t, pool, provider, "postcode", "0800"); err != nil {
			// 0800 rather than 3000 on purpose: a postcode is text, and an integer column
			// would have stored Darwin as 800.
			t.Fatalf("declaring 0800: %v", err)
		}
	})

	t.Run("a postcode at the state scope is refused", func(t *testing.T) {
		err := declareArea(t, pool, provider, "state", "3000")
		if err == nil {
			t.Fatal("a postcode was accepted as a whole state")
		}
		if !strings.Contains(err.Error(), "ck_provider_service_areas_scope_and_area") {
			t.Errorf("expected the scope-and-area check to refuse it, got: %v", err)
		}
	})

	t.Run("a state at the postcode scope is refused", func(t *testing.T) {
		if err := declareArea(t, pool, provider, "postcode", "NSW"); err == nil {
			t.Fatal("a state was accepted as a postcode")
		}
	})

	t.Run("a three-digit postcode is refused", func(t *testing.T) {
		if err := declareArea(t, pool, provider, "postcode", "800"); err == nil {
			t.Fatal("800 was accepted; a postcode is four digits and Darwin's is 0800")
		}
	})

	t.Run("a scope nobody has defined is refused", func(t *testing.T) {
		if err := declareArea(t, pool, provider, "suburb", "Fitzroy"); err == nil {
			t.Fatal("a suburb was accepted; there are seven Springfields and no reference data")
		}
	})
}

// TestAServiceAreaIsASet proves the unique index is real.
//
// Deduplication inside one request is the domain's, and this is the half it cannot do: two requests
// racing both find nothing and both write, so only the index can refuse the second.
func TestAServiceAreaIsASet(t *testing.T) {
	pool := pgtest.DB(t)
	provider := newProviderAccount(t, pool, "area-set@example.com", "+61400000391")

	if err := declareArea(t, pool, provider, "state", "NSW"); err != nil {
		t.Fatalf("declaring NSW: %v", err)
	}

	err := declareArea(t, pool, provider, "state", "NSW")
	if err == nil {
		t.Fatal("New South Wales was declared twice by one provider")
	}
	if !strings.Contains(err.Error(), "uq_provider_service_areas") {
		t.Errorf("expected uq_provider_service_areas to refuse it, got: %v", err)
	}

	// The rule is per provider. Two providers serving one state is the ordinary case, and a
	// global unique index would be a marketplace with one provider per state in it.
	other := newProviderAccount(t, pool, "area-set-other@example.com", "+61400000392")
	if err := declareArea(t, pool, other, "state", "NSW"); err != nil {
		t.Fatalf("a second provider could not declare NSW: %v", err)
	}
}

// TestASpecialtyIsDeclaredOnce is the same rule on the other table.
func TestASpecialtyIsDeclaredOnce(t *testing.T) {
	pool := pgtest.DB(t)
	provider := newProviderAccount(t, pool, "specialty-set@example.com", "+61400000393")

	declare := func(specialty string) error {
		id, err := uuid.NewV7()
		if err != nil {
			t.Fatalf("generating an id: %v", err)
		}
		_, err = pool.Exec(t.Context(),
			`INSERT INTO provider_specialties (id, provider_id, specialty) VALUES ($1, $2, $3)`,
			id, provider, specialty)
		return err
	}

	if err := declare("refrigerated"); err != nil {
		t.Fatalf("declaring refrigerated: %v", err)
	}
	if err := declare("refrigerated"); err == nil {
		t.Fatal("one provider declared refrigerated twice")
	}
	if err := declare("hovercraft"); err == nil {
		t.Fatal("'hovercraft' was accepted as a specialty")
	}
}

// TestADeclarationNamesAnAccountThatExists is the foreign key, and it is ON DELETE RESTRICT.
//
// Docs/10 §3.3: nothing should be deleting a user row at all, because SHIP-171 pseudonymises rather
// than deletes. A cascade here would silently discard the declaration a verification decision was
// made against (Docs/04 §3).
func TestADeclarationNamesAnAccountThatExists(t *testing.T) {
	pool := pgtest.DB(t)

	stranger := uuid.Must(uuid.NewV7())
	if err := declareArea(t, pool, stranger, "state", "QLD"); err == nil {
		t.Fatal("a service area was declared for an account that does not exist")
	}

	provider := newProviderAccount(t, pool, "area-fk@example.com", "+61400000394")
	if err := declareArea(t, pool, provider, "state", "QLD"); err != nil {
		t.Fatalf("declaring QLD: %v", err)
	}
	if _, err := pool.Exec(t.Context(), `DELETE FROM users WHERE id = $1`, provider); err == nil {
		t.Fatal("the account was deleted out from under its declaration")
	}
}
