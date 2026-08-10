package geocoding

import (
	"context"
	"errors"
	"testing"

	"github.com/DulsaraNethmin/Shipper/services/core/internal/config"
)

const sydney = "1 Martin Place, Sydney NSW 2000"

// A stub that answered differently on the second run would make every assertion built on a
// coordinate — a distance, a golden file, an eligibility radius — flap.
func TestStubIsDeterministic(t *testing.T) {
	first, second := NewStub(), NewStub()

	lat1, lng1, formatted1, found1, err1 := first.Lookup(context.Background(), sydney)
	lat2, lng2, formatted2, found2, err2 := second.Lookup(context.Background(), sydney)

	if err1 != nil || err2 != nil {
		t.Fatalf("Lookup: %v / %v", err1, err2)
	}
	if !found1 || !found2 {
		t.Fatal("the stub did not resolve an ordinary address")
	}
	if lat1 != lat2 || lng1 != lng2 || formatted1 != formatted2 {
		t.Errorf("two stubs disagreed: (%v, %v, %q) vs (%v, %v, %q)",
			lat1, lng1, formatted1, lat2, lng2, formatted2)
	}

	// Fixed, not merely equal to itself. These are the values SHIP-60's tests will be
	// written against, so a change to the hashing is a change somebody has to notice.
	if lat1 != -16.378079 || lng1 != 145.129545 {
		t.Errorf("stub coordinates for %q = (%v, %v); if this was changed deliberately, "+
			"update the expectation and everything asserting on it", sydney, lat1, lng1)
	}
}

// Whitespace is the only normalisation the stub claims, and it has to be applied before the
// hash or the same address typed twice resolves to two places.
func TestStubTidiesWhitespaceBeforeResolving(t *testing.T) {
	lat1, lng1, formatted, _, _ := NewStub().Lookup(context.Background(), "  1 Martin   Place,\tSydney NSW 2000 ")
	lat2, lng2, _, _, _ := NewStub().Lookup(context.Background(), sydney)

	if lat1 != lat2 || lng1 != lng2 {
		t.Errorf("whitespace changed the answer: (%v, %v) vs (%v, %v)", lat1, lng1, lat2, lng2)
	}
	if formatted != sydney {
		t.Errorf("formatted = %q, want %q", formatted, sydney)
	}
}

// Every coordinate the stub invents has to be somewhere plausible, because distances in
// this product are in kilometres from a real place (SHIP-81).
func TestStubResolvesInsideAustralia(t *testing.T) {
	addresses := []string{
		sydney,
		"200 Collins Street, Melbourne VIC 3000",
		"Lot 4, Bruce Highway, Bowen QLD 4805",
		"PO Box 91, Coober Pedy SA 5723",
		"12 Kangaroo Track, Nowhere NT 0870",
	}
	for _, a := range addresses {
		lat, lng, _, found, err := NewStub().Lookup(context.Background(), a)
		if err != nil || !found {
			t.Fatalf("Lookup(%q): found = %v, err = %v", a, found, err)
		}
		if lat < latSouth || lat > latNorth || lng < lngWest || lng > lngEast {
			t.Errorf("Lookup(%q) = (%v, %v), outside Australia", a, lat, lng)
		}
	}
}

// SHIP-59a's acceptance criterion: a failed lookup is an outcome, not a failure. The stub
// has to be able to produce it, or SHIP-60 and SHIP-63 have no way to test the path.
func TestStubReportsAnUnknownAddressAsNotFound(t *testing.T) {
	rural := "Lot 7, Unnamed Road, Innamincka SA 5731"
	s := NewStub(rural)

	lat, lng, formatted, found, err := s.Lookup(context.Background(), rural)
	if err != nil {
		t.Fatalf("an unknown address must not be an error: %v", err)
	}
	if found {
		t.Fatal("found = true for an address the stub was told it does not know")
	}
	if lat != 0 || lng != 0 || formatted != "" {
		t.Errorf("a not-found lookup returned data: (%v, %v, %q)", lat, lng, formatted)
	}

	// And everything else still resolves.
	if _, _, _, found, _ := s.Lookup(context.Background(), sydney); !found {
		t.Error("naming one unknown address stopped the others resolving")
	}
}

// Nothing was asked, so nothing was found — and that is not an error either. Whether an
// empty address is allowed at all is the validator's question (SHIP-60).
func TestStubTreatsAnEmptyAddressAsNotFound(t *testing.T) {
	for _, a := range []string{"", "   ", "\t\n"} {
		_, _, _, found, err := NewStub().Lookup(context.Background(), a)
		if found || err != nil {
			t.Errorf("Lookup(%q): found = %v, err = %v, want false and nil", a, found, err)
		}
	}
}

// The two failure modes must stay distinguishable in the stub as well as in the provider,
// or a test proves something the production path does not do.
func TestStubCanFailWithoutClaimingNotFound(t *testing.T) {
	boom := errors.New("provider unreachable")
	s := &Stub{Err: boom}

	_, _, _, found, err := s.Lookup(context.Background(), sydney)
	if !errors.Is(err, boom) {
		t.Errorf("err = %v, want the injected failure", err)
	}
	if found {
		t.Error("found = true alongside an error")
	}
}

// SHIP-59a's acceptance criterion: provider-backed in staging, stub in tests.
func TestUseStubSelectsByEnvironment(t *testing.T) {
	cases := []struct {
		env      config.Environment
		wantStub bool
	}{
		{config.Development, true},
		{config.Staging, false},
		{config.Production, false},
		{config.Environment(""), true},
	}
	for _, c := range cases {
		if got := UseStub(c.env); got != c.wantStub {
			t.Errorf("UseStub(%q) = %v, want %v", c.env, got, c.wantStub)
		}
	}
}
