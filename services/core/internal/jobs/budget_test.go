package jobs

import (
	"context"
	"go/ast"
	"go/parser"
	"go/token"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/DulsaraNethmin/Shipper/services/core/internal/clock"
	"github.com/DulsaraNethmin/Shipper/services/core/internal/db"
	"github.com/DulsaraNethmin/Shipper/services/core/internal/events"
	"github.com/DulsaraNethmin/Shipper/services/core/internal/testsupport/pgtest"
)

// SHIP-67 — the customer's budget is stored, and never serialised to a provider.
//
// Docs/01 §4.3 is the rule: the maximum is private from providers, not as an amount, not as a
// band, and not as a "budget supplied" flag. CLAUDE.md lists it among the invariants whose
// violation is a defect rather than a style choice, and Docs/01 §4.3 adds the part that makes it
// urgent rather than tidy — the behaviour it prevents is **not reversible**. Once providers learn
// to bid at the ceiling, removing the signal does not remove the habit.
//
// # What this file proves, and what it deliberately cannot
//
// Docs/11 §8 paired SHIP-67 with SHIP-83 so that the invariant would be tested against a
// *serialised provider response* rather than against struct fields. SHIP-83 is three dependency
// hops away — SHIP-82 → SHIP-81 → (SHIP-79, SHIP-80) → SHIP-78 — so there is no provider-facing
// response in this service to serialise. Deferring SHIP-67 with it would leave the column unbuilt
// for a rule it already satisfies, so the decision recorded in Docs/11 §3 and §8 is: build the
// field now with the strongest proof available today, and reserve SHIP-83 to the same owner.
//
// The strongest proof available today is three tests, and together they are stronger than a
// single assertion about one future endpoint would have been:
//
//   - [TestOnlyTheOwnersResponseCarriesTheBudget] reads this package's own source. A provider
//     shape written later cannot carry the field without changing an allow-list, which is a line
//     in a diff with a reviewer attached rather than a field somebody forgot to redact.
//   - [TestNoRefusalLeaksTheBudget] drives the real handlers and asserts that nothing anybody but
//     the owner can obtain from this domain mentions a budget in any form.
//   - [TestTheStatusChangedEventCarriesNoBudget] covers the copy of a job that travels furthest.
//     An event reaches Kafka and whatever is behind it (SHIP-134), which is well past the last
//     endpoint that could have redacted anything.
//
// **When SHIP-83 lands it adds the fourth**: its provider response, serialised, asserted to carry
// no budget. That test belongs beside these.

// budgetBearers are the types in this package that may hold the customer's maximum, and why.
//
// An allow-list rather than a rule about naming, on the same reasoning as cmd/api's
// publicMutatingRoutes: the safe set is small, closed, and each entry has an argument behind it,
// so widening it is a deliberate act somebody reviews rather than a field that slipped through.
//
// Nothing here is provider-facing. The nearest thing to a general principle is that the budget may
// exist on the domain's own record, on what the *owner* sends, and on what the *owner* is answered
// with — and nowhere else in the package.
var budgetBearers = map[string]string{
	"Job": "the domain's own record of a job. Never serialised — jobFrom builds the response " +
		"type instead, which is what keeps the two decisions apart",
	"DraftFields": "what a customer may set on their own draft, absent distinguished from empty",
	"draftRequest": "the body of POST /v1/jobs and PATCH /v1/jobs/{id}, both owner-only: " +
		"routes_jobs.go declares RequireUser and the domain checks ownership",
	"jobResponse": "the owning customer's view of their own job. The one serialised shape that " +
		"may carry it; SHIP-82's feed and SHIP-83's provider detail get types of their own",
}

