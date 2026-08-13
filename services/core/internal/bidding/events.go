package bidding

import (
	"context"
	"time"

	"github.com/google/uuid"

	"github.com/DulsaraNethmin/Shipper/services/core/internal/db"
	"github.com/DulsaraNethmin/Shipper/services/core/internal/events"
)

// The bidding domain's entry in the event catalogue, and every emission this package makes
// (SHIP-136).
//
// One file, in the shape internal/jobs/events.go established at SHIP-135: the event types, the
// payload struct behind each, one `init` registering them, and the emit helpers the service calls.
// **internal/events was not edited to add any of this**, which is what that ticket left the easy
// half for — the catalogue is a table it holds, not a list it writes, and a domain declares its own
// events in the same way it declares its own routes and its own ports.
//
// # Docs/01 §4.5's bid line is "new bid, counter-offer, withdrawal, or bid expiry", and this covers
// three of the four
//
// A placement, a counter and a withdrawal all emit here. **Bid expiry does not, because no bid
// expires**: [StatusExpired] is declared, `ck_bids_status` accepts it, and nothing in this platform
// writes it — SHIP-89's scheduled task is the ticket that starts, and its *Done when* already reads
// "bids expire on their own terms and emit an event". A schema registered here for an event nothing
// emits would put a line in cmd/api/events_golden.txt describing a payload no code marshals, which
// is the sort of thing that reads as covered when it is not. Docs/11 §3 records it rather than the
// catalogue implying it.
//
// Two more are emitted that §4.5 does not name in the bid line, and both are its "bid accepted"
// sentence read to its end: [EventBidAccepted] for the offer the customer chose, and
// [EventBidRejected] for each offer the same transaction closed (SHIP-93). A losing provider being
// told is the whole reason the sweep is in the award's transaction rather than beside it, and
// [EventBidRevised] is Docs/01 §4.2's middle verb — a live offer at a new price the customer may
// accept, which is a state change a customer watching a job has to hear about.
//
// # The aggregate id is the bid, not the job, and the pair of consequences is worth stating
//
// Ordering is promised per aggregate (000004_outbox.up.sql), and the key is the aggregate id — so
// keying on the bid means **one offer's events are ordered against each other** and two offers'
// are not. That is the ordering that matters here: `bid.placed` → `bid.revised` → `bid.withdrawn`
// all name one row, and a consumer that saw the withdrawal before the placement would notify a
// customer about an offer that is already gone. Two different providers' offers on one job have no
// order between them and need none.
//
// A counter is the one case where that is not the whole story, because it writes two rows: the
// displaced head moves to 'Superseded' and the counter is inserted at 'Submitted'. **One event, on
// the counter, naming the offer it displaced** — see [EventBidCountered].
//
// # No payload here carries anything of the customer's budget, and none ever may
//
// Docs/01 §4.3 keeps the customer's maximum private from providers, and CLAUDE.md is explicit that
// an event payload is a response: it travels through the outbox, onto a topic, into every consumer
// there will ever be, past every point a response body could have redacted it. Nothing below names
// a field of the job at all beyond its identifier, which is [Bid]'s own rule (model.go) carried
// across.
//
// **`amount_cents` is not an exception to that, and the distinction is Docs/11 §3's rather than
// this file's.** The amount on a bid is the *provider's* number; the amount on a customer's
// counter-offer is a number the customer deliberately offered to that provider, which
// `GET /v1/jobs/{id}/bids/{bid_id}/chain` has served to them since SHIP-88. Neither is the private
// maximum Docs/01 §4.3 protects.
const (
	// EventBidPlaced is a provider's first offer on a job (SHIP-84).
	EventBidPlaced = "bid.placed"

	// EventBidRevised is the same offer at a different price or timing (SHIP-85).
	//
	// The row keeps its identifier, so this is the second event on one aggregate and the ordering
	// argument above is what makes the pair readable.
	EventBidRevised = "bid.revised"

	// EventBidWithdrawn is a provider taking their own offer back (SHIP-86).
	EventBidWithdrawn = "bid.withdrawn"

	// EventBidCountered is either party answering the other with different terms (SHIP-87, SHIP-88).
	//
	// # One event for a transition that writes two rows, and Docs/02 §4 is why it can be one
	//
	// A counter supersedes the offer it answers and inserts a new one. That is two rows and — read
	// from the two ends — the two statuses Docs/02 §4 lists as `Countered` and `Superseded`. This
	// domain writes only `Superseded`, deliberately and since SHIP-87 (see [StatusCountered]), so
	// there is one transition here rather than two and it gets one event.
	//
	// It is emitted **on the counter**, which is the row that now exists, and names the offer it
	// displaced in `superseded_bid_id`. The party being superseded is by construction the party
	// being countered, so a second event on the displaced row would tell the same person the same
	// thing twice.
	//
	// **Docs/11 §9 has been holding the Countered/Superseded question for whoever next opened this
	// package, and this is where an event name would have forced it.** It did not: naming the event
	// after the act rather than after either status is true whichever way §9 is eventually settled,
	// and if `Countered` ever acquires a distinction of its own that is a second event and a version
	// bump rather than a rename. SHIP-96 still owns the document.
	EventBidCountered = "bid.countered"

	// EventBidAccepted is the offer the customer awarded (SHIP-92).
	EventBidAccepted = "bid.accepted"

	// EventBidRejected is an offer closed because the job was awarded elsewhere (SHIP-93).
	//
	// One per closed offer rather than one event listing them, because each names a different
	// provider to tell and because the aggregate id is the bid: a single event carrying a list would
	// have to choose one aggregate to be about, and would be about the wrong one for every provider
	// but one.
	EventBidRejected = "bid.rejected"
)

