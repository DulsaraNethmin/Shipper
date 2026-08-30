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

// --- SHIP-156: the reported jobs and messages queue ----------------------------------------------

// reportEntryColumns is every column of a queue entry, in the order [scanReportEntry] reads them.
//
// **Deliberately not [reportColumns].** The description is up to four thousand characters of
// somebody setting out what is wrong, and a page of fifty is two hundred thousand of them that
// nothing on a queue screen renders — [disputeSummaryColumns] made the same cut for the same reason.
// It is structural as well as a saving: [ReportEntry] has nowhere to put a description, so widening
// this list fails to compile rather than quietly making the queue heavy.
//
// `idempotency_key` is absent for a second reason that is not about size. It is the reporter's own
// value, it identifies nothing an administrator can use, and a column read is the first step towards
// a field on the wire.
const reportEntryColumns = `
	id, job_id, subject_type, message_id, reporter_id, reporter_party, reason, created_at`

// reportQueue is Docs/04 §5's second moderation queue: every report, oldest first.
//
// # Oldest first, and the ordering is the document's
//
// Docs/04 §8 sets an acknowledgement target, so the oldest entry is the one closest to breaching it —
// the same reasoning `idx_disputes_open` and the verification queue are both built on. This reads
// `idx_reports_queue`, which `000805` created as `(created_at, id)` and which nothing read until this
// ticket.
//
// **There is no predicate and no second statement**, unlike the dispute queue. That queue has two
// halves because a dispute is open or settled and the two are ordered oppositely; a report has no
// such state — `000805` records at length why there is no `resolved_at` and no status vocabulary on
// this table — so there is one queue, one ordering and one index.
//
// # The cursor is the whole index key, which is why the tie-break is free
//
// `(created_at, id)` is exactly `idx_reports_queue`, so the row constructor is matched against the
// index rather than served from the heap — the widening `000804` deliberately did not make to
// `000800`'s index, made here at creation because this is a platform-wide queue rather than the
// tens-of-rows read `admin_notes` gets.
//
// An absent cursor is passed as NULL rather than as a zero time, because `> (NULL, …)` is NULL
// rather than true — the trap the dispute queue, the audit search and the account search all record.
func (postgresStore) reportQueue(ctx context.Context, r db.Runner, q ReportQuery) ([]ReportEntry, error) {
	const stmt = `
		SELECT ` + reportEntryColumns + `
		FROM reports
		WHERE ($1::timestamptz IS NULL OR (created_at, id) > ($1, $2::uuid))
		ORDER BY created_at ASC, id ASC
		LIMIT $3`

	var after, afterID any
	if !q.After.Zero() {
		after, afterID = q.After.RaisedAt.UTC(), q.After.ID
	}

	rows, err := r.Query(ctx, stmt, after, afterID, q.Limit)
	if err != nil {
		return nil, fmt.Errorf("admin: reading the report queue: %w", err)
	}
	defer rows.Close()

	entries := make([]ReportEntry, 0)
	for rows.Next() {
		entry, err := scanReportEntry(rows)
		if err != nil {
			return nil, err
		}
		entries = append(entries, entry)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("admin: reading the report queue: %w", err)
	}
	return entries, nil
}

// scanReportEntry reads one row of [reportEntryColumns].
//
// [scanReport]'s arrangement and its reason: a column added to the list and not here is a scan
// mismatch at the first call, which is the failure worth having.
func scanReportEntry(row pgx.Row) (ReportEntry, error) {
	var (
		e         ReportEntry
		messageID *uuid.UUID
	)

	if err := row.Scan(
		&e.ID, &e.JobID, &e.Subject, &messageID, &e.ReporterID, &e.ReporterParty,
		&e.Reason, &e.RaisedAt,
	); err != nil {
		return ReportEntry{}, fmt.Errorf("admin: reading a report queue entry: %w", err)
	}

	// NULL means what the zero value says: this report is about the job itself.
	if messageID != nil {
		e.MessageID = *messageID
	}
	return e, nil
}

// reportByID is one report with everything the reporter sent.
//
// found is false when there is no such report, which the caller turns into [ErrReportNotFound] —
// disclosed plainly, unlike intake's 404, because the caller here is an administrator holding a
// permission over the moderation queues. [postgresStore.disputeByID] takes the same position.
func (postgresStore) reportByID(ctx context.Context, r db.Runner, reportID uuid.UUID) (Report, bool, error) {
	const q = `
		SELECT ` + reportColumns + `
		FROM reports
		WHERE id = $1`

	rep, err := scanReport(r.QueryRow(ctx, q, reportID))
	switch {
	case errors.Is(err, db.ErrNoRows):
		return Report{}, false, nil
	case err != nil:
		return Report{}, false, fmt.Errorf("admin: reading report %s: %w", reportID, err)
	}
	return rep, true, nil
}
