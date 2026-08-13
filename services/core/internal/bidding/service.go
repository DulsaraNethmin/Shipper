package bidding

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/DulsaraNethmin/Shipper/services/core/internal/clock"
	"github.com/DulsaraNethmin/Shipper/services/core/internal/db"
	"github.com/DulsaraNethmin/Shipper/services/core/internal/validate"
)

// The rules of placing an offer (SHIP-84).
//
// # The validation limits
//
// Bounds against a slipped decimal point and a runaway text field, not judgements about what a good
// offer looks like. Constants for the reason jobs' are: these do not move under operational
// pressure. What will — a commercial cap on what this marketplace carries — is reference data
// (Docs/06 §5.3, SHIP-58) and belongs nowhere near a validator.
const (
	// maxOfferCents is $1,000,000, in minor units.
	//
	// **The same number as jobs.maxBudgetCents, arrived at independently and deliberately not
	// shared.** Two domains do not import each other, so the alternative to a second constant is a
	// shared money package — `internal/money` is registered in internal/boundaries and unwritten,
	// and internal/boundaries is a shared file a domain branch may not edit. This is therefore the
	// second independent copy of a money bound in the service, and Docs/11 §3 records it as the
	// trigger the eventual money ticket should be aimed at rather than leaving it to be rediscovered.
	//
	// Far above any road-transport job this marketplace expects and far below what numeric(12,2) can
	// hold, which is the gap that matters: the validator refuses an implausible amount and names the
	// field, rather than letting ck_bids_amount or an overflow answer with something a provider
	// cannot act on.
	maxOfferCents = 100_000_000

	// maxMessageLen matches ck_bids_message, so the refusal names the field rather than arriving as
	// a constraint violation nobody can read.
	maxMessageLen = 2000

	// maxLeadTime bounds how far ahead an offer may be scheduled. Two years is far beyond any
	// delivery this marketplace arranges and near enough to catch a year typed wrong, which is the
	// mistake this exists for — 2027 for 2026 sails past every other check.
	maxLeadTime = 2 * 365 * 24 * time.Hour
)

// Service is this domain's rules.
//
// It holds the eligibility port rather than reaching for one per call, so that a caller cannot
// supply a different answer to "may this provider bid" on one request than on another. The store is
// a value with no state for the reason jobs' and fleet's are: it is a namespace for SQL, not a
// dependency to swap. Docs/06 §4.1 is explicit that the database is not abstracted, and the partial
// unique indexes this file is built against are PostgreSQL's.
type Service struct {
	eligibility Eligibility
	store       postgresStore
	clock       clock.Clock
}

// NewService wires the domain to what it cannot decide for itself.
//
// The clock is injected (Docs/10 §6.3) because two of the three timing rules compare against "now",
// and a test that had to wait for wall-clock time to pass would either be slow or be flaky.
func NewService(eligibility Eligibility, c clock.Clock) *Service {
	return &Service{eligibility: eligibility, clock: c}
}

// PlaceBid records one provider's offer against one job (SHIP-84).
//
// `created` is false when this key had already placed this offer, which is a retry rather than a
// second bid. The bid returned is the one the first attempt wrote, and nothing further is recorded.
//
// r must be a transaction. The eligibility answer and the row it authorises are one decision made
// against one version of the world: a job can be cancelled, expire, or be awarded between two
// statements, and a bid written against a stale "yes" is a bid nobody checked.
//
// # The order of the four steps is the design, and each one is in front of the next for a reason
//
//  1. **The retry is answered before anything else.** A request carrying a key that already placed
//     an offer is answered from that row without consulting eligibility at all — because by the time
//     a retry outlives the middleware's cache, the job may well have closed, and refusing it would
//     tell a provider their bid failed when it is live and awaiting an answer. A retry asks "what
//     happened to my request", and the answer to that does not change when the world does.
//  2. **Eligibility, which is fleet's and not this domain's.** See [Eligibility]. A job this
//     provider may not bid on is indistinguishable from one that does not exist.
//  3. **The insert, written as though the middleware were not there.** ON CONFLICT DO NOTHING
//     against uq_bids_idempotency absorbs the concurrent duplicate that step 1 cannot see — two
//     requests carrying one key, arriving together, both finding nothing.
//  4. **A second live offer is the database's refusal, not a check.** There is no SELECT asking
//     whether this provider has already bid. uq_bids_one_submitted_per_provider_per_job raises, and
//     the raise is translated to [ErrAlreadyBid]. A check-then-insert would be correct in a
//     single-threaded reading and wrong under two taps on one phone.
//
// # What this does not do: it does not move the job
//
// Docs/02 §2 has `Open → Negotiating` on "first bid or counter-offer submitted", and that is
// **SHIP-90's** ticket, which depends on SHIP-87 and SHIP-57. It is deliberately not done here.
// Negotiating is a presentation status — Docs/02 §1 says so in as many words — and moving the job
// would change nothing about what may happen to it, since a Negotiating job "remains open to
// eligible bids". Doing it early would also make every bid a status transition, with a
// job_status_history row and a lock on the job, for a change nobody reads yet.
func (s *Service) PlaceBid(ctx context.Context, r db.Runner, providerID, jobID uuid.UUID, o Offer) (Bid, bool, error) {
	if providerID == uuid.Nil {
		return Bid{}, false, fmt.Errorf("bidding: an offer names no provider: %w", ErrJobNotOffered)
	}
	if jobID == uuid.Nil {
		return Bid{}, false, ErrJobNotOffered
	}

	offer := o.normalise()
	if offer.Key == "" {
		return Bid{}, false, fmt.Errorf("bidding: an offer on %s: %w", jobID, ErrNoIdempotencyKey)
	}
	if err := offer.validate(s.clock.Now()); err != nil {
		return Bid{}, false, err
	}

	// 1. The retry, answered before the world is consulted.
	if existing, found, err := s.store.bidPlacedUnder(ctx, r, jobID, providerID, offer.Key); err != nil {
		return Bid{}, false, err
	} else if found {
		return existing, false, nil
	}

	// 2. Eligibility, which is fleet's answer and not this domain's.
	permitted, err := s.eligibility.EligibleFor(ctx, r, providerID, jobID)
	if err != nil {
		return Bid{}, false, fmt.Errorf("bidding: deciding whether %s may bid on %s: %w", providerID, jobID, err)
	}
	if !permitted {
		return Bid{}, false, ErrJobNotOffered
	}

	id, err := uuid.NewV7()
	if err != nil {
		return Bid{}, false, fmt.Errorf("bidding: generating a bid id: %w", err)
	}

	// 3 and 4. The insert, and the two indexes that answer for it.
	bid, created, err := s.store.insertBid(ctx, r, Bid{
		ID:          id,
		JobID:       jobID,
		ProviderID:  providerID,
		Status:      StatusSubmitted,
		AmountCents: offer.AmountCents,
		PickupAt:    offer.PickupAt,
		DeliverBy:   offer.DeliverBy,
		Message:     offer.Message,
		Key:         offer.Key,
	})
	switch {
	case err != nil:
		return Bid{}, false, err
	case created:
		return bid, true, nil
	}

	// The insert declined, so another request carrying this key committed while this one was in
	// flight. At READ COMMITTED each statement takes a fresh snapshot, so that row is visible here
	// even though this transaction began before it.
	existing, found, err := s.store.bidPlacedUnder(ctx, r, jobID, providerID, offer.Key)
	if err != nil {
		return Bid{}, false, err
	}
	if !found {
		// Unreachable: ON CONFLICT declined, so a row with this key exists. Reported rather than
		// papered over, because the alternative is answering a client with a zero-valued bid.
		return Bid{}, false, fmt.Errorf(
			"bidding: %s conflicted on %s and then could not be read back", offer.Key, jobID)
	}
	return existing, false, nil
}

