package jobs

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/DulsaraNethmin/Shipper/services/core/internal/clock"
	"github.com/DulsaraNethmin/Shipper/services/core/internal/testsupport/pgtest"
)

// SHIP-58's acceptance criterion, and the half of SHIP-59 that belongs to a draft.
//
// The *Done when* is "categories load from configuration and are served to clients, not compiled
// in", and the load-bearing word is the last one. A test that asserted the platform serves
// thirteen categories with particular names would be asserting the *default* — which is the one
// thing the criterion says must not be what a client depends on. So every test below builds a
// catalogue that looks nothing like the shipped one, and checks that the platform serves and
// enforces *that*.

// testCatalogue is deliberately nothing like the configured default.
//
// Two entries, invented names, and the refused one is the sort of thing no real policy would
// name. If any test below passed against the shipped list as well, it would not be testing what
// it claims to.
func testCatalogue(t *testing.T) Catalogue {
	t.Helper()

	c, err := NewCatalogue([]Category{
		{Code: "widgets", Label: "Widgets", Description: "Boxed widgets.",
			Carried: true, Provisional: true},
		{Code: "anvils", Label: "Anvils", Description: "Far too heavy.",
			Carried: false, Provisional: true},
	})
	if err != nil {
		t.Fatalf("building the test catalogue: %v", err)
	}
	return c
}

// TestCatalogueRefusesAListItCannotServe covers the four conditions [NewCatalogue] rejects.
//
// Each is a configuration mistake that would otherwise be discovered by a customer: a duplicate
// code answers according to insertion order, and a catalogue with nothing carried refuses every
// publication while looking like working software.
func TestCatalogueRefusesAListItCannotServe(t *testing.T) {
	cases := []struct {
		name string
		in   []Category
	}{
		{"empty", nil},
		{"a blank code", []Category{{Code: "", Label: "Nameless", Carried: true}}},
		{"a duplicate code", []Category{
			{Code: "widgets", Label: "Widgets", Carried: true},
			{Code: "widgets", Label: "Widgets again", Carried: false},
		}},
		{"nothing carried", []Category{
			{Code: "anvils", Label: "Anvils", Carried: false},
		}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := NewCatalogue(tc.in); !errors.Is(err, ErrCatalogueUnusable) {
				t.Fatalf("NewCatalogue(%s) = %v, want ErrCatalogueUnusable", tc.name, err)
			}
		})
	}
}

// TestTheServedCatalogueIsTheConfiguredOne is SHIP-58's acceptance criterion at the wire.
//
// It asserts the two halves of "from configuration, not compiled in" together: what comes back is
// exactly what this service was built with, and it is not the default. The second assertion is
// what makes the first mean anything — without it the test would pass on a hard-coded list.
func TestTheServedCatalogueIsTheConfiguredOne(t *testing.T) {
	pool := pgtest.DB(t)
	router := newTestRouter(t, pool, WithCatalogue(testCatalogue(t)))

	// No credential: the route is declared Public, and the app reads it before anybody has
	// signed in. See jobs.Handler.Categories.
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/v1/goods-categories", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("GET /v1/goods-categories = %d, want 200 (%s)", rec.Code, rec.Body)
	}

	got := decode[categoriesResponse](t, rec)
	if len(got.Categories) != 2 {
		t.Fatalf("served %d categories, want the 2 this service was configured with: %+v",
			len(got.Categories), got.Categories)
	}

	// Order is the configuration's and is served unchanged — a client renders it in a picker.
	if got.Categories[0].Code != "widgets" || got.Categories[1].Code != "anvils" {
		t.Errorf("served %q then %q, want widgets then anvils — the configured order",
			got.Categories[0].Code, got.Categories[1].Code)
	}

	// The refused entry is served rather than filtered out, which is what lets a client say
	// what Shipper will not take and lets SHIP-59 refuse a publication by name.
	anvils := got.Categories[1]
	if anvils.Carried {
		t.Error("anvils came back carried; a refused category must be served as refused")
	}
	if anvils.Label != "Anvils" || anvils.Description != "Far too heavy." {
		t.Errorf("anvils = %q / %q, want the configured label and description",
			anvils.Label, anvils.Description)
	}

	// Docs/11 §5's reduced-form acceptance turns on this being visible to a client.
	for _, c := range got.Categories {
		if !c.Provisional {
			t.Errorf("%s came back provisional=false; the configured catalogue says true", c.Code)
		}
	}

	// And the assertion that makes the rest mean something: none of the shipped default's
	// codes is here, so nothing above could have passed against a compiled-in list.
	for _, c := range got.Categories {
		if c.Code == "general_freight" || c.Code == "dangerous_goods" {
			t.Fatalf("%s is from the default catalogue; the served list is compiled in, "+
				"not configured", c.Code)
		}
	}
}

