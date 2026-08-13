package bidding

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

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

	// maxChainLength bounds one negotiation's history in a single response (SHIP-88).
	//
	// A truncation report rather than a page, which is the reading `identity`'s device list takes and
	// for the same reason: two people haggling over one delivery exchange a handful of rounds, so a
	// cursor would be machinery for a case that does not occur — while an unbounded response is a
	// promise this API should not make about a set no constraint bounds. `has_more` says the answer
	// was cut rather than inviting anybody to ask for the rest.
	//
	// Docs/02 §4 requires the history to remain visible, and a hundred rounds is far past the point
	// at which the next one is the one somebody needs.
	maxChainLength = 100
)

// Service is this domain's rules.
//
// It holds both ports rather than reaching for one per call, so that a caller cannot supply a
// different answer to "may this provider bid" or "is this the job's customer" on one request than on
// another. The store is a value with no state for the reason jobs' and fleet's are: it is a namespace
// for SQL, not a dependency to swap. Docs/06 §4.1 is explicit that the database is not abstracted,
// and the partial unique indexes this file is built against are PostgreSQL's.
type Service struct {
	eligibility Eligibility
	negotiation Negotiation
	store       postgresStore
	clock       clock.Clock
}

// NewService wires the domain to what it cannot decide for itself.
//
// Two ports since SHIP-87, because a counter-offer is the first thing in this domain a *customer*
// may do and "is this the customer who owns that job" is `jobs`' fact. See ports.go.
//
// The clock is injected (Docs/10 §6.3) because two of the three timing rules compare against "now",
// and a test that had to wait for wall-clock time to pass would either be slow or be flaky.
func NewService(eligibility Eligibility, negotiation Negotiation, c clock.Clock) *Service {
	return &Service{eligibility: eligibility, negotiation: negotiation, clock: c}
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
	if existing, found, err := s.store.bidPlacedUnder(
		ctx, r, jobID, providerID, PartyProvider, offer.Key); err != nil {
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
		OfferedBy:   PartyProvider,
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
	existing, found, err := s.store.bidPlacedUnder(ctx, r, jobID, providerID, PartyProvider, offer.Key)
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

// ReviseBid changes a provider's own live offer (SHIP-85).
//
// Docs/01 §4.2 gives the provider "place, update, and withdraw a bid until it is accepted or expires",
// and this is the middle verb. A revised bid is the *same* offer at a different price, timing or set
// of conditions — the row keeps its identifier, its status and the key it was placed under.
//
// r must be a transaction, and unlike [Service.PlaceBid] that is checked rather than documented. The
// difference is the mechanism each rests on: a placement's correctness is `ON CONFLICT`'s and holds
// statement by statement, while this reads a status, decides against it and writes — which is only one
// decision if [postgresStore.lockBid]'s `FOR UPDATE` is still held when the write lands.
//
// # The order of the four refusals, and why eligibility comes last
//
//  1. **A revision that changes nothing** is [ErrNothingToRevise], before the database is touched at
//     all. Almost always a client defect, and a `200` carrying the unchanged bid would hide it.
//  2. **A bid that is not this provider's** is refused before anything about it is read back —
//     ownership and the job it hangs under, both in [Service.ownBid]. On the wire the two are one
//     404, byte-identical to a bid that does not exist.
//  3. **A bid that is no longer the provider's to change** is [ErrBidAccepted] or [ErrBidClosed].
//     Before validation, deliberately: telling somebody their price is out of range on an offer that
//     was accepted an hour ago answers a question they did not ask.
//  4. **Eligibility**, which is `fleet`'s answer and not this domain's, exactly as it is for a
//     placement.
//
// # Why a revision is checked for eligibility and a withdrawal is not
//
// This is the one asymmetry in the pair and it is deliberate. **A revision produces a live offer at a
// new number, and the customer may accept it the moment it lands** — so the platform must not accept
// one on a job it would refuse a first offer on. Docs/07 §3 puts that decision server-side in exactly
// one place, and SHIP-81's filter is that place; a provider whose only vehicle left service must not
// be able to re-price work they can no longer do, and a job that has been cancelled, awarded or
// expired must not acquire a fresh price. The endpoint that offers a provider a job and the endpoint
// that lets them change their bid on it agreeing is the same property SHIP-84 was built around.
//
// **A withdrawal is the opposite act and takes no such check** — see [Service.WithdrawBid].
//
// The refusal is [ErrJobNotOffered], which is the 404 a placement gets, byte-identically. It reads
// oddly to a caller who plainly knows the job exists, and it is still the right answer: what the
// platform must not do is tell one provider that a job was awarded, cancelled or expired, because that
// is what became of work somebody else was given.
func (s *Service) ReviseBid(
	ctx context.Context,
	r db.Runner,
	providerID, jobID, bidID uuid.UUID,
	rev Revision,
) (Bid, error) {
	if _, inTx := r.(pgx.Tx); !inTx {
		return Bid{}, fmt.Errorf("bidding: revising %s: %w", bidID, ErrNotInTransaction)
	}
	if rev.IsEmpty() {
		return Bid{}, fmt.Errorf("bidding: revising %s: %w", bidID, ErrNothingToRevise)
	}

	bid, err := s.ownBid(ctx, r, providerID, jobID, bidID)
	if err != nil {
		return Bid{}, err
	}
	if err := changeable(bid); err != nil {
		return Bid{}, err
	}

	offer := rev.applyTo(bid)
	if err := offer.validate(s.clock.Now()); err != nil {
		return Bid{}, err
	}

	permitted, err := s.eligibility.EligibleFor(ctx, r, providerID, bid.JobID)
	if err != nil {
		return Bid{}, fmt.Errorf("bidding: deciding whether %s may still bid on %s: %w",
			providerID, bid.JobID, err)
	}
	if !permitted {
		return Bid{}, ErrJobNotOffered
	}

	return s.store.reviseOffer(ctx, r, bid.ID, offer)
}

// WithdrawBid takes a provider's own offer back before it is accepted (SHIP-86).
//
// The last of Docs/01 §4.2's three verbs. The offer becomes [StatusWithdrawn] and the row survives:
// Docs/01 §4.3 requires the platform to record every withdrawal, and Docs/02 §4 keeps the history
// readable to the customer, the bidding provider and administrators. There is no delete on this table.
//
// r must be a transaction, for the reason [Service.ReviseBid] gives.
//
// # Withdrawing an offer that is already Withdrawn succeeds
//
// It answers with the bid and writes nothing further. **This is what makes a retry of a withdrawal
// safe when it does not reuse its key**, and a withdrawal is exactly the request a phone retries after
// a dropped connection, a restart, and a freshly generated key. The idempotency middleware absorbs the
// retry that reuses its key; this absorbs the one that does not — the same reading `jobs` gives a
// repeated cancellation and `fleet` gives a repeated deactivation, and the reason those two say
// "the caller asked for an outcome that holds".
//
// It is also why there is no key column for this endpoint. A withdrawal is idempotent by its *state*
// rather than by its key, which is a stronger guarantee than a stored key gives: two different clients
// with two different keys still cannot withdraw one offer twice.
//
// # No eligibility check, and that is the asymmetry with a revision
//
// **A provider must always be able to take back their own offer.** Refusing a withdrawal because the
// provider's only vehicle left service, or their verification lapsed, would strand a live offer the
// customer can still accept and the provider can no longer retract — which is the worst of both
// answers. SHIP-81's filter governs what a provider may *offer*; it has no business governing what
// they may stop offering.
//
// # It does not move the job
//
// Docs/02 §2 has `Negotiating → Open` on "all active bids expire, are withdrawn, or are rejected", and
// nothing reaches Negotiating until **SHIP-90**, which owns that presentation status in both
// directions. Job status is never a settable field in any case (Docs/02 §2, CLAUDE.md): a move passes
// one guarded function and leaves a `job_status_history` row in the same transaction, and inventing
// half of that here would be inventing the half without the record.
func (s *Service) WithdrawBid(
	ctx context.Context,
	r db.Runner,
	providerID, jobID, bidID uuid.UUID,
) (Bid, error) {
	if _, inTx := r.(pgx.Tx); !inTx {
		return Bid{}, fmt.Errorf("bidding: withdrawing %s: %w", bidID, ErrNotInTransaction)
	}

	bid, err := s.ownBid(ctx, r, providerID, jobID, bidID)
	if err != nil {
		return Bid{}, err
	}
	if bid.Status == StatusWithdrawn {
		return bid, nil
	}
	if err := changeable(bid); err != nil {
		return Bid{}, err
	}

	return s.store.withdrawBid(ctx, r, bid.ID)
}

// CounterOffer answers the other party's offer with different terms (SHIP-87, SHIP-88).
//
// Docs/02 §4: "A customer counter-offer supersedes the prior provider offer. A provider counter-offer
// supersedes the prior customer offer." **One method for both directions**, because those two
// sentences describe one act — which is also why there is one endpoint rather than two, and why the
// party is derived from the caller rather than declared.
//
// `created` is false when this key had already made this counter, which is a retry rather than a
// second link in the chain.
//
// r must be a transaction, and this is the method with the most riding on that: it reads a status,
// decides against it, and then writes three statements that are one act. Outside a transaction the
// first would land and the rest might not, leaving a superseded offer with no successor — a
// negotiation with nothing live in it and no way to tell that from a bug.
//
// # The chain, in three statements, and why that order
//
//  1. **The head leaves `uq_bids_one_submitted_per_provider_per_job`'s predicate**
//     ([postgresStore.supersedeHead]), which is what frees it for the row about to be written. It is
//     a compare-and-set, so a counter that waited on somebody else's matches nothing.
//  2. **The counter is inserted at 'Submitted'**, carrying the key it was made under, so that a retry
//     outliving the middleware's cache is answered from the record rather than adding a second link.
//     The same division SHIP-84 drew: Redis makes the retry cheap, the column makes it correct.
//  3. **The displaced row is linked to its successor** ([postgresStore.linkSuccessor]), which is what
//     makes the history readable in SHIP-88's sense and what
//     `ck_bids_superseded_is_not_live` then holds SHIP-92 to.
//
// `superseded_by` cannot be written in step 1 because the row it names does not exist yet, and this
// migration takes no deferrable foreign key — so every constraint holds at every instant rather than
// only at commit. 000502 records that trade.
//
// # The refusals, in the order they are made
//
//  1. **A counter that names no field** is [ErrNothingToCounter], before the database is touched. A
//     counter that changes nothing is not a counter; it is agreement, and agreement is the award.
//  2. **A bid the caller cannot reach at all** is one 404, byte-identical to a bid that does not
//     exist: not on the job in the path, or on a negotiation the caller is neither side of.
//  3. **The retry, answered before the world is consulted** — and *before* the status check, which is
//     the ordering that matters. A retry of a counter arrives after that counter has already
//     superseded the offer it answered, so a status check in front of it would refuse the caller's
//     own successful request with "that offer is no longer live". [Service.PlaceBid] puts its retry
//     first for the same reason and states the principle: a retry asks what happened to a request,
//     and that answer does not change when the world does.
//  4. **An offer that is over** is [ErrBidAccepted] or [ErrBidClosed].
//  5. **An offer the caller made themselves** is [ErrWrongParty]. You counter the other party and
//     revise your own; a provider answering their own live offer wants `PATCH`.
//  6. **The merged offer**, through the same validator a placement meets.
//  7. **Eligibility or awardability**, whichever side is countering. See below.
//
// # The two sides are checked against different questions, and that is not an oversight
//
// A **provider's** counter is a live offer the customer may accept the moment it lands, so it goes
// through SHIP-81's filter exactly as a placement and a revision do — a provider whose only vehicle
// left service must not be able to re-price work they can no longer do. The refusal is
// [ErrJobNotOffered], the same 404 a placement gets.
//
// A **customer's** counter is not something the platform could accept on the provider's behalf —
// `ck_bids_only_a_providers_offer_is_accepted` makes an award of it impossible — so eligibility is
// the wrong question and would be asked of the wrong account anyway. What matters is whether the
// negotiation can still end anywhere: [Negotiation.AwardableBy]. The refusal is [ErrNegotiationOver],
// which is [CodeBidClosed] on the wire, because the customer's next screen is the same one — this
// negotiation is finished.
//
// # It does not move the job
//
// Docs/02 §2 has `Open → Negotiating` on "first bid or **counter-offer** submitted", which reads
// like an instruction to this ticket more than it did to SHIP-84. It is still **SHIP-90's**, which
// depends on this ticket and on SHIP-57 and owns that presentation status in both directions. Job
// status is never a settable field in any case: a move passes one guarded function and leaves a
// `job_status_history` row in the same transaction, and doing it here would be doing the half
// without the record.
func (s *Service) CounterOffer(
	ctx context.Context,
	r db.Runner,
	callerID, jobID, bidID uuid.UUID,
	c Counter,
) (Bid, bool, error) {
	if _, inTx := r.(pgx.Tx); !inTx {
		return Bid{}, false, fmt.Errorf("bidding: countering %s: %w", bidID, ErrNotInTransaction)
	}
	if c.IsEmpty() {
		return Bid{}, false, fmt.Errorf("bidding: countering %s: %w", bidID, ErrNothingToCounter)
	}
	if c.Key == "" {
		return Bid{}, false, fmt.Errorf("bidding: countering %s: %w", bidID, ErrNoIdempotencyKey)
	}

	// The lock is taken before anything is decided, and it is what serialises two counters against
	// one offer. The second waits here, then re-reads at a fresh snapshot: either it is the retry
	// answered below, or it meets a head that is already 'Superseded' and is refused legibly.
	answered, err := s.reachableBid(ctx, r, callerID, jobID, bidID, true)
	if err != nil {
		return Bid{}, false, err
	}

	// The counter is attributed to the caller, and [counterable] below is what makes that safe: the
	// offer being answered has to have been made by the *other* side, so this can never write a row
	// on somebody else's behalf.
	by := answered.party

	if existing, found, err := s.store.bidPlacedUnder(
		ctx, r, answered.bid.JobID, answered.bid.ProviderID, by, c.Key); err != nil {
		return Bid{}, false, err
	} else if found {
		return existing, false, nil
	}

	if err := counterable(answered.bid, answered.party); err != nil {
		return Bid{}, false, err
	}

	offer := c.applyTo(answered.bid)
	if err := offer.validate(s.clock.Now()); err != nil {
		return Bid{}, false, err
	}

	if err := s.mayCounter(ctx, r, callerID, answered); err != nil {
		return Bid{}, false, err
	}

	id, err := uuid.NewV7()
	if err != nil {
		return Bid{}, false, fmt.Errorf("bidding: generating a counter-offer id: %w", err)
	}

	moved, err := s.store.supersedeHead(ctx, r, answered.bid.ID)
	if err != nil {
		return Bid{}, false, err
	}
	if !moved {
		// Unreachable: the row is held FOR UPDATE and was read as 'Submitted' with no successor two
		// statements ago. Reported rather than papered over, because continuing would insert a second
		// live offer into a negotiation that already has one — and the index would then refuse it
		// with a message about a live offer that says nothing about what happened.
		return Bid{}, false, fmt.Errorf(
			"bidding: %s was live when it was locked and is not now", answered.bid.ID)
	}

	counter, created, err := s.store.insertBid(ctx, r, Bid{
		ID:          id,
		JobID:       answered.bid.JobID,
		ProviderID:  answered.bid.ProviderID,
		OfferedBy:   by,
		Status:      StatusSubmitted,
		AmountCents: offer.AmountCents,
		PickupAt:    offer.PickupAt,
		DeliverBy:   offer.DeliverBy,
		Message:     offer.Message,
		Key:         c.Key,
	})
	if err != nil {
		return Bid{}, false, err
	}
	if !created {
		// Unreachable for the reason above: a concurrent request under this key would have waited on
		// the head's lock and been answered as a retry. Returning an error rather than reading the
		// row back — which is what [Service.PlaceBid] does — because the head has already been
		// superseded by the statement above, and answering from some other row would commit a
		// negotiation with a displaced offer and no successor. The error rolls the whole act back.
		return Bid{}, false, fmt.Errorf(
			"bidding: %s conflicted while %s was held locked", c.Key, answered.bid.ID)
	}

	if _, err := s.store.linkSuccessor(ctx, r, answered.bid.ID, counter.ID); err != nil {
		return Bid{}, false, err
	}
	return counter, true, nil
}

// Chain is every offer in one negotiation, oldest first (SHIP-88).
//
// SHIP-88's *Done when* is "only the latest valid offer is acceptable; **full chain remains
// readable**", and this is the second half. Docs/02 §4: "Bid history remains visible to the customer,
// bidding provider, and administrators."
//
// `truncated` reports that the negotiation is longer than [maxChainLength] rather than offering a
// page. See that constant.
//
// # Who may read it, and the two rules that are not the same rule
//
// **Both parties to the negotiation, and nobody else.** The provider it is with, and the customer who
// owns the job — the two Docs/02 §4 names, less the administrator, whose view is SHIP-96's along with
// the rest of the visibility rules. A *competing* provider is refused with the 404 a bid that does not
// exist gets, which is Docs/01 §4.3's second line: another provider's price, timing and counters are
// private, and this endpoint is the one place a competitor could otherwise read a whole negotiation
// at once.
//
// **The customer's budget is not in this shape**, because no shape in this package carries anything
// of the job. What *is* here and is new is an amount the customer chose — their counter — reaching a
// provider. That is not the budget and Docs/11 §3 states the distinction rather than leaving it to be
// inferred: Docs/01 §4.3 forbids the platform disclosing the customer's private maximum, and a
// counter-offer is an offer the customer deliberately made to this provider.
//
// # It keeps working after the job ends, deliberately
//
// [Negotiation.CustomerOf] is asked and [Negotiation.AwardableBy] is not. A record is at its most
// useful once the job is over — the customer reconstructing why they awarded elsewhere, the provider
// checking what they committed to, an administrator handling a dispute — and a read gated on the job
// still being live would go dark exactly then.
//
// No transaction and no lock. This is a read, and a chain that changed under it would produce an
// older row beside a newer one rather than an inconsistent one.
func (s *Service) Chain(
	ctx context.Context,
	r db.Runner,
	callerID, jobID, bidID uuid.UUID,
) ([]Bid, bool, error) {
	answered, err := s.reachableBid(ctx, r, callerID, jobID, bidID, false)
	if err != nil {
		return nil, false, err
	}

	// One more than the cap, so that "there are more" is read off the query rather than off a second
	// count that could disagree with it.
	offers, err := s.store.chain(ctx, r, answered.bid.JobID, answered.bid.ProviderID, maxChainLength+1)
	if err != nil {
		return nil, false, err
	}
	if len(offers) > maxChainLength {
		return offers[:maxChainLength], true, nil
	}
	return offers, false, nil
}

// reached is a bid the caller may act on, and which side of the negotiation they are.
type reached struct {
	bid Bid

	// party is the caller's own side. The bid may have been made by either.
	party Party
}

// reachableBid finds the bid a caller named and works out which side of the negotiation they are on.
//
// **This is the widening SHIP-87 makes, and it is deliberately a separate path from [Service.ownBid]
// rather than a loosening of it.** Every endpoint before this one is the provider's alone, and
// `ownBid` refuses a customer by design — TestOnlyTheBidsOwnerCanReviseIt asserts exactly that,
// including for the job's own customer. Widening `ownBid` would have widened `PATCH` and `withdraw`
// with it, which is the shape where a rule is relaxed for one endpoint and quietly relaxed for three.
//
// The two comparisons `ownBid` makes are made here too and for the same reasons: the row is read by
// its identifier and judged in Go, so "somebody else's" and "nobody's" stay different facts behind
// one answer on the wire, and the job in the path is compared because a bid is addressed under its
// own job.
//
// # The provider is recognised without asking anybody, and the customer is not
//
// `bids.provider_id` is on the row, so the provider side is a comparison. The customer side is
// `jobs`' fact and is asked through [Negotiation.CustomerOf] — this domain does not decide who owns a
// job any more than it decides who may bid.
//
// The order matters in one direction only: a provider is checked first because it costs nothing, and
// the two cannot both be true — `users.role` is immutable (000005) and SHIP-81's filter excludes a
// job's own customer from bidding on it.
//
// `lock` is true for a writer and false for a reader. A writer needs the row held for the rest of the
// transaction, because it reads a status and decides against it; a reader taking a lock would be
// taking one it does not need and, on the pool, would take and release it inside the statement
// anyway.
func (s *Service) reachableBid(
	ctx context.Context,
	r db.Runner,
	callerID, jobID, bidID uuid.UUID,
	lock bool,
) (reached, error) {
	if bidID == uuid.Nil || jobID == uuid.Nil || callerID == uuid.Nil {
		return reached{}, fmt.Errorf("bidding: %s on %s: %w", bidID, jobID, ErrBidNotFound)
	}

	read := s.store.readBid
	if lock {
		read = s.store.lockBid
	}

	bid, err := read(ctx, r, bidID)
	if err != nil {
		return reached{}, err
	}
	if bid.JobID != jobID {
		return reached{}, fmt.Errorf("bidding: %s is not on %s: %w", bidID, jobID, ErrBidNotFound)
	}

	if bid.ProviderID == callerID {
		return reached{bid: bid, party: PartyProvider}, nil
	}

	owns, err := s.negotiation.CustomerOf(ctx, r, callerID, bid.JobID)
	if err != nil {
		return reached{}, fmt.Errorf("bidding: deciding whether %s owns %s: %w", callerID, bid.JobID, err)
	}
	if !owns {
		return reached{}, fmt.Errorf(
			"bidding: %s is neither party to %s: %w", callerID, bidID, ErrNotBidOwner)
	}
	return reached{bid: bid, party: PartyCustomer}, nil
}

// mayCounter is the last refusal, and it asks each side a different question.
//
// See [Service.CounterOffer] for why: a provider's counter is a live offer somebody may accept, so it
// meets SHIP-81's filter; a customer's cannot be accepted at all, so what matters is whether the
// negotiation can still end in an award.
func (s *Service) mayCounter(ctx context.Context, r db.Runner, callerID uuid.UUID, a reached) error {
	if a.party == PartyProvider {
		permitted, err := s.eligibility.EligibleFor(ctx, r, callerID, a.bid.JobID)
		if err != nil {
			return fmt.Errorf("bidding: deciding whether %s may still bid on %s: %w",
				callerID, a.bid.JobID, err)
		}
		if !permitted {
			return ErrJobNotOffered
		}
		return nil
	}

	open, err := s.negotiation.AwardableBy(ctx, r, callerID, a.bid.JobID)
	if err != nil {
		return fmt.Errorf("bidding: deciding whether %s can still award %s: %w",
			callerID, a.bid.JobID, err)
	}
	if !open {
		return fmt.Errorf("bidding: %s can no longer be awarded: %w", a.bid.JobID, ErrNegotiationOver)
	}
	return nil
}

// counterable refuses an offer that cannot be answered with a counter (SHIP-87).
//
// The status half is [changeable]'s, and for the same reason: Docs/02 §4 lets only the latest valid
// offer be acted on, and an offer that has been accepted, withdrawn, rejected, expired or superseded
// is not it.
//
// **The party half is this method's own, and it is the rule that makes one endpoint serve both
// sides.** You counter what the *other* party offered; your own offer you revise. A provider
// answering their own live bid wants `PATCH /v1/jobs/{id}/bids/{bid_id}`, and telling them so is
// worth a code of its own — the client's next action is a different request rather than a different
// screen.
//
// Status is judged before authorship, deliberately. An offer that is over is over whoever made it,
// and "revise it instead" would be advice about a request that would also fail.
func counterable(b Bid, by Party) error {
	switch {
	case b.Status == StatusAccepted:
		return fmt.Errorf("bidding: %s: %w", b.ID, ErrBidAccepted)
	case !b.Status.live():
		return fmt.Errorf("bidding: %s is %s: %w", b.ID, b.Status, ErrBidClosed)
	case b.OfferedBy == by:
		return fmt.Errorf("bidding: %s was offered by %s: %w", b.ID, b.OfferedBy, ErrWrongParty)
	}
	return nil
}

// ownBid reads the bid a caller has named and refuses anything that is not theirs.
//
// One function rather than the same three checks in two methods, because the alternative is two places
// for the ownership comparison to be forgotten — and a forgotten one here is another provider's price,
// which Docs/01 §4.3 makes private from competitors. The same arrangement fleet.Service.owned takes.
//
// **Both comparisons are made in Go against a row read by its identifier**, rather than being folded
// into the `WHERE` clause. A query scoped to the caller that returns nothing cannot say whether the
// bid was somebody else's or nobody's, and the two are different facts even though they are one answer
// on the wire. TestOneProvidersKeyCannotReachAnothersBid proved the same rule for the placement's
// lookup by deleting `provider_id` from its `WHERE`; the equivalent mutation here is deleting one of
// the two comparisons below, and there is a test for each.
//
// The job identifier is compared as well as the provider, because the bid is addressed under a job:
// a client pairing a real bid with the wrong job is naming something that does not exist, and
// answering it from the bid alone would make the path's first half decorative.
func (s *Service) ownBid(ctx context.Context, r db.Runner, providerID, jobID, bidID uuid.UUID) (Bid, error) {
	if bidID == uuid.Nil || jobID == uuid.Nil {
		return Bid{}, fmt.Errorf("bidding: %s on %s: %w", bidID, jobID, ErrBidNotFound)
	}

	bid, err := s.store.lockBid(ctx, r, bidID)
	if err != nil {
		return Bid{}, err
	}
	if bid.ProviderID != providerID {
		return Bid{}, fmt.Errorf("bidding: %s does not belong to %s: %w", bidID, providerID, ErrNotBidOwner)
	}
	if bid.JobID != jobID {
		return Bid{}, fmt.Errorf("bidding: %s is not on %s: %w", bidID, jobID, ErrBidNotFound)
	}
	return bid, nil
}

// changeable refuses an offer that has left the provider's hands (SHIP-85, SHIP-86).
//
// Docs/01 §4.2 bounds both verbs identically — "until it is accepted or expires" — so both ask this
// one question, and [Status.live] is where the answer lives.
//
// **Accepted is answered separately from everything else**, because the two lead a client somewhere
// different: an accepted offer is a job this provider has won and the app should show it, while a
// rejected, expired or superseded one is over and the app should show the feed. It is also the case
// with a rule behind it rather than a state — CLAUDE.md's one accepted bid per job, held by
// uq_bids_one_accepted_per_job — and Docs/02 §6.2 makes stepping away from an awarded job a provider
// cancellation with consequences rather than a withdrawal, which is a different endpoint nobody has
// built.
// **The authorship check is SHIP-87's addition and it closes a hole that ticket opened.** Before
// counters, every row in `bids` was written by the provider named in `provider_id`, so
// [Service.ownBid]'s comparison was the whole of "this is your offer". A customer's counter-offer
// carries the *same* provider id — 000502 explains why that is the cheaper reinterpretation — so
// without this line a provider could `PATCH` the customer's counter, or withdraw it, and the customer
// would find their own number rewritten by the party it was addressed to.
//
// It is [ErrWrongParty] rather than the 404 a stranger gets, because the caller is a party to this
// negotiation and can read the row in its history. Answering "no such bid" about something the
// platform will show them a moment later is the kind of inconsistency that costs a client author an
// afternoon.
func changeable(b Bid) error {
	switch {
	case b.Status == StatusAccepted:
		return fmt.Errorf("bidding: %s: %w", b.ID, ErrBidAccepted)
	case !b.Status.live():
		return fmt.Errorf("bidding: %s is %s: %w", b.ID, b.Status, ErrBidClosed)
	case b.OfferedBy != PartyProvider:
		return fmt.Errorf("bidding: %s was offered by %s: %w", b.ID, b.OfferedBy, ErrWrongParty)
	}
	return nil
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
