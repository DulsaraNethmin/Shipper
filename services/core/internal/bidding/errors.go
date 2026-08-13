package bidding

import (
	"errors"

	"github.com/DulsaraNethmin/Shipper/services/core/internal/httpx"
)

// The sentinel errors this domain raises, and the error codes its endpoints answer with.
//
// Docs/10 §2.1 puts the sentinels here rather than beside the code that returns them, so a caller
// deciding what to do about a failure has one file to read.
//
// The codes below are what reaches a client, and they are a separate list on purpose: a sentinel
// says what happened, a code says what the client should do about it.

var (
	// ErrJobNotOffered means the eligibility filter does not offer that job to that provider.
	//
	// **One sentinel covering three cases**: no such job, a job this provider may not bid on, and a
	// job that has stopped accepting bids. That is the same reading fleet.ErrJobNotOffered takes and
	// it is taken here for the same reason — a job is not this domain's row, so separating the cases
	// would take a second query against `jobs` whose only product is the knowledge that some
	// identifier exists. There is nothing this domain could truthfully say about a job it may not
	// read, so it says the one thing that is true of all three.
	ErrJobNotOffered = errors.New("bidding: that job is not offered to this provider")

	// ErrAlreadyBid means this provider already has a live offer standing on this job.
	//
	// It is the domain's reading of uq_bids_one_submitted_per_provider_per_job, which is partial on
	// `status = 'Submitted'` — so a bid that was withdrawn, superseded or rejected does not raise
	// it, and only a second *live* offer is refused. The index is what makes the rule true; this
	// sentinel is what makes the refusal legible, because a caller handed a constraint name learns
	// nothing they can act on.
	//
	// **It is deliberately not what a retry produces.** A retry carries the key that placed the
	// original offer and is answered from the row it wrote — see [Service.PlaceBid]. This is the
	// answer to a *second* offer, which is a different request that the provider should make through
	// SHIP-85's revision endpoint instead.
	ErrAlreadyBid = errors.New("bidding: that provider already has a live offer on this job")

	// ErrNoIdempotencyKey means an offer arrived with no key to record it against.
	//
	// SHIP-15's middleware refuses a state-changing request without one, so this is unreachable
	// through the served router. It is checked all the same, for the reason delivery's twin is: the
	// key is not merely how a retry is absorbed here, it is a column on the row, and a bid stored
	// with no key is a bid a later retry cannot be matched to. A service reached from a test, a
	// worker or a future admin path must not be able to write one.
	ErrNoIdempotencyKey = errors.New("bidding: an offer must carry the key it was placed under")

	// ErrBidNotFound means there is no such bid under that job (SHIP-85, SHIP-86).
	//
	// **Two cases deliberately, and the third is kept out.** No bid with that identifier, and a bid
	// that exists but is against a different job — because a bid is addressed under the job it was
	// placed on, and a client pairing the wrong two identifiers is asking for something that does not
	// exist. A bid belonging to *another provider* is [ErrNotBidOwner] and stays separate, for the
	// reason fleet keeps ErrNotVehicleOwner apart from ErrVehicleNotFound: both are one 404 on the
	// wire, and a test that could not tell them apart could not tell "the stranger was refused" from
	// "the row silently stopped existing".
	ErrBidNotFound = errors.New("bidding: no such bid on that job")

	// ErrNotBidOwner means that bid belongs to a different provider (SHIP-85, SHIP-86).
	//
	// **The refusal is explicit rather than a scope that returns nothing.** The read takes the row by
	// its identifier and then compares `provider_id`, which is the arrangement fleet.Service.owned
	// takes and for the same reason: a `WHERE … AND provider_id = $2` returning no rows cannot say
	// whether the bid was somebody else's or nobody's, and only one of those is worth noticing. On the
	// wire they are one answer — see [apiError].
	ErrNotBidOwner = errors.New("bidding: that bid belongs to another provider")

	// ErrBidAccepted means the offer has been awarded and is no longer the provider's to change.
	//
	// Kept apart from [ErrBidClosed] because the client's next screen is different and so is the news:
	// an accepted offer is a job this provider has won, and Docs/02 §6.2 makes stepping away from it a
	// provider cancellation with consequences rather than a withdrawal. Docs/01 §4.2 draws the same
	// line — "until it is accepted or expires" — and CLAUDE.md's one-accepted-bid invariant is what
	// stands behind it: uq_bids_one_accepted_per_job means an award is a commitment two parties are
	// holding, not a state one of them may leave unilaterally.
	ErrBidAccepted = errors.New("bidding: that offer has been accepted")

	// ErrBidClosed means the offer is no longer live, so there is nothing to revise or withdraw.
	//
	// Rejected, Expired or Superseded — and, for a revision, Withdrawn. A *withdrawal* of a withdrawn
	// offer is not this: the caller asked for an outcome that already holds, and
	// [Service.WithdrawBid] absorbs it.
	ErrBidClosed = errors.New("bidding: that offer is no longer live")

	// ErrNothingToRevise means a revision named no field at all.
	ErrNothingToRevise = errors.New("bidding: the revision changes nothing")

	// ErrNothingToCounter means a counter-offer named no field at all (SHIP-87).
	//
	// Kept apart from [ErrNothingToRevise] even though both are `bad_request` on the wire, because the
	// two are not the same mistake and the message a client shows is not the same message. A revision
	// naming nothing is a client defect. A *counter* naming nothing is agreement — the caller is
	// accepting the terms in front of them — and the act for that is the award, which is the
	// customer's and is SHIP-92's. Answering both with "send at least one field to revise" would point
	// a client at the wrong screen.
	ErrNothingToCounter = errors.New("bidding: the counter-offer changes nothing")

	// ErrWrongParty means the caller is on the wrong side of the offer they named (SHIP-87).
	//
	// **Two situations, one rule: you counter the other party's offer and revise your own.** Countering
	// an offer you made yourself is refused, and so is revising or withdrawing one the other party
	// made — which became possible only with counters, because a customer's counter carries the same
	// `provider_id` as the provider's offer it answers (000502).
	//
	// It is deliberately not the 404 a stranger gets. The caller is a party to this negotiation and can
	// read the row in its history a moment later; answering "no such bid" about something the platform
	// will then show them is an inconsistency a client author has to discover rather than read.
	ErrWrongParty = errors.New("bidding: that offer belongs to the other party")

	// ErrNegotiationOver means the customer's job can no longer be awarded, so a counter-offer on it
	// leads nowhere (SHIP-87).
	//
	// **The customer's half of the check a provider meets as eligibility**, and a different question
	// rather than the same one asked of a different account: a customer countering is asking whether
	// the negotiation can still end in an award, which is Negotiation.AwardableBy.
	//
	// It is [CodeBidClosed] on the wire rather than a code of its own, because the client does the
	// same thing with it as with a superseded or rejected offer — this negotiation is finished, show
	// the job. Which of the reasons it was is the customer's own job screen to answer, not an error
	// code's. It is also a temporary shape: once SHIP-93 closes competing bids on award, an offer on an
	// awarded job is already `Rejected` and reaches [ErrBidClosed] directly.
	ErrNegotiationOver = errors.New("bidding: that job can no longer be awarded")

	// ErrNotInTransaction means a method that reads a row, decides against it and writes it was handed
	// a connection pool rather than a transaction.
	//
	// Checked by [Service.ReviseBid], [Service.WithdrawBid] and [Service.CounterOffer] and deliberately
	// not by [Service.PlaceBid], because they do not rest on the same mechanism. A placement's
	// correctness is `ON CONFLICT`'s, which holds statement by statement; a revision's and a
	// withdrawal's is [postgresStore.lockBid]'s `FOR UPDATE`, and outside a transaction that lock is
	// released the instant the SELECT returns — leaving the status this code decided against free to
	// change before the UPDATE lands, with nothing to report afterwards. The same sentinel fleet has,
	// for the same reason.
	ErrNotInTransaction = errors.New("bidding: this must run inside a transaction")
)

