package delivery

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/DulsaraNethmin/Shipper/services/core/internal/db"
	"github.com/DulsaraNethmin/Shipper/services/core/internal/jobs"
	"github.com/DulsaraNethmin/Shipper/services/core/internal/testsupport/pgtest"
)

// SHIP-136's *Done when* for this domain: every state change in Docs/01 §4.5 emits its event from
// the domain rather than from the API layer.
//
// # Everything below reads the outbox table, and none of it reads a recording sink
//
// A stub sink can be told anything. What the ticket has to demonstrate is that the row **commits
// with the change it describes** (Docs/06 §4.0), and only the real writer inside the real
// transaction can show that — so the fixtures pass `events.NewOutbox()` and these read what a
// consumer would.
//
// # Two of the tests here exist because `job.status_changed` does not cover them
//
// A milestone that moves the job already emits from `jobs`, through the port, in this domain's
// transaction. What was invisible downstream before this ticket is everything that writes a row and
// moves nothing: the failed pickup attempt of Docs/02 §5, and SHIP-112's absorbed late milestone.
// TestAnAbsorbedMilestoneEmitsWithJobMovedFalse is the one that would have caught it.

// deliveryEvents is every outbox row this domain wrote about one job, oldest first.
//
// Fenced on `aggregate_type = 'delivery'` as well as on the identifier, which is what makes the
// counting honest: `jobs` writes `job.status_changed` about the same job id in the same
// transaction, and a query on the id alone would count both.
func deliveryEvents(t *testing.T, pool *pgxpool.Pool, jobID uuid.UUID) []deliveryEvent {
	t.Helper()

	rows, err := pool.Query(t.Context(), `
		SELECT event_type, payload::text
		FROM outbox
		WHERE aggregate_type = 'delivery' AND aggregate_id = $1
		ORDER BY id ASC`, jobID)
	if err != nil {
		t.Fatalf("reading the outbox for %s: %v", jobID, err)
	}
	defer rows.Close()

	var found []deliveryEvent
	for rows.Next() {
		var eventType, raw string
		if err := rows.Scan(&eventType, &raw); err != nil {
			t.Fatalf("scanning an outbox row: %v", err)
		}

		var payload map[string]any
		if err := json.Unmarshal([]byte(raw), &payload); err != nil {
			t.Fatalf("%s carries a payload that is not an object: %v", eventType, err)
		}
		found = append(found, deliveryEvent{eventType: eventType, payload: payload, raw: raw})
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("reading the outbox for %s: %v", jobID, err)
	}
	return found
}

type deliveryEvent struct {
	eventType string
	payload   map[string]any
	raw       string
}

func names(got []deliveryEvent) string {
	if len(got) == 0 {
		return "nothing"
	}
	out := make([]string, 0, len(got))
	for _, e := range got {
		out = append(out, e.eventType)
	}
	return "[" + strings.Join(out, " ") + "]"
}

// TestAnAssignmentEmitsItsEvent is Docs/01 §4.4's first recordable item.
//
// A repeated nomination of the same driver is the second half, and it is the one worth having: it
// writes nothing, so it must emit nothing. A customer told twice that a driver had been assigned is
// a symptom nothing in the response would show.
func TestAnAssignmentEmitsItsEventOncePerAssignment(t *testing.T) {
	pool := pgtest.DB(t)
	customer := newAccount(t, pool, "ev-assign-c@example.com", "+61400000870", "customer")
	provider := newAccount(t, pool, "ev-assign-p@example.com", "+61400000871", "provider")
	jobID := awardedJob(t, pool, customer, provider)
	svc := newTestService()

	nomination := Nomination{DriverName: "Alex Driver", DriverMobile: "+61412345678"}

	assignment, created, err := assign(t, pool, svc, provider, jobID, nomination)
	if err != nil || !created {
		t.Fatalf("assigning: %v (created %v)", err, created)
	}

	got := deliveryEvents(t, pool, jobID)
	if len(got) != 1 || got[0].eventType != EventDriverAssigned {
		t.Fatalf("the outbox holds %s, want one %s", names(got), EventDriverAssigned)
	}
	if id := got[0].payload["assignment_id"]; id != assignment.ID.String() {
		t.Errorf("assignment_id is %v, want %s", id, assignment.ID)
	}
	if id := got[0].payload["provider_id"]; id != provider.String() {
		t.Errorf("provider_id is %v, want %s", id, provider)
	}

	// **The driver's name and mobile are not on it**, and Docs/01 §5.1 is why: an event travels
	// through the outbox onto a topic with seven days of retention and into every consumer there
	// will ever be. A consumer with a reason to know who is driving reads the row.
	if strings.Contains(got[0].raw, "Alex Driver") || strings.Contains(got[0].raw, "61412345678") {
		t.Errorf("the assignment event carries the driver's details: %s", got[0].raw)
	}

	if _, _, err := assign(t, pool, svc, provider, jobID, nomination); err != nil {
		t.Fatalf("re-nominating the same driver: %v", err)
	}
	if got := deliveryEvents(t, pool, jobID); len(got) != 1 {
		t.Errorf("the repeated nomination left %s, want one %s", names(got), EventDriverAssigned)
	}
}

