package bidding

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/DulsaraNethmin/Shipper/services/core/internal/db"
	"github.com/DulsaraNethmin/Shipper/services/core/internal/events"
	"github.com/DulsaraNethmin/Shipper/services/core/internal/fleet"
	"github.com/DulsaraNethmin/Shipper/services/core/internal/httpx"
	"github.com/DulsaraNethmin/Shipper/services/core/internal/validate"
)

// SHIP-84's *Done when*: "A verified, eligible provider can bid once per job with price and timing."
//
// Four claims, and each has a test that fails if it stops holding:
//
//	verified, eligible   TestOnlyAVerifiedEligibleProviderCanBid, through the real fleet filter
//	can bid              TestAVerifiedEligibleProviderPlacesABid
//	once per job         TestASecondLiveOfferIsRefusedByTheDatabase, and the concurrent case below
//	price and timing     TestTheOfferCarriesPriceAndTiming, plus the validation table
//
// The fifth claim is not in the sentence and is in the ticket: **a retry is not a second bid**.
// TestARetryReturnsTheOriginalOfferAndWritesNothing and
// TestConcurrentRequestsUnderOneKeyPlaceExactlyOneBid are that, and the second is the one a
// check-then-write implementation fails.

// --- the acceptance criterion -------------------------------------------------------------------

// TestAVerifiedEligibleProviderPlacesABid is the positive half, and it has to come first.
//
// Every refusal below is only meaningful because this passes: an implementation that refused
// everything would satisfy each negative test and be entirely broken.
func TestAVerifiedEligibleProviderPlacesABid(t *testing.T) {
	m := newMarket(t)

	bid, created, err := m.place(t, m.provider, m.job, offer("key-happy"))
	if err != nil {
		t.Fatalf("PlaceBid() = %v, want a bid", err)
	}
	if !created {
		t.Error("the first offer reports itself as a replay")
	}

	if bid.ID == uuid.Nil {
		t.Error("the bid has no identifier")
	}
	if bid.JobID != m.job {
		t.Errorf("the bid is against %s, want %s", bid.JobID, m.job)
	}
	if bid.ProviderID != m.provider {
		t.Errorf("the bid is from %s, want %s", bid.ProviderID, m.provider)
	}
	if bid.Status != StatusSubmitted {
		t.Errorf("the bid is %q, want Submitted — a placed offer is live, not a draft", bid.Status)
	}

	// The row, not the answer the service gave about itself.
	var (
		status  string
		amount  float64
		pickup  time.Time
		deliver time.Time
		key     string
	)
	if err := m.pool.QueryRow(t.Context(),
		`SELECT status, amount, pickup_at, deliver_by, idempotency_key FROM bids WHERE id = $1`,
		bid.ID).Scan(&status, &amount, &pickup, &deliver, &key); err != nil {
		t.Fatalf("reading the stored bid: %v", err)
	}
	if status != "Submitted" {
		t.Errorf("the stored status is %q, want Submitted", status)
	}
	if amount != 450.00 {
		t.Errorf("the stored amount is %v, want 450.00", amount)
	}
	if key != "key-happy" {
		t.Errorf("the stored key is %q, want key-happy", key)
	}
}

// TestTheOfferCarriesPriceAndTiming is the "with price and timing" half, read back out of the
// database rather than off the struct that was passed in.
//
// **The amount is the part worth checking through a round trip.** Go holds cents in an int64 and
// PostgreSQL holds dollars in numeric(12,2), so the value crosses a conversion in each direction
// (Docs/10 §3.3). A conversion that used a float would survive 45000 and lose 45001; a conversion
// applied in only one direction would return a hundredth of the price. Both are checked.
func TestTheOfferCarriesPriceAndTiming(t *testing.T) {
	m := newMarket(t)

	// Deliberately not a round number of dollars. 450.00 divides and multiplies cleanly enough to
	// hide a defect that 450.01 exposes.
	o := offer("key-timing")
	o.AmountCents = 45001

	bid, _, err := m.place(t, m.provider, m.job, o)
	if err != nil {
		t.Fatalf("PlaceBid() = %v", err)
	}

	if bid.AmountCents != 45001 {
		t.Errorf("the offer came back as %d cents, want 45001", bid.AmountCents)
	}
	if !bid.PickupAt.Equal(o.PickupAt) {
		t.Errorf("pickup_at is %s, want %s", bid.PickupAt, o.PickupAt)
	}
	if !bid.DeliverBy.Equal(o.DeliverBy) {
		t.Errorf("deliver_by is %s, want %s", bid.DeliverBy, o.DeliverBy)
	}
	if bid.Message != "Can collect from the loading dock." {
		t.Errorf("the message is %q", bid.Message)
	}

	// The column, which is where a one-directional conversion would show.
	var stored float64
	if err := m.pool.QueryRow(t.Context(),
		`SELECT amount FROM bids WHERE id = $1`, bid.ID).Scan(&stored); err != nil {
		t.Fatalf("reading the stored amount: %v", err)
	}
	if stored != 450.01 {
		t.Errorf("the column holds %v, want 450.01 — cents and dollars have parted company", stored)
	}
}

// TestABidIsAcceptedOnBothBiddableStatuses is the trap Docs/02 §1 sets and this ticket was warned
// about by name.
//
// §1 defines Negotiating as "one or more active bids or counter-offers exist; **job remains open to
// eligible bids**", and adds the sentence that settles it: "'Negotiating' is a useful presentation
// status. Technically, the job remains available for eligible bids unless the customer closes it or
// awards a bid."
//
// **Nothing can reach Negotiating until SHIP-90**, so an implementation that accepted only Open
// would pass every other test in this file and every check in `make verify`. It would surface months
// later as jobs silently refusing bids the moment somebody negotiated — which reads as a bidding bug
// rather than as a missing string in a filter. This is the test that would have caught it, and it
// moves the job there by hand precisely because no endpoint can yet.
//
// The property is fleet's — `biddableStatuses` in eligibility.go is the list, and this package reads
// it through the port rather than holding a second copy. That is the arrangement being tested as
// much as the statuses are.
func TestABidIsAcceptedOnBothBiddableStatuses(t *testing.T) {
	for _, status := range []string{"Open", "Negotiating"} {
		t.Run(status, func(t *testing.T) {
			m := newMarket(t)
			if status != "Open" {
				transition(t, m.pool, m.job, m.customer, "Open", status)
			}

			bid, created, err := m.place(t, m.provider, m.job, offer("key-"+status))
			if err != nil {
				t.Fatalf("a job at %s refused a bid: %v.\n"+
					"Docs/02 §1 makes BOTH statuses biddable — a Negotiating job "+
					"\"remains open to eligible bids\". A filter accepting only Open passes every "+
					"other test today and breaks the marketplace the moment SHIP-90 lands.",
					status, err)
			}
			if !created || bid.Status != StatusSubmitted {
				t.Errorf("a bid on a %s job is %q, created=%v", status, bid.Status, created)
			}
		})
	}
}

// TestAJobThatIsNotBiddableRefusesTheOffer is the other side of the same coin.
//
// "Both statuses" must not mean "any status". Draft is the one that matters most — a draft is
// visible to nobody but its owner (Docs/02 §1), so a bid on one would be a provider acting on a job
// that has not been published.
func TestAJobThatIsNotBiddableRefusesTheOffer(t *testing.T) {
	for _, status := range []string{"Awarded", "Cancelled"} {
		t.Run(status, func(t *testing.T) {
			m := newMarket(t)
			transition(t, m.pool, m.job, m.customer, "Open", status)

			if _, _, err := m.place(t, m.provider, m.job, offer("key-"+status)); !errors.Is(err, ErrJobNotOffered) {
				t.Fatalf("a bid on a %s job = %v, want ErrJobNotOffered", status, err)
			}
			if n := m.bids(t, m.provider, m.job); n != 0 {
				t.Errorf("the refusal left %d rows behind", n)
			}
		})
	}

	t.Run("Draft", func(t *testing.T) {
		m := newMarket(t)

		// A separate job, because a published one cannot go back to Draft.
		draft, err := uuid.NewV7()
		if err != nil {
			t.Fatalf("generating an id: %v", err)
		}
		exec(t, m.pool, `
			INSERT INTO jobs (id, customer_id, pickup_suburb, pickup_state, pickup_postcode)
			VALUES ($1, $2, 'Richmond', 'VIC', '3121')`, draft, m.customer)

		if _, _, err := m.place(t, m.provider, draft, offer("key-draft")); !errors.Is(err, ErrJobNotOffered) {
			t.Fatalf("a bid on a Draft = %v, want ErrJobNotOffered", err)
		}
	})
}

// TestOnlyAVerifiedEligibleProviderCanBid is the first three words of the *Done when*, one subtest
// per filter.
//
// Each starts from the world every filter accepts and breaks exactly one thing, which is what makes
// a failure legible. The filters are Docs/01 §4.3's four, and they run through the real
// `*fleet.Service` — this test is asserting that SHIP-81's answer is the one that governs a bid, not
// that this package has reimplemented it.
//
// **The customer is in the table on purpose.** CLAUDE.md's "no authorisation decision on the device"
// means a customer reaching this endpoint must be *refused*, not merely hidden from — and a customer
// is refused by the same filter that refuses an unverified provider, which is why there is no
// separate role check anywhere in this package.
func TestOnlyAVerifiedEligibleProviderCanBid(t *testing.T) {
	cases := []struct {
		filter string
		what   string
		break_ func(t *testing.T, m *market)
	}{
		{
			filter: "verification state",
			what:   "the provider's email is no longer verified",
			break_: func(t *testing.T, m *market) {
				exec(t, m.pool, `UPDATE users SET email_verified_at = NULL WHERE id = $1`, m.provider)
			},
		},
		{
			filter: "verification state",
			what:   "the provider's account has been restricted",
			break_: func(t *testing.T, m *market) {
				exec(t, m.pool, `UPDATE users SET status = 'restricted' WHERE id = $1`, m.provider)
			},
		},
		{
			filter: "service area",
			what:   "the provider withdrew from the state the job picks up in",
			break_: func(t *testing.T, m *market) {
				declare(t, m.pool, m.provider, "NSW")
			},
		},
		{
			filter: "vehicle capability",
			what:   "the provider's only vehicle left service",
			break_: func(t *testing.T, m *market) {
				exec(t, m.pool, `UPDATE vehicles SET deactivated_at = now() WHERE provider_id = $1`, m.provider)
			},
		},
		{
			filter: "vehicle capability",
			what:   "the goods are heavier than anything the provider runs",
			break_: func(t *testing.T, m *market) {
				exec(t, m.pool, `UPDATE jobs SET weight_kg = 9000 WHERE id = $1`, m.job)
			},
		},
		{
			filter: "job status",
			what:   "the job's deadline has passed, though the sweep has not reached it",
			break_: func(t *testing.T, m *market) {
				exec(t, m.pool, `UPDATE jobs SET expires_at = $2 WHERE id = $1`,
					m.job, testInstant.Add(-time.Hour))
			},
		},
		{
			filter: "the account itself",
			what:   "the caller is a customer rather than a provider",
			break_: func(t *testing.T, m *market) {
				m.provider = newCustomer(t, m.pool, "bid-intruder@example.com", "+61400000842")
			},
		},
		{
			filter: "the account itself",
			what:   "the caller is the job's own customer",
			break_: func(t *testing.T, m *market) { m.provider = m.customer },
		},
	}

	for _, c := range cases {
		t.Run(c.filter+" — "+c.what, func(t *testing.T) {
			m := newMarket(t)

			// The happy path first, so a subtest that fails is telling you about the filter rather
			// than about a fixture that never worked.
			if _, _, err := m.place(t, m.provider, m.job, offer("key-before")); err != nil {
				t.Fatalf("the fixture could not bid before anything was broken: %v", err)
			}

			c.break_(t, &m)

			_, _, err := m.place(t, m.provider, m.job, offer("key-after"))
			if !errors.Is(err, ErrJobNotOffered) {
				t.Fatalf("%s: the offer was not refused (%v)", c.what, err)
			}
		})
	}
}

// --- "once per job" ------------------------------------------------------------------------------

// TestASecondLiveOfferIsRefusedByTheDatabase is the "once per job" half of the *Done when*.
//
// **There is no SELECT in this package asking whether the provider has already bid**, and that is
// the design rather than an omission. A read followed by a write is correct in a single-threaded
// reading and wrong under two taps on one phone: the window between them is small and it is not
// zero, and what arrives in it is a job carrying two live prices from one provider.
//
// uq_bids_one_submitted_per_provider_per_job is the whole of the guarantee, and this test is what
// says the refusal reaches a caller as something they can act on rather than as a constraint name.
func TestASecondLiveOfferIsRefusedByTheDatabase(t *testing.T) {
	m := newMarket(t)

	if _, _, err := m.place(t, m.provider, m.job, offer("key-first")); err != nil {
		t.Fatalf("the first offer: %v", err)
	}

	// A *different* key, so this is a second offer rather than a retry.
	_, _, err := m.place(t, m.provider, m.job, offer("key-second"))
	if !errors.Is(err, ErrAlreadyBid) {
		t.Fatalf("a second offer = %v, want ErrAlreadyBid", err)
	}
	if n := m.bids(t, m.provider, m.job); n != 1 {
		t.Errorf("the job carries %d bids from this provider, want exactly 1", n)
	}
}

// TestTheRuleIsPerProviderAndPerJob bounds the previous test in both directions.
//
// "Once per job" is once per *provider* per job — a marketplace where the second provider to bid was
// refused would be no marketplace — and it does not reach across jobs.
func TestTheRuleIsPerProviderAndPerJob(t *testing.T) {
	m := newMarket(t)

	if _, _, err := m.place(t, m.provider, m.job, offer("key-mine")); err != nil {
		t.Fatalf("the first provider's offer: %v", err)
	}

	other := newVerifiedProvider(t, m.pool, "bid-other@example.com", "+61400000843")
	declare(t, m.pool, other, "VIC")
	addVehicle(t, m.pool, other, "BID002")

	if _, _, err := m.place(t, other, m.job, offer("key-theirs")); err != nil {
		t.Fatalf("a second provider was refused a job the first had bid on: %v", err)
	}

	second := m.publish(t)
	if _, _, err := m.place(t, m.provider, second, offer("key-mine-again")); err != nil {
		t.Fatalf("the first provider was refused a different job: %v", err)
	}
}

// TestAWithdrawnOfferCanBeReplaced records a decision rather than discovering one.
//
// The index is partial on `status = 'Submitted'`, so an offer that has left that status stops
// blocking a new one. Docs/01 §4.2 gives a provider "place, update, and withdraw a bid until it is
// accepted or expires" and forbids nothing about what follows a withdrawal; refusing the second bid
// would mean a fat-fingered price is a job the provider can never bid on again.
//
// SHIP-84 wrote this against a hand-set status, because the withdrawal endpoint did not exist and the
// property was worth pinning early: "SHIP-86 should find this already true." **It did**, and the
// hand-set status is now [Service.WithdrawBid] — which turns this from a claim about a partial index
// into a claim about the sequence a provider actually performs.
func TestAWithdrawnOfferCanBeReplaced(t *testing.T) {
	m := newMarket(t)

	first, _, err := m.place(t, m.provider, m.job, offer("key-withdrawn"))
	if err != nil {
		t.Fatalf("the first offer: %v", err)
	}
	if _, err := m.withdraw(t, m.provider, m.job, first.ID); err != nil {
		t.Fatalf("withdrawing the first offer: %v", err)
	}

	second, created, err := m.place(t, m.provider, m.job, offer("key-replacement"))
	if err != nil {
		t.Fatalf("bidding again after withdrawing = %v, want a new offer", err)
	}
	if !created || second.ID == first.ID {
		t.Errorf("the replacement is not a new row (created=%v, id=%s)", created, second.ID)
	}
	if n := m.bids(t, m.provider, m.job); n != 2 {
		t.Errorf("the job carries %d rows, want 2 — the withdrawn offer must survive as record", n)
	}
}

// --- the retry, which is not a second bid ---------------------------------------------------------

// TestARetryReturnsTheOriginalOfferAndWritesNothing is the mechanism SHIP-15's middleware does not
// provide.
//
// The middleware replays a *response* while its Redis entry lives; this is what happens when the
// entry does not — a TTL expiry, an eviction, a failover, or simply a phone that was out of signal
// for longer than any TTL worth setting. The request runs a second time, all the way to the table,
// and the only thing between it and a refusal is uq_bids_idempotency plus the row it points at.
//
// **The alternative failure is the one worth naming**: without the stored key, this request would
// meet uq_bids_one_submitted_per_provider_per_job instead and be answered "you already have a live
// offer" — a 409 for a request that actually succeeded. The client would show a failure for a bid
// that is live and awaiting an answer.
func TestARetryReturnsTheOriginalOfferAndWritesNothing(t *testing.T) {
	m := newMarket(t)

	first, created, err := m.place(t, m.provider, m.job, offer("key-retried"))
	if err != nil || !created {
		t.Fatalf("the first offer: %v (created=%v)", err, created)
	}

	// The same key, and a body that differs — a retry from a phone whose clock has moved, or whose
	// user edited the form before the connection came back. The stored offer wins: a retry returns
	// what was recorded rather than overwriting it with a second attempt's values.
	changed := offer("key-retried")
	changed.AmountCents = 99900
	changed.Message = "different"

	again, created, err := m.place(t, m.provider, m.job, changed)
	if err != nil {
		t.Fatalf("the retry = %v, want the original offer", err)
	}
	if created {
		t.Error("the retry reports itself as a new offer")
	}
	if again.ID != first.ID {
		t.Errorf("the retry returned %s, want the original %s", again.ID, first.ID)
	}
	if again.AmountCents != 45000 {
		t.Errorf("the retry repriced the offer to %d — a retry must not overwrite", again.AmountCents)
	}
	if n := m.bids(t, m.provider, m.job); n != 1 {
		t.Errorf("the retry left %d rows, want 1", n)
	}
}

// TestConcurrentRequestsUnderOneKeyPlaceExactlyOneBid is the case a check-then-write implementation
// fails approximately always.
//
// Eight goroutines, one key, eight transactions. The middleware would refuse seven of them with
// `idempotency_request_in_progress` and never let them reach the service, which is exactly why this
// bypasses it: two API instances, or a cache miss on both sides of a retry, is the same race with
// nothing in front of it.
//
// The assertion is that exactly one believes it created something, that they all answer with the
// same bid, and that the table holds one row. `ON CONFLICT … DO NOTHING` is what makes that true —
// the second transaction waits on the first's speculative insertion and is then told the answer,
// rather than both reading "no bid yet" and both writing one.
func TestConcurrentRequestsUnderOneKeyPlaceExactlyOneBid(t *testing.T) {
	m := newMarket(t)

	const requests = 8

	var (
		wg      sync.WaitGroup
		mu      sync.Mutex
		created int
		ids     = map[uuid.UUID]bool{}
		fails   []error
	)

	wg.Add(requests)
	for range requests {
		go func() {
			defer wg.Done()

			var (
				bid   Bid
				fresh bool
			)
			err := db.InTx(t.Context(), m.pool, func(ctx context.Context, r db.Runner) error {
				var err error
				bid, fresh, err = m.svc.PlaceBid(ctx, r, m.provider, m.job, offer("key-raced"))
				return err
			})

			mu.Lock()
			defer mu.Unlock()
			if err != nil {
				fails = append(fails, err)
				return
			}
			if fresh {
				created++
			}
			ids[bid.ID] = true
		}()
	}
	wg.Wait()

	if len(fails) > 0 {
		t.Fatalf("%d of %d concurrent requests failed, first: %v", len(fails), requests, fails[0])
	}
	if created != 1 {
		t.Errorf("%d of %d requests believe they placed the offer, want exactly 1", created, requests)
	}
	if len(ids) != 1 {
		t.Errorf("the requests answered with %d different bids, want 1", len(ids))
	}
	if n := m.bids(t, m.provider, m.job); n != 1 {
		t.Errorf("the table holds %d bids, want exactly 1", n)
	}
}