func init() {
	events.Register(events.Schema{
		Type:      EventBidPlaced,
		Aggregate: events.AggregateBid,
		Version:   1,
		Payload:   bidOffered{},
	})

	events.Register(events.Schema{
		Type:      EventBidRevised,
		Aggregate: events.AggregateBid,
		Version:   1,
		Payload:   bidOffered{},
	})

	events.Register(events.Schema{
		Type:      EventBidCountered,
		Aggregate: events.AggregateBid,
		Version:   1,
		Payload:   bidCountered{},
	})

	events.Register(events.Schema{
		Type:      EventBidWithdrawn,
		Aggregate: events.AggregateBid,
		Version:   1,
		Payload:   bidClosed{},
	})

	events.Register(events.Schema{
		Type:      EventBidAccepted,
		Aggregate: events.AggregateBid,
		Version:   1,
		Payload:   bidAccepted{},
	})

	events.Register(events.Schema{
		Type:      EventBidRejected,
		Aggregate: events.AggregateBid,
		Version:   1,
		Payload:   bidClosed{},
	})
}

// bidOffered is what a live offer looks like: a placement and a revision both produce one.
//
// **One struct for two event types, which the catalogue permits and which is the honest shape
// here.** A placement and a revision differ in what happened, not in what now stands, and the event
// *type* is where "what happened" lives. Two identical structs would be two places for one shape to
// drift, and their two lines in cmd/api/events_golden.txt would have to be kept equal by hand.
//
// There is no timestamp field. The envelope carries `occurred_at`, written from the row's own
// clock (see [Service.emitOffer]), and a second copy inside the payload could only ever disagree
// with it.
type bidOffered struct {
	BidID      string `json:"bid_id"`
	JobID      string `json:"job_id"`
	ProviderID string `json:"provider_id"`

	// OfferedBy is which party made this offer. A placement is always the provider's; a revision
	// always is too, because [changeable] refuses anything else. It is here so a consumer reading
	// one event type does not have to know which of the two rules produced it.
	OfferedBy string `json:"offered_by"`

	AmountCents int64 `json:"amount_cents"`

	PickupAt  time.Time `json:"pickup_at"`
	DeliverBy time.Time `json:"deliver_by"`
}

// bidCountered is [bidOffered] plus the offer this one displaced.
//
// A separate struct rather than an optional field on [bidOffered], because a counter always has a
// predecessor and a placement never does: an `omitempty` string would make "no predecessor" and
// "the predecessor was not filled in" the same value, and the field set in the golden file would
// stop saying which events actually carry it.
type bidCountered struct {
	BidID      string `json:"bid_id"`
	JobID      string `json:"job_id"`
	ProviderID string `json:"provider_id"`

	// OfferedBy is the party that made the counter — either one, which is the whole of SHIP-87.
	OfferedBy string `json:"offered_by"`

	AmountCents int64 `json:"amount_cents"`

	PickupAt  time.Time `json:"pickup_at"`
	DeliverBy time.Time `json:"deliver_by"`

	// SupersededBidID is the offer this counter answered and displaced.
	SupersededBidID string `json:"superseded_bid_id"`
}

// bidClosed is an offer that is over, and it serves both ways an offer can end here.
//
// The status is what distinguishes them and it is in the payload as well as in the event type,
// because a consumer holding a row it is reconciling wants the value the column now has rather than
// a mapping it has to maintain from event names to statuses.
type bidClosed struct {
	BidID      string `json:"bid_id"`
	JobID      string `json:"job_id"`
	ProviderID string `json:"provider_id"`

	// OfferedBy matters most on a rejection: an award closes the customer's own outstanding
	// counter along with every provider offer, and a consumer resolving who to notify would
	// otherwise write to the customer about their own offer being declined by themselves.
	OfferedBy string `json:"offered_by"`

	Status string `json:"status"`
}

