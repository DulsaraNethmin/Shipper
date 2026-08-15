package jobs

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/DulsaraNethmin/Shipper/services/core/internal/clock"
	"github.com/DulsaraNethmin/Shipper/services/core/internal/db"
	"github.com/DulsaraNethmin/Shipper/services/core/internal/testsupport/pgtest"
)

// SHIP-70 — "a customer can extend an expiring job in one call".
//
// Against a real PostgreSQL, because the two things most worth proving here are the database's:
// that an extension passes 000402's status guard without a history row — since it is not a
// transition — and that 000407's trigger clears the warning mark on the way through. A mock would
// accept both whether or not the triggers existed.
//
// Every test runs the service over a stopped clock, and the job's deadline is placed relative to
// the same instant. The alternative is a test whose result depends on how far testInstant happens
// to be from the day it is run, which is a test that starts failing on a date nobody chose.

// extend runs an extension in a transaction, which is what Service.Extend requires.
func extend(t *testing.T, pool *pgxpool.Pool, svc *Service, customer, job uuid.UUID) (Job, error) {
	t.Helper()

	var extended Job
	err := db.InTx(t.Context(), pool, func(ctx context.Context, r db.Runner) error {
		var err error
		extended, err = svc.Extend(ctx, r, customer, job)
		return err
	})
	return extended, err
}

// expiringJob is an Open job whose deadline is `in` from the service's stopped clock, with the
// pickup window it was created with.
//
// The deadline is written explicitly rather than left to 000406, because the trigger computes it
// from the database's now() while the service reads a fixed clock — and a test that let the two
// drift apart would be measuring the gap between them rather than the rule.
func expiringJob(t *testing.T, pool *pgxpool.Pool, customer uuid.UUID,
	in time.Duration, pickupEnd time.Time) uuid.UUID {
	t.Helper()

	fields := DraftFields{}
	if !pickupEnd.IsZero() {
		fields.PickupWindow = &TimeWindow{End: pickupEnd}
	}

	created, err := newDraftService(t, &fakeGeocoder{}).CreateDraft(t.Context(), pool, customer, fields)
	if err != nil {
		t.Fatalf("creating the job: %v", err)
	}

	publish(t, pool, created.ID, customer)
	setDeadline(t, pool, created.ID, testInstant.Add(in))
	return created.ID
}

// TestExtendGivesTheJobAnotherFourteenDaysFromNow is SHIP-70's *Done when*, in the case
// Docs/02 §6.3 calls the backstop: a job with no pickup date, ending only because the listing has
// aged.
//
// **And it is not a status transition.** The job is Open before and after, and job_status_history
// gains nothing — asserted rather than assumed, because routing an extension through
// Service.Transition would have been the obvious way to write it and would have put an
// `Open -> Open` move in a customer's timeline that Docs/02 §2 has no row for.
func TestExtendGivesTheJobAnotherFourteenDaysFromNow(t *testing.T) {
	pool := pgtest.DB(t)
	sink := &recordingSink{}
	service := newTestService(sink)

	customer := newCustomer(t, pool, "extend-backstop@example.com", "+61400000700")
	job := expiringJob(t, pool, customer, 24*time.Hour, time.Time{})

	before := len(historyOf(t, pool, job))

	extended, err := extend(t, pool, service, customer, job)
	if err != nil {
		t.Fatalf("extending the job: %v", err)
	}

	want := testInstant.Add(ExtensionPeriod)
	if !extended.ExpiresAt.Equal(want) {
		t.Errorf("the extended deadline is %s, want %s — fourteen days from the moment the "+
			"customer acted", extended.ExpiresAt, want)
	}

	at := deadlineOf(t, pool, job)
	if at == nil || !at.Equal(want) {
		t.Errorf("the stored deadline is %v, want %s", at, want)
	}
	if extended.Status != StatusOpen || statusOf(t, pool, job) != StatusOpen {
		t.Errorf("the job is %s after an extension, want Open", extended.Status)
	}
	if after := len(historyOf(t, pool, job)); after != before {
		t.Errorf("the extension wrote %d history rows; an extension moves no job", after-before)
	}

	if len(sink.emitted) != 1 || sink.emitted[0].Type != EventExpiryExtended {
		t.Fatalf("emitted %d events (%v), want one %s", len(sink.emitted), sink.emitted, EventExpiryExtended)
	}

	var payload expiryExtended
	if err := json.Unmarshal(sink.emitted[0].Payload, &payload); err != nil {
		t.Fatalf("the payload is not the shape this domain wrote: %v", err)
	}
	switch {
	case payload.JobID != job.String():
		t.Errorf("the event names job %s, want %s", payload.JobID, job)
	case payload.CustomerID != customer.String():
		t.Errorf("the event names customer %s, want %s", payload.CustomerID, customer)
	case !payload.PreviousExpiresAt.Equal(testInstant.Add(24 * time.Hour)):
		// Without the deadline it moved from, a consumer holding SHIP-69's warning cannot
		// tell whether this extension is what voided it.
		t.Errorf("the event moved from %s, want %s", payload.PreviousExpiresAt, testInstant.Add(24*time.Hour))
	case !payload.ExpiresAt.Equal(want):
		t.Errorf("the event moved to %s, want %s", payload.ExpiresAt, want)
	}
}