// TestAMilestoneThatMovesTheJobEmitsFromBothDomains.
//
// One transaction, two domains, two events on two aggregates and therefore two topics: `jobs` says
// the job moved and `delivery` says what was recorded. Neither package imports the other and both
// rows are written inside the transaction the handler opened.
func TestAMilestoneThatMovesTheJobEmitsFromBothDomains(t *testing.T) {
	pool := pgtest.DB(t)
	customer := newAccount(t, pool, "ev-ms-c@example.com", "+61400000872", "customer")
	provider := newAccount(t, pool, "ev-ms-p@example.com", "+61400000873", "provider")
	jobID := awardedJob(t, pool, customer, provider)

	record, outcome, err := recordMilestone(t, pool, newTestService(), provider, jobID, enRoute(theKey))
	if err != nil || outcome != OutcomeRecorded {
		t.Fatalf("recording: %v (outcome %s)", err, outcome)
	}

	got := deliveryEvents(t, pool, jobID)
	if len(got) != 1 || got[0].eventType != EventMilestoneRecorded {
		t.Fatalf("the outbox holds %s, want one %s", names(got), EventMilestoneRecorded)
	}
	if id := got[0].payload["milestone_id"]; id != record.ID.String() {
		t.Errorf("milestone_id is %v, want %s", id, record.ID)
	}
	if m := got[0].payload["milestone"]; m != MilestoneEnRouteToPickup.Wire() {
		t.Errorf("milestone is %v, want %s", m, MilestoneEnRouteToPickup.Wire())
	}
	if moved := got[0].payload["job_moved"]; moved != true {
		t.Errorf("job_moved is %v, want true — this recording moved the job", moved)
	}

	var jobEvents int
	if err := pool.QueryRow(t.Context(), `
		SELECT count(*) FROM outbox
		WHERE aggregate_type = 'job' AND aggregate_id = $1 AND event_type = 'job.status_changed'`,
		jobID).Scan(&jobEvents); err != nil {
		t.Fatalf("counting the job's events: %v", err)
	}
	// Three: the fixture published the job and awarded it, and this recording moved it again.
	if jobEvents != 3 {
		t.Errorf("%d job.status_changed events, want 3 — the transition emits from `jobs`", jobEvents)
	}
}

// TestAnAbsorbedMilestoneEmitsWithJobMovedFalse is the case the job's status cannot report.
//
// Docs/02 §3.1: a queued milestone that syncs after the job has moved past it is "absorbed, not
// rejected as an error… the platform accepts the historical fact without moving the job backwards".
// So there is no transition, no `job_status_history` row and no `job.status_changed` — and before
// this ticket, nothing downstream at all. The event is what a consumer has.
func TestAnAbsorbedMilestoneEmitsWithJobMovedFalse(t *testing.T) {
	pool := pgtest.DB(t)
	customer := newAccount(t, pool, "ev-late-c@example.com", "+61400000874", "customer")
	provider := newAccount(t, pool, "ev-late-p@example.com", "+61400000875", "provider")
	jobID := awardedJob(t, pool, customer, provider)

	moveJob(t, pool, jobID, jobs.User(jobs.ActorProvider, provider),
		jobs.StatusEnRouteToPickup, jobs.StatusPickedUp)

	actedAt := testInstant.Add(-time.Hour)
	_, outcome, err := recordMilestone(t, pool, newTestService(), provider, jobID,
		Recording{Milestone: MilestoneEnRouteToPickup, RecordedAt: actedAt, Key: theKey})
	if err != nil || outcome != OutcomeAbsorbed {
		t.Fatalf("recording a late milestone: %v (outcome %s)", err, outcome)
	}

	got := deliveryEvents(t, pool, jobID)
	if len(got) != 1 || got[0].eventType != EventMilestoneRecorded {
		t.Fatalf("the absorbed milestone left %s, want one %s", names(got), EventMilestoneRecorded)
	}
	if moved := got[0].payload["job_moved"]; moved != false {
		t.Errorf("job_moved is %v, want false — the job was deliberately left where it was", moved)
	}

	// The two clocks, kept apart. An offline driver's claim and the platform's record are two
	// facts (000601's trigger refuses an insert that names the second), and a consumer rendering a
	// timeline needs both — which the envelope's single occurred_at cannot give it.
	actor, ok := got[0].payload["actor_recorded_at"].(string)
	if !ok {
		t.Fatal("the milestone event carries no actor_recorded_at")
	}
	server, ok := got[0].payload["server_recorded_at"].(string)
	if !ok {
		t.Fatal("the milestone event carries no server_recorded_at")
	}
	if actor == server {
		t.Errorf("both clocks read %s; the actor recorded this an hour before the platform saw it", actor)
	}
}

