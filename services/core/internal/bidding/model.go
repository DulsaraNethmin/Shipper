package bidding

import (
	"strings"
	"time"

	"github.com/google/uuid"
)

// The eight bid statuses are generated (SHIP-56a).
//
// contracts/statuses.yaml is the source and status_gen.go beside this file is the Go form:
// Status, its constants, Statuses, Valid, String, Wire and StatusFromWire. Docs/10 §8.2 named
// that arrangement before there was a generator, and the argument for why nothing writes
// 'Countered' — which used to sit here and, in almost the same words, in the Dart client — is
// now in the specification and rendered into all three languages from it.
//
// **live below stayed hand-written**, and the line is the same one the job transition table is on:
// it is a decision rather than a vocabulary. Docs/07 §3 puts every such decision on the platform,
// so no client wants a copy and the reason for generating disappears. Party is likewise still here
// — it names a kind of person rather than a lifecycle state, which is not what SHIP-56a is scoped
// to.

// live reports whether an offer is still open for either party to act on (SHIP-85, SHIP-86,
// SHIP-87).
//
// **One status, named rather than guessed at.** That is migration 000501's reading of
// uq_bids_one_submitted_per_provider_per_job, taken here for the same reason: 'Submitted' is the only
// status this platform writes and the only one that means "a live offer that the other party has not
// answered". A predicate written against the *word* "active" would have been a guess at a design
// SHIP-87 had not made.
//
// **SHIP-87 has now made it, and the set did not widen.** 000501 offered "if SHIP-87 finds it needs a
// wider predicate — 'Submitted' or 'Countered', say — that is one extra value in an additive
// migration". It does not need one. A counter-offer is a *new row* at 'Submitted', and the offer it
// displaces is moved to 'Superseded' in the same transaction — so one status still covers "the live
// offer in this negotiation". 000502's ck_bids_superseded_is_not_live makes that a database fact
// rather than a convention: a displaced row cannot re-enter this predicate.
//
// **Three verbs gate on this now.** Docs/01 §4.2 bounds a revision and a withdrawal identically —
// "place, update, and withdraw a bid until it is accepted or expires" — and Docs/02 §4 bounds a
// counter the same way, since only the latest valid offer can be acted on. Predicates that have to
// agree are one predicate; the endpoints differ in what they do about a status that fails it, not in
// which statuses fail.
//
// 'Countered' is deliberately never written — see [StatusCountered].
func (s Status) live() bool { return s == StatusSubmitted }

// Party is who made an offer: the provider bidding, or the customer answering them (SHIP-87).
//
// **The values are `users.role`'s and `job_status_history.actor_type`'s**, lower case, rather than
// Docs/02's sentence-case bid statuses. They name a kind of person rather than a lifecycle state, and
// both existing columns in this database that name a kind of person spell them this way.
//
// It is a separate fact from `bids.provider_id`, which names the provider a negotiation is *with*
// rather than the author of any one row. 000502 reinterprets that column deliberately and says why
// the alternative is worse.
type Party string

const (
	// PartyProvider is the provider bidding on the job.
	PartyProvider Party = "provider"

	// PartyCustomer is the customer who owns the job, answering an offer they received.
	PartyCustomer Party = "customer"
)

// Parties is both, in the order a negotiation reaches them: a provider offers, a customer answers.
//
// Ordered rather than a set for the reason [Statuses] is, and paired with `ck_bids_offered_by` by
// TestEveryOfferingPartyMatchesTheConstraint — the discipline Docs/10 §3.4 requires of every
// enumeration held in two places.
var Parties = []Party{PartyProvider, PartyCustomer}

// Valid reports whether p is one of the two.
func (p Party) Valid() bool {
	for _, known := range Parties {
		if p == known {
			return true
		}
	}
	return false
}

func (p Party) String() string { return string(p) }

