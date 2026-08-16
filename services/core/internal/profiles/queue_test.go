package profiles

import (
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/DulsaraNethmin/Shipper/services/core/internal/testsupport/pgtest"
)

// SHIP-153's read half, against a real PostgreSQL.
//
// The *Done when* is "pending provider verifications listed oldest first", and every clause of it is
// a claim about a query rather than about a shape: which providers appear, in what order, and what
// happens to one whose state moves. A test double for the store could satisfy none of them.

// submittedAt backdates a verification record so that an ordering assertion is deterministic.
//
// `created_at` defaults to `now()`, which is transaction start time — three inserts in three
// statements are three transactions and *usually* order the way they were written. "Usually" is not
// an ordering, and a test that relied on it would be the fixture-shaped failure wave 10 recorded:
// green because the data happened to agree, silent the day it did not.
//
// The UPDATE is permitted because `provider_verification_change_is_guarded` has an opinion about one
// column — `state` — and passes everything else straight through, which `000200` says in as many
// words.
func submittedAt(t *testing.T, pool *pgxpool.Pool, provider uuid.UUID, at time.Time) {
	t.Helper()

	if _, err := pool.Exec(t.Context(),
		`UPDATE provider_verifications SET created_at = $2 WHERE provider_id = $1`, provider, at); err != nil {
		t.Fatalf("backdating %s: %v", provider, err)
	}
}

func queueOf(t *testing.T, pool *pgxpool.Pool, q QueueQuery) []QueueEntry {
	t.Helper()

	entries, err := newTestService().AwaitingReview(t.Context(), pool, q)
	if err != nil {
		t.Fatalf("AwaitingReview(%+v) = %v", q, err)
	}
	return entries
}

// TestThePendingQueueIsOldestFirst is SHIP-153's *Done when*, read literally.
//
// Three providers registered in a known order, one of them decided. The queue must list the two that
// are still Pending, longest-waiting first, and must not list the one that has been dealt with.
func TestThePendingQueueIsOldestFirst(t *testing.T) {
	pool := pgtest.DB(t)
	admin := newAdmin(t, pool, "pq-reviewer@example.com")

	base := time.Date(2026, 6, 1, 9, 0, 0, 0, time.UTC)

	oldest := newProvider(t, pool, "pq-oldest@example.com", "+61400300001")
	middle := newProvider(t, pool, "pq-middle@example.com", "+61400300002")
	newest := newProvider(t, pool, "pq-newest@example.com", "+61400300003")

	// Deliberately backdated in an order that is not the insertion order, so that a queue reading
	// the primary key or the insertion sequence rather than the clock fails here.
	submittedAt(t, pool, newest, base.Add(3*time.Hour))
	submittedAt(t, pool, oldest, base)
	submittedAt(t, pool, middle, base.Add(time.Hour))

	entries := queueOf(t, pool, QueueQuery{State: StatePending, Limit: 10})
	if len(entries) != 3 {
		t.Fatalf("the Pending queue holds %d providers, want 3: %+v", len(entries), entries)
	}
	want := []uuid.UUID{oldest, middle, newest}
	for i, id := range want {
		if entries[i].ProviderID != id {
			t.Errorf("position %d is %s, want %s — the queue is not oldest first", i, entries[i].ProviderID, id)
		}
		if entries[i].State != StatePending {
			t.Errorf("position %d reads %q on the Pending queue", i, entries[i].State)
		}
	}

	// A decided provider leaves the queue, which is the half a list of *every* provider would get
	// wrong while looking right.
	if _, err := decide(t, pool, middle, StateVerified, Actor{Type: ActorAdmin, ID: admin},
		"Licence, registration and insurance all current."); err != nil {
		t.Fatalf("verifying the middle provider: %v", err)
	}

	entries = queueOf(t, pool, QueueQuery{State: StatePending, Limit: 10})
	if len(entries) != 2 {
		t.Fatalf("the Pending queue holds %d providers after one decision, want 2", len(entries))
	}
	for _, e := range entries {
		if e.ProviderID == middle {
			t.Error("a provider who has been decided is still on the Pending queue")
		}
	}

	// And is findable on the queue for the state they moved to. Docs/04 §5's first queue is "new or
	// **changed**" submissions, so the state is the caller's to choose rather than a constant.
	verified := queueOf(t, pool, QueueQuery{State: StateVerified, Limit: 10})
	if len(verified) != 1 || verified[0].ProviderID != middle {
		t.Errorf("the Verified queue is %+v, want just %s", verified, middle)
	}
}

// TestTheQueueCarriesWhoTheProviderIsAndNothingCommercial.
//
// A reviewer working Docs/04 §3 checks that the name on a licence matches the account, so the entry
// carries the account's own details. It must carry nothing from the trading side of the platform:
// Docs/01 §4.3 is absolute about a customer's budget, and the surest way to keep an administrative
// shape clear of one is for it never to have joined anything that has one.
func TestTheQueueCarriesWhoTheProviderIsAndNothingCommercial(t *testing.T) {
	pool := pgtest.DB(t)

	provider := newProvider(t, pool, "pq-details@example.com", "+61400300004")
	if _, err := pool.Exec(t.Context(),
		`UPDATE users SET name = 'Dulsara Haulage' WHERE id = $1`, provider); err != nil {
		t.Fatalf("naming the provider: %v", err)
	}

	entries := queueOf(t, pool, QueueQuery{State: StatePending, Limit: 10})
	if len(entries) != 1 {
		t.Fatalf("the queue holds %d entries, want 1", len(entries))
	}

	e := entries[0]
	if e.Name != "Dulsara Haulage" {
		t.Errorf("the entry names %q", e.Name)
	}
	if e.Email != "pq-details@example.com" {
		t.Errorf("the entry carries the address %q", e.Email)
	}
	if e.Phone != "+61400300004" {
		t.Errorf("the entry carries the number %q", e.Phone)
	}
	if e.SubmittedAt.IsZero() {
		t.Error("the entry has no submission clock, so 'oldest first' is not answerable from it")
	}
}