// TestARepeatedMilestoneEmitsOnce — the retry that outlives the middleware's cache.
//
// `uq_milestones_idempotency` refuses the second row and [Service.alreadyRecorded] answers from the
// first, before anything is emitted. A second event would be a second notification for one act.
func TestARepeatedMilestoneEmitsOnce(t *testing.T) {
	pool := pgtest.DB(t)
	customer := newAccount(t, pool, "ev-retry-c@example.com", "+61400000876", "customer")
	provider := newAccount(t, pool, "ev-retry-p@example.com", "+61400000877", "provider")
	jobID := awardedJob(t, pool, customer, provider)
	svc := newTestService()

	if _, _, err := recordMilestone(t, pool, svc, provider, jobID, enRoute(theKey)); err != nil {
		t.Fatalf("recording: %v", err)
	}
	_, outcome, err := recordMilestone(t, pool, svc, provider, jobID, enRoute(theKey))
	if err != nil || outcome != OutcomeAlreadyRecorded {
		t.Fatalf("retrying: %v (outcome %s)", err, outcome)
	}

	if got := deliveryEvents(t, pool, jobID); len(got) != 1 {
		t.Errorf("the retry left %s, want one %s", names(got), EventMilestoneRecorded)
	}
}

// TestAReasonedExceptionEmitsItsProofEventAfterTheMilestoneItStandsBehind.
//
// Two things at once, and the ordering is the half that needed deciding. Both events are about the
// job, so both carry the job's identifier as their aggregate id, so both hash to one partition and
// Kafka keeps their order (catalogue.go). A `delivery.proof_recorded` arriving before the milestone
// it names would make a consumer buffer an event pointing at something it has never seen.
func TestAReasonedExceptionEmitsItsProofEventAfterTheMilestoneItStandsBehind(t *testing.T) {
	pool := pgtest.DB(t)
	customer := newAccount(t, pool, "ev-exc-c@example.com", "+61400000878", "customer")
	provider := newAccount(t, pool, "ev-exc-p@example.com", "+61400000879", "provider")
	jobID := awardedJob(t, pool, customer, provider)

	svc := serviceReadingProofFrom(newRecordingObjects())

	record, outcome, err := recordWithException(t, pool, svc, provider, jobID,
		enRoute(theExceptionKey), ExceptionRecipientObjected)
	if err != nil || outcome != OutcomeRecorded {
		t.Fatalf("recording with an exception: %v (outcome %s)", err, outcome)
	}

	got := deliveryEvents(t, pool, jobID)
	if len(got) != 2 {
		t.Fatalf("the outbox holds %s, want [%s %s]",
			names(got), EventMilestoneRecorded, EventProofRecorded)
	}
	if got[0].eventType != EventMilestoneRecorded || got[1].eventType != EventProofRecorded {
		t.Fatalf("the outbox holds %s in that order; the evidence must follow the claim it stands "+
			"behind, because both are keyed on the job and therefore ordered", names(got))
	}
	if id := got[1].payload["milestone_id"]; id != record.ID.String() {
		t.Errorf("the proof event names milestone %v, want %s", id, record.ID)
	}
	if isException := got[1].payload["is_exception"]; isException != true {
		t.Errorf("is_exception is %v, want true", isException)
	}
	if reason := got[1].payload["exception_reason"]; reason != string(ExceptionRecipientObjected) {
		t.Errorf("exception_reason is %v, want %s", reason, ExceptionRecipientObjected)
	}
}

// TestAPhotographEmitsItsProofEventWithoutTheObjectKey.
//
// The event says evidence exists and what kind it is. **Where the bytes are is deliberately not on
// it**: a pre-signed URL is issued by [Service.ProofFor] *after* an authorisation check, which is a
// decision no consumer of a topic is in a position to make.
func TestAPhotographEmitsItsProofEventWithoutTheObjectKey(t *testing.T) {
	pool := pgtest.DB(t)
	customer := newAccount(t, pool, "ev-photo-c@example.com", "+61400000880", "customer")
	provider := newAccount(t, pool, "ev-photo-p@example.com", "+61400000881", "provider")
	jobID := awardedJob(t, pool, customer, provider)

	objectKey, err := proofObjectKey(jobID)
	if err != nil {
		t.Fatalf("generating an object key: %v", err)
	}
	objects := newRecordingObjects()
	objects.holding(objectKey, aPhotograph())
	svc := serviceReadingProofFrom(objects)

	if _, _, err := recordWithProof(t, pool, svc, provider, jobID, enRoute(theKey), objectKey); err != nil {
		t.Fatalf("recording with a photograph: %v", err)
	}

	got := deliveryEvents(t, pool, jobID)
	if len(got) != 2 || got[1].eventType != EventProofRecorded {
		t.Fatalf("the outbox holds %s, want [%s %s]",
			names(got), EventMilestoneRecorded, EventProofRecorded)
	}
	if isException := got[1].payload["is_exception"]; isException != false {
		t.Errorf("is_exception is %v, want false", isException)
	}
	if ct := got[1].payload["content_type"]; ct != "image/jpeg" {
		t.Errorf("content_type is %v, want the store's image/jpeg", ct)
	}
	if strings.Contains(got[1].raw, objectKey) {
		t.Errorf("the proof event carries the object key: %s", got[1].raw)
	}
}