// TestOnlyTheOwnersResponseCarriesTheBudget is the structural half of SHIP-67's *Done when*.
//
// It parses every non-test file in this package and fails when a struct outside [budgetBearers]
// declares a field carrying the budget — by Go name or by json tag, so neither renaming the field
// nor tagging it differently escapes the check.
//
// # Why the source and not reflection
//
// Because the failure to catch is a type that does not exist yet. Reflection can only be pointed
// at types somebody remembered to point it at, which is precisely the act a future provider
// response would omit. The source is the complete list by construction: a new struct is in it the
// moment it is written, whether or not anybody thought about this test.
//
// The reverse direction matters too, and is checked below: every name in the allow-list must
// actually carry the budget. A list that has rotted — naming a type that no longer has the field,
// or one that no longer exists — is a list nobody would notice had stopped constraining anything.
func TestOnlyTheOwnersResponseCarriesTheBudget(t *testing.T) {
	found := map[string][]string{}

	for _, file := range packageSourceFiles(t) {
		parsed, err := parser.ParseFile(token.NewFileSet(), file, nil, 0)
		if err != nil {
			t.Fatalf("parsing %s: %v", file, err)
		}

		ast.Inspect(parsed, func(node ast.Node) bool {
			spec, ok := node.(*ast.TypeSpec)
			if !ok {
				return true
			}
			structure, ok := spec.Type.(*ast.StructType)
			if !ok {
				return true
			}

			for _, field := range structure.Fields.List {
				name, tag := fieldNameAndJSONTag(field)
				if mentionsBudget(name) || mentionsBudget(tag) {
					found[spec.Name.Name] = append(found[spec.Name.Name], name)
				}
			}
			return true
		})
	}

	for typeName, fields := range found {
		if _, allowed := budgetBearers[typeName]; allowed {
			continue
		}
		t.Errorf("%s carries the budget (%s) and is not one of the shapes allowed to.\n"+
			"A customer's budget is never exposed to a provider — not as an amount, a band, or "+
			"a \"budget supplied\" flag (Docs/01 §4.3, CLAUDE.md).\n"+
			"If this really is the owning customer's own view, add it to budgetBearers with the "+
			"reason. If anything provider-facing can reach it, it must not have the field at all.",
			typeName, strings.Join(fields, ", "))
	}

	for typeName, why := range budgetBearers {
		if len(found[typeName]) == 0 {
			t.Errorf("budgetBearers names %s (%s) but it carries no budget field.\n"+
				"Either the field was renamed and this list is now checking nothing, or the "+
				"entry is stale and should go.", typeName, why)
		}
	}

	// The one that must exist, spelled the way the contract publishes it. Without this the
	// test would pass just as happily on a service that had removed the feature.
	if got := strings.Join(found["jobResponse"], ", "); got != "BudgetCents" {
		t.Errorf("jobResponse's budget field is %q, want BudgetCents — SHIP-65's *Done when* "+
			"returns the full job including budget to its owner", got)
	}
}

// TestNoRefusalLeaksTheBudget drives the real handlers and reads what came back.
//
// Every route in this domain is owner-only, so "a provider must not see the budget" is today the
// same statement as "nobody but the owner sees anything at all". That makes this test cheap and
// worth having anyway: the refusals are where a leak would actually appear, because an error
// message is the one part of a response nobody writes a schema for.
//
// A provider account is used deliberately rather than only a second customer. A provider holding
// a valid token is the caller Docs/01 §4.3 is about, and the platform decides on the account's
// role rather than on the token's claim.
func TestNoRefusalLeaksTheBudget(t *testing.T) {
	pool := pgtest.DB(t)
	router := newTestRouter(t, pool)

	owner := newCustomer(t, pool, "budget-owner@example.com", "+61400000670")
	provider := newProvider(t, pool, "budget-provider@example.com", "+61400000671")
	stranger := newCustomer(t, pool, "budget-stranger@example.com", "+61400000672")

	created := as(t, router, owner, http.MethodPost, "/v1/jobs",
		`{"goods_description": "Two-seater sofa", "budget_cents": 150000}`)
	if created.Code != http.StatusCreated {
		t.Fatalf("creating the job = %d: %s", created.Code, created.Body)
	}
	job, ok := decode[map[string]any](t, created)["id"].(string)
	if !ok {
		t.Fatalf("the create response carries no id: %s", created.Body)
	}

	// The owner sees it, on the way back from the write as well as from both reads. A budget
	// that could be set and not read would be a field the app cannot show.
	owned := map[string]*httptest.ResponseRecorder{
		"the create response": created,
		"the detail response": as(t, router, owner, http.MethodGet, "/v1/jobs/"+job, ""),
		"the list response":   as(t, router, owner, http.MethodGet, "/v1/jobs", ""),
	}
	for name, rec := range owned {
		if !strings.Contains(rec.Body.String(), `"budget_cents":150000`) {
			t.Errorf("%s does not carry the owner's own budget: %s", name, rec.Body)
		}
	}

	// And nobody else does, through any route this domain serves.
	elsewhere := map[string]*httptest.ResponseRecorder{
		"a provider reading the job":     as(t, router, provider, http.MethodGet, "/v1/jobs/"+job, ""),
		"a provider listing jobs":        as(t, router, provider, http.MethodGet, "/v1/jobs", ""),
		"a provider editing the job":     as(t, router, provider, http.MethodPatch, "/v1/jobs/"+job, `{"budget_cents": 1}`),
		"a provider cancelling it":       as(t, router, provider, http.MethodPost, "/v1/jobs/"+job+"/cancel", `{}`),
		"a provider creating a job":      as(t, router, provider, http.MethodPost, "/v1/jobs", `{"budget_cents": 1}`),
		"a stranger reading the job":     as(t, router, stranger, http.MethodGet, "/v1/jobs/"+job, ""),
		"a stranger listing jobs":        as(t, router, stranger, http.MethodGet, "/v1/jobs", ""),
		"a stranger editing the job":     as(t, router, stranger, http.MethodPatch, "/v1/jobs/"+job, `{"budget_cents": 1}`),
		"a stranger cancelling it":       as(t, router, stranger, http.MethodPost, "/v1/jobs/"+job+"/cancel", `{}`),
		"a malformed id from a provider": as(t, router, provider, http.MethodGet, "/v1/jobs/not-a-uuid", ""),
	}
	for name, rec := range elsewhere {
		body := rec.Body.String()
		if mentionsBudget(body) {
			t.Errorf("%s produced a response mentioning the budget, which Docs/01 §4.3 forbids "+
				"in every form:\n%s", name, body)
		}
		if strings.Contains(body, "150000") {
			t.Errorf("%s produced a response carrying the amount:\n%s", name, body)
		}
	}
}

