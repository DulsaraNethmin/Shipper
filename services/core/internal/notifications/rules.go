package notifications

// The routing table (SHIP-137): which domain event tells whom, on which channel, in which
// category.
//
// It is a table rather than a switch because it is the part of this domain a product person can be
// shown. Docs/01 §4.5 lists what must generate a notification; this is that list, mapped onto the
// thirteen events internal/events actually holds, and TestEveryRegisteredEventHasANotificationRule
// in cmd/api holds the two together — a new domain event with no rule here fails that test rather
// than becoming an event nobody is told about.
//
// # Statuses and milestones are plain strings here
//
// `Delivered`, `Cancelled`, `Picked up` — Docs/02 §1's own values, typed out rather than imported.
// internal/jobs is a domain and this is a domain, and the lint refuses the import in both
// directions. It is the same position `admin` takes and for the same reason (SHIP-117): this domain
// does not decide what a valid job status is, ck_jobs_status does, and this reports what it was
// told. Docs/10 §3.4 makes those strings the document's own, which is what lets two copies be
// diffed against the document rather than against each other.
//
// # Every rule routes to push and to email, and to nothing else
//
// Docs/01 §4.5 makes push the primary channel and keeps email "for records and for anything the
// user may need to retrieve later", so a rule that tells somebody tells them both ways. That is a
// change SHIP-139 and SHIP-140 made possible rather than a decision this table took: SHIP-137 wrote
// email alone because a push rule would have produced rows with no address that nothing could ever
// complete — there was no Firebase adapter and no device token registry. Both exist now.
//
// **A recipient with no device registered is not an error and produces no push row.** A customer
// who has never opened the app, or who has signed out everywhere, is reachable by email and by
// nothing else — see [Recipient.AddressesOn]. Nothing here has to know that.
//
// SMS still has a working sender and no product decision behind routing a notification to it; see
// [ChannelSMS]. The dispatcher remains channel-generic: it picks a sender by the row's channel and
// has no list of its own, which is what made adding push one edit here and one adapter.

// Audience is one party a rule tells.
//
// Two of the three are resolved through [Parties] and the third comes out of the event payload. The
// distinction matters for bids: the provider who placed a bid is not the provider awarded the job,
// and a rule that confused them would tell the winner about somebody else's withdrawal.
type Audience string

const (
	// ToJobCustomer is the account that owns the job, from [Parties].
	ToJobCustomer Audience = "job_customer"

	// ToAwardedProvider is the provider whose bid was accepted, from [Parties]. Nobody, on a
	// job that has not been awarded.
	ToAwardedProvider Audience = "awarded_provider"

	// ToBidProvider is the provider named in the event payload — the one whose bid this event
	// is about, awarded or not.
	ToBidProvider Audience = "bid_provider"
)

// Rule is what happens when one event arrives.
//
// A rule with no audience is a decision rather than a gap, and [Rule.Why] is where the decision is
// written. TestARuleThatTellsNobodySaysWhy refuses one without it — an event silently routed to
// nobody is indistinguishable from an event nobody remembered to route.
type Rule struct {
	Category Category
	To       []Audience
	Channels []Channel

	// Headline is the whole of what the notification says.
	//
	// Fixed text with no substitution, and that is the structural half of SHIP-141: a template
	// with a slot is a template somebody fills with an address. The job identifier is added by
	// the renderer and is the only variable part of a body. SHIP-138 owns the real copy.
	Headline string

	// Why a rule tells nobody. Empty on a rule that tells somebody.
	Why string
}

// channels is the set every rule that tells somebody uses, named once — which is what made adding
// push a one-line change, exactly as SHIP-137 predicted it would be.
//
// Push first, because Docs/01 §4.5 makes it the primary channel and because the order is the order
// rows are written in, which a test reads.
var channels = []Channel{ChannelPush, ChannelEmail}

