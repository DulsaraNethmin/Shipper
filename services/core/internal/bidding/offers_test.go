package bidding

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/DulsaraNethmin/Shipper/services/core/internal/clock"
	"github.com/DulsaraNethmin/Shipper/services/core/internal/db"
	"github.com/DulsaraNethmin/Shipper/services/core/internal/fleet"
)

// SHIP-102a — the customer's view of the offers on their own job.
//
// Every test here goes over the wire, through the real mux, for the reason http_test.go's header
// gives: the path parameter and the serialised shape are both part of what is under test, and a
// handler called directly sees neither.
//
// # The two clauses that needed their own tests
//
// **"A provider gets what a stranger gets"** is a clause of the *Done when* rather than a
// consequence, and [TestAProviderGetsWhatAStrangerGets] drives it in both directions — the provider
// bidding on the job and an account with no connection to it — and compares the two answers *byte
// for byte* rather than by status code. A 404 whose message differed would still disclose that the
// job exists.
//
// **"No budget in any form"** is [TestTheOfferResponseCarriesNothingItMayNot], which is
// TestTheBidResponseCarriesNothingOfTheCustomers pointed at the customer's shape and extended along
// the axis wave 9 found surviving: it asserts on *words* as well as on keys and values. A response —
// or a screen — that said "the customer has set a maximum" carries no budget field and no budget
// value, passes a closed key set and passes a source scan, and is exactly the flag Docs/01 §4.3
// forbids.

// offers sends one GET against the offers on a job (SHIP-102a).
//
// The job is a parameter rather than the fixture's, because "the job has to be yours" is itself the
// rule under test and a helper that always supplied the right one could not exercise it.
//
// No idempotency key: the route is read-only and the middleware lets safe methods through untouched.
func (w wire) offers(t *testing.T, caller, job uuid.UUID, query string) *httptest.ResponseRecorder {
	t.Helper()
	return send(t, w.router, http.MethodGet, caller, "",
		"/v1/jobs/"+job.String()+"/bids/received"+query, "")
}

// vehicleOf reads the identifier of a provider's only vehicle.
//
// [addVehicle] does not return one and its signature is shared with tests that do not want it, so
// this reads the row back instead. It fails rather than returning the zero value: a vehicle test
// whose fixture has no vehicle would otherwise assert that "no vehicle was named" and pass.
func vehicleOf(t *testing.T, pool *pgxpool.Pool, provider uuid.UUID) uuid.UUID {
	t.Helper()

	var id uuid.UUID
	if err := pool.QueryRow(t.Context(),
		`SELECT id FROM vehicles WHERE provider_id = $1 ORDER BY created_at LIMIT 1`,
		provider).Scan(&id); err != nil {
		t.Fatalf("reading %s's vehicle: %v", provider, err)
	}
	return id
}

// offerWith is [validBody] with a vehicle named.
func offerWith(vehicle uuid.UUID) string {
	return fmt.Sprintf(`{
		"amount_cents": 45000,
		"pickup_at": %q,
		"deliver_by": %q,
		"message": "Can collect from the loading dock.",
		"vehicle_id": %q
	}`,
		testInstant.Add(48*time.Hour).In(melbourne).Format(time.RFC3339),
		testInstant.Add(56*time.Hour).In(melbourne).Format(time.RFC3339),
		vehicle)
}

// secondBidder adds a verified provider serving Victoria with a van, so that a job can carry two
// competing offers.
//
// Two providers is the fixture this ticket needs and the one-provider market cannot express: a
// comparison screen compares, and the endpoint that hands a competitor every rival's price is the
// one this file has to prove is shut.
func secondBidder(t *testing.T, m market, email, phone, registration string) uuid.UUID {
	t.Helper()

	provider := newVerifiedProvider(t, m.pool, email, phone)
	declare(t, m.pool, provider, "VIC")
	addVehicle(t, m.pool, provider, registration)
	return provider
}

