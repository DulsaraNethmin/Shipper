package migrations_test

import (
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/DulsaraNethmin/Shipper/services/core/internal/jobs"
	"github.com/DulsaraNethmin/Shipper/services/core/internal/testsupport/pgtest"
	"github.com/DulsaraNethmin/Shipper/services/core/migrations"
)

// SHIP-56's table, checked where its guarantees live.
//
// Every assertion here is about a constraint, an index or a trigger, so all of them run against
// a real PostgreSQL — Docs/06 §4.1: "a mock happily accepts a write that the actual constraint
// would reject."

// newJob inserts one job at its starting status and returns its id.
//
// It names no status, which is the point: 'Draft' is the column default and 000402 refuses any
// insert that arrives at another status, so this is the only way a job is ever created.
func newJob(t *testing.T, pool *pgxpool.Pool, customer uuid.UUID) uuid.UUID {
	t.Helper()

	id, err := uuid.NewV7()
	if err != nil {
		t.Fatalf("generating an id: %v", err)
	}

	if _, err := pool.Exec(t.Context(),
		`INSERT INTO jobs (id, customer_id) VALUES ($1, $2)`, id, customer); err != nil {
		t.Fatalf("inserting a job for %s: %v", customer, err)
	}
	return id
}

// quotedLiteral pulls the string literals out of a constraint definition.
//
// PostgreSQL rewrites `status IN ('Draft', …)` as `status = ANY (ARRAY['Draft'::text, …])`, so
// what comes back from pg_get_constraintdef is not the text that was written. Reading the
// literals out of it is what makes the comparison against the Go constants meaningful rather
// than a string equality test that would fail on formatting.
var quotedLiteral = regexp.MustCompile(`'([^']*)'::text`)

// TestEveryJobStatusConstraintMatchesTheGoConstants is SHIP-56's acceptance criterion, checked
// in both directions and across all three constraints that hold the list.
//
// Docs/10 §3.4 requires every enumeration to be paired with a test that reads the constraint out
// of pg_constraint and asserts the PostgreSQL list equals the Go constant list. One direction
// alone is not enough: a status missing from the database is a job that cannot reach a state the
// product has, and a status missing from Go is a state the database will accept and no code
// knows how to handle.
//
// Three constraints rather than one because 000401 writes the list out again for from_status and
// to_status. That repetition is what this test makes safe — a status added to the jobs table and
// not to its history would be a transition the guard could never record.
func TestEveryJobStatusConstraintMatchesTheGoConstants(t *testing.T) {
	pool := pgtest.DB(t)

	inGo := map[string]bool{}
	for _, status := range jobs.Statuses {
		inGo[string(status)] = true
	}

	if len(jobs.Statuses) != 12 {
		t.Errorf("jobs.Statuses holds %d statuses; Docs/02 §1 has twelve", len(jobs.Statuses))
	}
	if len(inGo) != len(jobs.Statuses) {
		t.Errorf("jobs.Statuses contains a duplicate: %d constants, %d distinct values",
			len(jobs.Statuses), len(inGo))
	}

	for _, constraint := range []string{
		"ck_jobs_status",
		"ck_job_status_history_from_status",
		"ck_job_status_history_to_status",
	} {
		t.Run(constraint, func(t *testing.T) {
			var definition string
			if err := pool.QueryRow(t.Context(), `
				SELECT pg_get_constraintdef(oid)
				FROM pg_constraint
				WHERE conname = $1`, constraint,
			).Scan(&definition); err != nil {
				t.Fatalf("reading %s out of pg_constraint: %v", constraint, err)
			}

			inDatabase := map[string]bool{}
			for _, match := range quotedLiteral.FindAllStringSubmatch(definition, -1) {
				inDatabase[match[1]] = true
			}

			for status := range inGo {
				if !inDatabase[status] {
					t.Errorf("Go has the status %q and %s does not permit it; "+
						"the database would refuse a job that reached it", status, constraint)
				}
			}
			for status := range inDatabase {
				if !inGo[status] {
					t.Errorf("%s permits %q and Go has no constant for it; "+
						"the database would accept a state no code handles", constraint, status)
				}
			}

			// Named explicitly as well as compared, because both lists could drift
			// together if somebody "fixed" the spacing on one and then made the other
			// match. Docs/02 §1 is the authority for these exact strings, and
			// Docs/10 §3.4 for storing them unaltered.
			for _, want := range []string{"Driver assigned", "En route to pickup", "Picked up", "In transit"} {
				if !inDatabase[want] {
					t.Errorf("%s does not permit %q; Docs/02 §1 writes the statuses "+
						"with spaces and in sentence case, and Docs/10 §3.4 stores "+
						"them that way", constraint, want)
				}
			}
		})
	}
}