// TestAProviderWithNoNameIsStillOnTheQueue.
//
// `000006` made `users.name` nullable deliberately — an account created before it has none and a
// name cannot be backfilled. A queue that inner-joined on a name, or that scanned it into a string
// without coalescing, would drop exactly the oldest providers on the platform, which are the ones
// that have been waiting longest.
func TestAProviderWithNoNameIsStillOnTheQueue(t *testing.T) {
	pool := pgtest.DB(t)
	provider := newProvider(t, pool, "pq-noname@example.com", "+61400300005")

	entries := queueOf(t, pool, QueueQuery{State: StatePending, Limit: 10})
	if len(entries) != 1 || entries[0].ProviderID != provider {
		t.Fatalf("the queue is %+v, want the one provider with no name", entries)
	}
	if entries[0].Name != "" {
		t.Errorf("a provider who supplied no name reads %q, want the empty string", entries[0].Name)
	}
}

// TestTheQueueCursorNeitherSkipsNorRepeats.
//
// Two providers whose records were created in the same instant, which is what makes a single-column
// cursor wrong: paging by `created_at` alone either returns the same row twice or loses one, and on
// a review queue a lost row is a person nobody looks at.
func TestTheQueueCursorNeitherSkipsNorRepeats(t *testing.T) {
	pool := pgtest.DB(t)

	same := time.Date(2026, 6, 2, 10, 0, 0, 0, time.UTC)
	first := newProvider(t, pool, "pq-tie-a@example.com", "+61400300006")
	second := newProvider(t, pool, "pq-tie-b@example.com", "+61400300007")
	third := newProvider(t, pool, "pq-tie-c@example.com", "+61400300008")
	for _, id := range []uuid.UUID{first, second, third} {
		submittedAt(t, pool, id, same)
	}

	var seen []uuid.UUID
	cursor := QueueCursor{}
	for range 3 {
		page := queueOf(t, pool, QueueQuery{State: StatePending, Limit: 1, After: cursor})
		if len(page) != 1 {
			t.Fatalf("a page of one returned %d entries", len(page))
		}
		seen = append(seen, page[0].ProviderID)
		cursor = QueueCursor{SubmittedAt: page[0].SubmittedAt, ProviderID: page[0].ProviderID}
	}

	if page := queueOf(t, pool, QueueQuery{State: StatePending, Limit: 1, After: cursor}); len(page) != 0 {
		t.Errorf("a fourth page returned %d entries, want none", len(page))
	}

	distinct := map[uuid.UUID]bool{}
	for _, id := range seen {
		if distinct[id] {
			t.Errorf("%s was returned twice while paging", id)
		}
		distinct[id] = true
	}
	for _, id := range []uuid.UUID{first, second, third} {
		if !distinct[id] {
			t.Errorf("%s was never returned; paging skipped it", id)
		}
	}
}

// TestAnUnrecognisedStateIsRefusedRatherThanAnsweredEmpty.
//
// An empty page and "nobody is waiting" are the same answer to somebody who mistyped, and on this
// queue the cost of that confusion is a provider who is never reviewed.
func TestAnUnrecognisedStateIsRefusedRatherThanAnsweredEmpty(t *testing.T) {
	pool := pgtest.DB(t)
	newProvider(t, pool, "pq-badstate@example.com", "+61400300009")

	for _, state := range []State{"", "Approved", "pending"} {
		if _, err := newTestService().AwaitingReview(t.Context(), pool,
			QueueQuery{State: state, Limit: 10}); !errors.Is(err, ErrStateUnrecognised) {
			t.Errorf("AwaitingReview(%q) = %v, want ErrStateUnrecognised", state, err)
		}
	}
}

// TestACustomerIsNeverOnTheQueue.
//
// The trigger that creates a record is conditional on the role, and this is the read that depends on
// it: most accounts on this platform are customers, and a queue holding them is a queue of people
// nobody will ever review.
func TestACustomerIsNeverOnTheQueue(t *testing.T) {
	pool := pgtest.DB(t)

	customer := newAccount(t, pool, "pq-customer@example.com", "+61400300010", "customer")
	provider := newProvider(t, pool, "pq-provider@example.com", "+61400300011")

	entries := queueOf(t, pool, QueueQuery{State: StatePending, Limit: 10})
	if len(entries) != 1 || entries[0].ProviderID != provider {
		t.Fatalf("the queue is %+v, want just the provider %s", entries, provider)
	}
	for _, e := range entries {
		if e.ProviderID == customer {
			t.Error("a customer is on the provider verification queue")
		}
	}
}