// bidAccepted is the award, from the bid's side.
//
// `job.status_changed` says the job moved to 'Awarded' and this says which offer it moved on. Both
// are emitted, in one transaction, by two domains that do not import each other — which is what the
// award being one transaction owned by `bidding` (Docs/10 §3.2) looks like from the outbox.
//
// CustomerID is here and is the one field on any payload in this file naming an account that is not
// the provider. It is the account that acted, in the same sense `job.status_changed` carries its
// actor, and a consumer telling a provider they have won needs to be able to tell the customer they
// have awarded without reading the job back.
type bidAccepted struct {
	BidID      string `json:"bid_id"`
	JobID      string `json:"job_id"`
	ProviderID string `json:"provider_id"`
	CustomerID string `json:"customer_id"`

	AmountCents int64 `json:"amount_cents"`

	PickupAt  time.Time `json:"pickup_at"`
	DeliverBy time.Time `json:"deliver_by"`
}

// emitOffer writes [EventBidPlaced] or [EventBidRevised], inside the caller's transaction.
//
// # OccurredAt is the row's own timestamp rather than the injected clock, and that is deliberate
//
// `bids.created_at` and `bids.updated_at` are `now()` — the transaction's timestamp — so the event
// says the same instant the row does, from one source. Reading the service's clock here would
// produce a second answer that agrees only by luck, and disagrees for exactly as long as a
// transaction takes. jobs.Service.emit makes the same call with `server_recorded_at`.
func (s *Service) emitOffer(ctx context.Context, r db.Runner, eventType string, b Bid, at time.Time) error {
	return s.emit(ctx, r, eventType, b.ID, at, bidOffered{
		BidID:       b.ID.String(),
		JobID:       b.JobID.String(),
		ProviderID:  b.ProviderID.String(),
		OfferedBy:   string(b.OfferedBy),
		AmountCents: b.AmountCents,
		PickupAt:    b.PickupAt,
		DeliverBy:   b.DeliverBy,
	})
}

// emitCountered writes [EventBidCountered] for the counter that now stands.
func (s *Service) emitCountered(ctx context.Context, r db.Runner, counter Bid, superseded uuid.UUID) error {
	return s.emit(ctx, r, EventBidCountered, counter.ID, counter.CreatedAt, bidCountered{
		BidID:           counter.ID.String(),
		JobID:           counter.JobID.String(),
		ProviderID:      counter.ProviderID.String(),
		OfferedBy:       string(counter.OfferedBy),
		AmountCents:     counter.AmountCents,
		PickupAt:        counter.PickupAt,
		DeliverBy:       counter.DeliverBy,
		SupersededBidID: superseded.String(),
	})
}

// emitClosed writes [EventBidWithdrawn] or [EventBidRejected].
func (s *Service) emitClosed(ctx context.Context, r db.Runner, eventType string, b Bid) error {
	return s.emit(ctx, r, eventType, b.ID, b.UpdatedAt, bidClosed{
		BidID:      b.ID.String(),
		JobID:      b.JobID.String(),
		ProviderID: b.ProviderID.String(),
		OfferedBy:  string(b.OfferedBy),
		Status:     string(b.Status),
	})
}

// emitAccepted writes [EventBidAccepted] for the offer the customer awarded.
func (s *Service) emitAccepted(ctx context.Context, r db.Runner, b Bid, customerID uuid.UUID) error {
	return s.emit(ctx, r, EventBidAccepted, b.ID, b.UpdatedAt, bidAccepted{
		BidID:       b.ID.String(),
		JobID:       b.JobID.String(),
		ProviderID:  b.ProviderID.String(),
		CustomerID:  customerID.String(),
		AmountCents: b.AmountCents,
		PickupAt:    b.PickupAt,
		DeliverBy:   b.DeliverBy,
	})
}

// emit is the one place this domain builds an event and hands it to the sink.
//
// One function rather than a `events.New` at each of the five call sites, for the reason
// [Service.refused] exists in `delivery`: the argument that is easy to get wrong is the aggregate
// id, and a helper per event type makes it impossible to pass the job's where the bid's belongs.
//
// r must be the transaction making the state change, which every caller here already is — all five
// emitting methods check `r.(pgx.Tx)` at the top or are reached only from one that does. An event
// written outside that transaction can commit when the change does not, which is the failure the
// outbox exists to prevent (Docs/06 §4.0, Docs/10 §6.1).
func (s *Service) emit(
	ctx context.Context,
	r db.Runner,
	eventType string,
	aggregateID uuid.UUID,
	at time.Time,
	payload any,
) error {
	event, err := events.New(eventType, aggregateID, at, payload)
	if err != nil {
		return err
	}
	return s.events.Emit(ctx, r, event)
}
