package bidding

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/DulsaraNethmin/Shipper/services/core/internal/db"
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
// SHIP-86 builds the withdrawal endpoint. The status is moved here directly because that endpoint
// does not exist yet, and the property is worth pinning now: SHIP-86 should find this already true.
func TestAWithdrawnOfferCanBeReplaced(t *testing.T) {
	m := newMarket(t)

	first, _, err := m.place(t, m.provider, m.job, offer("key-withdrawn"))
	if err != nil {
		t.Fatalf("the first offer: %v", err)
	}
	exec(t, m.pool, `UPDATE bids SET status = 'Withdrawn' WHERE id = $1`, first.ID)

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
	m.svc = NewService(refusing{err: errors.New("the filter could not run")}, m.svc.clock)

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

// --- what a revision does not do ------------------------------------------------------------------------

// TestARevisionDoesNotMoveTheJob is SHIP-90's ticket, asserted here so that it stays SHIP-90's.
//
// Docs/02 §2 has `Open → Negotiating` on "first bid or counter-offer submitted" and `Negotiating →
// Open` on bids being withdrawn or rejected. Both belong to **SHIP-90**, which owns that presentation
// status in either direction — and job status is never a settable field in any case: a move passes one
// guarded function and leaves a `job_status_history` row in the same transaction (Docs/02 §2,
// CLAUDE.md). Doing half of that here would be doing the half without the record.
//
// Both halves are asserted, as SHIP-84's twin does: the job is where it was, and its history is
// unchanged. The second is what would catch a move made through some other path.
func TestARevisionDoesNotMoveTheJob(t *testing.T) {
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
		t.Errorf("revising wrote %d job_status_history rows", after-before)
	}
}

// TestRevisingRefusesAConnectionPool is the guard [ErrNotInTransaction] exists for.
//
// The method reads a status, decides against it and writes, and that is only one decision while
// lockBid's `FOR UPDATE` is held — which outside a transaction is released the instant the SELECT
// returns. An award committing in that window would leave a revision repricing a bid
// uq_bids_one_accepted_per_job says two parties are committed to, with nothing to report afterwards.
func TestRevisingRefusesAConnectionPool(t *testing.T) {
	m := newMarket(t)

	placed, _, err := m.place(t, m.provider, m.job, offer("key-no-transaction"))
	if err != nil {
		t.Fatalf("placing the offer: %v", err)
	}

	if _, err := m.svc.ReviseBid(t.Context(), m.pool, m.provider, m.job, placed.ID,
		Revision{AmountCents: ptr(int64(1))}); !errors.Is(err, ErrNotInTransaction) {
		t.Errorf("ReviseBid() on a pool = %v, want ErrNotInTransaction", err)
	}

	if after := m.row(t, placed.ID); after.status != "Submitted" || after.amount != 450.00 {
		t.Errorf("a refused call wrote to the row: %+v", after)
	}
}
