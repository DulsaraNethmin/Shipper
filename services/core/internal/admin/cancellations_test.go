package admin

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/DulsaraNethmin/Shipper/services/core/internal/db"
	"github.com/DulsaraNethmin/Shipper/services/core/internal/httpx"
	"github.com/DulsaraNethmin/Shipper/services/core/internal/jobs"
	"github.com/DulsaraNethmin/Shipper/services/core/internal/pagination"
	"github.com/DulsaraNethmin/Shipper/services/core/internal/testsupport/pgtest"
)

// SHIP-158 against a real PostgreSQL.
//
// The *Done when* is "cancellations after award are listed with the provider's history", and both
// halves are statements about rows: which transition puts a job on the queue, and two counts over
// every job the provider ever carried. A mocked source would prove that a slice round-trips.
//
// [testCancellationQueue] is this package's copy of the adapter cmd/api holds, for the reason
// [testExceptionQueue] is — `admin` may import neither `jobs` nor `bidding`, and package main has no
// database a Go test can reach — and it is held to the original by
// TestTheCancellationDoubleRunsTheStatementCmdApiRuns rather than trusted.
//
// newAccount, newDraft, moveJob, moveJobBecause and acceptBid come from service_test.go.

// testCancellationQueue is admin.CancellationQueue over `jobs`, `job_status_history` and `bids`.
type testCancellationQueue struct{}