// TestAnOfferWithNoKeyIsRefusedBeforeAnythingIsWritten covers the path the middleware makes
// unreachable.
//
// The key is not merely how a retry is absorbed here: it is a column on the row, and a bid stored
// with no key is a bid a later retry cannot be matched to — which would then be refused as a second
// offer. So the service refuses it rather than trusting that nothing will ever call it without one.
func TestAnOfferWithNoKeyIsRefusedBeforeAnythingIsWritten(t *testing.T) {
	m := newMarket(t)

	o := offer("")
	if _, _, err := m.place(t, m.provider, m.job, o); !errors.Is(err, ErrNoIdempotencyKey) {
		t.Fatalf("PlaceBid() with no key = %v, want ErrNoIdempotencyKey", err)
	}
	if n := m.bids(t, m.provider, m.job); n != 0 {
		t.Errorf("a keyless offer wrote %d rows", n)
	}
}

// --- validation ------------------------------------------------------------------------------------

// TestAnOfferIsRefusedFieldByField is "with price and timing" read as a requirement rather than as a
// description.
//
// Each case names the field it expects in `error.details`, because a validation error that does not
// say which field it is about is one a client cannot render beside an input.
func TestAnOfferIsRefusedFieldByField(t *testing.T) {
	cases := []struct {
		name  string
		field string
		code  httpx.Code
		spoil func(o *Offer)
	}{
		{
			name: "no price", field: "amount_cents", code: validate.CodeRequired,
			spoil: func(o *Offer) { o.AmountCents = 0 },
		},
		{
			name: "a negative price", field: "amount_cents", code: validate.CodeRequired,
			spoil: func(o *Offer) { o.AmountCents = -1 },
		},
		{
			name: "a price above the bound", field: "amount_cents", code: validate.CodeOutOfRange,
			spoil: func(o *Offer) { o.AmountCents = maxOfferCents + 1 },
		},
		{
			name: "no collection time", field: "pickup_at", code: validate.CodeRequired,
			spoil: func(o *Offer) { o.PickupAt = time.Time{} },
		},
		{
			name: "a collection time in the past", field: "pickup_at", code: validate.CodeOutOfRange,
			spoil: func(o *Offer) { o.PickupAt = testInstant.Add(-time.Hour) },
		},
		{
			name: "a year typed wrong", field: "pickup_at", code: validate.CodeOutOfRange,
			spoil: func(o *Offer) { o.PickupAt = testInstant.AddDate(3, 0, 0) },
		},
		{
			name: "no delivery time", field: "deliver_by", code: validate.CodeRequired,
			spoil: func(o *Offer) { o.DeliverBy = time.Time{} },
		},
		{
			name: "delivery before collection", field: "deliver_by", code: validate.CodeOutOfRange,
			spoil: func(o *Offer) { o.DeliverBy = o.PickupAt.Add(-time.Hour) },
		},
		{
			name: "delivery at the moment of collection", field: "deliver_by", code: validate.CodeOutOfRange,
			spoil: func(o *Offer) { o.DeliverBy = o.PickupAt },
		},
		{
			name: "a message longer than the column", field: "message", code: validate.CodeTooLong,
			spoil: func(o *Offer) { o.Message = strings.Repeat("a", maxMessageLen+1) },
		},
	}

	m := newMarket(t)

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			o := offer("key-" + strings.ReplaceAll(c.name, " ", "-"))
			c.spoil(&o)

			_, _, err := m.place(t, m.provider, m.job, o)
			if err == nil {
				t.Fatalf("%s was accepted", c.name)
			}

			var apiErr *httpx.Error
			if !errors.As(err, &apiErr) {
				t.Fatalf("PlaceBid() = %v, want a field error", err)
			}

			var found bool
			for _, problem := range apiErr.Details {
				if problem.Field == c.field {
					found = true
					if problem.Code != c.code {
						t.Errorf("%s is reported as %q, want %q", c.field, problem.Code, c.code)
					}
				}
			}
			if !found {
				t.Errorf("no detail names %q: %+v", c.field, apiErr.Details)
			}
		})
	}

	if n := m.bids(t, m.provider, m.job); n != 0 {
		t.Errorf("the refusals left %d rows behind", n)
	}
}

// TestTheJobsOwnWindowsDoNotBoundTheOffer records a decision that would otherwise look like an
// omission.
//
// A provider offering a pickup outside the window the customer asked for is making an offer the
// customer is free to decline. Docs/01 §4.3's answer to bids that miss is better job detail rather
// than a platform that refuses them, and a good many jobs state no window at all. Migration 000501
// records the same split from the schema's side.
func TestTheJobsOwnWindowsDoNotBoundTheOffer(t *testing.T) {
	m := newMarket(t)

	exec(t, m.pool, `UPDATE jobs SET pickup_window_start = $2, pickup_window_end = $3 WHERE id = $1`,
		m.job, testInstant.Add(2*time.Hour), testInstant.Add(6*time.Hour))

	// Two days after the window the customer asked for.
	if _, _, err := m.place(t, m.provider, m.job, offer("key-outside")); err != nil {
		t.Fatalf("an offer outside the customer's window was refused: %v.\n"+
			"That is a commercial decision for the customer, not a validation rule.", err)
	}
}

// TestTheMessageIsStoredAsWrittenApartFromWhitespace pins the one normalisation an offer gets.
//
// A provider composing on a phone produces double spaces and trailing newlines, and two offers
// differing only in those are one offer. Nothing else is touched: the text is the provider's own
// words and the platform is not an editor.
func TestTheMessageIsStoredAsWrittenApartFromWhitespace(t *testing.T) {
	m := newMarket(t)

	o := offer("key-message")
	o.Message = "  Loading   dock\n\naccess only.  "

	bid, _, err := m.place(t, m.provider, m.job, o)
	if err != nil {
		t.Fatalf("PlaceBid() = %v", err)
	}
	if bid.Message != "Loading dock access only." {
		t.Errorf("the message is %q, want %q", bid.Message, "Loading dock access only.")
	}
}

// TestAnOfferWithNoMessageStoresNULLRatherThanEmpty keeps the column's meaning honest.
//
// ck_bids_message refuses an empty string, so "" and NULL cannot be confused in either direction —
// which is only true if the write converts one to the other rather than relying on it.
func TestAnOfferWithNoMessageStoresNULLRatherThanEmpty(t *testing.T) {
	m := newMarket(t)

	o := offer("key-nomessage")
	o.Message = "   "

	bid, _, err := m.place(t, m.provider, m.job, o)
	if err != nil {
		t.Fatalf("PlaceBid() = %v", err)
	}
	if bid.Message != "" {
		t.Errorf("the message came back as %q, want empty", bid.Message)
	}

	var stored *string
	if err := m.pool.QueryRow(t.Context(),
		`SELECT message FROM bids WHERE id = $1`, bid.ID).Scan(&stored); err != nil {
		t.Fatalf("reading the stored message: %v", err)
	}
	if stored != nil {
		t.Errorf("the column holds %q, want NULL", *stored)
	}
}

// --- the port ---------------------------------------------------------------------------------------

// TestAnEligibilityFailureIsNotARefusal separates the two things the port can answer.
//
// A filter that says "no" is an answer and produces a 404. A filter that could not *reach* an answer
// — the database is gone — is a failure, and answering it with "no such job" would tell a provider
// their job had vanished during an outage. The distinction costs one branch and is the difference
// between a retryable 503 and a permanent-looking 404.
func TestAnEligibilityFailureIsNotARefusal(t *testing.T) {
	m := newMarket(t)
	m.svc = NewService(
		events.NewOutbox(),
		refusing{err: errors.New("the filter could not run")},
		newTestNegotiation(m.svc.clock),
		newTestAwarding(m.svc.clock),
		m.svc.clock,
	)

	_, _, err := m.place(t, m.provider, m.job, offer("key-broken"))
	if err == nil {
		t.Fatal("a broken eligibility filter placed a bid")
	}
	if errors.Is(err, ErrJobNotOffered) {
		t.Error("a filter that failed was reported as a filter that said no")
	}
	if !strings.Contains(err.Error(), "the filter could not run") {
		t.Errorf("the cause was lost: %v", err)
	}
}

// TestABidNamingNoJobOrNoProviderIsRefused covers the two zero values, which reach the service only
// through a programming mistake and must not reach the database at all.
//
// A uuid.Nil job would otherwise be handed to the eligibility filter, and a uuid.Nil provider to a
// foreign key.
func TestABidNamingNoJobOrNoProviderIsRefused(t *testing.T) {
	m := newMarket(t)

	for name, call := range map[string]func() error{
		"no provider": func() error { _, _, err := m.place(t, uuid.Nil, m.job, offer("k1")); return err },
		"no job":      func() error { _, _, err := m.place(t, m.provider, uuid.Nil, offer("k2")); return err },
	} {
		t.Run(name, func(t *testing.T) {
			if err := call(); !errors.Is(err, ErrJobNotOffered) {
				t.Fatalf("%s = %v, want ErrJobNotOffered", name, err)
			}
		})
	}
}

// --- the invariant this domain exists to protect ------------------------------------------------

// TestPlacingABidNeverMovesTheJob is the ticket next door, asserted here so that it stays next door.
//
// Docs/02 §2 has `Open → Negotiating` on "first bid or counter-offer submitted", and that is
// **SHIP-90's**, which depends on SHIP-87 and SHIP-57. Moving the job here would also be a status
// change, and every status change in this platform leaves a job_status_history row written by one
// guarded function (Docs/02 §2, CLAUDE.md).
//
// So the assertion is in both directions: the job is where it was, and nothing was written to its
// history. The second half is what would catch a move made through some other path.
func TestPlacingABidNeverMovesTheJob(t *testing.T) {
	m := newMarket(t)

	var before int
	if err := m.pool.QueryRow(t.Context(),
		`SELECT count(*) FROM job_status_history WHERE job_id = $1`, m.job).Scan(&before); err != nil {
		t.Fatalf("counting history: %v", err)
	}

	if _, _, err := m.place(t, m.provider, m.job, offer("key-nomove")); err != nil {
		t.Fatalf("PlaceBid() = %v", err)
	}

	var (
		status string
		after  int
	)
	if err := m.pool.QueryRow(t.Context(),
		`SELECT j.status, (SELECT count(*) FROM job_status_history WHERE job_id = j.id)
		   FROM jobs j WHERE j.id = $1`, m.job).Scan(&status, &after); err != nil {
		t.Fatalf("reading the job: %v", err)
	}

	if status != "Open" {
		t.Errorf("the job is %q after a bid, want Open — Open → Negotiating is SHIP-90's", status)
	}
	if after != before {
		t.Errorf("placing a bid wrote %d job_status_history rows", after-before)
	}
}

// TestTheGoStatusesMatchTheDatabaseConstraint is the pairing Docs/10 §3.4 requires of every
// enumeration held in two places, extended to the wire form this ticket added.
//
// model_test.go already holds [Statuses] to ck_bids_status. This adds the third copy: every wire
// string has to be distinct, stable, and derivable — a client already branching on `submitted`
// cannot have it renamed underneath it.
func TestTheWireFormsAreStableAndDistinct(t *testing.T) {
	want := map[Status]string{
		StatusDraft:      "draft",
		StatusSubmitted:  "submitted",
		StatusCountered:  "countered",
		StatusAccepted:   "accepted",
		StatusRejected:   "rejected",
		StatusWithdrawn:  "withdrawn",
		StatusExpired:    "expired",
		StatusSuperseded: "superseded",
	}

	if len(want) != len(Statuses) {
		t.Fatalf("this test names %d statuses and Docs/02 §4 has %d", len(want), len(Statuses))
	}

	seen := map[string]Status{}
	for _, status := range Statuses {
		wire := status.Wire()
		if wire != want[status] {
			t.Errorf("%s reaches the wire as %q, want %q — this string is published", status, wire, want[status])
		}
		if first, clash := seen[wire]; clash {
			t.Errorf("%s and %s both reach the wire as %q", first, status, wire)
		}
		seen[wire] = status
	}
}

// --- SHIP-85: revising an offer -------------------------------------------------------------------

// SHIP-85's *Done when*: "A provider can revise their own active bid."
//
// Three claims, and each has a test that fails if it stops holding:
//
//	a provider can revise   TestAProviderRevisesTheirOwnActiveBid
//	their own               TestOnlyTheBidsOwnerCanReviseIt, and the wrong-job pairing beside it
//	active                  TestAnAcceptedBidCannotBeRevised, TestAClosedOfferCannotBeRevised
//
// The fourth claim is not in the sentence and follows from the row being shared with SHIP-84:
// **a revision must not consume the key the offer was placed under**.
// TestARevisionDoesNotConsumeThePlacementsKey is that, and it is the test a `SET … idempotency_key = $n`
// fails.

// TestAProviderRevisesTheirOwnActiveBid is the acceptance criterion, read back out of the table.
//
// The positive half comes first for the reason SHIP-84's does: an implementation that refused
// everything would satisfy every refusal below and be entirely broken.
//
// **Four things are asserted to have changed and four to have stayed**, because a revision is defined
// as much by what it leaves alone. The identifier, the status, the placement key and the job are what
// make this the same offer at a new number rather than a second offer.
func TestAProviderRevisesTheirOwnActiveBid(t *testing.T) {
	m := newMarket(t)

	placed, _, err := m.place(t, m.provider, m.job, offer("key-revise-placed"))
	if err != nil {
		t.Fatalf("placing the offer: %v", err)
	}
	before := m.row(t, placed.ID)

	pickup := testInstant.Add(72 * time.Hour)
	deliver := testInstant.Add(80 * time.Hour)

	revised, err := m.revise(t, m.provider, m.job, placed.ID, Revision{
		AmountCents: ptr(int64(39900)),
		PickupAt:    &pickup,
		DeliverBy:   &deliver,
		Message:     ptr("Two people and a tail lift."),
	})
	if err != nil {
		t.Fatalf("ReviseBid() = %v, want the revised offer", err)
	}

	if revised.AmountCents != 39900 {
		t.Errorf("the revised amount is %d, want 39900", revised.AmountCents)
	}
	if !revised.PickupAt.Equal(pickup) {
		t.Errorf("pickup_at is %s, want %s", revised.PickupAt, pickup)
	}
	if !revised.DeliverBy.Equal(deliver) {
		t.Errorf("deliver_by is %s, want %s", revised.DeliverBy, deliver)
	}
	if revised.Message != "Two people and a tail lift." {
		t.Errorf("the message is %q", revised.Message)
	}

	// What must not have moved. The status in particular: a revised bid is the same live offer, and
	// moving it out of Submitted would leave uq_bids_one_submitted_per_provider_per_job's predicate
	// and open a window in which a second offer would be accepted.
	if revised.ID != placed.ID {
		t.Errorf("the revision produced a new row (%s, was %s) — a revision is not a counter-offer",
			revised.ID, placed.ID)
	}
	if revised.Status != StatusSubmitted {
		t.Errorf("the revised bid is %q, want Submitted", revised.Status)
	}
	if revised.JobID != m.job {
		t.Errorf("the revised bid is against %s, want %s", revised.JobID, m.job)
	}
	if n := m.bids(t, m.provider, m.job); n != 1 {
		t.Errorf("the job carries %d rows from this provider, want 1 — a revision writes no second row", n)
	}

	// The row, which is where a conversion applied in one direction only would show.
	after := m.row(t, placed.ID)
	if after.amount != 399.00 {
		t.Errorf("the column holds %v, want 399.00 — cents and dollars have parted company", after.amount)
	}
	if after.status != "Submitted" {
		t.Errorf("the stored status is %q, want Submitted", after.status)
	}
	if after.key != before.key {
		t.Errorf("the stored key changed from %q to %q", before.key, after.key)
	}
	if !after.updatedAt.After(before.updatedAt) {
		t.Errorf("updated_at did not move (%s to %s) — bids_set_updated_at should have run",
			before.updatedAt, after.updatedAt)
	}
}

// TestARevisionChangesOnlyWhatItNames is the pointer semantics, which is the whole reason [Revision]
// has a different shape from [Offer].
//
// A provider dropping their price names the price. Restating two timestamps they are not changing is
// how a client eventually sends one of them wrong.
func TestARevisionChangesOnlyWhatItNames(t *testing.T) {
	m := newMarket(t)

	placed, _, err := m.place(t, m.provider, m.job, offer("key-revise-partial"))
	if err != nil {
		t.Fatalf("placing the offer: %v", err)
	}

	revised, err := m.revise(t, m.provider, m.job, placed.ID, Revision{AmountCents: ptr(int64(41000))})
	if err != nil {
		t.Fatalf("ReviseBid() = %v", err)
	}

	if revised.AmountCents != 41000 {
		t.Errorf("the amount is %d, want 41000", revised.AmountCents)
	}
	if !revised.PickupAt.Equal(placed.PickupAt) {
		t.Errorf("pickup_at moved to %s from %s without being named", revised.PickupAt, placed.PickupAt)
	}
	if !revised.DeliverBy.Equal(placed.DeliverBy) {
		t.Errorf("deliver_by moved to %s from %s without being named", revised.DeliverBy, placed.DeliverBy)
	}
	if revised.Message != placed.Message {
		t.Errorf("the message became %q without being named", revised.Message)
	}
}

// TestARevisionCanClearTheMessage is the one field with a third state, and the state is expressed by
// the value rather than by a second flag.
//
// ck_bids_message refuses an empty string, so "" and NULL cannot be confused in either direction —
// which is only true if the write converts one to the other rather than relying on it. The same
// property SHIP-84 pinned for a placement, asserted again on the path that can *remove* a message.
func TestARevisionCanClearTheMessage(t *testing.T) {
	m := newMarket(t)

	placed, _, err := m.place(t, m.provider, m.job, offer("key-revise-clear"))
	if err != nil {
		t.Fatalf("placing the offer: %v", err)
	}
	if placed.Message == "" {
		t.Fatal("the fixture placed no message, so clearing one proves nothing")
	}

	revised, err := m.revise(t, m.provider, m.job, placed.ID, Revision{Message: ptr("   ")})
	if err != nil {
		t.Fatalf("ReviseBid() = %v", err)
	}
	if revised.Message != "" {
		t.Errorf("the message came back as %q, want empty", revised.Message)
	}

	var stored *string
	if err := m.pool.QueryRow(t.Context(),
		`SELECT message FROM bids WHERE id = $1`, placed.ID).Scan(&stored); err != nil {
		t.Fatalf("reading the stored message: %v", err)
	}
	if stored != nil {
		t.Errorf("the column holds %q, want NULL", *stored)
	}
}

