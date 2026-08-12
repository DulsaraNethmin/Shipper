package migrations_test

import (
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/DulsaraNethmin/Shipper/services/core/internal/testsupport/pgtest"
	"github.com/DulsaraNethmin/Shipper/services/core/migrations"
)

// SHIP-105's table, checked where its guarantees live.
//
// The ticket reaches no HTTP endpoint, so it has no `make verify` section and these tests are the
// demonstration — which is not a weaker one, because everything asserted below is a constraint,
// an index or a trigger. Docs/06 §4.1: "a mock happily accepts a write that the actual constraint
// would reject."
//
// newUser and newJob come from schema_test.go and jobs_test.go, in this same external test
// package.

// assign puts a driver on a job and returns the assignment id.
func assign(t *testing.T, pool *pgxpool.Pool, job uuid.UUID, name, mobile string) uuid.UUID {
	t.Helper()

	id, err := uuid.NewV7()
	if err != nil {
		t.Fatalf("generating an id: %v", err)
	}
	if _, err := pool.Exec(t.Context(), `
		INSERT INTO driver_assignments (id, job_id, driver_name, driver_mobile)
		VALUES ($1, $2, $3, $4)`, id, job, name, mobile); err != nil {
		t.Fatalf("assigning %s to %s: %v", name, job, err)
	}
	return id
}

// newDeliveryJob is a job with a customer behind it, which is all either table needs.
func newDeliveryJob(t *testing.T, pool *pgxpool.Pool, email, phone string) uuid.UUID {
	t.Helper()
	return newJob(t, pool, newUser(t, pool, email, phone, "customer"))
}

// TestDriverAssignmentRequiresARealJob is the foreign key and the fact that it is RESTRICT.
//
// Docs/10 §3.3 forbids a cascade, and here the consequence is sharper than usual: the assignment
// is the only record of who physically carried the goods, and a driver has no account for it to
// be reconstructed from.
func TestDriverAssignmentRequiresARealJob(t *testing.T) {
	pool := pgtest.DB(t)

	t.Run("an unknown job is refused", func(t *testing.T) {
		id, _ := uuid.NewV7()
		stranger, _ := uuid.NewV7()
		_, err := pool.Exec(t.Context(), `
			INSERT INTO driver_assignments (id, job_id, driver_name, driver_mobile)
			VALUES ($1, $2, 'Dave Nguyen', '+61412345678')`, id, stranger)
		if err == nil {
			t.Fatal("a driver was assigned to a job that does not exist")
		}
		if !strings.Contains(err.Error(), "fk_driver_assignments_job") {
			t.Errorf("expected the foreign key to refuse it, got: %v", err)
		}
	})

	t.Run("deleting the job is refused while an assignment exists", func(t *testing.T) {
		job := newDeliveryJob(t, pool, "assign-restrict@example.com", "+61400000600")
		assign(t, pool, job, "Dave Nguyen", "+61412345678")

		_, err := pool.Exec(t.Context(), `DELETE FROM jobs WHERE id = $1`, job)
		if err == nil {
			t.Fatal("the job was deleted, taking the record of who drove it with it")
		}
		if !strings.Contains(err.Error(), "fk_driver_assignments_job") {
			t.Errorf("expected ON DELETE RESTRICT to refuse it, got: %v", err)
		}
	})
}

