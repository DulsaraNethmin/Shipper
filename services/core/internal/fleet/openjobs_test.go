package fleet

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/DulsaraNethmin/Shipper/services/core/internal/pagination"
	"github.com/DulsaraNethmin/Shipper/services/core/internal/testsupport/pgtest"
)

// The provider's view of the marketplace at the wire: SHIP-82's feed and SHIP-83's single job.
//
// These drive the handlers on a mux of their own, the way http_test.go does — what is being checked
// is this domain's own half, and the middleware around it belongs to cmd/api. The mux mounts the
// same patterns cmd/api registers, because the {id} parameter is part of what is under test.
//
// # What has to be shown here
//
//   - **SHIP-82's *Done when*: "returns only eligible jobs, paginated."** Both halves.
//     [TestTheFeedCarriesOnlyEligibleJobs] is the first, with an ineligible job of each kind sitting
//     in the same database so that "only" has something to be wrong about.
//     [TestPagingTheFeedReachesEveryJobExactlyOnce] is the second, and it pages over jobs that share
//     a created_at to the millisecond — which is the case a one-field cursor gets wrong.
//   - **SHIP-83's *Done when*: "provider view omits budget entirely; verified by test."**
//     [TestTheProviderResponseCarriesNoBudgetInAnyForm] is that test and it is the reason this file
//     exists. Docs/11 §8 records it as the one proof still owed from the SHIP-67 pairing: not a
//     struct-field check, but the response a provider actually receives, serialised, asserted to
//     carry no budget in any form.
//   - **A provider who is not eligible cannot reach the job another way.**
//     [TestAJobThisProviderMayNotBidOnIsNotFound] drives the detail endpoint from every ineligible
//     position and requires the answer to be byte-identical to a job that does not exist.

// --- the world these tests run in ---------------------------------------------------------------

// marketplace is a provider, a customer, and jobs published into a real database, with the HTTP
// surface in front of them.
type marketplace struct {
	pool     *pgxpool.Pool
	router   http.Handler
	provider uuid.UUID
	customer uuid.UUID
}

// newMarketplace is a verified provider serving Victoria with a truck in service, and nothing
// published yet.
//
// Deliberately not [newWorld] with a router bolted on: these tests publish several jobs each and
// need to say what is in the database, and a fixture that had already published one would make
// "the feed carries exactly these" an assertion about somebody else's setup.
func newMarketplace(t *testing.T) marketplace {
	t.Helper()

	pool := pgtest.DB(t)
	m := marketplace{
		pool:     pool,
		router:   newTestRouter(t, pool),
		provider: newVerifiedProvider(t, pool, "open-jobs-provider@example.com", "+61400000820"),
		customer: newCustomer(t, pool, "open-jobs-customer@example.com", "+61400000821"),
	}

	declare(t, pool, m.provider, ProfileFields{States: &[]string{"VIC"}})
	addVehicle(t, pool, m.provider, "FEED01",
		Capacity{MaxWeightKg: 1200, LengthCm: 300, WidthCm: 160, HeightCm: 180})
	return m
}

// richJob is a job with every field this endpoint can carry filled in, including the two it must
// never disclose.
//
// **The budget and the street line are real values here on purpose.** A fixture that left them
// NULL would let a response that leaked them pass every check below, which is the failure mode
// Docs/11 §8 describes: a privacy test that asserts nothing reads exactly like one that asserts
// everything.
func richJob(budget float64) jobFields {
	return jobFields{
		PickupLine:       "5 Church Street",
		PickupSuburb:     "Richmond",
		PickupState:      "VIC",
		PickupPostcode:   "3121",
		PickupLatitude:   -37.8197,
		PickupLongitude:  144.9989,
		DropoffLine:      "1 Bourke Street",
		DropoffSuburb:    "Melbourne",
		DropoffState:     "VIC",
		DropoffPostcode:  "3000",
		GoodsDescription: "Two-seater sofa, wrapped, no legs attached",
		WeightKg:         80,
		LengthCm:         190,
		WidthCm:          90,
		HeightCm:         80,

		VehicleRequirement: "Ute with a tailgate lifter",
		HandlingNotes:      "Second-floor walk-up, no lift.",

		PickupWindowStart:  testInstant.Add(48 * time.Hour),
		PickupWindowEnd:    testInstant.Add(72 * time.Hour),
		DropoffWindowStart: testInstant.Add(96 * time.Hour),

		Budget: budget,
	}
}

// publish writes a job and moves it to Open, answering with its identifier.
func (m marketplace) publish(t *testing.T, f jobFields) uuid.UUID {
	t.Helper()
	return publishJob(t, m.pool, m.customer, f)
}

// get is one authenticated read as the marketplace's provider.
func (m marketplace) get(t *testing.T, target string) *httptest.ResponseRecorder {
	t.Helper()
	return as(t, m.router, m.provider, http.MethodGet, target, "")
}

