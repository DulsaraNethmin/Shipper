package jobs

import (
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
)

// These tests need no database. What they check is that the Go transcription of Docs/02 §2 says
// what the document says — the database's part of the guard is checked in migrations/jobs_test.go,
// against a real PostgreSQL.

// TestEveryStatusHasATransitionRule keeps the table total.
//
// A status missing from the map is not a compile error and not a runtime error either: Permitted
// returns false for an absent key, so the effect is a job that silently cannot move. That is the
// failure this test exists to make loud.
func TestEveryStatusHasATransitionRule(t *testing.T) {
	for _, status := range Statuses {
		if _, listed := permitted[status]; !listed {
			t.Errorf("%q has no entry in the transition table, so nothing can ever move out "+
				"of it; a terminal status is written as an explicit empty list", status)
		}
	}
	if len(permitted) != len(Statuses) {
		t.Errorf("the transition table has %d statuses and Docs/02 §1 has %d",
			len(permitted), len(Statuses))
	}

	for from, targets := range permitted {
		if !from.Valid() {
			t.Errorf("the transition table has a source status %q that is not one of the twelve", from)
		}
		seen := map[Status]bool{}
		for _, to := range targets {
			if !to.Valid() {
				t.Errorf("%q may move to %q, which is not one of the twelve", from, to)
			}
			if from == to {
				t.Errorf("%q lists itself as a transition; ck_job_status_history_moves "+
					"would refuse to record it", from)
			}
			if seen[to] {
				t.Errorf("%q lists %q twice", from, to)
			}
			seen[to] = true
		}
	}
}

// TestPermittedIsDocs02Section2 walks the document's table.
//
// Every row of Docs/02 §2 is below, and so are the moves the document is explicit about *not*
// permitting. The second half is the half worth having: a transition table with something extra
// in it passes every test that only checks the rows that should be there.
func TestPermittedIsDocs02Section2(t *testing.T) {
	allowed := []struct{ from, to Status }{
		{StatusDraft, StatusOpen},              // required details valid, customer verified
		{StatusDraft, StatusCancelled},         // customer abandons the draft
		{StatusOpen, StatusNegotiating},        // first bid or counter-offer
		{StatusNegotiating, StatusOpen},        // every active bid gone
		{StatusOpen, StatusAwarded},            // customer accepts a valid bid
		{StatusNegotiating, StatusAwarded},     // …from either presentation state
		{StatusOpen, StatusCancelled},          // cancelled before award, or expired (§6.3)
		{StatusNegotiating, StatusCancelled},   //
		{StatusAwarded, StatusDriverAssigned},  // provider nominates a driver
		{StatusAwarded, StatusOpen},            // provider cancels before pickup (§6.2)
		{StatusDriverAssigned, StatusOpen},     //
		{StatusAwarded, StatusEnRouteToPickup}, // Driver assigned is skippable
		{StatusDriverAssigned, StatusEnRouteToPickup},
		{StatusEnRouteToPickup, StatusPickedUp},
		{StatusPickedUp, StatusInTransit},       // may be a presentation change
		{StatusInTransit, StatusDelivered},      // proof, or a reasoned exception
		{StatusDelivered, StatusCompleted},      // confirmed, or 72 hours (§6.1)
		{StatusAwarded, StatusDisputed},         // "Awarded through Delivered" — all six
		{StatusDriverAssigned, StatusDisputed},  //
		{StatusEnRouteToPickup, StatusDisputed}, //
		{StatusPickedUp, StatusDisputed},        //
		{StatusInTransit, StatusDisputed},       //
		{StatusDelivered, StatusDisputed},       //
		{StatusDisputed, StatusCompleted},       // admin resolves, delivery accepted
		{StatusDisputed, StatusCancelled},       // admin resolves as cancelled or failed
	}

	for _, move := range allowed {
		if !Permitted(move.from, move.to) {
			t.Errorf("Docs/02 §2 permits %s → %s and the guard does not", move.from, move.to)
		}
	}

	// 25 rows, counted. The number is here so that a transition added without a document
	// change fails: the list above would still pass, because everything in it is still true.
	total := 0
	for _, targets := range permitted {
		total += len(targets)
	}
	if total != len(allowed) {
		t.Errorf("the transition table has %d moves and Docs/02 §2 has %d; a move added to "+
			"one and not the other is a lifecycle only half the platform agrees on",
			total, len(allowed))
	}

	refused := []struct {
		from, to Status
		why      string
	}{
		{StatusPickedUp, StatusCancelled,
			"Docs/02 §6.2: after pickup the goods are in somebody's vehicle, which is a support case"},
		{StatusInTransit, StatusCancelled, "Docs/02 §6.2"},
		{StatusDelivered, StatusCancelled, "Docs/02 §6.2; the route out is Disputed"},
		{StatusCompleted, StatusDisputed,
			"Completed is terminal; a dispute is raised before completion, not after"},
		{StatusCancelled, StatusOpen, "Cancelled is terminal; re-listing is a new job"},
		{StatusDraft, StatusAwarded, "a job cannot be awarded before it is published"},
		{StatusOpen, StatusDelivered, "the five milestones are not skippable as a group"},
		{StatusDraft, StatusDisputed, "there is nothing to dispute before award"},
		{StatusOpen, StatusDisputed, "Docs/02 §2 disputes run from Awarded through Delivered"},
	}

	for _, move := range refused {
		if Permitted(move.from, move.to) {
			t.Errorf("the guard permits %s → %s and Docs/02 does not: %s",
				move.from, move.to, move.why)
		}
	}
}

