package bidding

import (
	"strings"
	"time"

	"github.com/google/uuid"
)

// Status is one of the eight bid states in Docs/02 §4.
//
// The values are the document's own strings, because Docs/10 §3.4 requires it and the reason is
// that three languages hold a copy of this list. Go, Dart and TypeScript can each be diffed
// against Docs/02 rather than against one another, and a value that has drifted is visible
// without holding two files side by side. SHIP-56a generates the other two; this is the Go copy
// until it does.
//
// These eight happen to be single words in sentence case, unlike the twelve job statuses, so
// storing them unaltered looks like it costs nothing. It is the same rule all the same — the
// document is the source, and the moment somebody lower-cases one of them here the pairing test
// against ck_bids_status is what says so.
//
// The wire form is deliberately not here. Docs/10 §4.7 puts enum values on the wire in lower
// snake case, and [jobs.Status.Wire] arrived only when SHIP-61 became the first endpoint that had
// to serialise one. SHIP-80 adds no endpoint, so adding the mapping now would be adding an
// untested transformation with no caller.
//
// SHIP-84 is that endpoint, and [Status.Wire] is below.
type Status string

const (
	// StatusDraft is an offer the provider is composing and nobody else can see.
	StatusDraft Status = "Draft"

	// StatusSubmitted is a live offer awaiting the other party.
	StatusSubmitted Status = "Submitted"

	// StatusCountered is an offer that has been answered with a different price or timing.
	// Docs/02 §4 lets either party counter, and each counter supersedes the prior offer.
	StatusCountered Status = "Countered"

	// StatusAccepted is the awarded offer. At most one bid per job may hold it, and that is a
	// database guarantee rather than an application one — see uq_bids_one_accepted_per_job in
	// migration 000500.
	StatusAccepted Status = "Accepted"

	// StatusRejected is an offer the customer declined, including every competing bid closed by
	// an award (SHIP-93).
	StatusRejected Status = "Rejected"

	// StatusWithdrawn is an offer the provider took back before it was accepted.
	StatusWithdrawn Status = "Withdrawn"

	// StatusExpired is an offer that ran out on its own terms (SHIP-89).
	StatusExpired Status = "Expired"

	// StatusSuperseded is an offer displaced by a counter from either party. The row remains,
	// because Docs/02 §4 keeps the whole chain readable to the customer, the bidding provider
	// and administrators.
	StatusSuperseded Status = "Superseded"
)

// Statuses is every status, in the order Docs/02 §4 lists them.
//
// Ordered rather than a set because that order is the document's, and because it is what
// TestEveryBidStatusConstraintMatchesTheGoConstants compares against ck_bids_status. Docs/10
// §3.4 requires that pairing for every enumeration: the constraint is read out of pg_constraint
// and held to this list, which is what stops the twelve job statuses and the eight bid statuses
// drifting when they are built on separate branches.
var Statuses = []Status{
	StatusDraft,
	StatusSubmitted,
	StatusCountered,
	StatusAccepted,
	StatusRejected,
	StatusWithdrawn,
	StatusExpired,
	StatusSuperseded,
}

// Valid reports whether s is one of the eight.
func (s Status) Valid() bool {
	for _, known := range Statuses {
		if s == known {
			return true
		}
	}
	return false
}

func (s Status) String() string { return string(s) }

// Wire is the status as it appears in a response body (SHIP-84).
//
// Docs/10 §4.7 puts enum values on the wire in lower snake case. All eight of these are single words
// already, so the transformation is only a case fold today — and it is written as the same
// transformation [jobs.Status.Wire] applies, including the space replacement, because a ninth status
// with a space in it must not have to remember to be handled here.
//
// **Derived rather than tabulated**, which is the part that matters. An eight-entry table beside the
// eight constants is a second list that can disagree with the first — exactly the drift Docs/10 §3.4
// pairs every enumeration with a test to prevent. A transformation cannot disagree with its input.
// TestTheWireFormsAreStableAndDistinct still writes all eight out, because these strings are
// published: a client already branching on `submitted` cannot have it renamed underneath it.
//
// There is no `StatusFromWire`. Nothing a client sends names a bid status: status is not a settable
// field, and there is no `?status=` filter on this domain's one endpoint. The inverse arrives with
// the ticket that needs it.
func (s Status) Wire() string {
	return strings.ReplaceAll(strings.ToLower(string(s)), " ", "_")
}

