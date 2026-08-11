package migrations_test

import (
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/DulsaraNethmin/Shipper/services/core/internal/events"
	"github.com/DulsaraNethmin/Shipper/services/core/internal/testsupport/pgtest"
)

// These run against a real PostgreSQL, per Docs/06 §4.1: "a mock happily accepts a write that
// the actual constraint would reject". Every assertion here is about a constraint, so a mocked
// database would pass all of them while proving nothing.

func newUser(t *testing.T, pool *pgxpool.Pool, email, phone, role string) uuid.UUID {
	t.Helper()

	id, err := uuid.NewV7()
	if err != nil {
		t.Fatalf("generating an id: %v", err)
	}

	_, err = pool.Exec(t.Context(),
		`INSERT INTO users (id, email, phone, password_hash, role) VALUES ($1, $2, $3, 'x', $4)`,
		id, email, phone, role)
	if err != nil {
		t.Fatalf("inserting %s: %v", email, err)
	}
	return id
}

// TestEmailUniquenessIgnoresCase is why 000001_init installs citext.
//
// With a plain text column both of these would insert, and the person who owns the address
// would have two accounts that are indistinguishable to them and distinct to us.
func TestEmailUniquenessIgnoresCase(t *testing.T) {
	pool := pgtest.DB(t)

	newUser(t, pool, "alice@example.com", "+61400000001", "customer")

	id, _ := uuid.NewV7()
	_, err := pool.Exec(t.Context(),
		`INSERT INTO users (id, email, phone, password_hash, role) VALUES ($1, $2, $3, 'x', 'customer')`,
		id, "Alice@Example.com", "+61400000002")
	if err == nil {
		t.Fatal("a second account was created for the same address in different case")
	}
	if !strings.Contains(err.Error(), "uq_users_email") {
		t.Errorf("expected the email uniqueness index to refuse it, got: %v", err)
	}
}

func TestPhoneIsUnique(t *testing.T) {
	pool := pgtest.DB(t)

	newUser(t, pool, "one@example.com", "+61400000003", "customer")

	id, _ := uuid.NewV7()
	_, err := pool.Exec(t.Context(),
		`INSERT INTO users (id, email, phone, password_hash, role) VALUES ($1, $2, $3, 'x', 'provider')`,
		id, "two@example.com", "+61400000003")
	if err == nil {
		t.Fatal("two accounts share a phone number, so an OTP cannot say which it verifies")
	}
}

// TestRoleAndStatusAreConstrained proves the CHECK constraints are real.
//
// Docs/10 §3.4 chooses text-plus-CHECK over a PostgreSQL enum type, and the trade is that the
// constraint has to be tested rather than assumed from the column type.
func TestRoleAndStatusAreConstrained(t *testing.T) {
	pool := pgtest.DB(t)

	t.Run("role", func(t *testing.T) {
		id, _ := uuid.NewV7()
		_, err := pool.Exec(t.Context(),
			`INSERT INTO users (id, email, phone, password_hash, role) VALUES ($1, 'r@example.com', '+61400000010', 'x', $2)`,
			id, "admin")
		if err == nil {
			t.Fatal("'admin' was accepted as a role; admin sign-in is a separate system (SHIP-147)")
		}
		if !strings.Contains(err.Error(), "ck_users_role") {
			t.Errorf("expected ck_users_role to refuse it, got: %v", err)
		}
	})

	t.Run("status", func(t *testing.T) {
		id, _ := uuid.NewV7()
		_, err := pool.Exec(t.Context(),
			`INSERT INTO users (id, email, phone, password_hash, role, status)
			 VALUES ($1, 's@example.com', '+61400000011', 'x', 'customer', $2)`,
			id, "deleted")
		if err == nil {
			t.Fatal("'deleted' was accepted as a status; deletion pseudonymises (Docs/05 §3.1)")
		}
	})
}

