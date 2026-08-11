package jobs

import (
	"regexp"
	"sort"
	"testing"

	"github.com/DulsaraNethmin/Shipper/services/core/internal/httpx"
	"github.com/DulsaraNethmin/Shipper/services/core/internal/testsupport/pgtest"
	"github.com/DulsaraNethmin/Shipper/services/core/internal/validate"
)

// SHIP-60's value object, tested where its guarantees live: the normalisation and the field
// errors in Go, and the state constraint against a real PostgreSQL.

func TestParseStateAcceptsEveryFormAPersonTypes(t *testing.T) {
	cases := map[string]struct {
		want  State
		known bool
	}{
		"NSW":               {StateNSW, true},
		"nsw":               {StateNSW, true},
		"  Nsw  ":           {StateNSW, true},
		"New South Wales":   {StateNSW, true},
		"new  south  wales": {StateNSW, true},
		"QLD":               {StateQLD, true},
		"Queensland":        {StateQLD, true},
		"ACT":               {StateACT, true},
		"Tasmania":          {StateTAS, true},

		// Not states. "NSY" is the shape of a typo and "Auckland" the shape of somebody in
		// the wrong country; both must be refused rather than guessed at, because a
		// delivery sent to a guessed state is a delivery sent to the wrong place.
		"NSY":      {"", false},
		"Auckland": {"", false},
		"":         {"", false},
		"   ":      {"", false},
	}

	for input, want := range cases {
		t.Run(input, func(t *testing.T) {
			got, known := ParseState(input)
			if known != want.known {
				t.Fatalf("ParseState(%q) known = %v, want %v", input, known, want.known)
			}
			if got != want.want {
				t.Errorf("ParseState(%q) = %q, want %q", input, got, want.want)
			}
		})
	}
}

func TestNormaliseTidiesWithoutDestroying(t *testing.T) {
	got := Address{
		Line:     "  12   Smith  Street  ",
		Suburb:   " Coffs   Harbour ",
		State:    " new south wales ",
		Postcode: " 20 42 ",
	}.Normalise()

	want := Address{Line: "12 Smith Street", Suburb: "Coffs Harbour", State: StateNSW, Postcode: "2042"}
	if got != want {
		t.Errorf("Normalise() = %#v, want %#v", got, want)
	}

	// Case is deliberately left alone. Upper-casing the suburb is the Australia Post
	// convention and it is also how "McDonald Street" becomes something no person wrote.
	mixed := Address{Line: "5 McDonald Street", Suburb: "O'Connor", State: StateACT, Postcode: "2602"}
	if tidy := mixed.Normalise(); tidy != mixed {
		t.Errorf("Normalise() changed the case of an already tidy address: %#v", tidy)
	}
}

// An unrecognised state survives normalisation unchanged, so that the field error names the value
// the customer typed rather than an empty string they will not recognise.
func TestNormaliseKeepsAnUnrecognisedStateVisible(t *testing.T) {
	got := Address{State: "  Westeros "}.Normalise()
	if got.State != "Westeros" {
		t.Errorf("State = %q, want the value as typed", got.State)
	}
}

func TestAddressValidation(t *testing.T) {
	cases := []struct {
		name    string
		address Address
		fields  []string
	}{
		{
			name:    "an empty address is not a mistake",
			address: Address{},
			fields:  nil,
		},
		{
			name:    "a complete address passes",
			address: Address{Line: "12 Smith Street", Suburb: "Newtown", State: StateNSW, Postcode: "2042"},
			fields:  nil,
		},
		{
			// The asymmetry with the case above is the point: having started and stopped
			// halfway is a mistake, having not started is not.
			name:    "a partly filled address reports every missing part",
			address: Address{Suburb: "Newtown"},
			fields:  []string{"pickup.line", "pickup.postcode", "pickup.state"},
		},
		{
			name:    "a state that is not one of the eight",
			address: Address{Line: "1 High Street", Suburb: "Somewhere", State: "Westeros", Postcode: "2042"},
			fields:  []string{"pickup.state"},
		},
		{
			name:    "a postcode that is not four digits",
			address: Address{Line: "1 High Street", Suburb: "Somewhere", State: StateVIC, Postcode: "312"},
			fields:  []string{"pickup.postcode"},
		},
		{
			name:    "a postcode that is not digits at all",
			address: Address{Line: "1 High Street", Suburb: "Somewhere", State: StateVIC, Postcode: "3O42"},
			fields:  []string{"pickup.postcode"},
		},
		{
			// 0800 is Darwin. A postcode stored as a number would be 800 and would match
			// nothing, which is why the column is text and why this case is here.
			name:    "a leading zero is a valid postcode",
			address: Address{Line: "1 High Street", Suburb: "Darwin", State: StateNT, Postcode: "0800"},
			fields:  nil,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var e validate.Errors
			tc.address.Validate("pickup", &e)

			got := offendingFields(e.Fields())
			if len(got) != len(tc.fields) {
				t.Fatalf("fields = %v, want %v", got, tc.fields)
			}
			for i := range got {
				if got[i] != tc.fields[i] {
					t.Errorf("fields = %v, want %v", got, tc.fields)
					break
				}
			}
		})
	}
}