// TestARevisionDoesNotConsumeThePlacementsKey is the trap this ticket had to walk around, and it is
// the test a `SET … idempotency_key = $n` fails.
//
// `bids.idempotency_key` holds the key the offer was **placed** under, and 000501 added it so that a
// placement retried after the middleware has forgotten it is answered from its own row. Writing a
// revision's key over it would leave that retry finding nothing, meeting
// uq_bids_one_submitted_per_provider_per_job instead, and being told the provider already had a live
// offer — a `409` for a request that succeeded, which is precisely the failure the column exists to
// prevent.
//
// So the sequence is the one a bad phone actually produces: place, revise, and then the *placement*
// arrives again because the first attempt's response never got home.
func TestARevisionDoesNotConsumeThePlacementsKey(t *testing.T) {
	m := newMarket(t)

	const placementKey = "key-placed-then-revised"

	placed, _, err := m.place(t, m.provider, m.job, offer(placementKey))
	if err != nil {
		t.Fatalf("placing the offer: %v", err)
	}

	if _, err := m.revise(t, m.provider, m.job, placed.ID,
		Revision{AmountCents: ptr(int64(38000))}); err != nil {
		t.Fatalf("revising: %v", err)
	}

	again, created, err := m.place(t, m.provider, m.job, offer(placementKey))
	if err != nil {
		t.Fatalf("retrying the placement after a revision = %v.\n"+
			"  The revision must not write bids.idempotency_key: the placement's key is how a "+
			"retry that outlived the middleware's cache is matched back to its own row, and "+
			"without it this request meets uq_bids_one_submitted_per_provider_per_job and is "+
			"refused as a second offer.", err)
	}
	if created {
		t.Error("the retried placement reports itself as a new offer")
	}
	if again.ID != placed.ID {
		t.Errorf("the retry answered with %s, want the original %s", again.ID, placed.ID)
	}
	if again.AmountCents != 38000 {
		t.Errorf("the retry answered %d cents, want the revised 38000 — a retry is answered from "+
			"the row as it now stands", again.AmountCents)
	}
	if n := m.bids(t, m.provider, m.job); n != 1 {
		t.Errorf("the sequence left %d rows, want 1", n)
	}
}

// TestOnlyTheBidsOwnerCanReviseIt is "their own", and it is the half with the worst failure.
//
// SHIP-84 proved the same rule for the placement's lookup by deleting `provider_id` from one `WHERE`.
// The equivalent mutation here is deleting either comparison in [Service.ownBid], and there is a case
// below for each: another provider's bid, and this provider's bid addressed under the wrong job.
//
// **Both are refused explicitly rather than scoped away.** The read takes the row by its identifier
// and compares afterwards, so the domain can tell "the stranger was refused" from "the row is not
// there" even though the wire cannot.
func TestOnlyTheBidsOwnerCanReviseIt(t *testing.T) {
	m := newMarket(t)

	mine, _, err := m.place(t, m.provider, m.job, offer("key-mine-to-revise"))
	if err != nil {
		t.Fatalf("placing the offer: %v", err)
	}
	before := m.row(t, mine.ID)

	competitor := newVerifiedProvider(t, m.pool, "bid-revise-rival@example.com", "+61400000850")
	declare(t, m.pool, competitor, "VIC")
	addVehicle(t, m.pool, competitor, "BID010")

	otherJob := m.publish(t)

	for name, tc := range map[string]struct {
		caller uuid.UUID
		job    uuid.UUID
		want   error
	}{
		"another provider":       {caller: competitor, job: m.job, want: ErrNotBidOwner},
		"the job's own customer": {caller: m.customer, job: m.job, want: ErrNotBidOwner},
		"the owner, wrong job":   {caller: m.provider, job: otherJob, want: ErrBidNotFound},
		"the owner, no such bid": {caller: m.provider, job: m.job, want: ErrBidNotFound},

		// Ownership is compared before the job, so this is ErrNotBidOwner rather than
		// ErrBidNotFound — the stronger statement, and the one worth having in a log line. The
		// two are one 404 on the wire either way.
		"another provider, wrong job too": {caller: competitor, job: otherJob, want: ErrNotBidOwner},
	} {
		t.Run(name, func(t *testing.T) {
			target := mine.ID
			if name == "the owner, no such bid" {
				target = uuid.Must(uuid.NewV7())
			}

			_, err := m.revise(t, tc.caller, tc.job, target, Revision{AmountCents: ptr(int64(1))})
			if !errors.Is(err, tc.want) {
				t.Fatalf("%s revised the bid: %v, want %v", name, err, tc.want)
			}
		})
	}

	if after := m.row(t, mine.ID); after.amount != before.amount || after.status != before.status {
		t.Errorf("a refused revision changed the row: %+v, was %+v", after, before)
	}
}

// TestAnAcceptedBidCannotBeRevised is the sharpest of the status refusals.
//
// CLAUDE.md's one accepted bid per job is a commitment two parties are holding, and
// uq_bids_one_accepted_per_job is what makes it true. Repricing an awarded offer would change what the
// customer agreed to after they agreed to it, which is the one thing an award has to be safe from.
//
// Docs/01 §4.2 draws the same line in the sentence this ticket is built on: "place, update, and
// withdraw a bid **until it is accepted or expires**".
func TestAnAcceptedBidCannotBeRevised(t *testing.T) {
	m := newMarket(t)

	placed, _, err := m.place(t, m.provider, m.job, offer("key-accepted-revise"))
	if err != nil {
		t.Fatalf("placing the offer: %v", err)
	}
	m.setStatus(t, placed.ID, StatusAccepted)

	if _, err := m.revise(t, m.provider, m.job, placed.ID,
		Revision{AmountCents: ptr(int64(1))}); !errors.Is(err, ErrBidAccepted) {
		t.Fatalf("revising an accepted bid = %v, want ErrBidAccepted", err)
	}

	after := m.row(t, placed.ID)
	if after.status != "Accepted" || after.amount != 450.00 {
		t.Errorf("the accepted bid is now %s at %v, want Accepted at 450.00", after.status, after.amount)
	}
}

// TestAClosedOfferCannotBeRevised bounds [Status.live] in the other direction.
//
// None of these four is reachable through an endpoint yet — Rejected is SHIP-93's, Expired is
// SHIP-89's, Superseded is SHIP-88's and Withdrawn arrives with this branch's other ticket — which is
// exactly why they are written now: a predicate that said "anything but Accepted" would pass every
// other test in this file and let a provider re-price an offer the customer had already declined.
func TestAClosedOfferCannotBeRevised(t *testing.T) {
	for _, status := range []Status{StatusRejected, StatusExpired, StatusSuperseded, StatusWithdrawn} {
		t.Run(status.String(), func(t *testing.T) {
			m := newMarket(t)

			placed, _, err := m.place(t, m.provider, m.job, offer("key-closed-"+status.String()))
			if err != nil {
				t.Fatalf("placing the offer: %v", err)
			}
			m.setStatus(t, placed.ID, status)

			if _, err := m.revise(t, m.provider, m.job, placed.ID,
				Revision{AmountCents: ptr(int64(1))}); !errors.Is(err, ErrBidClosed) {
				t.Fatalf("revising a %s offer = %v, want ErrBidClosed", status, err)
			}
		})
	}
}

// TestARevisionIsValidatedAsAWholeOffer is [Revision.applyTo]'s consequence, asserted rather than
// assumed.
//
// The merged offer meets the same validator a placement meets, so an offer that could not be placed
// today is not reachable by revising one that could be placed yesterday. The second case is the one
// that surprises and is the reason this test exists: a provider re-pricing a stale bid is told about
// `pickup_at`, a field they did not send, because what they are asking the platform to keep live is an
// offer to collect in the past.
func TestARevisionIsValidatedAsAWholeOffer(t *testing.T) {
	cases := []struct {
		name  string
		field string
		code  httpx.Code
		setup func(t *testing.T, m market, bid uuid.UUID)
		rev   Revision
	}{
		{
			name: "a price below zero", field: "amount_cents", code: validate.CodeRequired,
			rev: Revision{AmountCents: ptr(int64(-1))},
		},
		{
			name: "a price above the bound", field: "amount_cents", code: validate.CodeOutOfRange,
			rev: Revision{AmountCents: ptr(int64(maxOfferCents + 1))},
		},
		{
			name: "delivery brought before collection", field: "deliver_by", code: validate.CodeOutOfRange,
			rev: Revision{DeliverBy: ptr(testInstant.Add(time.Hour))},
		},
		{
			name: "a message longer than the column", field: "message", code: validate.CodeTooLong,
			rev: Revision{Message: ptr(strings.Repeat("a", maxMessageLen+1))},
		},
		{
			name: "a re-price of an offer whose collection time has passed", field: "pickup_at",
			code: validate.CodeOutOfRange,
			setup: func(t *testing.T, m market, bid uuid.UUID) {
				exec(t, m.pool, `UPDATE bids SET pickup_at = $2, deliver_by = $3 WHERE id = $1`,
					bid, testInstant.Add(-48*time.Hour), testInstant.Add(-40*time.Hour))
			},
			rev: Revision{AmountCents: ptr(int64(41000))},
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			m := newMarket(t)

			placed, _, err := m.place(t, m.provider, m.job, offer("key-invalid-revision"))
			if err != nil {
				t.Fatalf("placing the offer: %v", err)
			}
			if c.setup != nil {
				c.setup(t, m, placed.ID)
			}
			before := m.row(t, placed.ID)

			_, err = m.revise(t, m.provider, m.job, placed.ID, c.rev)
			if err == nil {
				t.Fatalf("%s was accepted", c.name)
			}

			var apiErr *httpx.Error
			if !errors.As(err, &apiErr) {
				t.Fatalf("ReviseBid() = %v, want a field error", err)
			}

			var found bool
			for _, problem := range apiErr.Details {
				if problem.Field == c.field {
					found = true
					if problem.Code != c.code {
						t.Errorf("%s is reported as %q, want %q", c.field, problem.Code, c.code)
					}
				}
			}
			if !found {
				t.Errorf("no detail names %q: %+v", c.field, apiErr.Details)
			}

			if after := m.row(t, placed.ID); after.amount != before.amount {
				t.Errorf("a refused revision wrote %v over %v", after.amount, before.amount)
			}
		})
	}
}

// TestARevisionThatNamesNothingIsRefused keeps a client defect from arriving as a success.
//
// The same reading fleet gives an edit naming no field: answering `200` with the unchanged bid would
// hide it behind a response that looks right.
func TestARevisionThatNamesNothingIsRefused(t *testing.T) {
	m := newMarket(t)

	placed, _, err := m.place(t, m.provider, m.job, offer("key-empty-revision"))
	if err != nil {
		t.Fatalf("placing the offer: %v", err)
	}

	if _, err := m.revise(t, m.provider, m.job, placed.ID, Revision{}); !errors.Is(err, ErrNothingToRevise) {
		t.Fatalf("an empty revision = %v, want ErrNothingToRevise", err)
	}
}

// TestARevisionNeedsTheProviderToStillBeEligible is the asymmetry with a withdrawal, and the half that
// would look like over-reach without a reason.
//
// **A revision produces a live offer at a new number, and the customer may accept it the moment it
// lands.** So it is checked by the same filter a placement is checked by, through the same port —
// Docs/07 §3 puts that decision server-side in exactly one place, and a provider whose only vehicle
// left service must not be able to re-price work they can no longer do.
//
// The job cases matter as much as the fleet ones: nothing closes a bid when its job is cancelled or
// awarded elsewhere today (SHIP-93 is that ticket), so without this check a provider could re-price an
// offer against work that is over.
func TestARevisionNeedsTheProviderToStillBeEligible(t *testing.T) {
	cases := map[string]func(t *testing.T, m market){
		"the provider's only vehicle left service": func(t *testing.T, m market) {
			exec(t, m.pool, `UPDATE vehicles SET deactivated_at = now() WHERE provider_id = $1`, m.provider)
		},
		"the provider's verification lapsed": func(t *testing.T, m market) {
			exec(t, m.pool, `UPDATE users SET phone_verified_at = NULL WHERE id = $1`, m.provider)
		},
		"the job was cancelled": func(t *testing.T, m market) {
			transition(t, m.pool, m.job, m.customer, "Open", "Cancelled")
		},
		"the job was awarded to somebody else": func(t *testing.T, m market) {
			transition(t, m.pool, m.job, m.customer, "Open", "Awarded")
		},
	}

	for name, breakIt := range cases {
		t.Run(name, func(t *testing.T) {
			m := newMarket(t)

			placed, _, err := m.place(t, m.provider, m.job, offer("key-eligible-revision"))
			if err != nil {
				t.Fatalf("placing the offer: %v", err)
			}
			breakIt(t, m)

			if _, err := m.revise(t, m.provider, m.job, placed.ID,
				Revision{AmountCents: ptr(int64(41000))}); !errors.Is(err, ErrJobNotOffered) {
				t.Fatalf("%s: the revision was not refused (%v)", name, err)
			}
			if after := m.row(t, placed.ID); after.amount != 450.00 {
				t.Errorf("the refused revision wrote %v", after.amount)
			}
		})
	}
}

// TestRepeatingARevisionReachesTheSameState is why this endpoint stores no idempotency key.
//
// A revision is an `UPDATE` of a row that already exists, so applying it twice reaches the state
// applying it once reaches — the natural idempotency `PATCH /v1/jobs/{id}` and
// `PATCH /v1/fleet/vehicles/{id}` already rely on. There is no second row for a repeat to create and
// no constraint for it to trip, which is why 000501's stored-key mechanism is a placement's and not
// this one's.
func TestRepeatingARevisionReachesTheSameState(t *testing.T) {
	m := newMarket(t)

	placed, _, err := m.place(t, m.provider, m.job, offer("key-repeat-revision"))
	if err != nil {
		t.Fatalf("placing the offer: %v", err)
	}

	rev := Revision{AmountCents: ptr(int64(42500)), Message: ptr("Same request, sent twice.")}

	first, err := m.revise(t, m.provider, m.job, placed.ID, rev)
	if err != nil {
		t.Fatalf("the first revision: %v", err)
	}
	second, err := m.revise(t, m.provider, m.job, placed.ID, rev)
	if err != nil {
		t.Fatalf("the second revision = %v, want the same outcome", err)
	}

	if first.ID != second.ID || first.AmountCents != second.AmountCents || first.Message != second.Message {
		t.Errorf("repeating one revision reached two states:\n  first:  %+v\n  second: %+v", first, second)
	}
	if n := m.bids(t, m.provider, m.job); n != 1 {
		t.Errorf("two identical revisions left %d rows, want 1", n)
	}
}

// --- SHIP-86: withdrawing an offer -----------------------------------------------------------------

// SHIP-86's *Done when*: "A provider can withdraw before acceptance; status becomes Withdrawn."
//
//	a provider can withdraw   TestAProviderWithdrawsTheirOwnOfferBeforeAcceptance
//	before acceptance         TestAnAcceptedBidCannotBeWithdrawn
//	status becomes Withdrawn  the same test, read out of the row rather than off the return value
//
// And the claim that is in the ticket rather than the sentence: **a retry of a withdrawal must not
// fail because the first one succeeded**. TestWithdrawingAnOfferThatIsAlreadyWithdrawnSucceeds is
// that, and it is why this endpoint needs no key column either.

// TestAProviderWithdrawsTheirOwnOfferBeforeAcceptance is the acceptance criterion.
//
// **The row survives.** Docs/01 §4.3 requires the platform to record every withdrawal and Docs/02 §4
// keeps bid history readable to the customer, the bidding provider and administrators, so there is no
// delete on this table — the same reading fleet gives a deactivated vehicle.
func TestAProviderWithdrawsTheirOwnOfferBeforeAcceptance(t *testing.T) {
	m := newMarket(t)

	placed, _, err := m.place(t, m.provider, m.job, offer("key-withdraw-happy"))
	if err != nil {
		t.Fatalf("placing the offer: %v", err)
	}

	withdrawn, err := m.withdraw(t, m.provider, m.job, placed.ID)
	if err != nil {
		t.Fatalf("WithdrawBid() = %v, want the withdrawn offer", err)
	}
	if withdrawn.Status != StatusWithdrawn {
		t.Errorf("the offer is %q, want Withdrawn", withdrawn.Status)
	}
	if withdrawn.ID != placed.ID {
		t.Errorf("the withdrawal produced a new row (%s, was %s)", withdrawn.ID, placed.ID)
	}

	// The row, not the answer the service gave about itself — and the price is still there, because
	// what a withdrawal removes is the offer's standing rather than its record.
	after := m.row(t, placed.ID)
	if after.status != "Withdrawn" {
		t.Errorf("the stored status is %q, want Withdrawn", after.status)
	}
	if after.amount != 450.00 {
		t.Errorf("the withdrawal changed the recorded price to %v", after.amount)
	}
	if n := m.bids(t, m.provider, m.job); n != 1 {
		t.Errorf("the job carries %d rows, want 1 — a withdrawal is not a delete", n)
	}
}

// TestWithdrawingAnOfferThatIsAlreadyWithdrawnSucceeds is what makes a retry safe when it does not
// reuse its key.
//
// The idempotency middleware absorbs the retry that carries the same key; this absorbs the one that
// does not, which is the ordinary shape of a phone that lost its connection, was restarted, and
// generated a fresh value for the same intent. `jobs` makes the same call for a repeated cancellation
// and `fleet` for a repeated deactivation.
//
// **`updated_at` is the assertion that the second call wrote nothing.** A second `UPDATE` reaching the
// row would move it through bids_set_updated_at, and a test comparing only the status could not tell
// an absorbed request from one that rewrote Withdrawn over Withdrawn.
func TestWithdrawingAnOfferThatIsAlreadyWithdrawnSucceeds(t *testing.T) {
	m := newMarket(t)

	placed, _, err := m.place(t, m.provider, m.job, offer("key-withdraw-twice"))
	if err != nil {
		t.Fatalf("placing the offer: %v", err)
	}

	if _, err := m.withdraw(t, m.provider, m.job, placed.ID); err != nil {
		t.Fatalf("the first withdrawal: %v", err)
	}
	after := m.row(t, placed.ID)

	again, err := m.withdraw(t, m.provider, m.job, placed.ID)
	if err != nil {
		t.Fatalf("withdrawing twice = %v.\n"+
			"  A retry that generated a fresh idempotency key must not be told its withdrawal "+
			"failed when it succeeded. The caller asked for an outcome that already holds.", err)
	}
	if again.Status != StatusWithdrawn {
		t.Errorf("the second withdrawal answered %q", again.Status)
	}

	if second := m.row(t, placed.ID); !second.updatedAt.Equal(after.updatedAt) {
		t.Errorf("the second withdrawal wrote to the row (updated_at moved %s to %s)",
			after.updatedAt, second.updatedAt)
	}
}

// TestAnAcceptedBidCannotBeWithdrawn is the "before acceptance" half of the *Done when*.
//
// **Withdrawal after acceptance is a different thing entirely and is not this endpoint.** Docs/02
// §6.2 makes a provider stepping away from awarded work a provider cancellation, with the job moving
// back to Open, every bid closed, and the cancellation recorded against the provider. Allowing it here
// would be that flow with none of its consequences, and it would break the award silently: the job
// would still be Awarded, to a bid nobody could see.
func TestAnAcceptedBidCannotBeWithdrawn(t *testing.T) {
	m := newMarket(t)

	placed, _, err := m.place(t, m.provider, m.job, offer("key-accepted-withdraw"))
	if err != nil {
		t.Fatalf("placing the offer: %v", err)
	}
	m.setStatus(t, placed.ID, StatusAccepted)

	if _, err := m.withdraw(t, m.provider, m.job, placed.ID); !errors.Is(err, ErrBidAccepted) {
		t.Fatalf("withdrawing an accepted bid = %v, want ErrBidAccepted", err)
	}
	if after := m.row(t, placed.ID); after.status != "Accepted" {
		t.Errorf("the bid is now %s, want Accepted", after.status)
	}
}

// TestAClosedOfferCannotBeWithdrawn is the rest of [Status.live], minus the case above it.
//
// Withdrawn is deliberately absent: that one is absorbed rather than refused, and
// TestWithdrawingAnOfferThatIsAlreadyWithdrawnSucceeds is where it lives.
func TestAClosedOfferCannotBeWithdrawn(t *testing.T) {
	for _, status := range []Status{StatusRejected, StatusExpired, StatusSuperseded} {
		t.Run(status.String(), func(t *testing.T) {
			m := newMarket(t)

			placed, _, err := m.place(t, m.provider, m.job, offer("key-closed-withdraw-"+status.String()))
			if err != nil {
				t.Fatalf("placing the offer: %v", err)
			}
			m.setStatus(t, placed.ID, status)

			if _, err := m.withdraw(t, m.provider, m.job, placed.ID); !errors.Is(err, ErrBidClosed) {
				t.Fatalf("withdrawing a %s offer = %v, want ErrBidClosed", status, err)
			}
		})
	}
}

