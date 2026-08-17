package admin

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
// There is no repository interface, per Docs/10 §2.2 and Docs/06 §4.1. The guarantees this file
// rests on hardest are PostgreSQL's — uq_disputes_open_per_job and uq_disputes_idempotency are both
// *partial* unique indexes, so a job accumulates disputes over its life while never having two open
// at once and never having one key raise two — and an interface designed to keep the database
// swappable would hide exactly the mechanisms that make the rules correct. A mock would accept the
// second open dispute this store exists to have refused.
//
// Every method takes a db.Runner as its first argument after ctx, so the caller decides whether the
// work stands alone or joins a transaction it already opened (Docs/10 §3.2).
//
// # What this file does not query
//
// `jobs`, `bids` and `users`. All three are other domains' tables — `users` is shared and readable,
// but this domain has no reason to: who the complainant is comes from the token, and which side of
// the job they are on is [JobParties]' answer, resolved from the job and its accepted bid in
// cmd/api. The boundary lint reads imports and cannot see a SELECT, so this is a rule that holds by
// being written down and followed.
type postgresStore struct{}

// disputeColumns is every column of a dispute, in the order [scanDispute] reads them.
//
// resolved_at has no COALESCE because PostgreSQL's NULL has no representation in time.Time, and
// because the distinction it carries is the whole of [Dispute.Open]. `outcome` and `resolved_by`
// (SHIP-164, `000804`) are null under the same condition and for the same reason —
// `ck_disputes_resolution` holds all three together.
const disputeColumns = `
	id, job_id, complainant_id, complainant_party, category, description, desired_outcome,
	occurred_at, evidence, idempotency_key, resolved_at, outcome, resolved_by,
	created_at, updated_at`

// scanDispute reads one row of [disputeColumns].
//
// One function rather than a copy of the Scan call at each call site: a column added to
// disputeColumns and not here is a scan mismatch at the first call, which is the failure worth
// having.
func scanDispute(row pgx.Row) (Dispute, error) {
	var (
		d          Dispute
		key        *string
		resolvedAt *time.Time
		outcome    *string
		resolvedBy *uuid.UUID
	)

	if err := row.Scan(
		&d.ID, &d.JobID, &d.ComplainantID, &d.ComplainantParty, &d.Category,
		&d.Description, &d.DesiredOutcome, &d.OccurredAt, &d.Evidence,
		&key, &resolvedAt, &outcome, &resolvedBy, &d.CreatedAt, &d.UpdatedAt,
	); err != nil {
		return Dispute{}, err
	}

	// Four nullable columns, and each null means something the zero value says just as well: no
	// request behind the row, and not resolved yet.
	if key != nil {
		d.Key = *key
	}
	if resolvedAt != nil {
		d.ResolvedAt = *resolvedAt
	}
	if outcome != nil {
		d.Outcome = Outcome(*outcome)
	}
	if resolvedBy != nil {
		d.ResolvedBy = *resolvedBy
	}
	return d, nil
}