// Wire is the party as it appears in a response body.
//
// Both values are already lower snake case, so this is the identity — written as a method all the
// same, because Docs/10 §4.7's rule is about the wire form rather than about whether a particular
// pair of strings happens to satisfy it, and a third party added by some later ticket must not have
// to remember that the conversion was skipped here.
func (p Party) Wire() string { return string(p) }

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
// broken, and the provider already has `GET /v1/fleet/jobs/{id}` for the job itself — one shape,
// tested once (SHIP-83).
type Bid struct {
	ID    uuid.UUID
	JobID uuid.UUID

	// ProviderID is the provider this negotiation is with — **not necessarily the author of this
	// row**, since SHIP-87. A customer's counter-offer carries the same provider id and is
	// distinguished by [Bid.OfferedBy]; 000502 says why that reinterpretation is cheaper than the
	// alternative.
	//
	// Present on the model and deliberately absent from the wire. Both callers who can obtain a bid
	// already know which provider the negotiation is with: the provider is that provider, and the
	// customer reached it through their own job.
	ProviderID uuid.UUID

	// OfferedBy is which party made this offer (SHIP-87).
	//
	// Docs/02 §4 lets either party counter, so a chain alternates — provider, customer, provider —
	// and this is what says which link is which. It reaches the wire, unlike [Bid.ProviderID],
	// because a client rendering a negotiation has to show "you" against "them" and cannot derive
	// that from a chain it may have joined halfway through.
	OfferedBy Party

	// SupersededBy is the counter-offer that displaced this one, or [uuid.Nil] if this is the live
	// head of its chain (SHIP-88).
	//
	// **The link points backwards, from the displaced row to its successor**, and 000502 argues the
	// choice at length: with the link here, "this offer has been displaced" is a fact in the row
	// itself, which is what lets `ck_bids_superseded_is_not_live` express "only the latest valid
	// offer is acceptable" as an ordinary column constraint rather than as a rule SHIP-92 has to
	// remember.
	//
	// [uuid.Nil] rather than a pointer, because "no successor" is a total answer here rather than a
	// missing one: every row either has been displaced or is the head, and a nil pointer would add a
	// third state — unknown — that no read of this table can produce.
	SupersededBy uuid.UUID

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

	// VehicleID is the vehicle the offer is made with, or [uuid.Nil] when the provider named none
	// (SHIP-102a).
	//
	// **The seam 000500 and 000501 both deferred, arriving with the ticket they named as its
	// trigger.** Docs/01 §4.3 wants it for the customer's comparison — "price, timing, provider
	// profile, vehicle, and declared capability" — and 000504 argues one column against a join
	// table.
	//
	// [uuid.Nil] rather than a pointer, for [Bid.SupersededBy]'s reason: "no vehicle stated" is a
	// total answer rather than a missing one, and a nil pointer would add a third state no read of
	// this table can produce.
	//
	// **This is an identifier and nothing else, which is what keeps `fleet` out of this package.**
	// The make, model, type and capacity a customer compares are read by the *edge*, through
	// [Directory], and never stored here — a copy of a vehicle inside `bidding` would be a second
	// place for it to disagree with the fleet the provider actually runs.
	VehicleID uuid.UUID

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

	// VehicleID is the vehicle the offer is made with, and [uuid.Nil] means none was named
	// (SHIP-102a).
	//
	// **Optional, and that is a compatibility decision rather than a weak rule.** SHIP-84's request
	// schema has been served since wave 5 and every client written against it omits this field;
	// Docs/10 §4.2's rule is that a field added to a request is optional or it is a new endpoint.
	// A provider who names none makes an offer whose vehicle the customer's screen shows as
	// unstated, which is the truth about it.
	//
	// **Nothing in [Offer.validate] looks at it**, because everything worth checking about it is a
	// fact in another domain's table — that it is the caller's own, and in service. [Vehicles] is
	// the port that asks, and [Service.PlaceBid] asks inside the transaction that writes the row.
	VehicleID uuid.UUID

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

	// VehicleID is the vehicle the offer is made with (SHIP-102a). Present and [uuid.Nil] clears
	// it, which is the treatment [Revision.Message] gets and for the same reason: a provider who
	// named a truck and then decided not to commit to one has to be able to say so, and without an
	// explicit empty value "no longer stated" would be inexpressible.
	//
	// **A revision is the only way the vehicle on a negotiation ever changes.** A counter-offer
	// inherits it — [Counter] has no such field, so [Counter.terms] leaves this nil and
	// [Revision.applyTo] carries the superseded row's forward. That is deliberate: the vehicle is
	// the *provider's* commitment, and a customer countering on price must not silently drop the
	// truck out of the negotiation the customer is comparing.
	VehicleID *uuid.UUID
}

