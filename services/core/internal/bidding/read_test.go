package bidding

import (
	"encoding/json"
	"errors"
	"net/http"
	"testing"

	"github.com/google/uuid"

	"github.com/DulsaraNethmin/Shipper/services/core/internal/pagination"
)

// SHIP-101a — "a provider lists every bid they have placed, grouped by status, paginated, and sees
// no other provider's; the response carries no customer budget in any form."
//
// Four claims, and the third and fourth are the ones with a defect behind them. "Sees no other
// provider's" is Docs/01 §4.3's second privacy rule met on the one endpoint that returns a *set*
// rather than a row somebody named — so the failure would not be a refusal that was forgotten, it
// would be a `WHERE` clause that was never narrow enough. "No budget" is held by the same closed key
// set every other response in this package is held to, which is why this file adds no second list.

// bids reads one page through the service, on the pool, as the handler does.
func (m market) myBids(t *testing.T, provider uuid.UUID, q BidQuery) BidPage {
	t.Helper()

	page, err := m.svc.Bids(t.Context(), m.pool, provider, q)
	if err != nil {
		t.Fatalf("reading %s's bids: %v", provider, err)
	}
	return page
}

func ids(page BidPage) []uuid.UUID {
	out := make([]uuid.UUID, 0, len(page.Bids))
	for _, bid := range page.Bids {
		out = append(out, bid.ID)
	}
	return out
}

// TestAProviderListsTheirOwnBidsNewestFirst is the *Done when*'s first claim.
func TestAProviderListsTheirOwnBidsNewestFirst(t *testing.T) {
	m := newMarket(t)

	first, _, err := m.place(t, m.provider, m.job, offer("key-mine-1"))
	if err != nil {
		t.Fatalf("the first offer: %v", err)
	}
	second := m.publish(t)
	latest, _, err := m.place(t, m.provider, second, offer("key-mine-2"))
	if err != nil {
		t.Fatalf("the second offer: %v", err)
	}

	page := m.myBids(t, m.provider, BidQuery{})
	if got := ids(page); len(got) != 2 || got[0] != latest.ID || got[1] != first.ID {
		t.Fatalf("the list is %v, want %v then %v — newest first, across jobs",
			got, latest.ID, first.ID)
	}
	if page.HasMore {
		t.Error("a two-row list reports another page")
	}
}

// TestAProviderNeverSeesAnotherProvidersBid is Docs/01 §4.3's second privacy rule on the one
// endpoint that answers with a set.
//
// **The mutation this catches is a deleted `WHERE` clause**, not a forgotten refusal: there is no
// identifier a caller supplied for the platform to judge, so the scope *is* the query. Drop
// `provider_id = $1` from [postgresStore.bidsFor] and every other test in this file still passes.
func TestAProviderNeverSeesAnotherProvidersBid(t *testing.T) {
	m := newMarket(t)

	mine, _, err := m.place(t, m.provider, m.job, offer("key-mine-only"))
	if err != nil {
		t.Fatalf("my offer: %v", err)
	}
	rival := m.rival(t, 9101)
	theirs, _, err := m.place(t, rival, m.job, offer("key-theirs-only"))
	if err != nil {
		t.Fatalf("the rival's offer: %v", err)
	}

	page := m.myBids(t, m.provider, BidQuery{})
	if got := ids(page); len(got) != 1 || got[0] != mine.ID {
		t.Fatalf("the list is %v, want only %v — a competitor's price is private", got, mine.ID)
	}

	// And the other way round, so the test cannot pass against a query scoped to the wrong id.
	if got := ids(m.myBids(t, rival, BidQuery{})); len(got) != 1 || got[0] != theirs.ID {
		t.Fatalf("the rival's list is %v, want only %v", got, theirs.ID)
	}
}

