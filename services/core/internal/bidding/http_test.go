package bidding

import (
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/DulsaraNethmin/Shipper/services/core/internal/authctx"
	"github.com/DulsaraNethmin/Shipper/services/core/internal/httpx"
)

// The wire contract of SHIP-84.
//
// These drive the handler on a mux of their own rather than through cmd/api's router, because what
// is being checked here is the domain's own half: what it accepts, what it refuses, and what shape
// it puts on the wire. The middleware around it — authentication, idempotency, the error envelope —
// belongs to cmd/api and is exercised end to end by scripts/verify/61-bidding.sh against the real
// binary.
//
// # Two of these tests are the reason this file exists, and they are two different privacy rules
//
//   - [TestTheBidResponseCarriesNothingOfTheCustomers] is Docs/01 §4.3's budget rule, made against
//     the serialised bytes and held to a **closed set of keys at every depth**. Not a search for the
//     word "budget": a search catches `budget_cents` and misses `max_price`, which is the axis
//     SHIP-83 found a source-parsing guard cannot have.
//   - [TestOneProvidersKeyCannotReachAnothersBid] is Docs/01 §4.3's *other* line — "treat provider
//     bid price as private from competing providers" — and its failure mode is a WHERE clause rather
//     than a response field. It is the test that would catch a store read scoped by job and key but
//     not by provider.

// newTestRouter mounts the handler on the pattern cmd/api registers it under.
//
// A real ServeMux rather than calling the handler directly, because the path parameter is part of
// what is being tested: the handler reads {id} through r.PathValue, and a handler invoked without a
// pattern would see an empty one and pass a test the served route would fail.
//
// A handler mounted here and not in cmd/api/routes_bidding.go is an endpoint that exists only in the
// tests, so the two lists are worth reading against each other — routes_golden.txt is what makes the
// other direction visible.
func newTestRouter(t *testing.T, pool *pgxpool.Pool) http.Handler {
	t.Helper()

	handler, err := NewHandler(newTestService(), pool, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatalf("building the handler: %v", err)
	}

	mux := http.NewServeMux()
	mux.Handle("POST /v1/jobs/{id}/bids", handler.Place())
	return mux
}

// as sends a request on behalf of an authenticated caller.
//
// The subject is put on the context by hand, which is what httpx.ResolveSubject does one layer out.
// The route declares RequireUser, so a handler reached without one would be a wiring defect rather
// than a request anybody could send.
//
// **The role on the subject is deliberately customer, whatever the account is.** It is a claim in a
// token, and the platform decides from the database instead — so a test that carried the "right"
// role here would be proving something about its own fixture. It is also what makes
// [TestACustomerIsRefusedRatherThanHidden] meaningful: the customer there is refused by the
// eligibility filter, not by the claim.
func as(t *testing.T, h http.Handler, caller uuid.UUID, key, target, body string) *httptest.ResponseRecorder {
	t.Helper()

	var reader io.Reader
	if body != "" {
		reader = strings.NewReader(body)
	}

	req := httptest.NewRequest(http.MethodPost, target, reader)
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	if key != "" {
		req.Header.Set(httpx.HeaderIdempotencyKey, key)
	}
	req = req.WithContext(authctx.WithSubject(req.Context(), authctx.Subject{
		UserID:    caller.String(),
		Role:      authctx.RoleCustomer,
		SessionID: uuid.Must(uuid.NewV7()).String(),
	}))

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

// wire is a market with the HTTP surface in front of it.
type wire struct {
	market
	router http.Handler
}

func newWire(t *testing.T) wire {
	t.Helper()

	m := newMarket(t)
	return wire{market: m, router: newTestRouter(t, m.pool)}
}

// bid places one offer over the wire, as the caller named.
func (w wire) bid(t *testing.T, caller uuid.UUID, key, body string) *httptest.ResponseRecorder {
	t.Helper()
	return as(t, w.router, caller, key, "/v1/jobs/"+w.job.String()+"/bids", body)
}

// validBody is a complete offer, in the wire's own vocabulary.
//
// The timestamps carry an offset rather than a Z, because that is what a phone in Melbourne sends
// and because the conversion to UTC is part of what these tests are for.
func validBody() string {
	return fmt.Sprintf(`{
		"amount_cents": 45000,
		"pickup_at": %q,
		"deliver_by": %q,
		"message": "Can collect from the loading dock."
	}`,
		testInstant.Add(48*time.Hour).In(melbourne).Format(time.RFC3339),
		testInstant.Add(56*time.Hour).In(melbourne).Format(time.RFC3339))
}

var melbourne = time.FixedZone("AEST", 10*60*60)

func decode[T any](t *testing.T, rec *httptest.ResponseRecorder) T {
	t.Helper()

	var out T
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("the response is not JSON: %v (%s)", err, rec.Body)
	}
	return out
}