// page is the envelope Docs/10 §4.5 gives every list, decoded as far as `data` and no further.
//
// The entries stay as json.RawMessage deliberately: several tests below assert things about the
// *bytes* of one entry, and decoding into a Go struct would throw away exactly the evidence they
// need — a key present with a null value survives into a struct as an absent one.
type page struct {
	Data       []json.RawMessage `json:"data"`
	NextCursor string            `json:"next_cursor"`
	HasMore    bool              `json:"has_more"`
}

// feed reads one page, failing the test on anything but a 200.
func (m marketplace) feed(t *testing.T, query string) page {
	t.Helper()

	target := "/v1/fleet/jobs"
	if query != "" {
		target += "?" + query
	}

	rec := m.get(t, target)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET %s = %d, want 200 (%s)", target, rec.Code, rec.Body)
	}
	return decode[page](t, rec)
}

// ids is the identifiers in a page, in the order they arrived.
func (p page) ids(t *testing.T) []string {
	t.Helper()

	out := make([]string, 0, len(p.Data))
	for _, entry := range p.Data {
		var job struct {
			ID string `json:"id"`
		}
		if err := json.Unmarshal(entry, &job); err != nil {
			t.Fatalf("an entry is not a job: %v (%s)", err, entry)
		}
		out = append(out, job.ID)
	}
	return out
}

// --- SHIP-82: only eligible jobs ----------------------------------------------------------------

// TestTheFeedCarriesOnlyEligibleJobs is the first half of SHIP-82's *Done when*.
//
// One job of each kind in one database, so that "only the eligible one" is a claim about a filter
// rather than about an empty table. The four exclusions are one per filter in Docs/01 §4.3, which
// is the same burden SHIP-81 met against the service — repeated here because a handler can lose a
// filter that the service still applies, by calling the wrong method or by paging past it.
func TestTheFeedCarriesOnlyEligibleJobs(t *testing.T) {
	m := newMarketplace(t)

	eligible := m.publish(t, richJob(1500))

	// Job status: published and then cancelled.
	cancelled := m.publish(t, richJob(1500))
	transition(t, m.pool, cancelled, m.customer, "Open", "Cancelled")

	// Job status again: never published at all.
	draft := draftJob(t, m.pool, m.customer, richJob(1500))

	// Service area: picked up somewhere the provider has not declared.
	elsewhere := richJob(1500)
	elsewhere.PickupState, elsewhere.PickupPostcode, elsewhere.PickupSuburb = "QLD", "4000", "Brisbane"
	outOfArea := m.publish(t, elsewhere)

	// Vehicle capability: a known mismatch, not a missing measurement.
	heavy := richJob(1500)
	heavy.WeightKg = 9000
	tooHeavy := m.publish(t, heavy)

	// The provider's own account is the fourth filter, and it cannot be broken per job — an
	// unverified provider sees nothing at all, which is TestAnIneligibleProviderSeesAnEmptyFeed.

	got := m.feed(t, "").ids(t)
	if len(got) != 1 || got[0] != eligible.String() {
		t.Fatalf("the feed carries %v, want exactly %s.\n"+
			"  cancelled=%s draft=%s out of area=%s too heavy=%s",
			got, eligible, cancelled, draft, outOfArea, tooHeavy)
	}
}

// TestAnIneligibleProviderSeesAnEmptyFeed is the fourth filter and the empty-page decision at once.
//
// Three ways to be eligible for nothing, and a customer account as a fourth. None of them is an
// error: the feed answers the question truthfully, and the client sends the provider to onboarding
// from an empty list rather than from a 403 it would have to special-case.
func TestAnIneligibleProviderSeesAnEmptyFeed(t *testing.T) {
	cases := map[string]func(t *testing.T, m marketplace) uuid.UUID{
		"unverified": func(t *testing.T, m marketplace) uuid.UUID {
			exec(t, m.pool, `UPDATE users SET phone_verified_at = NULL WHERE id = $1`, m.provider)
			return m.provider
		},
		"has declared no service area": func(t *testing.T, m marketplace) uuid.UUID {
			declare(t, m.pool, m.provider, ProfileFields{States: &[]string{}, Postcodes: &[]string{}})
			return m.provider
		},
		"has no vehicle in service": func(t *testing.T, m marketplace) uuid.UUID {
			exec(t, m.pool, `UPDATE vehicles SET deactivated_at = now() WHERE provider_id = $1`, m.provider)
			return m.provider
		},
		"is a customer who followed the wrong link": func(t *testing.T, m marketplace) uuid.UUID {
			return m.customer
		},
	}

	for name, arrange := range cases {
		t.Run(name, func(t *testing.T) {
			m := newMarketplace(t)
			m.publish(t, richJob(1500))

			caller := arrange(t, m)

			rec := as(t, m.router, caller, http.MethodGet, "/v1/fleet/jobs", "")
			if rec.Code != http.StatusOK {
				t.Fatalf("status = %d, want 200 — being eligible for nothing is an answer, "+
					"not a failure of the request (%s)", rec.Code, rec.Body)
			}
			if body := strings.TrimSpace(rec.Body.String()); body != `{"data":[],"has_more":false}` {
				t.Errorf("the feed is %s, want an empty page. `data` is never null: a client "+
					"iterating it breaks the first time a new provider opens the app.", body)
			}
		})
	}
}