// TestAJobIsCreatedAsADraft covers the first half of 000402: the status column cannot be set on
// the way in either.
//
// Docs/02 §2 has one entry point into the lifecycle. A job inserted at 'Awarded' has no accepted
// bid behind it and one inserted at 'Delivered' has no proof, so creation is restricted to Draft
// and every other status is reached by a transition that was checked and recorded.
//
// ck_jobs_status is not what refuses the unknown status below — the trigger runs first, and
// gets there before the constraint is evaluated. What the constraint permits is asserted from
// its definition instead, in TestEveryJobStatusConstraintMatchesTheGoConstants.
func TestAJobIsCreatedAsADraft(t *testing.T) {
	pool := pgtest.DB(t)
	customer := newUser(t, pool, "creation-guard@example.com", "+61400000400", "customer")

	for _, status := range []string{"Open", "Awarded", "Delivered", "Pending"} {
		t.Run(status, func(t *testing.T) {
			id, _ := uuid.NewV7()
			_, err := pool.Exec(t.Context(),
				`INSERT INTO jobs (id, customer_id, status) VALUES ($1, $2, $3)`,
				id, customer, status)
			if err == nil {
				t.Fatalf("a job was created at %q, skipping every transition into it", status)
			}
			if !strings.Contains(err.Error(), "created as a Draft") {
				t.Errorf("expected the creation guard to refuse it, got: %v", err)
			}
		})
	}
}

// TestJobStatusIsNotASettableField is SHIP-57's invariant, tested where it is enforced.
//
// CLAUDE.md states it and Docs/02 §2 gives the transition table, but neither is a control. This
// is the control: an UPDATE that changes the status column is refused unless a
// job_status_history row written in the same transaction describes it. The three attempts below
// are the three ways somebody gets it wrong — no record at all, a record for something else, and
// a claim with no record behind it.
func TestJobStatusIsNotASettableField(t *testing.T) {
	pool := pgtest.DB(t)
	customer := newUser(t, pool, "settable@example.com", "+61400000404", "customer")

	t.Run("a direct update is refused", func(t *testing.T) {
		id := newJob(t, pool, customer)

		_, err := pool.Exec(t.Context(), `UPDATE jobs SET status = 'Open' WHERE id = $1`, id)
		if err == nil {
			t.Fatal("a job's status was changed by an ordinary UPDATE")
		}
		if !strings.Contains(err.Error(), "not a settable field") {
			t.Errorf("expected the status guard to refuse it, got: %v", err)
		}

		var status string
		if err := pool.QueryRow(t.Context(),
			`SELECT status FROM jobs WHERE id = $1`, id).Scan(&status); err != nil {
			t.Fatalf("reading the job back: %v", err)
		}
		if status != "Draft" {
			t.Errorf("the job is %q; the refused update moved it anyway", status)
		}
	})

	t.Run("a claim with no history row behind it is refused", func(t *testing.T) {
		id := newJob(t, pool, customer)
		invented, _ := uuid.NewV7()

		tx, err := pool.Begin(t.Context())
		if err != nil {
			t.Fatalf("beginning: %v", err)
		}
		defer func() { _ = tx.Rollback(t.Context()) }()

		if _, err := tx.Exec(t.Context(),
			`SELECT set_config('shipper.job_status_transition', $1, true)`, invented.String()); err != nil {
			t.Fatalf("setting the transition claim: %v", err)
		}
		if _, err := tx.Exec(t.Context(),
			`UPDATE jobs SET status = 'Open' WHERE id = $1`, id); err == nil {
			t.Fatal("a status change was accepted with nothing recording it")
		} else if !strings.Contains(err.Error(), "unrecorded") {
			t.Errorf("expected the guard to refuse it as unrecorded, got: %v", err)
		}
	})

	t.Run("another job's history row is refused", func(t *testing.T) {
		mine := newJob(t, pool, customer)
		theirs := newJob(t, pool, customer)

		tx, err := pool.Begin(t.Context())
		if err != nil {
			t.Fatalf("beginning: %v", err)
		}
		defer func() { _ = tx.Rollback(t.Context()) }()

		// A perfectly valid record — of a different job moving.
		history, _ := uuid.NewV7()
		if _, err := tx.Exec(t.Context(), `
			INSERT INTO job_status_history
				(id, job_id, from_status, to_status, actor_type, actor_id, actor_recorded_at)
			VALUES ($1, $2, 'Draft', 'Open', 'customer', $3, now())`,
			history, theirs, customer); err != nil {
			t.Fatalf("recording a transition for the other job: %v", err)
		}

		if _, err := tx.Exec(t.Context(),
			`SELECT set_config('shipper.job_status_transition', $1, true)`, history.String()); err != nil {
			t.Fatalf("setting the transition claim: %v", err)
		}
		if _, err := tx.Exec(t.Context(),
			`UPDATE jobs SET status = 'Open' WHERE id = $1`, mine); err == nil {
			t.Fatal("one job was moved on the strength of another job's history")
		} else if !strings.Contains(err.Error(), "unrecorded") {
			t.Errorf("expected the guard to refuse it as unrecorded, got: %v", err)
		}
	})

	t.Run("an update that leaves the status alone passes", func(t *testing.T) {
		id := newJob(t, pool, customer)

		// The guard has an opinion about one column. Everything else a job will carry —
		// category, addresses, budget — is edited by SHIP-62 with no ceremony at all.
		if _, err := pool.Exec(t.Context(),
			`UPDATE jobs SET customer_id = customer_id WHERE id = $1`, id); err != nil {
			t.Errorf("an ordinary update to a job was refused: %v", err)
		}
	})
}