// TestTheCustomerSeesEveryLiveOfferOnTheirJob is the *Done when*'s first clause, end to end.
//
// Two providers offer, one of them naming a vehicle, and the customer reads both back in Docs/10
// §4.5's envelope with the price, the timing, the provider summary and the vehicle on the element
// that has one.
func TestTheCustomerSeesEveryLiveOfferOnTheirJob(t *testing.T) {
	w := newWire(t)
	second := secondBidder(t, w.market, "offers-second@example.com", "+61400000860", "BID002")
	van := vehicleOf(t, w.pool, w.provider)

	first := w.bid(t, w.provider, "offers-first", offerWith(van))
	if first.Code != http.StatusCreated {
		t.Fatalf("the first offer = %d (%s)", first.Code, first.Body)
	}
	other := as(t, w.router, second, "offers-other",
		"/v1/jobs/"+w.job.String()+"/bids", validBody())
	if other.Code != http.StatusCreated {
		t.Fatalf("the second offer = %d (%s)", other.Code, other.Body)
	}

	rec := w.offers(t, w.customer, w.job, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("listing the offers = %d (%s)", rec.Code, rec.Body)
	}

	page := decode[struct {
		Data []struct {
			ID          string `json:"id"`
			JobID       string `json:"job_id"`
			Status      string `json:"status"`
			OfferedBy   string `json:"offered_by"`
			AmountCents int64  `json:"amount_cents"`
			PickupAt    string `json:"pickup_at"`
			DeliverBy   string `json:"deliver_by"`
			Provider    struct {
				ID          string `json:"id"`
				Verified    bool   `json:"verified"`
				MemberSince string `json:"member_since"`
			} `json:"provider"`
			Vehicle *struct {
				ID       string `json:"id"`
				Type     string `json:"type"`
				Make     string `json:"make"`
				Model    string `json:"model"`
				Capacity struct {
					MaxWeightKg float64 `json:"max_weight_kg"`
					LengthCm    int     `json:"length_cm"`
					WidthCm     int     `json:"width_cm"`
					HeightCm    int     `json:"height_cm"`
				} `json:"capacity"`
			} `json:"vehicle"`
		} `json:"data"`
		NextCursor string `json:"next_cursor"`
		HasMore    bool   `json:"has_more"`
	}](t, rec)

	if len(page.Data) != 2 {
		t.Fatalf("the customer sees %d offers, want both: %s", len(page.Data), rec.Body)
	}
	if page.HasMore {
		t.Error("two offers reported another page")
	}

	byProvider := map[string]int{}
	for i, offer := range page.Data {
		byProvider[offer.Provider.ID] = i

		// Price and timing, the first two things Docs/01 §4.3 asks a customer to compare.
		if offer.AmountCents != 45000 {
			t.Errorf("offer %s is priced at %d, want 45000", offer.ID, offer.AmountCents)
		}
		if offer.PickupAt == "" || offer.DeliverBy == "" {
			t.Errorf("offer %s carries no timing: %+v", offer.ID, offer)
		}
		if offer.JobID != w.job.String() {
			t.Errorf("offer %s names job %s, want %s", offer.ID, offer.JobID, w.job)
		}
		if offer.Status != "submitted" || offer.OfferedBy != "provider" {
			t.Errorf("offer %s is %s by %s, want a submitted provider offer",
				offer.ID, offer.Status, offer.OfferedBy)
		}

		// The provider summary, on every element rather than on some.
		if !offer.Provider.Verified {
			t.Errorf("offer %s reports an unverified provider; both fixtures are verified", offer.ID)
		}
		if offer.Provider.MemberSince == "" {
			t.Errorf("offer %s carries no member_since", offer.ID)
		}
	}

	if _, found := byProvider[w.provider.String()]; !found {
		t.Errorf("the first provider's offer is missing: %s", rec.Body)
	}
	if _, found := byProvider[second.String()]; !found {
		t.Errorf("the second provider's offer is missing: %s", rec.Body)
	}

	// The vehicle, on the offer that named one and absent from the offer that did not — which is
	// the pair that makes `omitempty` mean something rather than being a rendering detail.
	named := page.Data[byProvider[w.provider.String()]]
	if named.Vehicle == nil {
		t.Fatalf("the offer that named a vehicle carries none: %s", rec.Body)
	}
	if named.Vehicle.ID != van.String() {
		t.Errorf("the offer names vehicle %s, want %s", named.Vehicle.ID, van)
	}
	if named.Vehicle.Type != string(fleet.TypeVan) {
		t.Errorf("the vehicle is a %q, want %q", named.Vehicle.Type, fleet.TypeVan)
	}
	if named.Vehicle.Capacity.MaxWeightKg != 1200 || named.Vehicle.Capacity.LengthCm != 300 {
		t.Errorf("the declared capability is %+v, want the fixture van's",
			named.Vehicle.Capacity)
	}

	if unnamed := page.Data[byProvider[second.String()]]; unnamed.Vehicle != nil {
		t.Errorf("an offer that named no vehicle carries one: %+v", unnamed.Vehicle)
	}
}