// TestTheFeedIsNewestFirstAndCarriesTheEnvelope pins the two things a client reads before it reads
// a job: the envelope's shape, and the order.
func TestTheFeedIsNewestFirstAndCarriesTheEnvelope(t *testing.T) {
	m := newMarketplace(t)

	var published []uuid.UUID
	for i := range 3 {
		id := m.publish(t, richJob(1500))
		// Distinct creation instants, oldest first, so "newest first" is falsifiable.
		exec(t, m.pool, `UPDATE jobs SET created_at = $2 WHERE id = $1`,
			id, testInstant.Add(time.Duration(i)*time.Minute))
		published = append(published, id)
	}

	rec := m.get(t, "/v1/fleet/jobs")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (%s)", rec.Code, rec.Body)
	}

	// The envelope, from the raw body: Docs/10 §4.5 gives every list `data`, `next_cursor` and
	// `has_more`, and a fourth key would be this endpoint inventing its own shape.
	var envelope map[string]json.RawMessage
	if err := json.Unmarshal(rec.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("the response is not JSON: %v (%s)", err, rec.Body)
	}
	for key := range envelope {
		switch key {
		case "data", "next_cursor", "has_more":
		default:
			t.Errorf("the envelope carries %q, which Docs/10 §4.5 does not give a list", key)
		}
	}
	if _, ok := envelope["has_more"]; !ok {
		t.Error("has_more is absent; it is carried rather than inferred from the cursor")
	}

	got := decode[page](t, rec).ids(t)
	want := []string{published[2].String(), published[1].String(), published[0].String()}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("the feed reads %v, want newest first %v", got, want)
	}
}

// --- SHIP-82: paginated -------------------------------------------------------------------------

// TestPagingTheFeedReachesEveryJobExactlyOnce is the second half of SHIP-82's *Done when*, and it
// is written against the way the cursor can actually be wrong.
//
// **Three of the seven jobs share a created_at to the microsecond**, which is not contrived: a
// customer publishing several jobs in one sitting writes rows the database timestamps identically,
// and the fixtures below are what a keyset ordered on time alone gets wrong. With a one-field
// cursor those three either repeat forever or vanish, depending on whether the comparison is `<` or
// `<=` — and both failures are invisible at a page size that happens not to land between them,
// which is why this pages at every size from one to eight rather than at one convenient size.
//
// The check is a multiset, not a set: `seen` is compared for length as well as for membership, so a
// job returned on two consecutive pages fails even though the set of identifiers would still match.
func TestPagingTheFeedReachesEveryJobExactlyOnce(t *testing.T) {
	m := newMarketplace(t)

	const jobs = 7
	var published []string
	for i := range jobs {
		id := m.publish(t, richJob(1500))
		published = append(published, id.String())

		// Jobs 2, 3 and 4 are published in the same instant. The rest are a minute apart.
		at := testInstant.Add(time.Duration(i) * time.Minute)
		if i >= 2 && i <= 4 {
			at = testInstant.Add(2 * time.Minute)
		}
		exec(t, m.pool, `UPDATE jobs SET created_at = $2 WHERE id = $1`, id, at)
	}
	sort.Strings(published)

	for size := 1; size <= jobs+1; size++ {
		t.Run(fmt.Sprintf("pages of %d", size), func(t *testing.T) {
			var (
				seen   []string
				cursor string
				pages  int
			)
			for {
				pages++
				if pages > jobs+2 {
					t.Fatalf("paging did not terminate after %d pages; the cursor is not advancing", pages)
				}

				query := fmt.Sprintf("limit=%d", size)
				if cursor != "" {
					query += "&cursor=" + url.QueryEscape(cursor)
				}

				p := m.feed(t, query)
				seen = append(seen, p.ids(t)...)

				if !p.HasMore {
					if p.NextCursor != "" {
						t.Error("the last page carries a cursor")
					}
					break
				}
				if len(p.Data) != size {
					t.Fatalf("a page before the last holds %d jobs, want the limit of %d",
						len(p.Data), size)
				}
				if p.NextCursor == "" {
					t.Fatal("has_more is true and there is no cursor to follow")
				}
				cursor = p.NextCursor
			}

			if len(seen) != jobs {
				t.Fatalf("paging returned %d entries over %d pages, want %d — a repeat and a "+
					"skip are both invisible to a set comparison", len(seen), pages, jobs)
			}
			got := append([]string(nil), seen...)
			sort.Strings(got)
			if strings.Join(got, ",") != strings.Join(published, ",") {
				t.Errorf("paging reached %v, want %v", got, published)
			}
		})
	}
}

