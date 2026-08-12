package delivery

// Milestone is one of the five things a provider or an assigned driver records on a delivery
// (Docs/01 §4.4).
//
// The values are Docs/02 §1's own strings — spaces and sentence case included — because
// Docs/10 §3.4 requires it and because the same list is held in SQL by ck_milestones_milestone.
// TestMilestoneConstraintMatchesTheGoConstants reads the constraint out of pg_constraint and
// holds the two together in both directions, which is the pairing Docs/10 §3.4 asks of every
// enumeration.
//
// # Why this is not jobs.Status
//
// Four of the five are also job statuses and the strings are identical. They are still a
// different list, for two reasons that will not go away.
//
// Mechanically, domains do not import each other (Docs/06 §4.1), so delivery cannot reach into
// jobs for the constants even if it wanted to.
//
// Substantively, the two lists answer different questions and Docs/02 §2 says so directly:
// "Where 01 §4.4 numbers the five recordable milestones as a sequence, it is describing what a
// driver records, not constraining what the guard accepts." A milestone need not move the job at
// all — a late "Picked up" arriving after "In transit" is recorded and absorbed (SHIP-112) — and
// there are statuses nobody records, because 'Completed' expires into existence after seventy-two
// hours (Docs/02 §6.1) and 'Cancelled' and 'Disputed' are not things that happen on a delivery.
//
// # Not a sequence, despite Docs/01 §4.4 numbering one
//
// 'Driver assigned' is skippable: Docs/02 §2 permits Awarded → En route to pickup directly,
// because a provider driving the job themselves has nobody to nominate. Anything that treats the
// order below as a chain to be walked will be wrong about that job.
type Milestone string

const (
	MilestoneDriverAssigned  Milestone = "Driver assigned"
	MilestoneEnRouteToPickup Milestone = "En route to pickup"
	MilestonePickedUp        Milestone = "Picked up"
	MilestoneInTransit       Milestone = "In transit"
	MilestoneDelivered       Milestone = "Delivered"
)

// Milestones is every milestone, in the order Docs/01 §4.4 numbers them.
//
// Ordered rather than a set because that is the order a delivery normally runs in and the order
// a timeline reads in, not because anything may rely on it as a sequence — see above.
var Milestones = []Milestone{
	MilestoneDriverAssigned,
	MilestoneEnRouteToPickup,
	MilestonePickedUp,
	MilestoneInTransit,
	MilestoneDelivered,
}

// Valid reports whether m is one of the five.
func (m Milestone) Valid() bool {
	for _, known := range Milestones {
		if m == known {
			return true
		}
	}
	return false
}

func (m Milestone) String() string { return string(m) }

// ActorType is who recorded a milestone.
//
// Narrower than job_status_history's five on purpose. Docs/02 §3: "delivery-status updates must
// be made only by the awarded provider, their assigned driver, or an administrator acting with an
// audit reason". A customer is not among them — confirming a delivery is not recording one, and
// that confirmation is a status transition rather than a milestone.
//
// The platform is here because Docs/02 §2 permits Picked up → In transit as "an automatic
// presentation change", which is a milestone with nobody behind it.
type ActorType string

const (
	ActorProvider ActorType = "provider"

	// ActorDriver identifies the actor by their driver_assignments row rather than by an
	// account, because a driver has none: the driver portal is link-authenticated and holds a
	// job-scoped token that cannot be exchanged for a session (Docs/07 §3). This is the
	// convention 000401 declared for job_status_history and 000601 follows.
	ActorDriver ActorType = "driver"

	// ActorAdmin has no users row either. ck_users_role refuses 'admin' because admin sign-in
	// is a separate system (SHIP-147).
	ActorAdmin ActorType = "admin"

	// ActorSystem names no identity at all, which ck_milestones_actor_id enforces.
	ActorSystem ActorType = "system"
)

// ActorTypes is every actor that may record a milestone.
//
// Paired with ck_milestones_actor_type by TestMilestoneActorConstraintMatchesTheGoConstants, for
// the reason Docs/10 §3.4 gives: a value the database accepts and Go has no constant for is a
// state no code handles.
var ActorTypes = []ActorType{
	ActorProvider,
	ActorDriver,
	ActorAdmin,
	ActorSystem,
}

// Valid reports whether a is one of the four.
func (a ActorType) Valid() bool {
	for _, known := range ActorTypes {
		if a == known {
			return true
		}
	}
	return false
}

func (a ActorType) String() string { return string(a) }