// TestOnlyTheBidsOwnerCanWithdrawIt is the same rule [Service.ownBid] applies to a revision, driven
// through the other verb.
//
// Both endpoints go through one function precisely so this cannot hold on one and not the other — but
// "one function" is a claim about today's code, and this is the assertion that survives somebody
// inlining it.
func TestOnlyTheBidsOwnerCanWithdrawIt(t *testing.T) {
	m := newMarket(t)

	mine, _, err := m.place(t, m.provider, m.job, offer("key-mine-to-withdraw"))
	if err != nil {
		t.Fatalf("placing the offer: %v", err)
	}

	competitor := newVerifiedProvider(t, m.pool, "bid-withdraw-rival@example.com", "+61400000851")
	declare(t, m.pool, competitor, "VIC")
	addVehicle(t, m.pool, competitor, "BID011")
	otherJob := m.publish(t)

	if _, err := m.withdraw(t, competitor, m.job, mine.ID); !errors.Is(err, ErrNotBidOwner) {
		t.Fatalf("a competitor withdrew somebody else's offer: %v, want ErrNotBidOwner", err)
	}
	if _, err := m.withdraw(t, m.provider, otherJob, mine.ID); !errors.Is(err, ErrBidNotFound) {
		t.Fatalf("the bid was reachable under the wrong job: %v, want ErrBidNotFound", err)
	}

	if after := m.row(t, mine.ID); after.status != "Submitted" {
		t.Errorf("the offer is %s after two refused withdrawals, want Submitted", after.status)
	}
}

// TestAWithdrawalNeedsNoEligibility is the other half of the asymmetry [Service.ReviseBid] documents.
//
// **A provider must always be able to take back their own offer.** Refusing because their only vehicle
// left service, or their verification lapsed, would strand a live offer the customer can still accept
// and the provider can no longer retract — the worst of both answers. SHIP-81's filter governs what a
// provider may *offer*; it has no business governing what they may stop offering.
func TestAWithdrawalNeedsNoEligibility(t *testing.T) {
	cases := map[string]func(t *testing.T, m market){
		"the provider's only vehicle left service": func(t *testing.T, m market) {
			exec(t, m.pool, `UPDATE vehicles SET deactivated_at = now() WHERE provider_id = $1`, m.provider)
		},
		"the provider's verification lapsed": func(t *testing.T, m market) {
			exec(t, m.pool, `UPDATE users SET phone_verified_at = NULL WHERE id = $1`, m.provider)
		},
		"the provider withdrew from the state the job picks up in": func(t *testing.T, m market) {
			declare(t, m.pool, m.provider, "NSW")
		},
		"the job was cancelled underneath the offer": func(t *testing.T, m market) {
			transition(t, m.pool, m.job, m.customer, "Open", "Cancelled")
		},
	}

	for name, breakIt := range cases {
		t.Run(name, func(t *testing.T) {
			m := newMarket(t)

			placed, _, err := m.place(t, m.provider, m.job, offer("key-withdraw-ineligible"))
			if err != nil {
				t.Fatalf("placing the offer: %v", err)
			}
			breakIt(t, m)

			withdrawn, err := m.withdraw(t, m.provider, m.job, placed.ID)
			if err != nil {
				t.Fatalf("%s: the withdrawal was refused (%v).\n"+
					"  A provider must always be able to take back their own offer — refusing "+
					"leaves a live offer the customer can accept and the provider cannot retract.",
					name, err)
			}
			if withdrawn.Status != StatusWithdrawn {
				t.Errorf("the offer is %q, want Withdrawn", withdrawn.Status)
			}
		})
	}
}

// --- what neither verb does ------------------------------------------------------------------------

// TestNeitherRevisingNorWithdrawingMovesTheJob is SHIP-90's ticket, asserted here so that it stays
// SHIP-90's.
//
// Docs/02 §2 has `Negotiating → Open` on "all active bids expire, are withdrawn, or are rejected", and
// it is tempting to read a withdrawal as the trigger for it. Nothing reaches Negotiating until SHIP-90,
// which owns that presentation status in both directions — and job status is never a settable field in
// any case: a move passes one guarded function and leaves a `job_status_history` row in the same
// transaction (Docs/02 §2, CLAUDE.md). Doing half of that here would be doing the half without the
// record.
//
// Both halves are asserted, as SHIP-84's twin does: the job is where it was, and its history is
// unchanged. The second is what would catch a move made through some other path.
func TestNeitherRevisingNorWithdrawingMovesTheJob(t *testing.T) {
	m := newMarket(t)

	placed, _, err := m.place(t, m.provider, m.job, offer("key-nomove-revision"))
	if err != nil {
		t.Fatalf("placing the offer: %v", err)
	}

	var before int
	if err := m.pool.QueryRow(t.Context(),
		`SELECT count(*) FROM job_status_history WHERE job_id = $1`, m.job).Scan(&before); err != nil {
		t.Fatalf("counting history: %v", err)
	}

	if _, err := m.revise(t, m.provider, m.job, placed.ID, Revision{AmountCents: ptr(int64(41000))}); err != nil {
		t.Fatalf("revising: %v", err)
	}
	if _, err := m.withdraw(t, m.provider, m.job, placed.ID); err != nil {
		t.Fatalf("withdrawing: %v", err)
	}

	var (
		status string
		after  int
	)
	if err := m.pool.QueryRow(t.Context(),
		`SELECT j.status, (SELECT count(*) FROM job_status_history WHERE job_id = j.id)
		   FROM jobs j WHERE j.id = $1`, m.job).Scan(&status, &after); err != nil {
		t.Fatalf("reading the job: %v", err)
	}
	if status != "Open" {
		t.Errorf("the job is %q, want Open — Negotiating in either direction is SHIP-90's", status)
	}
	if after != before {
		t.Errorf("revising and withdrawing wrote %d job_status_history rows", after-before)
	}
}

// TestRevisingAndWithdrawingRefuseAConnectionPool is the guard [ErrNotInTransaction] exists for.
//
// Both methods read a status, decide against it and write, and the decision is only one decision while
// lockBid's `FOR UPDATE` is held — which outside a transaction is released the instant the SELECT
// returns. An award committing in that window would leave a withdrawal unpicking a bid
// uq_bids_one_accepted_per_job says two parties are committed to, with nothing to report afterwards.
func TestRevisingAndWithdrawingRefuseAConnectionPool(t *testing.T) {
	m := newMarket(t)

	placed, _, err := m.place(t, m.provider, m.job, offer("key-no-transaction"))
	if err != nil {
		t.Fatalf("placing the offer: %v", err)
	}

	if _, err := m.svc.ReviseBid(t.Context(), m.pool, m.provider, m.job, placed.ID,
		Revision{AmountCents: ptr(int64(1))}); !errors.Is(err, ErrNotInTransaction) {
		t.Errorf("ReviseBid() on a pool = %v, want ErrNotInTransaction", err)
	}
	if _, err := m.svc.WithdrawBid(t.Context(), m.pool, m.provider, m.job, placed.ID); !errors.Is(err, ErrNotInTransaction) {
		t.Errorf("WithdrawBid() on a pool = %v, want ErrNotInTransaction", err)
	}

	if after := m.row(t, placed.ID); after.status != "Submitted" || after.amount != 450.00 {
		t.Errorf("a refused call wrote to the row: %+v", after)
	}
}

// --- SHIP-87: the counter-offer, from both sides -------------------------------------------------

// SHIP-87's *Done when*: "Customer and provider can counter; each counter supersedes the prior
// offer."
//
// Three claims, and each has a test that fails if it stops holding:
//
//	customer can counter          TestACustomerCountersTheProvidersOffer
//	provider can counter          TestAProviderCountersTheCustomersCounter
//	supersedes the prior offer    both of the above, on the stored row rather than the return value
//
// SHIP-88's *Done when* — "only the latest valid offer is acceptable; full chain remains readable" —
// is TestOnlyTheHeadOfAChainCanBeCountered and TestTheDatabaseRefusesToAwardADisplacedOffer for the
// first half, and TestTheChainIsReadableAfterSeveralRounds for the second.

// TestACustomerCountersTheProvidersOffer is the first half of SHIP-87's *Done when*, and the first
// time in this domain that a customer changes anything.
//
// It asserts the stored rows rather than the returned one, in both directions: the counter exists as
// its own row attributed to the customer, and the offer it answered has been moved out of the live
// predicate *and* linked to it. A method returning what it meant to write would satisfy a test
// comparing against its own return value even if neither `UPDATE` had landed.
func TestACustomerCountersTheProvidersOffer(t *testing.T) {
	m := newMarket(t)

	placed, _, err := m.place(t, m.provider, m.job, offer("key-counter-base"))
	if err != nil {
		t.Fatalf("placing the offer to counter: %v", err)
	}

	countered, created, err := m.counter(t, m.customer, m.job, placed.ID, counterOf(40000, "key-counter"))
	if err != nil {
		t.Fatalf("the job's customer could not counter: %v", err)
	}
	if !created {
		t.Error("the first counter reports it created nothing")
	}
	if countered.ID == placed.ID {
		t.Fatal("the counter is the same row as the offer it answered; a counter creates a row and a " +
			"revision does not, and conflating them is what SHIP-85 refused to do")
	}

	if got := m.row(t, countered.ID); got.status != "Submitted" ||
		got.offeredBy != string(PartyCustomer) || got.amount != 400.00 ||
		got.supersededBy != uuid.Nil {
		t.Errorf("the counter row is %+v, want a live customer offer at 400.00 with no successor", got)
	}

	// The provider party is carried across, so `uq_bids_one_submitted_per_provider_per_job` still
	// means "one live offer in this negotiation" — 000502's whole reason for reinterpreting the
	// column rather than storing the author in it.
	if countered.ProviderID != m.provider {
		t.Errorf("the counter names %s as the provider party, want %s", countered.ProviderID, m.provider)
	}

	answered := m.row(t, placed.ID)
	if answered.status != string(StatusSuperseded) {
		t.Errorf("the answered offer is %q, want Superseded — Docs/02 §4 has each counter "+
			"superseding the prior offer", answered.status)
	}
	if answered.supersededBy != countered.ID {
		t.Errorf("the answered offer points at %s, want the counter %s — an unlinked row is a chain "+
			"nobody can read", answered.supersededBy, countered.ID)
	}
	if answered.amount != 450.00 {
		t.Errorf("the answered offer is now %v; a counter must not rewrite the offer it answers", answered.amount)
	}
}

// TestAProviderCountersTheCustomersCounter is the second half, and it is the direction that could
// not exist before 000502.
//
// The provider answers a row whose `provider_id` is their own and whose *author* is the customer.
// Before the `offered_by` column those two facts were the same one, and there would have been no way
// to tell "answer this" from "revise this".
func TestAProviderCountersTheCustomersCounter(t *testing.T) {
	m := newMarket(t)

	placed, _, err := m.place(t, m.provider, m.job, offer("key-round-one"))
	if err != nil {
		t.Fatalf("placing: %v", err)
	}
	theirs, _, err := m.counter(t, m.customer, m.job, placed.ID, counterOf(40000, "key-round-two"))
	if err != nil {
		t.Fatalf("the customer's counter: %v", err)
	}

	mine, created, err := m.counter(t, m.provider, m.job, theirs.ID, counterOf(43000, "key-round-three"))
	if err != nil {
		t.Fatalf("the provider could not counter back: %v", err)
	}
	if !created {
		t.Error("the provider's counter reports it created nothing")
	}

	if got := m.row(t, mine.ID); got.status != "Submitted" ||
		got.offeredBy != string(PartyProvider) || got.amount != 430.00 {
		t.Errorf("the provider's counter is %+v, want a live provider offer at 430.00", got)
	}
	if got := m.row(t, theirs.ID); got.status != string(StatusSuperseded) || got.supersededBy != mine.ID {
		t.Errorf("the customer's counter is %+v, want Superseded and linked to %s", got, mine.ID)
	}

	// Exactly one live offer at the end of three rounds, which is the property
	// uq_bids_one_submitted_per_provider_per_job holds and the one SHIP-92 will read under its lock.
	var live int
	if err := m.pool.QueryRow(t.Context(),
		`SELECT count(*) FROM bids WHERE job_id = $1 AND provider_id = $2 AND status = 'Submitted'`,
		m.job, m.provider).Scan(&live); err != nil {
		t.Fatalf("counting the live offers: %v", err)
	}
	if live != 1 {
		t.Errorf("the negotiation holds %d live offers after three rounds, want exactly 1", live)
	}
}

// TestACounterInheritsTheTimingItDoesNotRestate is the decision 000501 deferred to this ticket.
//
// That migration removed `ck_bids_offer_has_timing` in as many words: "whether a customer countering
// on *price alone* restates the timing or inherits it from the offer it supersedes is that ticket's
// decision." It inherits, and this is what says so.
func TestACounterInheritsTheTimingItDoesNotRestate(t *testing.T) {
	m := newMarket(t)

	placed, _, err := m.place(t, m.provider, m.job, offer("key-inherit"))
	if err != nil {
		t.Fatalf("placing: %v", err)
	}

	countered, _, err := m.counter(t, m.customer, m.job, placed.ID, counterOf(40000, "key-inherit-counter"))
	if err != nil {
		t.Fatalf("countering on price alone: %v", err)
	}

	if !countered.PickupAt.Equal(placed.PickupAt) || !countered.DeliverBy.Equal(placed.DeliverBy) {
		t.Errorf("the counter's timing is %s–%s, want the answered offer's %s–%s: a counter names "+
			"what it changes and takes the rest from the offer it answers",
			countered.PickupAt, countered.DeliverBy, placed.PickupAt, placed.DeliverBy)
	}
	if countered.Message != placed.Message {
		t.Errorf("the counter's message is %q, want the inherited %q", countered.Message, placed.Message)
	}

	// The stored row, not the answer the service gave about itself. An inheritance that existed only
	// in the returned value would leave the column NULL and the next round inheriting nothing.
	if got := m.row(t, countered.ID); !got.pickupAt.Equal(placed.PickupAt) {
		t.Errorf("the stored counter has pickup_at %s, want %s", got.pickupAt, placed.PickupAt)
	}
}

// TestACounterIsValidatedAsAWholeOffer holds a counter to the same rules a placement meets.
//
// [Counter.applyTo] merges the change over the stored offer and produces an [Offer], so there is one
// validator rather than two sets of rules that could disagree. The consequence is the one [Revision]
// records and it surprises in the same way: a counter on price alone against an offer whose
// collection time has since passed is refused, naming a field the caller did not send.
func TestACounterIsValidatedAsAWholeOffer(t *testing.T) {
	m := newMarket(t)

	placed, _, err := m.place(t, m.provider, m.job, offer("key-counter-validate"))
	if err != nil {
		t.Fatalf("placing: %v", err)
	}

	t.Run("an implausible amount is refused, naming the field", func(t *testing.T) {
		_, _, err := m.counter(t, m.customer, m.job, placed.ID,
			counterOf(maxOfferCents+1, "key-counter-too-much"))
		assertFieldRefused(t, err, "amount_cents")
	})

	t.Run("a collection time in the past is refused", func(t *testing.T) {
		past := testInstant.Add(-time.Hour)
		_, _, err := m.counter(t, m.customer, m.job, placed.ID,
			Counter{PickupAt: &past, Key: "key-counter-past"})
		assertFieldRefused(t, err, "pickup_at")
	})

	t.Run("delivery before collection is refused, including against the inherited pickup", func(t *testing.T) {
		early := placed.PickupAt.Add(-time.Hour)
		_, _, err := m.counter(t, m.customer, m.job, placed.ID,
			Counter{DeliverBy: &early, Key: "key-counter-backwards"})
		assertFieldRefused(t, err, "deliver_by")
	})

	if got := m.row(t, placed.ID); got.status != "Submitted" || got.supersededBy != uuid.Nil {
		t.Errorf("a refused counter superseded the offer anyway: %+v", got)
	}
}

// assertFieldRefused is the error-contract assertion the counter validation table makes three times.
func assertFieldRefused(t *testing.T, err error, field string) {
	t.Helper()

	var apiErr *httpx.Error
	if !errors.As(err, &apiErr) {
		t.Fatalf("the refusal is %v, want a field error in the contract's shape", err)
	}
	for _, detail := range apiErr.Details {
		if detail.Field == field {
			return
		}
	}
	t.Errorf("no detail names %q: %+v", field, apiErr.Details)
}

// TestACounterThatChangesNothingIsRefused separates a counter from an acceptance.
//
// A counter naming no field is not a client defect in the way an empty revision is: it is agreement,
// and agreement is the *award* — the customer's act, through an endpoint this ticket does not build.
// Writing it would put a second identical offer at the head of the chain and leave a client believing
// it had done something.
func TestACounterThatChangesNothingIsRefused(t *testing.T) {
	m := newMarket(t)

	placed, _, err := m.place(t, m.provider, m.job, offer("key-counter-empty-base"))
	if err != nil {
		t.Fatalf("placing: %v", err)
	}

	if _, _, err := m.counter(t, m.customer, m.job, placed.ID,
		Counter{Key: "key-counter-empty"}); !errors.Is(err, ErrNothingToCounter) {
		t.Fatalf("an empty counter = %v, want ErrNothingToCounter", err)
	}
	if n := m.bids(t, m.provider, m.job); n != 1 {
		t.Errorf("an empty counter wrote a row: the negotiation holds %d", n)
	}
}

// TestACounterWithNoKeyIsRefusedBeforeAnythingIsWritten covers the path the middleware makes
// unreachable.
//
// A counter writes a row, so the key is a *column* rather than only a cache entry — the whole of
// SHIP-84's argument, which a revision deliberately does not share. A counter stored with no key is a
// link a later retry cannot be matched to, and that retry would then add a second one.
func TestACounterWithNoKeyIsRefusedBeforeAnythingIsWritten(t *testing.T) {
	m := newMarket(t)

	placed, _, err := m.place(t, m.provider, m.job, offer("key-counter-nokey-base"))
	if err != nil {
		t.Fatalf("placing: %v", err)
	}

	if _, _, err := m.counter(t, m.customer, m.job, placed.ID,
		Counter{AmountCents: ptr(int64(40000))}); !errors.Is(err, ErrNoIdempotencyKey) {
		t.Fatalf("a keyless counter = %v, want ErrNoIdempotencyKey", err)
	}
	if n := m.bids(t, m.provider, m.job); n != 1 {
		t.Errorf("a keyless counter wrote a row: the negotiation holds %d", n)
	}
}

// --- SHIP-87: who may counter what ---------------------------------------------------------------

// TestOnlyAPartyToTheNegotiationCanCounter is Docs/01 §4.3's second privacy line, met by an endpoint
// two different kinds of caller may legitimately reach.
//
// **This is the widening SHIP-87 makes and the place it could have gone wrong.** Every endpoint
// before it is the provider's alone, and adding a customer to `Service.ownBid` would have added one
// to `PATCH` and `withdraw` as well. `Service.reachableBid` is a separate path for exactly that
// reason, and this test is what would catch it being widened too far — a competing provider must
// still meet the answer a bid that does not exist gets.
func TestOnlyAPartyToTheNegotiationCanCounter(t *testing.T) {
	m := newMarket(t)

	placed, _, err := m.place(t, m.provider, m.job, offer("key-counter-privacy"))
	if err != nil {
		t.Fatalf("placing: %v", err)
	}

	rival := newVerifiedProvider(t, m.pool, "counter-rival@example.com", "+61400000870")
	declare(t, m.pool, rival, "VIC")
	addVehicle(t, m.pool, rival, "BID870")

	stranger := newCustomer(t, m.pool, "counter-stranger@example.com", "+61400000871")

	for who, caller := range map[string]uuid.UUID{
		"a competing provider":                  rival,
		"a customer who owns some other job":    stranger,
		"an account with nothing to do with it": newCustomer(t, m.pool, "counter-nobody@example.com", "+61400000872"),
	} {
		t.Run(who+" cannot counter", func(t *testing.T) {
			_, _, err := m.counter(t, caller, m.job, placed.ID, counterOf(1, "key-counter-"+who))
			if !errors.Is(err, ErrNotBidOwner) {
				t.Fatalf("%s countered somebody else's offer: %v", who, err)
			}
		})
	}

	if got := m.row(t, placed.ID); got.status != "Submitted" || got.supersededBy != uuid.Nil {
		t.Errorf("a refused counter changed the offer: %+v", got)
	}
	if n := m.bids(t, m.provider, m.job); n != 1 {
		t.Errorf("a refused counter wrote a row: the negotiation holds %d", n)
	}
}