// The error codes this domain's endpoints answer with (Docs/10 §4.4).
//
// Registered rather than declared as constants, so that the generated Docs/10-api-error-codes.md
// describes them and cmd/api's uniqueness test can see them. Named <domain>_<condition>, which is
// what stops two domains meaning different things by one string.
//
// **There was deliberately one at SHIP-84, three at SHIP-86, and there are four now.** Everything else
// these endpoints can answer is already covered by the protocol codes: an implausible amount is
// `validation_failed` with details naming the field, a job this provider may not bid on is
// `not_found`, a bid that is not theirs is `not_found`, and a missing or reused key is the
// middleware's business. A domain code earns its place only where a client would otherwise have to
// parse a message to know what to do — and each of the four below leads somewhere different.
//
// **SHIP-87 added exactly one**, and the count is the evidence for the test rather than a target: a
// counter-offer introduces three new refusals and two of them reuse. A negotiation the customer can no
// longer award is [CodeBidClosed] — the same screen a superseded offer leads to — and a counter naming
// no field is `bad_request` like every other empty request body. Only "you are on the wrong side of
// this offer" sends the client somewhere it would not otherwise go, which is `PATCH` instead of
// `POST`, and that is what [CodeWrongParty] is for.
var CodeAlreadyBid = httpx.RegisterCode("bidding_already_bid",
	"You already have a live offer on this job. Revise or withdraw it rather than placing a second.")

// CodeBidAccepted is the answer to revising or withdrawing an offer the customer has awarded
// (SHIP-85, SHIP-86).
//
// The app's next screen is the job the provider has just won. That is a different screen from every
// other refusal on these two endpoints and it is good news rather than an error, which is precisely
// why it cannot be left to a client parsing a message.
var CodeBidAccepted = httpx.RegisterCode("bidding_bid_accepted",
	"That offer has been accepted. An accepted bid can be neither revised nor withdrawn.")

// CodeBidClosed is the answer to revising or withdrawing an offer that is no longer live (SHIP-85,
// SHIP-86).
//
// Rejected, Expired or Superseded — and Withdrawn, for a revision. One code rather than four, because
// the client does the same thing with all of them: the offer is over, and what to show is the feed
// rather than the form. Which of the four it was is the provider's own bid history to answer
// (SHIP-101), not an error code's.
var CodeBidClosed = httpx.RegisterCode("bidding_bid_closed",
	"That offer is no longer live, so it can be neither changed nor answered.")

// CodeWrongParty is the answer to acting on the wrong side of an offer (SHIP-87).
//
// **You counter the other party's offer and revise your own.** Both directions of getting that
// backwards land here: countering an offer you made yourself, and revising or withdrawing one the
// other party made.
//
// It earns a code rather than a message because the client's correct response is a **different
// request** — `PATCH /v1/jobs/{id}/bids/{bid_id}` where it sent a counter, or a counter where it sent
// a `PATCH` — rather than a different screen. That is Docs/10 §4.4's test, and it is the same test
// [CodeBidAccepted] passes and [ErrNegotiationOver] deliberately fails.
var CodeWrongParty = httpx.RegisterCode("bidding_wrong_party",
	"That offer belongs to the other party. Counter an offer they made; revise one you made yourself.")