// TestAnExtensionIsBoundedByThePickupWindow is Docs/02 §6.3's operative rule surviving the
// customer's escape hatch.
//
// "A job whose pickup window has gone is dead regardless of how recently it was posted." An
// extension that ignored the pickup date would put a listing in front of providers advertising a
// collection date that had passed — which wastes a bid rather than a glance, and is worse for the
// marketplace than the stale listing §6.3 is about.
//
// The useful half is here too: the job gains the time between its current deadline and its own
// pickup date, which is exactly what a customer whose fourteen days ran out first is asking for.
func TestAnExtensionIsBoundedByThePickupWindow(t *testing.T) {
	pool := pgtest.DB(t)
	service := newTestService(&recordingSink{})

	customer := newCustomer(t, pool, "extend-pickup@example.com", "+61400000701")

	// The pickup window closes in eight days; the listing would have ended in one.
	pickupEnd := testInstant.Add(8 * 24 * time.Hour).Truncate(time.Millisecond)
	job := expiringJob(t, pool, customer, 24*time.Hour, pickupEnd)

	extended, err := extend(t, pool, service, customer, job)
	if err != nil {
		t.Fatalf("extending the job: %v", err)
	}

	if !extended.ExpiresAt.Equal(pickupEnd) {
		t.Errorf("the extended deadline is %s, want the pickup window's end %s — fourteen days "+
			"would have carried the job past the date its goods were to be collected",
			extended.ExpiresAt, pickupEnd)
	}
}

// TestExtendRefusesAJobItsPickupDateIsEnding is the refusal that follows from the rule above.
//
// A job already bounded by its pickup date has nothing an extension can give it, and saying so is
// more useful than a 200 that changed nothing: the customer's real problem is the date, and the
// message names it.
func TestExtendRefusesAJobItsPickupDateIsEnding(t *testing.T) {
	pool := pgtest.DB(t)
	sink := &recordingSink{}
	service := newTestService(sink)

	customer := newCustomer(t, pool, "extend-bound@example.com", "+61400000702")

	pickupEnd := testInstant.Add(30 * time.Hour).Truncate(time.Millisecond)
	job := expiringJob(t, pool, customer, 30*time.Hour, pickupEnd)

	if _, err := extend(t, pool, service, customer, job); !errors.Is(err, ErrExpiryBoundByPickup) {
		t.Errorf("extending a pickup-bound job = %v, want ErrExpiryBoundByPickup", err)
	}

	at := deadlineOf(t, pool, job)
	if at == nil || !at.Equal(pickupEnd) {
		t.Errorf("the refused extension moved the deadline to %v", at)
	}
	if len(sink.emitted) != 0 {
		t.Errorf("the refused extension emitted %d events", len(sink.emitted))
	}
}

// TestExtendRefusesAJobThatIsNotOpen leans on the fact that only a job being offered expires.
//
// ExpiryClaim filters on [LiveStatuses] and Docs/02 §2's "job expires unclaimed" row is
// `Open / Negotiating → Cancelled`, so extending anything else moves a column nothing reads — and
// tells the customer their job was saved when it was never at risk. None of the three fixtures
// below is in that set, which is what SHIP-70a's widening had to leave true.
func TestExtendRefusesAJobThatIsNotOpen(t *testing.T) {
	pool := pgtest.DB(t)
	service := newTestService(&recordingSink{})

	customer := newCustomer(t, pool, "extend-status@example.com", "+61400000703")

	draft := newDraft(t, pool, customer)

	awarded := newDraft(t, pool, customer)
	publish(t, pool, awarded, customer)
	move(t, pool, awarded, StatusAwarded, User(ActorCustomer, customer))

	cancelled := newDraft(t, pool, customer)
	move(t, pool, cancelled, StatusCancelled, User(ActorCustomer, customer))

	for name, job := range map[string]uuid.UUID{
		"a draft":         draft,
		"an awarded job":  awarded,
		"a cancelled job": cancelled,
	} {
		if _, err := extend(t, pool, service, customer, job); !errors.Is(err, ErrJobNotExtendable) {
			t.Errorf("extending %s = %v, want ErrJobNotExtendable", name, err)
		}
	}
}