func offendingFields(fields []httpx.FieldError) []string {
	var out []string
	for _, f := range fields {
		out = append(out, f.Field)
	}
	sort.Strings(out)
	return out
}

// OneLine is what the geocoder is asked about, so it has to read like an address rather than like
// a struct dump.
func TestOneLineReadsLikeAnEnvelope(t *testing.T) {
	full := Address{Line: "12 Smith Street", Suburb: "Newtown", State: StateNSW, Postcode: "2042"}
	if got, want := full.OneLine(), "12 Smith Street, Newtown NSW 2042"; got != want {
		t.Errorf("OneLine() = %q, want %q", got, want)
	}

	// A partly filled address still produces something a geocoder can work with, because
	// OneLine is also what a caller logs and reads. It never produces stray commas.
	partial := Address{Suburb: "Newtown", State: StateNSW}
	if got, want := partial.OneLine(), "Newtown NSW"; got != want {
		t.Errorf("OneLine() = %q, want %q", got, want)
	}
	if got := (Address{}).OneLine(); got != "" {
		t.Errorf("OneLine() on an empty address = %q, want empty", got)
	}
}

// A location cannot be mistaken for one at the equator.
func TestAnUnresolvedLocationHasNoCoordinate(t *testing.T) {
	unresolved := Location{Address: Address{Suburb: "Newtown"}}
	if _, _, resolved := unresolved.Coordinate(); resolved {
		t.Error("an unresolved location reported a coordinate")
	}

	// Resolved to (0, 0) is a real answer and must survive, which is why Resolved is a field
	// rather than a test on the numbers.
	atNull := Location{Resolved: true}
	lat, lng, resolved := atNull.Coordinate()
	if !resolved || lat != 0 || lng != 0 {
		t.Errorf("Coordinate() = (%v, %v, %v), want (0, 0, true)", lat, lng, resolved)
	}
}

// quotedLiteral pulls the string literals out of a constraint definition.
//
// PostgreSQL rewrites `state IN ('ACT', …)` as `state = ANY (ARRAY['ACT'::text, …])`, so what
// comes back from pg_get_constraintdef is not the text that was written.
var quotedLiteral = regexp.MustCompile(`'([^']*)'::text`)

// TestJobStateConstraintsMatchTheGoConstants is the pairing Docs/10 §3.4 requires of every
// enumeration, and that ck_jobs_status already has.
//
// Both constraints rather than one, because 000403 writes the list out twice — once for pickup
// and once for drop-off. That repetition is exactly what this test makes safe: a territory added
// to one and not the other would be an address the platform accepts for a pickup and refuses for
// a delivery.
func TestJobStateConstraintsMatchTheGoConstants(t *testing.T) {
	pool := pgtest.DB(t)

	inGo := map[string]bool{}
	for _, state := range States {
		inGo[string(state)] = true
	}
	if len(States) != 8 {
		t.Errorf("jobs.States holds %d states; Australia has eight states and territories", len(States))
	}
	if len(inGo) != len(States) {
		t.Errorf("jobs.States contains a duplicate: %d constants, %d distinct", len(States), len(inGo))
	}

	for _, constraint := range []string{"ck_jobs_pickup_state", "ck_jobs_dropoff_state"} {
		t.Run(constraint, func(t *testing.T) {
			var definition string
			if err := pool.QueryRow(t.Context(),
				`SELECT pg_get_constraintdef(oid) FROM pg_constraint WHERE conname = $1`,
				constraint).Scan(&definition); err != nil {
				t.Fatalf("reading %s: %v", constraint, err)
			}

			inPostgres := map[string]bool{}
			for _, m := range quotedLiteral.FindAllStringSubmatch(definition, -1) {
				inPostgres[m[1]] = true
			}

			for state := range inGo {
				if !inPostgres[state] {
					t.Errorf("%s is a Go constant and %s does not allow it", state, constraint)
				}
			}
			for state := range inPostgres {
				if !inGo[state] {
					t.Errorf("%s allows %q and no Go constant names it", constraint, state)
				}
			}
		})
	}
}
