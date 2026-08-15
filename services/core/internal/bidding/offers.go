package bidding

import (
	"context"
	"fmt"

	"github.com/google/uuid"

	"github.com/DulsaraNethmin/Shipper/services/core/internal/db"
	"github.com/DulsaraNethmin/Shipper/services/core/internal/pagination"
)

// The customer's view of the offers on their own job (SHIP-102a).
//
// `GET /v1/jobs/{id}/bids/received`, and the read SHIP-102's comparison screen has nothing to work
// from without. Docs/11 §6 struck that ticket for two waves as "every dependency met and unbuildable
// in fact": every clause of its *Done when* — price, timing, provider profile, vehicle — was
// unserved, because the only bidding reads on the surface were a provider's own bids and one
// negotiation's history, and the history needs a bid identifier the customer would have to hold
// already.
//
// This is the mirror of [Service.Bids] and it differs in exactly one structural way, which is where
// all of its difficulty is: **that read is scoped by a column and this one is scoped by another
// domain's fact.** A provider's own bids are `provider_id = <the caller>`, so another provider's
// offer is never selected. A customer does not appear on `bids` at all — 000500 has no
// `customer_id`, deliberately, because the customer is the job's — so ownership is a question for
// `jobs`, asked through [Negotiation.CustomerOf] before a single row is read.
//
// # "A provider gets what a stranger gets", which is a clause of the *Done when* rather than a
// consequence
//
// The ownership check is the whole of it. A provider who is bidding on the job is not its customer,
// so [Negotiation.CustomerOf] answers false and they are refused with [ErrNotJobCustomer] — the
// identical 404 an account with no connection to the job receives, and the identical 404 a job that
// does not exist receives. **There is no branch anywhere in this file that asks whether the caller
// is a provider**, which is what makes the three answers the same by construction rather than by
// three code paths agreeing.
//
// That matters more here than on any other read in this domain. This endpoint is the one place a
// competing provider could otherwise obtain every rival's price on a job in one request — Docs/01
// §4.3's second privacy rule, and the disclosure [visibility.go]'s fourth rule names as "the one
// that matters".
//
// # The customer's budget is not here, and it is not here in three separate ways
//
// Docs/01 §4.3 forbids exposing it "not as an amount, not as a band, and not as a 'budget supplied'
// indicator", and this response is *customer*-facing — so the invariant is not about who reads this
// page but about who must never be able to reach it, which the paragraph above settles.
//
// What the shape guarantees separately is that nothing of the job travels at all. [Bid] carries the
// job's identifier and no field of it, [ProviderSummary] and [VehicleSummary] are closed sets
// declared in ports.go with no job in them, and [Directory] is handed provider and vehicle
// identifiers rather than a job. So there is no field to redact and no query that could acquire one.
//
// # Why the page is cut before the descriptions are read
//
// [Directory.Describe] is asked about the page the customer will actually see, not about the extra
// row that answers "is there another page". A page of twenty offers is two statements — one for the
// providers, one for the vehicles — however many offers there are, and the row that exists only to
// set `has_more` is dropped before either runs.

// OfferQuery is what a customer may narrow the offers on their job by.
//
// The same shape as [BidQuery] and deliberately so — one status, a limit and a cursor — because the
// two endpoints are two screens over one table and a client that learned one has learned the other.
type OfferQuery struct {
	// Status narrows to one of Docs/02 §4's eight.
	//
	// **The zero value is the live offers rather than every offer, and that is the one place this
	// differs from [BidQuery].** The difference is what each screen is for. A provider's list is a
	// record of what they have done — every offer they have made, whatever became of it — so
	// showing all eight statuses by default is the answer. A customer's is a *decision*: they are
	// choosing between the offers standing right now, and a default that mixed in every superseded
	// counter and every withdrawn offer would put rows on a comparison screen that cannot be
	// awarded.
	//
	// `?status=` reaches the rest, which is how a customer sees which offer they accepted after the
	// job is awarded, or what a negotiation looked like before their counter displaced it.
	Status Status

	// Limit is the page size. Zero means the configured default.
	Limit int

	// After is the position the previous page ended at.
	After BidCursor
}

// ReceivedOffer is one offer on the customer's job, with the two things about it that are not in
// `bids`.
//
// A struct rather than three parallel slices, so that a row and its description cannot come apart —
// which is the failure a map keyed on the provider would eventually produce.
type ReceivedOffer struct {
	Bid Bid

	// Provider is the closed customer-facing summary of who made the offer.
	//
	// Always present. Its zero value is what a provider whose account [Directory] could not describe
	// gets, and a page of offers does not fail because one of them is unreadable — see
	// [Directory.Describe].
	Provider ProviderSummary

	// Vehicle is the vehicle the offer is made with, and HasVehicle says whether it names one.
	//
	// False is the ordinary answer rather than an edge case: `bids.vehicle_id` arrived at 000504 and
	// every offer placed before it names none, as does every offer from a client that has not yet
	// started sending the field.
	Vehicle    VehicleSummary
	HasVehicle bool
}