// TestNeitherPartyCanCounterTheirOwnOffer is the rule that makes one endpoint safe for two callers.
//
// **You counter the other party's offer and revise your own.** A provider answering their own live
// bid wants `PATCH`, and telling them so is a different request rather than a different screen —
// which is why [ErrWrongParty] has a code and [ErrNegotiationOver] does not.
func TestNeitherPartyCanCounterTheirOwnOffer(t *testing.T) {
	m := newMarket(t)

	placed, _, err := m.place(t, m.provider, m.job, offer("key-own-base"))
	if err != nil {
		t.Fatalf("placing: %v", err)
	}

	if _, _, err := m.counter(t, m.provider, m.job, placed.ID,
		counterOf(44000, "key-own-provider")); !errors.Is(err, ErrWrongParty) {
		t.Fatalf("a provider countered their own offer: %v", err)
	}

	theirs, _, err := m.counter(t, m.customer, m.job, placed.ID, counterOf(40000, "key-own-customer"))
	if err != nil {
		t.Fatalf("the customer's counter: %v", err)
	}
	if _, _, err := m.counter(t, m.customer, m.job, theirs.ID,
		counterOf(39000, "key-own-customer-again")); !errors.Is(err, ErrWrongParty) {
		t.Fatalf("a customer countered their own counter: %v", err)
	}

	if n := m.bids(t, m.provider, m.job); n != 2 {
		t.Errorf("the negotiation holds %d rows, want 2 — neither refusal may write one", n)
	}
}

// TestTheOtherPartysOfferIsNotRevisableOrWithdrawable closes the hole 000502's reinterpretation
// opened.
//
// A customer's counter carries the *provider's* id in `provider_id`, so `Service.ownBid`'s comparison
// passes for the provider — and before the authorship check in [changeable] they could have written
// over the customer's own number, or withdrawn it. That would be one party editing the other's offer,
// which is the sharpest form of the rule this domain is built on.
//
// It is [ErrWrongParty] rather than the 404 a stranger gets, because the provider can read that row
// in the negotiation's history a moment later.
func TestTheOtherPartysOfferIsNotRevisableOrWithdrawable(t *testing.T) {
	m := newMarket(t)

	placed, _, err := m.place(t, m.provider, m.job, offer("key-cross-edit"))
	if err != nil {
		t.Fatalf("placing: %v", err)
	}
	theirs, _, err := m.counter(t, m.customer, m.job, placed.ID, counterOf(40000, "key-cross-counter"))
	if err != nil {
		t.Fatalf("the customer's counter: %v", err)
	}

	if _, err := m.revise(t, m.provider, m.job, theirs.ID,
		Revision{AmountCents: ptr(int64(45000))}); !errors.Is(err, ErrWrongParty) {
		t.Errorf("the provider revised the customer's counter: %v", err)
	}
	if _, err := m.withdraw(t, m.provider, m.job, theirs.ID); !errors.Is(err, ErrWrongParty) {
		t.Errorf("the provider withdrew the customer's counter: %v", err)
	}

	if got := m.row(t, theirs.ID); got.status != "Submitted" || got.amount != 400.00 {
		t.Errorf("the customer's counter was changed by the provider: %+v", got)
	}
}

// TestACustomerCannotCounterOnAJobThatCanNoLongerBeAwarded is the customer's half of the check a
// provider meets as eligibility.
//
// A counter-offer on a job that cannot be awarded leads nowhere, and nothing else would refuse it:
// SHIP-93 — which closes competing bids when a job is awarded — does not exist, so the offer itself
// is still `Submitted` on a job that is over. The port asks `jobs` whether the job could still reach
// `Awarded`, which follows Docs/02 §2 rather than a third copy of the biddable-status list.
func TestACustomerCannotCounterOnAJobThatCanNoLongerBeAwarded(t *testing.T) {
	m := newMarket(t)

	placed, _, err := m.place(t, m.provider, m.job, offer("key-job-over"))
	if err != nil {
		t.Fatalf("placing: %v", err)
	}

	transition(t, m.pool, m.job, m.customer, "Open", "Cancelled")

	if _, _, err := m.counter(t, m.customer, m.job, placed.ID,
		counterOf(40000, "key-job-over-counter")); !errors.Is(err, ErrNegotiationOver) {
		t.Fatalf("countering on a cancelled job = %v, want ErrNegotiationOver", err)
	}
	if got := m.row(t, placed.ID); got.status != "Submitted" || got.supersededBy != uuid.Nil {
		t.Errorf("a refused counter changed the offer: %+v", got)
	}
}

// TestAProviderCounterNeedsThemToStillBeEligible is the provider's half.
//
// A provider's counter is a live offer the customer may accept the moment it lands, so it goes
// through SHIP-81's filter exactly as a placement and a revision do — a provider whose only vehicle
// left service must not be able to re-price work they can no longer do. The refusal is the same 404 a
// placement gets, byte-identically, because what became of work somebody else was given is not
// something this API discloses.
func TestAProviderCounterNeedsThemToStillBeEligible(t *testing.T) {
	m := newMarket(t)

	placed, _, err := m.place(t, m.provider, m.job, offer("key-counter-eligible"))
	if err != nil {
		t.Fatalf("placing: %v", err)
	}
	theirs, _, err := m.counter(t, m.customer, m.job, placed.ID, counterOf(40000, "key-counter-eligible-c"))
	if err != nil {
		t.Fatalf("the customer's counter: %v", err)
	}

	exec(t, m.pool, `UPDATE vehicles SET deactivated_at = now() WHERE provider_id = $1`, m.provider)

	if _, _, err := m.counter(t, m.provider, m.job, theirs.ID,
		counterOf(43000, "key-counter-ineligible")); !errors.Is(err, ErrJobNotOffered) {
		t.Fatalf("a provider with no vehicle countered: %v", err)
	}
	if got := m.row(t, theirs.ID); got.status != "Submitted" {
		t.Errorf("a refused counter changed the offer it answered: %+v", got)
	}
}

// TestANegotiationFailureIsNotARefusal separates the two things the customer-side port can answer.
//
// The twin of TestAnEligibilityFailureIsNotARefusal, and it earns its place for the same reason: a
// port that says "no" is an answer and produces a 404, while a port that could not *reach* an answer
// is a failure — and reporting it as "no such bid" would tell a customer their own negotiation had
// vanished during an outage.
func TestANegotiationFailureIsNotARefusal(t *testing.T) {
	m := newMarket(t)

	placed, _, err := m.place(t, m.provider, m.job, offer("key-negotiation-broken"))
	if err != nil {
		t.Fatalf("placing: %v", err)
	}

	m.svc = NewService(
		events.NewOutbox(),
		fleet.NewService(m.svc.clock),
		brokenNegotiation{err: errors.New("the job service could not run")},
		newTestAwarding(m.svc.clock),
		m.svc.clock,
	)

	_, _, err = m.counter(t, m.customer, m.job, placed.ID, counterOf(40000, "key-negotiation-broken-c"))
	if err == nil {
		t.Fatal("a broken port let a counter through")
	}
	if errors.Is(err, ErrNotBidOwner) {
		t.Error("a port that failed was reported as a port that said no")
	}
	if !strings.Contains(err.Error(), "the job service could not run") {
		t.Errorf("the cause was lost: %v", err)
	}
}

// TestCounteringRefusesAConnectionPool is the guard [ErrNotInTransaction] exists for, and this is the
// method with the most riding on it.
//
// A counter is three statements that are one act. Outside a transaction the first would land and the
// rest might not, leaving an offer superseded with no successor — a negotiation with nothing live in
// it and no way to tell that from a defect.
func TestCounteringRefusesAConnectionPool(t *testing.T) {
	m := newMarket(t)

	placed, _, err := m.place(t, m.provider, m.job, offer("key-counter-pool"))
	if err != nil {
		t.Fatalf("placing: %v", err)
	}

	if _, _, err := m.svc.CounterOffer(t.Context(), m.pool, m.customer, m.job, placed.ID,
		counterOf(40000, "key-counter-pool-c")); !errors.Is(err, ErrNotInTransaction) {
		t.Errorf("CounterOffer() on a pool = %v, want ErrNotInTransaction", err)
	}
	if got := m.row(t, placed.ID); got.status != "Submitted" || got.supersededBy != uuid.Nil {
		t.Errorf("a refused counter wrote to the row: %+v", got)
	}
}

// TestCounteringNeverMovesTheJob keeps SHIP-90's ticket intact.
//
// Docs/02 §2 has `Open → Negotiating` on "first bid or **counter-offer** submitted", which reads like
// an instruction to this ticket more than it did to SHIP-84 — a counter is literally the second half
// of that sentence. It is still SHIP-90's, which depends on this ticket and owns the presentation
// status in both directions.
//
// The history count is the half that matters: job status is never a settable field, so a move made
// through some other path would still leave a `job_status_history` row, and asserting the status
// alone would miss a move made and then reversed.
func TestCounteringNeverMovesTheJob(t *testing.T) {
	m := newMarket(t)

	var before int
	if err := m.pool.QueryRow(t.Context(),
		`SELECT count(*) FROM job_status_history WHERE job_id = $1`, m.job).Scan(&before); err != nil {
		t.Fatalf("counting the history: %v", err)
	}

	placed, _, err := m.place(t, m.provider, m.job, offer("key-counter-nomove"))
	if err != nil {
		t.Fatalf("placing: %v", err)
	}
	if _, _, err := m.counter(t, m.customer, m.job, placed.ID, counterOf(40000, "key-counter-nomove-c")); err != nil {
		t.Fatalf("countering: %v", err)
	}

	var (
		status string
		after  int
	)
	if err := m.pool.QueryRow(t.Context(),
		`SELECT j.status, (SELECT count(*) FROM job_status_history WHERE job_id = j.id)
		   FROM jobs j WHERE j.id = $1`, m.job).Scan(&status, &after); err != nil {
		t.Fatalf("reading the job: %v", err)
	}
	if status != "Open" {
		t.Errorf("the job is %q after a counter, want Open — Negotiating is SHIP-90's", status)
	}
	if after != before {
		t.Errorf("a counter wrote %d job_status_history rows", after-before)
	}
}

// --- SHIP-87: retries, and the race that would fork a chain --------------------------------------

// TestARetriedCounterAddsNoLinkToTheChain is the guarantee a counter needs and a revision does not.
//
// A revision is an `UPDATE` and applying it twice reaches the state applying it once reaches. A
// counter is an `INSERT`, so SHIP-84's argument applies instead: the key is stored on the row and a
// retry that outlives the middleware's cache is answered from the record.
//
// **The retry is answered before the status is checked, and that ordering is the point.** By the time
// a retry arrives, its own first attempt has already superseded the offer it answered — so a status
// check in front of the key lookup would refuse the caller's own successful request with "that offer
// is no longer live".
func TestARetriedCounterAddsNoLinkToTheChain(t *testing.T) {
	m := newMarket(t)

	placed, _, err := m.place(t, m.provider, m.job, offer("key-retry-base"))
	if err != nil {
		t.Fatalf("placing: %v", err)
	}

	first, created, err := m.counter(t, m.customer, m.job, placed.ID, counterOf(40000, "key-retry"))
	if err != nil || !created {
		t.Fatalf("the first counter = %v, created=%v", err, created)
	}

	again, created, err := m.counter(t, m.customer, m.job, placed.ID, counterOf(40000, "key-retry"))
	if err != nil {
		t.Fatalf("the retry was refused: %v", err)
	}
	if created {
		t.Error("the retry believes it created a second counter")
	}
	if again.ID != first.ID {
		t.Errorf("the retry answered with %s, want the counter it made, %s", again.ID, first.ID)
	}

	if n := m.bids(t, m.provider, m.job); n != 2 {
		t.Errorf("two requests under one key left %d rows in the negotiation, want 2", n)
	}
}

// TestOneKeyFromEachPartyIsTwoDifferentCounters is 000501's privacy argument extended to the
// negotiation's second writer.
//
// That migration put `provider_id` in `uq_bids_idempotency` because "two providers whose clients
// happened to generate the same key would collide, and the second would be handed the first's bid".
// A negotiation now has two writers inside one `(job_id, provider_id)` pair, and the identical
// sentence applies to them — so 000502 added `offered_by` to the same index and to the lookup behind
// it.
//
// Both parties deliberately send the same key, which is how the defect would be found.
func TestOneKeyFromEachPartyIsTwoDifferentCounters(t *testing.T) {
	m := newMarket(t)

	const shared = "a-key-both-clients-generated"

	placed, _, err := m.place(t, m.provider, m.job, offer("key-shared-base"))
	if err != nil {
		t.Fatalf("placing: %v", err)
	}

	theirs, _, err := m.counter(t, m.customer, m.job, placed.ID, counterOf(40000, shared))
	if err != nil {
		t.Fatalf("the customer's counter: %v", err)
	}

	mine, created, err := m.counter(t, m.provider, m.job, theirs.ID, counterOf(43000, shared))
	if err != nil {
		t.Fatalf("the provider's counter under the same key was refused: %v", err)
	}
	if !created {
		t.Fatal("the provider's counter was absorbed as the customer's retry — the key lookup is not " +
			"scoped by the offering party")
	}
	if mine.ID == theirs.ID {
		t.Fatal("the provider was handed the customer's counter")
	}
	if mine.AmountCents != 43000 {
		t.Errorf("the provider's counter is %d cents, want their own 43000", mine.AmountCents)
	}
}

// TestConcurrentCountersLeaveExactlyOneLiveOffer is the race SHIP-88 has to survive, and the shape
// migrations/bids_test.go's TestOneAcceptedBidPerJobHoldsUnderARace established.
//
// **Two counters against one offer is the ordinary case rather than an exotic one**: a customer taps
// twice, or both parties answer in the same second. What must not happen is a fork — two live offers
// each believing they displaced the same predecessor, which would leave "the latest valid offer"
// meaningless and SHIP-92 with two rows to choose between.
//
// Three mechanisms stand behind it and the test does not care which one answers, only that the
// outcome holds: `lockBid`'s `FOR UPDATE` serialises the transactions, `supersedeHead`'s
// compare-and-set matches nothing for the loser, and `uq_bids_one_submitted_per_provider_per_job`
// refuses the second live offer at the index. The first is what makes the refusal legible; the last is
// 000501's index doing exactly what its header promised, and is what holds if the first two are ever
// removed.
//
// Eight goroutines under *eight different keys*, deliberately: one key would be answered as a retry
// and would prove the idempotency path rather than this one.
func TestConcurrentCountersLeaveExactlyOneLiveOffer(t *testing.T) {
	m := newMarket(t)

	placed, _, err := m.place(t, m.provider, m.job, offer("key-race-base"))
	if err != nil {
		t.Fatalf("placing: %v", err)
	}

	const requests = 8

	var (
		wg        sync.WaitGroup
		mu        sync.Mutex
		succeeded int
		refused   []error
		broke     []error
	)

	wg.Add(requests)
	for i := range requests {
		go func() {
			defer wg.Done()

			_, _, err := m.counter(t, m.customer, m.job, placed.ID,
				counterOf(int64(40000+i), fmt.Sprintf("key-race-%d", i)))

			mu.Lock()
			defer mu.Unlock()
			switch {
			case err == nil:
				succeeded++
			case errors.Is(err, ErrBidClosed):
				refused = append(refused, err)
			default:
				broke = append(broke, err)
			}
		}()
	}
	wg.Wait()

	if len(broke) > 0 {
		t.Fatalf("%d of %d concurrent counters failed with something other than a legible refusal, "+
			"first: %v", len(broke), requests, broke[0])
	}
	if succeeded != 1 {
		t.Errorf("%d of %d concurrent counters succeeded, want exactly 1", succeeded, requests)
	}
	if len(refused) != requests-1 {
		t.Errorf("%d counters were refused with ErrBidClosed, want %d — the losers must be told the "+
			"offer they answered is no longer live", len(refused), requests-1)
	}

	// The outcome that matters, read off the table rather than off the return values.
	var (
		live       int
		successors int
		total      int
	)
	if err := m.pool.QueryRow(t.Context(), `
		SELECT count(*) FILTER (WHERE status = 'Submitted'),
		       count(*) FILTER (WHERE superseded_by IS NOT NULL),
		       count(*)
		  FROM bids WHERE job_id = $1 AND provider_id = $2`, m.job, m.provider).
		Scan(&live, &successors, &total); err != nil {
		t.Fatalf("reading the negotiation: %v", err)
	}
	if live != 1 {
		t.Errorf("the negotiation holds %d live offers, want exactly 1 — a fork is what "+
			"uq_bids_one_submitted_per_provider_per_job and the row lock exist to prevent", live)
	}
	if successors != 1 {
		t.Errorf("%d offers have been superseded, want exactly 1", successors)
	}
	if total != 2 {
		t.Errorf("the negotiation holds %d rows after one placement and eight racing counters, want 2", total)
	}
}

// --- SHIP-88: only the latest valid offer, and the chain that stays readable ----------------------

// TestOnlyTheHeadOfAChainCanBeCountered is the application half of SHIP-88's first *Done when*.
//
// A superseded offer is not the latest valid one, so it cannot be answered. Docs/02 §4 says only the
// latest valid offer can be accepted; this is the same rule reached through the endpoint rather than
// through the constraint, and the refusal is legible where the constraint's would not be.
func TestOnlyTheHeadOfAChainCanBeCountered(t *testing.T) {
	m := newMarket(t)

	placed, _, err := m.place(t, m.provider, m.job, offer("key-head-base"))
	if err != nil {
		t.Fatalf("placing: %v", err)
	}
	if _, _, err := m.counter(t, m.customer, m.job, placed.ID, counterOf(40000, "key-head-one")); err != nil {
		t.Fatalf("the customer's counter: %v", err)
	}

	if _, _, err := m.counter(t, m.customer, m.job, placed.ID,
		counterOf(38000, "key-head-stale")); !errors.Is(err, ErrBidClosed) {
		t.Fatalf("countering a superseded offer = %v, want ErrBidClosed", err)
	}
	if n := m.bids(t, m.provider, m.job); n != 2 {
		t.Errorf("a stale counter wrote a row: the negotiation holds %d", n)
	}
}

// TestASupersededOfferCanBeNeitherRevisedNorWithdrawn is the same rule met by SHIP-85's and
// SHIP-86's verbs.
//
// `Status.live()` is one predicate and all three verbs gate on it, which is what stops a provider
// re-pricing an offer the customer has already answered — the customer would be looking at a
// negotiation whose earlier round had changed underneath them.
func TestASupersededOfferCanBeNeitherRevisedNorWithdrawn(t *testing.T) {
	m := newMarket(t)

	placed, _, err := m.place(t, m.provider, m.job, offer("key-superseded-verbs"))
	if err != nil {
		t.Fatalf("placing: %v", err)
	}
	if _, _, err := m.counter(t, m.customer, m.job, placed.ID, counterOf(40000, "key-superseded-c")); err != nil {
		t.Fatalf("countering: %v", err)
	}

	if _, err := m.revise(t, m.provider, m.job, placed.ID,
		Revision{AmountCents: ptr(int64(44000))}); !errors.Is(err, ErrBidClosed) {
		t.Errorf("revising a superseded offer = %v, want ErrBidClosed", err)
	}
	if _, err := m.withdraw(t, m.provider, m.job, placed.ID); !errors.Is(err, ErrBidClosed) {
		t.Errorf("withdrawing a superseded offer = %v, want ErrBidClosed", err)
	}
	if got := m.row(t, placed.ID); got.amount != 450.00 || got.status != string(StatusSuperseded) {
		t.Errorf("a refused verb changed the superseded offer: %+v", got)
	}
}

