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

// permitted is the transition table of Docs/02 §2, which that document calls authoritative for
// the guard.
//
// It lives in Go and in exactly one place. 000402 enforces that a status change went through the
// guard and was recorded; it deliberately does not also encode which moves are legal, because a
// second copy of this table is a copy that drifts, and a move that is legal in one layer and
// impossible in the other is a defect nobody can reproduce from either side.
//
// Two entries are easy to read as mistakes and are neither, both called out in Docs/02 §2:
//
//   - Awarded may go straight to En route to pickup. Driver assigned is skippable, because a
//     provider driving the job themselves has nobody to nominate.
//   - Picked up, In transit and Delivered may not be cancelled. Docs/02 §6.2 is explicit that
//     once the goods are in somebody's vehicle this is a support case, not a state change — the
//     route out is Disputed, and an administrator resolving it.
//
// Every status is a key, including the two that end the lifecycle. An absent key and an empty
// one would mean the same thing to Permitted, and saying it explicitly is what lets a test
// insist that all twelve appear.
var permitted = map[Status][]Status{
	StatusDraft:           {StatusOpen, StatusCancelled},
	StatusOpen:            {StatusNegotiating, StatusAwarded, StatusCancelled},
	StatusNegotiating:     {StatusOpen, StatusAwarded, StatusCancelled},
	StatusAwarded:         {StatusDriverAssigned, StatusEnRouteToPickup, StatusOpen, StatusDisputed},
	StatusDriverAssigned:  {StatusEnRouteToPickup, StatusOpen, StatusDisputed},
	StatusEnRouteToPickup: {StatusPickedUp, StatusDisputed},
	StatusPickedUp:        {StatusInTransit, StatusDisputed},
	StatusInTransit:       {StatusDelivered, StatusDisputed},
	StatusDelivered:       {StatusCompleted, StatusDisputed},
	StatusDisputed:        {StatusCompleted, StatusCancelled},

	// Terminal. Docs/02 §2 offers no move out of either, and a job that reached one is
	// finished as far as the lifecycle is concerned.
	StatusCompleted: {},
	StatusCancelled: {},
}

// Permitted reports whether Docs/02 §2 allows a job to move from one status to another.
//
// It is exported because a caller frequently needs to ask before acting rather than after
// failing: the app decides which actions to offer, and Docs/02 §3.1 requires a queued offline
// update that has been overtaken to be absorbed rather than reported as an error. Both are
// decisions made with this question, and neither is an authorisation decision — the platform
// still refuses the move itself (Docs/07 §3).
func Permitted(from, to Status) bool {
	for _, next := range permitted[from] {
		if next == to {
			return true
		}
	}
	return false
}

// ActorType is who moved a job.
//
// Finer-grained than audit_log's three kinds on purpose. Docs/02 §1 names a primary actor per
// status, and the difference between a provider and the driver they nominated is exactly the
// distinction support needs when a delivery goes wrong.
type ActorType string

const (
	ActorCustomer ActorType = "customer"
	ActorProvider ActorType = "provider"
	ActorDriver   ActorType = "driver"
	ActorAdmin    ActorType = "admin"

	// ActorSystem is the platform acting on its own: job expiry (SHIP-68), the 72-hour
	// auto-complete (SHIP-119). It has no account behind it and must not claim one.
	ActorSystem ActorType = "system"
)

// ActorTypes is every actor kind, in the order ck_job_status_history_actor_type lists them.
var ActorTypes = []ActorType{ActorCustomer, ActorProvider, ActorDriver, ActorAdmin, ActorSystem}

// Valid reports whether a is one of the five.
func (a ActorType) Valid() bool {
	for _, known := range ActorTypes {
		if a == known {
			return true
		}
	}
	return false
}

func (a ActorType) String() string { return string(a) }

// Actor is who is making a transition.
//
// ID is the account for a customer, a provider or an administrator, and the driver assignment
// (SHIP-105) for a driver — the driver portal is link-authenticated and its user has no account
// at all (Docs/07 §3). It is the zero UUID for the platform itself, which is the one case the
// database requires to be absent rather than present.
type Actor struct {
	Type ActorType
	ID   uuid.UUID
}

// System is the platform acting on its own behalf.
func System() Actor { return Actor{Type: ActorSystem} }

// User is an account-backed actor.
func User(kind ActorType, id uuid.UUID) Actor { return Actor{Type: kind, ID: id} }

// Move is one request to change a job's status.
//
// Reason is optional for everyone except an administrator, for whom Docs/01 §3 makes it the
// condition of changing a commercial record at all.
//
// RecordedAt is the *actor's* clock — when the person or process says they acted. Docs/02 §3.1
// requires it to be carried separately from when the platform accepted the change, because a
// driver records a milestone in a place with no signal and the request arrives much later. The
// zero value means "now", which is the ordinary case for anything happening online.
type Move struct {
	JobID  uuid.UUID
	To     Status
	Actor  Actor
	Reason string

	RecordedAt time.Time
}

// StatusChange is one recorded transition — a row of job_status_history.
type StatusChange struct {
	ID    uuid.UUID
	JobID uuid.UUID

	From Status
	To   Status

	Actor  Actor
	Reason string

	// ActorRecordedAt is the actor's clock; ServerRecordedAt is the platform's. Docs/10 §3.3
	// requires two explicit columns and Docs/02 §3.1 says why: the first is what the customer
	// is shown, the second is what audit and support rely on.
	ActorRecordedAt  time.Time
	ServerRecordedAt time.Time
}

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
