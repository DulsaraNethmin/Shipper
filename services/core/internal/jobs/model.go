package jobs

import (
	"time"

	"github.com/google/uuid"
)

// The twelve job statuses are generated (SHIP-56a).
//
// contracts/statuses.yaml is the source and status_gen.go beside this file is the Go form:
// Status, its constants, Statuses, Valid, String, Wire and StatusFromWire. Docs/10 §8.2 named
// that arrangement long before there was a generator, and three files carried a comment
// promising it.
//
// **What stayed here is the part a second language must not have a copy of.** The transition
// table below is a decision rather than a vocabulary: Docs/07 §3 puts every authorisation and
// permission decision on the platform, so a generated copy on the phone would be a second
// authority for a question that has exactly one. The same line keeps ActorType hand-written —
// it names a kind of person rather than a lifecycle state, and no client enumerates it.

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

// TimeWindow is when something may happen, as a customer thinks about it.
//
// A window rather than an instant because road transport is not scheduled to the minute: a
// customer who says "Tuesday or Wednesday" gets more bids than one who says "10:15", and
// Docs/01 §4.1 asks for date windows for that reason.
//
// Either end may be absent on its own. A customer who knows only the earliest date they can
// release the goods has said something useful, and whether that is enough to publish is
// SHIP-63's judgement rather than this type's.
type TimeWindow struct {
	Start time.Time
	End   time.Time
}

// IsZero reports whether neither end was given.
func (w TimeWindow) IsZero() bool { return w.Start.IsZero() && w.End.IsZero() }

// Dimensions is how big the goods are, in centimetres.
//
// Zero means "not supplied", and that is safe rather than sloppy: ck_jobs_length_cm and its two
// siblings refuse a dimension that is not positive, so zero is a value the column cannot hold
// and the two readings cannot be confused. The same argument covers [Job.WeightKg].
type Dimensions struct {
	LengthCm int
	WidthCm  int
	HeightCm int
}

// IsZero reports whether no dimension was supplied.
func (d Dimensions) IsZero() bool { return d.LengthCm == 0 && d.WidthCm == 0 && d.HeightCm == 0 }

// Job is the job record: who owns it, what state it is in, and what the customer has said about
// the delivery so far.
//
// It grows further through M2 — the goods category is still SHIP-58's — and each field arrives
// with the ticket that gives it meaning rather than as an empty column waiting for one. The
// budget (SHIP-67) and the expiry deadline (SHIP-68) are both here now.
//
// **Every field below Status is optional**, because a Draft is allowed to be incomplete: Docs/01
// §4.1 lets a customer save a draft and come back to it, and SHIP-75 has a partly completed job
// surviving an app restart. Completeness is decided at publication (SHIP-63), which is where
// Docs/02 §2 puts it.
//
// Status has no setter and is not assigned anywhere outside the transition guard. The database
// refuses the change regardless (000402), so a struct field that is written by mistake produces
// a failed transaction rather than a silently moved job.
//
// # BudgetCents is on this struct and must never reach a provider-facing shape
//
// Docs/01 §4.3 keeps the customer's maximum private — not as an amount, a band, or a "budget
// supplied" flag. A [Job] is the domain's own record and carries it; what may not carry it is a
// response, an event payload, or anything else that leaves the service towards a provider. That
// is held by two things rather than by memory: SHIP-82's feed and SHIP-83's provider detail get
// response types of their own rather than this one with fields hidden, and
// TestOnlyTheOwnersResponseCarriesTheBudget reads this package's own source and refuses a
// `budget` json tag anywhere but on the owning customer's response.
type Job struct {
	ID         uuid.UUID
	CustomerID uuid.UUID
	Status     Status

	// Where the goods are collected and where they are taken, each with whatever the
	// geocoder made of it (SHIP-60).
	Pickup  Location
	Dropoff Location

	// What is being moved, in the customer's words, and how big it is (SHIP-62).
	GoodsDescription string
	Dimensions       Dimensions
	WeightKg         float64

	// What the customer believes the job needs, and anything the driver has to know —
	// access constraints, stairs, gate codes.
	VehicleRequirement string
	HandlingNotes      string

	PickupWindow  TimeWindow
	DropoffWindow TimeWindow

	// BudgetCents is the customer's maximum, in minor units (SHIP-67).
	//
	// Cents rather than a float, per Docs/10 §3.3: money is numeric(12,2) in PostgreSQL and
	// int64 minor units in Go, never a float. AUD is implied; there is no currency in the MVP.
	//
	// Zero means not supplied, which is unambiguous because ck_jobs_budget refuses zero and
	// everything below it — the same arrangement [Dimensions] relies on.
	BudgetCents int64

	// ExpiresAt is when an Open job stops being offered (SHIP-68).
	//
	// The earlier of fourteen days after publication and the pickup window ending
	// (Docs/02 §6.3). Zero until the job is published: 000406's trigger sets it as the job
	// becomes Open, so nothing in Go computes it and no route into Open can forget it.
	//
	// A job that has left Open keeps the value it had. Nothing reads it outside [ExpiryClaim],
	// which filters on status, and if the job returns to Open — Docs/02 §6.2 — the original
	// deadline is the one Docs/02 §6.3 asks for.
	//
	// It is moved by exactly one thing a customer can reach: [Service.Extend] (SHIP-70).
	ExpiresAt time.Time

	// ExpiryWarnedAt is when the owner was told this job was about to expire (SHIP-69).
	//
	// Zero means "not warned against the deadline it has now" — which covers a job that has
	// never been warned and a job whose deadline has moved since, because 000407's trigger
	// clears the column whenever [ExpiresAt] changes. That is what makes the warning fire once
	// per deadline rather than once per job.
	//
	// **Internal machinery, and not on any response.** A client that wants to show "expires in
	// two days" has [ExpiresAt] and a clock; whether the platform has already sent a push about
	// it is the platform's business, and putting it on the wire would invite a client to decide
	// whether to warn — which is the notification domain's decision, not the app's.
	ExpiryWarnedAt time.Time

	CreatedAt time.Time
	UpdatedAt time.Time
}

