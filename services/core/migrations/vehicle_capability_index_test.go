package migrations_test

import (
	"strings"
	"testing"

	"github.com/DulsaraNethmin/Shipper/services/core/internal/testsupport/pgtest"
)

// SHIP-81's index, checked for the two properties its comment claims.
//
// An index is not a correctness object and this file does not pretend otherwise — nothing in
// internal/fleet gives a different *answer* if it is missing. What it does claim is a shape, and
// the shape is the whole reason it exists beside idx_vehicles_provider rather than instead of it:
// **partial**, so a fleet of retired vehicles costs nothing to skip, and **covering**, so the
// capability check that runs once per candidate job never reaches the heap.
//
// Both are easy to lose in a later edit — dropping INCLUDE, or moving the capacity columns into
// the key — and neither loss produces a failing test anywhere else. This is what notices.
func TestTheCapabilityIndexIsPartialAndCovering(t *testing.T) {
	pool := pgtest.DB(t)

	var definition string
	if err := pool.QueryRow(t.Context(),
		`SELECT indexdef FROM pg_indexes WHERE indexname = 'idx_vehicles_capability'`,
	).Scan(&definition); err != nil {
		t.Fatalf("reading idx_vehicles_capability: %v — SHIP-81's capability filter runs once "+
			"per candidate job and this is the index it reads (000302)", err)
	}

	// Partial. Without the predicate the index carries every vehicle a provider has ever owned,
	// and the deactivated ones are exactly the rows the filter must not consider.
	if !strings.Contains(definition, "WHERE (deactivated_at IS NULL)") {
		t.Errorf("the index is not partial on vehicles in service:\n%s\n"+
			"A deactivated vehicle stops making its provider eligible for new work (000300).",
			definition)
	}

	// Covering. These four are the columns the filter compares against a job's dimensions, and
	// having them in the leaf is what makes the check an index-only scan.
	for _, column := range []string{
		"max_weight_kg", "load_length_cm", "load_width_cm", "load_height_cm",
	} {
		if !strings.Contains(includedColumns(definition), column) {
			t.Errorf("%s is not carried by idx_vehicles_capability:\n%s\n"+
				"Without it the capability check reads the heap for every vehicle the provider "+
				"owns, which is the cost 000302 exists to remove.", column, definition)
		}
	}
}

// includedColumns is the INCLUDE list of an index definition, or the empty string when it has
// none — so a plain index fails the check above rather than passing it by accident because the
// column name also appears in the key.
func includedColumns(definition string) string {
	_, included, found := strings.Cut(definition, "INCLUDE (")
	if !found {
		return ""
	}
	list, _, _ := strings.Cut(included, ")")
	return list
}
