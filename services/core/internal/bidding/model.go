package bidding

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