// TestTerminalStatusesGoNowhere is the property SHIP-119 and SHIP-68 both depend on: once a job
// is Completed or Cancelled, no scheduled task can move it again.
func TestTerminalStatusesGoNowhere(t *testing.T) {
	for _, terminal := range []Status{StatusCompleted, StatusCancelled} {
		for _, to := range Statuses {
			if Permitted(terminal, to) {
				t.Errorf("%s is terminal and the guard permits %s → %s", terminal, terminal, to)
			}
		}
	}
}

// TestStatusValidity checks the twelve and rejects the plausible near-misses. The values are
// stored strings (Docs/10 §3.4), so case and spacing are load-bearing rather than cosmetic.
func TestStatusValidity(t *testing.T) {
	for _, status := range Statuses {
		if !status.Valid() {
			t.Errorf("%q is in Statuses and reports itself invalid", status)
		}
	}
	for _, wrong := range []Status{"", "draft", "DRAFT", "driver_assigned", "Driver Assigned", "En Route To Pickup", "Pending"} {
		if wrong.Valid() {
			t.Errorf("%q was accepted as a job status", wrong)
		}
	}
}

func TestActorTypeValidity(t *testing.T) {
	for _, actor := range ActorTypes {
		if !actor.Valid() {
			t.Errorf("%q is in ActorTypes and reports itself invalid", actor)
		}
	}
	for _, wrong := range []ActorType{"", "user", "platform", "robot", "Customer"} {
		if wrong.Valid() {
			t.Errorf("%q was accepted as an actor kind", wrong)
		}
	}
}

// TestMoveValidation checks what a caller is told before anything is locked or written.
//
// Every rule here is also a constraint in 000401. Both exist deliberately: the constraint is what
// makes the guarantee true of the database, and this is what makes the failure legible — a caller
// that forgot an administrator's reason should be told that, not handed a constraint name.
func TestMoveValidation(t *testing.T) {
	job, _ := uuid.NewV7()
	someone, _ := uuid.NewV7()

	cases := map[string]struct {
		move Move
		want error
	}{
		"no job": {
			Move{To: StatusOpen, Actor: User(ActorCustomer, someone)},
			ErrJobNotFound,
		},
		"unknown status": {
			Move{JobID: job, To: "Pending", Actor: User(ActorCustomer, someone)},
			ErrInvalidStatus,
		},
		"unknown actor kind": {
			Move{JobID: job, To: StatusOpen, Actor: Actor{Type: "robot", ID: someone}},
			ErrInvalidActor,
		},
		"an account-backed actor with no account": {
			Move{JobID: job, To: StatusOpen, Actor: Actor{Type: ActorCustomer}},
			ErrInvalidActor,
		},
		"the platform claiming an account": {
			Move{JobID: job, To: StatusOpen, Actor: Actor{Type: ActorSystem, ID: someone}},
			ErrInvalidActor,
		},
		"an administrator with no reason": {
			Move{JobID: job, To: StatusCancelled, Actor: User(ActorAdmin, someone)},
			ErrReasonRequired,
		},
	}

	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			if err := c.move.validate(); !errors.Is(err, c.want) {
				t.Errorf("validate() = %v, want %v", err, c.want)
			}
		})
	}

	valid := map[string]Move{
		"a customer publishing":     {JobID: job, To: StatusOpen, Actor: User(ActorCustomer, someone)},
		"the platform expiring":     {JobID: job, To: StatusCancelled, Actor: System()},
		"an administrator with why": {JobID: job, To: StatusCancelled, Actor: User(ActorAdmin, someone), Reason: "prohibited goods"},
		"a driver, recorded late":   {JobID: job, To: StatusPickedUp, Actor: User(ActorDriver, someone), RecordedAt: time.Now().Add(-time.Hour)},
	}

	for name, move := range valid {
		t.Run(name, func(t *testing.T) {
			if err := move.validate(); err != nil {
				t.Errorf("validate() = %v, want nil", err)
			}
		})
	}
}