// TestAProviderGetsWhatAStrangerGets is the *Done when*'s own clause, in both directions.
//
// **Byte-for-byte rather than by status code.** Three callers ask about a job that is not theirs —
// the provider currently bidding on it, an account with no connection to it, and anybody asking
// about a job that does not exist — and all three have to receive the same answer. A 404 whose
// message differed by a word would still tell a competitor that the job exists, which is the
// disclosure this endpoint's refusal is for.
//
// The request id is stripped before comparing, because it is per-request by construction.
func TestAProviderGetsWhatAStrangerGets(t *testing.T) {
	w := newWire(t)

	// The provider is *bidding on this job*, which is the case that matters. A provider with no
	// relationship to the job would be refused by anything.
	if rec := w.bid(t, w.provider, "offers-refusal", validBody()); rec.Code != http.StatusCreated {
		t.Fatalf("placing the offer = %d (%s)", rec.Code, rec.Body)
	}

	stranger := newCustomer(t, w.pool, "offers-stranger@example.com", "+61400000861")
	absent := uuid.Must(uuid.NewV7())

	answers := map[string]*httptest.ResponseRecorder{
		"the provider bidding on it": w.offers(t, w.provider, w.job, ""),
		"a stranger":                 w.offers(t, stranger, w.job, ""),
		"a job that does not exist":  w.offers(t, w.customer, absent, ""),
	}

	var reference string
	for who, rec := range answers {
		if rec.Code != http.StatusNotFound {
			t.Fatalf("%s got %d, want 404: %s", who, rec.Code, rec.Body)
		}

		// Not vacuous: a body with no offers in it would compare equal for the wrong reason.
		if strings.Contains(rec.Body.String(), "amount_cents") {
			t.Fatalf("%s was refused with a body carrying an offer: %s", who, rec.Body)
		}

		normalised := identifier.ReplaceAllString(rec.Body.String(), "<id>")
		if reference == "" {
			reference = normalised
			continue
		}
		if normalised != reference {
			t.Errorf("%s is told something different from the other refusals.\n"+
				"  got  %s\n  want %s\n"+
				"  A refusal that distinguishes \"not yours\" from \"no such job\" confirms that "+
				"somebody else's job exists, which is the disclosure this 404 exists to prevent.",
				who, normalised, reference)
		}
	}

	// And the customer really can read it, so that the three refusals above are refusals rather
	// than an endpoint that answers nobody.
	if rec := w.offers(t, w.customer, w.job, ""); rec.Code != http.StatusOK {
		t.Fatalf("the owning customer got %d, want 200: %s", rec.Code, rec.Body)
	}
}

// customerOfferKeys is every key the customer's view of an offer may carry.
//
// [providerBidKeys] plus the two objects SHIP-102a adds and their fields. A closed list for the
// reason that one is closed: a deny-list of names cannot express "no budget in any form", because
// `max_price` is a budget and does not contain the word.
//
// **`registration` is deliberately not here.** A plate is not something a losing bidder publishes to
// the customer who did not choose them, and a field added to `vehicleSummaryResponse` would have to
// be added here first.
var customerOfferKeys = func() map[string]bool {
	keys := map[string]bool{
		"provider":       true,
		"verified":       true,
		"member_since":   true,
		"vehicle":        true,
		"type":           true,
		"make":           true,
		"model":          true,
		"capacity":       true,
		"max_weight_kg":  true,
		"length_cm":      true,
		"width_cm":       true,
		"height_cm":      true,
		"superseded_by":  true,
		"idempotency_id": false,
	}
	delete(keys, "idempotency_id")
	for key := range providerBidKeys {
		keys[key] = true
	}
	return keys
}()