func (testCancellationQueue) CancelledAfterAward(
	ctx context.Context,
	r db.Runner,
	q QueueQuery,
) ([]CancellationEntry, error) {
	var after, afterID any
	if !q.After.Zero() {
		after, afterID = q.After.RecordedAt, q.After.EntryID
	}

	rows, err := r.Query(ctx, testCancellationQuery, after, afterID, q.Limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []CancellationEntry
	for rows.Next() {
		var e CancellationEntry
		if err := rows.Scan(&e.JobID, &e.TransitionID, &e.ProviderID, &e.Outcome,
			&e.FromStatus, &e.ActorType, &e.Reason, &e.CancelledAt,
			&e.ProviderCancellations, &e.ProviderCompletions); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

// testCancellationQuery is the statement, verbatim from cmd/api/routes_admin.go with its constants
// resolved. Held to the original by TestTheCancellationDoubleRunsTheStatementCmdApiRuns.
const testCancellationQuery = `
		WITH cancellations AS (
		    SELECT h.job_id, h.id AS transition_id, h.server_recorded_at AS cancelled_at,
		           h.from_status, h.actor_type, coalesce(h.reason, '') AS reason,
		           CASE WHEN h.to_status = 'Open'
		                THEN 'returned_to_market'
		                ELSE 'ended' END AS outcome
		    FROM job_status_history h
		    WHERE (h.to_status = 'Open' AND h.from_status IN ('Awarded', 'Driver assigned'))
		       OR (h.to_status = 'Cancelled'
		           AND EXISTS (SELECT 1
		                         FROM job_status_history a
		                        WHERE a.job_id = h.job_id
		                          AND a.to_status = 'Awarded'
		                          AND (a.server_recorded_at, a.id) < (h.server_recorded_at, h.id)))
		),
		carrier AS (
		    SELECT c.transition_id, b.provider_id
		    FROM cancellations c
		    LEFT JOIN bids b ON b.job_id = c.job_id AND b.status = 'Accepted'
		),
		walked AS (
		    SELECT provider_id, count(*) AS cancellations
		    FROM carrier
		    WHERE provider_id IS NOT NULL
		    GROUP BY provider_id
		),
		completions AS (
		    SELECT b.provider_id, count(*) AS completions
		    FROM bids b
		    JOIN jobs j ON j.id = b.job_id
		    WHERE b.status = 'Accepted' AND j.status = 'Completed'
		    GROUP BY b.provider_id
		)
		SELECT c.job_id, c.transition_id,
		       coalesce(r.provider_id, '00000000-0000-0000-0000-000000000000'::uuid),
		       c.outcome, c.from_status, c.actor_type, c.reason, c.cancelled_at,
		       coalesce(w.cancellations, 0), coalesce(m.completions, 0)
		FROM cancellations c
		JOIN carrier r      ON r.transition_id = c.transition_id
		LEFT JOIN walked w      ON w.provider_id = r.provider_id
		LEFT JOIN completions m ON m.provider_id = r.provider_id
		WHERE ($1::timestamptz IS NULL OR (c.cancelled_at, c.transition_id) < ($1, $2))
		ORDER BY c.cancelled_at DESC, c.transition_id DESC
		LIMIT $3`

// cancellationFixture is a queue service over a fresh database, with a customer and a provider.
func cancellationFixture(t *testing.T) (*Cancellations, *pgxpool.Pool, uuid.UUID, uuid.UUID) {
	t.Helper()

	pool := pgtest.DB(t)

	customer := newAccount(t, pool, "cancel-customer@example.com", "0419000158", "customer")
	provider := newAccount(t, pool, "cancel-provider@example.com", "0419100158", "provider")

	queue, err := NewCancellations(testCancellationQueue{}, pool)
	if err != nil {
		t.Fatalf("building the cancellation queue: %v", err)
	}
	return queue, pool, customer, provider
}

// awardedTo builds a job, publishes it and awards it to the provider, all through the guard.
//
// Every move goes through `jobs.Service.Transition` rather than an UPDATE, which is what makes the
// `job_status_history` rows this queue reads exist at all — `000402` refuses a status written any
// other way, and a fixture that wrote one would be testing a queue over rows the platform cannot
// produce.
func awardedTo(t *testing.T, pool *pgxpool.Pool, customer, provider uuid.UUID) uuid.UUID {
	t.Helper()

	jobID := newDraft(t, pool, customer)
	moveJob(t, pool, jobID, jobs.User(jobs.ActorCustomer, customer),
		jobs.StatusOpen, jobs.StatusAwarded)
	acceptBid(t, pool, jobID, provider)
	return jobID
}

// returnedToMarket is Docs/02 §6.2's provider cancellation: awarded, then back to Open.
//
// `stopAt` is `Awarded` or `Driver assigned`, the only two statuses §2 permits this from — after
// `Picked up` the goods are in somebody's vehicle and §6.2 makes ending it a support case.
func returnedToMarket(
	t *testing.T,
	pool *pgxpool.Pool,
	customer, provider uuid.UUID,
	stopAt jobs.Status,
	reason string,
) uuid.UUID {
	t.Helper()

	jobID := awardedTo(t, pool, customer, provider)
	if stopAt == jobs.StatusDriverAssigned {
		moveJob(t, pool, jobID, jobs.User(jobs.ActorProvider, provider), jobs.StatusDriverAssigned)
	}
	moveJobBecause(t, pool, jobID, jobs.User(jobs.ActorProvider, provider), reason, jobs.StatusOpen)
	return jobID
}

// endedAfterAward is the other shape: awarded, disputed, and resolved as cancelled.
//
// **The only route to `Cancelled` after award that Docs/02 §2 permits.** There is no
// `Awarded → Cancelled`, which is the finding this ticket turned on: a queue reading
// `to_status = 'Cancelled'` alone would list the pre-award cancellations and none of these.
func endedAfterAward(
	t *testing.T,
	pool *pgxpool.Pool,
	customer, provider uuid.UUID,
	reason string,
) uuid.UUID {
	t.Helper()

	jobID := awardedTo(t, pool, customer, provider)
	moveJob(t, pool, jobID, jobs.User(jobs.ActorCustomer, customer), jobs.StatusDisputed)
	moveJobBecause(t, pool, jobID, jobs.User(jobs.ActorAdmin, customer), reason, jobs.StatusCancelled)
	return jobID
}

// entriesByJob indexes a page, so an assertion can name a job rather than a position.
func entriesByJob(entries []CancellationEntry) map[uuid.UUID]CancellationEntry {
	out := make(map[uuid.UUID]CancellationEntry, len(entries))
	for _, e := range entries {
		out[e.JobID] = e
	}
	return out
}

// TestOnlyPostAwardCancellationsAreListed is the first half of SHIP-158's *Done when*, and the
// negatives are what make it evidence.
//
// **The negative that matters most is a job cancelled from `Open`** — the ordinary route out of the
// marketplace, which Docs/02 §2 names and SHIP-68's expiry sweep also takes. A queue carrying it
// would be a list of every cancelled job the platform has ever had, which is the same as no queue at
// all; and because there is no `Awarded → Cancelled` transition, a naive `to_status = 'Cancelled'`
// implementation returns *exactly* those and nothing else.
func TestOnlyPostAwardCancellationsAreListed(t *testing.T) {
	queue, pool, customer, provider := cancellationFixture(t)

	walkedAway := returnedToMarket(t, pool, customer, provider, jobs.StatusAwarded,
		"The provider could not source a vehicle with a tailgate lifter.")
	ended := endedAfterAward(t, pool, customer, provider,
		"Resolved as a failed delivery; the goods never left the depot.")

	// Cancelled from Open: nobody had committed to it.
	beforeAward := newDraft(t, pool, customer)
	moveJob(t, pool, beforeAward, jobs.User(jobs.ActorCustomer, customer), jobs.StatusOpen)
	moveJob(t, pool, beforeAward, jobs.User(jobs.ActorCustomer, customer), jobs.StatusCancelled)

	// Abandoned as a draft: the other pre-award route, and the one an implementation keyed on
	// `from_status` rather than on the history would also let through.
	abandoned := newDraft(t, pool, customer)
	moveJob(t, pool, abandoned, jobs.User(jobs.ActorCustomer, customer), jobs.StatusCancelled)

	entries, err := queue.AfterAward(t.Context(), QueueQuery{Limit: 50})
	if err != nil {
		t.Fatalf("reading the queue: %v", err)
	}
	found := entriesByJob(entries)

	returned, ok := found[walkedAway]
	if !ok {
		t.Fatalf("a provider cancellation after award is not in the queue (%d entries).\n"+
			"Docs/02 §6.2 says this cancellation is recorded against the provider and cannot "+
			"be reconstructed later; this queue is where it is read.", len(entries))
	}
	if returned.Outcome != OutcomeReturnedToMarket {
		t.Errorf("outcome = %q, want %q", returned.Outcome, OutcomeReturnedToMarket)
	}
	if returned.FromStatus != jobs.StatusAwarded.String() {
		t.Errorf("from_status = %q, want %q", returned.FromStatus, jobs.StatusAwarded)
	}
	if returned.ProviderID != provider {
		t.Errorf("provider_id = %s, want the provider whose bid was accepted (%s)",
			returned.ProviderID, provider)
	}
	if returned.ActorType == "" {
		t.Error("the entry does not say who moved it, so nothing can be triaged from it")
	}
	if returned.Reason == "" {
		t.Error("the recorded reason is not carried, and it is the only account of why")
	}
	if returned.CancelledAt.IsZero() {
		t.Error("the entry carries no instant, so the queue cannot be ordered")
	}

	endedEntry, ok := found[ended]
	if !ok {
		t.Fatal("a job cancelled through a dispute after award is not in the queue")
	}
	if endedEntry.Outcome != OutcomeEnded {
		t.Errorf("outcome = %q, want %q", endedEntry.Outcome, OutcomeEnded)
	}
	if endedEntry.FromStatus != jobs.StatusDisputed.String() {
		t.Errorf("from_status = %q, want %q — Docs/02 §2's only route to Cancelled after award",
			endedEntry.FromStatus, jobs.StatusDisputed)
	}

	for label, jobID := range map[string]uuid.UUID{
		"a job cancelled from Open":      beforeAward,
		"a draft the customer abandoned": abandoned,
	} {
		if _, ok := found[jobID]; ok {
			t.Errorf("%s is in the post-award queue.\n"+
				"Docs/02 §2 makes both the ordinary route out of the marketplace, and a queue "+
				"carrying them is a list of every cancelled job there has ever been.", label)
		}
	}
}

// TestBothStatusesAProviderMayWalkAwayFromAreListed.
//
// Docs/02 §2 permits `Awarded / Driver assigned → Open` and nothing later. The set is written out
// rather than taken as a complement, so a status left out of it is a cancellation that silently
// never appears — and the `Driver assigned` one is the easier to forget, because the shorter route
// skips that status entirely.
func TestBothStatusesAProviderMayWalkAwayFromAreListed(t *testing.T) {
	queue, pool, customer, provider := cancellationFixture(t)

	made := map[jobs.Status]uuid.UUID{}
	for _, status := range []jobs.Status{jobs.StatusAwarded, jobs.StatusDriverAssigned} {
		made[status] = returnedToMarket(t, pool, customer, provider, status,
			"Cancelled while the job was "+status.String()+", for the record.")
	}

	entries, err := queue.AfterAward(t.Context(), QueueQuery{Limit: 100})
	if err != nil {
		t.Fatalf("reading the queue: %v", err)
	}
	found := entriesByJob(entries)

	for status, jobID := range made {
		entry, ok := found[jobID]
		if !ok {
			t.Errorf("a provider cancellation from %q is not in the queue; the status set left "+
				"it out", status)
			continue
		}
		if entry.FromStatus != status.String() {
			t.Errorf("the entry for a cancellation from %q reports %q", status, entry.FromStatus)
		}
	}
}

// TestTheEntryCarriesTheProvidersHistory is the second half of the *Done when*, and it is the half a
// queue of job identifiers would not meet.
//
// One cancellation is an event. Whether it is a pattern is the question, and the pair of counts is
// the answer: a provider with three cancellations against four hundred completions is not the
// provider with three against none, and a single figure cannot tell them apart.
func TestTheEntryCarriesTheProvidersHistory(t *testing.T) {
	queue, pool, customer, provider := cancellationFixture(t)

	// A second provider, so the counts are demonstrably *per provider* rather than platform-wide
	// — which is the way this fails while still looking plausible.
	other := newAccount(t, pool, "cancel-other@example.com", "0419200158", "provider")

	first := returnedToMarket(t, pool, customer, provider, jobs.StatusAwarded,
		"The first of this provider's two cancellations, recorded here.")
	second := endedAfterAward(t, pool, customer, provider,
		"The second of this provider's two cancellations, recorded here.")
	theirs := returnedToMarket(t, pool, customer, other, jobs.StatusAwarded,
		"A different provider's only cancellation, recorded here.")

	// One completed job for the first provider, and none for the second.
	completed := newDraft(t, pool, customer)
	moveJob(t, pool, completed, jobs.User(jobs.ActorCustomer, customer),
		jobs.StatusOpen, jobs.StatusAwarded)
	acceptBid(t, pool, completed, provider)
	moveJob(t, pool, completed, jobs.User(jobs.ActorProvider, provider),
		jobs.StatusDriverAssigned, jobs.StatusEnRouteToPickup, jobs.StatusPickedUp,
		jobs.StatusInTransit, jobs.StatusDelivered)
	moveJob(t, pool, completed, jobs.User(jobs.ActorCustomer, customer), jobs.StatusCompleted)

	entries, err := queue.AfterAward(t.Context(), QueueQuery{Limit: 100})
	if err != nil {
		t.Fatalf("reading the queue: %v", err)
	}
	found := entriesByJob(entries)

	for _, jobID := range []uuid.UUID{first, second} {
		entry, ok := found[jobID]
		if !ok {
			t.Fatalf("%s is not in the queue", jobID)
		}
		if entry.ProviderCancellations != 2 {
			t.Errorf("provider_cancellations = %d, want 2 — this provider has two, and the "+
				"count is what makes one cancellation readable as a pattern or not",
				entry.ProviderCancellations)
		}
		if entry.ProviderCompletions != 1 {
			t.Errorf("provider_completions = %d, want 1 — without the denominator, three "+
				"cancellations against four hundred deliveries looks like three against none",
				entry.ProviderCompletions)
		}
	}

	theirEntry, ok := found[theirs]
	if !ok {
		t.Fatalf("%s is not in the queue", theirs)
	}
	if theirEntry.ProviderCancellations != 1 {
		t.Errorf("the second provider's count is %d, want 1 — the counts are per provider, and "+
			"a platform-wide count would look right on the first provider and wrong here",
			theirEntry.ProviderCancellations)
	}
	if theirEntry.ProviderCompletions != 0 {
		t.Errorf("the second provider's completions = %d, want 0", theirEntry.ProviderCompletions)
	}
}

// TestTheCancellationQueueIsNewestFirstAndPagesWithoutSkippingOrRepeating.
//
// Newest first, unlike the exception queue: Docs/04 §8's acknowledgement targets are about deliveries
// going wrong, and a cancellation is already over. The paging half is what matters — an entry a
// cursor hides is an entry nobody sees.
func TestTheCancellationQueueIsNewestFirstAndPagesWithoutSkippingOrRepeating(t *testing.T) {
	queue, pool, customer, provider := cancellationFixture(t)

	written := map[uuid.UUID]bool{}
	for i := 0; i < 5; i++ {
		written[returnedToMarket(t, pool, customer, provider, jobs.StatusAwarded,
			"One of five cancellations written for the paging check.")] = true
	}

	seen := map[uuid.UUID]int{}
	var previous CancellationEntry
	var cursor QueueCursor

	for page := 0; page < 20; page++ {
		entries, err := queue.AfterAward(t.Context(), QueueQuery{Limit: 2, After: cursor})
		if err != nil {
			t.Fatalf("page %d: %v", page, err)
		}
		if len(entries) == 0 {
			break
		}

		for _, e := range entries {
			if !previous.CancelledAt.IsZero() && e.CancelledAt.After(previous.CancelledAt) {
				t.Errorf("the queue is not newest first: %s came after %s",
					e.CancelledAt, previous.CancelledAt)
			}
			previous = e
			seen[e.JobID]++
		}

		last := entries[len(entries)-1]
		cursor = QueueCursor{RecordedAt: last.CancelledAt, EntryID: last.TransitionID}
	}

	for id := range written {
		switch seen[id] {
		case 0:
			t.Errorf("%s was never returned — paging hid an entry", id)
		case 1:
		default:
			t.Errorf("%s was returned %d times", id, seen[id])
		}
	}
}

// TestTheCancellationShapeCarriesNothingCommercial.
//
// A closed key set rather than a search for the word "budget": SHIP-83 established that a field
// called `max_price` passes that search and leaks the same fact. This asserts what the shape *has*,
// so a field added for a screen that wanted it fails here rather than in review — and the field this
// queue would most plausibly acquire is the accepted bid's amount.
func TestTheCancellationShapeCarriesNothingCommercial(t *testing.T) {
	queue, pool, customer, provider := cancellationFixture(t)
	returnedToMarket(t, pool, customer, provider, jobs.StatusAwarded,
		"A cancellation written so the response shape has something in it.")

	entries, err := queue.AfterAward(t.Context(), QueueQuery{Limit: 5})
	if err != nil {
		t.Fatalf("reading the queue: %v", err)
	}
	if len(entries) == 0 {
		t.Fatal("no entries to check the shape of")
	}

	encoded, err := json.Marshal(cancellationEntryFrom(entries[0]))
	if err != nil {
		t.Fatalf("encoding an entry: %v", err)
	}

	var keys map[string]json.RawMessage
	if err := json.Unmarshal(encoded, &keys); err != nil {
		t.Fatalf("decoding an entry: %v", err)
	}

	permitted := map[string]bool{
		"job_id": true, "transition_id": true, "outcome": true, "provider_id": true,
		"from_status": true, "actor_type": true, "reason": true, "cancelled_at": true,
		"provider_cancellations": true, "provider_completions": true,
	}

	var unexpected []string
	for key := range keys {
		if !permitted[key] {
			unexpected = append(unexpected, key)
		}
	}
	slices.Sort(unexpected)

	if len(unexpected) != 0 {
		t.Errorf("a cancellation entry carries %v.\nThis shape is a closed set. A customer's "+
			"budget is never exposed in any form (Docs/01 §4.3), and the accepted bid's amount "+
			"is a commercial fact belonging to SHIP-152's console — if a field belongs here, "+
			"add it to the permitted set deliberately.", unexpected)
	}
	for key := range permitted {
		if _, ok := keys[key]; !ok {
			t.Errorf("a cancellation entry is missing %q", key)
		}
	}
}

// TestTheCancellationQueueNeedsAnAdministratorAndAPool.
//
// The two failures a queue has that nothing else notices: an endpoint anybody can read, and an
// endpoint that answers "nothing here" because its database is gone.
func TestTheCancellationQueueNeedsAnAdministratorAndAPool(t *testing.T) {
	t.Run("an unreachable database says so rather than reporting an empty queue", func(t *testing.T) {
		queue, err := NewCancellations(testCancellationQueue{}, nil)
		if err != nil {
			t.Fatalf("building a queue with no pool: %v", err)
		}

		entries, err := queue.AfterAward(t.Context(), QueueQuery{Limit: 20})
		if err == nil {
			t.Fatalf("an unreachable database reported %d entries rather than failing", len(entries))
		}
	})

	t.Run("a queue with no source is refused at construction", func(t *testing.T) {
		if _, err := NewCancellations(nil, nil); err == nil {
			t.Error("a cancellation queue was built with no source, so it would report an " +
				"empty queue for ever")
		}
	})

	t.Run("the endpoint refuses a request with no administrator session", func(t *testing.T) {
		f := newAuditFixture(t)

		req := httptest.NewRequest(http.MethodGet, "/v1/admin/moderation/cancellations", nil)
		rec := httptest.NewRecorder()
		RequireAdmin(f.auth)(f.handler.CancellationQueue()).ServeHTTP(rec, req)

		if rec.Code != http.StatusUnauthorized {
			t.Errorf("status = %d, want 401 (%s)", rec.Code, rec.Body)
		}
	})
}

// TestACancellationCursorFromAnotherEndpointIsRefused.
//
// A cursor this endpoint did not issue is a `400` rather than an empty page: a client that sent one
// has a bug, and answering "nothing here" would let it page for ever through a queue it never saw.
// The exception queue's cursor has three fields and this one has two, so each is the other's most
// likely wrong cursor.
func TestACancellationCursorFromAnotherEndpointIsRefused(t *testing.T) {
	f := newAuditFixture(t)
	_, token := f.signedIn(t, "cancel-reader@example.com", RoleSupport, "10.0.60.9")

	threeFields := pagination.Cursor{
		"2026-08-14T02:15:30.000Z", "failed_proof", uuid.Nil.String(),
	}.Encode()

	req := httptest.NewRequest(http.MethodGet,
		"/v1/admin/moderation/cancellations?cursor="+threeFields, nil)
	req.Header.Set(httpx.HeaderAuthorization, "Bearer "+token)

	rec := httptest.NewRecorder()
	RequireAdmin(f.auth)(f.handler.CancellationQueue()).ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400 for a cursor from the exception queue (%s)",
			rec.Code, rec.Body)
	}
}

// TestTheCancellationDoubleRunsTheStatementCmdApiRuns.
//
// [testCancellationQueue] is a copy of a query and a copy can drift, which would make every test
// here about a statement nothing serves. This reads cmd/api/routes_admin.go, resolves the
// `postAwardStatuses` constant by name, and compares the predicate, join and ordering lines — the
// same guard [TestTheTestDoubleRunsTheStatementCmdApiRuns] applies to the exception queue, and it
// was mutation-tested there.
func TestTheCancellationDoubleRunsTheStatementCmdApiRuns(t *testing.T) {
	source, err := os.ReadFile(filepath.Join("..", "..", "cmd", "api", "routes_admin.go"))
	if err != nil {
		t.Fatalf("reading cmd/api/routes_admin.go: %v", err)
	}

	production := extractStatement(t, string(source),
		"WITH cancellations AS (", "LIMIT $3")

	if got, want := sqlSkeleton(production), sqlSkeleton(testCancellationQuery); !slices.Equal(got, want) {
		t.Errorf("the two statements have drifted, so every test in this file is about a query "+
			"nothing serves.\n\ncmd/api:\n  %s\n\nthis package's copy:\n  %s",
			strings.Join(got, "\n  "), strings.Join(want, "\n  "))
	}
}