type errorEnvelope struct {
	Error struct {
		Code    string `json:"code"`
		Message string `json:"message"`
		Details []struct {
			Field string `json:"field"`
			Code  string `json:"code"`
		} `json:"details"`
	} `json:"error"`
}

// --- the endpoint ---------------------------------------------------------------------------------

// TestPlacingABidOverTheWire is SHIP-84's *Done when* at the wire.
func TestPlacingABidOverTheWire(t *testing.T) {
	w := newWire(t)

	rec := w.bid(t, w.provider, "wire-first", validBody())
	if rec.Code != http.StatusCreated {
		t.Fatalf("POST = %d, want 201 (%s)", rec.Code, rec.Body)
	}

	body := decode[map[string]any](t, rec)
	if id, _ := body["id"].(string); id == "" {
		t.Error("the response carries no bid id")
	}
	if body["job_id"] != w.job.String() {
		t.Errorf("job_id is %v, want %s", body["job_id"], w.job)
	}
	if body["status"] != "submitted" {
		t.Errorf("status is %v, want the lower snake case wire form 'submitted'", body["status"])
	}
	if body["amount_cents"] != float64(45000) {
		t.Errorf("amount_cents is %v, want 45000", body["amount_cents"])
	}

	// The offsets the client sent are rendered back in UTC, which is what every other endpoint does
	// and what a client comparing two bids needs.
	if pickup, _ := body["pickup_at"].(string); !strings.HasSuffix(pickup, "Z") {
		t.Errorf("pickup_at is %q, want UTC", pickup)
	}
}

// TestARetryIsAnsweredTwoHundredWithTheOriginal is the retry path at the wire.
//
// 201 then 200, the same shape both times, and one row at the end. The status code is the only thing
// that differs, which is what lets a client that does not care parse one type and a client that does
// read the code.
func TestARetryIsAnsweredTwoHundredWithTheOriginal(t *testing.T) {
	w := newWire(t)

	first := w.bid(t, w.provider, "wire-retry", validBody())
	if first.Code != http.StatusCreated {
		t.Fatalf("the first offer = %d, want 201 (%s)", first.Code, first.Body)
	}

	again := w.bid(t, w.provider, "wire-retry", validBody())
	if again.Code != http.StatusOK {
		t.Fatalf("the retry = %d, want 200 (%s)", again.Code, again.Body)
	}
	if again.Body.String() != first.Body.String() {
		t.Errorf("the retry answered differently:\n  first: %s\n  again: %s", first.Body, again.Body)
	}
	if n := w.bids(t, w.provider, w.job); n != 1 {
		t.Errorf("two requests under one key left %d bids", n)
	}
}

// TestASecondOfferIs409WithItsOwnCode is "once per job" at the wire.
//
// A distinct code rather than a bare 409, because the client can act on it: the app's next screen is
// the offer the provider already has, not the form they were filling in.
func TestASecondOfferIs409WithItsOwnCode(t *testing.T) {
	w := newWire(t)

	if rec := w.bid(t, w.provider, "wire-one", validBody()); rec.Code != http.StatusCreated {
		t.Fatalf("the first offer = %d (%s)", rec.Code, rec.Body)
	}

	rec := w.bid(t, w.provider, "wire-two", validBody())
	if rec.Code != http.StatusConflict {
		t.Fatalf("a second offer = %d, want 409 (%s)", rec.Code, rec.Body)
	}
	if got := decode[errorEnvelope](t, rec).Error.Code; got != string(CodeAlreadyBid) {
		t.Errorf("the code is %q, want %q", got, CodeAlreadyBid)
	}
}

// --- privacy rule one: the customer's budget ---------------------------------------------------