// TestTheOfferResponseCarriesNothingItMayNot is the *Done when*'s last clause, and it is the one
// this ticket exists to get right.
//
// It does everything TestTheBidResponseCarriesNothingOfTheCustomers does — real HTTP, raw bytes, a
// closed key set at every depth, the value in four renderings — and adds the axis wave 9 found
// surviving everything else:
//
//	**it asserts on words.**
//
// A response that said "the customer has set a maximum" has no budget key and no budget value. It
// passes a closed key set, it passes a value search, and it passes a source-parsing scan, because it
// is a *sentence* rather than a field. That is the third clause of Docs/01 §4.3 — "not as a 'budget
// supplied' indicator" — and it is the form that survived until a pixel-level test caught it on a
// screen. The same form is available on this side of the wire through a `message`, and this is what
// closes it here.
//
// The three forbidden subjects are checked together: the budget, the provider's service area and
// their specialties. All three are things the *Done when* names, and none of them has a field in the
// shape — so what these assertions really guard is a later ticket adding one.
func TestTheOfferResponseCarriesNothingItMayNot(t *testing.T) {
	w := newWire(t)
	van := vehicleOf(t, w.pool, w.provider)

	// The fixture, verified rather than assumed — a privacy test whose fixture has nothing to leak
	// passes forever. The same 4321.99 the provider-facing test uses, and the same reasoning.
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

	// And the provider has a declaration to leak, which is the other half of the *Done when*'s
	// forbidden list. Without this the service-area assertions below would be vacuous.
	if err := db.InTx(t.Context(), w.pool, func(ctx context.Context, r db.Runner) error {
		specialties := []fleet.Specialty{fleet.SpecialtyRefrigerated}
		_, err := fleet.NewService(clock.NewFixed(testInstant)).
			Declare(ctx, r, w.provider, fleet.ProfileFields{Specialties: &specialties})
		return err
	}); err != nil {
		t.Fatalf("declaring a specialty for the provider: %v", err)
	}

	if rec := w.bid(t, w.provider, "offers-privacy", offerWith(van)); rec.Code != http.StatusCreated {
		t.Fatalf("placing the offer = %d (%s)", rec.Code, rec.Body)
	}

	rec := w.offers(t, w.customer, w.job, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("listing the offers = %d (%s)", rec.Code, rec.Body)
	}
	body := rec.Body.String()

	// Not vacuous, twice over: the response has to be carrying an offer *and* a vehicle before
	// anything below means anything.
	if !strings.Contains(body, `"amount_cents"`) || !strings.Contains(body, `"capacity"`) {
		t.Fatalf("this response carries no described offer, so it proves nothing: %s", body)
	}

	// 1. Every key, at every depth, is one this API promised the customer.
	var document any
	if err := json.Unmarshal([]byte(body), &document); err != nil {
		t.Fatalf("the response is not JSON: %v (%s)", err, body)
	}
	walkKeys(document, func(path, key string) {
		if !customerOfferKeys[key] {
			t.Errorf("the customer's response carries %q (at %s).\n"+
				"  Every key the customer may see is in customerOfferKeys, and this is not one of "+
				"them. If it is the provider's service area, their specialties, or one of their "+
				"other jobs, it is a defect — SHIP-102a's Done when forbids all three. If it is a "+
				"genuinely new field, add it here and to the ReceivedOffer schema deliberately.",
				key, path)
		}
	})

	// 2. No value of the budget, in any rendering a JSON encoder could produce, with identifiers
	//    stripped first because a UUID is hexadecimal.
	searchable := identifier.ReplaceAllString(body, "<id>")
	for _, rendering := range []string{"4321.99", "432199", "4321,99", "4,321.99"} {
		if strings.Contains(searchable, rendering) {
			t.Errorf("the customer's budget appears in the offers response as %q: %s",
				rendering, body)
		}
	}

	// 3. **And no words, which is the assertion wave 9's finding is about.** A sentence saying a
	//    maximum exists is the "budget supplied" flag Docs/01 §4.3 forbids, carries no field and no
	//    value, and would pass every check above.
	lowered := strings.ToLower(body)
	for _, forbidden := range []string{
		"budget", "maximum", "max price", "ceiling", "price cap", "willing to pay",
		"service area", "specialt", "refrigerated", "victoria", "\"vic\"",
	} {
		if strings.Contains(lowered, forbidden) {
			t.Errorf("the customer's offers response contains the word %q.\n"+
				"  A response saying a budget exists is the \"budget supplied\" indicator Docs/01 "+
				"§4.3 forbids even with no amount in it, and a provider's declared area or "+
				"specialties are what SHIP-102a's Done when excludes by name.\n  %s",
				forbidden, body)
		}
	}
}