// TestTheFeedRefusesAQueryItCannotHonour keeps the two parameters honest.
//
// A mangled cursor is refused rather than read as "the first page", which would silently restart a
// client at the top of the feed and show it jobs it had already worked through. A limit that is not
// a number is a client defect; a limit above the maximum is a request the platform narrows.
func TestTheFeedRefusesAQueryItCannotHonour(t *testing.T) {
	m := newMarketplace(t)
	m.publish(t, richJob(1500))

	// A cursor this endpoint could not have issued: mangled, of the wrong field count, or two
	// fields that decode and do not parse as this list's ordering key.
	wrongFieldCount := pagination.Cursor{"one", "two", "three"}.Encode()
	notAKeysetPosition := pagination.Cursor{"yesterday", "the-second-one"}.Encode()

	refused := map[string]string{
		"a cursor that is not base64":                         "cursor=!!!not-a-cursor!!!",
		"a cursor with too many fields":                       "cursor=" + url.QueryEscape(wrongFieldCount),
		"a cursor whose fields are not a timestamp and an id": "cursor=" + url.QueryEscape(notAKeysetPosition),
		"a limit of zero":                                     "limit=0",
		"a negative limit":                                    "limit=-1",
		"a limit that is not a number":                        "limit=twenty",
	}
	for name, query := range refused {
		t.Run(name, func(t *testing.T) {
			rec := m.get(t, "/v1/fleet/jobs?"+query)
			if rec.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, want 400 (%s)", rec.Code, rec.Body)
			}
			if code := decode[errorEnvelope](t, rec).Error.Code; code != "bad_request" {
				t.Errorf("error.code = %q, want bad_request — a query parameter is how the "+
					"request was addressed, not data somebody typed into a form", code)
			}
		})
	}

	t.Run("a limit above the maximum is narrowed rather than refused", func(t *testing.T) {
		rec := m.get(t, "/v1/fleet/jobs?limit=5000")
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200 — raising the maximum later must not break a client (%s)",
				rec.Code, rec.Body)
		}
	})
}

// --- SHIP-83: one job, and no budget ------------------------------------------------------------