// TestAnAcceptedOfferCannotBeCountered keeps the award final.
//
// Its own sentinel rather than [ErrBidClosed], because the two lead a client somewhere different: an
// accepted offer is a job somebody has won and the app shows it, and Docs/02 §6.2 makes stepping away
// from awarded work a provider cancellation rather than another round of haggling.
func TestAnAcceptedOfferCannotBeCountered(t *testing.T) {
	m := newMarket(t)

	placed, _, err := m.place(t, m.provider, m.job, offer("key-accepted-counter"))
	if err != nil {
		t.Fatalf("placing: %v", err)
	}
	m.setStatus(t, placed.ID, StatusAccepted)

	if _, _, err := m.counter(t, m.customer, m.job, placed.ID,
		counterOf(40000, "key-accepted-counter-c")); !errors.Is(err, ErrBidAccepted) {
		t.Fatalf("countering an accepted offer = %v, want ErrBidAccepted", err)
	}
}

// TestTheDatabaseRefusesToAwardADisplacedOffer is the half of SHIP-88's *Done when* that application
// logic cannot have, and it is the entry SHIP-92 is being handed.
//
// "Only the latest valid offer is acceptable" is `ck_bids_superseded_is_not_live`, an ordinary column
// constraint made possible by putting the chain link on the *displaced* row. An award transaction
// that wrote `status = 'Accepted'` over a countered offer — because it forgot to re-read the status
// under its lock, or read it before taking one — is refused by PostgreSQL rather than by a rule it had
// to remember.
//
// The statement below is deliberately the naive one: no lock, no status check, exactly what a
// plausible first draft of SHIP-92 would do.
func TestTheDatabaseRefusesToAwardADisplacedOffer(t *testing.T) {
	m := newMarket(t)

	placed, _, err := m.place(t, m.provider, m.job, offer("key-award-stale"))
	if err != nil {
		t.Fatalf("placing: %v", err)
	}
	if _, _, err := m.counter(t, m.customer, m.job, placed.ID, counterOf(40000, "key-award-stale-c")); err != nil {
		t.Fatalf("countering: %v", err)
	}

	_, err = m.pool.Exec(t.Context(), `UPDATE bids SET status = 'Accepted' WHERE id = $1`, placed.ID)
	if err == nil {
		t.Fatal("a superseded offer was awarded; ck_bids_superseded_is_not_live is what makes " +
			"\"only the latest valid offer is acceptable\" true for SHIP-92 rather than remembered")
	}
	if !strings.Contains(err.Error(), "ck_bids_superseded_is_not_live") {
		t.Errorf("expected ck_bids_superseded_is_not_live to refuse the award, got: %v", err)
	}
}

// TestTheDatabaseRefusesToAwardACustomersOwnOffer is the second constraint SHIP-92 inherits.
//
// With counters, the head of a chain is sometimes the customer's own offer — and awarding that would
// bind a provider to a price and a date they never agreed to, with `uq_bids_one_accepted_per_job`
// allowing it and nothing else noticing. Docs/02 §1 defines Awarded as "Customer has accepted one
// **provider** bid; provider commitment exists", and `ck_bids_only_a_providers_offer_is_accepted` is
// that sentence.
//
// It costs a customer nothing: a customer who wants their own number accepted waits for the provider
// to counter at it, and that row is the provider's commitment and is awardable.
func TestTheDatabaseRefusesToAwardACustomersOwnOffer(t *testing.T) {
	m := newMarket(t)

	placed, _, err := m.place(t, m.provider, m.job, offer("key-award-customers"))
	if err != nil {
		t.Fatalf("placing: %v", err)
	}
	theirs, _, err := m.counter(t, m.customer, m.job, placed.ID, counterOf(40000, "key-award-customers-c"))
	if err != nil {
		t.Fatalf("countering: %v", err)
	}

	_, err = m.pool.Exec(t.Context(), `UPDATE bids SET status = 'Accepted' WHERE id = $1`, theirs.ID)
	if err == nil {
		t.Fatal("the customer's own counter was awarded; a provider would be committed to terms they " +
			"never agreed to")
	}
	if !strings.Contains(err.Error(), "ck_bids_only_a_providers_offer_is_accepted") {
		t.Errorf("expected ck_bids_only_a_providers_offer_is_accepted to refuse it, got: %v", err)
	}

	// And the provider's counter at the same number *is* awardable, which is what makes the
	// constraint a shape rather than a wall.
	mine, _, err := m.counter(t, m.provider, m.job, theirs.ID, counterOf(40000, "key-award-agreed"))
	if err != nil {
		t.Fatalf("the provider's counter: %v", err)
	}
	if _, err := m.pool.Exec(t.Context(), `UPDATE bids SET status = 'Accepted' WHERE id = $1`, mine.ID); err != nil {
		t.Fatalf("the provider's own counter could not be awarded: %v", err)
	}
}

// TestTheChainIsReadableAfterSeveralRounds is the second half of SHIP-88's *Done when*.
//
// Docs/01 §4.3 requires the platform to "record all offers, counter-offers, withdrawals, and
// acceptances", and Docs/02 §4 keeps that history visible. **Nothing is deleted and nothing is
// overwritten**: every round is still there at the amount it was made at, in order, with the link
// that says which offer answered which.
//
// Five rounds rather than two, because a two-round chain passes against an implementation that keeps
// only the previous offer.
func TestTheChainIsReadableAfterSeveralRounds(t *testing.T) {
	m := newMarket(t)

	head, _, err := m.place(t, m.provider, m.job, offer("key-chain-0"))
	if err != nil {
		t.Fatalf("placing: %v", err)
	}

	want := []struct {
		amount int64
		by     Party
	}{{45000, PartyProvider}}

	callers := []uuid.UUID{m.customer, m.provider, m.customer, m.provider}
	for i, caller := range callers {
		amount := int64(40000 + i*500)
		head, _, err = m.counter(t, caller, m.job, head.ID, counterOf(amount, fmt.Sprintf("key-chain-%d", i+1)))
		if err != nil {
			t.Fatalf("round %d: %v", i+1, err)
		}
		by := PartyCustomer
		if caller == m.provider {
			by = PartyProvider
		}
		want = append(want, struct {
			amount int64
			by     Party
		}{amount, by})
	}

	offers, truncated, err := m.chain(t, m.customer, m.job, head.ID)
	if err != nil {
		t.Fatalf("reading the chain: %v", err)
	}
	if truncated {
		t.Error("a five-round chain reports itself truncated")
	}
	if len(offers) != len(want) {
		t.Fatalf("the chain holds %d offers after five rounds, want %d — a superseded offer is "+
			"neither deleted nor overwritten", len(offers), len(want))
	}

	for i, expected := range want {
		got := offers[i]
		if got.AmountCents != expected.amount {
			t.Errorf("round %d is %d cents, want %d — an intermediate price was lost",
				i, got.AmountCents, expected.amount)
		}
		if got.OfferedBy != expected.by {
			t.Errorf("round %d was offered by %s, want %s", i, got.OfferedBy, expected.by)
		}

		switch i {
		case len(want) - 1:
			if got.Status != StatusSubmitted || got.SupersededBy != uuid.Nil {
				t.Errorf("the last round is %s with successor %s, want the live head",
					got.Status, got.SupersededBy)
			}
		default:
			if got.Status != StatusSuperseded {
				t.Errorf("round %d is %s, want Superseded", i, got.Status)
			}
			if got.SupersededBy != offers[i+1].ID {
				t.Errorf("round %d points at %s, want the round that answered it, %s",
					i, got.SupersededBy, offers[i+1].ID)
			}
		}
	}

	// The provider reads the identical chain, which is the other half of "visible to the customer and
	// the bidding provider". Addressed through the *first* offer rather than the head, because a
	// client holding an old identifier is the ordinary case for a history.
	byProvider, _, err := m.chain(t, m.provider, m.job, offers[0].ID)
	if err != nil {
		t.Fatalf("the provider could not read their own chain: %v", err)
	}
	if len(byProvider) != len(offers) {
		t.Errorf("the provider sees %d offers and the customer sees %d", len(byProvider), len(offers))
	}
}

// TestOnlyAPartyToTheNegotiationCanReadTheChain is Docs/01 §4.3's second line at its sharpest.
//
// "Treat provider bid price as private from competing providers" — and this endpoint is the one place
// a competitor could otherwise read an entire negotiation at once: every price, every counter, every
// commitment about timing. A rival meets the answer a bid that does not exist gets.
func TestOnlyAPartyToTheNegotiationCanReadTheChain(t *testing.T) {
	m := newMarket(t)

	placed, _, err := m.place(t, m.provider, m.job, offer("key-chain-privacy"))
	if err != nil {
		t.Fatalf("placing: %v", err)
	}
	if _, _, err := m.counter(t, m.customer, m.job, placed.ID, counterOf(40000, "key-chain-privacy-c")); err != nil {
		t.Fatalf("countering: %v", err)
	}

	rival := newVerifiedProvider(t, m.pool, "chain-rival@example.com", "+61400000875")
	declare(t, m.pool, rival, "VIC")
	addVehicle(t, m.pool, rival, "BID875")

	// The rival bids on the same job, so they have a negotiation of their own on it — which is
	// exactly the caller who might expect to reach the other one.
	if _, _, err := m.place(t, rival, m.job, offer("key-chain-rival")); err != nil {
		t.Fatalf("the rival's own bid: %v", err)
	}

	if _, _, err := m.chain(t, rival, m.job, placed.ID); !errors.Is(err, ErrNotBidOwner) {
		t.Fatalf("a competing provider read another provider's negotiation: %v", err)
	}

	stranger := newCustomer(t, m.pool, "chain-stranger@example.com", "+61400000876")
	if _, _, err := m.chain(t, stranger, m.job, placed.ID); !errors.Is(err, ErrNotBidOwner) {
		t.Fatalf("a stranger read a negotiation: %v", err)
	}
}

// TestAChainHoldsOnlyItsOwnNegotiation is the other direction of the same rule.
//
// A refusal is not the only way a competitor's price could leak: a read scoped by job alone would
// answer the caller's own request with everybody's offers in it. The negotiation is
// `(job_id, provider_id)`, and this is what says the second half of that pair is really in the query.
func TestAChainHoldsOnlyItsOwnNegotiation(t *testing.T) {
	m := newMarket(t)

	mine, _, err := m.place(t, m.provider, m.job, offer("key-chain-scope"))
	if err != nil {
		t.Fatalf("placing: %v", err)
	}

	rival := newVerifiedProvider(t, m.pool, "chain-scope-rival@example.com", "+61400000877")
	declare(t, m.pool, rival, "VIC")
	addVehicle(t, m.pool, rival, "BID877")

	theirOffer := offer("key-chain-scope-rival")
	theirOffer.AmountCents = 777701
	if _, _, err := m.place(t, rival, m.job, theirOffer); err != nil {
		t.Fatalf("the rival's bid: %v", err)
	}

	offers, _, err := m.chain(t, m.customer, m.job, mine.ID)
	if err != nil {
		t.Fatalf("reading the chain: %v", err)
	}
	if len(offers) != 1 {
		t.Fatalf("the chain holds %d offers, want 1 — it is one negotiation, not every bid on the job", len(offers))
	}
	for _, o := range offers {
		if o.AmountCents == 777701 {
			t.Error("the chain carries a competing provider's price")
		}
	}
}

// TestTheChainStaysReadableAfterTheJobIsOver is why [Negotiation.CustomerOf] is a separate question
// from [Negotiation.AwardableBy].
//
// A record is at its most useful once the work is over: the customer reconstructing why they awarded
// elsewhere, the provider checking what they committed to, an administrator handling a dispute
// (Docs/02 §4). A read gated on the job still being live would go dark exactly then.
func TestTheChainStaysReadableAfterTheJobIsOver(t *testing.T) {
	m := newMarket(t)

	placed, _, err := m.place(t, m.provider, m.job, offer("key-chain-after"))
	if err != nil {
		t.Fatalf("placing: %v", err)
	}
	countered, _, err := m.counter(t, m.customer, m.job, placed.ID, counterOf(40000, "key-chain-after-c"))
	if err != nil {
		t.Fatalf("countering: %v", err)
	}

	transition(t, m.pool, m.job, m.customer, "Open", "Cancelled")

	for who, caller := range map[string]uuid.UUID{"the customer": m.customer, "the provider": m.provider} {
		offers, _, err := m.chain(t, caller, m.job, countered.ID)
		if err != nil {
			t.Fatalf("%s could not read the chain on a cancelled job: %v", who, err)
		}
		if len(offers) != 2 {
			t.Errorf("%s sees %d offers on a cancelled job, want 2 — the record outlives the job", who, len(offers))
		}
	}
}

// TestAWithdrawnOfferAndItsReplacementAreBothInTheChain is why the read is a negotiation rather than
// a walk along the links.
//
// SHIP-86 established that a provider who withdraws may bid again, so a negotiation legitimately
// holds rows outside any one link chain. Following `superseded_by` from the first row would omit
// exactly the rows somebody is most likely to be asking about — and Docs/01 §4.3 requires every
// withdrawal to be recorded, not merely retained where a query happens to look.
func TestAWithdrawnOfferAndItsReplacementAreBothInTheChain(t *testing.T) {
	m := newMarket(t)

	first, _, err := m.place(t, m.provider, m.job, offer("key-chain-withdraw"))
	if err != nil {
		t.Fatalf("placing: %v", err)
	}
	if _, err := m.withdraw(t, m.provider, m.job, first.ID); err != nil {
		t.Fatalf("withdrawing: %v", err)
	}

	replacement := offer("key-chain-replace")
	replacement.AmountCents = 41000
	second, _, err := m.place(t, m.provider, m.job, replacement)
	if err != nil {
		t.Fatalf("bidding again: %v", err)
	}

	offers, _, err := m.chain(t, m.customer, m.job, second.ID)
	if err != nil {
		t.Fatalf("reading the chain: %v", err)
	}
	if len(offers) != 2 {
		t.Fatalf("the chain holds %d offers, want 2 — a withdrawn offer is record and stays", len(offers))
	}
	if offers[0].ID != first.ID || offers[0].Status != StatusWithdrawn {
		t.Errorf("the first entry is %s at %s, want the withdrawn offer %s",
			offers[0].ID, offers[0].Status, first.ID)
	}
	if offers[1].ID != second.ID {
		t.Errorf("the second entry is %s, want the replacement %s", offers[1].ID, second.ID)
	}
}

// --- SHIP-92: the award, in one transaction -------------------------------------------------------
//
// SHIP-92's *Done when*: "POST /v1/jobs/{id}/award accepts one bid and moves the job to Awarded in
// one transaction."
//
// Three claims, and each has a test that fails if it stops holding:
//
//	accepts one bid          TestAwardingAcceptsTheBidAndMovesTheJob, read out of the row
//	moves the job to Awarded the same test, read out of `jobs` and `job_status_history` together
//	in one transaction       TestAFailedTransitionRollsTheAcceptBackWithIt
//
// The fourth claim is Docs/02 §3's rather than the ticket sentence's — "only the customer can award a
// job, and the selected bid must be active" — and it is two tests: TestOnlyTheJobsCustomerCanAward
// and TestOnlyALiveProvidersOfferCanBeAwarded.
//
// **The award is the one place in this domain where four database constraints stand behind the
// code**, and the tests below are deliberately split between the two kinds of proof. What the schema
// enforces was proved at SHIP-88, against naive statements this service does not make
// (TestTheDatabaseRefusesToAwardADisplacedOffer, TestTheDatabaseRefusesToAwardACustomersOwnOffer).
// What is proved here is the half no constraint can express — that the offer being accepted was
// **live when it was accepted** — plus the translation of every refusal into something a client can
// act on.
//
// **The concurrency suite is deliberately not here.** SHIP-95 owns "double award,
// withdraw-during-award, and expiry-during-award", and Docs/11 §8 asks for it to be written against
// Docs/02 §3 and Docs/08's named races by somebody who has not read this implementation. What is here
// instead is TestTheIndexRefusesASecondAcceptedBid, which demonstrates the same backstop
// deterministically rather than by racing.

// TestAwardingAcceptsTheBidAndMovesTheJob is SHIP-92's *Done when*, read out of the tables.
//
// Every assertion is against a row rather than against what the service said about itself. A method
// returning the value it meant to write would satisfy a test comparing against its own return value
// even if nothing had been written at all — which is the specific failure this endpoint cannot have,
// because a customer who is told they awarded a job and did not has no way to find out.
//
// The history row is checked as well as the status, and that pairing is what makes the transition
// real: 000402 refuses a status write that no `job_status_history` row written in the same
// transaction describes, so a job at 'Awarded' with one history row is a job that went through the
// guard rather than around it.
func TestAwardingAcceptsTheBidAndMovesTheJob(t *testing.T) {
	m := newMarket(t)

	placed, _, err := m.place(t, m.provider, m.job, offer("key-award"))
	if err != nil {
		t.Fatalf("placing: %v", err)
	}

	before, changes := m.jobStatus(t, m.job)
	if before != "Open" || changes != 1 {
		t.Fatalf("the fixture job is %s with %d transitions, want Open with 1", before, changes)
	}

	accepted, err := m.award(t, m.customer, m.job, placed.ID)
	if err != nil {
		t.Fatalf("awarding: %v", err)
	}
	if accepted.ID != placed.ID {
		t.Errorf("the award answered with %s, want the bid that was awarded %s", accepted.ID, placed.ID)
	}
	if accepted.Status != StatusAccepted {
		t.Errorf("the award answered %s, want %s", accepted.Status, StatusAccepted)
	}

	if stored := m.row(t, placed.ID); stored.status != string(StatusAccepted) {
		t.Errorf("the stored bid is %s, want Accepted", stored.status)
	}

	after, changes := m.jobStatus(t, m.job)
	if after != "Awarded" {
		t.Errorf("the job is %s, want Awarded", after)
	}
	if changes != 2 {
		t.Errorf("the job has %d recorded transitions, want 2 — an award that moved the status "+
			"without leaving a history row would have gone round 000402's guard", changes)
	}
}

// TestOnlyTheJobsCustomerCanAward is Docs/02 §3's first transition control.
//
// "Only the customer can award a job." The three callers who are not that customer are the bidding
// provider, a competing provider, and a stranger — and all three get one answer, which is the answer
// a job that does not exist gets.
//
// **The refusal happens before the bid is read**, and the test asserts it by naming a real bid: a
// caller who could tell "that bid exists and the job is not yours" from "no such bid" would be able
// to confirm identifiers by trying them.
func TestOnlyTheJobsCustomerCanAward(t *testing.T) {
	m := newMarket(t)

	placed, _, err := m.place(t, m.provider, m.job, offer("key-award-who"))
	if err != nil {
		t.Fatalf("placing: %v", err)
	}

	rival := newVerifiedProvider(t, m.pool, "award-rival@example.com", "+61400000920")
	stranger := newCustomer(t, m.pool, "award-stranger@example.com", "+61400000921")

	for who, caller := range map[string]uuid.UUID{
		"the bidding provider": m.provider,
		"a competing provider": rival,
		"another customer":     stranger,
	} {
		t.Run(who, func(t *testing.T) {
			_, err := m.award(t, caller, m.job, placed.ID)
			if !errors.Is(err, ErrNotJobCustomer) {
				t.Fatalf("%s awarding the job: %v, want ErrNotJobCustomer", who, err)
			}
		})
	}

	if stored := m.row(t, placed.ID); stored.status != string(StatusSubmitted) {
		t.Errorf("the bid is %s after three refused awards, want Submitted", stored.status)
	}
	if status, _ := m.jobStatus(t, m.job); status != "Open" {
		t.Errorf("the job is %s after three refused awards, want Open", status)
	}
}

