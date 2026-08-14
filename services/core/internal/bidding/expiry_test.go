package bidding

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/DulsaraNethmin/Shipper/services/core/internal/clock"
	"github.com/DulsaraNethmin/Shipper/services/core/internal/db"
	"github.com/DulsaraNethmin/Shipper/services/core/internal/events"
	"github.com/DulsaraNethmin/Shipper/services/core/internal/fleet"
)

// SHIP-89 — "bids expire on their own terms and emit an event", against a real PostgreSQL.
//
// Two halves, and only one of them is about a status. The terms are the offer's own `pickup_at`,
// so the interesting assertions are about what the sweep **does not** take: an offer whose
// collection time is still ahead of it, and every offer that has already closed for some other
// reason. Overwriting a `Withdrawn` or a `Superseded` row with `Expired` would replace the record
// of *how* an offer ended with the record of *when*, which is the failure
// [postgresStore.rejectCompeting] documents for the award's sweep and which no constraint refuses.
//
// The pass itself — the claim, the batch, `FOR UPDATE SKIP LOCKED`, two workers sharing — is
// cmd/worker's and is tested there.

// sweepAt is one pass, run exactly as cmd/worker/tasks_bidding.go runs it: the claim and the
// expiries inside one transaction, judged against one instant.
//
// The service is built the way the task builds it — the real outbox and **nil ports** — rather
// than reusing [market.svc]. That is the assertion rather than a shortcut: an expiry consults
// neither eligibility, nor who owns the job, nor whether the job can still be awarded, and a pass
// built over the fixture's fully wired service could not show it.
func (m market) sweepAt(t *testing.T, at time.Time) (int, error) {
	t.Helper()

	service := NewService(events.NewOutbox(), nil, nil, nil, clock.NewFixed(at))

	var claimed int
	err := db.InTx(t.Context(), m.pool, func(ctx context.Context, r db.Runner) error {
		rows, err := r.Query(ctx, ExpiryClaim, at, ExpiryBatch)
		if err != nil {
			return err
		}

		var due []uuid.UUID
		for rows.Next() {
			var id uuid.UUID
			if err := rows.Scan(&id); err != nil {
				rows.Close()
				return err
			}
			due = append(due, id)
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return err
		}

		for _, id := range due {
			if _, err := service.Expire(ctx, r, id); err != nil {
				return err
			}
		}
		claimed = len(due)
		return nil
	})
	return claimed, err
}

// pastCollection is an instant after the fixture offer's `pickup_at`, which is [testInstant] plus
// forty-eight hours. Anything later than that makes the offer due.
var pastCollection = testInstant.Add(49 * time.Hour)

// TestAnOfferPastItsCollectionTimeExpires is SHIP-89's *Done when*, first half.
func TestAnOfferPastItsCollectionTimeExpires(t *testing.T) {
	m := newMarket(t)

	bid, _, err := m.place(t, m.provider, m.job, offer("expiry-happy"))
	if err != nil {
		t.Fatalf("placing the offer: %v", err)
	}

	claimed, err := m.sweepAt(t, pastCollection)
	if err != nil {
		t.Fatalf("the sweep failed: %v", err)
	}
	if claimed != 1 {
		t.Fatalf("the sweep claimed %d offers, want the one whose collection time has passed", claimed)
	}

	if got := m.row(t, bid.ID).status; got != string(StatusExpired) {
		t.Errorf("the offer is %s after the sweep, want Expired", got)
	}
}

// TestAnOfferWhoseCollectionTimeIsStillAheadIsUntouched is the other half of the same rule, and it
// is the one a mutation reaches. Delete the deadline from [ExpiryClaim] and this fails while the
// test above still passes.
func TestAnOfferWhoseCollectionTimeIsStillAheadIsUntouched(t *testing.T) {
	m := newMarket(t)

	bid, _, err := m.place(t, m.provider, m.job, offer("expiry-live"))
	if err != nil {
		t.Fatalf("placing the offer: %v", err)
	}

	// An hour after the offer was placed and forty-seven hours before it collects.
	claimed, err := m.sweepAt(t, testInstant.Add(time.Hour))
	if err != nil {
		t.Fatalf("the sweep failed: %v", err)
	}
	if claimed != 0 {
		t.Fatalf("the sweep claimed %d offers, want none: nothing is due", claimed)
	}

	if got := m.row(t, bid.ID).status; got != string(StatusSubmitted) {
		t.Errorf("the live offer is %s after the sweep, want Submitted", got)
	}
}