// TestExtendKeepsANegotiatingJobAlive is the other half of SHIP-70a, and it is the half that would
// otherwise have been left broken by the first.
//
// Docs/02 §6.3 is one mechanism in two sentences: "the customer is warned 48 hours before expiry
// **and can extend in one action**". The moment the warning sweep can reach a Negotiating job — as
// SHIP-70a makes it — an extend endpoint still filtering on Open alone answers 422 to the one
// customer the warning was for, on the one kind of job somebody has actually bid on.
//
// The deadline is asserted to have *moved*, not merely to have been accepted, because refusing
// quietly and accepting quietly look identical from a status code.
func TestExtendKeepsANegotiatingJobAlive(t *testing.T) {
	pool := pgtest.DB(t)
	service := newTestService(&recordingSink{})

	customer := newCustomer(t, pool, "extend-negotiating@example.com", "+61400000704")
	job := newDraft(t, pool, customer)
	publish(t, pool, job, customer)
	move(t, pool, job, StatusNegotiating, User(ActorCustomer, customer))

	before := deadlineOf(t, pool, job)
	if before == nil {
		t.Fatal("the Negotiating job has no deadline to extend")
	}

	// Pulled back inside the warning window, which is the state a customer acts from.
	setDeadline(t, pool, job, testInstant.Add(24*time.Hour))

	extended, err := extend(t, pool, service, customer, job)
	if err != nil {
		t.Fatalf("extending a job somebody has bid on = %v, want it kept alive", err)
	}
	if extended.Status != StatusNegotiating {
		t.Errorf("the extended job is %s, want Negotiating — an extension is not a transition",
			extended.Status)
	}

	after := deadlineOf(t, pool, job)
	if after == nil || !after.After(testInstant.Add(24*time.Hour)) {
		t.Errorf("the deadline is %v after extending, want it moved past the window it was in", after)
	}
}

// TestExtendRejectsExtensionsByNonOwners keeps the ownership check ahead of the status check.
//
// Both are one 404 on the wire, so a stranger who could tell "not extendable" from "no such job"
// would learn that the job exists and roughly what state it is in.
func TestExtendRejectsExtensionsByNonOwners(t *testing.T) {
	pool := pgtest.DB(t)
	service := newTestService(&recordingSink{})

	owner := newCustomer(t, pool, "extend-owner@example.com", "+61400000704")
	stranger := newCustomer(t, pool, "extend-stranger@example.com", "+61400000705")

	job := expiringJob(t, pool, owner, 24*time.Hour, time.Time{})

	if _, err := extend(t, pool, service, stranger, job); !errors.Is(err, ErrNotJobOwner) {
		t.Errorf("a stranger's extension = %v, want ErrNotJobOwner", err)
	}
	if at := deadlineOf(t, pool, job); at == nil || !at.Equal(testInstant.Add(24*time.Hour)) {
		t.Errorf("the refused extension moved the deadline to %v", at)
	}

	// A draft belonging to somebody else is refused for *ownership* rather than for status —
	// the order matters, because the second answer would confirm the job exists.
	stranged := newDraft(t, pool, owner)
	if _, err := extend(t, pool, service, stranger, stranged); !errors.Is(err, ErrNotJobOwner) {
		t.Errorf("a stranger extending somebody else's draft = %v, want ErrNotJobOwner", err)
	}
}

// TestExtendRefusesToRunOutsideATransaction keeps the read, the write and the event atomic.
func TestExtendRefusesToRunOutsideATransaction(t *testing.T) {
	pool := pgtest.DB(t)
	service := newTestService(&recordingSink{})

	customer := newCustomer(t, pool, "extend-notx@example.com", "+61400000706")
	job := expiringJob(t, pool, customer, 24*time.Hour, time.Time{})

	if _, err := service.Extend(t.Context(), pool, customer, job); !errors.Is(err, ErrNotInTransaction) {
		t.Errorf("extending through the pool = %v, want ErrNotInTransaction", err)
	}
}