// TestACustomersCounterIsInTheProvidersList is the reading read.go argues for.
//
// `bids.provider_id` is the provider a negotiation is *with*, so a customer's counter carries it.
// Including it is what makes the row waiting for an answer visible on a "my bids" screen, and
// `offered_by` is what stops it being mistaken for the provider's own.
func TestACustomersCounterIsInTheProvidersList(t *testing.T) {
	m := newMarket(t)

	placed, _, err := m.place(t, m.provider, m.job, offer("key-mine-countered"))
	if err != nil {
		t.Fatalf("placing: %v", err)
	}
	counter, _, err := m.counter(t, m.customer, m.job, placed.ID, counterOf(40000, "key-mine-counter"))
	if err != nil {
		t.Fatalf("countering: %v", err)
	}

	page := m.myBids(t, m.provider, BidQuery{})
	got := ids(page)
	if len(got) != 2 || got[0] != counter.ID || got[1] != placed.ID {
		t.Fatalf("the list is %v, want the counter then the offer it displaced", got)
	}
	if page.Bids[0].OfferedBy != PartyCustomer {
		t.Errorf("the counter reads offered_by %s, want customer", page.Bids[0].OfferedBy)
	}
	if page.Bids[1].OfferedBy != PartyProvider {
		t.Errorf("the displaced offer reads offered_by %s, want provider", page.Bids[1].OfferedBy)
	}
}

// TestTheListNarrowsToOneStatus is the "grouped by status" half, which the platform serves by
// letting a client ask for one group.
//
// The filter runs in SQL rather than over the page, which is the part worth pinning: applied after
// the cut it would produce short pages and a `has_more` that lied about them.
func TestTheListNarrowsToOneStatus(t *testing.T) {
	m := newMarket(t)

	live, _, err := m.place(t, m.provider, m.job, offer("key-group-live"))
	if err != nil {
		t.Fatalf("the live offer: %v", err)
	}
	second := m.publish(t)
	gone, _, err := m.place(t, m.provider, second, offer("key-group-gone"))
	if err != nil {
		t.Fatalf("the second offer: %v", err)
	}
	if _, err := m.withdraw(t, m.provider, second, gone.ID); err != nil {
		t.Fatalf("withdrawing: %v", err)
	}

	if got := ids(m.myBids(t, m.provider, BidQuery{Status: StatusSubmitted})); len(got) != 1 || got[0] != live.ID {
		t.Errorf("the live group is %v, want only %v", got, live.ID)
	}
	if got := ids(m.myBids(t, m.provider, BidQuery{Status: StatusWithdrawn})); len(got) != 1 || got[0] != gone.ID {
		t.Errorf("the withdrawn group is %v, want only %v", got, gone.ID)
	}
	if got := ids(m.myBids(t, m.provider, BidQuery{Status: StatusAccepted})); len(got) != 0 {
		t.Errorf("the accepted group is %v, want nothing", got)
	}
	if got := ids(m.myBids(t, m.provider, BidQuery{})); len(got) != 2 {
		t.Errorf("the unfiltered list is %v, want both", got)
	}
}

// TestAStatusNobodyIssuesIsRefusedRatherThanAnsweredEmpty keeps a client defect from reading as an
// empty group.
func TestAStatusNobodyIssuesIsRefusedRatherThanAnsweredEmpty(t *testing.T) {
	m := newMarket(t)

	if _, err := m.svc.Bids(t.Context(), m.pool, m.provider, BidQuery{Status: "Haggling"}); err == nil {
		t.Fatal("an unknown status was answered rather than refused")
	}
}

// TestTheListPagesWithoutRepeatingOrDroppingARow is the keyset, and the tie is the point.
//
// Every offer below is written in the same test and can land in the same millisecond, so a cursor
// that carried only `created_at` would repeat or drop a row at exactly the page boundary. That is
// the failure keyset pagination exists to avoid, arriving by a different route.
func TestTheListPagesWithoutRepeatingOrDroppingARow(t *testing.T) {
	m := newMarket(t)

	const offers = 5
	want := map[uuid.UUID]bool{}
	for n := range offers {
		job := m.job
		if n > 0 {
			job = m.publish(t)
		}
		placed, _, err := m.place(t, m.provider, job, offer("key-page-"+string(rune('a'+n))))
		if err != nil {
			t.Fatalf("offer %d: %v", n, err)
		}
		want[placed.ID] = true
	}

	seen := map[uuid.UUID]int{}
	var cursor BidCursor
	for range offers + 2 {
		page := m.myBids(t, m.provider, BidQuery{Limit: 2, After: cursor})
		for _, bid := range page.Bids {
			seen[bid.ID]++
		}
		if !page.HasMore {
			break
		}
		cursor = page.Next
	}

	if len(seen) != offers {
		t.Fatalf("paging saw %d distinct offers, want %d", len(seen), offers)
	}
	for id, times := range seen {
		if times != 1 {
			t.Errorf("%s appeared %d times across the pages", id, times)
		}
		if !want[id] {
			t.Errorf("%s is not one of this provider's offers", id)
		}
	}
}

