package delivery

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/DulsaraNethmin/Shipper/services/core/internal/db"
)

// postgresStore is this domain's persistence, concrete and unexported.
//
// There is no repository interface, per Docs/10 §2.2 and Docs/06 §4.1. The guarantee this file
// rests on hardest is PostgreSQL's — uq_driver_assignments_active is a *partial* unique index, so a
// job accumulates assignments over its life while never having two live at once — and an interface
// designed to keep the database swappable would hide exactly the mechanism that makes the rule
// correct. A mock would accept the second live driver this store exists to have refused.
//
// Every method takes a db.Runner as its first argument after ctx, so the caller decides whether the
// work stands alone or joins a transaction it already opened (Docs/10 §3.2).
//
// # What this file does not query
//
// `jobs` and `bids`. Both are other domains' tables and both are reached through the ports in
// ports.go instead, wired in cmd/api — the boundary lint reads imports and cannot see a SELECT, so
// this is a rule that holds by being written down and followed. `users` is different and is read
// below: it lives in the shared migration block precisely because the whole service reads it
// (Docs/10 §9.2), which is the same sanction `jobs` and `fleet` rely on for their role checks.
type postgresStore struct{}

// assignmentColumns is every column of an assignment, in the order [scanAssignment] reads them.
//
// unassigned_at has no COALESCE because PostgreSQL's NULL has no representation in time.Time, and
// because the distinction it carries is the whole of [Assignment.Live].
const assignmentColumns = `
	id, job_id, driver_name, driver_mobile, unassigned_at, created_at, updated_at`

// scanAssignment reads one row of [assignmentColumns].
//
// One function rather than a copy of the Scan call at each call site: a column added to
// assignmentColumns and not here is a scan mismatch at the first call, which is the failure worth
// having.
func scanAssignment(row pgx.Row) (Assignment, error) {
	var (
		a            Assignment
		unassignedAt *time.Time
	)

	if err := row.Scan(
		&a.ID, &a.JobID, &a.DriverName, &a.DriverMobile,
		&unassignedAt, &a.CreatedAt, &a.UpdatedAt,
	); err != nil {
		return Assignment{}, err
	}

	if unassignedAt != nil {
		a.UnassignedAt = *unassignedAt
	}
	return a, nil
}

// insert creates an assignment, live.
//
// unassigned_at is not named, so a driver is always put on the job rather than off it. There is no
// request shape that could ask for anything else, which is what "an assignment ends by being ended,
// not by being created dead" means at the moment the row comes into existence.
//
// A unique violation on uq_driver_assignments_active is translated here rather than reported as a
// database error: it is the index doing the work [Service.AssignDriver] describes, and a caller
// handed a constraint name learns nothing they can act on.
func (postgresStore) insert(ctx context.Context, r db.Runner, a Assignment) (Assignment, error) {
	const q = `
		INSERT INTO driver_assignments (id, job_id, driver_name, driver_mobile)
		VALUES ($1, $2, $3, $4)
		RETURNING ` + assignmentColumns

	created, err := scanAssignment(r.QueryRow(ctx, q, a.ID, a.JobID, a.DriverName, a.DriverMobile))
	if err != nil {
		if db.IsUniqueViolation(err, "uq_driver_assignments_active") {
			return Assignment{}, fmt.Errorf("delivery: %s already has a live driver: %w",
				a.JobID, ErrDriverAlreadyAssigned)
		}
		return Assignment{}, fmt.Errorf("delivery: assigning a driver to %s: %w", a.JobID, err)
	}
	return created, nil
}

// liveAssignment reads the driver currently on a job, and reports whether there is one.
//
// Comma-ok rather than a sentinel error, because "this job has no driver yet" is the ordinary state
// of every awarded job and reporting it as an error would make the normal path the exceptional one.
//
// **No FOR UPDATE.** Locking the rows that exist cannot stop the row that does not yet exist from
// being inserted, so a lock here would buy nothing that uq_driver_assignments_active does not
// already guarantee — and it would suggest to the next reader that the check is what enforces the
// rule.
func (postgresStore) liveAssignment(ctx context.Context, r db.Runner, jobID uuid.UUID) (Assignment, bool, error) {
	const q = `
		SELECT ` + assignmentColumns + `
		FROM driver_assignments
		WHERE job_id = $1 AND unassigned_at IS NULL`

	a, err := scanAssignment(r.QueryRow(ctx, q, jobID))
	switch {
	case errors.Is(err, db.ErrNoRows):
		return Assignment{}, false, nil
	case err != nil:
		return Assignment{}, false, fmt.Errorf("delivery: reading the driver on %s: %w", jobID, err)
	}
	return a, true, nil
}

// accountPhone is the caller's own verified mobile, for a self-assignment.
//
// Reading `users` from here is the sanctioned kind of cross-table read: the table is in the shared
// migration block because the whole service reads it, and `jobs` and `fleet` both read it for the
// account's role. What this domain must not do — and does not — is read `jobs` or `bids`.
//
// A missing row is reported rather than absorbed. The caller is authenticated, so their account
// exists; if it does not, something is wrong that a blank driver mobile would only hide.
func (postgresStore) accountPhone(ctx context.Context, r db.Runner, userID uuid.UUID) (string, error) {
	const q = `SELECT phone FROM users WHERE id = $1`

	var phone string
	err := r.QueryRow(ctx, q, userID).Scan(&phone)
	switch {
	case errors.Is(err, db.ErrNoRows):
		return "", fmt.Errorf("delivery: no account %s to self-assign from: %w", userID, err)
	case err != nil:
		return "", fmt.Errorf("delivery: reading the mobile on %s: %w", userID, err)
	}
	return phone, nil
}