// TestOnlyLiveOffersAreListedByDefault pins [OfferQuery.Status]'s zero value.
//
// The one place this endpoint deliberately differs from `GET /v1/fleet/bids`, whose default is every
// status. A comparison screen compares the offers that can be awarded; a withdrawn one on it is a
// row a customer would try to act on.
func TestOnlyLiveOffersAreListedByDefault(t *testing.T) {
	w := newWire(t)

	placed := w.bid(t, w.provider, "offers-live", validBody())
	if placed.Code != http.StatusCreated {
		t.Fatalf("placing the offer = %d (%s)", placed.Code, placed.Body)
	}
	bidID := uuid.MustParse(decode[map[string]any](t, placed)["id"].(string))

	if rec := w.withdraw(t, w.provider, w.job, bidID, "offers-live-withdraw"); rec.Code != http.StatusOK {
		t.Fatalf("withdrawing = %d (%s)", rec.Code, rec.Body)
	}

	live := decode[struct {
		Data []map[string]any `json:"data"`
	}](t, w.offers(t, w.customer, w.job, ""))
	if len(live.Data) != 0 {
		t.Errorf("a withdrawn offer is on the comparison screen: %+v", live.Data)
	}

	// And `?status=` reaches it, so the row is present rather than gone.
	withdrawn := decode[struct {
		Data []map[string]any `json:"data"`
	}](t, w.offers(t, w.customer, w.job, "?status=withdrawn"))
	if len(withdrawn.Data) != 1 {
		t.Fatalf("?status=withdrawn found %d offers, want the one: %+v", len(withdrawn.Data), withdrawn.Data)
	}
	if withdrawn.Data[0]["id"] != bidID.String() {
		t.Errorf("?status=withdrawn found %v, want %s", withdrawn.Data[0]["id"], bidID)
	}
}

// TestTheOffersListPagesWithACursor is the *Done when*'s "cursor pagination", exercised rather than
// declared.
//
// Three providers, a page of two, and the cursor handed back unchanged — with the union of the two
// pages checked for duplicates and omissions, which is the failure keyset pagination exists to
// prevent and the one an off-by-one in the extra-row trick produces.
func TestTheOffersListPagesWithACursor(t *testing.T) {
	w := newWire(t)

	bidders := []uuid.UUID{w.provider}
	bidders = append(bidders,
		secondBidder(t, w.market, "offers-page-b@example.com", "+61400000862", "BID003"),
		secondBidder(t, w.market, "offers-page-c@example.com", "+61400000863", "BID004"))

	for i, provider := range bidders {
		rec := as(t, w.router, provider, fmt.Sprintf("offers-page-%d", i),
			"/v1/jobs/"+w.job.String()+"/bids", validBody())
		if rec.Code != http.StatusCreated {
			t.Fatalf("offer %d = %d (%s)", i, rec.Code, rec.Body)
		}
	}

	type page struct {
		Data       []map[string]any `json:"data"`
		NextCursor string           `json:"next_cursor"`
		HasMore    bool             `json:"has_more"`
	}

	first := decode[page](t, w.offers(t, w.customer, w.job, "?limit=2"))
	if len(first.Data) != 2 || !first.HasMore || first.NextCursor == "" {
		t.Fatalf("the first page is %d offers, has_more=%v, cursor=%q; want 2, true and a cursor",
			len(first.Data), first.HasMore, first.NextCursor)
	}

	second := decode[page](t, w.offers(t, w.customer, w.job, "?limit=2&cursor="+first.NextCursor))
	if len(second.Data) != 1 || second.HasMore || second.NextCursor != "" {
		t.Fatalf("the second page is %d offers, has_more=%v, cursor=%q; want 1, false and none",
			len(second.Data), second.HasMore, second.NextCursor)
	}

	seen := map[string]bool{}
	for _, offer := range append(append([]map[string]any{}, first.Data...), second.Data...) {
		id, _ := offer["id"].(string)
		if seen[id] {
			t.Errorf("offer %s appears on both pages", id)
		}
		seen[id] = true
	}
	if len(seen) != len(bidders) {
		t.Errorf("the two pages carry %d distinct offers, want %d", len(seen), len(bidders))
	}

	// A cursor this endpoint did not issue is refused rather than read as the first page — the
	// failure `internal/pagination`'s version prefix exists to catch, checked here because only the
	// domain knows what its ordering key is.
	if rec := w.offers(t, w.customer, w.job, "?cursor=not-a-cursor"); rec.Code != http.StatusBadRequest {
		t.Errorf("a mangled cursor = %d, want 400: %s", rec.Code, rec.Body)
	}
}