// Rules is the routing table, keyed by event type.
//
// Complete over internal/events.Catalogue(); cmd/api is where that is checked, because it is the
// one binary that links every domain and therefore the only place the whole catalogue exists.
var Rules = map[string]Rule{
	// --- jobs ------------------------------------------------------------------------------

	// The status change is routed by which status it moved to; see [StatusRules]. The rule
	// here exists so the event type is covered by the exhaustiveness check, and its Why points
	// at the table that does the work.
	"job.status_changed": {
		Category: CategoryAward,
		Why: "routed by the status it moved to, in StatusRules — most transitions are " +
			"already announced by the bid or delivery event that caused them, and " +
			"telling somebody twice about one thing is worse than the second message " +
			"being useful",
	},

	"job.expiry_warned": {
		Category: CategoryJobExpiry,
		To:       []Audience{ToJobCustomer},
		Channels: channels,
		Headline: "Your job is about to stop taking offers. You can extend it in the app.",
	},

	"job.expiry_extended": {
		Category: CategoryJobExpiry,
		To:       []Audience{ToJobCustomer},
		Channels: channels,
		Headline: "Your job will keep taking offers until its new closing date.",
	},

	// --- bidding ---------------------------------------------------------------------------

	"bid.placed": {
		Category: CategoryBidding,
		To:       []Audience{ToJobCustomer},
		Channels: channels,
		Headline: "You have a new offer on one of your jobs.",
	},

	"bid.revised": {
		Category: CategoryBidding,
		To:       []Audience{ToJobCustomer},
		Channels: channels,
		Headline: "An offer on one of your jobs has changed.",
	},

	// Both parties, minus whoever countered — see [ConsumeSuppression] on offered_by. A counter
	// is the one bid event either side can raise, which is why it is the only rule with two
	// audiences on the bidding aggregate.
	"bid.countered": {
		Category: CategoryBidding,
		To:       []Audience{ToJobCustomer, ToBidProvider},
		Channels: channels,
		Headline: "There is a counter-offer waiting on one of your jobs.",
	},

	"bid.withdrawn": {
		Category: CategoryBidding,
		To:       []Audience{ToJobCustomer},
		Channels: channels,
		Headline: "An offer on one of your jobs has been withdrawn.",
	},

	"bid.rejected": {
		Category: CategoryBidding,
		To:       []Audience{ToBidProvider},
		Channels: channels,
		Headline: "Your offer was not accepted.",
	},

	"bid.expired": {
		Category: CategoryBidding,
		To:       []Audience{ToBidProvider},
		Channels: channels,
		Headline: "Your offer has expired on its own terms.",
	},

	// Docs/01 §4.5's "bid accepted". Both sides, because an award is the one event where the
	// two of them now have to find each other.
	"bid.accepted": {
		Category: CategoryAward,
		To:       []Audience{ToJobCustomer, ToBidProvider},
		Channels: channels,
		Headline: "A job has been awarded. Both parties can see it in the app.",
	},

	// --- delivery --------------------------------------------------------------------------

	"delivery.driver_assigned": {
		Category: CategoryDelivery,
		To:       []Audience{ToJobCustomer},
		Channels: channels,
		Headline: "A driver has been assigned to your delivery.",
	},

	"delivery.milestone_recorded": {
		Category: CategoryDelivery,
		To:       []Audience{ToJobCustomer},
		Channels: channels,
		Headline: "Your delivery has moved on.",
	},

	// Not routed to the provider: they recorded it. The customer is the one who has been
	// waiting for evidence that the goods arrived.
	"delivery.proof_recorded": {
		Category: CategoryDelivery,
		To:       []Audience{ToJobCustomer},
		Channels: channels,
		Headline: "Proof of delivery has been recorded for your job.",
	},
}

