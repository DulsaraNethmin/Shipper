package bidding

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/DulsaraNethmin/Shipper/services/core/internal/db"
	"github.com/DulsaraNethmin/Shipper/services/core/internal/pagination"
)

// A provider's own bids (SHIP-101a).
//
// `GET /v1/fleet/bids`, and the read SHIP-101's screen has nothing to work from without. Docs/11 §6
// struck that ticket for two waves as "every dependency met and unbuildable in fact", because the
// only bidding read on the served surface was one negotiation's history — which needs a job
// identifier and a bid identifier the provider would have to have already.
//
// # What "their own bids" is scoped by, and the one place that reading is wider than it looks
//
// `bids.provider_id`, which since 000502 means **the provider a negotiation is with** rather than
// the author of any one row. A customer's counter-offer carries the same provider id and is told
// apart by `offered_by`.
//
// So this lists **every row in the negotiations this provider is in**, not only the rows they wrote,
// and that is deliberate. The screen this serves has to show four things — which offers are live,
// which were accepted, which lost, and **which are waiting on an answer from this provider** — and
// the fourth is a customer's counter. Excluding it would hide the one row that needs an action, and
// the provider can already read it a negotiation at a time through
// `GET /v1/jobs/{id}/bids/{bid_id}/history`. Every row says who offered it, so nothing is ambiguous.
//
// **It is never wider than that.** The provider comes from the authenticated subject and there is no
// parameter that widens the scope, so another provider's offer is not refused here — it is never
// selected. That is the same construction `fleet.Service.Vehicles` uses and states.
//
// # The customer's budget is not here, and structurally cannot be
//
// Nothing in this package reads a job at all beyond its identifier ([Bid]), and this list answers
// with the same [bidResponse] every other endpoint in the domain answers with — one closed key set
// rather than two. Docs/01 §4.3 is therefore true here by construction rather than by redaction, and
// TestTheBidResponseCarriesNothingOfTheCustomers holds the shape whatever it is reached through.

// BidQuery is what a provider may narrow their list by.
//
// **One optional status and nothing else.** SHIP-101's *Done when* is "provider sees their own bids
// grouped by status", and the grouping is the client's: Docs/10 §4.5's collection envelope is a flat
// array with a cursor, so a response of named buckets would have to page each bucket separately or
// abandon paging — and a screen that groups four live offers does not need the platform's help.
//
// What the platform owes it is the ability to *ask for one group*, which is this, and the status on
// every row, which [bidResponse] already carries. `GET /v1/jobs`'s status filter is the same shape
// and the same reasoning.
type BidQuery struct {
	// Status narrows to one of Docs/02 §4's eight. The zero value is every status.
	Status Status

	// Limit is the page size. Zero means the configured default.
	Limit int

	// After is the position the previous page ended at.
	After BidCursor
}

// BidCursor is a position in the list: the ordering key of the last row of a page.
//
// Two fields for [pagination.Cursor]'s reason: the list is ordered by `created_at` and a provider
// placing offers in one sitting writes rows in the same millisecond, so a cursor that could not
// break the tie would repeat or drop an offer at exactly the page boundary.
type BidCursor struct {
	CreatedAt time.Time
	ID        uuid.UUID
}

func (c BidCursor) IsZero() bool { return c.ID == uuid.Nil }

// BidPage is one page of a provider's bids, newest first.
type BidPage struct {
	Bids []Bid

	// Next is the cursor for the following page, zero when this is the last one.
	Next BidCursor

	// HasMore says whether Next names anything.
	HasMore bool
}

// Bids is the provider's own negotiations, newest first (SHIP-101a).
//
// Keyset rather than offset, per Docs/10 §4.5 and through `internal/pagination` rather than a page
// size of this domain's own — every list endpoint needs the same answer and the bounds come from
// configuration (SHIP-15g).
//
// No transaction and no lock. This is a read, and a row that changed under it would produce an older
// row beside a newer one rather than an inconsistent one — the same reading [Service.Chain] takes.
func (s *Service) Bids(ctx context.Context, r db.Runner, providerID uuid.UUID, q BidQuery) (BidPage, error) {
	if providerID == uuid.Nil {
		return BidPage{}, fmt.Errorf("bidding: a bid list names no provider: %w", ErrJobNotOffered)
	}
	if q.Status != "" && !q.Status.Valid() {
		return BidPage{}, fmt.Errorf("bidding: %q is not a bid status: %w", q.Status, ErrBidNotFound)
	}

	limit := q.Limit
	switch {
	case limit <= 0:
		limit = pagination.DefaultLimit
	case limit > pagination.MaxLimit:
		limit = pagination.MaxLimit
	}

	// One more row than was asked for, which answers "is there another page" without a second query
	// and without counting the whole set. The extra is dropped below and never reaches a caller.
	found, err := s.store.bidsFor(ctx, r, providerID, q.Status, q.After, limit+1)
	if err != nil {
		return BidPage{}, err
	}

	page := BidPage{Bids: found}
	if len(found) > limit {
		page.Bids = found[:limit]

		last := page.Bids[limit-1]
		page.Next = BidCursor{CreatedAt: last.CreatedAt, ID: last.ID}
		page.HasMore = true
	}
	return page, nil
}