// Bid is one offer against one job.
//
// # There is no budget field here and there never may be
//
// The customer's maximum is private from providers in any form (Docs/01 §4.3, CLAUDE.md), and this
// is the shape a provider is handed back after bidding. [Amount] is the *provider's own* number and
// has nothing to do with it — 000500's column comment says so from the schema's side.
//
// The protection is not this paragraph. TestTheBidResponseCarriesNothingOfTheCustomers holds the
// serialised response to a closed set of keys at every depth, so a field arriving under any name —
// `max_price`, `ceiling`, `budget_cents` — fails whatever it is called. That is the axis SHIP-83
// found a source-parsing guard cannot have.
//
// # Nor is there anything about the job beyond its identifier
//
// A bid names the job it is against and carries no copy of it. That is a privacy decision as much as
// a modelling one: every field of the job copied here would be a second place Docs/01 §4.3 could be
// broken, and the provider already has `GET /v1/jobs/open/{id}` for the job itself — one shape,
// tested once (SHIP-83).
type Bid struct {
	ID    uuid.UUID
	JobID uuid.UUID

	// ProviderID is who made the offer. Present on the model and deliberately absent from the wire:
	// the only caller who can obtain a bid today is the provider who made it, so a field naming them
	// would be the client's own identifier read back to it.
	ProviderID uuid.UUID

	// Status is one of the eight in Docs/02 §4. Never settable by a client, on this endpoint or any
	// other.
	Status Status

	// AmountCents is what the provider is asking, in minor units.
	//
	// Cents in an int64 against a numeric(12,2) column, which is `jobs`' convention for the budget
	// (Docs/10 §3.3) copied deliberately rather than abstracted. `internal/money` is registered in
	// internal/boundaries and unwritten; writing it is a shared-file edit this ticket may not make.
	// **This is the second domain storing an amount independently, and Docs/11 §3 records it as the
	// trigger the eventual money ticket should be aimed at.**
	AmountCents int64

	// PickupAt and DeliverBy are the timing the offer commits to.
	//
	// Two instants rather than two windows, argued in migration 000501: the job states the
	// customer's flexibility, and the bid states a commitment against it. Both are always set on a
	// live offer — ck_bids_offer_has_timing refuses an offer past Draft that names neither.
	PickupAt  time.Time
	DeliverBy time.Time

	// Message is the conditions accompanying the offer, in the provider's own words. Docs/03's third
	// item after price and timing; empty when none was given.
	Message string

	// Key is the idempotency key the offer was placed under.
	//
	// Held on the model because it is the row's own identity for a retry, not merely how one request
	// was absorbed: a retry that outlives the middleware's cache is matched to this. It never reaches
	// the wire — a client that sent the key already has it.
	Key string

	CreatedAt time.Time
	UpdatedAt time.Time
}

// Offer is a bid a provider is placing, before the platform has accepted it.
//
// Separate from [Bid] for the reason jobs.DraftFields is separate from jobs.Job: this is what a
// caller supplies and that is what the platform stores. Nothing here is a pointer, because unlike a
// draft there is no partial edit to express — an offer is placed whole, and SHIP-85's revision is a
// different ticket with a different shape.
//
// **There is no status field and no provider field.** The status is the platform's (Docs/02 §2's
// rule for jobs is the same rule here) and the provider is whoever the token says is calling — a
// provider id in a request body would be an authorisation decision made from client input, which
// Docs/07 §3 puts on the platform.
type Offer struct {
	AmountCents int64
	PickupAt    time.Time
	DeliverBy   time.Time
	Message     string

	// Key is the caller's idempotency key, which becomes the row's. Required: see
	// [ErrNoIdempotencyKey].
	Key string
}

// normalise collapses whitespace and moves both instants to UTC.
//
// Both halves matter and for different reasons. The message is stored as typed except for runs of
// whitespace, which is the treatment fleet gives a vehicle model — a provider composing on a phone
// produces double spaces and trailing newlines, and two offers differing only in those are one
// offer. The instants are moved to UTC because a client sends an offset ("+10:00") and every
// comparison below, every column, and every rendering back out is in UTC; converting at the boundary
// is what stops a Melbourne winter offer being read an hour early in a Brisbane summer.
func (o Offer) normalise() Offer {
	o.Message = strings.Join(strings.Fields(o.Message), " ")
	if !o.PickupAt.IsZero() {
		o.PickupAt = o.PickupAt.UTC()
	}
	if !o.DeliverBy.IsZero() {
		o.DeliverBy = o.DeliverBy.UTC()
	}
	return o
}
