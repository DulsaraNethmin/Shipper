package admin

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/DulsaraNethmin/Shipper/services/core/internal/db"
	"github.com/DulsaraNethmin/Shipper/services/core/internal/jobs"
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

// testExceptionQueue is admin.ExceptionQueue over `delivery`s and `jobs`' rows — the same statement
// cmd/api/routes_admin.go makes.
//
// **A copy of a query is a thing that can drift, and this one is guarded rather than trusted.**
// TestTheTestQueueMatchesTheOneCmdApiRuns reads both files and compares the SQL, so a predicate
// changed in one and not the other fails here rather than passing in both packages and failing in
// `make verify` — which is where wave 8 found SHIP-117's only defect.
type testExceptionQueue struct{}

func (testExceptionQueue) ExceptionsAwaitingReview(
	ctx context.Context,
	r db.Runner,
	q QueueQuery,
	unsyncedThreshold time.Duration,
) ([]ExceptionEntry, error) {
	query := testExceptionQuery

	var after, ground, afterID any
	if !q.After.Zero() {
		after, ground, afterID = q.After.RecordedAt, q.After.Ground.String(), q.After.EntryID
	}

	rows, err := r.Query(ctx, query,
		unsyncedThreshold, q.Ground.String(), after, ground, afterID, q.Limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []ExceptionEntry
	for rows.Next() {
		var e ExceptionEntry
		if err := rows.Scan(&e.Ground, &e.EntryID, &e.JobID, &e.Milestone, &e.Reason,
			&e.Note, &e.RecordedAt, &e.JobStatus); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

// testExceptionQuery is the statement, verbatim from cmd/api/routes_admin.go.
//
// Kept as a package-level constant rather than inline so that the guard below can find it by name
// in the source of both files.
const testExceptionQuery = `
		WITH entries AS (
		    SELECT 'overdue_pickup'::text AS ground,
		           j.id AS entry_id, j.id AS job_id,
		           ''::text AS milestone, ''::text AS reason, ''::text AS note,
		           j.pickup_window_end AS recorded_at
		    FROM jobs j
		    WHERE j.pickup_window_end IS NOT NULL
		      AND j.pickup_window_end < now()
		      AND j.status IN ('Awarded', 'Driver assigned', 'En route to pickup')

		    UNION ALL

		    SELECT 'delayed_delivery'::text,
		           j.id, j.id, ''::text, ''::text, ''::text, j.dropoff_window_end
		    FROM jobs j
		    WHERE j.dropoff_window_end IS NOT NULL
		      AND j.dropoff_window_end < now()
		      AND j.status IN ('Awarded', 'Driver assigned', 'En route to pickup', 'Picked up', 'In transit')

		    UNION ALL

		    SELECT 'failed_proof'::text,
		           p.id, p.job_id, m.milestone, p.exception_reason, coalesce(m.reason, ''),
		           p.created_at
		    FROM proofs p
		    JOIN milestones m ON m.id = p.milestone_id
		    WHERE p.exception_reason IS NOT NULL

		    UNION ALL

		    SELECT 'unsynced_milestone'::text,
		           m.id, m.job_id, m.milestone, ''::text, coalesce(m.reason, ''),
		           m.server_recorded_at
		    FROM milestones m
		    WHERE m.server_recorded_at - m.actor_recorded_at >= $1
		)
		SELECT e.ground, e.entry_id, e.job_id, e.milestone, e.reason, e.note,
		       e.recorded_at, j.status
		FROM entries e
		JOIN jobs j ON j.id = e.job_id
		WHERE ($2 = '' OR e.ground = $2)
		  AND ($3::timestamptz IS NULL
		       OR (e.recorded_at, e.ground, e.entry_id) > ($3, $4, $5))
		ORDER BY e.recorded_at, e.ground, e.entry_id
		LIMIT $6`

// testUnsyncedThreshold is Docs/02 §3.1's twenty-four hours, as `delivery.UnsyncedAlertThreshold`
// holds it.
//
// **Written out rather than imported**, because `internal/admin` may not import `internal/delivery`
// — the boundary lint refuses it, which is the whole reason the threshold is a parameter. The two
// agreeing is demonstrated where they meet: cmd/api passes the real constant, and
// scripts/verify/90-admin.sh drives the built binary.
const testUnsyncedThreshold = 24 * time.Hour

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
				(id, job_id, milestone, actor_type, actor_id, reason, actor_recorded_at,
				 recipient_name, delivery_note)
			VALUES ($1, $2, $3, 'driver', gen_random_uuid(), $4, $5,
			        CASE WHEN $3 = 'Delivered' THEN 'R. Chen' END,
			        CASE WHEN $3 = 'Delivered' THEN 'Left with reception' END)`,
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

	moderation, err := NewModeration(testExceptionQueue{}, testUnsyncedThreshold, pool)
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
		if entries[i].EntryID == photographed {
			t.Errorf("a delivery evidenced by a photograph is in the exception queue.\n" +
				"Docs/04 §5's fourth queue is failed proof of delivery, not every delivery.")
		}
		if entries[i].EntryID == exception {
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
			seen[e.EntryID]++
		}

		last := entries[len(entries)-1]
		cursor = QueueCursor{
			RecordedAt: last.RecordedAt,
			Ground:     last.Ground,
			EntryID:    last.EntryID,
		}
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
	moderation, err := NewModeration(testExceptionQueue{}, testUnsyncedThreshold, nil)
	if err != nil {
		t.Fatalf("building a moderation service with no pool: %v", err)
	}

	entries, err := moderation.Exceptions(t.Context(), QueueQuery{Limit: 20})
	if err == nil {
		t.Fatalf("an unreachable database reported %d entries rather than failing", len(entries))
	}

	if _, err := NewModeration(nil, testUnsyncedThreshold, nil); err == nil {
		t.Error("a moderation service was built with no source of exceptions, so it would " +
			"report an empty queue for ever")
	}
}

// --- SHIP-157: the other three grounds, and the queue as one screen ------------------------------

// setWindows puts a job's pickup and drop-off windows where a test needs them.
//
// Both are nullable and both are set together, because a job with one and not the other is a real
// draft state and the two window grounds must each be exercisable without the other firing.
func setWindows(t *testing.T, pool *pgxpool.Pool, jobID uuid.UUID, pickupEnd, dropoffEnd *time.Time) {
	t.Helper()

	if _, err := pool.Exec(t.Context(),
		`UPDATE jobs SET pickup_window_end = $2, dropoff_window_end = $3 WHERE id = $1`,
		jobID, pickupEnd, dropoffEnd); err != nil {
		t.Fatalf("setting the windows on %s: %v", jobID, err)
	}
}

// recordLateMilestone writes a milestone whose actor clock is `behind` earlier than the platform's.
//
// `server_recorded_at` cannot be supplied — `000601`s trigger refuses an INSERT that names it,
// because it is the platform's clock — so the gap is made by backdating `actor_recorded_at`, which
// is exactly how a real one arises: the handset recorded it then and the platform saw it now.
func recordLateMilestone(
	t *testing.T,
	pool *pgxpool.Pool,
	jobID uuid.UUID,
	milestone string,
	behind time.Duration,
) uuid.UUID {
	t.Helper()

	id, err := uuid.NewV7()
	if err != nil {
		t.Fatalf("generating a milestone id: %v", err)
	}

	if _, err := pool.Exec(t.Context(), `
		INSERT INTO milestones (id, job_id, milestone, actor_type, actor_id, actor_recorded_at)
		VALUES ($1, $2, $3, 'driver', gen_random_uuid(), now() - $4::interval)`,
		id, jobID, milestone, behind); err != nil {
		t.Fatalf("recording a late milestone: %v", err)
	}
	return id
}

// groundsOf indexes a page by entry, so an assertion can say "this row, on this ground".
func groundsOf(entries []ExceptionEntry) map[uuid.UUID]ExceptionGround {
	out := make(map[uuid.UUID]ExceptionGround, len(entries))
	for _, e := range entries {
		out[e.EntryID] = e.Ground
	}
	return out
}

// TestAllFourGroundsSurfaceInOneQueue is SHIP-157's *Done when*, entire.
//
// "Overdue pickup, delayed delivery, failed proof, and unsynced milestones surface here." Four facts
// are made, one read is taken, and all four are in it — which is the claim, and which four separate
// endpoints would not have made.
//
// **Each ground has a negative beside it**, because every one of these predicates passes trivially
// if it is missing: a queue with no `WHERE` returns every job and every milestone and would satisfy
// a test that only ever looked for what it put in.
func TestAllFourGroundsSurfaceInOneQueue(t *testing.T) {
	moderation, pool, deliveredJobID := moderationFixture(t)

	customer := newAccount(t, pool, "grounds-customer@example.com", "0419200157", "customer")
	provider := newAccount(t, pool, "grounds-provider@example.com", "0419300157", "provider")

	past := time.Now().UTC().Add(-48 * time.Hour)
	future := time.Now().UTC().Add(48 * time.Hour)

	// Ground one: awarded, past its pickup window, not collected.
	overdue := newDraft(t, pool, customer)
	moveJob(t, pool, overdue, jobs.User(jobs.ActorCustomer, customer),
		jobs.StatusOpen, jobs.StatusAwarded)
	setWindows(t, pool, overdue, &past, &future)

	// Ground two: picked up, past its drop-off window, not delivered. It is deliberately *not*
	// also overdue for pickup — its pickup window is in the future — so the two grounds are told
	// apart rather than both firing on one job.
	delayed := newDraft(t, pool, customer)
	moveJob(t, pool, delayed, jobs.User(jobs.ActorCustomer, customer),
		jobs.StatusOpen, jobs.StatusAwarded)
	acceptBid(t, pool, delayed, provider)
	moveJob(t, pool, delayed, jobs.User(jobs.ActorProvider, provider),
		jobs.StatusDriverAssigned, jobs.StatusEnRouteToPickup, jobs.StatusPickedUp)
	setWindows(t, pool, delayed, &future, &past)

	// The negative for both window grounds: an awarded job whose windows have not closed.
	onTime := newDraft(t, pool, customer)
	moveJob(t, pool, onTime, jobs.User(jobs.ActorCustomer, customer),
		jobs.StatusOpen, jobs.StatusAwarded)
	setWindows(t, pool, onTime, &future, &future)

	// Ground three, and its negative: a reasoned delivery and a photographed one.
	at := time.Date(2026, 8, 14, 2, 15, 30, 0, time.UTC)
	failedProof := recordEvidence(t, pool, deliveredJobID, "Delivered", "location_unsafe", "", at)
	photographed := recordEvidence(t, pool, deliveredJobID, "Picked up", "", "", at)

	// Ground four, and its negative: one update a day late and one that arrived promptly.
	late := recordLateMilestone(t, pool, deliveredJobID, "In transit", 25*time.Hour)
	prompt := recordLateMilestone(t, pool, deliveredJobID, "En route to pickup", time.Minute)

	entries, err := moderation.Exceptions(t.Context(), QueueQuery{Limit: 100})
	if err != nil {
		t.Fatalf("reading the queue: %v", err)
	}
	got := groundsOf(entries)

	for _, want := range []struct {
		id     uuid.UUID
		ground ExceptionGround
		what   string
	}{
		{overdue, GroundOverduePickup, "a job past its pickup window with no collection"},
		{delayed, GroundDelayedDelivery, "a job past its drop-off window still in transit"},
		{failedProof, GroundFailedProof, "a delivery evidenced by a reason"},
		{late, GroundUnsyncedMilestone, "an update that arrived a day after it was recorded"},
	} {
		switch got[want.id] {
		case want.ground:
		case "":
			t.Errorf("%s is not in the queue at all.\nSHIP-157's Done when is that all four "+
				"grounds surface here, and this is %q.", want.what, want.ground)
		default:
			t.Errorf("%s is in the queue on ground %q, want %q", want.what, got[want.id], want.ground)
		}
	}

	for _, unwanted := range []struct {
		id   uuid.UUID
		what string
	}{
		{onTime, "a job whose windows have not closed"},
		{photographed, "a delivery evidenced by a photograph"},
		{prompt, "an update that reached the platform a minute after it was recorded"},
	} {
		if ground, ok := got[unwanted.id]; ok {
			t.Errorf("%s is in the queue on ground %q.\nEvery predicate here passes "+
				"trivially when it is missing, and this is what says it is not.",
				unwanted.what, ground)
		}
	}
}

// TestTheUnsyncedGroundUsesTheThresholdItWasGiven.
//
// **This is the wave-10 lesson applied**: a test that read `testUnsyncedThreshold` and compared it
// with a constant would prove the two constants are equal and nothing about the SQL that
// interpolates one. So the service is built twice at two different thresholds, over the same rows,
// and the queue's *content* is what changes — which can only happen through the predicate.
func TestTheUnsyncedGroundUsesTheThresholdItWasGiven(t *testing.T) {
	pool := pgtest.DB(t)

	customer := newAccount(t, pool, "threshold-customer@example.com", "0419400157", "customer")
	provider := newAccount(t, pool, "threshold-provider@example.com", "0419500157", "provider")
	jobID := deliveredJob(t, pool, customer, provider)

	// Two hours behind: over a one-hour threshold and under the twenty-four-hour one.
	twoHoursLate := recordLateMilestone(t, pool, jobID, "In transit", 2*time.Hour)

	for name, tc := range map[string]struct {
		threshold time.Duration
		want      bool
	}{
		"a threshold the gap clears":     {time.Hour, true},
		"the real twenty-four hours":     {testUnsyncedThreshold, false},
		"a threshold exactly on the gap": {2 * time.Hour, true},
	} {
		t.Run(name, func(t *testing.T) {
			moderation, err := NewModeration(testExceptionQueue{}, tc.threshold, pool)
			if err != nil {
				t.Fatalf("building the moderation service: %v", err)
			}

			entries, err := moderation.Exceptions(t.Context(),
				QueueQuery{Limit: 100, Ground: GroundUnsyncedMilestone})
			if err != nil {
				t.Fatalf("reading the queue: %v", err)
			}

			_, found := groundsOf(entries)[twoHoursLate]
			if found != tc.want {
				t.Errorf("a two-hour gap at a threshold of %s is in the queue = %v, want %v.\n"+
					"The threshold reaches the statement as a parameter; if this does not "+
					"move with it, the predicate is using something else.",
					tc.threshold, found, tc.want)
			}
		})
	}

	// The boundary itself. Docs/02 §3.1 says "24 hours — operations alert", so a row exactly on
	// the threshold is over it — `>=` rather than `>`, which the case above pins from the other
	// direction.
	if _, err := NewModeration(testExceptionQueue{}, 0, pool); err == nil {
		t.Error("a moderation service was built with a threshold of zero, which puts every " +
			"milestone ever recorded on the unsynced ground")
	}
}

// TestTheGroundFilterNarrowsTheOneQueueRatherThanSelectingBetweenFour.
//
// Both directions, on the reasoning the standing filter's test records: a filter that quietly
// excluded a ground would pass a check that only asked for the ground it wanted.
func TestTheGroundFilterNarrowsTheOneQueueRatherThanSelectingBetweenFour(t *testing.T) {
	moderation, pool, jobID := moderationFixture(t)

	customer := newAccount(t, pool, "filter-customer@example.com", "0419600157", "customer")
	past := time.Now().UTC().Add(-48 * time.Hour)

	overdue := newDraft(t, pool, customer)
	moveJob(t, pool, overdue, jobs.User(jobs.ActorCustomer, customer),
		jobs.StatusOpen, jobs.StatusAwarded)
	setWindows(t, pool, overdue, &past, nil)

	at := time.Date(2026, 8, 14, 3, 0, 0, 0, time.UTC)
	proof := recordEvidence(t, pool, jobID, "Delivered", "camera_unavailable", "", at)

	unfiltered, err := moderation.Exceptions(t.Context(), QueueQuery{Limit: 100})
	if err != nil {
		t.Fatalf("reading the unfiltered queue: %v", err)
	}
	all := groundsOf(unfiltered)
	if _, ok := all[overdue]; !ok {
		t.Error("the unfiltered queue does not carry the overdue job")
	}
	if _, ok := all[proof]; !ok {
		t.Error("the unfiltered queue does not carry the reasoned delivery")
	}

	filtered, err := moderation.Exceptions(t.Context(),
		QueueQuery{Limit: 100, Ground: GroundOverduePickup})
	if err != nil {
		t.Fatalf("reading the filtered queue: %v", err)
	}
	narrowed := groundsOf(filtered)
	if _, ok := narrowed[overdue]; !ok {
		t.Error("filtering to overdue pickups excluded the overdue job")
	}
	if _, ok := narrowed[proof]; ok {
		t.Error("filtering to overdue pickups returned a failed-proof entry")
	}
	for _, e := range filtered {
		if e.Ground != GroundOverduePickup {
			t.Errorf("a filtered page carries an entry on ground %q", e.Ground)
		}
	}
}

// TestAnUnrecognisedGroundIsRefusedRatherThanIgnored.
//
// An ignored filter answers with every entry, which reads exactly like "every exception is on this
// ground" to somebody who mistyped one — and on a moderation queue the mistake somebody then makes
// is to believe nothing is overdue.
func TestAnUnrecognisedGroundIsRefusedRatherThanIgnored(t *testing.T) {
	moderation, _, _ := moderationFixture(t)

	_, err := moderation.Exceptions(t.Context(),
		QueueQuery{Limit: 20, Ground: ExceptionGround("overdue_pickups")})
	if !errors.Is(err, ErrExceptionGroundUnrecognised) {
		t.Fatalf("a mistyped ground answered %v, want ErrExceptionGroundUnrecognised", err)
	}
}

// TestTheCursorIsTotalAcrossGrounds is why [QueueCursor] grew a third field.
//
// One job whose pickup and drop-off windows close at the **same instant** produces two entries with
// the same `recorded_at` and the same `entry_id` — the job's — differing only in the ground. A
// two-column cursor cannot separate them, and the way it fails is by hiding one, which is the single
// failure a moderation queue must not have.
func TestTheCursorIsTotalAcrossGrounds(t *testing.T) {
	moderation, pool, _ := moderationFixture(t)

	customer := newAccount(t, pool, "cursor-customer@example.com", "0419700157", "customer")
	provider := newAccount(t, pool, "cursor-provider@example.com", "0419800157", "provider")

	sameInstant := time.Now().UTC().Add(-72 * time.Hour)

	both := newDraft(t, pool, customer)
	moveJob(t, pool, both, jobs.User(jobs.ActorCustomer, customer),
		jobs.StatusOpen, jobs.StatusAwarded)
	acceptBid(t, pool, both, provider)
	setWindows(t, pool, both, &sameInstant, &sameInstant)

	seen := map[string]int{}
	var cursor QueueCursor

	for page := 0; page < 20; page++ {
		entries, err := moderation.Exceptions(t.Context(), QueueQuery{Limit: 1, After: cursor})
		if err != nil {
			t.Fatalf("page %d: %v", page, err)
		}
		if len(entries) == 0 {
			break
		}
		for _, e := range entries {
			if e.EntryID == both {
				seen[e.Ground.String()]++
			}
		}
		last := entries[len(entries)-1]
		cursor = QueueCursor{
			RecordedAt: last.RecordedAt,
			Ground:     last.Ground,
			EntryID:    last.EntryID,
		}
	}

	for _, ground := range []ExceptionGround{GroundOverduePickup, GroundDelayedDelivery} {
		switch seen[ground.String()] {
		case 0:
			t.Errorf("paging one entry at a time never returned the %s entry for a job whose "+
				"two windows close at the same instant — the cursor is not total", ground)
		case 1:
		default:
			t.Errorf("the %s entry was returned %d times", ground, seen[ground.String()])
		}
	}
}

// TestTheExceptionGroundsAreTheOnesTheStatementEmits is the Docs/10 §3.4 pairing for the ground.
//
// A ground is the one vocabulary in this queue that `admin` owns, and it exists twice: as a Go
// constant and as a string literal in the SQL that cmd/api runs. **A test that read the constants
// would prove they are what they are**, so this reads the statement and checks that every constant
// appears in it and that the statement emits no ground the domain does not declare.
func TestTheExceptionGroundsAreTheOnesTheStatementEmits(t *testing.T) {
	declared := map[string]bool{}
	for _, g := range ExceptionGrounds {
		declared[g.String()] = true
		if !strings.Contains(testExceptionQuery, "'"+g.String()+"'::text") {
			t.Errorf("the ground %q is declared and the statement never emits it, so nothing "+
				"can ever be in the queue on it", g)
		}
	}

	// The other direction: a literal in the statement that no constant names would produce
	// entries a client cannot branch on and a filter cannot select.
	for _, emitted := range regexp.MustCompile(`'([a-z_]+)'::text AS ground|
		'([a-z_]+)'::text,`).FindAllStringSubmatch(testExceptionQuery, -1) {
		for _, group := range emitted[1:] {
			if group != "" && !declared[group] {
				t.Errorf("the statement emits the ground %q, which is not one of "+
					"ExceptionGrounds", group)
			}
		}
	}
}

// TestTheTestDoubleRunsTheStatementCmdApiRuns.
//
// [testExceptionQueue] is a copy of a query, and a copy is a thing that can drift. This reads the
// SQL out of cmd/api/routes_admin.go, resolves the Go concatenation the production statement uses
// for its ground literals and status sets, and compares the two — so a predicate changed in one file
// and not the other fails here rather than passing in two packages and being found by `make verify`,
// which is where wave 8 found SHIP-117's only defect.
//
// **It resolves the constants rather than restating them**, which is what makes it a guard instead
// of a third copy: a status added to `delayedDeliveryStatuses` in cmd/api changes the text this test
// compares against, and the comparison then fails until the copy is updated too.
func TestTheTestDoubleRunsTheStatementCmdApiRuns(t *testing.T) {
	source, err := os.ReadFile(filepath.Join("..", "..", "cmd", "api", "routes_admin.go"))
	if err != nil {
		t.Fatalf("reading cmd/api/routes_admin.go: %v", err)
	}

	production := extractStatement(t, string(source), "WITH entries AS (", "LIMIT $6")
	if !strings.Contains(production, "UNION ALL") {
		t.Fatalf("the statement read out of cmd/api is not the union this guards:\n%s", production)
	}

	if got, want := sqlSkeleton(production), sqlSkeleton(testExceptionQuery); !slices.Equal(got, want) {
		t.Errorf("the two statements have drifted, so every test in this file is about a query "+
			"nothing serves.\n\ncmd/api:\n  %s\n\nthis package's copy:\n  %s",
			strings.Join(got, "\n  "), strings.Join(want, "\n  "))
	}
}

// extractStatement pulls one SQL statement out of cmd/api's source with its Go constants resolved.
//
// Deliberately textual rather than a parse: the thing being compared is the SQL a database receives,
// which is what string concatenation produces, and go/ast would give the pieces rather than the
// result.
//
// `open` and `close` bracket the statement. Shared with the cancellation queue's guard, which is the
// second caller and the reason this took two parameters rather than knowing one statement.
func extractStatement(t *testing.T, source, open, close string) string {
	t.Helper()

	from := strings.Index(source, open)
	to := strings.Index(source, close)
	if from < 0 || to < from {
		t.Fatal("the exception-queue statement was not found in cmd/api/routes_admin.go; this " +
			"guard has stopped guarding anything")
	}
	query := source[from : to+len(close)]

	// `x` + IDENT + `y` — the production statement's ground literals and status sets. Resolved
	// from the same file, so a value changed there changes what this test compares against.
	concatenation := regexp.MustCompile("`" + `\s*\+\s*(?:string\()?([A-Za-z.]+)\)?\s*\+\s*` + "`")
	return concatenation.ReplaceAllStringFunc(query, func(match string) string {
		name := concatenation.FindStringSubmatch(match)[1]
		value := constantValue(t, source, name)
		return value
	})
}

// constantValue reads a constant that cmd/api's SQL concatenates in, by name.
//
// Two shapes appear: a package constant declared in the same file (`overduePickupStatuses`), read
// out of the source; and a qualified constant this package exports (`admin.GroundFailedProof`),
// whose value is resolved from the declaration rather than from the text — so a renamed *value*
// changes what the guard compares against, which is the whole point of resolving rather than
// restating.
func constantValue(t *testing.T, source, name string) string {
	t.Helper()

	if value, ok := exportedConstant(name); ok {
		return value
	}
	if strings.HasPrefix(name, "admin.") {
		t.Fatalf("cmd/api's statement names %s, which this guard cannot resolve; add it to "+
			"exportedConstant so a change to its value still fails here", name)
	}

	pattern := regexp.MustCompile(regexp.QuoteMeta(name) + "\\s*=\\s*`([^`]*)`")
	found := pattern.FindStringSubmatch(source)
	if found == nil {
		t.Fatalf("cmd/api's statement names %s and no single-line constant of that name is "+
			"declared in the file", name)
	}
	return found[1]
}

// exportedConstant maps a `admin.X` reference in cmd/api's SQL to its value here.
//
// Derived from the declared sets rather than tabulated by hand, so a fifth ground or a third
// outcome needs no edit: the identifier is reconstructed from the wire value, which is the same
// direction the constants themselves are written in.
func exportedConstant(name string) (string, bool) {
	for _, g := range ExceptionGrounds {
		if "admin.Ground"+camelFromWire(g.String()) == name {
			return g.String(), true
		}
	}
	for _, o := range CancellationOutcomes {
		if "admin.Outcome"+camelFromWire(o.String()) == name {
			return o.String(), true
		}
	}
	return "", false
}

// camelFromWire is the Go identifier suffix for a wire value: `overdue_pickup` is `OverduePickup`.
//
// Derived rather than tabulated, so a value added to either closed set needs no edit here.
func camelFromWire(value string) string {
	var out strings.Builder
	for _, word := range strings.Split(value, "_") {
		if word == "" {
			continue
		}
		out.WriteString(strings.ToUpper(word[:1]))
		out.WriteString(word[1:])
	}
	return out.String()
}

// sqlSkeleton reduces SQL to its predicate, join and ordering lines, whitespace-normalised.
//
// Deliberately narrow: it keeps the lines that decide *which rows come back and in what order*, and
// drops the projection — which is where the two statements legitimately differ in layout while
// selecting exactly the same columns in exactly the same order, a fact the scan itself enforces by
// failing on a mismatch.
func sqlSkeleton(text string) []string {
	var out []string
	for _, line := range strings.Split(text, "\n") {
		trimmed := strings.Join(strings.Fields(line), " ")
		switch {
		case strings.HasPrefix(trimmed, "--"), trimmed == "":
			continue
		case strings.HasPrefix(trimmed, "WHERE "), strings.HasPrefix(trimmed, "AND "),
			strings.HasPrefix(trimmed, "OR "), strings.HasPrefix(trimmed, "ORDER BY "),
			strings.HasPrefix(trimmed, "JOIN "), strings.HasPrefix(trimmed, "FROM "),
			strings.HasPrefix(trimmed, "LIMIT "), trimmed == "UNION ALL":
			out = append(out, trimmed)
		}
	}
	return out
}