// ReceivedPage is one page of the offers on a customer's job, newest first.
type ReceivedPage struct {
	Offers []ReceivedOffer

	// Next is the cursor for the following page, zero when this is the last one.
	Next BidCursor

	// HasMore says whether Next names anything.
	HasMore bool
}

// Offers is every live offer on one of the customer's jobs, newest first (SHIP-102a).
//
// Keyset rather than offset, per Docs/10 §4.5 and through `internal/pagination`, which is
// [Service.Bids]' arrangement and the same one.
//
// No transaction and no lock, for [Service.Bids]' reason: this is a read, and a row that changed
// under it would produce an older row beside a newer one rather than an inconsistent one.
//
// **Ordered by `created_at` rather than by price**, which is worth stating because a comparison
// screen sorts by price. Docs/01 §4.3 says the budget is "used only on the customer's side, to
// filter and sort the bids they receive" — the sorting is the client's, over a page it holds, and
// the cursor has to be over something stable. `idx_bids_job` is `(job_id, created_at DESC)` from
// 000500, which is this predicate and this order exactly.
func (s *Service) Offers(
	ctx context.Context,
	r db.Runner,
	customerID, jobID uuid.UUID,
	q OfferQuery,
) (ReceivedPage, error) {
	if customerID == uuid.Nil || jobID == uuid.Nil {
		return ReceivedPage{}, fmt.Errorf("bidding: offers on %s for %s: %w",
			jobID, customerID, ErrNotJobCustomer)
	}
	if q.Status != "" && !q.Status.Valid() {
		return ReceivedPage{}, fmt.Errorf("bidding: %q is not a bid status: %w", q.Status, ErrBidNotFound)
	}
	if s.negotiation == nil {
		return ReceivedPage{}, fmt.Errorf(
			"bidding: no negotiation port is wired, so ownership of %s cannot be established", jobID)
	}

	// Ownership first, before a single row of `bids` is read. Not an optimisation: it is the whole
	// of "a provider gets what a stranger gets", and a query that ran first would be a query whose
	// timing differs between a job that exists and one that does not.
	owns, err := s.negotiation.CustomerOf(ctx, r, customerID, jobID)
	if err != nil {
		return ReceivedPage{}, fmt.Errorf(
			"bidding: deciding whether %s owns %s: %w", customerID, jobID, err)
	}
	if !owns {
		return ReceivedPage{}, fmt.Errorf("bidding: offers on %s: %w", jobID, ErrNotJobCustomer)
	}

	status := q.Status
	if status == "" {
		status = StatusSubmitted
	}

	limit := q.Limit
	switch {
	case limit <= 0:
		limit = pagination.DefaultLimit
	case limit > pagination.MaxLimit:
		limit = pagination.MaxLimit
	}

	// One more row than was asked for, which answers "is there another page" without a second query
	// and without counting the whole set. The extra is dropped below and never reaches a caller —
	// nor [Directory], which is asked only about the page that will be rendered.
	found, err := s.store.offersOn(ctx, r, jobID, status, q.After, limit+1)
	if err != nil {
		return ReceivedPage{}, err
	}

	page := ReceivedPage{}
	if len(found) > limit {
		last := found[limit-1]
		page.Next = BidCursor{CreatedAt: last.CreatedAt, ID: last.ID}
		page.HasMore = true
		found = found[:limit]
	}

	described, err := s.describe(ctx, r, found)
	if err != nil {
		return ReceivedPage{}, err
	}

	page.Offers = make([]ReceivedOffer, 0, len(found))
	for _, bid := range found {
		detail := described[bid.ID]
		page.Offers = append(page.Offers, ReceivedOffer{
			Bid:        bid,
			Provider:   detail.Provider,
			Vehicle:    detail.Vehicle,
			HasVehicle: detail.HasVehicle,
		})
	}
	return page, nil
}

// describe asks [Directory] about one page of offers.
//
// Separate from [Service.Offers] so that the "no port wired" case is one branch rather than one
// scattered through the assembly loop, and so that an empty page costs no call at all.
//
// **A missing port is an error and not an empty description**, for the reason
// [Service.vehicleUsable] gives: a service that cannot answer the question must say so. This one is
// stricter than it looks — every element of this response has a provider, so a page rendered with
// zero-valued summaries would be a screen of blank providers reported as a success.
func (s *Service) describe(
	ctx context.Context,
	r db.Runner,
	bids []Bid,
) (map[uuid.UUID]OfferorDetail, error) {
	if len(bids) == 0 {
		return nil, nil
	}
	if s.directory == nil {
		return nil, fmt.Errorf("bidding: no directory port is wired, so the %d offers on %s "+
			"cannot be described", len(bids), bids[0].JobID)
	}

	offerors := make(map[uuid.UUID]Offeror, len(bids))
	for _, bid := range bids {
		offerors[bid.ID] = Offeror{ProviderID: bid.ProviderID, VehicleID: bid.VehicleID}
	}

	described, err := s.directory.Describe(ctx, r, offerors)
	if err != nil {
		return nil, fmt.Errorf("bidding: describing the offers on %s: %w", bids[0].JobID, err)
	}
	return described, nil
}