// TestJobStatusHistoryRecordsBothClocks is SHIP-57a's acceptance criterion at the schema level:
// the actor's clock and the platform's are two columns, and only one of them is the caller's to
// choose.
//
// Docs/02 §3.1 requires the distinction because a driver records a milestone where there is no
// signal and the request arrives much later. If the platform stored one timestamp, the evidence
// that the two ever differed would be gone.
func TestJobStatusHistoryRecordsBothClocks(t *testing.T) {
	pool := pgtest.DB(t)
	customer := newUser(t, pool, "two-clocks@example.com", "+61400000405", "customer")
	job := newJob(t, pool, customer)

	// Forty minutes ago, out of signal.
	actorRecordedAt := time.Now().UTC().Add(-40 * time.Minute).Truncate(time.Millisecond)

	id, _ := uuid.NewV7()
	if _, err := pool.Exec(t.Context(), `
		INSERT INTO job_status_history
			(id, job_id, from_status, to_status, actor_type, actor_id, reason, actor_recorded_at)
		VALUES ($1, $2, 'Draft', 'Open', 'customer', $3, 'published from the app', $4)`,
		id, job, customer, actorRecordedAt); err != nil {
		t.Fatalf("recording a transition: %v", err)
	}

	var actor, server time.Time
	if err := pool.QueryRow(t.Context(),
		`SELECT actor_recorded_at, server_recorded_at FROM job_status_history WHERE id = $1`, id,
	).Scan(&actor, &server); err != nil {
		t.Fatalf("reading it back: %v", err)
	}

	if !actor.Equal(actorRecordedAt) {
		t.Errorf("actor_recorded_at is %s, want the %s the actor claimed; the platform "+
			"does not correct the actor's clock", actor, actorRecordedAt)
	}
	if !server.After(actor) {
		t.Errorf("server_recorded_at (%s) is not after actor_recorded_at (%s); "+
			"the two clocks have been collapsed into one", server, actor)
	}
}

