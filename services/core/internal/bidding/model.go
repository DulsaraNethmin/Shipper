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

// live reports whether an offer is still the provider's to change (SHIP-85, SHIP-86).
//
// **One status, named rather than guessed at.** That is migration 000501's reading of
// uq_bids_one_submitted_per_provider_per_job, taken here for the same reason: 'Submitted' is the only
// status this platform writes and the only one that today means "a live offer from this provider that
// the other party has not answered". A predicate written against the *word* "active" would be a guess
// at a design SHIP-87 has not made.
//
// **SHIP-86's withdrawal will gate on this too, because Docs/01 §4.2 bounds the two identically** —
// "place, update, and withdraw a bid until it is accepted or expires". Two predicates that have to
// agree are one predicate; the two endpoints differ in what they do about a status that fails it, not
// in which statuses fail.
//
// It is deliberately not widened to 'Countered'. A provider answering a customer's counter is making a
// counter-offer, which supersedes the prior offer (Docs/02 §4) and is SHIP-87's ticket with SHIP-88's
// chain behind it; treating that as an in-place revision would settle their design by accident. If
// SHIP-87 finds the set should be wider, adding a status here is one line — the same additive
// direction 000501 deliberately left its index in.
func (s Status) live() bool { return s == StatusSubmitted }

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

// Revision is the change a provider is making to an offer they have already placed (SHIP-85).
//
// **Pointers throughout, where [Offer]'s fields are values**, which is what http.go's note on
// bidRequest predicted: "an offer is placed whole … SHIP-85's revision is a different request with a
// different shape and will need the pointers this one does not." A revision names what changes and
// says nothing about the rest, because a provider dropping their price by fifty dollars should not
// have to restate two timestamps they are not touching — and a client that restates them is a client
// that can get them wrong.
//
// **There is no status field and no provider field**, for the two reasons [Offer] has neither: a bid's
// status is the platform's (Docs/02 §2's rule for jobs is the same rule here), and the provider is
// whoever the token says is calling.
//
// # There is no key either, and that is the whole difference from a placement
//
// [Offer.Key] is on that type because it is a *column*. A placement retried after the idempotency
// middleware has forgotten it is answered from the row it wrote, and without the stored key that
// request would meet uq_bids_one_submitted_per_provider_per_job and be told the provider already had a
// live offer — a 409 for a request that succeeded.
//
// A revision has no such failure mode. It is an `UPDATE` of one row that already exists, so applying
// it twice reaches the state applying it once reaches; there is no second row for a repeat to create
// and nothing for a constraint to refuse. That is the natural idempotency `PATCH /v1/jobs/{id}` and
// `PATCH /v1/fleet/vehicles/{id}` already rely on, and neither of those stores a key either.
//
// **What matters instead is that the placement's key survives a revision**, and that is the trap worth
// naming rather than discovering. Writing this request's key over `bids.idempotency_key` would leave a
// late retry of the *placement* unable to find its row, and it would then be refused as a second
// offer — reintroducing exactly the failure the column was added to prevent.
// [postgresStore.reviseOffer] does not name that column and
// TestARevisionDoesNotConsumeThePlacementsKey is what says so.
type Revision struct {
	AmountCents *int64
	PickupAt    *time.Time
	DeliverBy   *time.Time

	// Message is the conditions accompanying the offer. Present and blank clears it, which is the
	// same treatment a placement gives a blank message: `nullif` puts NULL in the column, so ""
	// and "not given" cannot part company.
	Message *string
}

// IsEmpty reports whether the caller named no field at all.
//
// A revision that changes nothing is refused rather than answered, exactly as fleet refuses an edit
// naming no field: it is almost always a client defect, and answering `200` with the unchanged bid
// would hide it behind a success.
func (r Revision) IsEmpty() bool {
	return r.AmountCents == nil && r.PickupAt == nil && r.DeliverBy == nil && r.Message == nil
}

// applyTo is the offer that would stand if this revision were accepted.
//
// **It produces an [Offer], so that the same validator runs over a revised offer as over a placed
// one.** There is deliberately no second set of rules: an offer that could not be placed today must
// not be reachable by revising one that could be placed yesterday, and a rule stated twice is a rule
// that will eventually be stated differently.
//
// The key is left empty. A revision writes no key, and [Offer.validate] does not read one — the
// requirement that a *placement* carry one is [Service.PlaceBid]'s, checked there because the column
// is what a later retry is matched against.
//
// # The whole offer is validated, not only the fields that changed
//
// Worth stating because it can surprise. A provider re-pricing a three-day-old bid whose collection
// time has since passed is refused, naming `pickup_at` — a field they did not send. That is the honest
// answer rather than an awkward one: what they are asking the platform to keep live is an offer to
// collect in the past, and the customer could accept it. Validating only the fields that arrived would
// make the stored offer's coherence depend on the order somebody edited it in.
func (r Revision) applyTo(b Bid) Offer {
	o := Offer{
		AmountCents: b.AmountCents,
		PickupAt:    b.PickupAt,
		DeliverBy:   b.DeliverBy,
		Message:     b.Message,
	}

	if r.AmountCents != nil {
		o.AmountCents = *r.AmountCents
	}
	if r.PickupAt != nil {
		o.PickupAt = *r.PickupAt
	}
	if r.DeliverBy != nil {
		o.DeliverBy = *r.DeliverBy
	}
	if r.Message != nil {
		o.Message = *r.Message
	}
	return o.normalise()
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