// providerBidKeys is every key the provider's view of their own bid may carry.
//
// **A closed list, and the point of the exercise.** Docs/01 §4.3 forbids the customer's budget
// reaching a provider "as an amount, a band, or a 'budget supplied' flag", and a deny-list of names
// cannot express that: `max_price` is a budget and does not contain the word. This is the other
// direction — anything not promised is refused — so a field added to this shape has to be added here
// too, which makes it a decision somebody records rather than one that arrives with a schema change.
//
// It is the same list as the `Bid` schema in contracts/paths/bidding.yaml, which is
// `additionalProperties: false` for the same reason, and the same list
// scripts/verify/61-bidding.sh asserts from outside Go.
var providerBidKeys = map[string]bool{
	"id":           true,
	"job_id":       true,
	"status":       true,
	"amount_cents": true,
	"pickup_at":    true,
	"deliver_by":   true,
	"message":      true,
	"created_at":   true,
	"updated_at":   true,
}

// TestTheBidResponseCarriesNothingOfTheCustomers is SHIP-83's proof applied to the first
// provider-facing shape outside `fleet`.
//
// It does four things a struct-field check cannot, and each was chosen against a specific way the
// weaker version would pass while the invariant was broken:
//
//  1. **It obtains the response the way a provider obtains it** — an HTTP request through the real
//     handler, mounted on the pattern cmd/api serves, answered as bytes. A field can be absent from a
//     struct and present on the wire through an embedded type, a custom MarshalJSON, or a map.
//  2. **It works on the raw bytes**, so a key that is present and null still fails. Decoding into a
//     Go type turns `"budget_cents": null` into a zero value indistinguishable from a field never
//     sent — and a present-but-null key *is* the "budget supplied" flag Docs/01 §4.3 forbids.
//  3. **It asserts a closed set of keys rather than searching for the word.** The one that matters
//     most.
//  4. **It checks the value, not only the name**, with identifiers stripped first — a UUID is
//     hexadecimal, so a run of digits can occur inside one by chance.
//
// **And it refuses to run against a job with no budget.** The first thing it does is read
// `jobs.budget` back out of the row. A privacy test whose fixture has nothing to leak passes forever
// and proves nothing, which is the specific way this could rot without anybody noticing.
//
// Both responses are covered — the 201 and the 200 replay — because a shape that is safe on one and
// not the other is the failure two code paths invite.
func TestTheBidResponseCarriesNothingOfTheCustomers(t *testing.T) {
	w := newWire(t)

	// The fixture, verified rather than assumed. 4321.99 renders as 4321.99, 4321 and 432199 in
	// cents, and appears nowhere else in the job — not in a dimension, a postcode or a timestamp.
	const budget = 4321.99

	var stored *float64
	if err := w.pool.QueryRow(t.Context(),
		`SELECT budget FROM jobs WHERE id = $1`, w.job).Scan(&stored); err != nil {
		t.Fatalf("reading the stored budget: %v", err)
	}
	if stored == nil || *stored != budget {
		t.Fatalf("the job's budget is %v, want %v — this test asserts nothing against a job "+
			"that has no budget to leak", stored, budget)
	}

	responses := map[string][]byte{}

	created := w.bid(t, w.provider, "wire-privacy", validBody())
	if created.Code != http.StatusCreated {
		t.Fatalf("placing the offer = %d (%s)", created.Code, created.Body)
	}
	responses["the offer, 201"] = created.Body.Bytes()

	replayed := w.bid(t, w.provider, "wire-privacy", validBody())
	if replayed.Code != http.StatusOK {
		t.Fatalf("the replay = %d (%s)", replayed.Code, replayed.Body)
	}
	responses["the replay, 200"] = replayed.Body.Bytes()

	for how, body := range responses {
		t.Run(how, func(t *testing.T) {
			// Not vacuous: the response has to be carrying a bid before "it carries no budget"
			// means anything at all.
			if !strings.Contains(string(body), `"amount_cents"`) {
				t.Fatalf("this response carries no bid, so it proves nothing: %s", body)
			}

			// 1. Every key in the document, at every depth, is one this API promised a provider.
			var document any
			if err := json.Unmarshal(body, &document); err != nil {
				t.Fatalf("the response is not JSON: %v (%s)", err, body)
			}
			walkKeys(document, func(path, key string) {
				if !providerBidKeys[key] {
					t.Errorf("the provider's response carries %q (at %s).\n"+
						"  Every key a provider may see is in providerBidKeys, and this is not "+
						"one of them. If it is the customer's budget under another name, it is a "+
						"defect: Docs/01 §4.3 forbids it as an amount, a band, or a \"budget "+
						"supplied\" flag. If it is a genuinely new field, add it to that list and "+
						"to the Bid schema deliberately.", key, path)
				}
			})

			// 2. No key named for the budget in any spelling, including one present and null.
			if strings.Contains(strings.ToLower(string(body)), "budget") {
				t.Errorf("the word \"budget\" appears in a provider's response: %s", body)
			}

			// 3. Not the value either, in any rendering a JSON encoder could produce.
			searchable := identifier.ReplaceAllString(string(body), "<id>")
			for _, rendering := range []string{"4321.99", "432199", "4321,99", "4,321.99"} {
				if strings.Contains(searchable, rendering) {
					t.Errorf("the customer's budget appears in a provider's response as %q: %s",
						rendering, body)
				}
			}
		})
	}
}

