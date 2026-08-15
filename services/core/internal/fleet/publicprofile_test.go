package fleet

import (
	"context"
	"encoding/json"
	"go/ast"
	"go/parser"
	"go/token"
	"net/http"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/DulsaraNethmin/Shipper/services/core/internal/db"
	"github.com/DulsaraNethmin/Shipper/services/core/internal/testsupport/pgtest"
)

// SHIP-79a — who a provider trades as, and the closed set a customer may be shown.
//
// # What this ticket had to create rather than serve
//
// SHIP-102's comparison screen has a customer weighing "price, timing, provider profile, and
// vehicle", and before this ticket the profile clause had nothing behind it. Measured:
// `internal/profiles` held `doc.go` and nothing else, the only provider profile in the service was
// [Profile] — **whose two fields are the service area and the specialties, which SHIP-102a's *Done
// when* forbids showing a customer** — and there was no trading name, no rating and no completed-job
// count anywhere in the schema. So a customer choosing between two strangers could be told a UUID, a
// verification flag and a join date.
//
// # The two halves of a profile are two types, and that is the design
//
// [PublicProfile] is what a customer may see; the service area and the specialties hang off
// [Profile] beside it and are never in reach of a disclosure that took the public half. The
// alternative — six fields on one struct and a rule about which four to copy — is correct exactly as
// long as everybody keeps copying four.
//
// [TestThePublicProfileIsAClosedSet] is what makes the set a set rather than a convention.

// TestAProviderDeclaresWhoTheyTradeAs is the first half of the *Done when*: there is now something
// to say about a provider beyond their account.
func TestAProviderDeclaresWhoTheyTradeAs(t *testing.T) {
	pool := pgtest.DB(t)
	provider := newVerifiedProvider(t, pool, "trades-as@example.com", "+61400000841")
	router := newTestRouter(t, pool)

	rec := as(t, router, provider, http.MethodPatch, "/v1/fleet/profile",
		`{"display_name":"Southbank  Removals","operates_as":"Business"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("declaring = %d, want 200 (%s)", rec.Code, rec.Body)
	}

	var body struct {
		DisplayName string `json:"display_name"`
		OperatesAs  string `json:"operates_as"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("the response is not JSON: %v", err)
	}

	// Collapsed, not merely trimmed — "Southbank  Removals" and "Southbank Removals" are one
	// business, the treatment a make and a model already get.
	if body.DisplayName != "Southbank Removals" {
		t.Errorf("display_name = %q, want %q", body.DisplayName, "Southbank Removals")
	}
	// Lower-cased, so a client sending the label rather than the value is not a validation error.
	if body.OperatesAs != "business" {
		t.Errorf("operates_as = %q, want business", body.OperatesAs)
	}

	// And it survives a read, which is what the customer's disclosure will go through.
	read := as(t, router, provider, http.MethodGet, "/v1/fleet/profile", "")
	if read.Code != http.StatusOK {
		t.Fatalf("reading the profile = %d (%s)", read.Code, read.Body)
	}
	if !strings.Contains(read.Body.String(), `"display_name":"Southbank Removals"`) {
		t.Errorf("the declaration did not survive a read: %s", read.Body)
	}
}

// TestAnUndeclaredProfileOmitsTheNameRatherThanInventingOne pins the state a provider is in before
// they finish onboarding.
//
// **Never a fallback to an email address or a phone number.** Docs/01 §7 asks the platform to
// minimise how far a contact detail travels, and a default that filled a customer-facing name from
// one would be the most direct possible breach of it — worse than the blank it replaced.
func TestAnUndeclaredProfileOmitsTheNameRatherThanInventingOne(t *testing.T) {
	pool := pgtest.DB(t)
	provider := newVerifiedProvider(t, pool, "undeclared@example.com", "+61400000842")

	rec := as(t, newTestRouter(t, pool), provider, http.MethodGet, "/v1/fleet/profile", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("reading an empty profile = %d (%s)", rec.Code, rec.Body)
	}

	body := rec.Body.String()
	for _, key := range []string{"display_name", "operates_as"} {
		if strings.Contains(body, key) {
			t.Errorf("an undeclared profile carries %q: %s", key, body)
		}
	}
	// The account's own contact details are not in reach of this response at all.
	for _, leaked := range []string{"undeclared@example.com", "61400000842"} {
		if strings.Contains(body, leaked) {
			t.Errorf("the profile response carries %q — a name is never defaulted from a contact "+
				"detail (Docs/01 §7)", leaked)
		}
	}
}

