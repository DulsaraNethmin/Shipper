package main

import (
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/DulsaraNethmin/Shipper/services/core/internal/identity"
	"github.com/DulsaraNethmin/Shipper/services/core/internal/testsupport/pgtest"
)

// The second query in this binary, tested against a real database (SHIP-171b).
//
// parties_test.go's argument applies unchanged: the domain stubs this port, which is right —
// internal/notifications must not know that "has this account been deleted" is a read of identity's
// table — and what a stub cannot show is that the read is the correct one. The cases below are the
// ones that differ, and three of the four are states a stub would never distinguish.

// deletionRequest records one request in a given state, and answers with the account it belongs to.
//
// `complete_by` is required and is what the person was promised, so it is written rather than
// defaulted; nothing here reads it, and a fixture that left it out would fail the NOT NULL rather
// than the test.
func deletionRequest(t *testing.T, pool *pgxpool.Pool, email, phone string, state identity.DeletionState) uuid.UUID {
	t.Helper()

	user := uuid.Must(uuid.NewV7())
	if _, err := pool.Exec(t.Context(),
		`INSERT INTO users (id, email, phone, password_hash, role) VALUES ($1, $2, $3, 'x', 'customer')`,
		user, email, phone); err != nil {
		t.Fatalf("inserting %s: %v", email, err)
	}

	if _, err := pool.Exec(t.Context(), `
		INSERT INTO account_deletion_requests (id, user_id, state, requested_at, complete_by)
		VALUES ($1, $2, $3, now(), now() + interval '30 days')`,
		uuid.Must(uuid.NewV7()), user, state); err != nil {
		t.Fatalf("recording a %s deletion request: %v", state, err)
	}
	return user
}

// TestDeletedAccountsIsCompletedRequestsAndNothingElse.
//
// # The two open states are the cases that matter, and they are not symmetry
//
// A person inside their thirty days is still a person. Docs/05 §3.1 gives them the window, and a
// platform that stopped notifying them the moment they asked would deny them exactly the messages
// about a delivery in flight that the window exists to let them finish — while `requested` and
// `deferred` are the states SHIP-170 and SHIP-171 move a request through before anything has been
// erased at all. A predicate of `state <> 'requested'`, or one that forgot a state name, would pass
// a test that only checked "completed is deleted".
func TestDeletedAccountsIsCompletedRequestsAndNothingElse(t *testing.T) {
	pool := pgtest.DB(t)

	completed := deletionRequest(t, pool, "del-completed@example.com", "+61400960001",
		identity.DeletionCompleted)
	requested := deletionRequest(t, pool, "del-requested@example.com", "+61400960002",
		identity.DeletionRequested)
	deferred := deletionRequest(t, pool, "del-deferred@example.com", "+61400960003",
		identity.DeletionDeferred)

	never := uuid.Must(uuid.NewV7())
	if _, err := pool.Exec(t.Context(),
		`INSERT INTO users (id, email, phone, password_hash, role) VALUES ($1, $2, $3, 'x', 'customer')`,
		never, "del-never@example.com", "+61400960004"); err != nil {
		t.Fatalf("inserting the account that never asked: %v", err)
	}

	deleted, err := deletedAccountLookup{}.DeletedAccounts(t.Context(), pool,
		[]uuid.UUID{completed, requested, deferred, never})
	if err != nil {
		t.Fatalf("reading which accounts have been deleted: %v", err)
	}

	for _, tc := range []struct {
		name string
		id   uuid.UUID
		want bool
	}{
		{name: "a completed request is a deleted account", id: completed, want: true},
		{name: "a request inside its thirty days is not", id: requested, want: false},
		{name: "a request deferred behind a delivery is not", id: deferred, want: false},
		{name: "an account that never asked is not", id: never, want: false},
	} {
		if got := deleted[tc.id]; got != tc.want {
			t.Errorf("%s: got %v, want %v", tc.name, got, tc.want)
		}
	}

	// Exactly one, so a predicate matching everything fails here rather than passing three of
	// the four assertions above by accident.
	if len(deleted) != 1 {
		t.Errorf("the lookup reported %d deleted account(s) out of four, want 1", len(deleted))
	}
}

// TestDeletedAccountsAnswersNothingForNoIdentifiers.
//
// The consumer calls this with whatever a rule resolved, which is legitimately empty — a rule that
// tells nobody, or every recipient suppressed already. `= ANY('{}')` is valid SQL and would be a
// round trip inside a transaction holding a Kafka partition's progress, for an answer that is known
// without asking.
func TestDeletedAccountsAnswersNothingForNoIdentifiers(t *testing.T) {
	pool := pgtest.DB(t)

	deleted, err := deletedAccountLookup{}.DeletedAccounts(t.Context(), pool, nil)
	if err != nil {
		t.Fatalf("reading for no identifiers: %v", err)
	}
	if len(deleted) != 0 {
		t.Errorf("the lookup reported %d account(s) for an empty request", len(deleted))
	}
}
