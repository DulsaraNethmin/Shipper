package jobs

import (
	"time"

	"github.com/google/uuid"
)

// Status is one of the twelve job states in Docs/02 §1.
//
// The values are the document's own strings — spaces and sentence case included — because
// Docs/10 §3.4 requires it, and the reason is that three languages hold a copy of this list. Go,
// Dart and TypeScript can each be diffed against Docs/02 rather than against one another, and a
// value that has drifted is visible without holding two files side by side. SHIP-56a generates
// the other two from contracts/statuses.yaml; this is the Go copy until it does.
//
// The wire form is deliberately not here. Docs/10 §4.7 puts enum values on the wire in lower
// snake case even where the stored form has spaces, and the mapping between the two belongs with
// the generator that has to produce it in three languages, not with the first language to need
// it.
type Status string

const (
	StatusDraft           Status = "Draft"
	StatusOpen            Status = "Open"
	StatusNegotiating     Status = "Negotiating"
	StatusAwarded         Status = "Awarded"
	StatusDriverAssigned  Status = "Driver assigned"
	StatusEnRouteToPickup Status = "En route to pickup"
	StatusPickedUp        Status = "Picked up"
	StatusInTransit       Status = "In transit"
	StatusDelivered       Status = "Delivered"
	StatusCompleted       Status = "Completed"
	StatusCancelled       Status = "Cancelled"
	StatusDisputed        Status = "Disputed"
)

// Statuses is every status, in the order Docs/02 §1 lists them.
//
// Ordered rather than a set because the order is the lifecycle, and because it is what
// TestJobStatusConstraintMatchesTheGoConstants compares against ck_jobs_status. Docs/10 §3.4
// requires that pairing for every enumeration: the constraint is read out of pg_constraint and
// held to this list, which is what stops the twelve statuses and the eight bid statuses drifting
// when they are built on separate branches.
var Statuses = []Status{
	StatusDraft,
	StatusOpen,
	StatusNegotiating,
	StatusAwarded,
	StatusDriverAssigned,
	StatusEnRouteToPickup,
	StatusPickedUp,
	StatusInTransit,
	StatusDelivered,
	StatusCompleted,
	StatusCancelled,
	StatusDisputed,
}

// Valid reports whether s is one of the twelve.
func (s Status) Valid() bool {
	for _, known := range Statuses {
		if s == known {
			return true
		}
	}
	return false
}

func (s Status) String() string { return string(s) }

// Job is the job record as SHIP-56 leaves it: who owns it and what state it is in.
//
// It grows through M2 — category (SHIP-58), addresses (SHIP-60), the draft's own fields
// (SHIP-62), the budget (SHIP-67) and expiry (SHIP-68) — and each of those arrives with the
// ticket that gives it meaning rather than as an empty column waiting for one.
//
// Status has no setter and is not assigned anywhere outside the transition guard. The database
// refuses the change regardless (000402), so a struct field that is written by mistake produces
// a failed transaction rather than a silently moved job.
type Job struct {
	ID         uuid.UUID
	CustomerID uuid.UUID
	Status     Status

	CreatedAt time.Time
	UpdatedAt time.Time
}