// TestTheStatusChangedEventCarriesNoBudget covers the copy of a job that travels furthest.
//
// SHIP-134 publishes the outbox to Kafka, and Docs/06 §4 has consumers acting on events without
// reading the database back. An event carrying the budget would therefore reach every consumer
// that ever subscribes, past the last endpoint with an opportunity to redact anything — and the
// notification a provider receives when a job is published (M5) is one of them.
//
// Read out of the outbox table rather than from the recording sink, because what a consumer sees
// is the stored jsonb rather than the Go value that produced it.
func TestTheStatusChangedEventCarriesNoBudget(t *testing.T) {
	pool := pgtest.DB(t)
	service := newDraftService(t, &fakeGeocoder{})

	customer := newCustomer(t, pool, "budget-event@example.com", "+61400000673")

	created, err := service.CreateDraft(t.Context(), pool, customer, DraftFields{
		GoodsDescription: text("Two-seater sofa"),
		BudgetCents:      money(150_000),
	})
	if err != nil {
		t.Fatalf("creating the job: %v", err)
	}

	publisher := NewService(events.NewOutbox(), clock.NewFixed(testInstant), nil)
	if err := db.InTx(t.Context(), pool, func(ctx context.Context, r db.Runner) error {
		_, err := publisher.Transition(ctx, r, Move{
			JobID: created.ID, To: StatusOpen, Actor: User(ActorCustomer, customer),
		})
		return err
	}); err != nil {
		t.Fatalf("publishing the job: %v", err)
	}

	var payload string
	if err := pool.QueryRow(t.Context(),
		`SELECT payload::text FROM outbox WHERE aggregate_id = $1`, created.ID).Scan(&payload); err != nil {
		t.Fatalf("reading the event back: %v", err)
	}

	if mentionsBudget(payload) || strings.Contains(payload, "150000") {
		t.Errorf("the published event carries the customer's budget, which travels to every "+
			"consumer there will ever be (Docs/01 §4.3):\n%s", payload)
	}
}