// TestOnlyOneDriverIsAssignedAtATime is the partial unique index, and it is the reason
// SHIP-105 has one.
//
// Two live assignments mean two valid driver links to one job once SHIP-107 hangs a token off
// this row, which is what SHIP-108's "exactly one job and nothing else" would quietly stop
// guaranteeing. The second half of the test is the case the index is partial *for*: a provider
// replaces a driver, and both rows have to survive because milestones point at them.
func TestOnlyOneDriverIsAssignedAtATime(t *testing.T) {
	pool := pgtest.DB(t)
	job := newDeliveryJob(t, pool, "one-driver@example.com", "+61400000601")

	first := assign(t, pool, job, "Dave Nguyen", "+61412345678")

	t.Run("a second live assignment is refused", func(t *testing.T) {
		id, _ := uuid.NewV7()
		_, err := pool.Exec(t.Context(), `
			INSERT INTO driver_assignments (id, job_id, driver_name, driver_mobile)
			VALUES ($1, $2, 'Priya Sharma', '+61498765432')`, id, job)
		if err == nil {
			t.Fatal("a job has two live drivers, and will have two valid portal links")
		}
		if !strings.Contains(err.Error(), "uq_driver_assignments_active") {
			t.Errorf("expected uq_driver_assignments_active to refuse it, got: %v", err)
		}
	})

	t.Run("replacing the driver is permitted, and the first row survives", func(t *testing.T) {
		if _, err := pool.Exec(t.Context(),
			`UPDATE driver_assignments SET unassigned_at = now() WHERE id = $1`, first); err != nil {
			t.Fatalf("standing the first driver down: %v", err)
		}

		second := assign(t, pool, job, "Priya Sharma", "+61498765432")

		var live int
		if err := pool.QueryRow(t.Context(), `
			SELECT count(*) FROM driver_assignments
			WHERE job_id = $1 AND unassigned_at IS NULL`, job).Scan(&live); err != nil {
			t.Fatalf("counting live assignments: %v", err)
		}
		if live != 1 {
			t.Errorf("%d live assignments after a replacement, want 1", live)
		}

		var name string
		if err := pool.QueryRow(t.Context(),
			`SELECT driver_name FROM driver_assignments WHERE id = $1`, first).Scan(&name); err != nil {
			t.Fatalf("reading the stood-down assignment back: %v", err)
		}
		if name != "Dave Nguyen" {
			t.Errorf("the first assignment reads %q; a milestone recorded against it now "+
				"names the wrong person", name)
		}
		if second == first {
			t.Fatal("the replacement reused the first assignment's id")
		}
	})

	t.Run("several stood-down assignments are permitted", func(t *testing.T) {
		if _, err := pool.Exec(t.Context(),
			`UPDATE driver_assignments SET unassigned_at = now()
			 WHERE job_id = $1 AND unassigned_at IS NULL`, job); err != nil {
			t.Fatalf("standing the second driver down: %v", err)
		}
		assign(t, pool, job, "Tom Okafor", "+61455512345")

		var total int
		if err := pool.QueryRow(t.Context(),
			`SELECT count(*) FROM driver_assignments WHERE job_id = $1`, job).Scan(&total); err != nil {
			t.Fatalf("counting: %v", err)
		}
		if total != 3 {
			t.Errorf("%d assignments recorded over the job's life, want 3", total)
		}
	})
}

// TestADriverAssignmentIdentityIsImmutable is why the table has a trigger at all.
//
// A driver has no account, so 000401 made this row the driver's identity:
// job_status_history.actor_id names it when actor_type is 'driver', and 000601 does the same. An
// UPDATE to driver_name is therefore not an edit — it silently re-attributes every milestone and
// every transition already recorded against this assignment to a different person.
func TestADriverAssignmentIdentityIsImmutable(t *testing.T) {
	pool := pgtest.DB(t)
	job := newDeliveryJob(t, pool, "immutable-driver@example.com", "+61400000602")
	other := newDeliveryJob(t, pool, "immutable-driver-2@example.com", "+61400000603")
	id := assign(t, pool, job, "Dave Nguyen", "+61412345678")

	refused := func(t *testing.T, sql string, args ...any) error {
		t.Helper()
		_, err := pool.Exec(t.Context(), sql, args...)
		if err == nil {
			t.Fatal("the update was accepted")
		}
		return err
	}

	t.Run("the name cannot change", func(t *testing.T) {
		err := refused(t, `UPDATE driver_assignments SET driver_name = 'Someone Else' WHERE id = $1`, id)
		if !strings.Contains(err.Error(), "immutable") {
			t.Errorf("expected the immutability trigger to refuse it, got: %v", err)
		}
	})

	t.Run("the mobile cannot change", func(t *testing.T) {
		err := refused(t, `UPDATE driver_assignments SET driver_mobile = '+61400111222' WHERE id = $1`, id)
		if !strings.Contains(err.Error(), "immutable") {
			t.Errorf("expected the immutability trigger to refuse it, got: %v", err)
		}
	})

	t.Run("the assignment cannot be moved to another job", func(t *testing.T) {
		err := refused(t, `UPDATE driver_assignments SET job_id = $1 WHERE id = $2`, other, id)
		if !strings.Contains(err.Error(), "job_id is immutable") {
			t.Errorf("expected the immutability trigger to refuse it, got: %v", err)
		}
	})

	t.Run("standing the driver down is permitted", func(t *testing.T) {
		if _, err := pool.Exec(t.Context(),
			`UPDATE driver_assignments SET unassigned_at = now() WHERE id = $1`, id); err != nil {
			t.Fatalf("a driver could not be taken off a job: %v", err)
		}
	})

	t.Run("but not undone", func(t *testing.T) {
		err := refused(t, `UPDATE driver_assignments SET unassigned_at = NULL WHERE id = $1`, id)
		if !strings.Contains(err.Error(), "set once") {
			t.Errorf("expected the trigger to refuse the revival, got: %v", err)
		}
	})

	t.Run("nor moved", func(t *testing.T) {
		err := refused(t,
			`UPDATE driver_assignments SET unassigned_at = now() - interval '1 day' WHERE id = $1`, id)
		if !strings.Contains(err.Error(), "set once") {
			t.Errorf("expected the trigger to refuse the change, got: %v", err)
		}
	})

	t.Run("rewriting the same values is not a change", func(t *testing.T) {
		// An UPDATE naming every column is what a "save the whole row" write looks like, and
		// refusing one that changes nothing would break it for no gain — the same allowance
		// 000005 makes for users.role.
		if _, err := pool.Exec(t.Context(), `
			UPDATE driver_assignments
			SET driver_name   = driver_name,
			    driver_mobile = driver_mobile,
			    unassigned_at = unassigned_at
			WHERE id = $1`, id); err != nil {
			t.Errorf("rewriting identical values was refused: %v", err)
		}
	})
}