// insertDispute raises a dispute, or reports that this key already raised one.
//
// # ON CONFLICT DO NOTHING, and every word of that is load-bearing
//
// **ON CONFLICT** rather than an insert whose unique violation is caught: a violation aborts the
// surrounding transaction, and this insert runs inside one that still has a status transition to
// make. Catching the error would mean unwinding to a savepoint to ask a question the conflict has
// already answered.
//
// **DO NOTHING** rather than DO UPDATE: a retry must return what was raised, not overwrite it with
// a second attempt's account of the same incident.
//
// **The index is inferred by its columns and its predicate**, because uq_disputes_idempotency is a
// partial index and therefore has no constraint name to name. The WHERE clause here is not a filter
// on rows; it is how PostgreSQL is told which index this statement expects.
//
// # Naming the idempotency index as the arbiter is what tells the two conflicts apart
//
// This table carries two unique indexes and a request can meet either. PostgreSQL checks the
// arbiter first and abandons the insert without touching the rest when it conflicts there — so a
// genuine retry, which conflicts on the key, comes back as `raised = false` and is answered from
// the row it already wrote. A *fresh* key against a job that already has an open dispute does not
// conflict on the arbiter, reaches uq_disputes_open_per_job, and raises — which is translated to
// [ErrDisputeAlreadyOpen] below rather than reported as a database fault.
//
// Under a concurrent duplicate the second statement waits on the first's speculative insertion,
// then finds the committed row and returns none — which is why the caller's follow-up read is what
// answers, rather than this returning a partially written row.
//
// raised is false when the key had already raised something on this job. The existing row is not
// read here: what to do about it is [Service.RaiseDispute]'s decision.
func (postgresStore) insertDispute(ctx context.Context, r db.Runner, d Dispute) (Dispute, bool, error) {
	const q = `
		INSERT INTO disputes
			(id, job_id, complainant_id, complainant_party, category, description,
			 desired_outcome, occurred_at, evidence, idempotency_key)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, coalesce($9, '{}'::text[]), nullif($10, ''))
		ON CONFLICT (job_id, idempotency_key) WHERE idempotency_key IS NOT NULL DO NOTHING
		RETURNING ` + disputeColumns

	created, err := scanDispute(r.QueryRow(ctx, q,
		d.ID, d.JobID, d.ComplainantID, string(d.ComplainantParty), string(d.Category),
		d.Description, d.DesiredOutcome, d.OccurredAt, d.Evidence, d.Key))

	switch {
	case errors.Is(err, db.ErrNoRows):
		return Dispute{}, false, nil

	case db.IsUniqueViolation(err, "uq_disputes_open_per_job"):
		return Dispute{}, false, fmt.Errorf("admin: %s already has an open dispute: %w",
			d.JobID, ErrDisputeAlreadyOpen)

	case err != nil:
		return Dispute{}, false, fmt.Errorf("admin: raising a dispute on %s: %w", d.JobID, err)
	}
	return created, true, nil
}

// disputeRaisedBy is what a key already raised on a job, if anything.
//
// Read after [postgresStore.insertDispute] declines, which is its only caller: at READ COMMITTED
// each statement takes a fresh snapshot, so a row committed by the request that won a race is
// visible to this one even though the transaction around it began earlier.
func (postgresStore) disputeRaisedBy(
	ctx context.Context,
	r db.Runner,
	jobID uuid.UUID,
	key string,
) (Dispute, bool, error) {
	const q = `
		SELECT ` + disputeColumns + `
		FROM disputes
		WHERE job_id = $1 AND idempotency_key = $2`

	d, err := scanDispute(r.QueryRow(ctx, q, jobID, key))
	switch {
	case errors.Is(err, db.ErrNoRows):
		return Dispute{}, false, nil
	case err != nil:
		return Dispute{}, false, fmt.Errorf("admin: reading what %s raised on %s: %w", key, jobID, err)
	}
	return d, true, nil
}

// openDispute is the dispute on a job that is still awaiting an outcome, if there is one.
//
// Comma-ok rather than a sentinel error, because "no dispute on this job" is the ordinary state of
// every job and reporting it as an error would make the normal path the exceptional one.
//
// **No FOR UPDATE.** Locking the rows that exist cannot stop the row that does not yet exist from
// being inserted, so a lock here would buy nothing that uq_disputes_open_per_job does not already
// guarantee — and it would suggest to the next reader that the check is what enforces the rule. It
// is not: this read exists so that the ordinary case answers with a message a person can act on,
// and the index is what is right about the race.
func (postgresStore) openDispute(ctx context.Context, r db.Runner, jobID uuid.UUID) (Dispute, bool, error) {
	const q = `
		SELECT ` + disputeColumns + `
		FROM disputes
		WHERE job_id = $1 AND resolved_at IS NULL`

	d, err := scanDispute(r.QueryRow(ctx, q, jobID))
	switch {
	case errors.Is(err, db.ErrNoRows):
		return Dispute{}, false, nil
	case err != nil:
		return Dispute{}, false, fmt.Errorf("admin: reading the open dispute on %s: %w", jobID, err)
	}
	return d, true, nil
}
