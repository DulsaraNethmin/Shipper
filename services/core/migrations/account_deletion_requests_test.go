package migrations_test

import (
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/DulsaraNethmin/Shipper/services/core/internal/identity"
	"github.com/DulsaraNethmin/Shipper/services/core/internal/testsupport/pgtest"
)

// SHIP-169's schema, against a real PostgreSQL per Docs/06 §4.1.
//
// Every claim here is about a constraint — a partial unique index, a CHECK, a foreign key's
// ON DELETE behaviour — so a mocked store would report all of them passing while proving none.
//
// quotedLiteral and newUser come from jobs_test.go and schema_test.go respectively.

// requestDeletion writes one open request the way the domain does, and returns its identifier.
func requestDeletion(t *testing.T, pool *pgxpool.Pool, user uuid.UUID, at time.Time) uuid.UUID {
	t.Helper()

	id, err := uuid.NewV7()
	if err != nil {
		t.Fatalf("generating an id: %v", err)
	}

	if _, err := pool.Exec(t.Context(), `
		INSERT INTO account_deletion_requests (id, user_id, state, requested_at, complete_by)
		VALUES ($1, $2, 'requested', $3, $4)`,
		id, user, at, at.Add(identity.DeletionWindow)); err != nil {
		t.Fatalf("recording a deletion request: %v", err)
	}
	return id
}

// TestDeletionStateConstraintMatchesTheGoConstants is Docs/10 §3.4's pairing, in both directions.
//
// It matters more here than usual, because the list is *expected to grow on other branches*:
// 000105 names 'deferred' as SHIP-170's and 'completed' as SHIP-171's. Widening the CHECK without
// the constant is a state nothing in Go can render; adding the constant without the CHECK is a
// state the database refuses at the moment the ticket tries to write it.
func TestDeletionStateConstraintMatchesTheGoConstants(t *testing.T) {
	pool := pgtest.DB(t)

	inGo := map[string]bool{}
	for _, state := range identity.DeletionStates() {
		inGo[string(state)] = true
	}
	if len(inGo) != len(identity.DeletionStates()) {
		t.Errorf("identity.DeletionStates() contains a duplicate: %d constants, %d distinct values",
			len(identity.DeletionStates()), len(inGo))
	}

	var definition string
	if err := pool.QueryRow(t.Context(),
		`SELECT pg_get_constraintdef(oid) FROM pg_constraint WHERE conname = $1`,
		"ck_account_deletion_requests_state").Scan(&definition); err != nil {
		t.Fatalf("reading ck_account_deletion_requests_state: %v", err)
	}

	inSQL := map[string]bool{}
	for _, match := range quotedLiteral.FindAllStringSubmatch(definition, -1) {
		inSQL[match[1]] = true
	}

	for state := range inGo {
		if !inSQL[state] {
			t.Errorf("%q is an identity.DeletionState and ck_account_deletion_requests_state refuses it", state)
		}
	}
	for state := range inSQL {
		if !inGo[state] {
			t.Errorf("ck_account_deletion_requests_state accepts %q and no Go constant names it", state)
		}
	}
}

// TestAnUnknownDeletionStateIsRefused proves the CHECK is real rather than decorative.
func TestAnUnknownDeletionStateIsRefused(t *testing.T) {
	pool := pgtest.DB(t)
	user := newUser(t, pool, "deletion-state@example.com", "+61400000700", "customer")

	id, _ := uuid.NewV7()
	now := time.Now().UTC()
	_, err := pool.Exec(t.Context(), `
		INSERT INTO account_deletion_requests (id, user_id, state, requested_at, complete_by)
		VALUES ($1, $2, $3, $4, $5)`,
		id, user, "cancelled", now, now.Add(identity.DeletionWindow))
	if err == nil {
		t.Fatal("'cancelled' was accepted as a deletion state; withdrawing a request has no ticket " +
			"and inventing the state here would invent the product decision with it")
	}
	if !strings.Contains(err.Error(), "ck_account_deletion_requests_state") {
		t.Errorf("expected ck_account_deletion_requests_state to refuse it, got: %v", err)
	}
}