// TestAPageOverTheMaximumIsClampedRatherThanRefused is `internal/pagination`'s rule, asserted here
// because the service applies it rather than the handler.
func TestAPageOverTheMaximumIsClampedRatherThanRefused(t *testing.T) {
	m := newMarket(t)

	if _, _, err := m.place(t, m.provider, m.job, offer("key-clamp")); err != nil {
		t.Fatalf("placing: %v", err)
	}
	if got := len(m.myBids(t, m.provider, BidQuery{Limit: pagination.MaxLimit + 1000}).Bids); got != 1 {
		t.Errorf("an over-large page returned %d rows", got)
	}
}

// TestTheBidListCarriesNothingOfTheCustomers is CLAUDE.md's budget invariant on the newest
// provider-facing response.
//
// **The whole serialised page is held to a closed key set at every depth**, which is the axis
// SHIP-83 found a source-parsing guard cannot have: a field named `max_price` is a budget and does
// not contain the word. The element keys are [providerBidKeys], the same list the four write
// endpoints and the history are held to — one list rather than two, which is what stops a field
// reaching a provider through the endpoint nobody re-read.
func TestTheBidListCarriesNothingOfTheCustomers(t *testing.T) {
	w := newWire(t)
	w.placed(t, "key-list-budget")

	var budget *float64
	if err := w.pool.QueryRow(t.Context(),
		`SELECT budget FROM jobs WHERE id = $1`, w.job).Scan(&budget); err != nil {
		t.Fatalf("reading the fixture's budget: %v", err)
	}
	if budget == nil {
		t.Fatal("the fixture job has no budget, so this test asserts nothing")
	}

	rec := w.mine(t, w.provider, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /v1/fleet/bids = %d, want 200: %s", rec.Code, rec.Body.String())
	}

	var page struct {
		Data       []map[string]any `json:"data"`
		NextCursor *string          `json:"next_cursor"`
		HasMore    bool             `json:"has_more"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &page); err != nil {
		t.Fatalf("the response is not the collection envelope: %v", err)
	}
	if len(page.Data) != 1 {
		t.Fatalf("the page holds %d rows, want the one offer this test placed", len(page.Data))
	}

	var envelope map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("the response is not an object: %v", err)
	}
	for key := range envelope {
		switch key {
		case "data", "next_cursor", "has_more":
		default:
			t.Errorf("the envelope carries %q, which Docs/10 §4.5 does not describe", key)
		}
	}
	for key := range page.Data[0] {
		if !providerBidKeys[key] {
			t.Errorf("a listed bid carries %q. If it belongs there, add it to providerBidKeys and "+
				"to the Bid schema — and read Docs/01 §4.3 first", key)
		}
	}
}

// TestTheListNeedsNoIdempotencyKeyAndNoTransaction is the read-only half of SHIP-15's rule, and the
// pool half of Docs/10 §3.2's.
//
// It takes no lock and opens no transaction: a row that changed under it would produce an older row
// beside a newer one rather than an inconsistent one. Asserted by calling it on the pool, which is
// what the handler passes and what [Service.ReviseBid] refuses.
func TestTheListNeedsNoIdempotencyKeyAndNoTransaction(t *testing.T) {
	m := newMarket(t)

	if _, _, err := m.place(t, m.provider, m.job, offer("key-list-pool")); err != nil {
		t.Fatalf("placing: %v", err)
	}
	if _, err := m.svc.Bids(t.Context(), m.pool, m.provider, BidQuery{}); err != nil {
		t.Fatalf("reading the list on the pool: %v", err)
	}
	if _, err := m.svc.Bids(t.Context(), m.pool, uuid.Nil, BidQuery{}); !errors.Is(err, ErrJobNotOffered) {
		t.Errorf("a list naming no provider answered %v", err)
	}
}