// TestJobStatusHistoryConstraints covers the rules 000401 carries, each of which is a product
// decision rather than a tidiness one.
func TestJobStatusHistoryConstraints(t *testing.T) {
	pool := pgtest.DB(t)
	customer := newUser(t, pool, "history-rules@example.com", "+61400000406", "customer")
	job := newJob(t, pool, customer)

	record := func(t *testing.T, from, to, actorType string, actorID any, reason any) error {
		t.Helper()
		id, _ := uuid.NewV7()
		_, err := pool.Exec(t.Context(), `
			INSERT INTO job_status_history
				(id, job_id, from_status, to_status, actor_type, actor_id, reason, actor_recorded_at)
			VALUES ($1, $2, $3, $4, $5, $6, $7, now())`,
			id, job, from, to, actorType, actorID, reason)
		return err
	}

	t.Run("an administrator must say why", func(t *testing.T) {
		// Docs/01 §3: an administrator may not change a commercial record without an
		// auditable reason, and this table is where that reason is auditable.
		if err := record(t, "Open", "Cancelled", "admin", customer, nil); err == nil {
			t.Error("an administrator cancelled a job with no reason recorded")
		} else if !strings.Contains(err.Error(), "ck_job_status_history_admin_reason") {
			t.Errorf("expected ck_job_status_history_admin_reason to refuse it, got: %v", err)
		}

		if err := record(t, "Open", "Cancelled", "admin", customer, "prohibited goods"); err != nil {
			t.Errorf("an administrator with a reason was refused: %v", err)
		}
	})

	t.Run("the platform has no account and everyone else has one", func(t *testing.T) {
		if err := record(t, "Open", "Cancelled", "system", customer, nil); err == nil {
			t.Error("a system transition claimed an account")
		}
		if err := record(t, "Open", "Cancelled", "customer", nil, nil); err == nil {
			t.Error("a customer transition was recorded with no customer attached")
		}
		if err := record(t, "Open", "Cancelled", "system", nil, nil); err != nil {
			t.Errorf("the expiry sweep cannot record what it did: %v", err)
		}
	})

	t.Run("an unknown actor kind is refused", func(t *testing.T) {
		if err := record(t, "Open", "Cancelled", "robot", customer, nil); err == nil {
			t.Error("'robot' was accepted as an actor")
		}
	})

	t.Run("a transition has to move", func(t *testing.T) {
		if err := record(t, "Open", "Open", "customer", customer, nil); err == nil {
			t.Error("a job was recorded as moving from Open to Open")
		} else if !strings.Contains(err.Error(), "ck_job_status_history_moves") {
			t.Errorf("expected ck_job_status_history_moves to refuse it, got: %v", err)
		}
	})

	t.Run("history belongs to a real job", func(t *testing.T) {
		id, _ := uuid.NewV7()
		stranger, _ := uuid.NewV7()
		_, err := pool.Exec(t.Context(), `
			INSERT INTO job_status_history
				(id, job_id, from_status, to_status, actor_type, actor_id, actor_recorded_at)
			VALUES ($1, $2, 'Draft', 'Open', 'customer', $3, now())`,
			id, stranger, customer)
		if err == nil {
			t.Error("a transition was recorded against a job that does not exist")
		} else if !strings.Contains(err.Error(), "fk_job_status_history_job") {
			t.Errorf("expected the foreign key to refuse it, got: %v", err)
		}
	})
}

// TestJobStatusHistoryIsAppendOnly is the same control 000003 puts on audit_log, for the same
// reason: a history that can be edited is not evidence.
//
// Docs/02 §3.1 requires an update that contradicts an administrative action to be retained
// rather than discarded, so this table holds attempts as well as outcomes — and the value of
// both rests on nobody being able to tidy them afterwards, including at a psql prompt.
func TestJobStatusHistoryIsAppendOnly(t *testing.T) {
	pool := pgtest.DB(t)
	customer := newUser(t, pool, "append-only@example.com", "+61400000407", "customer")
	job := newJob(t, pool, customer)

	id, _ := uuid.NewV7()
	if _, err := pool.Exec(t.Context(), `
		INSERT INTO job_status_history
			(id, job_id, from_status, to_status, actor_type, actor_id, reason, actor_recorded_at)
		VALUES ($1, $2, 'Draft', 'Open', 'customer', $3, 'published', now())`,
		id, job, customer); err != nil {
		t.Fatalf("recording a transition: %v", err)
	}

	t.Run("update is refused", func(t *testing.T) {
		_, err := pool.Exec(t.Context(),
			`UPDATE job_status_history SET reason = 'something else' WHERE id = $1`, id)
		if err == nil {
			t.Fatal("a recorded transition was rewritten")
		}
		if !strings.Contains(err.Error(), "append-only") {
			t.Errorf("expected the append-only trigger to refuse it, got: %v", err)
		}
	})

	t.Run("delete is refused", func(t *testing.T) {
		_, err := pool.Exec(t.Context(), `DELETE FROM job_status_history WHERE id = $1`, id)
		if err == nil {
			t.Fatal("a recorded transition was deleted")
		}
		if !strings.Contains(err.Error(), "append-only") {
			t.Errorf("expected the append-only trigger to refuse it, got: %v", err)
		}
	})
}

