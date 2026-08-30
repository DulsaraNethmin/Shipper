package admin

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/DulsaraNethmin/Shipper/services/core/internal/db"
)

// SHIP-155a's persistence, on [postgresStore] like the rest of this domain's.
//
// A file of its own rather than three more methods in postgres.go, which is the arrangement this
// package already uses — postgres_notes.go, postgres_users.go, postgres_suspension.go. The type is
// shared and the files are per subject, so a reader looking for how a report is written is not
// reading past how a dispute is resolved.
//
// # What this file does not query
//
// `jobs`, `bids`, `job_messages` and `users`. All four are other domains' tables. Who the reporter
// is comes from the token; which side of the job they are on is [JobParties]' answer; whether a
// message is on the job is [JobMessages]'. The boundary lint reads imports and cannot see a SELECT,
// so this is a rule that holds by being written down and followed — postgres.go says the same.

// reportColumns is every column of a report, in the order [scanReport] reads them.
//
// `message_id` has no COALESCE: PostgreSQL's NULL has no representation in uuid.UUID, and the
// distinction it carries is the whole of `ck_reports_subject`. There is no `updated_at` to read —
// reports are appended and never edited (`000805`).
const reportColumns = `
	id, job_id, subject_type, message_id, reporter_id, reporter_party, reason, description,
	idempotency_key, created_at`

// scanReport reads one row of [reportColumns].
//
// One function rather than a copy of the Scan call at each call site: a column added to
// reportColumns and not here is a scan mismatch at the first call, which is the failure worth
// having. [scanDispute]'s reasoning.
func scanReport(row pgx.Row) (Report, error) {
	var (
		rep       Report
		messageID *uuid.UUID
		key       *string
	)

	if err := row.Scan(
		&rep.ID, &rep.JobID, &rep.Subject, &messageID, &rep.ReporterID, &rep.ReporterParty,
		&rep.Reason, &rep.Description, &key, &rep.CreatedAt,
	); err != nil {
		return Report{}, err
	}

	// Two nullable columns, and each null means what the zero value says just as well: this
	// report is about the job itself, and no client request stands behind the row.
	if messageID != nil {
		rep.MessageID = *messageID
	}
	if key != nil {
		rep.Key = *key
	}
	return rep, nil
}

// insertReport raises a report, or reports that this key already raised one on this job.
//
// # ON CONFLICT DO NOTHING, for [postgresStore.insertDispute]'s reasons
//
// **ON CONFLICT** rather than catching a unique violation: a violation aborts the surrounding
// transaction, and this may be called inside one.
//
// **DO NOTHING** rather than DO UPDATE: a retry must return what was raised, not overwrite it with
// a second attempt's account of the same complaint.
//
// **The index is inferred by its columns and its predicate**, because `uq_reports_idempotency` is a
// partial index and therefore has no constraint name to name. The WHERE clause is not a filter on
// rows; it is how PostgreSQL is told which index this statement expects.
//
// # Unlike the dispute insert, there is only one index to conflict on
//
// `disputes` carries two, and naming the idempotency one as the arbiter is what tells a retry from
// a second raise. This table carries one, because a job may accumulate any number of reports
// (`000805`) — so a conflict here means one thing, and the second answer that file's sibling needs
// does not arise. `raised = false` is a retry and nothing else.
//
// Under a concurrent duplicate the second statement waits on the first's speculative insertion, then
// finds the committed row and returns none — which is why the caller's follow-up read is what
// answers, rather than this returning a partially written row.
func (postgresStore) insertReport(ctx context.Context, r db.Runner, rep Report) (Report, bool, error) {
	const q = `
		INSERT INTO reports
			(id, job_id, subject_type, message_id, reporter_id, reporter_party, reason,
			 description, idempotency_key)
		VALUES ($1, $2, $3, nullif($4, '00000000-0000-0000-0000-000000000000'::uuid), $5, $6, $7,
		        $8, nullif($9, ''))
		ON CONFLICT (job_id, idempotency_key) WHERE idempotency_key IS NOT NULL DO NOTHING
		RETURNING ` + reportColumns

	created, err := scanReport(r.QueryRow(ctx, q,
		rep.ID, rep.JobID, string(rep.Subject), rep.MessageID, rep.ReporterID,
		string(rep.ReporterParty), string(rep.Reason), rep.Description, rep.Key))

	switch {
	case errors.Is(err, db.ErrNoRows):
		return Report{}, false, nil
	case err != nil:
		return Report{}, false, fmt.Errorf("admin: reporting %s: %w", rep.JobID, err)
	}
	return created, true, nil
}

// reportRaisedBy is what a key already raised on a job, if anything.
//
// Read after [postgresStore.insertReport] declines, which is its only caller: at READ COMMITTED each
// statement takes a fresh snapshot, so a row committed by the request that won a race is visible to
// this one even though the transaction around it began earlier.
func (postgresStore) reportRaisedBy(
	ctx context.Context,
	r db.Runner,
	jobID uuid.UUID,
	key string,
) (Report, bool, error) {
	const q = `
		SELECT ` + reportColumns + `
		FROM reports
		WHERE job_id = $1 AND idempotency_key = $2`

	rep, err := scanReport(r.QueryRow(ctx, q, jobID, key))
	switch {
	case errors.Is(err, db.ErrNoRows):
		return Report{}, false, nil
	case err != nil:
		return Report{}, false, fmt.Errorf("admin: reading what %s raised on %s: %w", key, jobID, err)
	}
	return rep, true, nil
}

// reportsOn is every report about a job, newest first.
//
// Reads `idx_reports_job`, whose trailing `created_at DESC` makes the ordering an index scan rather
// than a sort. Not paged: this answers "what has been reported about this one job", which is tens at
// most. SHIP-156's platform-wide queue is a different read against `idx_reports_queue`, and it is
// the one that needs a cursor.
func (postgresStore) reportsOn(ctx context.Context, r db.Runner, jobID uuid.UUID) ([]Report, error) {
	const q = `
		SELECT ` + reportColumns + `
		FROM reports
		WHERE job_id = $1
		ORDER BY created_at DESC, id DESC`

	rows, err := r.Query(ctx, q, jobID)
	if err != nil {
		return nil, fmt.Errorf("admin: reading the reports on %s: %w", jobID, err)
	}
	defer rows.Close()

	reports := make([]Report, 0)
	for rows.Next() {
		rep, err := scanReport(rows)
		if err != nil {
			return nil, fmt.Errorf("admin: reading a report on %s: %w", jobID, err)
		}
		reports = append(reports, rep)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("admin: reading the reports on %s: %w", jobID, err)
	}
	return reports, nil
}
