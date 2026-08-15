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

// The wire contract of SHIP-84, SHIP-85 and SHIP-86.
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
//
// SHIP-85 and SHIP-86 give the second rule a second failure mode, and
// [TestAnotherProvidersBidIsUnreachable] is for that one. Their endpoints reach a bid by *naming* it
// rather than by looking one up, so what would break the rule there is a missing ownership comparison
// rather than a missing scope — the same rule, a different line of code, and therefore a different
// test.

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
	mux.Handle("PATCH /v1/jobs/{id}/bids/{bid_id}", handler.Revise())
	mux.Handle("POST /v1/jobs/{id}/bids/{bid_id}/withdraw", handler.Withdraw())
	mux.Handle("POST /v1/jobs/{id}/bids/{bid_id}/counter", handler.Counter())
	mux.Handle("GET /v1/jobs/{id}/bids/{bid_id}/history", handler.History())
	mux.Handle("POST /v1/jobs/{id}/award", handler.Award())
	mux.Handle("GET /v1/fleet/bids", handler.Mine())

	// **Five segments, and the shape was a finding rather than a preference (SHIP-102a).** The
	// intended `GET /v1/jobs/{id}/bids` could not be registered beside `GET /v1/jobs/open/{id}`:
	// both matched `/v1/jobs/open/bids` with neither more specific, and Go's ServeMux panics rather
	// than choosing. SHIP-83a moved that feed to `/v1/fleet/jobs/{id}` and the four-segment space is
	// free again, but this path is published and stays where it is (`Docs/06` §5.3). This mux
	// carries no feed route at all, so it would accept either — the pattern here is the one cmd/api
	// actually serves, which is the whole reason this file mounts a real mux rather than calling
	// handlers directly.
	mux.Handle("GET /v1/jobs/{id}/bids/received", handler.Received())
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
	return send(t, h, http.MethodPost, caller, key, target, body)
}