// TestOnlyALiveProvidersOfferCanBeAwarded is the second half of Docs/02 §3's first control — "the
// selected bid must be active" — and the half no constraint can hold.
//
// **This is the one rule the schema cannot express**, and Docs/11 §3's SHIP-88 entry says so: a
// partial unique index can hold "at most one accepted bid per job", and a column `CHECK` can hold "a
// displaced offer is never live", but "the offer you are accepting was live when you accepted it" is
// a statement about a transition rather than about a row. 000500 deliberately declined to give `bids`
// the transition trigger `jobs` has, so this is application logic and it runs inside the transaction,
// under the bid's own lock.
//
// The four closed statuses are written directly, which is what [market.setStatus] exists for: three
// of them are reachable only through tickets that do not exist yet.
func TestOnlyALiveProvidersOfferCanBeAwarded(t *testing.T) {
	for _, status := range []Status{StatusWithdrawn, StatusRejected, StatusExpired, StatusSuperseded} {
		t.Run(string(status), func(t *testing.T) {
			m := newMarket(t)

			placed, _, err := m.place(t, m.provider, m.job, offer("key-award-"+string(status)))
			if err != nil {
				t.Fatalf("placing: %v", err)
			}
			m.setStatus(t, placed.ID, status)

			_, err = m.award(t, m.customer, m.job, placed.ID)
			if !errors.Is(err, ErrBidClosed) {
				t.Fatalf("awarding a %s offer: %v, want ErrBidClosed", status, err)
			}
			if stored := m.row(t, placed.ID); stored.status != string(status) {
				t.Errorf("the refused award moved the bid to %s", stored.status)
			}
			if job, changes := m.jobStatus(t, m.job); job != "Open" || changes != 1 {
				t.Errorf("the job is %s with %d transitions, want Open with 1", job, changes)
			}
		})
	}
}

// TestACustomersOwnCounterCannotBeAwarded is `ck_bids_only_a_providers_offer_is_accepted` met from
// the service's side.
//
// The constraint was proved at SHIP-88 against a naive `UPDATE`. What is proved here is that the
// service refuses first and refuses legibly — a customer handed a constraint name learns nothing they
// can act on, and the thing they can act on is "wait for the provider to counter at your number".
//
// **The second half is what makes the rule a shape rather than a wall**: the provider's counter at
// the customer's own number is awardable, and the same customer awards it through the same call.
func TestACustomersOwnCounterCannotBeAwarded(t *testing.T) {
	m := newMarket(t)

	placed, _, err := m.place(t, m.provider, m.job, offer("key-award-mine"))
	if err != nil {
		t.Fatalf("placing: %v", err)
	}
	theirs, _, err := m.counter(t, m.customer, m.job, placed.ID, counterOf(40000, "key-award-mine-c"))
	if err != nil {
		t.Fatalf("countering: %v", err)
	}

	if _, err := m.award(t, m.customer, m.job, theirs.ID); !errors.Is(err, ErrNotAProvidersOffer) {
		t.Fatalf("awarding the customer's own counter: %v, want ErrNotAProvidersOffer", err)
	}
	if status, _ := m.jobStatus(t, m.job); status != "Open" {
		t.Errorf("the refused award moved the job to %s", status)
	}

	agreed, _, err := m.counter(t, m.provider, m.job, theirs.ID, counterOf(40000, "key-award-agreed-c"))
	if err != nil {
		t.Fatalf("the provider's counter: %v", err)
	}
	if _, err := m.award(t, m.customer, m.job, agreed.ID); err != nil {
		t.Fatalf("awarding the provider's counter at the customer's own number: %v", err)
	}
	if status, _ := m.jobStatus(t, m.job); status != "Awarded" {
		t.Errorf("the job is %s, want Awarded", status)
	}
}

// TestOnlyTheHeadOfAChainCanBeAwarded is SHIP-88's first *Done when* — "only the latest valid offer
// is acceptable" — met by the endpoint it was written for.
//
// The service's own check is [Status.live], and `ck_bids_superseded_is_not_live` is what makes "live"
// and "head of the chain" the same row rather than two facts that have to agree. So a displaced offer
// is refused as closed, and the refusal never reaches the database.
func TestOnlyTheHeadOfAChainCanBeAwarded(t *testing.T) {
	m := newMarket(t)

	first, _, err := m.place(t, m.provider, m.job, offer("key-award-head"))
	if err != nil {
		t.Fatalf("placing: %v", err)
	}
	if _, _, err := m.counter(t, m.customer, m.job, first.ID, counterOf(40000, "key-award-head-c")); err != nil {
		t.Fatalf("countering: %v", err)
	}

	if _, err := m.award(t, m.customer, m.job, first.ID); !errors.Is(err, ErrBidClosed) {
		t.Fatalf("awarding a superseded offer: %v, want ErrBidClosed", err)
	}
	if stored := m.row(t, first.ID); stored.status != string(StatusSuperseded) {
		t.Errorf("the displaced offer is %s, want Superseded", stored.status)
	}
}

// TestAwardingTheSameBidTwiceIsTheSameOutcome is idempotency by state, which SHIP-86 established is
// stronger than idempotency by key.
//
// **A phone that reconnected, restarted and generated a fresh key must not be told its award
// failed.** The middleware's key absorbs a retry while its entry lives; this absorbs the one that
// arrives afterwards, or under a different value, and it does so without a stored key and without a
// column — because an `UPDATE` applied twice reaches the state applying it once reaches.
//
// The proof that nothing further was recorded is `updated_at` and the history count together.
// `bids_set_updated_at` moves the column on every write to the row, so a second accept would be
// visible there even though it would set the same status.
func TestAwardingTheSameBidTwiceIsTheSameOutcome(t *testing.T) {
	m := newMarket(t)

	placed, _, err := m.place(t, m.provider, m.job, offer("key-award-twice"))
	if err != nil {
		t.Fatalf("placing: %v", err)
	}

	first, err := m.award(t, m.customer, m.job, placed.ID)
	if err != nil {
		t.Fatalf("the first award: %v", err)
	}
	written := m.row(t, placed.ID)

	second, err := m.award(t, m.customer, m.job, placed.ID)
	if err != nil {
		t.Fatalf("awarding the same bid again: %v — an award is idempotent by state, and a phone "+
			"that retried under a fresh key must not be told it failed", err)
	}
	if second.ID != first.ID || second.Status != StatusAccepted {
		t.Errorf("the second award answered %s at %s, want the accepted bid %s",
			second.ID, second.Status, first.ID)
	}

	if again := m.row(t, placed.ID); !again.updatedAt.Equal(written.updatedAt) {
		t.Errorf("the repeated award wrote to the row: updated_at moved from %s to %s",
			written.updatedAt, again.updatedAt)
	}
	if status, changes := m.jobStatus(t, m.job); status != "Awarded" || changes != 2 {
		t.Errorf("the job is %s with %d transitions, want Awarded with 2", status, changes)
	}
}

// TestASecondAwardOnOneJobIsRefused is CLAUDE.md's "exactly one accepted bid per job", met from the
// job's side rather than the index's.
//
// A customer awarding a *different* bid on a job they have already awarded is refused with
// [ErrJobNotAwardable] — because the job is at 'Awarded' and Docs/02 §2 has no move from there to
// itself. That refusal happens under the job's lock, before the second bid is touched, which is why
// the index below it is a backstop rather than the mechanism.
func TestASecondAwardOnOneJobIsRefused(t *testing.T) {
	m := newMarket(t)

	rival := newVerifiedProvider(t, m.pool, "award-second@example.com", "+61400000922")
	declare(t, m.pool, rival, "VIC")
	addVehicle(t, m.pool, rival, "AWD002")

	mine, _, err := m.place(t, m.provider, m.job, offer("key-award-first"))
	if err != nil {
		t.Fatalf("placing: %v", err)
	}
	theirs, _, err := m.place(t, rival, m.job, offer("key-award-other"))
	if err != nil {
		t.Fatalf("the rival's offer: %v", err)
	}

	if _, err := m.award(t, m.customer, m.job, mine.ID); err != nil {
		t.Fatalf("the first award: %v", err)
	}

	if _, err := m.award(t, m.customer, m.job, theirs.ID); !errors.Is(err, ErrJobNotAwardable) {
		t.Fatalf("awarding a second bid on one job: %v, want ErrJobNotAwardable", err)
	}
	// **SHIP-93 makes this the interesting assertion rather than a formality.** The rival's offer is
	// already `Rejected` when the second award arrives, so an implementation that judged the *offer*
	// before the *job* would answer ErrBidClosed — a true statement that sends the customer to the
	// job's other offers, every one of which the same sweep has just closed. The job is what they
	// need to look at, and that is what the refusal above says.
	if stored := m.row(t, theirs.ID); stored.status != string(StatusRejected) {
		t.Errorf("the rival's offer is %s, want Rejected — the award closed it (SHIP-93)", stored.status)
	}

	var accepted int
	if err := m.pool.QueryRow(t.Context(),
		`SELECT count(*) FROM bids WHERE job_id = $1 AND status = 'Accepted'`, m.job).Scan(&accepted); err != nil {
		t.Fatalf("counting accepted bids: %v", err)
	}
	if accepted != 1 {
		t.Errorf("the job has %d accepted bids, want exactly 1", accepted)
	}
}

// TestTheIndexRefusesASecondAcceptedBid is `uq_bids_one_accepted_per_job` doing the job the lock
// ordering usually does first, and the translation of its refusal.
//
// **A deterministic stand-in for a race, and it is honest about being one.** Two awards arriving at
// once are serialised by the `jobs` lock long before they reach the index — Docs/11 §3's ordering is
// what arranges that — so the index's refusal is not reachable through two well-behaved requests.
// SHIP-95 owns the racing version. What this does instead is put the database in the state a race
// would have to produce for the index to matter: a job that is still Open carrying an accepted bid,
// which is what a writer that skipped the job lock would leave.
//
// The assertion is that the violation comes back as [ErrJobNotAwardable] rather than as a 500. A
// constraint name reaching a client is a defect twice over: it is unactionable, and it discloses the
// schema.
func TestTheIndexRefusesASecondAcceptedBid(t *testing.T) {
	m := newMarket(t)

	rival := newVerifiedProvider(t, m.pool, "award-index@example.com", "+61400000923")
	declare(t, m.pool, rival, "VIC")
	addVehicle(t, m.pool, rival, "AWD003")

	mine, _, err := m.place(t, m.provider, m.job, offer("key-award-index"))
	if err != nil {
		t.Fatalf("placing: %v", err)
	}
	theirs, _, err := m.place(t, rival, m.job, offer("key-award-index-rival"))
	if err != nil {
		t.Fatalf("the rival's offer: %v", err)
	}

	// The state a writer that skipped the job lock would leave behind: an accepted bid on a job that
	// is still Open, so LockForAward answers "awardable" and the index is the only thing left.
	m.setStatus(t, theirs.ID, StatusAccepted)

	if _, err := m.award(t, m.customer, m.job, mine.ID); !errors.Is(err, ErrJobNotAwardable) {
		t.Fatalf("the index's refusal came back as %v, want ErrJobNotAwardable", err)
	}
	if stored := m.row(t, mine.ID); stored.status != string(StatusSubmitted) {
		t.Errorf("the refused award left the bid at %s", stored.status)
	}
	if status, _ := m.jobStatus(t, m.job); status != "Open" {
		t.Errorf("the refused award moved the job to %s", status)
	}
}

// TestAJobThatHasMovedOnCannotBeAwarded is the third of Docs/02 §2's refusals, and the one a customer
// meets most often.
//
// Cancelled and Draft are both jobs with no row to 'Awarded' in the transition table, reached from
// two different directions: one has finished and one has not started. Both answer
// [ErrJobNotAwardable], and neither answer distinguishes itself from the other — which is the same
// collapse this domain makes everywhere else, and here it costs nothing because the customer owns the
// job and can read its status.
func TestAJobThatHasMovedOnCannotBeAwarded(t *testing.T) {
	m := newMarket(t)

	placed, _, err := m.place(t, m.provider, m.job, offer("key-award-gone"))
	if err != nil {
		t.Fatalf("placing: %v", err)
	}
	transition(t, m.pool, m.job, m.customer, "Open", "Cancelled")

	if _, err := m.award(t, m.customer, m.job, placed.ID); !errors.Is(err, ErrJobNotAwardable) {
		t.Fatalf("awarding a cancelled job: %v, want ErrJobNotAwardable", err)
	}
	if stored := m.row(t, placed.ID); stored.status != string(StatusSubmitted) {
		t.Errorf("the refused award moved the bid to %s", stored.status)
	}
}

// TestAwardingABidOnAnotherJobIsNotFound keeps the job in the path from being decorative.
//
// The award names a job in its path and a bid in its body, which is the one shape in this domain
// where the two could drift apart without anybody noticing. A real bid paired with a job it is not on
// is a request for something that does not exist, and answering it from the bid alone would let a
// customer award, against their own job, an offer somebody made on a different one.
func TestAwardingABidOnAnotherJobIsNotFound(t *testing.T) {
	m := newMarket(t)

	placed, _, err := m.place(t, m.provider, m.job, offer("key-award-elsewhere"))
	if err != nil {
		t.Fatalf("placing: %v", err)
	}

	other := m.publish(t)
	if _, err := m.award(t, m.customer, other, placed.ID); !errors.Is(err, ErrBidNotFound) {
		t.Fatalf("awarding a bid under the wrong job: %v, want ErrBidNotFound", err)
	}
	if status, _ := m.jobStatus(t, other); status != "Open" {
		t.Errorf("the refused award moved the other job to %s", status)
	}
	if stored := m.row(t, placed.ID); stored.status != string(StatusSubmitted) {
		t.Errorf("the refused award moved the bid to %s", stored.status)
	}
}

// TestAwardingRefusesAConnectionPool is the guard [ErrNotInTransaction] exists for, at its strongest.
//
// The award holds two row locks in two tables and writes through a port into a third statement.
// Outside a transaction each of those is an independent act that can half happen, and a job at
// 'Awarded' with no accepted bid is not a state anything downstream can read. It is checked before
// anything is locked, so a caller that got this wrong finds out before it has changed anything.
func TestAwardingRefusesAConnectionPool(t *testing.T) {
	m := newMarket(t)

	placed, _, err := m.place(t, m.provider, m.job, offer("key-award-pool"))
	if err != nil {
		t.Fatalf("placing: %v", err)
	}

	if _, err := m.svc.AwardBid(t.Context(), m.pool, m.customer, m.job, placed.ID); !errors.Is(err, ErrNotInTransaction) {
		t.Fatalf("awarding on the pool: %v, want ErrNotInTransaction", err)
	}
	if stored := m.row(t, placed.ID); stored.status != string(StatusSubmitted) {
		t.Errorf("an award outside a transaction still wrote: the bid is %s", stored.status)
	}
}

// TestAnAwardingFailureIsNotARefusal separates the two things the port can answer, which is the
// distinction [refusing] and [brokenNegotiation] exist for on the other two ports.
//
// A port that says "no" is an answer and produces a 404 or a 409. A port that could not *reach* an
// answer — the database is gone — is a failure, and reporting it as a refusal would tell a customer
// their job had vanished during an outage.
func TestAnAwardingFailureIsNotARefusal(t *testing.T) {
	m := newMarket(t)

	placed, _, err := m.place(t, m.provider, m.job, offer("key-award-broken"))
	if err != nil {
		t.Fatalf("placing: %v", err)
	}

	m.svc = NewService(
		events.NewOutbox(),
		fleet.NewService(m.svc.clock),
		newTestNegotiation(m.svc.clock),
		brokenAwarding{lockErr: errors.New("the job service could not run")},
		m.svc.clock,
	)

	_, err = m.award(t, m.customer, m.job, placed.ID)
	if err == nil {
		t.Fatal("a broken port let an award through")
	}
	if errors.Is(err, ErrNotJobCustomer) || errors.Is(err, ErrJobNotAwardable) {
		t.Error("a port that failed was reported as a port that said no")
	}
	if !strings.Contains(err.Error(), "the job service could not run") {
		t.Errorf("the cause was lost: %v", err)
	}
}

// TestAnUnrecognisedAwardAnswerIsRefused is why [JobAwardUnrecognised] is first in the enumeration.
//
// A half-written adapter, a stub, or a switch with a missing case returns the zero value, and the
// service has to refuse it rather than read silence as permission to award. **The real adapter cannot
// produce this** — every branch of it returns something else — so a stub is the only way to
// demonstrate that the refusal is there at all, and without this test the branch would be reachable
// only by a mistake nobody had made yet.
func TestAnUnrecognisedAwardAnswerIsRefused(t *testing.T) {
	m := newMarket(t)

	placed, _, err := m.place(t, m.provider, m.job, offer("key-award-zero"))
	if err != nil {
		t.Fatalf("placing: %v", err)
	}

	m.svc = NewService(
		events.NewOutbox(),
		fleet.NewService(m.svc.clock),
		newTestNegotiation(m.svc.clock),
		brokenAwarding{lock: JobAwardUnrecognised},
		m.svc.clock,
	)

	if _, err := m.award(t, m.customer, m.job, placed.ID); err == nil {
		t.Fatal("an adapter that answered nothing was read as permission to award")
	}
	if stored := m.row(t, placed.ID); stored.status != string(StatusSubmitted) {
		t.Errorf("the bid was accepted anyway: %s", stored.status)
	}
}

// TestAFailedTransitionRollsTheAcceptBackWithIt is the "one transaction" half of SHIP-92's *Done
// when*, and it is the claim that cannot be demonstrated by a happy path.
//
// The accept is written and *then* the job is moved. If the two were not one transaction, a refusal
// at the second would leave an accepted bid on a job that never moved — a provider believing they had
// won work the customer's own job screen says is still open, and a state SHIP-93's sweep would then
// close every other bid against.
//
// The transition is made to fail through the port, which is the only way to produce it: the job has
// been held `FOR UPDATE` since before the bid was read, so nothing can genuinely move it in between.
// What the test then asserts is the *rollback*, in both tables.
func TestAFailedTransitionRollsTheAcceptBackWithIt(t *testing.T) {
	m := newMarket(t)

	placed, _, err := m.place(t, m.provider, m.job, offer("key-award-rollback"))
	if err != nil {
		t.Fatalf("placing: %v", err)
	}

	m.svc = NewService(
		events.NewOutbox(),
		fleet.NewService(m.svc.clock),
		newTestNegotiation(m.svc.clock),
		brokenAwarding{lock: JobAwardable, move: JobAwardNotPermitted},
		m.svc.clock,
	)

	if _, err := m.award(t, m.customer, m.job, placed.ID); err == nil {
		t.Fatal("the award reported success with the job left where it was")
	}

	if stored := m.row(t, placed.ID); stored.status != string(StatusSubmitted) {
		t.Errorf("the bid is %s, want Submitted — the accept committed without the transition, "+
			"which leaves a provider holding work the job says nobody was given", stored.status)
	}
	if status, changes := m.jobStatus(t, m.job); status != "Open" || changes != 1 {
		t.Errorf("the job is %s with %d transitions, want Open with 1", status, changes)
	}
}

