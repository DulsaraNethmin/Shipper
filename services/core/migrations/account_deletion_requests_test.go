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

// --- SHIP-170: 000106 widens the CHECK, and the index with it ---------------------------------

// deferDeletionAt writes one deferred request the way SHIP-170 does.
func deferDeletionAt(t *testing.T, pool *pgxpool.Pool, user uuid.UUID, at time.Time) uuid.UUID {
	t.Helper()

	id, err := uuid.NewV7()
	if err != nil {
		t.Fatalf("generating an id: %v", err)
	}

	if _, err := pool.Exec(t.Context(), `
		INSERT INTO account_deletion_requests (id, user_id, state, requested_at, complete_by)
		VALUES ($1, $2, 'deferred', $3, $4)`,
		id, user, at, at.Add(identity.DeletionWindow)); err != nil {
		t.Fatalf("recording a deferred deletion request: %v", err)
	}
	return id
}

// TestADeferredRequestIsStillAnOpenRequest is the half of 000106 a reader would miss.
//
// Widening `ck_account_deletion_requests_state` on its own would have left
// `uq_account_deletion_requests_open` partial on `state = 'requested'` — so the index would stop
// covering a row the instant it was deferred, and an account could hold one deferred request and
// one live one. **Two rows are two promises about one account**, which is the exact defect 000105
// built that index to prevent, and it would have been invisible until SHIP-171 read whichever of
// them it happened to find.
//
// Both orderings are checked. A predicate widened in one direction only — deferred blocks
// requested but not the reverse — passes half of this.
func TestADeferredRequestIsStillAnOpenRequest(t *testing.T) {
	pool := pgtest.DB(t)

	t.Run("a live request is refused while a deferred one is open", func(t *testing.T) {
		user := newUser(t, pool, "deletion-deferred-one@example.com", "+61400000706", "provider")
		deferDeletionAt(t, pool, user, time.Now().UTC())

		id, _ := uuid.NewV7()
		now := time.Now().UTC()
		_, err := pool.Exec(t.Context(), `
			INSERT INTO account_deletion_requests (id, user_id, state, requested_at, complete_by)
			VALUES ($1, $2, 'requested', $3, $4)`,
			id, user, now, now.Add(identity.DeletionWindow))
		if err == nil {
			t.Fatal("an account now holds a deferred request and a live one, so it has been " +
				"promised two different completion dates")
		}
		if !strings.Contains(err.Error(), "uq_account_deletion_requests_open") {
			t.Errorf("expected uq_account_deletion_requests_open to refuse it, got: %v", err)
		}
	})

	t.Run("a deferred request is refused while a live one is open", func(t *testing.T) {
		user := newUser(t, pool, "deletion-deferred-two@example.com", "+61400000707", "customer")
		requestDeletion(t, pool, user, time.Now().UTC())

		id, _ := uuid.NewV7()
		now := time.Now().UTC()
		_, err := pool.Exec(t.Context(), `
			INSERT INTO account_deletion_requests (id, user_id, state, requested_at, complete_by)
			VALUES ($1, $2, 'deferred', $3, $4)`,
			id, user, now, now.Add(identity.DeletionWindow))
		if err == nil {
			t.Fatal("an account now holds a live request and a deferred one")
		}
		if !strings.Contains(err.Error(), "uq_account_deletion_requests_open") {
			t.Errorf("expected uq_account_deletion_requests_open to refuse it, got: %v", err)
		}
	})

	t.Run("and another account is unaffected", func(t *testing.T) {
		other := newUser(t, pool, "deletion-deferred-three@example.com", "+61400000708", "provider")
		deferDeletionAt(t, pool, other, time.Now().UTC())
	})
}

// TestTheOpenStatesTheIndexCoversAreTheOnesTheDomainCallsOpen is Docs/10 §3.4's pairing applied to
// the index rather than to the CHECK.
//
// **SHIP-171 is what this exists for.** 'completed' is a state the domain must add to
// [identity.DeletionStates] and must keep *out* of the open set — a completed request is history and
// must not stop the same account asking again, which is 000105's own reason for making the index
// partial. Adding it to the index predicate would be a silent, permanent refusal, and nothing else
// in the build would report it.
func TestTheOpenStatesTheIndexCoversAreTheOnesTheDomainCallsOpen(t *testing.T) {
	pool := pgtest.DB(t)

	var definition string
	if err := pool.QueryRow(t.Context(),
		`SELECT indexdef FROM pg_indexes WHERE indexname = $1`,
		"uq_account_deletion_requests_open").Scan(&definition); err != nil {
		t.Fatalf("reading uq_account_deletion_requests_open: %v", err)
	}

	// The predicate alone. The indexed column is `user_id`, which is not a state and must not
	// be read as one.
	_, predicate, found := strings.Cut(definition, " WHERE ")
	if !found {
		t.Fatalf("the index is no longer partial, so a completed request would stop a later "+
			"one forever: %s", definition)
	}

	inSQL := map[string]bool{}
	for _, match := range quotedLiteral.FindAllStringSubmatch(predicate, -1) {
		inSQL[match[1]] = true
	}

	inGo := map[string]bool{}
	for _, state := range identity.OpenDeletionStates() {
		inGo[string(state)] = true
	}

	for state := range inGo {
		if !inSQL[state] {
			t.Errorf("%q is open to the domain and the index does not cover it — an account "+
				"could hold two open deletion requests", state)
		}
	}
	for state := range inSQL {
		if !inGo[state] {
			t.Errorf("the index covers %q and the domain does not call it open — an account "+
				"in that state could never ask again", state)
		}
	}
}