// TestRoleIsImmutable is SHIP-45's second half, checked where it is actually enforced.
//
// The first half — the role is chosen at registration — lives in internal/identity. This is the
// "and immutable thereafter" part, and it is a trigger rather than application logic for the
// same reason audit_log's append-only rule is: a rule the application keeps does not apply to a
// support query typed at a psql prompt, which is exactly the path that would be used to change
// somebody's role "just this once".
func TestRoleIsImmutable(t *testing.T) {
	pool := pgtest.DB(t)

	id := newUser(t, pool, "immutable@example.com", "+61400000020", "customer")

	t.Run("changing the role is refused", func(t *testing.T) {
		_, err := pool.Exec(t.Context(),
			`UPDATE users SET role = 'provider' WHERE id = $1`, id)
		if err == nil {
			t.Fatal("a customer became a provider, which silently rewrites the meaning of " +
				"every job already attached to the account")
		}
		if !strings.Contains(err.Error(), "immutable") {
			t.Errorf("expected the users_role_is_immutable trigger to refuse it, got: %v", err)
		}
	})

	t.Run("the role survived", func(t *testing.T) {
		var role string
		if err := pool.QueryRow(t.Context(),
			`SELECT role FROM users WHERE id = $1`, id).Scan(&role); err != nil {
			t.Fatalf("reading it back: %v", err)
		}
		if role != "customer" {
			t.Errorf("role = %q, want customer", role)
		}
	})

	// The trigger has a WHEN clause so it does not fire on the writes that happen constantly —
	// verification timestamps, account standing. If it were unconditional, every one of those
	// would raise, and the failure would look like the endpoint being broken rather than like
	// the trigger being wrong.
	t.Run("other columns still update", func(t *testing.T) {
		if _, err := pool.Exec(t.Context(),
			`UPDATE users SET status = 'restricted', email_verified_at = now() WHERE id = $1`,
			id); err != nil {
			t.Fatalf("an unrelated update was refused by the role trigger: %v", err)
		}
	})

	// Writing the same role is not a change, and refusing it would break any UPDATE that names
	// every column — which is what an ORM or a hand-written "save the whole row" does.
	t.Run("writing the same role is not a change", func(t *testing.T) {
		if _, err := pool.Exec(t.Context(),
			`UPDATE users SET role = 'customer' WHERE id = $1`, id); err != nil {
			t.Errorf("rewriting the same role was refused: %v", err)
		}
	})
}

// TestUpdatedAtIsMaintained proves the trigger is attached, not merely that the column exists.
func TestUpdatedAtIsMaintained(t *testing.T) {
	pool := pgtest.DB(t)

	id := newUser(t, pool, "touch@example.com", "+61400000004", "customer")

	var before time.Time
	if err := pool.QueryRow(t.Context(), `SELECT updated_at FROM users WHERE id = $1`, id).Scan(&before); err != nil {
		t.Fatalf("reading updated_at: %v", err)
	}

	// A separate transaction, because now() is transaction start time — within one
	// transaction the timestamp would legitimately not move.
	if _, err := pool.Exec(t.Context(),
		`UPDATE users SET status = 'restricted' WHERE id = $1`, id); err != nil {
		t.Fatalf("updating: %v", err)
	}

	var after time.Time
	if err := pool.QueryRow(t.Context(), `SELECT updated_at FROM users WHERE id = $1`, id).Scan(&after); err != nil {
		t.Fatalf("reading updated_at: %v", err)
	}

	if !after.After(before) {
		t.Errorf("updated_at did not move on update (%s then %s); is the trigger attached?", before, after)
	}
}

// TestEveryMutableTableHasItsUpdatedAtTrigger is a guard against the quietest schema defect
// there is.
//
// The column exists, is NOT NULL, and defaults correctly on insert — so a table whose trigger
// was forgotten looks completely healthy and simply never records a change. It stays invisible
// until someone asks "when was this last touched" months later and gets the creation time.
//
// Written as a sweep rather than per table so that a table added on another branch is covered
// the moment it merges, without anyone remembering to extend a list.
func TestEveryMutableTableHasItsUpdatedAtTrigger(t *testing.T) {
	pool := pgtest.DB(t)

	rows, err := pool.Query(t.Context(), `
		SELECT c.table_name
		FROM information_schema.columns c
		WHERE c.table_schema = 'public'
		  AND c.column_name = 'updated_at'
		  AND NOT EXISTS (
		      SELECT 1
		      FROM pg_trigger tr
		      JOIN pg_class cl ON cl.oid = tr.tgrelid
		      JOIN pg_proc p   ON p.oid  = tr.tgfoid
		      WHERE cl.relname = c.table_name
		        AND p.proname  = 'set_updated_at'
		        AND NOT tr.tgisinternal
		  )`)
	if err != nil {
		t.Fatalf("querying for untriggered tables: %v", err)
	}
	defer rows.Close()

	for rows.Next() {
		var table string
		if err := rows.Scan(&table); err != nil {
			t.Fatalf("scanning: %v", err)
		}
		t.Errorf("%s has an updated_at column but no set_updated_at trigger, so it will never "+
			"record a change; attach it as 000001_init documents", table)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterating: %v", err)
	}
}

