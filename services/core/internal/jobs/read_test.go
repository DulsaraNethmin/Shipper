package jobs

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/DulsaraNethmin/Shipper/services/core/internal/testsupport/pgtest"
)

// SHIP-65 against a real PostgreSQL.

// TestJobReturnsTheWholeJobToItsOwner is SHIP-65's acceptance criterion, minus the budget column,
// which does not exist: SHIP-67 brings it together with the test proving it cannot reach a
// provider (Docs/11 §3, §8).
//
// "In full" is checked by round-tripping a job with every field populated rather than by naming
// three of them, because the failure this guards against is a reader that quietly drops a column —
// which a spot check of the fields somebody happened to think of would pass.
func TestJobReturnsTheWholeJobToItsOwner(t *testing.T) {
	pool := pgtest.DB(t)
	service := newDraftService(t, &fakeGeocoder{})

	customer := newCustomer(t, pool, "read-owner@example.com", "+61400000650")

	created, err := service.CreateDraft(t.Context(), pool, customer, DraftFields{
		Pickup:             sydney(),
		Dropoff:            melbourne(),
		GoodsDescription:   text("Two-seater sofa, wrapped"),
		LengthCm:           number(190),
		WidthCm:            number(90),
		HeightCm:           number(85),
		WeightKg:           decimal(45.5),
		VehicleRequirement: text("Ute with a tailgate lifter"),
		HandlingNotes:      text("Second-floor walk-up, no lift."),
		PickupWindow:       &TimeWindow{Start: testInstant, End: testInstant.Add(24 * time.Hour)},
	})
	if err != nil {
		t.Fatalf("creating the job: %v", err)
	}

	read, err := service.Job(t.Context(), pool, customer, created.ID)
	if err != nil {
		t.Fatalf("reading the job: %v", err)
	}

	// Compared with what the store returns for the same row, so a column dropped from the
	// read path fails here rather than being invisible until a client notices.
	if want := reread(t, pool, created.ID); read != want {
		t.Errorf("the job read back differs from the stored row:\n got  %+v\n want %+v", read, want)
	}
	if read.Status != StatusDraft || read.CustomerID != customer {
		t.Errorf("job = %s owned by %s, want a Draft owned by %s", read.Status, read.CustomerID, customer)
	}
	if !read.Pickup.Resolved || read.Pickup.Latitude == 0 {
		t.Errorf("the pickup came back unresolved: %+v", read.Pickup)
	}
}

// TestJobRefusesAStrangerAndKeepsTheTwoRefusalsApart is the substantive half of SHIP-65: "for the
// owning customer only".
//
// The two sentinels are one 404 on the wire and must stay distinguishable in Go, so a test
// asserting "the stranger was refused" fails if the job silently stopped existing instead.
func TestJobRefusesAStrangerAndKeepsTheTwoRefusalsApart(t *testing.T) {
	pool := pgtest.DB(t)
	service := newDraftService(t, &fakeGeocoder{})

	owner := newCustomer(t, pool, "read-owner-2@example.com", "+61400000651")
	stranger := newCustomer(t, pool, "read-stranger@example.com", "+61400000652")
	job := newDraft(t, pool, owner)

	if _, err := service.Job(t.Context(), pool, stranger, job); !errors.Is(err, ErrNotJobOwner) {
		t.Errorf("a stranger's read = %v, want ErrNotJobOwner", err)
	}

	missing, _ := uuid.NewV7()
	if _, err := service.Job(t.Context(), pool, stranger, missing); !errors.Is(err, ErrJobNotFound) {
		t.Errorf("reading a job that does not exist = %v, want ErrJobNotFound", err)
	}
}

// A published job stays readable by its owner. Editing stops at Draft (SHIP-62); reading does not,
// and a customer who cannot see their own Open job cannot be shown its bids.
func TestJobIsReadableInEveryStatus(t *testing.T) {
	pool := pgtest.DB(t)
	service := newDraftService(t, &fakeGeocoder{})

	customer := newCustomer(t, pool, "read-published@example.com", "+61400000653")
	job := newDraft(t, pool, customer)
	publish(t, pool, job, customer)

	read, err := service.Job(t.Context(), pool, customer, job)
	if err != nil {
		t.Fatalf("reading a published job: %v", err)
	}
	if read.Status != StatusOpen {
		t.Errorf("status = %s, want Open", read.Status)
	}
}

// The read takes no row lock, and that is a property worth pinning rather than trusting to the
// comment on postgresStore.job.
//
// A GET that took FOR UPDATE would serialise every reader of a job behind whatever is writing it.
// Here the writer holds the row for the length of an open transaction and the reader still
// returns — which it could not do if it were waiting on the same lock.
func TestReadingAJobDoesNotWaitOnAWriter(t *testing.T) {
	pool := pgtest.DB(t)
	service := newDraftService(t, &fakeGeocoder{})

	customer := newCustomer(t, pool, "read-unlocked@example.com", "+61400000654")
	job := newDraft(t, pool, customer)

	holder, err := pool.Begin(t.Context())
	if err != nil {
		t.Fatalf("beginning a transaction: %v", err)
	}
	defer func() { _ = holder.Rollback(t.Context()) }()

	if _, err := service.store.lockJob(t.Context(), holder, job); err != nil {
		t.Fatalf("locking the job: %v", err)
	}

	done := make(chan error, 1)
	go func() {
		_, err := service.Job(context.Background(), pool, customer, job)
		done <- err
	}()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("reading a locked job: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the read blocked on the writer's row lock; a GET must not take FOR UPDATE")
	}
}