// identifier matches a UUID as it appears in a JSON document.
var identifier = regexp.MustCompile(`[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}`)

// walkKeys visits every object key in a decoded JSON document, with the path that reached it.
func walkKeys(node any, visit func(path, key string)) {
	var walk func(node any, path string)
	walk = func(node any, path string) {
		switch value := node.(type) {
		case map[string]any:
			for key, child := range value {
				visit(path, key)
				walk(child, path+"."+key)
			}
		case []any:
			for i, child := range value {
				walk(child, fmt.Sprintf("%s[%d]", path, i))
			}
		}
	}
	walk(node, "$")
}

// TestNothingOfTheJobTravelsInABid is the structural half of the same rule.
//
// The budget is kept out of this shape by there being no job in it at all, which is stronger than
// redaction and worth pinning: a later ticket that "helpfully" copied the pickup suburb into a bid
// response would be opening the door the closed key set is holding shut, and this says so in terms a
// reader of that ticket would understand.
func TestNothingOfTheJobTravelsInABid(t *testing.T) {
	w := newWire(t)

	rec := w.bid(t, w.provider, "wire-nojob", validBody())
	if rec.Code != http.StatusCreated {
		t.Fatalf("placing the offer = %d (%s)", rec.Code, rec.Body)
	}

	body := rec.Body.String()
	for what, disclosure := range map[string]string{
		"the pickup street line": "Church Street",
		"the pickup suburb":      "Richmond",
		"the goods description":  "sofa",
		"the weight":             `"weight`,
	} {
		if strings.Contains(body, disclosure) {
			t.Errorf("a bid carries %s (%q). A bid names the job it is against and nothing else: "+
				"GET /v1/jobs/open/{id} is where a provider reads the job, and that shape is "+
				"tested once (SHIP-83) rather than twice.", what, disclosure)
		}
	}
}

// --- privacy rule two: one provider's bid is not another's -------------------------------------

// TestOneProvidersKeyCannotReachAnothersBid is Docs/01 §4.3's second line: "treat provider bid price
// as private from competing providers".
//
// **Its failure mode is a WHERE clause, not a response field**, which is why it needs a test of its
// own rather than being covered by the closed key set. A store read scoped by job and idempotency key
// but not by provider would hand the second provider the first's offer — including its price — and
// every key in that response would be one this API promises a provider, so the budget test would pass
// on it.
//
// The two providers deliberately use **the same key**. Clients generate their own; two colliding is
// unlikely and is not something this API may rely on, and reusing one here is how a competitor would
// probe for exactly this defect.
func TestOneProvidersKeyCannotReachAnothersBid(t *testing.T) {
	w := newWire(t)

	const shared = "a-key-both-clients-generated"

	// The first provider bids a distinctive amount, so the second's response can be searched for it.
	mine := w.bid(t, w.provider, shared, `{
		"amount_cents": 777701,
		"pickup_at": "2026-08-15T09:00:00Z",
		"deliver_by": "2026-08-15T17:00:00Z"
	}`)
	if mine.Code != http.StatusCreated {
		t.Fatalf("the first provider's offer = %d (%s)", mine.Code, mine.Body)
	}
	first := decode[map[string]any](t, mine)

	competitor := newVerifiedProvider(t, w.pool, "bid-competitor@example.com", "+61400000844")
	declare(t, w.pool, competitor, "VIC")
	addVehicle(t, w.pool, competitor, "BID003")

	theirs := w.bid(t, competitor, shared, validBody())
	if theirs.Code != http.StatusCreated {
		t.Fatalf("the competitor's offer = %d, want 201 — a key of theirs must not be refused by "+
			"somebody else's bid (%s)", theirs.Code, theirs.Body)
	}

	second := decode[map[string]any](t, theirs)
	if second["id"] == first["id"] {
		t.Fatal("the competitor was handed the first provider's bid.\n" +
			"  The store read must be scoped by provider as well as by job and key — see " +
			"uq_bids_idempotency and postgresStore.bidPlacedUnder.")
	}
	if strings.Contains(theirs.Body.String(), "777701") {
		t.Errorf("the competitor's response carries the first provider's price: %s", theirs.Body)
	}
	if second["amount_cents"] != float64(45000) {
		t.Errorf("the competitor's own amount is %v, want their own 45000", second["amount_cents"])
	}
}