// TestADeliveryEventWhoseTransactionRollsBackIsNotInTheOutbox is the whole reason the seam takes a
// db.Runner, and the one property a recording sink cannot show.
//
// Docs/06 §4.0: publish then fail to commit, and a consumer acts on something that never happened.
func TestADeliveryEventWhoseTransactionRollsBackIsNotInTheOutbox(t *testing.T) {
	pool := pgtest.DB(t)
	customer := newAccount(t, pool, "ev-roll-c@example.com", "+61400000882", "customer")
	provider := newAccount(t, pool, "ev-roll-p@example.com", "+61400000883", "provider")
	jobID := awardedJob(t, pool, customer, provider)
	svc := newTestService()

	sentinel := errors.New("the caller changed its mind after the milestone was written")

	err := db.InTx(t.Context(), pool, func(ctx context.Context, r db.Runner) error {
		if _, _, err := svc.RecordMilestone(ctx, r, provider, jobID, enRoute(theKey)); err != nil {
			return err
		}
		return sentinel
	})
	if !errors.Is(err, sentinel) {
		t.Fatalf("the transaction reported %v, want the sentinel", err)
	}

	if got := deliveryEvents(t, pool, jobID); len(got) != 0 {
		t.Errorf("the rolled-back transaction left %s in the outbox", names(got))
	}
	if n := milestoneCount(t, pool, jobID); n != 0 {
		t.Error("the milestone survived a rolled-back transaction, so this test proves nothing")
	}
}

// TestNoDeliveryEventCarriesMoreThanItsDeclaredKeys is Docs/01 §5.1 and §4.3 applied to the surface
// that travels furthest.
//
// **A closed set of keys rather than a search for a field name**, which is SHIP-83's finding brought
// to the outbox: a search catches `driver_mobile` and misses `contact`. A field added to any payload
// in this package fails this whatever it is called, and adding it to the list is where somebody has
// to think about what they are putting on a topic for seven days.
func TestNoDeliveryEventCarriesMoreThanItsDeclaredKeys(t *testing.T) {
	permitted := map[string]bool{
		"schema_version": true,

		"assignment_id": true, "milestone_id": true, "proof_id": true,
		"job_id": true, "provider_id": true,

		"milestone": true, "job_moved": true,
		"actor_type": true, "actor_id": true, "reason": true,
		"actor_recorded_at": true, "server_recorded_at": true,

		"is_exception": true, "exception_reason": true, "content_type": true,
	}

	pool := pgtest.DB(t)
	customer := newAccount(t, pool, "ev-keys-c@example.com", "+61400000884", "customer")
	provider := newAccount(t, pool, "ev-keys-p@example.com", "+61400000885", "provider")
	jobID := awardedJob(t, pool, customer, provider)

	svc := serviceReadingProofFrom(newRecordingObjects())

	if _, _, err := assign(t, pool, svc, provider, jobID,
		Nomination{DriverName: "Sam Driver", DriverMobile: "+61412345679"}); err != nil {
		t.Fatalf("assigning: %v", err)
	}
	if _, _, err := recordWithException(t, pool, svc, provider, jobID,
		enRoute(theExceptionKey), ExceptionCameraUnavailable); err != nil {
		t.Fatalf("recording with an exception: %v", err)
	}

	got := deliveryEvents(t, pool, jobID)
	seen := map[string]bool{}
	for _, e := range got {
		seen[e.eventType] = true
		for key := range e.payload {
			if !permitted[key] {
				t.Errorf("%s carries %q, which is not in the permitted set. If it belongs on a "+
					"topic, add it here — and read Docs/01 §5.1 first, because an event travels "+
					"past every point a response body could have redacted it", e.eventType, key)
			}
		}
	}

	for _, want := range []string{EventDriverAssigned, EventMilestoneRecorded, EventProofRecorded} {
		if !seen[want] {
			t.Fatalf("the fixture emitted no %s, so this test proves nothing about it", want)
		}
	}
}