// TestServingCategoriesWithoutACatalogueIsAWiringError holds the decision in [WithCatalogue].
//
// An empty list would be indistinguishable to a client from a platform that carries nothing, and
// the composition root is where the mistake actually is.
func TestServingCategoriesWithoutACatalogueIsAWiringError(t *testing.T) {
	pool := pgtest.DB(t)
	router := newTestRouter(t, pool)

	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/v1/goods-categories", nil))

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("GET /v1/goods-categories with no catalogue = %d, want 500 (%s)",
			rec.Code, rec.Body)
	}
}

// TestADraftAcceptsAnyServedCategoryIncludingOneNotCarried is the SHIP-58/SHIP-59 split.
//
// Docs/09 puts the prohibition "on publish", and Docs/01 §4.1 lets a customer save a draft and
// come back to it. A customer who has not yet worked out that Shipper will not take their anvils
// is allowed to write it down.
func TestADraftAcceptsAnyServedCategoryIncludingOneNotCarried(t *testing.T) {
	pool := pgtest.DB(t)
	svc := NewService(&recordingSink{}, clock.NewFixed(testInstant), &fakeGeocoder{},
		WithCatalogue(testCatalogue(t)))
	customer := newCustomer(t, pool, "draft-category@example.com", "+61400000710")

	for _, code := range []string{"widgets", "anvils"} {
		t.Run(code, func(t *testing.T) {
			job, err := svc.CreateDraft(t.Context(), pool, customer,
				DraftFields{GoodsCategory: text(code)})
			if err != nil {
				t.Fatalf("CreateDraft with %s: %v", code, err)
			}
			if job.GoodsCategory != code {
				t.Errorf("stored %q, want %q", job.GoodsCategory, code)
			}
		})
	}
}

// TestADraftRefusesACategoryTheCatalogueDoesNotServe checks the code is validated where the
// customer can still see the form.
//
// It is a field error rather than a domain code — the client's move is to re-fetch the catalogue,
// which is what a stale list on a phone needs. See [Catalogue.Lookup] for why this is kept
// distinct from a category that is served and refused.
func TestADraftRefusesACategoryTheCatalogueDoesNotServe(t *testing.T) {
	pool := pgtest.DB(t)
	svc := NewService(&recordingSink{}, clock.NewFixed(testInstant), &fakeGeocoder{},
		WithCatalogue(testCatalogue(t)))
	customer := newCustomer(t, pool, "bad-category@example.com", "+61400000711")

	_, err := svc.CreateDraft(t.Context(), pool, customer,
		DraftFields{GoodsCategory: text("live_unicorns")})
	if err == nil {
		t.Fatal("CreateDraft accepted a category the catalogue does not serve")
	}

	// The stored default is not consulted: general_freight is in the shipped list and not in
	// this service's, so it must be refused here too.
	if _, err := svc.CreateDraft(t.Context(), pool, customer,
		DraftFields{GoodsCategory: text("general_freight")}); err == nil {
		t.Fatal("CreateDraft accepted general_freight, which this service was not configured " +
			"with — the catalogue is compiled in rather than injected")
	}
}

// TestNamingACategoryWithoutACatalogueIsAWiringErrorRatherThanARefusal holds the other half of
// [WithCatalogue]'s argument.
//
// A nil catalogue reports every code unknown, so the tempting implementation refuses every draft
// that names a category and looks exactly like a client sending bad data.
func TestNamingACategoryWithoutACatalogueIsAWiringErrorRatherThanARefusal(t *testing.T) {
	pool := pgtest.DB(t)
	svc := NewService(&recordingSink{}, clock.NewFixed(testInstant), &fakeGeocoder{})
	customer := newCustomer(t, pool, "no-catalogue@example.com", "+61400000712")

	_, err := svc.CreateDraft(t.Context(), pool, customer,
		DraftFields{GoodsCategory: text("widgets")})
	if !errors.Is(err, ErrNoCatalogue) {
		t.Fatalf("CreateDraft with no catalogue = %v, want ErrNoCatalogue", err)
	}

	// A draft that names no category is unaffected, so a deployment with no catalogue still
	// serves every endpoint that does not involve one.
	if _, err := svc.CreateDraft(t.Context(), pool, customer,
		DraftFields{GoodsDescription: text("A pallet of something")}); err != nil {
		t.Fatalf("CreateDraft naming no category: %v", err)
	}
}