// TestExtendingRearmsTheExpiryWarning is the interlock between the two tickets, and the reason
// 000407 puts the clearing in a trigger rather than in this endpoint.
//
// A customer warned forty-eight hours out extends, and must be warned again forty-eight hours
// before the deadline they now have. Without it SHIP-70 would quietly switch SHIP-69 off for
// exactly the jobs that had used it — the failure being a notification that never arrives, which
// nothing reports.
func TestExtendingRearmsTheExpiryWarning(t *testing.T) {
	pool := pgtest.DB(t)
	service := newTestService(&recordingSink{})

	customer := newCustomer(t, pool, "extend-rearm@example.com", "+61400000707")
	job := expiringJob(t, pool, customer, 24*time.Hour, time.Time{})

	if _, err := warn(t, pool, service, job); err != nil {
		t.Fatalf("the first warning: %v", err)
	}
	if warnedAt(t, pool, job) == nil {
		t.Fatal("the job was not marked warned, so this test proves nothing")
	}

	if _, err := extend(t, pool, service, customer, job); err != nil {
		t.Fatalf("extending the job: %v", err)
	}

	if at := warnedAt(t, pool, job); at != nil {
		t.Fatalf("the job is still marked warned at %s after being extended", at)
	}

	// And the claim finds it again, forty-eight hours before the new deadline rather than the
	// old one.
	approaching := testInstant.Add(ExtensionPeriod - 24*time.Hour)
	if claimed := claimWarnings(t, pool, approaching); len(claimed) != 1 || claimed[0] != job {
		t.Errorf("the claim took %v approaching the extended deadline, want [%s]", claimed, job)
	}
}

// TestExtendedDeadlineIsTheEarlierOfTheTwo holds the arithmetic to Docs/02 §6.3 directly.
//
// A table rather than four database tests, because this is the one piece of the ticket that is pure
// arithmetic — including the case a running service makes awkward to reach, where the pickup window
// has already closed and the answer is a deadline in the past that Service.Extend must refuse
// rather than write.
func TestExtendedDeadlineIsTheEarlierOfTheTwo(t *testing.T) {
	at := testInstant

	cases := map[string]struct {
		pickupEnd time.Time
		want      time.Time
	}{
		"no pickup window at all":       {time.Time{}, at.Add(ExtensionPeriod)},
		"a pickup window beyond it":     {at.Add(30 * 24 * time.Hour), at.Add(ExtensionPeriod)},
		"a pickup window inside it":     {at.Add(3 * 24 * time.Hour), at.Add(3 * 24 * time.Hour)},
		"a pickup window already past":  {at.Add(-time.Hour), at.Add(-time.Hour)},
		"a pickup window exactly on it": {at.Add(ExtensionPeriod), at.Add(ExtensionPeriod)},
	}

	for name, c := range cases {
		if got := extendedDeadline(at, c.pickupEnd); !got.Equal(c.want) {
			t.Errorf("%s: extendedDeadline = %s, want %s", name, got, c.want)
		}
	}
}

// TestExtendUsesTheServiceClockRatherThanTheDatabases keeps the injected clock load-bearing
// (Docs/10 §6.3).
//
// The whole endpoint is arithmetic on "now", and a now() taken from PostgreSQL would make the
// extension untestable without waiting — the same argument the expiry claim makes for taking its
// instant as a parameter.
func TestExtendUsesTheServiceClockRatherThanTheDatabases(t *testing.T) {
	pool := pgtest.DB(t)

	customer := newCustomer(t, pool, "extend-clock@example.com", "+61400000708")
	job := expiringJob(t, pool, customer, 24*time.Hour, time.Time{})

	// A clock a long way from both the database's and testInstant's, so an implementation
	// reading either would produce a deadline this cannot mistake for the right one.
	elsewhen := testInstant.Add(90 * 24 * time.Hour)
	service := NewService(&recordingSink{}, clock.NewFixed(elsewhen), nil)

	extended, err := extend(t, pool, service, customer, job)
	if err != nil {
		t.Fatalf("extending the job: %v", err)
	}
	if want := elsewhen.Add(ExtensionPeriod); !extended.ExpiresAt.Equal(want) {
		t.Errorf("the extended deadline is %s, want %s — computed from the injected clock",
			extended.ExpiresAt, want)
	}
}