// DraftFields is everything a customer may set on a draft, with absent distinguished from empty.
//
// One type serves both verbs, and the nil pointer means something slightly different in each —
// which is the whole reason these are pointers rather than values:
//
//   - to [Service.CreateDraft], nil means "not supplied", and the field starts empty;
//   - to [Service.UpdateDraft], nil means "not mentioned", and the field is left as it was.
//
// A non-nil pointer to a zero value is a third thing and is honoured in both. It is how a
// customer clears a handling note they no longer want, which a value type could not express at
// all: `""` would be indistinguishable from "did not mention it", and the note would be
// impossible to remove.
//
// **Status is deliberately not here, and never will be.** Job status is not a settable field;
// every transition passes the guard in [Service.Transition] (Docs/02 §2, CLAUDE.md). The wire
// types in http.go do not carry it either, so a client that sends one is told the field does not
// exist rather than having it quietly ignored.
type DraftFields struct {
	Pickup  *Address
	Dropoff *Address

	GoodsDescription *string
	LengthCm         *int
	WidthCm          *int
	HeightCm         *int
	WeightKg         *float64

	VehicleRequirement *string
	HandlingNotes      *string

	PickupWindow  *TimeWindow
	DropoffWindow *TimeWindow

	// BudgetCents is the customer's maximum, in minor units (SHIP-67). A pointer to zero
	// clears it, which is how a customer who set a budget removes it again.
	BudgetCents *int64
}

// IsEmpty reports whether nothing at all was supplied.
//
// Used by [Service.UpdateDraft] to refuse a PATCH that names no field. An empty patch is almost
// always a client defect — a request whose body was built from an empty form — and answering
// 200 to it would tell that client everything is fine.
func (f DraftFields) IsEmpty() bool {
	return f.Pickup == nil && f.Dropoff == nil &&
		f.GoodsDescription == nil &&
		f.LengthCm == nil && f.WidthCm == nil && f.HeightCm == nil && f.WeightKg == nil &&
		f.VehicleRequirement == nil && f.HandlingNotes == nil &&
		f.PickupWindow == nil && f.DropoffWindow == nil &&
		f.BudgetCents == nil
}