// TestOneOpenDeletionRequestPerAccount is the partial unique index, and why it is partial.
//
// Two rows would be two promises about one account, and whichever the execution happened to read
// would be the one that counted. The index is what makes "the open request" unambiguous rather
// than a convention the application keeps.
func TestOneOpenDeletionRequestPerAccount(t *testing.T) {
	pool := pgtest.DB(t)

	user := newUser(t, pool, "deletion-one@example.com", "+61400000701", "customer")
	requestDeletion(t, pool, user, time.Now().UTC())

	t.Run("a second open request is refused", func(t *testing.T) {
		id, _ := uuid.NewV7()
		now := time.Now().UTC()
		_, err := pool.Exec(t.Context(), `
			INSERT INTO account_deletion_requests (id, user_id, state, requested_at, complete_by)
			VALUES ($1, $2, 'requested', $3, $4)`,
			id, user, now, now.Add(identity.DeletionWindow))
		if err == nil {
			t.Fatal("one account now holds two open deletion requests, so it has been promised " +
				"two different completion dates")
		}
		if !strings.Contains(err.Error(), "uq_account_deletion_requests_open") {
			t.Errorf("expected uq_account_deletion_requests_open to refuse it, got: %v", err)
		}
	})

	t.Run("another account is unaffected", func(t *testing.T) {
		other := newUser(t, pool, "deletion-two@example.com", "+61400000702", "provider")
		requestDeletion(t, pool, other, time.Now().UTC())
	})
}

// TestTheCompletionDateMustBeInTheFuture is 000105's weak CHECK, and what it is weak on purpose
// about.
//
// It says nothing about thirty days — that window is a product decision, and a constraint
// encoding it would make moving it a migration. What it catches is the shape an implementation
// that stopped recording the date takes from the database's side: a zero value, or the wrong
// variable, written into a column that has to hold a promise.
func TestTheCompletionDateMustBeInTheFuture(t *testing.T) {
	pool := pgtest.DB(t)
	user := newUser(t, pool, "deletion-past@example.com", "+61400000703", "customer")

	id, _ := uuid.NewV7()
	now := time.Now().UTC()
	_, err := pool.Exec(t.Context(), `
		INSERT INTO account_deletion_requests (id, user_id, state, requested_at, complete_by)
		VALUES ($1, $2, 'requested', $3, $4)`,
		id, user, now, now.Add(-time.Hour))
	if err == nil {
		t.Fatal("a request was accepted promising completion before it was made")
	}
	if !strings.Contains(err.Error(), "ck_account_deletion_requests_complete_by") {
		t.Errorf("expected ck_account_deletion_requests_complete_by to refuse it, got: %v", err)
	}
}

// TestTheCompletionDateCannotBeOmitted is the NOT NULL doing the work the column exists for.
//
// An implementation that computes the completion date while rendering the response has no value to
// insert. This is the layer that turns that from "an answer that looks right and drifts" into a
// write that fails.
func TestTheCompletionDateCannotBeOmitted(t *testing.T) {
	pool := pgtest.DB(t)
	user := newUser(t, pool, "deletion-null@example.com", "+61400000704", "customer")

	id, _ := uuid.NewV7()
	_, err := pool.Exec(t.Context(), `
		INSERT INTO account_deletion_requests (id, user_id, state, requested_at)
		VALUES ($1, $2, 'requested', $3)`, id, user, time.Now().UTC())
	if err == nil {
		t.Fatal("a deletion request was stored with no completion date, so the platform holds no " +
			"record of what it promised")
	}
	if !strings.Contains(err.Error(), "complete_by") {
		t.Errorf("expected the NOT NULL on complete_by to refuse it, got: %v", err)
	}
}

// TestDeletingTheAccountCannotRemoveItsRequest is the ON DELETE RESTRICT, checked against the one
// event it exists for.
//
// Docs/05 §3.1 keeps the account row and replaces the person, so nothing legitimate ever removes
// the parent — which is exactly why a cascade here would be invisible until the day somebody ran
// the DELETE the whole design says not to run, and took the evidence with it.
func TestDeletingTheAccountCannotRemoveItsRequest(t *testing.T) {
	pool := pgtest.DB(t)

	user := newUser(t, pool, "deletion-restrict@example.com", "+61400000705", "customer")
	request := requestDeletion(t, pool, user, time.Now().UTC())

	if _, err := pool.Exec(t.Context(), `DELETE FROM users WHERE id = $1`, user); err == nil {
		t.Fatal("the account was removed, taking the record of its own deletion request with it")
	}

	var state string
	if err := pool.QueryRow(t.Context(),
		`SELECT state FROM account_deletion_requests WHERE id = $1`, request).Scan(&state); err != nil {
		t.Fatalf("reading the request back: %v", err)
	}
	if state != string(identity.DeletionRequested) {
		t.Errorf("state = %q, want %q", state, identity.DeletionRequested)
	}
}
