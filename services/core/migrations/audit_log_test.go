package migrations_test

import (
	"testing"

	"github.com/DulsaraNethmin/Shipper/services/core/internal/admin"
	"github.com/DulsaraNethmin/Shipper/services/core/internal/testsupport/pgtest"
)

// SHIP-150's half of `000003`, which is the pairing SHIP-149 could not write.
//
// The append-only triggers and the two actor-pairing constraints are already held to account in
// schema_test.go, by a test that inserts its own rows. What could not exist until this ticket is a
// pairing between the constraint and a **Go vocabulary**, because there was no Go vocabulary: the
// only writes to `audit_log` in the repository were four statements in that same test file.
//
// Docs/10 §3.4 asks for the pairing in both directions, and the two failures are different. A value
// the column accepts that Go has no constant for is an actor kind nothing in the service can write
// and nothing can classify on the way out; a constant Go holds that the column refuses is a write
// that fails at run time, in the transaction of whatever privileged action it was recording — which
// takes the action down with it, because SHIP-150 deliberately does not swallow that error.

// TestAuditActorTypeConstraintMatchesTheGoConstants.
func TestAuditActorTypeConstraintMatchesTheGoConstants(t *testing.T) {
	pool := pgtest.DB(t)

	inDatabase := constraintLiterals(t, pool, "ck_audit_log_actor_type")

	for _, actor := range admin.AuditActorTypes {
		if !inDatabase[actor.String()] {
			t.Errorf("admin.AuditActorTypes has %q and ck_audit_log_actor_type refuses it, so "+
				"an entry naming that actor cannot be written at all", actor)
		}
		delete(inDatabase, actor.String())
	}
	for leftover := range inDatabase {
		t.Errorf("ck_audit_log_actor_type accepts %q and admin.AuditActorTypes has no such "+
			"actor, so the trail could hold entries the service has no word for", leftover)
	}
}

// TestNoConstraintNarrowsTheActionColumn, which is a deliberate absence rather than an oversight.
//
// `audit_log.action` has no CHECK and `target_type` has none either, and both were argued in
// `000003`: the table outlives its subjects (Docs/05 §3.1) and has to be able to name a kind of
// thing that no longer exists. What keeps the values consistent is [admin.AuditActions] on the
// writing side, checked by [admin.AuditAction.Valid] before any statement runs.
//
// This test exists so that adding a CHECK is a deliberate act. A constraint here would be the
// opposite trade — the database would refuse an action the catalogue had grown and the migration
// had not, which turns a widening into an outage rather than into a failed test.
func TestNoConstraintNarrowsTheActionColumn(t *testing.T) {
	pool := pgtest.DB(t)

	for _, column := range []string{"action", "target_type"} {
		var constraints int
		if err := pool.QueryRow(t.Context(), `
			SELECT count(*)
			FROM pg_constraint c
			JOIN pg_attribute a
			  ON a.attrelid = c.conrelid AND a.attnum = ANY (c.conkey)
			WHERE c.conrelid = 'audit_log'::regclass
			  AND c.contype  = 'c'
			  AND a.attname  = $1`, column).Scan(&constraints); err != nil {
			t.Fatalf("reading the constraints on audit_log.%s: %v", column, err)
		}
		if constraints != 0 {
			t.Errorf("audit_log.%s has %d check constraint(s) on it.\n"+
				"It is deliberately unconstrained: an audit entry outlives its subject "+
				"(Docs/05 §3.1) and may need to name a kind of thing that no longer exists. "+
				"The closed list lives in admin.AuditActions, where widening it is a Go "+
				"change rather than a migration the writer can get ahead of.",
				column, constraints)
		}
	}
}
