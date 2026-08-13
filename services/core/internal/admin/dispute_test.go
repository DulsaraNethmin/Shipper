package admin_test

import (
	"strings"
	"testing"

	"github.com/DulsaraNethmin/Shipper/services/core/internal/admin"
)

// The parts of SHIP-163 that need no database.
//
// The database side of these constants is checked in migrations/disputes_test.go, which reads
// ck_disputes_category and ck_disputes_complainant_party out of pg_constraint and holds them to the
// Go lists in both directions (Docs/10 §3.4). What that pairing cannot see is a constant declared
// here and left out of the slice: the slice would agree with the database, and Valid would refuse a
// value the package itself defines.

func TestEveryCategoryConstantIsInTheList(t *testing.T) {
	declared := []admin.Category{
		admin.CategoryProviderNoShow,
		admin.CategoryGoodsDiffer,
		admin.CategoryCustomerUnavailable,
		admin.CategoryLate,
		admin.CategoryGoodsDamaged,
		admin.CategoryOther,
	}
	if len(declared) != len(admin.Categories) {
		t.Errorf("%d constants and %d in admin.Categories", len(declared), len(admin.Categories))
	}
	for _, c := range declared {
		if !c.Valid() {
			t.Errorf("%q is a declared category that admin.Categories omits, so Valid refuses "+
				"a value this package defines", c)
		}
	}

	declaredParties := []admin.Party{admin.PartyCustomer, admin.PartyProvider}
	if len(declaredParties) != len(admin.Parties) {
		t.Errorf("%d party constants and %d in admin.Parties", len(declaredParties), len(admin.Parties))
	}
	for _, p := range declaredParties {
		if !p.Valid() {
			t.Errorf("%q is a declared party that admin.Parties omits", p)
		}
	}
}

// TestWhatIsNotAParty names the actor kinds that look like they belong here and do not.
//
// This column records which side of a *delivery* the complainant was on. An administrator is not on
// one, a driver has no account, and the platform does not complain about itself.
func TestWhatIsNotAParty(t *testing.T) {
	for _, notAParty := range []admin.Party{"admin", "driver", "system", "Customer", ""} {
		if notAParty.Valid() {
			t.Errorf("%q is accepted as a party to a job; Docs/02 §2's eligible users are the "+
				"customer who owns it and the provider who won it", notAParty)
		}
	}
}

// TestCategoryWireFormsAreStableAndDistinct pins the six strings a client sends and reads.
//
// [admin.Category.Wire] is derived rather than tabulated, so it cannot disagree with the constants —
// which is the drift worth preventing and not the risk this test covers. **These strings are
// published.** A client already sending `goods_damaged_or_missing` cannot have it renamed underneath
// it, and writing all six out is what makes a change to that function visible as a change to the
// contract rather than as a passing refactor.
//
// It is also what holds the one shortened string in place. Docs/02 §5 writes "Customer unavailable
// at pickup/delivery"; the stored form drops the tail because the derived wire form would otherwise
// contain a slash, and this test is where that decision is visible.
func TestCategoryWireFormsAreStableAndDistinct(t *testing.T) {
	want := map[admin.Category]string{
		admin.CategoryProviderNoShow:      "provider_fails_to_arrive",
		admin.CategoryGoodsDiffer:         "goods_differ_from_listing",
		admin.CategoryCustomerUnavailable: "customer_unavailable",
		admin.CategoryLate:                "delivery_is_late",
		admin.CategoryGoodsDamaged:        "goods_damaged_or_missing",
		admin.CategoryOther:               "other",
	}

	if len(want) != len(admin.Categories) {
		t.Fatalf("%d wire forms for %d categories", len(want), len(admin.Categories))
	}

	seen := map[string]admin.Category{}
	for _, c := range admin.Categories {
		got := c.Wire()
		if got != want[c] {
			t.Errorf("%q on the wire is %q, want %q", c, got, want[c])
		}
		if first, clash := seen[got]; clash {
			t.Errorf("%q and %q are both %q on the wire", first, c, got)
		}
		seen[got] = c

		// Every published form has to be legal in three client languages, which is what the
		// one shortened string exists for. A slash is the case that caught it.
		if strings.ContainsAny(got, "/ .-") {
			t.Errorf("%q is not a legal enum value in a generated client", got)
		}

		back, known := admin.CategoryFromWire(got)
		if !known || back != c {
			t.Errorf("CategoryFromWire(%q) = %q, %v; want %q back", got, back, known, c)
		}
	}
}

// TestTheStoredFormIsNotAcceptedOnTheWire is the same call delivery makes about milestones.
//
// A client that sent "Goods damaged or missing" has misread the contract rather than typed the
// wrong case, and accepting both spellings would make two forms interchangeable in one direction
// and not the other.
func TestTheStoredFormIsNotAcceptedOnTheWire(t *testing.T) {
	for _, c := range admin.Categories {
		if _, known := admin.CategoryFromWire(string(c)); known && string(c) != c.Wire() {
			t.Errorf("the stored form %q was accepted as a wire value", c)
		}
	}
	for _, notACategory := range []string{"", "damaged", "GOODS_DAMAGED_OR_MISSING", "goods damaged or missing"} {
		if _, known := admin.CategoryFromWire(notACategory); known {
			t.Errorf("%q was accepted as a category", notACategory)
		}
	}
}

// TestCategoriesWireIsDerivedFromTheList stops a validation message naming a value the handler
// would then refuse.
func TestCategoriesWireIsDerivedFromTheList(t *testing.T) {
	wire := admin.CategoriesWire()
	if len(wire) != len(admin.Categories) {
		t.Fatalf("CategoriesWire() has %d entries for %d categories", len(wire), len(admin.Categories))
	}
	for i, c := range admin.Categories {
		if wire[i] != c.Wire() {
			t.Errorf("CategoriesWire()[%d] = %q, want %q", i, wire[i], c.Wire())
		}
	}
}
