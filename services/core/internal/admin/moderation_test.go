package admin

import (
	"context"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/DulsaraNethmin/Shipper/services/core/internal/db"
	"github.com/DulsaraNethmin/Shipper/services/core/internal/testsupport/pgtest"
)

// SHIP-117 against a real PostgreSQL.
//
// The *Done when* is that an exception-completed job **enters the moderation queue**, and the queue
// is a query rather than a table of flags — so the only way to demonstrate it is to record the
// evidence the way `delivery` records it and then ask. A mocked source would prove that a slice
// round-trips.
//
// [testExceptionQueue] is this package's copy of the adapter cmd/api holds, for the reason
// [testParties] is: `admin` may not import `delivery` or `jobs`, and package main has no database a
// Go test can reach. The production adapter is exercised end to end against the built binary by
// scripts/verify/90-admin.sh.
//
// newAccount, newDraft, moveJob and acceptBid come from service_test.go.

// testExceptionQueue is admin.ExceptionQueue over delivery's evidence tables — the same statement
// cmd/api/routes_admin.go makes.
type testExceptionQueue struct{}

func (testExceptionQueue) ExceptionsAwaitingReview(
	ctx context.Context,
	r db.Runner,
	q QueueQuery,
) ([]ExceptionEntry, error) {
	const query = `
		SELECT p.id, p.job_id, m.milestone, p.exception_reason,
		       coalesce(m.reason, ''), p.created_at, j.status
		FROM proofs p
		JOIN milestones m ON m.id = p.milestone_id
		JOIN jobs j       ON j.id = p.job_id
		WHERE p.exception_reason IS NOT NULL
		  AND ($1::timestamptz IS NULL OR (p.created_at, p.id) > ($1, $2))
		ORDER BY p.created_at, p.id
		LIMIT $3`

	var after, afterID any
	if !q.After.Zero() {
		after, afterID = q.After.RecordedAt, q.After.ProofID
	}

	rows, err := r.Query(ctx, query, after, afterID, q.Limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []ExceptionEntry
	for rows.Next() {
		var e ExceptionEntry
		if err := rows.Scan(&e.ProofID, &e.JobID, &e.Milestone, &e.Reason,
			&e.Note, &e.RecordedAt, &e.JobStatus); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

// recordEvidence writes a milestone and the proof row standing behind it.
//
// `reason` empty means a photograph rather than an exception, which is the negative case: a delivery
// that was photographed must **not** be in this queue, and a test that only ever wrote exceptions
// would pass against a query with no `WHERE` clause at all.
func recordEvidence(
	t *testing.T,
	pool *pgxpool.Pool,
	jobID uuid.UUID,
	milestone, reason, note string,
	at time.Time,
) uuid.UUID {
	t.Helper()

	milestoneID, err := uuid.NewV7()
	if err != nil {
		t.Fatalf("generating a milestone id: %v", err)
	}
	proofID, err := uuid.NewV7()
	if err != nil {
		t.Fatalf("generating a proof id: %v", err)
	}

	var noteValue any
	if note != "" {
		noteValue = note
	}

	// One transaction, because 000605's constraint trigger is DEFERRABLE INITIALLY DEFERRED: a
	// 'Delivered' milestone is checked for evidence at COMMIT, and the evidence points at the
	// milestone and is therefore written second. Two auto-committed statements would fail on the
	// first — which is the invariant working, and not what these tests are about.
	if err := db.InTx(t.Context(), pool, func(ctx context.Context, r db.Runner) error {
		// server_recorded_at is deliberately not supplied: 000601's trigger refuses an INSERT
		// that names it, because it is the platform's clock. The instant these tests control
		// is proofs.created_at, which is the column the queue orders by — and that is not an
		// accident of the fixture, it is the reason the ordering was put there rather than on
		// the actor's clock.
		if _, err := r.Exec(ctx, `
			INSERT INTO milestones
				(id, job_id, milestone, actor_type, actor_id, reason, actor_recorded_at)
			VALUES ($1, $2, $3, 'driver', gen_random_uuid(), $4, $5)`,
			milestoneID, jobID, milestone, noteValue, at,
		); err != nil {
			return fmt.Errorf("recording a milestone: %w", err)
		}

		if reason == "" {
			// A photograph: all four object facts present, no reason. This is the negative
			// case, and a queue that returned it would be a queue of every delivery.
			if _, err := r.Exec(ctx, `
				INSERT INTO proofs
					(id, job_id, milestone_id, object_key, content_type, content_length,
					 etag, created_at)
				VALUES ($1, $2, $3, $4, 'image/jpeg', 12345, $5, $6)`,
				proofID, jobID, milestoneID,
				fmt.Sprintf("proof/%s.jpg", proofID), fmt.Sprintf("etag-%s", proofID), at,
			); err != nil {
				return fmt.Errorf("recording a photograph: %w", err)
			}
			return nil
		}

		if _, err := r.Exec(ctx, `
			INSERT INTO proofs (id, job_id, milestone_id, exception_reason, created_at)
			VALUES ($1, $2, $3, $4, $5)`,
			proofID, jobID, milestoneID, reason, at,
		); err != nil {
			return fmt.Errorf("recording an exception: %w", err)
		}
		return nil
	}); err != nil {
		t.Fatalf("recording evidence: %v", err)
	}

	return proofID
}

// structFieldNames is the exported field names of a struct, for the shape assertion below.
func structFieldNames(v any) []string {
	typ := reflect.TypeOf(v)
	out := make([]string, 0, typ.NumField())
	for i := range typ.NumField() {
		out = append(out, typ.Field(i).Name)
	}
	return out
}

// moderationFixture is a moderation service over a fresh database, and the delivered job the tests
// hang evidence off.
func moderationFixture(t *testing.T) (*Moderation, *pgxpool.Pool, uuid.UUID) {
	t.Helper()

	pool := pgtest.DB(t)

	customer := newAccount(t, pool, "queue-customer@example.com", "0419000117", "customer")
	provider := newAccount(t, pool, "queue-provider@example.com", "0419100117", "provider")
	jobID := deliveredJob(t, pool, customer, provider)

	moderation, err := NewModeration(testExceptionQueue{}, pool)
	if err != nil {
		t.Fatalf("building the moderation service: %v", err)
	}
	return moderation, pool, jobID
}

// TestAnExceptionCompletedJobEntersTheModerationQueue is SHIP-117's *Done when*, and the negative
// half is what makes it evidence.
//
// A delivery recorded with a reason appears; a delivery recorded with a photograph does not. Without
// the second, a query with no predicate at all would pass — and a moderation queue containing every
// delivery is the same as one containing none.
func TestAnExceptionCompletedJobEntersTheModerationQueue(t *testing.T) {
	moderation, pool, jobID := moderationFixture(t)

	at := time.Date(2026, 8, 14, 2, 15, 30, 0, time.UTC)
	exception := recordEvidence(t, pool, jobID, "Delivered", "recipient_objected",
		"The recipient asked me not to photograph their door.", at)

	// The same job, and a milestone that *was* photographed. Two rows on one job is exactly the
	// case a query keyed on the job rather than on the evidence would get wrong.
	photographed := recordEvidence(t, pool, jobID, "Picked up", "", "", at.Add(-time.Hour))

	entries, err := moderation.Exceptions(t.Context(), QueueQuery{Limit: 20})
	if err != nil {
		t.Fatalf("reading the queue: %v", err)
	}

	var found *ExceptionEntry
	for i := range entries {
		if entries[i].ProofID == photographed {
			t.Errorf("a delivery evidenced by a photograph is in the exception queue.\n" +
				"Docs/04 §5's fourth queue is failed proof of delivery, not every delivery.")
		}
		if entries[i].ProofID == exception {
			found = &entries[i]
		}
	}

	if found == nil {
		t.Fatalf("the exception-completed job is not in the moderation queue (%d entries)", len(entries))
	}
	if found.JobID != jobID {
		t.Errorf("the entry names job %s, want %s", found.JobID, jobID)
	}
	if found.Reason != "recipient_objected" {
		t.Errorf("reason = %q, want the reason that was recorded", found.Reason)
	}
	if found.Milestone != "Delivered" {
		t.Errorf("milestone = %q, want the claim the exception stands behind", found.Milestone)
	}
	if found.Note == "" {
		t.Error("the driver's own words are not carried, so a moderator triages from the reason alone")
	}
	if !found.RecordedAt.Equal(at) {
		t.Errorf("recorded_at = %s, want the platform's clock %s", found.RecordedAt, at)
	}
	if found.JobStatus == "" {
		t.Error("the entry does not say where the job is, so nothing can be triaged from it")
	}
}

// TestTheQueueCarriesNoPhotographAndNoMoney.
//
// Two invariants in one assertion, and both are about a shape rather than about a rule somebody
// applies. There is no object key because a proof row is a photograph *or* a reason and never both
// (`ck_proofs_photograph_or_exception`), and there is no budget because [ExceptionEntry] has nowhere
// to put one — a customer's budget is never exposed, and the strongest form of that is a struct that
// could not carry it.
//
// Written as a reflective check rather than by reading fields, because what is being asserted is
// that no *future* field appears: somebody adding `Budget` or `ObjectKey` to the entry for a screen
// that wanted it would fail here rather than in review.
func TestTheQueueCarriesNoPhotographAndNoMoney(t *testing.T) {
	forbidden := []string{"budget", "amount", "price", "money", "objectkey", "object_key", "url", "photo"}

	entry := ExceptionEntry{}
	for _, field := range structFieldNames(entry) {
		lowered := strings.ToLower(field)
		for _, bad := range forbidden {
			if lowered == bad {
				t.Errorf("ExceptionEntry has a %s field.\n"+
					"A queue entry carries what somebody triaging needs to decide whether to "+
					"open the job. A budget is never exposed (CLAUDE.md), and there is no "+
					"photograph to point at — this row exists because there was none.", field)
			}
		}
	}
}

// TestTheQueueIsOldestFirstAndPagesWithoutSkippingOrRepeating.
//
// Oldest first because Docs/04 §8 sets acknowledgement targets and the oldest entry is closest to
// breaching one. The paging half matters more: **two entries recorded at the same instant** are what
// a single-column cursor gets wrong, and it gets it wrong by hiding one — which is the failure a
// queue must not have.
func TestTheQueueIsOldestFirstAndPagesWithoutSkippingOrRepeating(t *testing.T) {
	moderation, pool, jobID := moderationFixture(t)

	base := time.Date(2026, 8, 14, 2, 0, 0, 0, time.UTC)
	written := map[uuid.UUID]bool{}

	// Three at distinct instants and two sharing one, so the tie-break is exercised rather than
	// assumed.
	for _, at := range []time.Time{base, base.Add(time.Minute), base.Add(2 * time.Minute), base.Add(3 * time.Minute), base.Add(3 * time.Minute)} {
		written[recordEvidence(t, pool, jobID, "Delivered", "camera_unavailable", "", at)] = true
	}

	seen := map[uuid.UUID]int{}
	var previous ExceptionEntry
	var cursor QueueCursor

	for page := 0; page < 10; page++ {
		entries, err := moderation.Exceptions(t.Context(), QueueQuery{Limit: 2, After: cursor})
		if err != nil {
			t.Fatalf("page %d: %v", page, err)
		}
		if len(entries) == 0 {
			break
		}

		for _, e := range entries {
			if !previous.RecordedAt.IsZero() {
				if e.RecordedAt.Before(previous.RecordedAt) {
					t.Errorf("the queue is not oldest first: %s came after %s",
						e.RecordedAt, previous.RecordedAt)
				}
			}
			previous = e
			seen[e.ProofID]++
		}

		last := entries[len(entries)-1]
		cursor = QueueCursor{RecordedAt: last.RecordedAt, ProofID: last.ProofID}
	}

	for id := range written {
		switch seen[id] {
		case 0:
			t.Errorf("%s was never returned — paging hid an entry, which is the one thing a "+
				"moderation queue must not do", id)
		case 1:
		default:
			t.Errorf("%s was returned %d times", id, seen[id])
		}
	}
}

// TestAQueueWithNoDatabaseSaysSoRatherThanReportingItIsEmpty.
//
// The worst available failure for a moderation queue is an empty one that is indistinguishable from
// a quiet week. An unreachable database answers 503 rather than an empty list, and the constructor
// refuses a nil source for the same reason.
func TestAQueueWithNoDatabaseSaysSoRatherThanReportingItIsEmpty(t *testing.T) {
	moderation, err := NewModeration(testExceptionQueue{}, nil)
	if err != nil {
		t.Fatalf("building a moderation service with no pool: %v", err)
	}

	entries, err := moderation.Exceptions(t.Context(), QueueQuery{Limit: 20})
	if err == nil {
		t.Fatalf("an unreachable database reported %d entries rather than failing", len(entries))
	}

	if _, err := NewModeration(nil, nil); err == nil {
		t.Error("a moderation service was built with no source of exceptions, so it would " +
			"report an empty queue for ever")
	}
}
