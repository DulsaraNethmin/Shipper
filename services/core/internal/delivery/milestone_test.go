package delivery_test

import (
	"testing"

	"github.com/DulsaraNethmin/Shipper/services/core/internal/delivery"
)

// The database side of these constants is checked in migrations/delivery_test.go, which reads
// ck_milestones_milestone out of pg_constraint and holds it to delivery.Milestones in both
// directions (Docs/10 §3.4). What that pairing cannot see is a constant declared here and left
// out of the slice: the slice would agree with the database, and Valid would refuse a value the
// package itself defines. These two tests cover that gap and need no database to do it.

func TestEveryMilestoneConstantIsInTheList(t *testing.T) {
	declared := []delivery.Milestone{
		delivery.MilestoneDriverAssigned,
		delivery.MilestoneEnRouteToPickup,
		delivery.MilestonePickedUp,
		delivery.MilestoneInTransit,
		delivery.MilestoneDelivered,
	}
	if len(declared) != len(delivery.Milestones) {
		t.Errorf("%d constants and %d in delivery.Milestones", len(declared), len(delivery.Milestones))
	}
	for _, m := range declared {
		if !m.Valid() {
			t.Errorf("%q is a declared milestone that delivery.Milestones omits, so Valid "+
				"refuses a value this package defines", m)
		}
	}

	declaredActors := []delivery.ActorType{
		delivery.ActorProvider,
		delivery.ActorDriver,
		delivery.ActorAdmin,
		delivery.ActorSystem,
	}
	if len(declaredActors) != len(delivery.ActorTypes) {
		t.Errorf("%d actor constants and %d in delivery.ActorTypes",
			len(declaredActors), len(delivery.ActorTypes))
	}
	for _, a := range declaredActors {
		if !a.Valid() {
			t.Errorf("%q is a declared actor that delivery.ActorTypes omits", a)
		}
	}
}

// TestWhatIsNotAMilestone names the values that look like they belong and do not.
//
// Four of the five milestones are also job statuses with identical strings, which makes the two
// lists easy to conflate — and Docs/02 §2 is explicit that they are not the same list. 'Completed'
// is the sharpest case: it is a status a job reaches by seventy-two hours passing (Docs/02 §6.1),
// so there is nobody to record it.
func TestWhatIsNotAMilestone(t *testing.T) {
	for _, notAMilestone := range []delivery.Milestone{
		"Draft", "Open", "Negotiating", "Awarded", "Completed", "Cancelled", "Disputed",
		"delivered", "picked_up", "",
	} {
		if notAMilestone.Valid() {
			t.Errorf("%q is accepted as a milestone; Docs/01 §4.4 numbers five and this is "+
				"not one of them", notAMilestone)
		}
	}

	for _, notAnActor := range []delivery.ActorType{"customer", "Driver", "robot", ""} {
		if notAnActor.Valid() {
			t.Errorf("%q is accepted as a delivery actor; Docs/02 §3 permits the awarded "+
				"provider, their driver, an administrator and the platform", notAnActor)
		}
	}
}
