package pagination

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"testing"

	"github.com/DulsaraNethmin/Shipper/services/core/internal/httpx"
)

// TestACursorSurvivesARoundTrip is the property everything else rests on.
func TestACursorSurvivesARoundTrip(t *testing.T) {
	cases := map[string]Cursor{
		"a timestamp and an id": {"2026-08-11T03:30:00.000Z", "0198f2c1-6b40-7a11-9c3e-2f9a4d51b7e0"},
		"one field":             {"42"},
		"a field with spaces":   {"Two-seater sofa, wrapped"},
		"an empty field":        {"", "0198f2c1-6b40-7a11-9c3e-2f9a4d51b7e0"},
	}

	for name, want := range cases {
		t.Run(name, func(t *testing.T) {
			got, err := Decode(want.Encode(), len(want))
			if err != nil {
				t.Fatalf("Decode(%q) = %v", want.Encode(), err)
			}
			if len(got) != len(want) {
				t.Fatalf("decoded %d fields, want %d", len(got), len(want))
			}
			for i := range want {
				if got[i] != want[i] {
					t.Errorf("field %d = %q, want %q", i, got[i], want[i])
				}
			}
		})
	}
}

// The empty cursor and the empty string are the same request: the first page.
func TestTheEmptyCursorIsTheFirstPage(t *testing.T) {
	if encoded := (Cursor{}).Encode(); encoded != "" {
		t.Errorf("the empty cursor encodes to %q, want the empty string", encoded)
	}

	got, err := Decode("", 2)
	if err != nil {
		t.Fatalf("Decode(\"\") = %v, want no error", err)
	}
	if len(got) != 0 {
		t.Errorf("Decode(\"\") = %v, want nothing", got)
	}
}

// A cursor a client has mangled, truncated, or kept across an encoding change is refused rather
// than decoded into plausible nonsense.
//
// What is checked here is *shape* — that the token decodes, is this version, and carries the
// number of fields the caller expects. Whether a field is a real timestamp or a real UUID is the
// domain's question, because only the domain knows what its ordering key is; `jobs` answers it in
// TestAListRefusesACursorItDidNotIssue.
//
// The last two cases are what the version prefix and the field count exist for: without them, a
// cursor from an older encoding would decode into the wrong number of fields, and every list
// endpoint would be indexing into a slice whose length came from a client.
func TestAMangledCursorIsRefused(t *testing.T) {
	valid := Cursor{"2026-08-11T03:30:00.000Z", "0198f2c1-6b40-7a11-9c3e-2f9a4d51b7e0"}.Encode()
	anotherVersion := base64.RawURLEncoding.EncodeToString(
		[]byte("2" + cursorSeparator + "a" + cursorSeparator + "b"))

	cases := map[string]struct {
		raw    string
		fields int
	}{
		"not base64":             {"!!!not-base64!!!", 2},
		"base64 of nothing much": {"YWJj", 2},
		"the wrong field count":  {valid, 3},
		"an encoding we retired": {anotherVersion, 2},
		"a version prefix only":  {base64.RawURLEncoding.EncodeToString([]byte(cursorVersion)), 2},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := Decode(tc.raw, tc.fields)
			if err == nil {
				t.Fatal("a mangled cursor was accepted")
			}

			var apiErr *httpx.Error
			if !errors.As(err, &apiErr) {
				t.Fatalf("Decode() = %v, want an error in the contract's shape", err)
			}
			if apiErr.Status != http.StatusBadRequest {
				t.Errorf("status = %d, want 400", apiErr.Status)
			}
		})
	}
}

