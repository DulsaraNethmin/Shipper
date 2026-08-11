package jobs

import (
	"context"
	"strings"
	"unicode/utf8"

	"github.com/DulsaraNethmin/Shipper/services/core/internal/httpx"
	"github.com/DulsaraNethmin/Shipper/services/core/internal/validate"
)

// The address and location value object (SHIP-60).
//
// A job has two of these and the whole product turns on them: a provider decides whether to bid
// by reading them, an eligibility filter will compare them (SHIP-81), and a driver is sent to
// them. They arrive as whatever a person typed into a form on a phone.
//
// # Why the coordinate lives on the same type as the address
//
// [Location] is an address *and* what the platform made of it. Keeping them together is what
// makes "resolved" a property of one value rather than a pair of fields on the job that could
// disagree — a coordinate belonging to an address that has since been edited is worse than no
// coordinate at all, and this type makes that state unrepresentable: changing the address
// produces a new Location, unresolved until it is resolved again.
//
// # Why there is no shared geo package
//
// Docs/11 §9 held this open from wave 1 and SHIP-60 is where it was to be settled. It is
// settled in favour of leaving it alone, and the reasoning is recorded in Docs/11 §3. In short:
// the geocoding adapter's wide signature is named once, in ports.go, and converted here — the
// width does not propagate past this file, so the cost a neutral coordinate type would remove is
// one declaration rather than a shape three domains adopt. The trigger for revisiting is named
// there too.

// State is an Australian state or territory, in the abbreviated form Australia Post uses.
//
// Stored and compared as the abbreviation, because that is what every Australian address form
// offers and what a postcode is allocated against. The long names are accepted on input (see
// [ParseState]) and never stored.
type State string

const (
	StateACT State = "ACT"
	StateNSW State = "NSW"
	StateNT  State = "NT"
	StateQLD State = "QLD"
	StateSA  State = "SA"
	StateTAS State = "TAS"
	StateVIC State = "VIC"
	StateWA  State = "WA"
)

// States is all eight, sorted, which is the order ck_jobs_pickup_state lists them in.
//
// Docs/10 §3.4 requires an enumeration in the database to be held to the Go list by a test, the
// same pairing ck_jobs_status already has —
// TestJobStateConstraintsMatchTheGoConstants is that test.
var States = []State{StateACT, StateNSW, StateNT, StateQLD, StateSA, StateTAS, StateVIC, StateWA}

// Valid reports whether s is one of the eight.
func (s State) Valid() bool {
	for _, known := range States {
		if s == known {
			return true
		}
	}
	return false
}

func (s State) String() string { return string(s) }

// longStateNames maps the spelled-out form to the abbreviation.
//
// Accepted on input because people type what is on their driving licence, and refusing "New
// South Wales" on a delivery address is the kind of validation failure that makes a person
// abandon the form. Normalisation is the platform's job, not theirs.
var longStateNames = map[string]State{
	"australian capital territory": StateACT,
	"new south wales":              StateNSW,
	"northern territory":           StateNT,
	"queensland":                   StateQLD,
	"south australia":              StateSA,
	"tasmania":                     StateTAS,
	"victoria":                     StateVIC,
	"western australia":            StateWA,
}

// ParseState reads a state from what somebody typed, and reports whether it recognised it.
//
// Comma-ok rather than an error, because "that is not a state" is the only failure and the
// caller already knows which field it came from — an error value would carry nothing the caller
// could not say better itself.
func ParseState(value string) (State, bool) {
	tidy := collapse(value)
	if tidy == "" {
		return "", false
	}

	if s := State(strings.ToUpper(tidy)); s.Valid() {
		return s, true
	}
	if s, known := longStateNames[strings.ToLower(tidy)]; known {
		return s, true
	}
	return "", false
}