// TestAuditLogIsAppendOnly is the invariant from Docs/04 §9 and CLAUDE.md, checked where it is
// actually enforced.
//
// Application code refusing to update an audit row is a convention. The trigger is the control,
// because it also applies to a support query typed at a psql prompt — which is precisely the
// path an administrator covering their tracks would use.
func TestAuditLogIsAppendOnly(t *testing.T) {
	pool := pgtest.DB(t)

	actor := newUser(t, pool, "admin-actor@example.com", "+61400000005", "customer")
	entry, _ := uuid.NewV7()
	target, _ := uuid.NewV7()

	if _, err := pool.Exec(t.Context(), `
		INSERT INTO audit_log (id, actor_type, actor_id, action, target_type, target_id, reason)
		VALUES ($1, 'admin', $2, 'job.unpublished', 'job', $3, 'prohibited goods')`,
		entry, actor, target); err != nil {
		t.Fatalf("appending an entry: %v", err)
	}

	t.Run("update is refused", func(t *testing.T) {
		_, err := pool.Exec(t.Context(),
			`UPDATE audit_log SET reason = 'something else' WHERE id = $1`, entry)
		if err == nil {
			t.Fatal("an audit entry was rewritten")
		}
		if !strings.Contains(err.Error(), "append-only") {
			t.Errorf("expected the append-only trigger to refuse it, got: %v", err)
		}
	})

	t.Run("delete is refused", func(t *testing.T) {
		_, err := pool.Exec(t.Context(), `DELETE FROM audit_log WHERE id = $1`, entry)
		if err == nil {
			t.Fatal("an audit entry was deleted")
		}
		if !strings.Contains(err.Error(), "append-only") {
			t.Errorf("expected the append-only trigger to refuse it, got: %v", err)
		}
	})

	t.Run("the entry survived both attempts", func(t *testing.T) {
		var reason string
		if err := pool.QueryRow(t.Context(),
			`SELECT reason FROM audit_log WHERE id = $1`, entry).Scan(&reason); err != nil {
			t.Fatalf("reading it back: %v", err)
		}
		if reason != "prohibited goods" {
			t.Errorf("reason = %q, want the original", reason)
		}
	})
}

// TestAuditLogRequiresAnActorThatMakesSense checks the paired constraint: an account-backed
// actor names its account, and 'system' does not pretend to have one.
func TestAuditLogRequiresAnActorThatMakesSense(t *testing.T) {
	pool := pgtest.DB(t)
	target, _ := uuid.NewV7()

	t.Run("an admin without an account is refused", func(t *testing.T) {
		id, _ := uuid.NewV7()
		_, err := pool.Exec(t.Context(), `
			INSERT INTO audit_log (id, actor_type, action, target_type, target_id)
			VALUES ($1, 'admin', 'user.suspended', 'user', $2)`, id, target)
		if err == nil {
			t.Error("an admin action was recorded with no administrator attached")
		}
	})

	t.Run("system with an account is refused", func(t *testing.T) {
		id, _ := uuid.NewV7()
		actor, _ := uuid.NewV7()
		_, err := pool.Exec(t.Context(), `
			INSERT INTO audit_log (id, actor_type, actor_id, action, target_type, target_id)
			VALUES ($1, 'system', $2, 'job.expired', 'job', $3)`, id, actor, target)
		if err == nil {
			t.Error("a system action claimed an account")
		}
	})

	t.Run("system without an account is accepted", func(t *testing.T) {
		id, _ := uuid.NewV7()
		if _, err := pool.Exec(t.Context(), `
			INSERT INTO audit_log (id, actor_type, action, target_type, target_id)
			VALUES ($1, 'system', 'job.expired', 'job', $2)`, id, target); err != nil {
			t.Errorf("the expiry sweep cannot record what it did: %v", err)
		}
	})
}

// TestOutboxAcceptsAnEvent exercises the writer against the real table, so that the column
// names in events.Emit and the ones in the migration cannot drift apart.
func TestOutboxAcceptsAnEvent(t *testing.T) {
	pool := pgtest.DB(t)

	jobID, _ := uuid.NewV7()
	occurred := time.Now().UTC().Truncate(time.Millisecond)

	event, err := events.New("job", jobID, "job.published", occurred, map[string]any{
		"job_id": jobID.String(),
	})
	if err != nil {
		t.Fatalf("building the event: %v", err)
	}

	if err := events.NewOutbox().Emit(t.Context(), pool, event); err != nil {
		t.Fatalf("emitting: %v", err)
	}

	var (
		aggregateType string
		eventType     string
		published     *time.Time
	)
	if err := pool.QueryRow(t.Context(),
		`SELECT aggregate_type, event_type, published_at FROM outbox WHERE id = $1`, event.ID,
	).Scan(&aggregateType, &eventType, &published); err != nil {
		t.Fatalf("reading it back: %v", err)
	}

	if aggregateType != "job" || eventType != "job.published" {
		t.Errorf("stored %s/%s, want job/job.published", aggregateType, eventType)
	}
	if published != nil {
		t.Error("a freshly written event is already marked published")
	}
}

// TestOutboxRefusesAnIncompleteEvent covers the checks in events.Emit itself. An event with no
// payload would otherwise reach jsonb and fail with a syntax error that says nothing about
// which emitter was at fault.
func TestOutboxRefusesAnIncompleteEvent(t *testing.T) {
	pool := pgtest.DB(t)
	outbox := events.NewOutbox()
	id, _ := uuid.NewV7()

	cases := map[string]events.Event{
		"no id":      {AggregateType: "job", AggregateID: id, Type: "job.published", Payload: []byte(`{}`)},
		"no type":    {ID: id, AggregateType: "job", AggregateID: id, Payload: []byte(`{}`)},
		"no payload": {ID: id, AggregateType: "job", AggregateID: id, Type: "job.published"},
	}

	for name, event := range cases {
		t.Run(name, func(t *testing.T) {
			if err := outbox.Emit(t.Context(), pool, event); err == nil {
				t.Error("an incomplete event was written")
			}
		})
	}
}