// TestLimitAppliesTheDefaultRefusesNonsenseAndClampsTheRest is Docs/10 §4.5's three rules.
//
// The asymmetry is the point: a value that is not a number is a client defect and is refused, while
// a value above the maximum is a request the platform narrows — refusing that one would turn a
// server-side tuning change into a broken client.
func TestLimitAppliesTheDefaultRefusesNonsenseAndClampsTheRest(t *testing.T) {
	cases := map[string]struct {
		raw     string
		want    int
		refused bool
	}{
		"absent":       {"", DefaultLimit, false},
		"whitespace":   {"   ", DefaultLimit, false},
		"in range":     {"5", 5, false},
		"at the limit": {"100", MaxLimit, false},
		"above it":     {"5000", MaxLimit, false},
		"zero":         {"0", 0, true},
		"negative":     {"-1", 0, true},
		"not a number": {"twenty", 0, true},
		"a decimal":    {"20.5", 0, true},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			got, err := Limit(tc.raw)
			switch {
			case tc.refused && err == nil:
				t.Fatalf("Limit(%q) = %d, want a refusal", tc.raw, got)
			case tc.refused:
				return
			case err != nil:
				t.Fatalf("Limit(%q) = %v", tc.raw, err)
			case got != tc.want:
				t.Errorf("Limit(%q) = %d, want %d", tc.raw, got, tc.want)
			}
		})
	}
}

// The envelope is the one in Docs/10 §4.5, and `data` is an empty array rather than null.
//
// A nil slice marshals to `null`, and a client iterating it breaks the first time a new customer
// with no jobs opens the app — and never again in testing.
func TestTheEnvelopeNeverSendsNullData(t *testing.T) {
	body, err := json.Marshal(NewPage[string](nil, ""))
	if err != nil {
		t.Fatalf("marshalling: %v", err)
	}

	got := string(body)
	if strings.Contains(got, "null") {
		t.Errorf("an empty page marshals to %s", got)
	}
	if got != `{"data":[],"has_more":false}` {
		t.Errorf("an empty page is %s", got)
	}

	full := NewPage([]string{"a"}, "cursor-token")
	if !full.HasMore {
		t.Error("a page with a next cursor says there is no more")
	}
	body, _ = json.Marshal(full)
	if want := `{"data":["a"],"next_cursor":"cursor-token","has_more":true}`; string(body) != want {
		t.Errorf("a page is %s, want %s", body, want)
	}
}

// A field carrying the separator cannot be encoded, because it would decode into the wrong number
// of fields — silently, and only for the rows whose key contains that byte.
func TestACursorFieldCannotCarryTheSeparator(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("a field containing the separator was encoded")
		}
	}()
	_ = Cursor{"a" + cursorSeparator + "b"}.Encode()
}

// The page sizes moved from constants to configuration at SHIP-15g. These cover the move itself:
// that Limit reads what was installed, and that a bad Bounds cannot make every page empty.

func TestLimitUsesTheInstalledBounds(t *testing.T) {
	t.Cleanup(func() { SetBounds(Bounds{Default: DefaultLimit, Max: MaxLimit}) })
	SetBounds(Bounds{Default: 5, Max: 50})

	got, err := Limit("")
	if err != nil || got != 5 {
		t.Errorf("Limit(\"\") = %d, %v; want the configured default 5", got, err)
	}

	got, err = Limit("999")
	if err != nil || got != 50 {
		t.Errorf("Limit(\"999\") = %d, %v; want narrowing to the configured maximum 50", got, err)
	}
}

// internal/config refuses all of these at load, so reaching SetBounds with one means a caller
// built a Bounds by hand. Keeping the known-good fallbacks beats applying a zero default, which
// would make every page empty and look like a database with no rows.
func TestSetBoundsIgnoresValuesConfigWouldHaveRefused(t *testing.T) {
	t.Cleanup(func() { SetBounds(Bounds{Default: DefaultLimit, Max: MaxLimit}) })

	for name, b := range map[string]Bounds{
		"zero default":         {Default: 0, Max: 100},
		"zero maximum":         {Default: 20, Max: 0},
		"default over ceiling": {Default: 200, Max: 100},
	} {
		SetBounds(Bounds{Default: DefaultLimit, Max: MaxLimit})
		SetBounds(b)

		got, err := Limit("")
		if err != nil || got != DefaultLimit {
			t.Errorf("%s: Limit(\"\") = %d, %v; want the fallback %d", name, got, err, DefaultLimit)
		}
	}
}