// TestTheExpirySweepLeavesEveryClosedOfferAsItWas is the assertion with no constraint behind
// it.
//
// `ck_bids_superseded_is_not_live` refuses `Submitted` on a displaced row and nothing else here is
// refused by the schema at all: PostgreSQL would happily write `Expired` over `Withdrawn`,
// `Rejected` or `Accepted`. **The claim's `status = 'Submitted'` is the only thing standing between
// the sweep and a rewritten history**, which is why there is a case per closed status rather than
// one for the shape.
//
// Every one of them is given a collection time in the past, so the deadline is not what is
// excluding them.
func TestTheExpirySweepLeavesEveryClosedOfferAsItWas(t *testing.T) {
	for _, closed := range []Status{
		StatusAccepted, StatusRejected, StatusWithdrawn, StatusSuperseded, StatusExpired,
	} {
		t.Run(string(closed), func(t *testing.T) {
			m := newMarket(t)

			bid, _, err := m.place(t, m.provider, m.job, offer("expiry-"+string(closed)))
			if err != nil {
				t.Fatalf("placing the offer: %v", err)
			}
			m.setStatus(t, bid.ID, closed)

			claimed, err := m.sweepAt(t, pastCollection)
			if err != nil {
				t.Fatalf("the sweep failed: %v", err)
			}
			if claimed != 0 {
				t.Fatalf("the sweep claimed %d offers, want none: the offer is already %s",
					claimed, closed)
			}

			if got := m.row(t, bid.ID).status; got != string(closed) {
				t.Errorf("the offer reads %s after the sweep, want %s — the sweep "+
					"overwrote the record of how it ended", got, closed)
			}
		})
	}
}

// TestAnOfferWithNoCollectionTimeIsNotSwept holds the claim's `pickup_at IS NOT NULL`.
//
// No endpoint can produce such a row — [Offer.validate] refuses an offer naming neither instant —
// and the column is nullable and no constraint says otherwise (000501 removed
// `ck_bids_offer_has_timing`, and Docs/11 §9 still carries it). A claim that relied on the
// validator holding would sweep a row whose terms it cannot read the first time some other writer
// skipped one.
func TestAnOfferWithNoCollectionTimeIsNotSwept(t *testing.T) {
	m := newMarket(t)

	bid, _, err := m.place(t, m.provider, m.job, offer("expiry-untimed"))
	if err != nil {
		t.Fatalf("placing the offer: %v", err)
	}
	exec(t, m.pool, `UPDATE bids SET pickup_at = NULL, deliver_by = NULL WHERE id = $1`, bid.ID)

	claimed, err := m.sweepAt(t, pastCollection)
	if err != nil {
		t.Fatalf("the sweep failed: %v", err)
	}
	if claimed != 0 {
		t.Fatalf("the sweep claimed %d offers, want none: this one names no collection time", claimed)
	}
}

// TestAnExpiryEmitsItsEvent is the second half of the *Done when*, read out of the outbox.
//
// SHIP-136 registered no `bid.expired` schema because nothing wrote the status, and left a check in
// `scripts/verify/61-bidding.sh` asserting that no bid is `Expired` so that the day one appeared
// would name this ticket. This is that pair closed from the Go side.
func TestAnExpiryEmitsItsEvent(t *testing.T) {
	m := newMarket(t)

	bid, _, err := m.place(t, m.provider, m.job, offer("expiry-event"))
	if err != nil {
		t.Fatalf("placing the offer: %v", err)
	}
	if _, err := m.sweepAt(t, pastCollection); err != nil {
		t.Fatalf("the sweep failed: %v", err)
	}

	history := emitted(t, m, bid.ID)
	if len(history) != 2 {
		t.Fatalf("the offer emitted %s, want the placement and the expiry", describe(history))
	}
	if history[1].eventType != EventBidExpired {
		t.Errorf("the second event is %s, want %s", history[1].eventType, EventBidExpired)
	}
	if got := field(t, history[1], "status"); got != string(StatusExpired) {
		t.Errorf("the payload's status is %s, want Expired", got)
	}
	if got := field(t, history[1], "bid_id"); got != bid.ID.String() {
		t.Errorf("the payload names %s, want the offer that expired", got)
	}
}

// TestAnExpiryOutsideATransactionIsRefused is the outbox contract, checked before anything is
// written: a status that commits when its event does not is an offer nobody is told about.
func TestAnExpiryOutsideATransactionIsRefused(t *testing.T) {
	m := newMarket(t)

	bid, _, err := m.place(t, m.provider, m.job, offer("expiry-no-tx"))
	if err != nil {
		t.Fatalf("placing the offer: %v", err)
	}

	service := NewService(events.NewOutbox(), nil, nil, nil, clock.NewFixed(pastCollection))
	if _, err := service.Expire(t.Context(), m.pool, bid.ID); !errors.Is(err, ErrNotInTransaction) {
		t.Fatalf("expiring on the pool answered %v, want ErrNotInTransaction", err)
	}
	if got := m.row(t, bid.ID).status; got != string(StatusSubmitted) {
		t.Errorf("the offer is %s, want Submitted: nothing should have been written", got)
	}
}