// TestAnOfferNamesAVehicleFromTheCallersOwnFleet is the write half of 000504, and it is what the
// port migration 000501 declined to guess at.
//
// The three refusals are one answer for [Vehicles.Usable]'s reason: telling them apart would let a
// provider enumerate a competitor's fleet one identifier at a time. What this pins is that all three
// are refused at all, and that they are refused as a *field* rather than as a 404 about the job.
func TestAnOfferNamesAVehicleFromTheCallersOwnFleet(t *testing.T) {
	other := uuid.Must(uuid.NewV7())

	cases := map[string]func(t *testing.T, w wire) string{
		"a vehicle that does not exist": func(*testing.T, wire) string { return other.String() },
		"another provider's vehicle": func(t *testing.T, w wire) string {
			rival := secondBidder(t, w.market, "offers-rival@example.com", "+61400000864", "BID005")
			return vehicleOf(t, w.pool, rival).String()
		},
		// **The provider keeps a second van in service, and that is the point of the case rather
		// than fixture noise.** Deactivating their only vehicle makes them ineligible for the job
		// entirely — SHIP-81's third filter — so the offer would be refused with the job's 404
		// before the vehicle was ever looked at, and the test would pass while proving nothing
		// about this rule. The fixture that isolates it is a provider who can still bid, offering
		// a truck they have taken off the road.
		"a vehicle out of service": func(t *testing.T, w wire) string {
			van := vehicleOf(t, w.pool, w.provider)
			addVehicle(t, w.pool, w.provider, "BID006")
			if err := db.InTx(t.Context(), w.pool, func(ctx context.Context, r db.Runner) error {
				_, err := fleet.NewService(clock.NewFixed(testInstant)).
					Deactivate(ctx, r, w.provider, van)
				return err
			}); err != nil {
				t.Fatalf("deactivating the van: %v", err)
			}
			return van.String()
		},
	}

	for name, vehicle := range cases {
		t.Run(name, func(t *testing.T) {
			w := newWire(t)
			id := vehicle(t, w)

			rec := w.bid(t, w.provider, "offers-vehicle", offerWith(uuid.MustParse(id)))
			if rec.Code != http.StatusUnprocessableEntity {
				t.Fatalf("offering %s = %d, want 422: %s", name, rec.Code, rec.Body)
			}

			envelope := decode[errorEnvelope](t, rec)
			if len(envelope.Error.Details) == 0 || envelope.Error.Details[0].Field != "vehicle_id" {
				t.Errorf("the refusal names %+v, want the vehicle_id field: %s",
					envelope.Error.Details, rec.Body)
			}
		})
	}
}