// TestAwardingRefusesAnIdentifierThatNamesNothing is the shape of every "no such thing" in this
// domain, applied to the endpoint that takes two identifiers from two places.
func TestAwardingRefusesAnIdentifierThatNamesNothing(t *testing.T) {
	m := newMarket(t)

	placed, _, err := m.place(t, m.provider, m.job, offer("key-award-nothing"))
	if err != nil {
		t.Fatalf("placing: %v", err)
	}

	if _, err := m.award(t, m.customer, m.job, uuid.Must(uuid.NewV7())); !errors.Is(err, ErrBidNotFound) {
		t.Errorf("awarding a bid that does not exist: %v, want ErrBidNotFound", err)
	}
	if _, err := m.award(t, m.customer, uuid.Must(uuid.NewV7()), placed.ID); !errors.Is(err, ErrNotJobCustomer) {
		t.Errorf("awarding on a job that does not exist: %v, want ErrNotJobCustomer", err)
	}
	if _, err := m.award(t, m.customer, m.job, uuid.Nil); !errors.Is(err, ErrBidNotFound) {
		t.Errorf("awarding the nil bid: %v, want ErrBidNotFound", err)
	}
}

// TestAnAwardClosesEveryCompetingOffer is SHIP-93's *Done when*: "every other bid on the job becomes
// Rejected in the same transaction".
//
// Docs/02 §3 is the sentence behind it — "awarding a job atomically marks one bid accepted and all
// others closed" — and SHIP-92 met half of it. This is the other half.
//
// **Three competitors rather than one**, because a sweep against a single row is indistinguishable
// from a statement that closes whatever it happens to find first, and because the shape a customer
// actually meets is a job with several offers on it.
//
// The final assertion is the one that matters most and is the easiest to leave out: the job has
// exactly one accepted bid and no live one. A sweep that closed two of the three would leave a
// provider whose offer the customer can no longer act on and who has been told nothing.
func TestAnAwardClosesEveryCompetingOffer(t *testing.T) {
	m := newMarket(t)

	winner, _, err := m.place(t, m.provider, m.job, offer("key-sweep-winner"))
	if err != nil {
		t.Fatalf("the winning offer: %v", err)
	}

	losers := make([]uuid.UUID, 0, 3)
	for n := 930; n < 933; n++ {
		placed, _, err := m.place(t, m.rival(t, n), m.job, offer(fmt.Sprintf("key-sweep-%d", n)))
		if err != nil {
			t.Fatalf("the offer from rival %d: %v", n, err)
		}
		losers = append(losers, placed.ID)
	}

	before := m.statuses(t, m.job)
	if len(before) != 4 {
		t.Fatalf("the job carries %d offers before the award, want 4", len(before))
	}

	if _, err := m.award(t, m.customer, m.job, winner.ID); err != nil {
		t.Fatalf("awarding: %v", err)
	}

	after := m.statuses(t, m.job)
	if after[winner.ID] != string(StatusAccepted) {
		t.Errorf("the awarded offer is %s, want Accepted", after[winner.ID])
	}
	for _, loser := range losers {
		if after[loser] != string(StatusRejected) {
			t.Errorf("the competing offer %s is %s, want Rejected", loser, after[loser])
		}
	}

	var live, accepted int
	if err := m.pool.QueryRow(t.Context(), `
		SELECT count(*) FILTER (WHERE status = 'Submitted'),
		       count(*) FILTER (WHERE status = 'Accepted')
		FROM bids WHERE job_id = $1`, m.job).Scan(&live, &accepted); err != nil {
		t.Fatalf("counting the offers on %s: %v", m.job, err)
	}
	if live != 0 || accepted != 1 {
		t.Errorf("the awarded job holds %d live offers and %d accepted, want 0 and 1", live, accepted)
	}
	if status, changes := m.jobStatus(t, m.job); status != "Awarded" || changes != 2 {
		t.Errorf("the job is %s with %d transitions, want Awarded with 2", status, changes)
	}
}

// TestTheSweepReachesNoOtherJob is the `WHERE job_id` clause, which is the one part of the statement
// whose absence would be catastrophic and silent.
//
// A sweep without it closes every live offer in the marketplace on the first award of the day, and
// nothing else in the platform would notice: the rows are legitimately `Rejected`, no constraint is
// violated, and the customers whose jobs were emptied are not the ones making the request. This test
// is what says so — one job awarded, another job's offers untouched, including one from the same
// provider.
func TestTheSweepReachesNoOtherJob(t *testing.T) {
	m := newMarket(t)

	elsewhere := m.publish(t)
	bystander := m.rival(t, 933)

	winner, _, err := m.place(t, m.provider, m.job, offer("key-sweep-scope-winner"))
	if err != nil {
		t.Fatalf("the winning offer: %v", err)
	}
	if _, _, err := m.place(t, bystander, m.job, offer("key-sweep-scope-loser")); err != nil {
		t.Fatalf("the competing offer: %v", err)
	}

	// The same provider bidding on the other job as well, which is what makes the scope a *job*
	// scope rather than something a provider filter would also have satisfied.
	untouched, _, err := m.place(t, m.provider, elsewhere, offer("key-sweep-scope-other"))
	if err != nil {
		t.Fatalf("the offer on the other job: %v", err)
	}
	alsoUntouched, _, err := m.place(t, bystander, elsewhere, offer("key-sweep-scope-other-rival"))
	if err != nil {
		t.Fatalf("the rival's offer on the other job: %v", err)
	}

	if _, err := m.award(t, m.customer, m.job, winner.ID); err != nil {
		t.Fatalf("awarding: %v", err)
	}

	other := m.statuses(t, elsewhere)
	for name, bid := range map[string]uuid.UUID{
		"the winning provider's offer on the other job": untouched.ID,
		"a rival's offer on the other job":              alsoUntouched.ID,
	} {
		if other[bid] != string(StatusSubmitted) {
			t.Errorf("%s is %s, want Submitted — the sweep left its own job", name, other[bid])
		}
	}
	if status, _ := m.jobStatus(t, elsewhere); status != "Open" {
		t.Errorf("the other job is %s, want Open", status)
	}
}

// TestTheSweepLeavesAnAlreadyClosedOfferAsItWas is the predicate's other half, and the half no
// constraint would catch.
//
// `ck_bids_superseded_is_not_live` refuses `Submitted` and `Accepted` on a displaced row and permits
// `Rejected` — so a sweep written as "everything except the winner" would overwrite a `Superseded`
// row with `Rejected`, the database would allow it, and the negotiation's history would then say the
// customer declined an offer that was actually displaced by their own counter. Docs/01 §4.3 requires
// every offer, counter-offer and withdrawal to be *recorded*; a status is the record.
//
// So each closed status is set up and checked by name rather than as a group, because the four end
// in four different ways and only one predicate keeps all four.
func TestTheSweepLeavesAnAlreadyClosedOfferAsItWas(t *testing.T) {
	for _, closed := range []Status{StatusWithdrawn, StatusRejected, StatusExpired, StatusSuperseded} {
		t.Run(string(closed), func(t *testing.T) {
			m := newMarket(t)

			winner, _, err := m.place(t, m.provider, m.job, offer("key-sweep-closed-winner"))
			if err != nil {
				t.Fatalf("the winning offer: %v", err)
			}
			done, _, err := m.place(t, m.rival(t, 934), m.job, offer("key-sweep-closed"))
			if err != nil {
				t.Fatalf("the offer that is already over: %v", err)
			}
			m.setStatus(t, done.ID, closed)
			was := m.row(t, done.ID)

			if _, err := m.award(t, m.customer, m.job, winner.ID); err != nil {
				t.Fatalf("awarding: %v", err)
			}

			now := m.row(t, done.ID)
			if now.status != string(closed) {
				t.Errorf("the sweep rewrote a %s offer as %s — how an offer ended is the record, "+
					"and no constraint would have refused this", closed, now.status)
			}
			if !now.updatedAt.Equal(was.updatedAt) {
				t.Errorf("the sweep wrote to a %s offer: updated_at moved from %s to %s",
					closed, was.updatedAt, now.updatedAt)
			}
		})
	}
}

// TestTheSweepClosesALosingNegotiationsHeadAndNotItsHistory is the chain case, which is where "every
// other bid" stops being one question.
//
// A negotiation the customer did not award holds two kinds of row: the offers already displaced by a
// counter, which are `Superseded` and terminal, and the one at the head, which is live. **Only the
// head is swept**, and both halves of that are defects if they go the other way — leaving the head
// `Submitted` would strand a live offer on an awarded job, and closing the predecessors would erase
// how they ended.
//
// The head here is the **customer's own counter**, which is the awkward case and the reason it is the
// one written: they decline their own outstanding offer by awarding somebody else, and `Rejected` —
// "an offer the customer declined" — is the only status in Docs/02 §4 that says so.
func TestTheSweepClosesALosingNegotiationsHeadAndNotItsHistory(t *testing.T) {
	m := newMarket(t)

	winner, _, err := m.place(t, m.provider, m.job, offer("key-sweep-chain-winner"))
	if err != nil {
		t.Fatalf("the winning offer: %v", err)
	}

	losing := m.rival(t, 935)
	round1, _, err := m.place(t, losing, m.job, offer("key-sweep-chain-1"))
	if err != nil {
		t.Fatalf("the losing provider's offer: %v", err)
	}
	round2, _, err := m.counter(t, m.customer, m.job, round1.ID, counterOf(40000, "key-sweep-chain-2"))
	if err != nil {
		t.Fatalf("the customer's counter: %v", err)
	}

	if _, err := m.award(t, m.customer, m.job, winner.ID); err != nil {
		t.Fatalf("awarding: %v", err)
	}

	after := m.statuses(t, m.job)
	if after[round1.ID] != string(StatusSuperseded) {
		t.Errorf("the displaced offer is %s, want Superseded — it was already terminal and the "+
			"sweep has no business saying how it ended", after[round1.ID])
	}
	if after[round2.ID] != string(StatusRejected) {
		t.Errorf("the live head of the losing negotiation is %s, want Rejected — a live offer on an "+
			"awarded job is what the sweep exists to remove", after[round2.ID])
	}
	if stored := m.row(t, round1.ID); stored.supersededBy != round2.ID {
		t.Errorf("the chain link was lost: %s points at %s", round1.ID, stored.supersededBy)
	}
}

// TestASweptOfferCanNoLongerBeActedOn is what the sweep buys the rest of the domain, and it is more
// than tidiness.
//
// Before SHIP-93 a competing offer stayed `Submitted` after the award, so the three verbs that gate
// on [Status.live] all let it through: its provider could revise the price of an offer on a job
// somebody else had already won, or withdraw it, or counter it. Nothing corrupted — the customer
// could not accept it, because the job had moved — but every one of those is a provider acting on
// work that no longer exists and being told it worked.
//
// The refusals all answer [ErrBidClosed], which is the code that already means "this offer is over".
func TestASweptOfferCanNoLongerBeActedOn(t *testing.T) {
	m := newMarket(t)

	winner, _, err := m.place(t, m.provider, m.job, offer("key-sweep-acted-winner"))
	if err != nil {
		t.Fatalf("the winning offer: %v", err)
	}
	loser := m.rival(t, 936)
	theirs, _, err := m.place(t, loser, m.job, offer("key-sweep-acted"))
	if err != nil {
		t.Fatalf("the competing offer: %v", err)
	}

	if _, err := m.award(t, m.customer, m.job, winner.ID); err != nil {
		t.Fatalf("awarding: %v", err)
	}

	newPrice := int64(30000)
	if _, err := m.revise(t, loser, m.job, theirs.ID, Revision{AmountCents: &newPrice}); !errors.Is(err, ErrBidClosed) {
		t.Errorf("revising a swept offer: %v, want ErrBidClosed", err)
	}
	if _, err := m.withdraw(t, loser, m.job, theirs.ID); !errors.Is(err, ErrBidClosed) {
		t.Errorf("withdrawing a swept offer: %v, want ErrBidClosed", err)
	}
	if _, _, err := m.counter(t, m.customer, m.job, theirs.ID, counterOf(30000, "key-sweep-acted-c")); !errors.Is(err, ErrBidClosed) {
		t.Errorf("countering a swept offer: %v, want ErrBidClosed", err)
	}
	if stored := m.row(t, theirs.ID); stored.status != string(StatusRejected) {
		t.Errorf("the swept offer is %s after three refused acts, want Rejected", stored.status)
	}
}

// TestAFailedTransitionRollsTheSweepBackToo extends SHIP-92's rollback proof to the statement SHIP-93
// added between the accept and the move.
//
// "In the same transaction" is SHIP-93's *Done when* as much as SHIP-92's, and the only way to
// demonstrate it is to make the last step fail: an award that rolled back leaving three offers
// `Rejected` would have closed a live market on a job that is still open, with no accepted bid to
// show for it and nothing for the providers to be told.
func TestAFailedTransitionRollsTheSweepBackToo(t *testing.T) {
	m := newMarket(t)

	winner, _, err := m.place(t, m.provider, m.job, offer("key-sweep-rollback-winner"))
	if err != nil {
		t.Fatalf("the winning offer: %v", err)
	}
	loser, _, err := m.place(t, m.rival(t, 937), m.job, offer("key-sweep-rollback"))
	if err != nil {
		t.Fatalf("the competing offer: %v", err)
	}

	m.svc = NewService(
		events.NewOutbox(),
		fleet.NewService(m.svc.clock),
		newTestNegotiation(m.svc.clock),
		brokenAwarding{lock: JobAwardable, move: JobAwardNotPermitted},
		m.svc.clock,
	)

	if _, err := m.award(t, m.customer, m.job, winner.ID); err == nil {
		t.Fatal("the award reported success with the job left where it was")
	}

	after := m.statuses(t, m.job)
	if after[winner.ID] != string(StatusSubmitted) {
		t.Errorf("the accept survived the rollback: the awarded offer is %s", after[winner.ID])
	}
	if after[loser.ID] != string(StatusSubmitted) {
		t.Errorf("the sweep survived the rollback: the competing offer is %s — a job still Open "+
			"with its offers closed is a market emptied by a request that failed", after[loser.ID])
	}
	if status, changes := m.jobStatus(t, m.job); status != "Open" || changes != 1 {
		t.Errorf("the job is %s with %d transitions, want Open with 1", status, changes)
	}
}

// TestARetriedAwardDoesNotSweepAgain is SHIP-94's *Done when* met at the layer the key cannot reach.
//
// The ticket asks that "a retried award with the same key returns the original outcome, not an
// error", and the middleware is what answers a key it still holds. **This is the retry the middleware
// has already forgotten** — a TTL that expired, an eviction, a failover, or a phone that restarted and
// generated a fresh key — where the request runs a second time all the way to the transaction and the
// only thing standing between it and a second act is the record itself.
//
// SHIP-93 raises what "not an error" has to mean. A repeated award now has a *second* write behind
// it, and returning the right bid while sweeping again would be a retry that wrote — silently, to
// rows no caller named. `updated_at` on every offer of the job is what says it did not: the trigger
// moves the column on any write, so a sweep that closed already-closed offers would be visible even
// though it would set the status they already have.
func TestARetriedAwardDoesNotSweepAgain(t *testing.T) {
	m := newMarket(t)

	winner, _, err := m.place(t, m.provider, m.job, offer("key-retry-sweep-winner"))
	if err != nil {
		t.Fatalf("the winning offer: %v", err)
	}
	loser, _, err := m.place(t, m.rival(t, 940), m.job, offer("key-retry-sweep-loser"))
	if err != nil {
		t.Fatalf("the competing offer: %v", err)
	}

	if _, err := m.award(t, m.customer, m.job, winner.ID); err != nil {
		t.Fatalf("the first award: %v", err)
	}
	wroteWinner, wroteLoser := m.row(t, winner.ID), m.row(t, loser.ID)
	if wroteLoser.status != string(StatusRejected) {
		t.Fatalf("the competing offer is %s after the award, want Rejected", wroteLoser.status)
	}

	again, err := m.award(t, m.customer, m.job, winner.ID)
	if err != nil {
		t.Fatalf("the retry: %v — an award is idempotent by state, and a phone that restarted and "+
			"generated a fresh key must not be told it failed", err)
	}
	if again.ID != winner.ID || again.Status != StatusAccepted {
		t.Errorf("the retry answered %s at %s, want the accepted offer %s", again.ID, again.Status, winner.ID)
	}

	if now := m.row(t, winner.ID); !now.updatedAt.Equal(wroteWinner.updatedAt) {
		t.Errorf("the retry rewrote the accepted offer: updated_at moved from %s to %s",
			wroteWinner.updatedAt, now.updatedAt)
	}
	if now := m.row(t, loser.ID); !now.updatedAt.Equal(wroteLoser.updatedAt) {
		t.Errorf("the retry ran the sweep a second time: the closed offer's updated_at moved from "+
			"%s to %s, and no caller ever named that row", wroteLoser.updatedAt, now.updatedAt)
	}
	if status, changes := m.jobStatus(t, m.job); status != "Awarded" || changes != 2 {
		t.Errorf("the job is %s with %d transitions, want Awarded with 2", status, changes)
	}
}

// TestARetryIsAnsweredAfterTheJobHasMovedOnAgain is the sentence SHIP-92 wrote and nothing tested: a
// retry asks what happened to a request, and that answer does not change when the world does.
//
// The interesting retry is not the one that arrives a second later. It is the one that arrives after
// the delivery has **started** — the phone found signal in the afternoon and sent the morning's award
// again — by which time the job is past `Awarded` and `LockForAward` answers "not permitted". The
// award still happened, the customer still needs to be told so, and the branch that says so is the
// already-`Accepted` check sitting in front of the job's standing.
//
// It is also the one case where getting the ordering backwards is invisible in ordinary use and
// plausible on inspection: refusing with `conflict` reads like a correct answer about a job that has
// moved on, and it would be a client told its award failed when the provider is already driving.
func TestARetryIsAnsweredAfterTheJobHasMovedOnAgain(t *testing.T) {
	m := newMarket(t)

	winner, _, err := m.place(t, m.provider, m.job, offer("key-retry-moved-winner"))
	if err != nil {
		t.Fatalf("the winning offer: %v", err)
	}
	loser, _, err := m.place(t, m.rival(t, 941), m.job, offer("key-retry-moved-loser"))
	if err != nil {
		t.Fatalf("the competing offer: %v", err)
	}

	if _, err := m.award(t, m.customer, m.job, winner.ID); err != nil {
		t.Fatalf("the award: %v", err)
	}
	wrote := m.row(t, winner.ID)

	// The provider sets off. Docs/02 §2 permits Awarded → En route to pickup directly, and from there
	// there is no move to Awarded at all — so the job's standing is now a refusal.
	transitionBy(t, m.pool, "provider", m.job, m.provider, "Awarded", "En route to pickup")

	late, err := m.award(t, m.customer, m.job, winner.ID)
	if err != nil {
		t.Fatalf("a retry arriving after the delivery started: %v, want the accepted offer", err)
	}
	if late.ID != winner.ID || late.Status != StatusAccepted {
		t.Errorf("the late retry answered %s at %s, want the accepted offer %s", late.ID, late.Status, winner.ID)
	}
	if now := m.row(t, winner.ID); !now.updatedAt.Equal(wrote.updatedAt) {
		t.Errorf("the late retry wrote to the row: updated_at moved from %s to %s", wrote.updatedAt, now.updatedAt)
	}

	// And a *different* offer under the same conditions is still refused, which is what says the
	// branch above recognised a retry rather than stopping checking.
	if _, err := m.award(t, m.customer, m.job, loser.ID); !errors.Is(err, ErrJobNotAwardable) {
		t.Errorf("awarding a different offer on a job under way: %v, want ErrJobNotAwardable", err)
	}
	if status, changes := m.jobStatus(t, m.job); status != "En route to pickup" || changes != 3 {
		t.Errorf("the job is %s with %d transitions, want En route to pickup with 3", status, changes)
	}
}