// --- who may reach the endpoint at all ------------------------------------------------------------

// TestACustomerIsRefusedRatherThanHidden is CLAUDE.md's "no authorisation decision on the device".
//
// The app hides the bid form from a customer; the platform refuses it. Both customers are tried —
// the job's own owner, who has the strongest claim to be allowed, and a stranger — and both get the
// answer a job that does not exist gets.
//
// **Note that the token says `customer` in every request in this file**, including the ones that
// succeed. The role claim is evidence about the token; the platform reads `users.role` and four
// other facts through the eligibility filter. A test that carried the "right" role would be proving
// something about its own fixture.
func TestACustomerIsRefusedRatherThanHidden(t *testing.T) {
	w := newWire(t)

	stranger := newCustomer(t, w.pool, "bid-stranger@example.com", "+61400000845")

	for name, caller := range map[string]uuid.UUID{
		"the job's own customer": w.customer,
		"another customer":       stranger,
	} {
		t.Run(name, func(t *testing.T) {
			rec := w.bid(t, caller, "wire-"+strings.ReplaceAll(name, " ", "-"), validBody())
			if rec.Code != http.StatusNotFound {
				t.Fatalf("%s bid: %d, want 404 (%s)", name, rec.Code, rec.Body)
			}
			if got := decode[errorEnvelope](t, rec).Error.Code; got != string(httpx.CodeNotFound) {
				t.Errorf("the code is %q, want not_found", got)
			}
		})
	}

	if n := w.bids(t, w.customer, w.job); n != 0 {
		t.Error("a customer's refused bid left a row behind")
	}
}

// TestARefusedJobIsIndistinguishableFromOneThatIsNotThere is the disclosure decision, asserted on the
// bytes rather than on the status code.
//
// A refusal that explained itself would disclose what the status code is withholding — that the job
// exists, and by implication what became of it. `GET /v1/jobs/open/{id}` answers the same way for the
// same reason (SHIP-83), so a provider gets one consistent answer whichever endpoint they reach.
func TestARefusedJobIsIndistinguishableFromOneThatIsNotThere(t *testing.T) {
	w := newWire(t)

	// A job that exists and has been awarded to somebody else.
	transition(t, w.pool, w.job, w.customer, "Open", "Awarded")
	taken := w.bid(t, w.provider, "wire-taken", validBody())

	// A job that does not exist.
	missing := as(t, w.router, w.provider, "wire-missing",
		"/v1/jobs/00000000-0000-7000-8000-000000000084/bids", validBody())

	if taken.Code != http.StatusNotFound || missing.Code != http.StatusNotFound {
		t.Fatalf("statuses are %d and %d, want 404 and 404", taken.Code, missing.Code)
	}

	a, b := decode[errorEnvelope](t, taken).Error, decode[errorEnvelope](t, missing).Error
	if a.Code != b.Code || a.Message != b.Message {
		t.Errorf("a job awarded to somebody else answers differently from one that is not there:\n"+
			"  taken:   %s / %s\n  missing: %s / %s", a.Code, a.Message, b.Code, b.Message)
	}
}

// --- the request shape -----------------------------------------------------------------------------

// TestStatusIsNotASettableField is the invariant, checked at the only layer a client can reach.
//
// httpx.DecodeJSON refuses unknown fields, so a client that sends one is told it does not exist
// rather than having it silently ignored. That matters more here than usual: a provider that
// believed it had accepted its own bid would show a won job that nobody awarded.
func TestStatusIsNotASettableField(t *testing.T) {
	w := newWire(t)

	for _, field := range []string{`"status":"accepted"`, `"provider_id":"x"`, `"job_id":"x"`} {
		rec := w.bid(t, w.provider, "wire-"+field, `{
			"amount_cents": 45000,
			"pickup_at": "2026-08-15T09:00:00Z",
			"deliver_by": "2026-08-15T17:00:00Z",
			`+field+`}`)
		if rec.Code != http.StatusBadRequest {
			t.Errorf("a body carrying %s = %d, want 400 (%s)", field, rec.Code, rec.Body)
		}
	}

	if n := w.bids(t, w.provider, w.job); n != 0 {
		t.Error("a refused body wrote a bid")
	}
}