// TestTheProviderJobDetailIsTheJobTheFeedShowed is SHIP-83's endpoint, and the one-shape rule.
//
// A client that fetched a job from the feed and then fetched it again by identifier must get the
// same object, byte for byte. Two shapes would be two places a field could be added — including the
// one field that must never be added — and the reason this is asserted rather than assumed is that
// a "detail" view is exactly where somebody would later put "just a little more".
func TestTheProviderJobDetailIsTheJobTheFeedShowed(t *testing.T) {
	m := newMarketplace(t)
	job := m.publish(t, richJob(1500))

	fromFeed := m.feed(t, "")
	if len(fromFeed.Data) != 1 {
		t.Fatalf("the feed carries %d jobs, want the one just published", len(fromFeed.Data))
	}

	rec := m.get(t, "/v1/fleet/jobs/"+job.String())
	if rec.Code != http.StatusOK {
		t.Fatalf("GET the job = %d, want 200 (%s)", rec.Code, rec.Body)
	}

	if got, want := canonical(t, rec.Body.Bytes()), canonical(t, fromFeed.Data[0]); got != want {
		t.Errorf("the detail view and the feed entry are different shapes.\n  detail: %s\n  feed:   %s",
			got, want)
	}

	// And it carries what a provider prices on. Docs/01 §4.3's answer to a provider who wants the
	// budget is better job detail, so a shape that dropped the handling notes to be safe would be
	// taking away the thing the rule offers in exchange.
	var job1 struct {
		ID                 string `json:"id"`
		Status             string `json:"status"`
		GoodsDescription   string `json:"goods_description"`
		HandlingNotes      string `json:"handling_notes"`
		VehicleRequirement string `json:"vehicle_requirement"`
		ExpiresAt          string `json:"expires_at"`
		Pickup             struct {
			Suburb, State, Postcode string
		} `json:"pickup"`
		PickupWindow *struct {
			Start, End string
		} `json:"pickup_window"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &job1); err != nil {
		t.Fatalf("the response is not JSON: %v", err)
	}
	switch {
	case job1.ID != job.String():
		t.Errorf("id = %q, want %s", job1.ID, job)
	case job1.Status != "open":
		t.Errorf("status = %q, want the wire form `open` (Docs/10 §4.7)", job1.Status)
	case job1.Pickup.Suburb != "Richmond" || job1.Pickup.State != "VIC" || job1.Pickup.Postcode != "3121":
		t.Errorf("pickup = %+v, want Richmond VIC 3121", job1.Pickup)
	case job1.HandlingNotes == "" || job1.VehicleRequirement == "" || job1.GoodsDescription == "":
		t.Error("the detail a provider prices on is missing; Docs/01 §4.3 offers it in exchange " +
			"for the budget, so dropping it is not the safe direction")
	case job1.PickupWindow == nil || job1.PickupWindow.Start == "":
		t.Error("the pickup window is absent from a job that has one")
	case job1.ExpiresAt == "":
		t.Error("expires_at is absent; a provider cannot tell how long they have to price it")
	}
}

// TestTheProviderResponseCarriesNoBudgetInAnyForm is SHIP-83's *Done when*, and the proof
// `Docs/11` §8 records as the one thing the SHIP-67 pairing still owed.
//
// SHIP-67 landed three tests: one that parses `internal/jobs` for a budget field on a shape it
// should not be on, one over the responses a provider could obtain *at that time*, and one on the
// stored event payload. §8 states what was missing plainly — "SHIP-83 remains reserved to this
// owner and adds the fourth: its provider response, serialised, asserted to carry no budget."
//
// # What this test does that a struct-field check cannot
//
//  1. **It obtains the response the way a provider obtains it** — an HTTP request, through the real
//     handler, mounted on the pattern cmd/api serves, answered as bytes. A field can be absent from
//     a struct and present on the wire (an embedded type, a custom MarshalJSON, a map) and a struct
//     check would not see any of the three.
//  2. **It works on the raw bytes, so a key that is present and null still fails.** Decoding into a
//     Go type would turn `"budget_cents": null` into a zero value indistinguishable from a field
//     that was never sent — and a present-but-null key is exactly the "budget supplied" signal
//     Docs/01 §4.3 forbids, because a provider learns which jobs have a budget from which responses
//     carry the key at all.
//  3. **It asserts a closed set of keys rather than searching for the word "budget".** A search
//     catches `budget_cents` and misses `max_price`, `ceiling`, or `customer_maximum`. The
//     allow-list below is every key this API promises a provider, so a field that arrives under any
//     name at all fails — which is the only form of this test that survives somebody deciding to be
//     helpful.
//  4. **It checks the value, not only the name.** The fixture's budget is a number that appears
//     nowhere else in the job, and the whole serialised body is searched for it in every rendering
//     a JSON encoder could produce. A band, a rounding, or an amount smuggled into free text fails
//     here.
//
// # It refuses to run against a job with no budget
//
// The first thing it does is read `jobs.budget` back out of the row. A privacy test whose fixture
// has nothing to leak passes forever and proves nothing, and that is the specific way this test
// could rot without anybody noticing.
func TestTheProviderResponseCarriesNoBudgetInAnyForm(t *testing.T) {
	m := newMarketplace(t)

	// A number that appears nowhere else in the job: not in a dimension, a postcode, a timestamp
	// or an identifier. 4321.99 renders as 4321.99, 4321, and 432199 in cents — all three are
	// searched for below.
	const budget = 4321.99
	job := m.publish(t, richJob(budget))

	// The fixture, verified rather than assumed.
	var stored *float64
	if err := m.pool.QueryRow(t.Context(),
		`SELECT budget FROM jobs WHERE id = $1`, job).Scan(&stored); err != nil {
		t.Fatalf("reading the stored budget: %v", err)
	}
	if stored == nil || *stored != budget {
		t.Fatalf("the job's budget is %v, want %v — this test asserts nothing against a job "+
			"that has no budget to leak", stored, budget)
	}

	// Every way a provider can obtain a job from this service. A shape that is safe on one
	// endpoint and not on the other is the failure two responses invite, and it is why both are
	// here rather than only the one SHIP-83 adds.
	responses := map[string][]byte{
		"the feed, GET /v1/fleet/jobs":               nil,
		"the job itself, GET /v1/fleet/jobs/{id}":    nil,
		"the feed a second time, following a cursor": nil,
	}

	feed := m.get(t, "/v1/fleet/jobs")
	if feed.Code != http.StatusOK {
		t.Fatalf("the feed = %d, want 200 (%s)", feed.Code, feed.Body)
	}
	responses["the feed, GET /v1/fleet/jobs"] = feed.Body.Bytes()

	detail := m.get(t, "/v1/fleet/jobs/"+job.String())
	if detail.Code != http.StatusOK {
		t.Fatalf("the job = %d, want 200 (%s)", detail.Code, detail.Body)
	}
	responses["the job itself, GET /v1/fleet/jobs/{id}"] = detail.Body.Bytes()

	// A second page is a different code path through the same handler — the one that renders a
	// cursor — and a leak there would be reached only by a provider who scrolled.
	second := m.publish(t, richJob(budget))
	exec(t, m.pool, `UPDATE jobs SET created_at = $2 WHERE id = $1`, second, testInstant.Add(time.Minute))
	firstPage := m.feed(t, "limit=1")
	if !firstPage.HasMore {
		t.Fatalf("the fixture did not produce a second page")
	}
	paged := m.get(t, "/v1/fleet/jobs?limit=1&cursor="+url.QueryEscape(firstPage.NextCursor))
	if paged.Code != http.StatusOK {
		t.Fatalf("the second page = %d, want 200 (%s)", paged.Code, paged.Body)
	}
	responses["the feed a second time, following a cursor"] = paged.Body.Bytes()

	for how, body := range responses {
		t.Run(how, func(t *testing.T) {
			// Not vacuous: the response has to be carrying the job before "it carries no
			// budget" means anything at all.
			if !strings.Contains(string(body), `"goods_description"`) {
				t.Fatalf("this response carries no job, so it proves nothing: %s", body)
			}

			assertNoBudget(t, body)
		})
	}
}

// providerJobKeys is every key the provider's view of a job may carry.
//
// **A closed list, and the point of the exercise.** Docs/01 §4.3 forbids the customer's budget
// reaching a provider "as an amount, a band, or a 'budget supplied' flag", and a deny-list of names
// cannot express that: `max_price` is a budget and does not contain the word. This is the other
// direction — anything not promised is refused — so a field added to the provider's shape has to be
// added here too, which makes it a decision somebody records rather than one that arrives with a
// schema change.
//
// It is the same list as the `OpenJob` schema in contracts/paths/fleet.yaml, which is
// `additionalProperties: false` for the same reason.
var providerJobKeys = map[string]bool{
	"id":                  true,
	"status":              true,
	"pickup":              true,
	"dropoff":             true,
	"goods_description":   true,
	"length_cm":           true,
	"width_cm":            true,
	"height_cm":           true,
	"weight_kg":           true,
	"vehicle_requirement": true,
	"handling_notes":      true,
	"pickup_window":       true,
	"dropoff_window":      true,
	"expires_at":          true,
	"created_at":          true,

	// The envelope Docs/10 §4.5 wraps a list in, and the two nested shapes' own keys. They are
	// in one list rather than three because the walk below is recursive: what matters is that
	// **no** key anywhere in the document is unaccounted for.
	"data":        true,
	"next_cursor": true,
	"has_more":    true,
	"suburb":      true,
	"state":       true,
	"postcode":    true,
	"start":       true,
	"end":         true,
}

// assertNoBudget is the whole of the check, applied to one serialised response.
func assertNoBudget(t *testing.T, body []byte) {
	t.Helper()

	// 1. Every key in the document, at every depth, is one this API promised a provider.
	var document any
	if err := json.Unmarshal(body, &document); err != nil {
		t.Fatalf("the response is not JSON: %v (%s)", err, body)
	}
	walkKeys(document, func(path, key string) {
		if !providerJobKeys[key] {
			t.Errorf("the provider's response carries %q (at %s).\n"+
				"  Every key a provider may see is in providerJobKeys, and this is not one of "+
				"them. If it is the customer's budget under another name, it is a defect: "+
				"Docs/01 §4.3 forbids it as an amount, a band, or a \"budget supplied\" flag. "+
				"If it is a genuinely new field, add it to that list and to the OpenJob schema "+
				"deliberately.", key, path)
		}
	})

	// 2. No key named for the budget in any spelling — including one present and null, which the
	//    walk above catches by name and this catches in the bytes even if it is nested inside a
	//    string.
	if strings.Contains(strings.ToLower(string(body)), "budget") {
		t.Errorf("the word \"budget\" appears in a provider's response: %s", body)
	}

	// 3. Not the value either, in any rendering a JSON encoder could produce. `4321.99` is the
	//    stored number; `432199` is it in cents, which is the form the customer's own response
	//    uses and therefore the likeliest way it would arrive here.
	//
	//    Identifiers are removed before the search. A UUID is hexadecimal, so a run of digits
	//    can occur inside one by chance — rarely enough to pass in review and often enough to
	//    fail in CI one morning, which is the worst kind of test.
	searchable := identifier.ReplaceAllString(string(body), "<id>")
	for _, rendering := range []string{"4321.99", "432199", "4321,99", "4,321.99"} {
		if strings.Contains(searchable, rendering) {
			t.Errorf("the customer's budget appears in a provider's response as %q: %s",
				rendering, body)
		}
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

// TestTheProviderJobCarriesNeitherTheStreetLineNorTheCoordinate is the other disclosure decision
// SHIP-81 left for this ticket, confirmed rather than inherited.
//
// **Confirmed: suburb, state and postcode, and not the street line.** No document takes a position
// on when a provider learns the exact door, so this stays with the reversible direction — the
// argument Docs/01 §4.3 makes about the budget, applied to an address. A provider prices on the
// locality, the distance and the state; the doorstep is needed by whoever drives to it, which is
// after an award. Disclosing later is easy and withdrawing later is not.
//
// **And the coordinate goes with it**, which is the half that would have been easy to leave open:
// `jobs` geocodes the whole address, so the pickup coordinate *is* the street line as two numbers.
// A shape that withheld `line` and sent `coordinate` would have kept the letter of the decision and
// broken it completely. This asserts both against a job that has both.
func TestTheProviderJobCarriesNeitherTheStreetLineNorTheCoordinate(t *testing.T) {
	m := newMarketplace(t)
	job := m.publish(t, richJob(1500))

	// The fixture has to have an address and a coordinate, or this proves nothing.
	var line string
	var latitude *float64
	if err := m.pool.QueryRow(t.Context(),
		`SELECT pickup_line, pickup_latitude FROM jobs WHERE id = $1`, job).Scan(&line, &latitude); err != nil {
		t.Fatalf("reading the stored address: %v", err)
	}
	if line == "" || latitude == nil {
		t.Fatalf("the job has no street line or no coordinate (%q, %v), so this asserts nothing",
			line, latitude)
	}

	rec := m.get(t, "/v1/fleet/jobs/"+job.String())
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (%s)", rec.Code, rec.Body)
	}

	body := rec.Body.String()
	for what, disclosure := range map[string]string{
		"the pickup street line":  line,
		"the pickup latitude":     "-37.8197",
		"the pickup longitude":    "144.9989",
		"the dropoff street line": "1 Bourke Street",
	} {
		if strings.Contains(body, disclosure) {
			t.Errorf("%s (%q) reached a provider who has not bid.\n"+
				"  SHIP-83 confirmed SHIP-81's decision: a provider is given the locality and "+
				"not the doorstep until they are the one driving to it. The awarded provider's "+
				"view is a different shape at a different moment.", what, disclosure)
		}
	}

	// And the locality is there, so this is a decision about grain rather than a response that
	// forgot the address.
	if !strings.Contains(body, "Richmond") || !strings.Contains(body, "3121") {
		t.Errorf("the pickup locality is missing; a provider cannot price a job without it: %s", body)
	}
}

// TestAJobThisProviderMayNotBidOnIsNotFound is the authorisation half of SHIP-83.
//
// Every way of being ineligible answers 404, and the body has to be byte-identical to the answer
// for a job that does not exist. 403 would confirm the job is there, and which jobs exist — and
// which of them a competitor may bid on — is information nobody published.
func TestAJobThisProviderMayNotBidOnIsNotFound(t *testing.T) {
	// Each case makes the provider ineligible somehow and answers with the job to then ask for —
	// which is the published one for all but the draft, where the point is a job that was never
	// offered at all.
	cases := map[string]func(t *testing.T, m marketplace, job uuid.UUID) uuid.UUID{
		"the job was cancelled": func(t *testing.T, m marketplace, job uuid.UUID) uuid.UUID {
			transition(t, m.pool, job, m.customer, "Open", "Cancelled")
			return job
		},
		"the job was never published": func(t *testing.T, m marketplace, job uuid.UUID) uuid.UUID {
			// A second job, left as a draft. The status is not moved back by UPDATE, because
			// 000402 refuses a status change that did not come through the guard — which is the
			// invariant working rather than an inconvenience.
			return draftJob(t, m.pool, m.customer, richJob(1500))
		},
		"the job's deadline has passed": func(t *testing.T, m marketplace, job uuid.UUID) uuid.UUID {
			setExpiry(t, m.pool, job, alreadyPast)
			return job
		},
		"the provider does not serve where it is picked up": func(t *testing.T, m marketplace, job uuid.UUID) uuid.UUID {
			declare(t, m.pool, m.provider, ProfileFields{States: &[]string{"WA"}})
			return job
		},
		"the provider's only vehicle is off the road": func(t *testing.T, m marketplace, job uuid.UUID) uuid.UUID {
			exec(t, m.pool, `UPDATE vehicles SET deactivated_at = now() WHERE provider_id = $1`, m.provider)
			return job
		},
		"the provider is not verified": func(t *testing.T, m marketplace, job uuid.UUID) uuid.UUID {
			exec(t, m.pool, `UPDATE users SET email_verified_at = NULL WHERE id = $1`, m.provider)
			return job
		},
		"the provider's account is suspended": func(t *testing.T, m marketplace, job uuid.UUID) uuid.UUID {
			exec(t, m.pool, `UPDATE users SET status = 'suspended' WHERE id = $1`, m.provider)
			return job
		},
		"the truck cannot carry it": func(t *testing.T, m marketplace, job uuid.UUID) uuid.UUID {
			exec(t, m.pool, `UPDATE jobs SET weight_kg = 9000 WHERE id = $1`, job)
			return job
		},
		"the caller published the job themselves": func(t *testing.T, m marketplace, job uuid.UUID) uuid.UUID {
			// A customer reading their own job through the provider's route. `GET /v1/jobs/{id}`
			// is where they read it, and that one carries their budget; this must not become a
			// second way to the same row.
			return job
		},
	}

	for name, ineligible := range cases {
		t.Run(name, func(t *testing.T) {
			m := newMarketplace(t)
			job := m.publish(t, richJob(1500))

			caller := m.provider
			if name == "the caller published the job themselves" {
				caller = m.customer
			}

			// It is reachable first, by the provider. Otherwise every case below would pass
			// against an endpoint that answered 404 to everything.
			if rec := m.get(t, "/v1/fleet/jobs/"+job.String()); rec.Code != http.StatusOK {
				t.Fatalf("the job was already unreachable before the test broke anything: %d (%s)",
					rec.Code, rec.Body)
			}

			asked := ineligible(t, m, job)

			refused := as(t, m.router, caller, http.MethodGet, "/v1/fleet/jobs/"+asked.String(), "")
			if refused.Code != http.StatusNotFound {
				t.Fatalf("status = %d, want 404 (%s)", refused.Code, refused.Body)
			}

			// The job that does not exist, asked for by the same caller in the same state.
			absent := as(t, m.router, caller, http.MethodGet,
				"/v1/fleet/jobs/00000000-0000-7000-8000-000000000010", "")
			if absent.Code != http.StatusNotFound {
				t.Fatalf("a job that does not exist = %d, want 404 (%s)", absent.Code, absent.Body)
			}

			refusedBody := decode[errorEnvelope](t, refused).Error
			absentBody := decode[errorEnvelope](t, absent).Error
			if refusedBody.Code != absentBody.Code || refusedBody.Message != absentBody.Message {
				t.Errorf("a job this provider may not bid on answers %q/%q and a job that does "+
					"not exist answers %q/%q. The two must be indistinguishable, or the refusal "+
					"confirms the job exists.",
					refusedBody.Code, refusedBody.Message, absentBody.Code, absentBody.Message)
			}
			if refusedBody.Code != "not_found" {
				t.Errorf("error.code = %q, want not_found", refusedBody.Code)
			}

			// Nothing about eligibility, either: a message explaining *why* would disclose what
			// the status code is withholding.
			if strings.Contains(strings.ToLower(refusedBody.Message), "eligib") {
				t.Errorf("the refusal explains itself (%q), which tells the caller the job is "+
					"there", refusedBody.Message)
			}
		})
	}
}

// TestAnotherProvidersFeedIsNotReachableByAsking is the rule Docs/07 §3 puts on the platform.
//
// There is no parameter for whose feed to read, so the only thing to check is that adding one
// changes nothing: a caller who appends `provider_id` gets their own feed, not the other
// provider's. An unknown query parameter is passed over rather than refused, so the failure this
// prevents is the quiet one — a client believing it had asked for something.
func TestAnotherProvidersFeedIsNotReachableByAsking(t *testing.T) {
	m := newMarketplace(t)
	m.publish(t, richJob(1500))

	// A second provider, verified and serving a state where nothing is published.
	other := newVerifiedProvider(t, m.pool, "other-provider@example.com", "+61400000822")
	declare(t, m.pool, other, ProfileFields{States: &[]string{"WA"}})
	addVehicle(t, m.pool, other, "OTHR01", Capacity{MaxWeightKg: 1200})

	target := "/v1/fleet/jobs?provider_id=" + m.provider.String()
	rec := as(t, m.router, other, http.MethodGet, target, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (%s)", rec.Code, rec.Body)
	}
	if got := decode[page](t, rec).ids(t); len(got) != 0 {
		t.Errorf("naming another provider returned %v. The feed is the caller's, and a provider "+
			"id in a request would be an authorisation decision made from client input.", got)
	}
}

// TestTheBiddableStatusesReachTheWireInDocs02sNames pairs the wire form with the stored form.
//
// `Docs/10` §4.7 puts statuses on the wire in lower snake case and `Docs/02` §1 names them; the
// enum in contracts/paths/fleet.yaml lists exactly the two below. The stored forms are already held
// to `ck_jobs_status` by TestTheBiddableStatusesAreRealJobStatuses, so this closes the other half
// of the loop: a client branching on `open` keeps working, and a `Negotiating` job — which nothing
// can produce until SHIP-90 — arrives as `negotiating` rather than as `Negotiating`.
func TestTheBiddableStatusesReachTheWireInDocs02sNames(t *testing.T) {
	want := map[string]string{"Open": "open", "Negotiating": "negotiating"}

	if len(biddableStatuses) != len(want) {
		t.Fatalf("biddableStatuses is %v; this test names the wire form of each and has not been "+
			"updated", biddableStatuses)
	}
	for _, stored := range biddableStatuses {
		if got := wireStatus(stored); got != want[stored] {
			t.Errorf("%q reaches the wire as %q, want %q", stored, got, want[stored])
		}
	}
	if got := wireStatus("Driver assigned"); got != "driver_assigned" {
		t.Errorf("wireStatus is not the general transformation: %q", got)
	}

	// And through the endpoint, for the status that is otherwise unreachable today.
	m := newMarketplace(t)
	job := m.publish(t, richJob(1500))
	transition(t, m.pool, job, m.customer, "Open", "Negotiating")

	rec := m.get(t, "/v1/fleet/jobs/"+job.String())
	if rec.Code != http.StatusOK {
		t.Fatalf("a Negotiating job = %d, want 200 — Docs/02 §1 keeps it open to eligible bids (%s)",
			rec.Code, rec.Body)
	}
	var body struct {
		Status string `json:"status"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("the response is not JSON: %v", err)
	}
	if body.Status != "negotiating" {
		t.Errorf("status = %q, want negotiating", body.Status)
	}
}

// TestTheJobIDInThePathMustBeAnIdentifier keeps a malformed path out of the database.
//
// bad_request rather than not_found, which is httpx's own division: the path was addressed wrongly
// rather than naming something absent. It also discloses nothing — the answer is the same whether
// or not any job exists.
func TestTheJobIDInThePathMustBeAnIdentifier(t *testing.T) {
	m := newMarketplace(t)
	m.publish(t, richJob(1500))

	rec := m.get(t, "/v1/fleet/jobs/not-a-uuid")
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400 (%s)", rec.Code, rec.Body)
	}
	if code := decode[errorEnvelope](t, rec).Error.Code; code != "bad_request" {
		t.Errorf("error.code = %q, want bad_request", code)
	}
}

// canonical re-encodes JSON with its keys sorted, so two documents can be compared for content
// rather than for the order a map happened to marshal in.
func canonical(t *testing.T, raw []byte) string {
	t.Helper()

	var value any
	if err := json.Unmarshal(raw, &value); err != nil {
		t.Fatalf("not JSON: %v (%s)", err, raw)
	}
	out, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("re-encoding: %v", err)
	}
	return string(out)
}