// TestJobStatusHistoryForeignKeyIsIndexed is Docs/10 §3.3's foreign key rule, and here the index
// is load-bearing rather than only prudent: 000402's trigger runs an EXISTS against this table
// on every status change.
func TestJobStatusHistoryForeignKeyIsIndexed(t *testing.T) {
	pool := pgtest.DB(t)

	var indexes int
	if err := pool.QueryRow(t.Context(), `
		SELECT count(*)
		FROM pg_index i
		JOIN pg_class t     ON t.oid = i.indrelid
		JOIN pg_attribute a ON a.attrelid = t.oid AND a.attnum = i.indkey[0]
		WHERE t.relname = 'job_status_history' AND a.attname = 'job_id'`,
	).Scan(&indexes); err != nil {
		t.Fatalf("looking for the index: %v", err)
	}
	if indexes == 0 {
		t.Error("job_status_history.job_id carries a foreign key and no index leading with it")
	}
}

// TestJobStatusHistoryTimestampsCarryTheirZone is Docs/10 §3.3's "timestamptz always".
//
// It matters more here than anywhere else in the schema: these two columns exist to be compared
// with each other, and a driver's clock and the platform's are frequently not in the same zone.
func TestJobStatusHistoryTimestampsCarryTheirZone(t *testing.T) {
	pool := pgtest.DB(t)

	rows, err := pool.Query(t.Context(), `
		SELECT column_name, data_type
		FROM information_schema.columns
		WHERE table_schema = 'public'
		  AND table_name = 'job_status_history'
		  AND data_type LIKE 'timestamp%'
		ORDER BY column_name`)
	if err != nil {
		t.Fatalf("reading the column types: %v", err)
	}
	defer rows.Close()

	found := 0
	for rows.Next() {
		var name, dataType string
		if err := rows.Scan(&name, &dataType); err != nil {
			t.Fatalf("scanning: %v", err)
		}
		found++
		if dataType != "timestamp with time zone" {
			t.Errorf("job_status_history.%s is %s, want timestamp with time zone", name, dataType)
		}
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterating: %v", err)
	}
	if found != 2 {
		t.Errorf("found %d timestamp columns, want 2 (actor_recorded_at, server_recorded_at)", found)
	}
}

// TestJobRequiresARealCustomer is the foreign key, and the fact that it is RESTRICT.
//
// Docs/10 §3.3 forbids a cascade: SHIP-171 pseudonymises an account rather than deleting it, so
// nothing should be removing a user row — and if something does, it must fail loudly rather than
// quietly destroying the transaction record Docs/05 §3.1 requires retaining.
func TestJobRequiresARealCustomer(t *testing.T) {
	pool := pgtest.DB(t)

	t.Run("an unknown customer is refused", func(t *testing.T) {
		id, _ := uuid.NewV7()
		stranger, _ := uuid.NewV7()
		_, err := pool.Exec(t.Context(),
			`INSERT INTO jobs (id, customer_id) VALUES ($1, $2)`, id, stranger)
		if err == nil {
			t.Fatal("a job was created for an account that does not exist")
		}
		if !strings.Contains(err.Error(), "fk_jobs_customer") {
			t.Errorf("expected the foreign key to refuse it, got: %v", err)
		}
	})

	t.Run("deleting the customer is refused while a job exists", func(t *testing.T) {
		customer := newUser(t, pool, "restrict-job@example.com", "+61400000401", "customer")
		newJob(t, pool, customer)

		_, err := pool.Exec(t.Context(), `DELETE FROM users WHERE id = $1`, customer)
		if err == nil {
			t.Fatal("the account was deleted, taking its jobs with it")
		}
		if !strings.Contains(err.Error(), "fk_jobs_customer") {
			t.Errorf("expected ON DELETE RESTRICT to refuse it, got: %v", err)
		}
	})
}

// TestJobStartsAsADraft pins the column default.
//
// Docs/02 §2 has exactly one entry point into the lifecycle, and every path out of Draft is a
// transition. A job created at any other status would have skipped one.
func TestJobStartsAsADraft(t *testing.T) {
	pool := pgtest.DB(t)

	customer := newUser(t, pool, "draft@example.com", "+61400000402", "customer")
	id := newJob(t, pool, customer)

	var status string
	if err := pool.QueryRow(t.Context(),
		`SELECT status FROM jobs WHERE id = $1`, id).Scan(&status); err != nil {
		t.Fatalf("reading the job back: %v", err)
	}
	if status != string(jobs.StatusDraft) {
		t.Errorf("a new job is %q, want %q", status, jobs.StatusDraft)
	}
}