// TestTheBudgetIsRefusedWhenItIsNotAnAmount keeps the field in the error contract's shape rather
// than in the database's.
//
// ck_jobs_budget refuses a negative amount on its own, and a caller told `ck_jobs_budget` can do
// nothing with that. Docs/10 §4.6 puts the answer in the validator, which names the field a client
// has to correct.
func TestTheBudgetIsRefusedWhenItIsNotAnAmount(t *testing.T) {
	pool := pgtest.DB(t)
	router := newTestRouter(t, pool)

	customer := newCustomer(t, pool, "budget-invalid@example.com", "+61400000674")

	bodies := map[string]string{
		"a negative":        `{"budget_cents": -1}`,
		"one above the cap": `{"budget_cents": ` + strconv.FormatInt(maxBudgetCents+1, 10) + `}`,
	}
	for name, body := range bodies {
		rec := as(t, router, customer, http.MethodPost, "/v1/jobs", body)
		if rec.Code != http.StatusUnprocessableEntity {
			t.Errorf("%s budget = %d, want 422: %s", name, rec.Code, rec.Body)
			continue
		}

		named := false
		for _, detail := range decode[errorEnvelope](t, rec).Error.Details {
			named = named || detail.Field == "budget_cents"
		}
		if !named {
			t.Errorf("%s budget: no detail names budget_cents: %s", name, rec.Body)
		}
	}
}

// TestClearingTheBudgetIsDistinctFromNotMentioningIt is the three-way distinction DraftFields
// exists for, applied to the one field where getting it wrong has a commercial consequence.
//
// A customer who set a maximum and thought better of it must be able to remove it. Without the
// pointer, `0` and "did not mention it" would be one request and a budget would be impossible to
// clear once set.
func TestClearingTheBudgetIsDistinctFromNotMentioningIt(t *testing.T) {
	pool := pgtest.DB(t)
	service := newDraftService(t, &fakeGeocoder{})

	customer := newCustomer(t, pool, "budget-clear@example.com", "+61400000675")

	created, err := service.CreateDraft(t.Context(), pool, customer, DraftFields{
		BudgetCents: money(150_000),
	})
	if err != nil {
		t.Fatalf("creating the job: %v", err)
	}

	untouched, err := edit(t, pool, service, customer, created.ID,
		DraftFields{GoodsDescription: text("A sofa")})
	if err != nil {
		t.Fatalf("editing another field: %v", err)
	}
	if untouched.BudgetCents != 150_000 {
		t.Errorf("an edit that did not mention the budget changed it to %d", untouched.BudgetCents)
	}

	cleared, err := edit(t, pool, service, customer, created.ID, DraftFields{BudgetCents: money(0)})
	if err != nil {
		t.Fatalf("clearing the budget: %v", err)
	}
	if cleared.BudgetCents != 0 {
		t.Errorf("the budget = %d after being cleared", cleared.BudgetCents)
	}

	// NULL rather than 0 in the column, because ck_jobs_budget refuses zero and "not supplied"
	// has to be representable.
	var stored *int64
	if err := pool.QueryRow(t.Context(),
		`SELECT (budget * 100)::bigint FROM jobs WHERE id = $1`, created.ID).Scan(&stored); err != nil {
		t.Fatalf("reading the column: %v", err)
	}
	if stored != nil {
		t.Errorf("the cleared budget is stored as %d, want NULL", *stored)
	}
}

// --- helpers -----------------------------------------------------------------------------------

// packageSourceFiles is every non-test Go file in this package's directory.
func packageSourceFiles(t *testing.T) []string {
	t.Helper()

	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatalf("reading the package directory: %v", err)
	}

	var files []string
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		files = append(files, filepath.Join(".", name))
	}
	sort.Strings(files)

	if len(files) == 0 {
		t.Fatal("no source files were found, so this test is checking nothing")
	}
	return files
}

// fieldNameAndJSONTag reads what a struct field is called in Go and on the wire.
//
// An embedded field has no name of its own, so its type is reported instead — which is what a
// reader would call it, and what a leak through embedding would look like.
func fieldNameAndJSONTag(field *ast.Field) (name, tag string) {
	if len(field.Names) > 0 {
		name = field.Names[0].Name
	} else if ident, ok := field.Type.(*ast.Ident); ok {
		name = ident.Name
	}

	if field.Tag == nil {
		return name, ""
	}
	literal, err := strconv.Unquote(field.Tag.Value)
	if err != nil {
		return name, field.Tag.Value
	}
	return name, reflect.StructTag(literal).Get("json")
}

// mentionsBudget is the one definition of "carries the budget", used by every check here so the
// source test and the wire tests cannot disagree about what they are looking for.
func mentionsBudget(s string) bool { return strings.Contains(strings.ToLower(s), "budget") }