// TestACounterCarriesTheVehicleForward is the rule [Counter] expresses by having no vehicle field.
//
// A customer countering on price must not silently drop the truck out of the negotiation they are
// comparing — the counter is a new row, so without the carry-forward the live head of the chain
// would name no vehicle and the comparison screen would lose it.
func TestACounterCarriesTheVehicleForward(t *testing.T) {
	w := newWire(t)
	van := vehicleOf(t, w.pool, w.provider)

	placed := w.bid(t, w.provider, "offers-carry", offerWith(van))
	if placed.Code != http.StatusCreated {
		t.Fatalf("placing the offer = %d (%s)", placed.Code, placed.Body)
	}
	bidID := uuid.MustParse(decode[map[string]any](t, placed)["id"].(string))

	countered := w.counter(t, w.customer, w.job, bidID, "offers-carry-counter",
		`{"amount_cents": 40000}`)
	if countered.Code != http.StatusCreated {
		t.Fatalf("the counter = %d (%s)", countered.Code, countered.Body)
	}

	page := decode[struct {
		Data []struct {
			AmountCents int64 `json:"amount_cents"`
			Vehicle     *struct {
				ID string `json:"id"`
			} `json:"vehicle"`
		} `json:"data"`
	}](t, w.offers(t, w.customer, w.job, ""))

	if len(page.Data) != 1 {
		t.Fatalf("the live head is %d offers, want the counter alone: %+v", len(page.Data), page.Data)
	}
	if page.Data[0].AmountCents != 40000 {
		t.Fatalf("the live offer is %d, want the counter's 40000", page.Data[0].AmountCents)
	}
	if page.Data[0].Vehicle == nil {
		t.Fatal("the customer's counter dropped the vehicle out of the negotiation")
	}
	if page.Data[0].Vehicle.ID != van.String() {
		t.Errorf("the counter names vehicle %s, want the provider's %s",
			page.Data[0].Vehicle.ID, van)
	}
}

// TestARevisionMovesTheVehicleAndAClearanceRemovesIt is the one way the vehicle on a negotiation
// changes.
func TestARevisionMovesTheVehicleAndAClearanceRemovesIt(t *testing.T) {
	w := newWire(t)
	van := vehicleOf(t, w.pool, w.provider)

	placed := w.bid(t, w.provider, "offers-revise", validBody())
	if placed.Code != http.StatusCreated {
		t.Fatalf("placing the offer = %d (%s)", placed.Code, placed.Body)
	}
	bidID := uuid.MustParse(decode[map[string]any](t, placed)["id"].(string))

	vehicleOnTheOffer := func(t *testing.T) *string {
		t.Helper()

		page := decode[struct {
			Data []struct {
				Vehicle *struct {
					ID string `json:"id"`
				} `json:"vehicle"`
			} `json:"data"`
		}](t, w.offers(t, w.customer, w.job, ""))
		if len(page.Data) != 1 {
			t.Fatalf("the job carries %d live offers, want one", len(page.Data))
		}
		if page.Data[0].Vehicle == nil {
			return nil
		}
		return &page.Data[0].Vehicle.ID
	}

	if got := vehicleOnTheOffer(t); got != nil {
		t.Fatalf("an offer placed without a vehicle carries %s", *got)
	}

	if rec := w.revise(t, w.provider, w.job, bidID, "offers-revise-set",
		fmt.Sprintf(`{"vehicle_id": %q}`, van)); rec.Code != http.StatusOK {
		t.Fatalf("naming a vehicle = %d (%s)", rec.Code, rec.Body)
	}
	if got := vehicleOnTheOffer(t); got == nil || *got != van.String() {
		t.Fatalf("after the revision the offer carries %v, want %s", got, van)
	}

	// A revision that does not mention the vehicle leaves it alone, which is the case a single
	// pointer would have collapsed with the one below.
	if rec := w.revise(t, w.provider, w.job, bidID, "offers-revise-price",
		`{"amount_cents": 41000}`); rec.Code != http.StatusOK {
		t.Fatalf("re-pricing = %d (%s)", rec.Code, rec.Body)
	}
	if got := vehicleOnTheOffer(t); got == nil || *got != van.String() {
		t.Fatalf("re-pricing dropped the vehicle: got %v, want %s", got, van)
	}

	// **Blank rather than `null`**, and the reason is the decoder rather than taste:
	// `encoding/json` nils a pointer for the JSON literal `null`, so no depth of pointer can tell
	// "absent" from "null". `fleet.VehicleFields` states the same rule for the same reason.
	if rec := w.revise(t, w.provider, w.job, bidID, "offers-revise-clear",
		`{"vehicle_id": ""}`); rec.Code != http.StatusOK {
		t.Fatalf("clearing the vehicle = %d (%s)", rec.Code, rec.Body)
	}
	if got := vehicleOnTheOffer(t); got != nil {
		t.Errorf("after clearing, the offer still carries %s", *got)
	}
}