// TestJobUpdatedAtIsMaintained proves the trigger is attached to this table, rather than relying
// on the sweep in schema_test.go noticing later.
//
// The write is a no-op on purpose: jobs has no mutable column of its own yet, and status is not
// one anybody may set. Any UPDATE fires the trigger, which is what is being checked.
func TestJobUpdatedAtIsMaintained(t *testing.T) {
	pool := pgtest.DB(t)

	customer := newUser(t, pool, "touched-job@example.com", "+61400000403", "customer")
	id := newJob(t, pool, customer)

	var before time.Time
	if err := pool.QueryRow(t.Context(),
		`SELECT updated_at FROM jobs WHERE id = $1`, id).Scan(&before); err != nil {
		t.Fatalf("reading updated_at: %v", err)
	}

	// A separate statement, and therefore a separate transaction: now() is transaction start
	// time, so within one transaction the timestamp would legitimately not move.
	if _, err := pool.Exec(t.Context(),
		`UPDATE jobs SET customer_id = customer_id WHERE id = $1`, id); err != nil {
		t.Fatalf("updating: %v", err)
	}

	var after time.Time
	if err := pool.QueryRow(t.Context(),
		`SELECT updated_at FROM jobs WHERE id = $1`, id).Scan(&after); err != nil {
		t.Fatalf("reading updated_at: %v", err)
	}

	if !after.After(before) {
		t.Errorf("updated_at did not move on update (%s then %s); is the trigger attached?", before, after)
	}
}

// TestJobTimestampsCarryTheirZone is Docs/10 §3.3's "timestamptz always, never timestamp".
//
// A plain timestamp column accepts every write and reads back correctly on the machine that
// wrote it. It goes wrong only across a zone boundary, months later, in a way that looks like a
// data-entry mistake rather than a schema one — and this table will carry pickup windows read by
// a driver in another state.
func TestJobTimestampsCarryTheirZone(t *testing.T) {
	pool := pgtest.DB(t)

	rows, err := pool.Query(t.Context(), `
		SELECT column_name, data_type
		FROM information_schema.columns
		WHERE table_schema = 'public'
		  AND table_name = 'jobs'
		  AND data_type LIKE 'timestamp%'
		ORDER BY column_name`)
	if err != nil {
		t.Fatalf("reading the column types: %v", err)
	}
	defer rows.Close()

	found := 0
	for rows.Next() {
		var name, dataType string
		if err := rows.Scan(&name, &dataType); err != nil {
			t.Fatalf("scanning: %v", err)
		}
		found++
		if dataType != "timestamp with time zone" {
			t.Errorf("jobs.%s is %s, want timestamp with time zone", name, dataType)
		}
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterating: %v", err)
	}
	if found != 2 {
		t.Errorf("found %d timestamp columns, want 2 (created_at, updated_at)", found)
	}
}

// TestJobForeignKeyIsIndexed is the other half of Docs/10 §3.3's foreign key rule.
//
// An unindexed foreign key is invisible until the table is large: every delete on users takes a
// sequential scan of this one to check the constraint, and so does SHIP-66's "my jobs" list.
func TestJobForeignKeyIsIndexed(t *testing.T) {
	pool := pgtest.DB(t)

	var indexes int
	if err := pool.QueryRow(t.Context(), `
		SELECT count(*)
		FROM pg_index i
		JOIN pg_class t     ON t.oid = i.indrelid
		JOIN pg_attribute a ON a.attrelid = t.oid AND a.attnum = i.indkey[0]
		WHERE t.relname = 'jobs' AND a.attname = 'customer_id'`,
	).Scan(&indexes); err != nil {
		t.Fatalf("looking for the index: %v", err)
	}
	if indexes == 0 {
		t.Error("jobs.customer_id carries a foreign key and no index leading with it")
	}
}

// TestJobMigrationIsInTheJobsBlock names the number, because the block scheme is only worth
// having if something checks that a migration landed inside its range.
func TestJobMigrationIsInTheJobsBlock(t *testing.T) {
	for _, version := range []int{400, 401, 402} {
		block, ok := migrations.BlockContaining(version)
		if !ok {
			t.Errorf("version %d falls in no reserved block", version)
			continue
		}
		if block.Domain != "jobs" {
			t.Errorf("version %d is in the %q block, want jobs", version, block.Domain)
		}
	}
}