// StatusRules routes job.status_changed by the status the job moved to.
//
// # Why most of these tell nobody
//
// Because something else already did. A job reaching `Awarded` emits bid.accepted in the same
// transaction; a job reaching `Picked up` emits delivery.milestone_recorded; a first offer moving a
// job to `Negotiating` emits bid.placed. Routing the status change as well would send two messages
// about one event, which is a worse failure than the second message being redundant — a recipient
// who learns that this platform emails twice starts ignoring the first one.
//
// What is left is the three transitions no other event announces, and one that nothing can address.
//
// # `Open` is the interesting absence
//
// Docs/01 §4.5 lists "job published" as an essential event, and it is the one line of §4.5 this
// ticket cannot meet. The audience for a published job is *eligible providers*, which is SHIP-81's
// query over service areas and vehicle capability — a fleet-domain read this domain may not make
// and no port here declares. It is not a rendering problem or a channel problem; there is no list
// of recipients to resolve. Whoever builds provider job alerts owns it, and the honest thing is to
// say so here rather than route it to the customer who just pressed publish.
var StatusRules = map[string]Rule{
	"Open": {
		Category: CategoryAward,
		Why: "the audience is eligible providers, which is a fleet-domain query this " +
			"domain cannot make and no port here declares; the customer who published " +
			"it does not need telling that they did",
	},

	"Negotiating": {
		Category: CategoryBidding,
		Why:      "the offer that moved it emits bid.placed, which is already routed",
	},

	"Awarded": {
		Category: CategoryAward,
		Why:      "the award emits bid.accepted in the same transaction, which is already routed",
	},

	"Driver assigned": {
		Category: CategoryDelivery,
		Why:      "delivery.driver_assigned announces this, and is already routed",
	},

	"En route to pickup": {
		Category: CategoryDelivery,
		Why:      "delivery.milestone_recorded announces every milestone, and is already routed",
	},
	"Picked up": {
		Category: CategoryDelivery,
		Why:      "delivery.milestone_recorded announces every milestone, and is already routed",
	},
	"In transit": {
		Category: CategoryDelivery,
		Why:      "delivery.milestone_recorded announces every milestone, and is already routed",
	},
	"Delivered": {
		Category: CategoryDelivery,
		Why:      "delivery.milestone_recorded announces every milestone, and is already routed",
	},

	// The three nothing else announces.

	// Docs/01 §4.5: "bid accepted or job cancelled". Both parties, because a cancellation after
	// award is the provider's problem as much as the customer's — and an expiry (SHIP-68) is a
	// cancellation too, on a job with no provider, where ToAwardedProvider resolves to nobody.
	"Cancelled": {
		Category: CategoryAward,
		To:       []Audience{ToJobCustomer, ToAwardedProvider},
		Channels: channels,
		Headline: "A job has been cancelled.",
	},

	// SHIP-119's seventy-two hour auto-complete arrives here, and so does a customer confirming
	// (Docs/02 §2). The two are indistinguishable in this rule and should be: the message is
	// that the job is closed, and who closed it is in the status history where support reads it.
	"Completed": {
		Category: CategoryAward,
		To:       []Audience{ToJobCustomer, ToAwardedProvider},
		Channels: channels,
		Headline: "A job has been completed and is now closed.",
	},

	// Docs/01 §4.5: "dispute opened or resolved". Opening lands here; resolving lands on
	// Completed or Cancelled above.
	"Disputed": {
		Category: CategoryAward,
		To:       []Audience{ToJobCustomer, ToAwardedProvider},
		Channels: channels,
		Headline: "A dispute has been raised on a job. Support will be in touch.",
	},
}

// RuleFor is the routing decision for one event.
//
// jobStatus is the `to` field of a job.status_changed payload and is ignored for every other event
// type. ok is false only for an event type with no rule at all, which cmd/api's exhaustiveness test
// exists to make impossible — a rule that tells nobody is ok with an empty [Rule.To].
func RuleFor(eventType, jobStatus string) (Rule, bool) {
	rule, ok := Rules[eventType]
	if !ok {
		return Rule{}, false
	}
	if eventType != EventJobStatusChanged {
		return rule, true
	}

	// A status with no entry is a status somebody added to Docs/02 §1 without deciding who
	// hears about it. Reported as unrouted rather than defaulted to telling everybody, because
	// the wrong default here is a message about a state nobody has written copy for.
	byStatus, known := StatusRules[jobStatus]
	if !known {
		return Rule{
			Category: rule.Category,
			Why: "job.status_changed to " + jobStatus + ", which StatusRules has no entry " +
				"for; add one rather than letting a new status route by accident",
		}, true
	}
	return byStatus, true
}

// EventJobStatusChanged is the one event type this package names, because it is the one whose
// routing depends on its payload.
//
// A copy of internal/jobs.EventStatusChanged, and it has to be one: the constant is declared in a
// domain this domain may not import. That is the same trade every string in this file makes, and
// the cost is bounded by [Rules] being held against the live catalogue in cmd/api — an event type
// renamed in `jobs` and not here fails there.
const EventJobStatusChanged = "job.status_changed"