// TestDriverAssignmentRefusesAnUnusableContact covers the two column constraints.
//
// The mobile is not a tidiness rule: it is the only channel the platform has to a person with no
// account, and a malformed one means a driver who never receives the link and a job that stalls
// with nobody knowing why.
func TestDriverAssignmentRefusesAnUnusableContact(t *testing.T) {
	pool := pgtest.DB(t)
	job := newDeliveryJob(t, pool, "contact@example.com", "+61400000604")

	insert := func(t *testing.T, name, mobile string) error {
		t.Helper()
		id, _ := uuid.NewV7()
		_, err := pool.Exec(t.Context(), `
			INSERT INTO driver_assignments (id, job_id, driver_name, driver_mobile)
			VALUES ($1, $2, $3, $4)`, id, job, name, mobile)
		if err == nil {
			// Keep the partial unique index out of the way of the next case.
			_, _ = pool.Exec(t.Context(),
				`UPDATE driver_assignments SET unassigned_at = now() WHERE id = $1`, id)
		}
		return err
	}

	t.Run("a blank name is refused", func(t *testing.T) {
		for _, name := range []string{"", "   ", "\t"} {
			if err := insert(t, name, "+61412345678"); err == nil {
				t.Errorf("%q was accepted as a driver name", name)
			} else if !strings.Contains(err.Error(), "ck_driver_assignments_name") {
				t.Errorf("expected ck_driver_assignments_name to refuse %q, got: %v", name, err)
			}
		}
	})

	t.Run("a number that is not E.164 is refused", func(t *testing.T) {
		// The shapes identity's validE164 rejects: a domestic trunk prefix that was never
		// normalised, a country code starting with zero, letters, and too few digits.
		for _, mobile := range []string{"0412345678", "+0412345678", "+6141234567a", "+61123", "412 345 678"} {
			if err := insert(t, "Dave Nguyen", mobile); err == nil {
				t.Errorf("%q was accepted as a mobile number", mobile)
			} else if !strings.Contains(err.Error(), "ck_driver_assignments_mobile") {
				t.Errorf("expected ck_driver_assignments_mobile to refuse %q, got: %v", mobile, err)
			}
		}
	})

	t.Run("a normalised Australian mobile is accepted", func(t *testing.T) {
		if err := insert(t, "Dave Nguyen", "+61412345678"); err != nil {
			t.Errorf("a valid assignment was refused: %v", err)
		}
	})
}

