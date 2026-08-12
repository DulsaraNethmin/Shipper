package fleet

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/DulsaraNethmin/Shipper/services/core/internal/pagination"
	"github.com/DulsaraNethmin/Shipper/services/core/internal/testsupport/pgtest"
)

// The provider's view of the marketplace at the wire: SHIP-82's feed.
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
//   - **The feed is the caller's own and cannot be widened by asking.**
//     [TestAnotherProvidersFeedIsNotReachableByAsking]. Eligibility is the platform's decision, so
//     there is no parameter for whose feed to read and nothing here to forget to filter.
//
// SHIP-83 adds the single job to this file, and with it the serialised-response budget test that
// Docs/11 §8 records as the one proof the SHIP-67 pairing still owes.

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
// **The budget and the street line are real values here on purpose**, even though nothing in this
// file reads them yet. A fixture that left them NULL would let a response that leaked them pass
// SHIP-83's checks, which is the failure mode Docs/11 §8 describes: a privacy test that asserts
// nothing reads exactly like one that asserts everything.
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

	target := "/v1/jobs/open"
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

			rec := as(t, m.router, caller, http.MethodGet, "/v1/jobs/open", "")
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

	rec := m.get(t, "/v1/jobs/open")
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
			rec := m.get(t, "/v1/jobs/open?"+query)
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
		rec := m.get(t, "/v1/jobs/open?limit=5000")
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200 — raising the maximum later must not break a client (%s)",
				rec.Code, rec.Body)
		}
	})
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

	target := "/v1/jobs/open?provider_id=" + m.provider.String()
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

	var body struct {
		Status string `json:"status"`
	}
	page := m.feed(t, "")
	if len(page.Data) != 1 {
		t.Fatalf("the feed carries %d jobs, want the Negotiating one — Docs/02 §1 keeps it open "+
			"to eligible bids", len(page.Data))
	}
	if err := json.Unmarshal(page.Data[0], &body); err != nil {
		t.Fatalf("the entry is not JSON: %v", err)
	}
	if body.Status != "negotiating" {
		t.Errorf("status = %q, want negotiating", body.Status)
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
