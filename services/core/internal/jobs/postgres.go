package jobs

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/DulsaraNethmin/Shipper/services/core/internal/db"
)

// transitionSetting is the session variable 000402's trigger reads.
//
// It carries the id of the job_status_history row describing the change that is about to be
// made. The trigger refuses the update unless that row exists, belongs to the job being updated,
// and names the same two statuses — so the variable is not a password that unlocks the column,
// it is a pointer to the record that justifies the write.
//
// A custom setting name has to contain a dot; PostgreSQL treats the part before it as an
// extension namespace, which is why every one of ours starts with "shipper.".
const transitionSetting = "shipper.job_status_transition"

// postgresStore is this domain's persistence, concrete and unexported.
//
// There is no repository interface, per Docs/10 §2.2 and Docs/06 §4.1: the guarantees this
// domain rests on are PostgreSQL's — row locking here, a trigger in 000402 — and an interface
// designed to keep the database swappable would hide exactly the mechanisms that make the
// lifecycle correct. A mock would accept every write these methods exist to have refused.
//
// Every method takes a db.Runner as its first argument after ctx, so the caller decides whether
// the work stands alone or joins a transaction it already opened (Docs/10 §3.2). For a
// transition that decision is made for them: 000402 requires a transaction.
type postgresStore struct{}

const jobColumns = `id, customer_id, status, created_at, updated_at`

// lockJob reads a job and holds the row for the rest of the transaction.
//
// FOR UPDATE rather than a plain read, because a transition is a read-modify-write on the status
// column and two of them arriving at once is not hypothetical: a customer cancelling while an
// administrator disputes, a provider's queued milestone landing beside a live one. In READ
// COMMITTED, the second transaction blocks here and then re-reads the row the first one
// committed, so its own check of Docs/02 §2 runs against the status the job actually has rather
// than the one it had when the request arrived.
func (postgresStore) lockJob(ctx context.Context, r db.Runner, id uuid.UUID) (Job, error) {
	const q = `SELECT ` + jobColumns + ` FROM jobs WHERE id = $1 FOR UPDATE`

	var j Job
	err := r.QueryRow(ctx, q, id).Scan(&j.ID, &j.CustomerID, &j.Status, &j.CreatedAt, &j.UpdatedAt)
	switch {
	case errors.Is(err, db.ErrNoRows):
		return Job{}, fmt.Errorf("jobs: %s: %w", id, ErrJobNotFound)
	case err != nil:
		return Job{}, fmt.Errorf("jobs: lock %s: %w", id, err)
	}
	return j, nil
}

// recordTransition writes the history row and returns the platform's clock reading.
//
// server_recorded_at is not supplied. It defaults from now(), which is transaction start time,
// so it is the same instant as the job row's updated_at — and, more to the point, it is a
// timestamp no caller can choose. Docs/02 §3.1 makes the second of the two clocks the one audit
// and support rely on, and a value the application could pass would be one it could backdate.
func (postgresStore) recordTransition(ctx context.Context, r db.Runner, c StatusChange) (time.Time, error) {
	const q = `
		INSERT INTO job_status_history
			(id, job_id, from_status, to_status, actor_type, actor_id, reason, actor_recorded_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
		RETURNING server_recorded_at`

	// The database distinguishes "no account" from "the zero account", and "no reason given"
	// from "the empty reason". Go's zero values do not, so the conversion happens here rather
	// than leaving two columns that are never NULL and two constraints that never fire.
	var actorID any
	if c.Actor.ID != uuid.Nil {
		actorID = c.Actor.ID
	}
	var reason any
	if c.Reason != "" {
		reason = c.Reason
	}

	var serverRecordedAt time.Time
	if err := r.QueryRow(ctx, q,
		c.ID, c.JobID, string(c.From), string(c.To),
		string(c.Actor.Type), actorID, reason, c.ActorRecordedAt,
	).Scan(&serverRecordedAt); err != nil {
		return time.Time{}, fmt.Errorf("jobs: record %s moving %s to %s: %w", c.JobID, c.From, c.To, err)
	}
	return serverRecordedAt, nil
}

// claimTransition tells 000402's trigger which history row authorises the update that follows.
//
// The third argument to set_config makes the setting local to the current transaction. Outside
// one it lasts only for this statement, which is why a caller holding a pool rather than a
// transaction finds the update refused — and why Transition checks for a transaction before it
// writes anything at all.
func (postgresStore) claimTransition(ctx context.Context, r db.Runner, historyID uuid.UUID) error {
	const q = `SELECT set_config($1, $2, true)`

	if _, err := r.Exec(ctx, q, transitionSetting, historyID.String()); err != nil {
		return fmt.Errorf("jobs: claim transition %s: %w", historyID, err)
	}
	return nil
}

// setStatus is the one place in this service that writes jobs.status.
//
// It is unexported and takes no route to the outside: the only caller is Transition, which has
// already validated the move and written the record 000402 will look for. A second caller would
// be the defect the guard exists to prevent.
func (postgresStore) setStatus(ctx context.Context, r db.Runner, id uuid.UUID, to Status) (Job, error) {
	const q = `UPDATE jobs SET status = $2 WHERE id = $1 RETURNING ` + jobColumns

	var j Job
	err := r.QueryRow(ctx, q, id, string(to)).
		Scan(&j.ID, &j.CustomerID, &j.Status, &j.CreatedAt, &j.UpdatedAt)
	switch {
	case errors.Is(err, db.ErrNoRows):
		// The row was locked a few statements ago, so this means it has been deleted
		// underneath us — which nothing in this platform does (Docs/10 §3.3 has no soft
		// deletes and SHIP-171 pseudonymises). Reported rather than assumed away.
		return Job{}, fmt.Errorf("jobs: %s vanished mid-transition: %w", id, ErrJobNotFound)
	case err != nil:
		return Job{}, fmt.Errorf("jobs: move %s to %s: %w", id, to, err)
	}
	return j, nil
}

// history reads a job's transitions, oldest first.
//
// Written now because a status timeline is what SHIP-77 shows a customer and what support reads
// during a dispute, and because a test that only ever writes history cannot tell whether the
// columns hold what they were given.
func (postgresStore) history(ctx context.Context, r db.Runner, jobID uuid.UUID) ([]StatusChange, error) {
	const q = `
		SELECT id, job_id, from_status, to_status, actor_type, actor_id, reason,
		       actor_recorded_at, server_recorded_at
		FROM job_status_history
		WHERE job_id = $1
		ORDER BY server_recorded_at, id`

	rows, err := r.Query(ctx, q, jobID)
	if err != nil {
		return nil, fmt.Errorf("jobs: read the history of %s: %w", jobID, err)
	}
	defer rows.Close()

	var out []StatusChange
	for rows.Next() {
		var (
			c        StatusChange
			actorID  *uuid.UUID
			reason   *string
			actorKnd string
		)
		if err := rows.Scan(&c.ID, &c.JobID, &c.From, &c.To, &actorKnd, &actorID, &reason,
			&c.ActorRecordedAt, &c.ServerRecordedAt); err != nil {
			return nil, fmt.Errorf("jobs: scanning the history of %s: %w", jobID, err)
		}
		c.Actor.Type = ActorType(actorKnd)
		if actorID != nil {
			c.Actor.ID = *actorID
		}
		if reason != nil {
			c.Reason = *reason
		}
		out = append(out, c)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("jobs: reading the history of %s: %w", jobID, err)
	}
	return out, nil
}