// TestDriverAssignmentUpdatedAtIsMaintained proves the trigger is attached to this table rather
// than relying on the sweep in schema_test.go noticing later.
//
// It also proves the two BEFORE UPDATE triggers coexist: the immutability check runs first (they
// fire in name order) and lets the write through, and set_updated_at then does its job.
func TestDriverAssignmentUpdatedAtIsMaintained(t *testing.T) {
	pool := pgtest.DB(t)
	job := newDeliveryJob(t, pool, "touched-assignment@example.com", "+61400000605")
	id := assign(t, pool, job, "Dave Nguyen", "+61412345678")

	var before time.Time
	if err := pool.QueryRow(t.Context(),
		`SELECT updated_at FROM driver_assignments WHERE id = $1`, id).Scan(&before); err != nil {
		t.Fatalf("reading updated_at: %v", err)
	}

	// A separate statement, and therefore a separate transaction: now() is transaction start
	// time, so within one transaction the timestamp would legitimately not move.
	if _, err := pool.Exec(t.Context(),
		`UPDATE driver_assignments SET unassigned_at = now() WHERE id = $1`, id); err != nil {
		t.Fatalf("updating: %v", err)
	}

	var after time.Time
	if err := pool.QueryRow(t.Context(),
		`SELECT updated_at FROM driver_assignments WHERE id = $1`, id).Scan(&after); err != nil {
		t.Fatalf("reading updated_at: %v", err)
	}
	if !after.After(before) {
		t.Errorf("updated_at did not move on update (%s then %s); is the trigger attached?", before, after)
	}
}

// TestDriverAssignmentForeignKeyIsIndexed is Docs/10 §3.3's foreign key rule.
//
// The partial index does not satisfy it. It covers live assignments only, so a DELETE on jobs
// checking the constraint would still scan every stood-down row.
func TestDriverAssignmentForeignKeyIsIndexed(t *testing.T) {
	pool := pgtest.DB(t)

	var indexes int
	if err := pool.QueryRow(t.Context(), `
		SELECT count(*)
		FROM pg_index i
		JOIN pg_class t     ON t.oid = i.indrelid
		JOIN pg_attribute a ON a.attrelid = t.oid AND a.attnum = i.indkey[0]
		WHERE t.relname = 'driver_assignments'
		  AND a.attname = 'job_id'
		  AND i.indpred IS NULL`,
	).Scan(&indexes); err != nil {
		t.Fatalf("looking for the index: %v", err)
	}
	if indexes == 0 {
		t.Error("driver_assignments.job_id carries a foreign key and no unconditional index leading with it")
	}
}

// TestDriverAssignmentTimestampsCarryTheirZone is Docs/10 §3.3's "timestamptz always".
func TestDriverAssignmentTimestampsCarryTheirZone(t *testing.T) {
	assertTimestamptz(t, "driver_assignments", "created_at", "updated_at", "unassigned_at")
}

// TestDeliveryMigrationsAreInTheDeliveryBlock names the numbers, because the block scheme is only
// worth having if something checks that a migration landed inside its range.
func TestDeliveryMigrationsAreInTheDeliveryBlock(t *testing.T) {
	for _, version := range []int{600} {
		block, ok := migrations.BlockContaining(version)
		if !ok {
			t.Errorf("version %d falls in no reserved block", version)
			continue
		}
		if block.Domain != "delivery" {
			t.Errorf("version %d is in the %q block, want delivery", version, block.Domain)
		}
	}
}

// --- helpers ---------------------------------------------------------------------------

// assertTimestamptz checks that every timestamp column on a table carries its zone, and that the
// named ones exist.
//
// A plain timestamp column accepts every write and reads back correctly on the machine that wrote
// it. It goes wrong only across a zone boundary, months later, in a way that looks like a
// data-entry mistake rather than a schema one.
//
// Named rather than counted, for the reason TestJobTimestampsCarryTheirZone gives: a count is a
// snapshot of how many columns existed on the day it was written, and SHIP-107 and SHIP-115 both
// add columns to these tables.
func assertTimestamptz(t *testing.T, table string, required ...string) {
	t.Helper()
	pool := pgtest.DB(t)

	rows, err := pool.Query(t.Context(), `
		SELECT column_name, data_type
		FROM information_schema.columns
		WHERE table_schema = 'public' AND table_name = $1 AND data_type LIKE 'timestamp%'
		ORDER BY column_name`, table)
	if err != nil {
		t.Fatalf("reading the column types: %v", err)
	}
	defer rows.Close()

	found := map[string]bool{}
	for rows.Next() {
		var name, dataType string
		if err := rows.Scan(&name, &dataType); err != nil {
			t.Fatalf("scanning: %v", err)
		}
		found[name] = true
		if dataType != "timestamp with time zone" {
			t.Errorf("%s.%s is %s, want timestamp with time zone", table, name, dataType)
		}
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterating: %v", err)
	}

	for _, name := range required {
		if !found[name] {
			t.Errorf("%s has no %s column", table, name)
		}
	}
}