// validate is what an offer has to be before the platform will carry it.
//
// Every failure is a field error in the error contract's shape (Docs/10 §4.6), and they are
// collected rather than returned one at a time: a provider who mistypes the amount and the date
// should be told about both at once rather than fixing one and being sent back for the other.
//
// # Every field is required, which is the opposite of a job draft and deliberately so
//
// jobs.DraftFields makes everything optional because Docs/01 §4.1 lets a customer save a draft and
// come back to it. An offer has no such state at this endpoint: `POST /v1/jobs/{id}/bids` places a
// live offer that a customer can act on immediately, and ck_bids_offer_has_an_amount and
// ck_bids_offer_has_timing already refuse an incomplete one at the column. Reporting that as three
// named fields rather than as a constraint violation is the whole of what this adds.
//
// (Docs/02 §4's 'Draft' status is where an incomplete offer would live. Nothing writes it — no
// ticket has asked for a bid a provider composes over several sittings — and 000500 keeps the status
// for when one does.)
//
// # The three timing rules, and the one that is deliberately absent
//
// A pickup in the past is not an offer anyone can accept, and a delivery before its own pickup is
// not a delivery. Both are refused. What is **not** checked is whether the timing falls inside the
// job's own windows: a provider offering Thursday against a customer who asked for Wednesday is
// making an offer the customer is free to decline, and Docs/01 §4.3's answer to bids that miss is
// better job detail rather than a platform that refuses them. Migration 000501 records the same
// split from the schema's side.
func (o Offer) validate(now time.Time) error {
	var e validate.Errors

	switch {
	case o.AmountCents <= 0:
		e.Add("amount_cents", validate.CodeRequired,
			"Enter what you are asking for this job, in cents.")
	case o.AmountCents > maxOfferCents:
		e.Add("amount_cents", validate.CodeOutOfRange,
			"Enter an amount between $1 and $%d.", maxOfferCents/100)
	}

	switch {
	case o.PickupAt.IsZero():
		e.Add("pickup_at", validate.CodeRequired, "Say when you can collect.")
	case !o.PickupAt.After(now):
		e.Add("pickup_at", validate.CodeOutOfRange, "Enter a collection time in the future.")
	case o.PickupAt.Sub(now) > maxLeadTime:
		e.Add("pickup_at", validate.CodeOutOfRange,
			"That is more than two years away. Check the year.")
	}

	switch {
	case o.DeliverBy.IsZero():
		e.Add("deliver_by", validate.CodeRequired, "Say when it will be delivered.")
	case !o.PickupAt.IsZero() && !o.DeliverBy.After(o.PickupAt):
		e.Add("deliver_by", validate.CodeOutOfRange,
			"Delivery has to be after collection.")
	case o.DeliverBy.Sub(now) > maxLeadTime:
		e.Add("deliver_by", validate.CodeOutOfRange,
			"That is more than two years away. Check the year.")
	}

	if len(o.Message) > maxMessageLen {
		e.Add("message", validate.CodeTooLong, "Keep this to %d characters or fewer.", maxMessageLen)
	}

	return e.Err()
}