// send is [as] with the method named, for SHIP-85's PATCH.
//
// The two are one function with a default rather than two, because every request in this file has to
// carry the same subject and the same header handling — and a second copy is where one of them
// eventually stops.
func send(t *testing.T, h http.Handler, method string, caller uuid.UUID, key, target, body string) *httptest.ResponseRecorder {
	t.Helper()

	var reader io.Reader
	if body != "" {
		reader = strings.NewReader(body)
	}

	req := httptest.NewRequest(method, target, reader)
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

// revise sends one PATCH against a bid addressed under a job (SHIP-85).
//
// The job is a parameter rather than the fixture's, because "the bid is on the job in the path" is
// itself a rule under test — a helper that always supplied the right one could not exercise it.
func (w wire) revise(t *testing.T, caller uuid.UUID, job, bid uuid.UUID, key, body string) *httptest.ResponseRecorder {
	t.Helper()
	return send(t, w.router, http.MethodPatch, caller, key,
		"/v1/jobs/"+job.String()+"/bids/"+bid.String(), body)
}

// withdraw sends one POST against a bid's withdraw verb (SHIP-86).
func (w wire) withdraw(t *testing.T, caller uuid.UUID, job, bid uuid.UUID, key string) *httptest.ResponseRecorder {
	t.Helper()
	return as(t, w.router, caller, key,
		"/v1/jobs/"+job.String()+"/bids/"+bid.String()+"/withdraw", `{}`)
}

// counter sends one POST against a bid's counter verb (SHIP-87).
//
// The caller is a parameter and is often the *customer*, which is what makes this the first helper in
// this file that is not a provider acting on their own bid.
func (w wire) counter(t *testing.T, caller uuid.UUID, job, bid uuid.UUID, key, body string) *httptest.ResponseRecorder {
	t.Helper()
	return as(t, w.router, caller, key,
		"/v1/jobs/"+job.String()+"/bids/"+bid.String()+"/counter", body)
}

// history sends one GET against a bid's history (SHIP-88).
//
// No idempotency key: the route is read-only, the middleware lets safe methods through untouched, and
// a key on a request that changes nothing would be a key stored for no reason.
func (w wire) history(t *testing.T, caller uuid.UUID, job, bid uuid.UUID) *httptest.ResponseRecorder {
	t.Helper()
	return send(t, w.router, http.MethodGet, caller, "",
		"/v1/jobs/"+job.String()+"/bids/"+bid.String()+"/history", "")
}

// mine sends one GET against the caller's own bid list (SHIP-101a).
//
// No idempotency key and no job in the path: the resource is the caller's own bids across every job,
// which is what puts it under `/v1/fleet` rather than under any one job.
func (w wire) mine(t *testing.T, caller uuid.UUID, query string) *httptest.ResponseRecorder {
	t.Helper()
	return send(t, w.router, http.MethodGet, caller, "", "/v1/fleet/bids"+query, "")
}

// award sends one POST against a job's award verb (SHIP-92).
//
// **The bid travels in the body**, which is the one request in this file where the subject of the act
// is not in the URL — see [awardRequest] for why. The body is a parameter rather than built from the
// bid, because what a client may send in it is itself under test.
func (w wire) award(t *testing.T, caller uuid.UUID, job uuid.UUID, key, body string) *httptest.ResponseRecorder {
	t.Helper()
	return as(t, w.router, caller, key, "/v1/jobs/"+job.String()+"/award", body)
}

// awarding is the ordinary award body: the offer, named.
func awarding(bid uuid.UUID) string { return fmt.Sprintf(`{"bid_id": %q}`, bid) }

// placed is one offer on the fixture job, over the wire, with its identifier read back out.
//
// Every test below starts from a bid that exists, and reading the id out of the response rather than
// out of the database is deliberate: it is the identifier a client would actually hold.
func (w wire) placed(t *testing.T, key string) uuid.UUID {
	t.Helper()

	rec := w.bid(t, w.provider, key, validBody())
	if rec.Code != http.StatusCreated {
		t.Fatalf("placing the offer = %d (%s)", rec.Code, rec.Body)
	}

	id, err := uuid.Parse(decode[map[string]any](t, rec)["id"].(string))
	if err != nil {
		t.Fatalf("the placed bid has no usable id: %v (%s)", err, rec.Body)
	}
	return id
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
// **SHIP-87 and SHIP-88 add two keys, and the mechanism worked exactly as it was built to.** Adding
// `offered_by` and `superseded_by` to the response made all four existing subtests fail before a line
// of the new tests had been written, which is what a closed set is for: the field arrived as a
// deliberate entry here, in the contract, and in the verify script, rather than as a schema change
// nobody read.
//
// The chain envelope's own two keys are here too, since it is a response carrying bids and is held to
// the same set at every depth.
var providerBidKeys = map[string]bool{
	"id":            true,
	"job_id":        true,
	"status":        true,
	"offered_by":    true,
	"amount_cents":  true,
	"pickup_at":     true,
	"deliver_by":    true,
	"message":       true,
	"superseded_by": true,
	"created_at":    true,
	"updated_at":    true,

	// The collection envelope of Docs/10 §4.5, which the history endpoint answers with.
	"data":        true,
	"next_cursor": true,
	"has_more":    true,
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
// **Every response that carries a bid is covered**, which at SHIP-84 was the 201 and the 200 replay
// and is now four: SHIP-85's revision and SHIP-86's withdrawal answer with the same shape from two
// more code paths. A shape that is safe on one path and not another is exactly the failure several
// paths invite, and the list below is what stops a fifth being added without being added here.
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

	bidID, err := uuid.Parse(decode[map[string]any](t, created)["id"].(string))
	if err != nil {
		t.Fatalf("the placed bid has no usable id: %v", err)
	}

	revised := w.revise(t, w.provider, w.job, bidID, "wire-privacy-revise", `{"amount_cents": 41000}`)
	if revised.Code != http.StatusOK {
		t.Fatalf("the revision = %d (%s)", revised.Code, revised.Body)
	}
	responses["the revision, 200"] = revised.Body.Bytes()

	withdrawn := w.withdraw(t, w.provider, w.job, bidID, "wire-privacy-withdraw")
	if withdrawn.Code != http.StatusOK {
		t.Fatalf("the withdrawal = %d (%s)", withdrawn.Code, withdrawn.Body)
	}
	responses["the withdrawal, 200"] = withdrawn.Body.Bytes()

	// **SHIP-87 and SHIP-88 are the hardest two cases in this test and the reason it grew.** A
	// counter-offer is an amount the *customer* chose, and the history is the response that carries it
	// to a provider — so a budget leaking through either would be doing so on the one path where an
	// amount from the customer's side is legitimately present, which is exactly where a weaker test
	// would stop looking. The counter is placed at 40000 cents, which is not the budget in any
	// rendering.
	replaced := w.bid(t, w.provider, "wire-privacy-replace", validBody())
	if replaced.Code != http.StatusCreated {
		t.Fatalf("bidding again after the withdrawal = %d (%s)", replaced.Code, replaced.Body)
	}
	replacedID, err := uuid.Parse(decode[map[string]any](t, replaced)["id"].(string))
	if err != nil {
		t.Fatalf("the replacement bid has no usable id: %v", err)
	}

	countered := w.counter(t, w.customer, w.job, replacedID, "wire-privacy-counter",
		`{"amount_cents": 40000}`)
	if countered.Code != http.StatusCreated {
		t.Fatalf("the customer's counter = %d (%s)", countered.Code, countered.Body)
	}
	responses["the customer's counter, 201"] = countered.Body.Bytes()

	// Read by the **provider**, deliberately. The customer reading their own job's negotiation could
	// not leak anything to a competitor; the provider reading a chain that holds the customer's
	// counters is the direction Docs/01 §4.3 is about.
	chain := w.history(t, w.provider, w.job, replacedID)
	if chain.Code != http.StatusOK {
		t.Fatalf("the history = %d (%s)", chain.Code, chain.Body)
	}
	responses["the negotiation's history, 200"] = chain.Body.Bytes()

	// **SHIP-92's award is the seventh and last, and it is the response most likely to grow a
	// field.** The award is where the customer's side and the provider's side meet, so it is the
	// shape a later ticket is most tempted to widen — with the number the two agreed on, or with the
	// job it just moved. Neither may arrive without being added to this list first.
	//
	// The provider has to counter back before there is anything awardable, because the head of the
	// chain is the customer's own offer and `ck_bids_only_a_providers_offer_is_accepted` refuses that
	// — which is exactly the sequence a real negotiation ends with. It is placed at 42000, which is
	// not the budget in any rendering.
	counterID := uuid.MustParse(decode[map[string]any](t, countered)["id"].(string))
	agreed := w.counter(t, w.provider, w.job, counterID, "wire-privacy-agree",
		`{"amount_cents": 42000}`)
	if agreed.Code != http.StatusCreated {
		t.Fatalf("the provider's counter = %d (%s)", agreed.Code, agreed.Body)
	}
	agreedID := uuid.MustParse(decode[map[string]any](t, agreed)["id"].(string))

	awarded := w.award(t, w.customer, w.job, "wire-privacy-award", awarding(agreedID))
	if awarded.Code != http.StatusOK {
		t.Fatalf("the award = %d (%s)", awarded.Code, awarded.Body)
	}
	responses["the award, 200"] = awarded.Body.Bytes()

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
				"GET /v1/fleet/jobs/{id} is where a provider reads the job, and that shape is "+
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
// exists, and by implication what became of it. `GET /v1/fleet/jobs/{id}` answers the same way for the
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

// --- SHIP-85 and SHIP-86 at the wire ---------------------------------------------------------------

// TestRevisingABidOverTheWire is SHIP-85's *Done when* at the wire.
//
// 200 and the same shape a placement answers with, so a client that has just revised holds exactly the
// object it held before — same identifier, same status, new number.
func TestRevisingABidOverTheWire(t *testing.T) {
	w := newWire(t)
	bid := w.placed(t, "wire-revise-place")

	rec := w.revise(t, w.provider, w.job, bid, "wire-revise", `{
		"amount_cents": 39900,
		"message": "Two people and a tail lift."
	}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("PATCH = %d, want 200 (%s)", rec.Code, rec.Body)
	}

	body := decode[map[string]any](t, rec)
	if body["id"] != bid.String() {
		t.Errorf("the revision answered with %v, want the same bid %s — a revision is not a new offer",
			body["id"], bid)
	}
	if body["amount_cents"] != float64(39900) {
		t.Errorf("amount_cents is %v, want 39900", body["amount_cents"])
	}
	if body["status"] != "submitted" {
		t.Errorf("status is %v, want submitted — a revised offer is still live", body["status"])
	}
	if body["message"] != "Two people and a tail lift." {
		t.Errorf("message is %v", body["message"])
	}
}

// TestWithdrawingABidOverTheWire is SHIP-86's *Done when* at the wire, including the exact string.
//
// `withdrawn` is the lower snake case wire form of Docs/02 §4's `Withdrawn`, and it is a published
// string a client is already branching on — TestTheWireFormsAreStableAndDistinct is what stops it
// being renamed, and this is what stops the endpoint answering with something else entirely.
func TestWithdrawingABidOverTheWire(t *testing.T) {
	w := newWire(t)
	bid := w.placed(t, "wire-withdraw-place")

	rec := w.withdraw(t, w.provider, w.job, bid, "wire-withdraw")
	if rec.Code != http.StatusOK {
		t.Fatalf("POST withdraw = %d, want 200 (%s)", rec.Code, rec.Body)
	}

	body := decode[map[string]any](t, rec)
	if body["status"] != "withdrawn" {
		t.Errorf("status is %v, want withdrawn", body["status"])
	}
	if body["id"] != bid.String() {
		t.Errorf("the withdrawal answered with %v, want %s", body["id"], bid)
	}
	if body["amount_cents"] != float64(45000) {
		t.Errorf("the withdrawal changed the recorded price to %v — the row survives as record",
			body["amount_cents"])
	}
}

// TestARetriedWithdrawalUnderAFreshKeyIsStillTwoHundred is the retry this endpoint has to survive.
//
// The idempotency middleware is not in front of this handler here, which is the point: what is being
// exercised is the case the middleware cannot cover — a phone that lost its connection, was restarted,
// and generated a *new* key for the same intent. A 409 there would tell a provider their withdrawal
// failed when it succeeded.
func TestARetriedWithdrawalUnderAFreshKeyIsStillTwoHundred(t *testing.T) {
	w := newWire(t)
	bid := w.placed(t, "wire-withdraw-twice-place")

	first := w.withdraw(t, w.provider, w.job, bid, "wire-withdraw-1")
	if first.Code != http.StatusOK {
		t.Fatalf("the first withdrawal = %d (%s)", first.Code, first.Body)
	}

	again := w.withdraw(t, w.provider, w.job, bid, "wire-withdraw-2")
	if again.Code != http.StatusOK {
		t.Fatalf("a second withdrawal under a fresh key = %d, want 200 (%s)", again.Code, again.Body)
	}
	if again.Body.String() != first.Body.String() {
		t.Errorf("the second withdrawal answered differently:\n  first: %s\n  again: %s",
			first.Body, again.Body)
	}
}

// TestAnotherProvidersBidIsUnreachable is Docs/01 §4.3's second privacy rule on the two endpoints that
// take a bid identifier.
//
// SHIP-84's version of this rule was about a *lookup* — a store read scoped by job and key but not by
// provider. These two endpoints are reached by naming a bid outright, so the same rule has a second
// failure mode: an ownership comparison that is missing or is folded into a `WHERE` that quietly
// matches nothing useful.
//
// **The competitor is a real, eligible provider with a bid of their own**, so nothing about them is
// what makes the refusal happen. And the answer is byte-identical to a bid that does not exist,
// because "that is not yours" would confirm a competitor's offer to anybody willing to try
// identifiers.
func TestAnotherProvidersBidIsUnreachable(t *testing.T) {
	w := newWire(t)
	mine := w.placed(t, "wire-owned")

	competitor := newVerifiedProvider(t, w.pool, "bid-wire-rival@example.com", "+61400000852")
	declare(t, w.pool, competitor, "VIC")
	addVehicle(t, w.pool, competitor, "BID012")

	missing := uuid.Must(uuid.NewV7())

	for name, rec := range map[string]*httptest.ResponseRecorder{
		"revising somebody else's bid":    w.revise(t, competitor, w.job, mine, "wire-x1", `{"amount_cents": 1}`),
		"withdrawing somebody else's bid": w.withdraw(t, competitor, w.job, mine, "wire-x2"),
		"revising a bid that is not there": w.revise(t, competitor, w.job, missing, "wire-x3",
			`{"amount_cents": 1}`),
		"withdrawing a bid that is not there": w.withdraw(t, competitor, w.job, missing, "wire-x4"),
	} {
		t.Run(name, func(t *testing.T) {
			if rec.Code != http.StatusNotFound {
				t.Fatalf("%s = %d, want 404 (%s)", name, rec.Code, rec.Body)
			}
			if got := decode[errorEnvelope](t, rec).Error.Code; got != string(httpx.CodeNotFound) {
				t.Errorf("the code is %q, want not_found", got)
			}
		})
	}

	// Byte-identical, asserted rather than assumed: a refusal that explained itself would disclose
	// what the status code is withholding.
	taken := w.revise(t, competitor, w.job, mine, "wire-x5", `{"amount_cents": 1}`)
	nothing := w.revise(t, competitor, w.job, missing, "wire-x6", `{"amount_cents": 1}`)
	if taken.Body.String() != nothing.Body.String() {
		t.Errorf("somebody else's bid answers differently from one that is not there:\n"+
			"  theirs:  %s\n  missing: %s", taken.Body, nothing.Body)
	}

	// And nothing happened to the bid they were reaching for.
	still := w.revise(t, w.provider, w.job, mine, "wire-still", `{"message": "unchanged"}`)
	if still.Code != http.StatusOK {
		t.Fatalf("the owner's own revision = %d (%s)", still.Code, still.Body)
	}
	if body := decode[map[string]any](t, still); body["status"] != "submitted" ||
		body["amount_cents"] != float64(45000) {
		t.Errorf("the competitor's attempts changed the bid: %v", body)
	}
}

// TestABidIsAddressedUnderItsOwnJob is the check that stops the first half of the URL being
// decorative.
//
// The bid is real and the caller owns it; only the job it is paired with is wrong. Without the
// comparison in [Service.ownBid] this would succeed, and `/v1/jobs/{id}/bids/{bid_id}` would be one
// resource reachable at as many addresses as there are jobs — the shape where a permission check gets
// added to one address and forgotten on the rest.
func TestABidIsAddressedUnderItsOwnJob(t *testing.T) {
	w := newWire(t)
	bid := w.placed(t, "wire-wrongjob-place")

	other := w.publish(t)

	for name, rec := range map[string]*httptest.ResponseRecorder{
		"revising":    w.revise(t, w.provider, other, bid, "wire-wj1", `{"amount_cents": 1}`),
		"withdrawing": w.withdraw(t, w.provider, other, bid, "wire-wj2"),
	} {
		if rec.Code != http.StatusNotFound {
			t.Errorf("%s under the wrong job = %d, want 404 (%s)", name, rec.Code, rec.Body)
		}
	}
}

// TestAnAcceptedBidAnswersItsOwnCodeOnBothEndpoints is the refusal a client acts on rather than
// reports.
//
// An accepted offer is a job this provider has won, so the app's next screen is that job — which is
// why it is not folded into `bidding_bid_closed` with the rejected and expired ones.
func TestAnAcceptedBidAnswersItsOwnCodeOnBothEndpoints(t *testing.T) {
	w := newWire(t)
	bid := w.placed(t, "wire-accepted-place")

	// Awarding is SHIP-92's endpoint and does not exist. Unlike a job, a bid's status has no trigger
	// guarding it (000500 says why), so this is the statement the award itself will run.
	exec(t, w.pool, `UPDATE bids SET status = 'Accepted' WHERE id = $1`, bid)

	for name, rec := range map[string]*httptest.ResponseRecorder{
		"revising":    w.revise(t, w.provider, w.job, bid, "wire-acc1", `{"amount_cents": 1}`),
		"withdrawing": w.withdraw(t, w.provider, w.job, bid, "wire-acc2"),
	} {
		t.Run(name, func(t *testing.T) {
			if rec.Code != http.StatusConflict {
				t.Fatalf("%s an accepted bid = %d, want 409 (%s)", name, rec.Code, rec.Body)
			}
			if got := decode[errorEnvelope](t, rec).Error.Code; got != string(CodeBidAccepted) {
				t.Errorf("the code is %q, want %q", got, CodeBidAccepted)
			}
		})
	}
}

// TestAClosedOfferAnswersItsOwnCode is the other half of the status refusal.
//
// One code for four statuses, because the client does the same thing with all of them: the offer is
// over and what to show is the feed. Which of the four it was belongs to the provider's own bid
// history (SHIP-101), not to an error code.
func TestAClosedOfferAnswersItsOwnCode(t *testing.T) {
	w := newWire(t)
	bid := w.placed(t, "wire-closed-place")

	if rec := w.withdraw(t, w.provider, w.job, bid, "wire-closed-withdraw"); rec.Code != http.StatusOK {
		t.Fatalf("withdrawing = %d (%s)", rec.Code, rec.Body)
	}

	rec := w.revise(t, w.provider, w.job, bid, "wire-closed-revise", `{"amount_cents": 1}`)
	if rec.Code != http.StatusConflict {
		t.Fatalf("revising a withdrawn offer = %d, want 409 (%s)", rec.Code, rec.Body)
	}
	if got := decode[errorEnvelope](t, rec).Error.Code; got != string(CodeBidClosed) {
		t.Errorf("the code is %q, want %q", got, CodeBidClosed)
	}
}

// TestARevisionCannotSetTheStatus is the invariant at the layer a client can reach, on the endpoint
// most likely to tempt somebody into allowing it.
//
// A `PATCH` carrying `"status": "withdrawn"` is the obvious way to write the withdrawal endpoint, and
// it is refused: a bid's status is the platform's, and httpx.DecodeJSON reports the unknown field
// rather than ignoring it. `POST …/withdraw` exists because the client names an intent and the
// platform decides what the state becomes.
func TestARevisionCannotSetTheStatus(t *testing.T) {
	w := newWire(t)
	bid := w.placed(t, "wire-setstatus-place")

	for _, field := range []string{
		`"status":"withdrawn"`,
		`"status":"accepted"`,
		`"provider_id":"x"`,
		`"job_id":"x"`,
		`"id":"x"`,
	} {
		rec := w.revise(t, w.provider, w.job, bid, "wire-set-"+field, `{`+field+`}`)
		if rec.Code != http.StatusBadRequest {
			t.Errorf("a revision carrying %s = %d, want 400 (%s)", field, rec.Code, rec.Body)
		}
	}

	// And likewise on the verb, where a body is required but empty.
	if rec := send(t, w.router, http.MethodPost, w.provider, "wire-set-withdraw",
		"/v1/jobs/"+w.job.String()+"/bids/"+bid.String()+"/withdraw",
		`{"status":"accepted"}`); rec.Code != http.StatusBadRequest {
		t.Errorf("a withdrawal carrying a status = %d, want 400 (%s)", rec.Code, rec.Body)
	}
}

// TestARevisionThatNamesNothingIsRefusedAtTheWire keeps a client defect from arriving as a success.
func TestARevisionThatNamesNothingIsRefusedAtTheWire(t *testing.T) {
	w := newWire(t)
	bid := w.placed(t, "wire-nofields-place")

	rec := w.revise(t, w.provider, w.job, bid, "wire-nofields", `{}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("an empty revision = %d, want 400 (%s)", rec.Code, rec.Body)
	}
	if got := decode[errorEnvelope](t, rec).Error.Code; got != string(httpx.CodeBadRequest) {
		t.Errorf("the code is %q, want bad_request", got)
	}
}

// TestABadTimestampInARevisionNamesTheField is [bidRequest]'s reasoning applied to the second request
// shape: encoding/json reports a bad timestamp as an ordinary error with no type of its own, so a
// `time.Time` field would make the whole body "not valid JSON".
//
// Both timestamps are reported together, which is the half a provider notices. What is *not* attempted
// is merging these with the domain's value rules — see [reviseRequest.revision] for why that ordering
// is a security property rather than a message-quality one.
func TestABadTimestampInARevisionNamesTheField(t *testing.T) {
	w := newWire(t)
	bid := w.placed(t, "wire-badtime-place")

	rec := w.revise(t, w.provider, w.job, bid, "wire-badtime", `{
		"pickup_at": "next tuesday",
		"deliver_by": "the day after"
	}`)
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("a bad timestamp = %d, want 422 (%s)", rec.Code, rec.Body)
	}

	seen := map[string]int{}
	for _, d := range decode[errorEnvelope](t, rec).Error.Details {
		seen[d.Field]++
		if d.Code != "invalid_format" {
			t.Errorf("%s is reported as %q, want invalid_format", d.Field, d.Code)
		}
	}
	if seen["pickup_at"] != 1 || seen["deliver_by"] != 1 {
		t.Errorf("the details are %v, want each field named exactly once", seen)
	}
}

// TestTheBidIDInThePathMustBeAnIdentifier keeps a malformed path out of the store.
//
// bad_request rather than not_found, exactly as the job id is: a value that is not an identifier is
// not a bid that is missing, and the answer discloses nothing either way.
func TestTheBidIDInThePathMustBeAnIdentifier(t *testing.T) {
	w := newWire(t)

	for name, rec := range map[string]*httptest.ResponseRecorder{
		"revising": send(t, w.router, http.MethodPatch, w.provider, "wire-badbid1",
			"/v1/jobs/"+w.job.String()+"/bids/not-a-uuid", `{"amount_cents": 1}`),
		"withdrawing": send(t, w.router, http.MethodPost, w.provider, "wire-badbid2",
			"/v1/jobs/"+w.job.String()+"/bids/not-a-uuid/withdraw", `{}`),
	} {
		if rec.Code != http.StatusBadRequest {
			t.Errorf("%s with a malformed bid id = %d, want 400 (%s)", name, rec.Code, rec.Body)
		}
	}
}

// --- SHIP-87 and SHIP-88 at the wire -------------------------------------------------------------

// TestCounteringOverTheWire is SHIP-87's *Done when* at the wire, in both directions.
//
// **The customer's request is the one worth watching.** Every request in this file before it is a
// provider acting on their own bid, and this is the first that succeeds for anybody else — so it is
// also the first place a widened authorisation check would show up as a pass rather than as a
// failure.
func TestCounteringOverTheWire(t *testing.T) {
	w := newWire(t)
	placed := w.placed(t, "wire-counter-base")

	rec := w.counter(t, w.customer, w.job, placed, "wire-counter", `{"amount_cents": 40000}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("the customer's counter = %d, want 201 (%s)", rec.Code, rec.Body)
	}

	countered := decode[map[string]any](t, rec)
	if countered["id"] == placed.String() {
		t.Error("the counter answered with the offer it superseded; a counter creates a row")
	}
	if countered["offered_by"] != "customer" {
		t.Errorf("the counter is offered_by %v, want customer", countered["offered_by"])
	}
	if countered["status"] != "submitted" {
		t.Errorf("the counter is %v, want submitted", countered["status"])
	}
	if countered["amount_cents"] != float64(40000) {
		t.Errorf("the counter is %v cents, want 40000", countered["amount_cents"])
	}
	if _, present := countered["superseded_by"]; present {
		t.Error("the live head carries superseded_by; it is omitted while nothing has displaced it")
	}

	counterID := uuid.MustParse(countered["id"].(string))

	back := w.counter(t, w.provider, w.job, counterID, "wire-counter-back", `{"amount_cents": 43000}`)
	if back.Code != http.StatusCreated {
		t.Fatalf("the provider's counter = %d, want 201 (%s)", back.Code, back.Body)
	}
	if got := decode[map[string]any](t, back)["offered_by"]; got != "provider" {
		t.Errorf("the provider's counter is offered_by %v, want provider", got)
	}
}

// TestARetriedCounterIsTwoHundredWithTheOriginal is the retry contract at the wire.
//
// 201 the first time and 200 for a repeat of the same key, which is the pair [Handler.Place] answers
// with. A client that does not care which happened parses one type; one recovering from a dropped
// connection generally does not care.
func TestARetriedCounterIsTwoHundredWithTheOriginal(t *testing.T) {
	w := newWire(t)
	placed := w.placed(t, "wire-counter-retry-base")

	first := w.counter(t, w.customer, w.job, placed, "wire-counter-retry", `{"amount_cents": 40000}`)
	if first.Code != http.StatusCreated {
		t.Fatalf("the counter = %d, want 201 (%s)", first.Code, first.Body)
	}

	again := w.counter(t, w.customer, w.job, placed, "wire-counter-retry", `{"amount_cents": 40000}`)
	if again.Code != http.StatusOK {
		t.Fatalf("the retry = %d, want 200 (%s)", again.Code, again.Body)
	}
	if decode[map[string]any](t, again)["id"] != decode[map[string]any](t, first)["id"] {
		t.Error("the retry answered with a different counter")
	}
}

// TestACounterFromAStrangerIsIndistinguishableFromNoBid is Docs/01 §4.3's second line at the wire.
//
// A competing provider and an account with nothing to do with the job both get the answer a bid that
// does not exist gets, byte for byte. Whose offers exist is not something this API discloses, and a
// competitor probing identifiers must not be able to tell "that is somebody's" from "that is nobody's".
func TestACounterFromAStrangerIsIndistinguishableFromNoBid(t *testing.T) {
	w := newWire(t)
	placed := w.placed(t, "wire-counter-stranger-base")

	stranger := newCustomer(t, w.pool, "wire-counter-stranger@example.com", "+61400000878")

	refused := w.counter(t, stranger, w.job, placed, "wire-counter-stranger", `{"amount_cents": 1}`)
	if refused.Code != http.StatusNotFound {
		t.Fatalf("a stranger's counter = %d, want 404 (%s)", refused.Code, refused.Body)
	}

	missing := w.counter(t, stranger, w.job, uuid.Must(uuid.NewV7()), "wire-counter-nothing",
		`{"amount_cents": 1}`)
	if missing.Code != http.StatusNotFound {
		t.Fatalf("a counter on nothing = %d, want 404 (%s)", missing.Code, missing.Body)
	}
	if refused.Body.String() != missing.Body.String() {
		t.Errorf("somebody else's offer answers %s and a missing one answers %s; the two must be one "+
			"answer", refused.Body, missing.Body)
	}
}

// TestCounteringYourOwnOfferAnswersItsOwnCode is Docs/10 §4.4's test applied to the one code SHIP-87
// adds.
//
// The client's correct response is a **different request** — `PATCH` rather than a counter — which is
// what earns a code rather than a message. Both directions of getting it backwards land on it.
func TestCounteringYourOwnOfferAnswersItsOwnCode(t *testing.T) {
	w := newWire(t)
	placed := w.placed(t, "wire-wrongparty-base")

	own := w.counter(t, w.provider, w.job, placed, "wire-wrongparty-provider", `{"amount_cents": 44000}`)
	if own.Code != http.StatusConflict {
		t.Fatalf("a provider countering their own offer = %d, want 409 (%s)", own.Code, own.Body)
	}
	if got := decode[errorEnvelope](t, own).Error.Code; got != string(CodeWrongParty) {
		t.Errorf("the code is %q, want %q", got, CodeWrongParty)
	}

	countered := w.counter(t, w.customer, w.job, placed, "wire-wrongparty-counter", `{"amount_cents": 40000}`)
	if countered.Code != http.StatusCreated {
		t.Fatalf("the customer's counter = %d (%s)", countered.Code, countered.Body)
	}
	counterID := uuid.MustParse(decode[map[string]any](t, countered)["id"].(string))

	// And the provider cannot *revise* what the customer offered, which is the hole 000502's
	// reinterpretation of provider_id would otherwise have opened.
	revised := w.revise(t, w.provider, w.job, counterID, "wire-wrongparty-revise", `{"amount_cents": 1}`)
	if revised.Code != http.StatusConflict {
		t.Fatalf("the provider revised the customer's counter = %d, want 409 (%s)", revised.Code, revised.Body)
	}
	if got := decode[errorEnvelope](t, revised).Error.Code; got != string(CodeWrongParty) {
		t.Errorf("the revision's code is %q, want %q", got, CodeWrongParty)
	}
}

// TestACounterCannotSetTheStatusOrTheParty is the two fields a client must not be able to send.
//
// A bid's status is the platform's, and so is *which party made the offer*: a body naming the author
// would be an authorisation decision made from client input, which Docs/07 §3 puts on the platform.
// httpx.DecodeJSON refuses unknown fields, so both are reported rather than ignored.
func TestACounterCannotSetTheStatusOrTheParty(t *testing.T) {
	w := newWire(t)
	placed := w.placed(t, "wire-counter-fields-base")

	for field, body := range map[string]string{
		"status":     `{"amount_cents": 40000, "status": "accepted"}`,
		"offered_by": `{"amount_cents": 40000, "offered_by": "provider"}`,
	} {
		t.Run("a body naming "+field+" is refused", func(t *testing.T) {
			rec := w.counter(t, w.customer, w.job, placed, "wire-counter-"+field, body)
			if rec.Code != http.StatusBadRequest {
				t.Fatalf("= %d, want 400 (%s)", rec.Code, rec.Body)
			}
			if !strings.Contains(rec.Body.String(), field) {
				t.Errorf("the refusal does not name %s: %s", field, rec.Body)
			}
		})
	}
}

// TestACounterThatChangesNothingIsRefusedAtTheWire keeps a counter apart from an acceptance.
//
// `bad_request` rather than a code of its own, and the message is what carries the difference: the
// client's next screen is the award, not the form it just submitted.
func TestACounterThatChangesNothingIsRefusedAtTheWire(t *testing.T) {
	w := newWire(t)
	placed := w.placed(t, "wire-counter-empty-base")

	rec := w.counter(t, w.customer, w.job, placed, "wire-counter-empty", `{}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("an empty counter = %d, want 400 (%s)", rec.Code, rec.Body)
	}
	if got := decode[errorEnvelope](t, rec).Error.Code; got != string(httpx.CodeBadRequest) {
		t.Errorf("the code is %q, want %q", got, httpx.CodeBadRequest)
	}
}

// TestTheHistoryIsReadableByBothPartiesAndNobodyElse is SHIP-88's *Done when* at the wire, with the
// privacy rule that guards it.
//
// Docs/02 §4 keeps bid history visible to the customer and the bidding provider; a competing provider
// gets the answer a bid that does not exist gets. The administrator's view is SHIP-96's.
func TestTheHistoryIsReadableByBothPartiesAndNobodyElse(t *testing.T) {
	w := newWire(t)
	placed := w.placed(t, "wire-history-base")

	countered := w.counter(t, w.customer, w.job, placed, "wire-history-counter", `{"amount_cents": 40000}`)
	if countered.Code != http.StatusCreated {
		t.Fatalf("the counter = %d (%s)", countered.Code, countered.Body)
	}

	for who, caller := range map[string]uuid.UUID{"the customer": w.customer, "the provider": w.provider} {
		rec := w.history(t, caller, w.job, placed)
		if rec.Code != http.StatusOK {
			t.Fatalf("%s reading the history = %d (%s)", who, rec.Code, rec.Body)
		}

		var page struct {
			Data       []map[string]any `json:"data"`
			NextCursor *string          `json:"next_cursor"`
			HasMore    bool             `json:"has_more"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &page); err != nil {
			t.Fatalf("the history is not JSON: %v (%s)", err, rec.Body)
		}
		if len(page.Data) != 2 {
			t.Fatalf("%s sees %d offers, want the offer and the counter", who, len(page.Data))
		}
		if page.HasMore {
			t.Errorf("a two-entry history reports itself truncated")
		}
		if page.NextCursor != nil {
			t.Errorf("the history carries a cursor; it does not page")
		}
		if page.Data[0]["offered_by"] != "provider" || page.Data[1]["offered_by"] != "customer" {
			t.Errorf("%s sees the rounds in the wrong order or wrongly attributed: %s", who, rec.Body)
		}
		if page.Data[0]["status"] != "superseded" {
			t.Errorf("the answered offer is %v, want superseded", page.Data[0]["status"])
		}
		if page.Data[0]["superseded_by"] != page.Data[1]["id"] {
			t.Errorf("the first round points at %v, want the second, %v",
				page.Data[0]["superseded_by"], page.Data[1]["id"])
		}
	}

	rival := newVerifiedProvider(t, w.pool, "wire-history-rival@example.com", "+61400000879")
	declare(t, w.pool, rival, "VIC")
	addVehicle(t, w.pool, rival, "BID879")

	refused := w.history(t, rival, w.job, placed)
	if refused.Code != http.StatusNotFound {
		t.Fatalf("a competing provider read the negotiation: %d (%s)", refused.Code, refused.Body)
	}
}

// TestTheHistoryNeedsNoIdempotencyKey is the read-only half of SHIP-15's rule.
//
// Every *state-changing* request carries a key and is refused without one. A `GET` changes nothing, so
// a key on it would be a key stored for no reason — and an endpoint that demanded one would be telling
// a client to generate a value per read.
func TestTheHistoryNeedsNoIdempotencyKey(t *testing.T) {
	w := newWire(t)
	placed := w.placed(t, "wire-history-nokey-base")

	if rec := w.history(t, w.provider, w.job, placed); rec.Code != http.StatusOK {
		t.Fatalf("a history read with no key = %d, want 200 (%s)", rec.Code, rec.Body)
	}
}

// --- SHIP-92: the award, at the wire --------------------------------------------------------------

// TestAwardingABidOverTheWire is SHIP-92's *Done when* at the wire.
//
// `POST /v1/jobs/{id}/award` with the offer named in the body, answered 200 with the accepted bid.
// The status is read back in its wire form, which is the string a client branches on and the one
// SHIP-84's `Status.Wire` published.
func TestAwardingABidOverTheWire(t *testing.T) {
	w := newWire(t)
	placed := w.placed(t, "wire-award-base")

	rec := w.award(t, w.customer, w.job, "wire-award", awarding(placed))
	if rec.Code != http.StatusOK {
		t.Fatalf("awarding = %d, want 200 (%s)", rec.Code, rec.Body)
	}

	body := decode[map[string]any](t, rec)
	if body["id"] != placed.String() {
		t.Errorf("the award answered with %v, want the bid it accepted %s", body["id"], placed)
	}
	if body["status"] != "accepted" {
		t.Errorf("the bid came back as %v, want accepted", body["status"])
	}

	var status string
	if err := w.pool.QueryRow(t.Context(), `SELECT status FROM jobs WHERE id = $1`, w.job).Scan(&status); err != nil {
		t.Fatalf("reading the job: %v", err)
	}
	if status != "Awarded" {
		t.Errorf("the job is %s, want Awarded — the award moves the job in the same transaction", status)
	}
}

// TestAwardingIsTheCustomersAloneAtTheWire is Docs/02 §3's first control, and CLAUDE.md's "no
// authorisation decision on the device" seen from the other end.
//
// Every request in this file carries a subject claiming `role: customer`, including the provider's.
// The platform decides from `jobs.customer_id` instead, so the provider is refused with the answer a
// job that does not exist gets — and the 404 is what stops this endpoint confirming that a bid
// identifier is real.
func TestAwardingIsTheCustomersAloneAtTheWire(t *testing.T) {
	w := newWire(t)
	placed := w.placed(t, "wire-award-who-base")

	rec := w.award(t, w.provider, w.job, "wire-award-who", awarding(placed))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("the provider awarding their own bid = %d, want 404 (%s)", rec.Code, rec.Body)
	}
	if got := decode[errorEnvelope](t, rec).Error.Code; got != "not_found" {
		t.Errorf("the code is %q, want not_found", got)
	}
	if strings.Contains(rec.Body.String(), placed.String()) {
		t.Errorf("the refusal echoes the bid identifier back: %s", rec.Body)
	}
}

// TestAnAwardNamingNoBidIsRefusedByField is Docs/10 §4.6 applied to the one endpoint here whose
// subject is in the body.
//
// A missing or malformed identifier is a field error naming `bid_id`, not "the request body is not
// valid JSON" — which is what encoding/json alone could say, and which is untrue when the body is
// perfectly good JSON containing a truncated value.
func TestAnAwardNamingNoBidIsRefusedByField(t *testing.T) {
	w := newWire(t)

	for how, body := range map[string]string{
		"an empty body":             `{}`,
		"a blank identifier":        `{"bid_id": ""}`,
		"a truncated identifier":    `{"bid_id": "0198f2c1-6b40-7a11"}`,
		"something that is not one": `{"bid_id": "the cheapest one"}`,
	} {
		t.Run(how, func(t *testing.T) {
			rec := w.award(t, w.customer, w.job, "wire-award-field-"+how, body)
			if rec.Code != http.StatusUnprocessableEntity {
				t.Fatalf("= %d, want 422 (%s)", rec.Code, rec.Body)
			}

			envelope := decode[errorEnvelope](t, rec)
			if envelope.Error.Code != "validation_failed" {
				t.Fatalf("the code is %q, want validation_failed (%s)", envelope.Error.Code, rec.Body)
			}
			if len(envelope.Error.Details) != 1 || envelope.Error.Details[0].Field != "bid_id" {
				t.Errorf("the refusal does not name bid_id: %s", rec.Body)
			}
		})
	}
}

// TestAnAwardCannotSetTheStatus is the invariant at the only layer a client can reach.
//
// A bid's status is the platform's and so is a job's. httpx.DecodeJSON refuses unknown fields, so a
// client sending one is told it does not exist rather than having it quietly ignored — which is the
// failure worth preventing here above anywhere else, because a request that appeared to award a job
// and did not is one a customer acts on.
func TestAnAwardCannotSetTheStatus(t *testing.T) {
	w := newWire(t)
	placed := w.placed(t, "wire-award-status-base")

	rec := w.award(t, w.customer, w.job, "wire-award-status",
		fmt.Sprintf(`{"bid_id": %q, "status": "accepted"}`, placed))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("= %d, want 400 (%s)", rec.Code, rec.Body)
	}
	if !strings.Contains(rec.Body.String(), "status") {
		t.Errorf("the refusal does not name the field: %s", rec.Body)
	}
	if stored := w.row(t, placed); stored.status != string(StatusSubmitted) {
		t.Errorf("the refused request moved the bid to %s", stored.status)
	}
}

// TestAwardingAClosedOfferAnswersItsOwnCode is the refusal a client acts on rather than parses.
//
// `bidding_bid_closed` and not `conflict`: the *offer* is over, and the customer's next action is to
// pick another one from the same job. That is a different screen from a job that can no longer be
// awarded, which is the distinction the two codes carry.
func TestAwardingAClosedOfferAnswersItsOwnCode(t *testing.T) {
	w := newWire(t)
	placed := w.placed(t, "wire-award-closed-base")

	if rec := w.withdraw(t, w.provider, w.job, placed, "wire-award-closed-withdraw"); rec.Code != http.StatusOK {
		t.Fatalf("withdrawing = %d (%s)", rec.Code, rec.Body)
	}

	rec := w.award(t, w.customer, w.job, "wire-award-closed", awarding(placed))
	if rec.Code != http.StatusConflict {
		t.Fatalf("awarding a withdrawn offer = %d, want 409 (%s)", rec.Code, rec.Body)
	}
	if got := decode[errorEnvelope](t, rec).Error.Code; got != string(CodeBidClosed) {
		t.Errorf("the code is %q, want %q", got, CodeBidClosed)
	}
}

// TestASecondAwardOnOneJobIsAConflictAtTheWire is the protocol code doing what it was registered for.
//
// `conflict`'s description in Docs/10-api-error-codes.md has named "a second award on one job" since
// SHIP-12, before this endpoint existed. The client's next screen is the *job*, which is what
// separates it from `bidding_bid_closed` above.
//
// **SHIP-93 is what makes this test load-bearing rather than a formality.** The rival's offer is
// `Rejected` by the time the second award is sent, so an implementation that judged the offer's
// status before the job's would answer `bidding_bid_closed` here — sending the customer to offers
// the same sweep has closed, and quietly retiring a description the error registry has carried
// since SHIP-12.
func TestASecondAwardOnOneJobIsAConflictAtTheWire(t *testing.T) {
	w := newWire(t)
	mine := w.placed(t, "wire-award-second-base")

	rival := newVerifiedProvider(t, w.pool, "wire-award-rival@example.com", "+61400000924")
	declare(t, w.pool, rival, "VIC")
	addVehicle(t, w.pool, rival, "BID924")

	theirs := as(t, w.router, rival, "wire-award-second-rival",
		"/v1/jobs/"+w.job.String()+"/bids", validBody())
	if theirs.Code != http.StatusCreated {
		t.Fatalf("the rival's offer = %d (%s)", theirs.Code, theirs.Body)
	}
	theirID := uuid.MustParse(decode[map[string]any](t, theirs)["id"].(string))

	if rec := w.award(t, w.customer, w.job, "wire-award-second-first", awarding(mine)); rec.Code != http.StatusOK {
		t.Fatalf("the first award = %d (%s)", rec.Code, rec.Body)
	}

	if stored := w.row(t, theirID); stored.status != string(StatusRejected) {
		t.Fatalf("the competing offer is %s after the award, want Rejected — without the sweep this "+
			"test proves the wrong thing", stored.status)
	}

	rec := w.award(t, w.customer, w.job, "wire-award-second", awarding(theirID))
	if rec.Code != http.StatusConflict {
		t.Fatalf("a second award = %d, want 409 (%s)", rec.Code, rec.Body)
	}
	if got := decode[errorEnvelope](t, rec).Error.Code; got != "conflict" {
		t.Errorf("the code is %q, want conflict", got)
	}
}

// TestARetriedAwardUnderAFreshKeyIsStillTwoHundred is the retry this endpoint has to survive, and the
// reason it needs no key column.
//
// A phone that lost its connection, was restarted and generated a **fresh** `Idempotency-Key` for the
// same intent must not be told its award failed when it succeeded. The middleware absorbs the retry
// that reuses its key; this absorbs the one that does not — idempotency by state, which SHIP-86
// established is stronger than a stored key because two clients with two keys still cannot award one
// job twice.
func TestARetriedAwardUnderAFreshKeyIsStillTwoHundred(t *testing.T) {
	w := newWire(t)
	placed := w.placed(t, "wire-award-retry-base")

	first := w.award(t, w.customer, w.job, "wire-award-retry-one", awarding(placed))
	if first.Code != http.StatusOK {
		t.Fatalf("the first award = %d (%s)", first.Code, first.Body)
	}

	second := w.award(t, w.customer, w.job, "wire-award-retry-two", awarding(placed))
	if second.Code != http.StatusOK {
		t.Fatalf("the retry under a fresh key = %d, want 200 (%s)", second.Code, second.Body)
	}
	if first.Body.String() != second.Body.String() {
		t.Errorf("the retry answered differently:\n  first:  %s\n  second: %s", first.Body, second.Body)
	}
}

// TestAwardingABidOnAnotherJobIsNotFoundAtTheWire keeps the job in the path from being decorative.
//
// This is the endpoint where the two identifiers come from two places — the job from the URL and the
// bid from the body — so a client that pairs them wrongly is naming something that does not exist.
// Answering from the bid alone would let a customer award, against their own job, an offer somebody
// made on a different one.
func TestAwardingABidOnAnotherJobIsNotFoundAtTheWire(t *testing.T) {
	w := newWire(t)
	placed := w.placed(t, "wire-award-elsewhere-base")

	other := w.publish(t)
	rec := w.award(t, w.customer, other, "wire-award-elsewhere", awarding(placed))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("awarding a bid under the wrong job = %d, want 404 (%s)", rec.Code, rec.Body)
	}
	if got := decode[errorEnvelope](t, rec).Error.Code; got != "not_found" {
		t.Errorf("the code is %q, want not_found", got)
	}
}