// TestAFirstDeclarationNamesBothFields is the rule 000303's NOT NULLs make necessary, enforced where
// it can be explained.
//
// Both columns are NOT NULL, so there is no half-declared provider to write. Left to the store, the
// upsert would violate `ck_provider_profiles_operates_as` and reach the client as a `500` — an error
// about the database standing in for a rule about the request. The service reads the row under the
// lock it already holds and answers `422` naming the field that is missing.
func TestAFirstDeclarationNamesBothFields(t *testing.T) {
	pool := pgtest.DB(t)
	provider := newVerifiedProvider(t, pool, "half-declared@example.com", "+61400000843")
	router := newTestRouter(t, pool)

	rec := as(t, router, provider, http.MethodPatch, "/v1/fleet/profile",
		`{"display_name":"Half Declared"}`)
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("a first declaration naming one field = %d, want 422 (%s)", rec.Code, rec.Body)
	}
	if !strings.Contains(rec.Body.String(), "operates_as") {
		t.Errorf("the refusal does not name the missing field: %s", rec.Body)
	}

	// Nothing was written, so the next attempt is still a first declaration.
	var rows int
	if err := pgtest.DB(t).QueryRow(t.Context(),
		`SELECT count(*) FROM provider_profiles WHERE provider_id = $1`, provider).Scan(&rows); err != nil {
		t.Fatalf("counting profiles: %v", err)
	}
	if rows != 0 {
		t.Errorf("a refused declaration wrote %d rows", rows)
	}

	// With both, it is accepted — and afterwards either may be sent alone, because the row the
	// upsert keeps the other value in now exists.
	if rec := as(t, router, provider, http.MethodPatch, "/v1/fleet/profile",
		`{"display_name":"Half Declared","operates_as":"individual"}`); rec.Code != http.StatusOK {
		t.Fatalf("a complete first declaration = %d (%s)", rec.Code, rec.Body)
	}
	rec = as(t, router, provider, http.MethodPatch, "/v1/fleet/profile",
		`{"operates_as":"business"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("amending one field afterwards = %d, want 200 (%s)", rec.Code, rec.Body)
	}
	if !strings.Contains(rec.Body.String(), `"display_name":"Half Declared"`) {
		t.Errorf("amending one field lost the other: %s", rec.Body)
	}
}

// TestAnEmptyDisplayNameIsRefusedRatherThanClearing is the one place [ProfileFields]'s pointer rule
// departs from the sets beside it, stated so it is not later "fixed".
//
// An empty list clears a service area, because "I no longer serve any single postcode" is a real
// declaration. An empty name is not the equivalent: there is no state in which a provider who has
// told customers who they are goes back to being an identifier on a comparison screen, and 000303
// cannot hold one.
func TestAnEmptyDisplayNameIsRefusedRatherThanClearing(t *testing.T) {
	pool := pgtest.DB(t)
	provider := newVerifiedProvider(t, pool, "clearing@example.com", "+61400000844")
	router := newTestRouter(t, pool)

	if rec := as(t, router, provider, http.MethodPatch, "/v1/fleet/profile",
		`{"display_name":"Yarra Freight","operates_as":"business"}`); rec.Code != http.StatusOK {
		t.Fatalf("declaring = %d (%s)", rec.Code, rec.Body)
	}

	for _, attempt := range []string{`{"display_name":""}`, `{"display_name":"   "}`, `{"display_name":"A"}`} {
		rec := as(t, router, provider, http.MethodPatch, "/v1/fleet/profile", attempt)
		if rec.Code != http.StatusUnprocessableEntity {
			t.Errorf("%s = %d, want 422 (%s)", attempt, rec.Code, rec.Body)
		}
	}

	read := as(t, router, provider, http.MethodGet, "/v1/fleet/profile", "")
	if !strings.Contains(read.Body.String(), "Yarra Freight") {
		t.Errorf("a refused clearance changed the declaration: %s", read.Body)
	}
}

// TestOnlyTheTwoOperatingFormsAreAccepted pairs [OperatingForms] with
// `ck_provider_profiles_operates_as`, in both directions.
//
// Docs/10 §3.4's discipline for an enumeration held in two places. A value the Go list knows and the
// constraint refuses is a 500 on a request the validator accepted; one the constraint knows and Go
// does not is a value nothing can ever store through the API and nothing will ever remove.
func TestOnlyTheTwoOperatingFormsAreAccepted(t *testing.T) {
	pool := pgtest.DB(t)

	var definition string
	if err := pool.QueryRow(t.Context(),
		`SELECT pg_get_constraintdef(oid) FROM pg_constraint WHERE conname = 'ck_provider_profiles_operates_as'`,
	).Scan(&definition); err != nil {
		t.Fatalf("reading ck_provider_profiles_operates_as: %v — 000303 is what creates it", err)
	}

	inConstraint := quotedStrings(definition)
	sort.Strings(inConstraint)

	inGo := make([]string, 0, len(OperatingForms))
	for _, form := range OperatingForms {
		inGo = append(inGo, form.String())
	}
	sort.Strings(inGo)

	if !reflect.DeepEqual(inGo, inConstraint) {
		t.Errorf("fleet.OperatingForms is %v and ck_provider_profiles_operates_as permits %v.\n"+
			"  Docs/10 §3.4: an enumeration held in two places is paired with a test that reads "+
			"the constraint. They have parted company.", inGo, inConstraint)
	}

	// And through the endpoint, so the validator agrees with both.
	provider := newVerifiedProvider(t, pool, "forms@example.com", "+61400000845")
	router := newTestRouter(t, pool)
	rec := as(t, router, provider, http.MethodPatch, "/v1/fleet/profile",
		`{"display_name":"Not A Form","operates_as":"partnership"}`)
	if rec.Code != http.StatusUnprocessableEntity {
		t.Errorf("an unknown operating form = %d, want 422 (%s)", rec.Code, rec.Body)
	}
}

// TestThePublicProfileIsAClosedSet is the *Done when*'s "the fields are a closed set", made
// enforceable rather than asserted.
//
// **The point is the second discloser, not the first.** Today one read shows a customer a provider —
// the offers list, through `cmd/api`'s mapping into `bidding.ProviderSummary`. The clause asks that
// every other read disclosing a provider show the same set, and the only way to keep that true
// through a reader nobody has written is for the set to be a type somebody has to map from. This
// test is what stops a field being added to [PublicProfile] without the decision being taken: a new
// one fails here, and the failure names what it costs.
//
// The forbidden pair is checked in the same breath, because a widening of this struct is exactly
// where they would arrive.
func TestThePublicProfileIsAClosedSet(t *testing.T) {
	want := []string{"DisplayName", "OperatesAs", "ProviderID"}

	var got []string
	found := false

	for _, file := range fleetSourceFiles(t) {
		parsed, err := parser.ParseFile(token.NewFileSet(), file, nil, 0)
		if err != nil {
			t.Fatalf("parsing %s: %v", file, err)
		}
		ast.Inspect(parsed, func(node ast.Node) bool {
			spec, ok := node.(*ast.TypeSpec)
			if !ok || spec.Name.Name != "PublicProfile" {
				return true
			}
			structure, ok := spec.Type.(*ast.StructType)
			if !ok {
				return true
			}
			found = true
			for _, field := range structure.Fields.List {
				name, _ := fieldNameAndJSONTag(field)
				got = append(got, name)
			}
			return true
		})
	}

	if !found {
		t.Fatal("PublicProfile was not found in this package, so this test is checking nothing")
	}

	sort.Strings(got)
	if !reflect.DeepEqual(got, want) {
		t.Errorf("PublicProfile carries %v, and the closed set is %v.\n"+
			"  SHIP-79a's Done when asks for a closed set, and for the same set to reach every "+
			"read that discloses a provider to a customer. Adding a field here widens what every "+
			"such read may show, so it is a decision to record rather than a struct to grow — and "+
			"the service area and the specialties may never be among them (SHIP-102a).", got, want)
	}

	// Belt and braces on the pair the *Done when* names: not by field name, which the list above
	// already covers, but by the words a well-meaning addition would use.
	for _, forbidden := range []string{"area", "specialt", "postcode", "state"} {
		for _, name := range got {
			if strings.Contains(strings.ToLower(name), forbidden) {
				t.Errorf("PublicProfile carries %q. A provider's service area and specialties are "+
					"a competitor's map of the market and SHIP-102a's Done when forbids showing "+
					"either to a customer.", name)
			}
		}
	}
}

// TestThePublicProfilesBatchReadAnswersOnlyThePublicHalf is the disclosure boundary at the seam that
// actually serves a customer.
//
// `cmd/api` assembles the comparison screen from what this returns. It returns [PublicProfile], so a
// service area cannot arrive there by being copied out of a [Profile] somebody read for convenience
// — which is the failure the split exists to make impossible rather than unlikely.
func TestThePublicProfilesBatchReadAnswersOnlyThePublicHalf(t *testing.T) {
	pool := pgtest.DB(t)
	service := newTestService()

	declared := newVerifiedProvider(t, pool, "batch-declared@example.com", "+61400000846")
	silent := newVerifiedProvider(t, pool, "batch-silent@example.com", "+61400000847")

	if err := db.InTx(t.Context(), pool, func(ctx context.Context, r db.Runner) error {
		_, err := service.Declare(ctx, r, declared, ProfileFields{
			DisplayName: ptr("Bass Strait Carriers"),
			OperatesAs:  ptr("business"),
			States:      &[]string{"TAS"},
			Specialties: &[]Specialty{SpecialtyRefrigerated},
		})
		return err
	}); err != nil {
		t.Fatalf("declaring: %v", err)
	}

	// The fixture has an area and a specialty to leak, or the assertion below is vacuous.
	profile, err := service.Profile(t.Context(), pool, declared)
	if err != nil {
		t.Fatalf("reading the whole profile: %v", err)
	}
	if len(profile.Areas) == 0 || len(profile.Specialties) == 0 {
		t.Fatalf("the fixture declared no area or no specialty, so this proves nothing: %+v", profile)
	}

	found, err := service.PublicProfiles(t.Context(), pool, []uuid.UUID{declared, silent, uuid.Nil, declared})
	if err != nil {
		t.Fatalf("reading the public profiles: %v", err)
	}

	if got := found[declared].DisplayName; got != "Bass Strait Carriers" {
		t.Errorf("display_name = %q, want %q", got, "Bass Strait Carriers")
	}
	if got := found[declared].OperatesAs; got != OperatesAsBusiness {
		t.Errorf("operates_as = %q, want business", got)
	}

	// A provider who has declared nothing is absent rather than present and blank: an offer must
	// not vanish from a comparison screen because its provider has not finished onboarding, and
	// the caller renders what it has.
	if _, present := found[silent]; present {
		t.Errorf("a provider who has declared nothing is in the map: %+v", found[silent])
	}
	if _, present := found[uuid.Nil]; present {
		t.Error("the nil provider is in the map")
	}
}
