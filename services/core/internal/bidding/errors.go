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
)

// The error codes this domain's endpoints answer with (Docs/10 §4.4).
//
// Registered rather than declared as constants, so that the generated Docs/10-api-error-codes.md
// describes them and cmd/api's uniqueness test can see them. Named <domain>_<condition>, which is
// what stops two domains meaning different things by one string.
//
// **There is deliberately one.** Everything else this endpoint can answer is already covered by the
// protocol codes: an implausible amount is `validation_failed` with details naming the field, a job
// this provider may not bid on is `not_found`, and a missing or reused key is the middleware's
// business. A domain code earns its place only where a client would otherwise have to parse a
// message to know what to do, and this is the one case where it would: the app's next screen after
// "you already have a bid on this job" is that bid, not the form the provider was filling in.
var CodeAlreadyBid = httpx.RegisterCode("bidding_already_bid",
	"You already have a live offer on this job. Revise or withdraw it rather than placing a second.")