// The validation limits.
//
// Constants rather than configuration, and that is a deliberate narrowing of Docs/06 §5.3 rather
// than an oversight. The rule there is that anything expected to move under *operational*
// pressure lives server-side, and these do not move: the longest legitimate Australian street
// line is a fact about addresses, not a policy the marketplace tunes. The limits that will move
// — the largest load, the goods categories — are the ones SHIP-58 puts in reference data.
//
// They are bounds against abuse and against a client with a runaway text field, not a judgement
// about what a good address looks like. Generous on purpose.
const (
	maxAddressLine   = 200
	maxSuburb        = 100
	postcodeDigits   = 4
	maxGoodsText     = 2000
	maxVehicleText   = 200
	maxHandlingNotes = 2000
)

// Address is an Australian delivery address, in the four parts a form collects.
//
// The street part is freeform and stays that way. Unit numbers, level numbers, lot numbers, PO
// boxes, roadside mail boxes and "the shed behind the second gate" are all legitimate first lines
// of an Australian address, and a type that tried to structure them would refuse deliveries this
// marketplace exists to carry.
type Address struct {
	Line     string
	Suburb   string
	State    State
	Postcode string
}

// IsZero reports whether nothing at all was supplied.
//
// A draft is allowed to have no address yet (Docs/01 §4.1 saves drafts; SHIP-75 resumes them),
// so "empty" is a state the product supports rather than a validation failure. A *partly*
// supplied address is a different thing and is refused — see [Address.Validate].
func (a Address) IsZero() bool {
	return a.Line == "" && a.Suburb == "" && a.State == "" && a.Postcode == ""
}

// Normalise tidies what was typed, without deciding what it means.
//
// Three things happen and no more: whitespace is collapsed, the state is resolved to its
// abbreviation, and the postcode loses any spaces inside it. An unrecognised state is left
// exactly as typed so that [Address.Validate] can report it against the value the customer
// entered rather than against an empty string.
//
// **Case is deliberately not touched.** Upper-casing the suburb is the Australia Post convention
// and it is also how "McDonald Street" becomes "MCDONALD STREET" and "Coffs Harbour" stops
// looking like a place a person wrote. Comparisons that need to ignore case can do so at the
// point of comparison; a normalisation that destroys information cannot be undone.
func (a Address) Normalise() Address {
	out := Address{
		Line:     collapse(a.Line),
		Suburb:   collapse(a.Suburb),
		Postcode: strings.Join(strings.Fields(string(a.Postcode)), ""),
	}
	if state, known := ParseState(string(a.State)); known {
		out.State = state
	} else {
		out.State = State(collapse(string(a.State)))
	}
	return out
}

// Validate reports what is wrong with a supplied address, one entry per offending field.
//
// field is the dotted JSON path the caller sent it under — "pickup" or "dropoff" — so the
// details in the error contract name the field the client can actually fix (Docs/10 §4.6).
//
// An empty address produces nothing. A partly filled one produces a `required` entry per missing
// part, and that asymmetry is the point: "I have not started" and "I have started and stopped
// halfway" are different, and only the second is a mistake worth reporting.
func (a Address) Validate(field string, e *validate.Errors) {
	if a.IsZero() {
		return
	}

	if e.Required(field+".line", a.Line) {
		e.Length(field+".line", a.Line, 1, maxAddressLine)
	}
	if e.Required(field+".suburb", a.Suburb) {
		e.Length(field+".suburb", a.Suburb, 1, maxSuburb)
	}

	switch {
	case a.State == "":
		e.Add(field+".state", validate.CodeRequired, "This is required.")
	case !a.State.Valid():
		e.Add(field+".state", validate.CodeNotAllowed,
			"Enter an Australian state or territory.")
	}

	switch {
	case a.Postcode == "":
		e.Add(field+".postcode", validate.CodeRequired, "This is required.")
	case !isDigits(a.Postcode) || utf8.RuneCountInString(a.Postcode) != postcodeDigits:
		// The allocation of a postcode range to a state is deliberately not checked, here
		// or in the constraint. The ranges have exceptions — 2600 and 2611 are ACT inside
		// the NSW range, 3644 is NSW inside the Victorian one — and they change when
		// Australia Post says they do, which makes them reference data rather than a rule
		// to compile in (Docs/06 §5.3).
		e.Add(field+".postcode", validate.CodeInvalid, "Enter a four-digit postcode.")
	}
}