// TestExpiringAnOfferThatIsNotDueIsReportedRatherThanDone is the store's own compare-and-set, and
// it is the redundancy that catches a claim which stopped reading the deadline.
//
// Unreachable through the worker — the claim selected the row and holds its lock — and reported
// rather than answered as success, because a sweep that said "nothing happened" would let a pass
// report work it did not do.
func TestExpiringAnOfferThatIsNotDueIsReportedRatherThanDone(t *testing.T) {
	m := newMarket(t)

	bid, _, err := m.place(t, m.provider, m.job, offer("expiry-not-due"))
	if err != nil {
		t.Fatalf("placing the offer: %v", err)
	}

	// Judged an hour after placement, when the offer collects in forty-seven hours' time.
	service := NewService(events.NewOutbox(), nil, nil, nil, clock.NewFixed(testInstant.Add(time.Hour)))
	err = db.InTx(t.Context(), m.pool, func(ctx context.Context, r db.Runner) error {
		_, err := service.Expire(ctx, r, bid.ID)
		return err
	})
	if err == nil {
		t.Fatal("expiring an offer that is not due succeeded")
	}
	if got := m.row(t, bid.ID).status; got != string(StatusSubmitted) {
		t.Errorf("the offer is %s, want Submitted", got)
	}
}

// TestAnExpiredOfferIsOverForEveryVerbThatActsOnIt is the wire-visible consequence, at the service.
//
// [Status.live] is the one predicate three verbs gate on, so an offer this sweep closed is refused
// by all three without any of them naming expiry. Asserted rather than assumed, because "Expired
// is not live" is the whole reason the sweep is safe to run against a marketplace.
func TestAnExpiredOfferIsOverForEveryVerbThatActsOnIt(t *testing.T) {
	m := newMarket(t)

	bid, _, err := m.place(t, m.provider, m.job, offer("expiry-closes"))
	if err != nil {
		t.Fatalf("placing the offer: %v", err)
	}
	if _, err := m.sweepAt(t, pastCollection); err != nil {
		t.Fatalf("the sweep failed: %v", err)
	}

	if _, err := m.revise(t, m.provider, m.job, bid.ID,
		Revision{AmountCents: ptr(int64(40000))}); !errors.Is(err, ErrBidClosed) {
		t.Errorf("revising an expired offer answered %v, want ErrBidClosed", err)
	}
	if _, _, err := m.counter(t, m.customer, m.job, bid.ID,
		counterOf(40000, "expiry-counter")); !errors.Is(err, ErrBidClosed) {
		t.Errorf("countering an expired offer answered %v, want ErrBidClosed", err)
	}
	if _, err := m.award(t, m.customer, m.job, bid.ID); !errors.Is(err, ErrBidClosed) {
		t.Errorf("awarding an expired offer answered %v, want ErrBidClosed", err)
	}
}

// TestTheExpirySweepFreesTheProviderToBidAgain is the index's predicate seen from the provider's
// side.
//
// `uq_bids_one_submitted_per_provider_per_job` is partial on `Submitted`, so an expired offer
// leaves it — which is what 000502's header predicted this ticket would need and why it needed no
// schema change to the constraint. The same property SHIP-86 established for a withdrawal.
func TestTheExpirySweepFreesTheProviderToBidAgain(t *testing.T) {
	m := newMarket(t)

	if _, _, err := m.place(t, m.provider, m.job, offer("expiry-first")); err != nil {
		t.Fatalf("placing the first offer: %v", err)
	}
	if _, err := m.sweepAt(t, pastCollection); err != nil {
		t.Fatalf("the sweep failed: %v", err)
	}

	// A fresh offer, priced against a clock that has moved past the first one's collection
	// time — so its own timing is in the future and the validator accepts it.
	at := clock.NewFixed(pastCollection)
	later := NewService(events.NewOutbox(), fleet.NewService(at),
		newTestNegotiation(at), newTestAwarding(at), at)

	second := Offer{
		AmountCents: 47000,
		PickupAt:    pastCollection.Add(24 * time.Hour),
		DeliverBy:   pastCollection.Add(32 * time.Hour),
		Key:         "expiry-second",
	}
	err := db.InTx(t.Context(), m.pool, func(ctx context.Context, r db.Runner) error {
		_, _, err := later.PlaceBid(ctx, r, m.provider, m.job, second)
		return err
	})
	if err != nil {
		t.Fatalf("placing again after the expiry: %v", err)
	}
	if got := m.bids(t, m.provider, m.job); got != 2 {
		t.Errorf("the provider has %d rows on the job, want the expired offer and its replacement", got)
	}
}
