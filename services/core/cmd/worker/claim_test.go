package main

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/DulsaraNethmin/Shipper/services/core/internal/db"
	"github.com/DulsaraNethmin/Shipper/services/core/internal/testsupport/pgtest"
)

// TestAClaimQueryHasToLockAndSkip is the check that catches the two mistakes a single-worker test
// never will.
//
// Both broken forms below return the right rows on their own. The first blocks behind another
// worker instead of sharing with it; the second lets two workers act on the same row. Neither is
// an error, and neither is visible until there are two workers — which, on any rolling
// deployment, is most of the time.
func TestAClaimQueryHasToLockAndSkip(t *testing.T) {
	broken := map[string]string{
		"no locking at all": `SELECT id FROM worker_due_work WHERE done_by IS NULL LIMIT 10`,
		"locks but waits":   `SELECT id FROM worker_due_work WHERE done_by IS NULL FOR UPDATE LIMIT 10`,
		"skips without locking": `SELECT id FROM worker_due_work WHERE done_by IS NULL
		                          ORDER BY due_at SKIP LOCKED LIMIT 10`,
	}

	for name, query := range broken {
		t.Run(name, func(t *testing.T) {
			err := checkClaim(query)
			if err == nil {
				t.Fatal("the claim was accepted")
			}
			if !strings.Contains(err.Error(), "Docs/10 §6.2") {
				t.Errorf("the error does not say where the rule is written down: %v", err)
			}
		})
	}

	// Case and line breaks are how the query is actually written, so the check has to see
	// through both.
	for name, query := range map[string]string{
		"lower case": `select id from worker_due_work for update skip locked limit 10`,
		"upper case": `SELECT id FROM worker_due_work FOR UPDATE SKIP LOCKED LIMIT 10`,
		"across lines": `SELECT id FROM worker_due_work
		                 ORDER BY due_at
		                 FOR UPDATE SKIP LOCKED
		                 LIMIT 10`,
	} {
		t.Run(name, func(t *testing.T) {
			if err := checkClaim(query); err != nil {
				t.Errorf("a well-formed claim was refused: %v", err)
			}
		})
	}
}

// TestClaimIDsRefusesBeforeItQueries proves the check is not decorative: a bad claim never
// reaches the database, so a task with one fails on its first pass rather than on its first busy
// afternoon.
func TestClaimIDsRefusesBeforeItQueries(t *testing.T) {
	pool := pgtest.DB(t)
	dueWork(t, pool, 3)

	var ran bool
	_, err := ClaimIDs(t.Context(), watcher{pool, &ran},
		`SELECT id FROM worker_due_work WHERE done_by IS NULL LIMIT 1`)
	if err == nil {
		t.Fatal("a claim with no locking was run")
	}
	if ran {
		t.Error("the query reached the database before it was refused")
	}
}

// watcher records whether a query was sent, so the test above can tell "refused" from "ran and
// returned nothing".
type watcher struct {
	db.Runner
	ran *bool
}

func (w watcher) Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
	*w.ran = true
	return w.Runner.Query(ctx, sql, args...)
}

// TestClaimIDsReadsTheIdentifiers covers the ordinary path against a real database, since a claim
// that returns the wrong type is a task that never runs.
func TestClaimIDsReadsTheIdentifiers(t *testing.T) {
	pool := pgtest.DB(t)
	dueWork(t, pool, 4)

	scheduler := newTestScheduler(t, pool)

	var got int
	if _, err := scheduler.RunOnce(t.Context(), Task{
		Name:  "reads",
		Every: time.Hour,
		Run: func(ctx context.Context, r db.Runner) (int, error) {
			ids, err := ClaimIDs(ctx, r, claimDueWork, 3)
			if err != nil {
				return 0, err
			}
			got = len(ids)
			for _, id := range ids {
				if id.String() == "00000000-0000-0000-0000-000000000000" {
					t.Error("a claimed id came back empty")
				}
			}
			return len(ids), nil
		},
	}); err != nil {
		t.Fatalf("the pass failed: %v", err)
	}
	if got != 3 {
		t.Errorf("the claim returned %d ids, want 3", got)
	}
}