// OneLine renders the address the way a person would write it on an envelope.
//
// This is what goes to the geocoder, which takes a single string — see [Geocoder]. It is not a
// display format for a client: the app has the four parts and can lay them out however its
// screen requires.
func (a Address) OneLine() string {
	var parts []string
	if a.Line != "" {
		parts = append(parts, a.Line)
	}

	// The suburb, state and postcode are one line on an envelope, separated by spaces rather
	// than by commas.
	var tail []string
	for _, p := range []string{a.Suburb, string(a.State), a.Postcode} {
		if p != "" {
			tail = append(tail, p)
		}
	}
	if len(tail) > 0 {
		parts = append(parts, strings.Join(tail, " "))
	}
	return strings.Join(parts, ", ")
}

// Location is an address and what the platform resolved it to.
//
// Resolved is not derivable from the coordinate being non-zero: (0, 0) is a real point in the
// Gulf of Guinea, and a boolean that means "we looked and found it" says something a pair of
// floats cannot. The database stores the same distinction as two NULLable columns bound by
// ck_jobs_pickup_coordinate_is_a_pair.
//
// Formatted is the geocoder's own rendering of the place it matched. It is kept for display and
// for support: when a customer says the driver went to the wrong address, this is what the
// platform actually looked up, which is a different question from what the customer typed.
type Location struct {
	Address

	Latitude  float64
	Longitude float64
	Resolved  bool
	Formatted string
}

// Coordinate returns the resolved point, and reports whether there is one.
//
// Comma-ok so that a caller cannot mistake an unresolved location for one at the equator. This
// is the accessor a response type and an eligibility filter both go through.
func (l Location) Coordinate() (lat, lng float64, resolved bool) {
	if !l.Resolved {
		return 0, 0, false
	}
	return l.Latitude, l.Longitude, true
}

// collapse trims a string and reduces every run of whitespace inside it to one space.
//
// The whole of this platform's whitespace normalisation, applied to every piece of address text.
// It matches what the geocoding stub does to an address before hashing it, which is why the same
// address typed with a double space resolves to the same coordinate.
func collapse(s string) string { return strings.Join(strings.Fields(s), " ") }

func isDigits(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

// resolve asks the geocoder where an address is, and never fails because it could not find out.
//
// SHIP-59a's acceptance criterion is that a failed lookup does not fail the job, and this is
// where that is honoured. Three outcomes reach the same place by different routes:
//
//   - no geocoder configured — the location is stored as typed;
//   - the provider answered and did not recognise the address — likewise, and this is expected
//     often enough for rural addresses that it is logged at info rather than as a problem;
//   - the lookup did not complete — likewise, and this is logged as a warning, because it says
//     something about the platform rather than about the address.
//
// # What is deliberately not logged
//
// The address itself. It is a customer's or a recipient's home in most cases, and an address in
// an application log is personal information sitting somewhere with a different retention period
// and a much wider audience than the job record (Docs/05 §3). The label and the error are enough
// to tell an operator which lookup failed and why; which address it was is in the job.
func (s *Service) resolve(ctx context.Context, label string, l Location) Location {
	if s.geo == nil || l.Address.IsZero() || l.Resolved {
		return l
	}

	lat, lng, formatted, found, err := s.geo.Lookup(ctx, l.OneLine())
	switch {
	case err != nil:
		httpx.LoggerFrom(ctx).Warn("the geocoder could not be reached; the address is stored unresolved",
			"address", label, "error", err)
		return l
	case !found:
		httpx.LoggerFrom(ctx).Info("the geocoder did not recognise the address; it is stored unresolved",
			"address", label)
		return l
	}

	l.Latitude, l.Longitude, l.Formatted, l.Resolved = lat, lng, formatted, true
	return l
}