// TestABadTimestampNamesTheFieldRatherThanTheBody is why the two instants are strings on the request.
//
// encoding/json reports a bad timestamp as an ordinary error with no type of its own, so a
// time.Time field would make httpx.DecodeJSON answer "the request body is not valid JSON" — untrue
// and unactionable when the body is perfectly good JSON containing "next tuesday".
func TestABadTimestampNamesTheFieldRatherThanTheBody(t *testing.T) {
	w := newWire(t)

	rec := w.bid(t, w.provider, "wire-badtime", `{
		"amount_cents": 45000,
		"pickup_at": "next tuesday",
		"deliver_by": "2026-08-15T17:00:00Z"
	}`)
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("a bad timestamp = %d, want 422 (%s)", rec.Code, rec.Body)
	}

	env := decode[errorEnvelope](t, rec)
	if env.Error.Code != string(httpx.CodeValidationFailed) {
		t.Errorf("the code is %q, want validation_failed", env.Error.Code)
	}

	var named bool
	for _, d := range env.Error.Details {
		if d.Field == "pickup_at" {
			named = true
			if d.Code != "invalid_format" {
				t.Errorf("pickup_at is reported as %q, want invalid_format", d.Code)
			}
		}
		if d.Field == "deliver_by" {
			t.Errorf("deliver_by was reported too, and it parsed fine: %+v", env.Error.Details)
		}
	}
	if !named {
		t.Errorf("no detail names pickup_at: %+v", env.Error.Details)
	}
}

// TestAParseFailureAndAValueFailureAreReportedTogether is the collection rule at the wire.
//
// A provider who mistypes a date and an amount should be told about both at once rather than fixing
// one and being sent back for the other. The subtlety is that the field which failed to *parse* must
// be reported once, not twice: the domain sees an unreadable timestamp as an absent one and would
// otherwise add "say when you can collect" beside "that is not a date".
func TestAParseFailureAndAValueFailureAreReportedTogether(t *testing.T) {
	w := newWire(t)

	rec := w.bid(t, w.provider, "wire-both", `{
		"amount_cents": 0,
		"pickup_at": "whenever",
		"deliver_by": "2026-08-15T17:00:00Z"
	}`)
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("= %d, want 422 (%s)", rec.Code, rec.Body)
	}

	seen := map[string]int{}
	for _, d := range decode[errorEnvelope](t, rec).Error.Details {
		seen[d.Field]++
	}
	if seen["amount_cents"] != 1 {
		t.Errorf("amount_cents is reported %d times, want once — both problems belong in one answer",
			seen["amount_cents"])
	}
	if seen["pickup_at"] != 1 {
		t.Errorf("pickup_at is reported %d times, want exactly once", seen["pickup_at"])
	}
}

// TestAnOfferWithNoIdempotencyKeyIsRefusedByTheDomainToo covers the path SHIP-15's middleware makes
// unreachable through the served router.
//
// The key is a column on the row here, not merely how a retry is absorbed, so the domain refuses a
// request without one rather than trusting that nothing will ever reach it that way. It answers with
// the same code and the same status the middleware uses, so the two paths cannot tell a client two
// different things about one condition.
func TestAnOfferWithNoIdempotencyKeyIsRefusedByTheDomainToo(t *testing.T) {
	w := newWire(t)

	rec := w.bid(t, w.provider, "", validBody())
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("no key = %d, want 400 (%s)", rec.Code, rec.Body)
	}
	if got := decode[errorEnvelope](t, rec).Error.Code; got != string(httpx.CodeIdempotencyKeyRequired) {
		t.Errorf("the code is %q, want idempotency_key_required", got)
	}
}

// TestTheJobIDInThePathMustBeAnIdentifier keeps a malformed path out of the eligibility filter.
//
// bad_request rather than not_found, which is the httpx registry's own description of that code. It
// discloses nothing either: the answer is the same whether or not any job exists.
func TestTheJobIDInThePathMustBeAnIdentifier(t *testing.T) {
	w := newWire(t)

	rec := as(t, w.router, w.provider, "wire-badid", "/v1/jobs/not-a-uuid/bids", validBody())
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("= %d, want 400 (%s)", rec.Code, rec.Body)
	}
}