// IsEmpty reports whether the caller named no field at all.
//
// A revision that changes nothing is refused rather than answered, exactly as fleet refuses an edit
// naming no field: it is almost always a client defect, and answering `200` with the unchanged bid
// would hide it behind a success.
func (r Revision) IsEmpty() bool {
	return r.AmountCents == nil && r.PickupAt == nil && r.DeliverBy == nil &&
		r.Message == nil && r.VehicleID == nil
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
		VehicleID:   b.VehicleID,
	}

	if r.VehicleID != nil {
		o.VehicleID = *r.VehicleID
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

// Counter is one party answering the other party's offer with different terms (SHIP-87).
//
// Docs/02 §4: "A customer counter-offer supersedes the prior provider offer. A provider
// counter-offer supersedes the prior customer offer." One type for both directions, because the two
// sentences describe one act — which is also why there is one endpoint rather than two.
//
// # It carries the same pointers a [Revision] does, and that is the decision 000501 deferred to this
// ticket
//
// 000501 removed `ck_bids_offer_has_timing` because it would have bound this choice: "whether a
// customer countering on *price alone* restates the timing or inherits it from the offer it
// supersedes is that ticket's decision."
//
// **It inherits.** A counter names what it is changing and takes the rest from the offer it answers,
// for the reason [Revision] gives about a revision and one more that is specific to a negotiation.
// The reason it shares: a client made to restate two timestamps it is not touching is a client that
// can get them wrong, and countering on price alone is the ordinary case rather than the exceptional
// one. The reason it does not: in a negotiation the *unchanged* fields are the agreement so far, and
// a shape that made both parties restate them would turn every round into a fresh offer that
// happened to look similar — which is exactly the thing "supersedes the prior offer" says a counter
// is not.
//
// The merged result is validated by [Offer.validate], the same validator a placement meets, so a
// counter cannot reach a state a placement could not. That includes the surprise [Revision] records:
// countering on price alone against an offer whose `pickup_at` has since passed is refused naming
// `pickup_at`, a field the caller did not send.
//
// # There is no party field and no status field
//
// Which party is countering is decided from the caller and the offer being answered, never from the
// body — an author in a request body would be an authorisation decision made from client input,
// which Docs/07 §3 puts on the platform. The status is the platform's, as it is on every other shape
// in this package.
//
// # Unlike a [Revision], it carries a key, because it writes a row
//
// [Revision] has no key and says why at length: an `UPDATE` applied twice reaches the state applying
// it once reaches, so there is no second row for a retry to create. A counter is an `INSERT`, so the
// whole of that argument runs the other way and SHIP-84's applies instead — the key is stored on the
// row, and a retry that outlives the middleware's cache is answered from the record rather than
// adding a second link to the chain.
type Counter struct {
	AmountCents *int64
	PickupAt    *time.Time
	DeliverBy   *time.Time

	// Message is the conditions accompanying the counter. Present and blank clears whatever the
	// offer being answered carried, which is the treatment [Revision] gives it.
	Message *string

	// Key is the caller's idempotency key, which becomes the new row's. Required: see
	// [ErrNoIdempotencyKey].
	Key string
}

// terms is the counter's changes as a [Revision], so that the merge is written once.
//
// The two shapes are the same four pointers over the same four columns, and the merge rule is the
// same rule: name what changes, inherit the rest, validate the whole. A second copy of `applyTo`
// would be a second place for that rule to be corrected, and the correction that reached only one of
// them would produce a revision and a counter that disagreed about what an omitted field means.
//
// It is not the other way round — [Revision] is not defined in terms of [Counter] — because a
// revision genuinely has no key and giving it one would reintroduce the trap [Revision] documents.
func (c Counter) terms() Revision {
	return Revision{
		AmountCents: c.AmountCents,
		PickupAt:    c.PickupAt,
		DeliverBy:   c.DeliverBy,
		Message:     c.Message,
	}
}

// IsEmpty reports whether the caller named no field at all.
//
// Refused rather than answered, for [Revision.IsEmpty]'s reason and a sharper one. A counter that
// changes nothing is not a counter: it is agreement, and agreement is the *award* — the customer's
// act, through an endpoint this ticket does not build. Writing it as a counter would put a second
// identical offer at the head of the chain and leave a client believing it had done something.
func (c Counter) IsEmpty() bool { return c.terms().IsEmpty() }

// applyTo is the offer that would stand if this counter were made.
func (c Counter) applyTo(b Bid) Offer { return c.terms().applyTo(b) }

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
