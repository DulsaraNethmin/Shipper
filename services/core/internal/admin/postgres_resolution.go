// SHIP-164: the statements the dispute workflow runs.
//
// Methods on the same unexported [postgresStore] as the rest of this domain's SQL (Docs/10 §2.2), in
// a file of their own rather than beside `postgres.go`'s intake statements. That file's header
// describes what it does not query — "`jobs`, `bids` and `users`" — and that claim stays true here:
// a dispute's job is moved through the port and the guard, never by an `UPDATE jobs` in this
// package, and `000402`'s trigger refuses one anyway.
//
// **There is one `UPDATE` in this file and it is the only one in the package.** `postgres_audit.go`
// asserts that there is no `UPDATE` or `DELETE` on `audit_log` anywhere here, which is a claim about
// that table rather than about the domain; `disputes` is deliberately not append-only — `000800`
// said so at intake, naming this ticket — and what is append-only is the trail of what
// administrators did to it.
//
// The blank line below keeps this a file note rather than a second package comment.

package admin

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/DulsaraNethmin/Shipper/services/core/internal/db"
)

// disputeSummaryColumns is every column of a queue entry, in the order [scanDisputeSummary] reads
// them.
//
// **Deliberately not [disputeColumns].** The description, the desired outcome and the evidence are
// what an administrator reads when they open one dispute, and a page of them is four thousand
// characters a row that nothing on the screen renders. It is also structural: [DisputeSummary] has
// nowhere to put them, so a widened column list would not compile rather than quietly making the
// queue heavy.
const disputeSummaryColumns = `
	id, job_id, complainant_id, complainant_party, category, occurred_at, created_at,
	resolved_at, outcome, resolved_by`

// The two halves of Docs/04 §5's sixth queue, as two statements.
//
// # They are ordered oppositely, and both orderings are the document's
//
// Open disputes come **oldest first**: Docs/04 §8 sets an acknowledgement target of two business
// days and a resolution target of ten, so the oldest entry is the one closest to breaching one.
// Resolved disputes come **newest first**: a settled dispute is looked up to see what was decided,
// and the decision somebody is asking about is overwhelmingly a recent one. It is the same split the
// account and job searches make against the review queues.
//
// # Two statements rather than one with a direction flag, and the reason is the index
//
// A single statement picking its ordering column with a `CASE` is expressible and was rejected: an
// expression the planner cannot match against an index key turns both halves into a sort over every
// row the predicate returns, and the whole point of these two partial indexes is that neither half
// ever reads the other's rows. Two constants cost a branch and buy a plan per half.
//
//   - `idx_disputes_open` — `(created_at) WHERE resolved_at IS NULL`, built by `000800` for this
//     queue and read by nothing until this ticket.
//   - `idx_disputes_resolved` — `(resolved_at DESC, id DESC) WHERE resolved_at IS NOT NULL`,
//     `000804`s.
//
// The `IS NULL` / `IS NOT NULL` predicate is what lets the planner match either.
//
// # The cursor is a row constructor and its direction follows the ordering
//
// `>` ascending and `<` descending. One direction for both would page forwards through one half and
// immediately off the end of the other. The open half tie-breaks on `id` even though
// `idx_disputes_open` carries only `created_at`: the tie-break is served from the heap, which is
// what `000804`s header records about deliberately not widening `000800`s index.
const (
	openDisputeQueue = `
		SELECT ` + disputeSummaryColumns + `
		FROM disputes
		WHERE resolved_at IS NULL
		  AND ($1::timestamptz IS NULL OR (created_at, id) > ($1, $2::uuid))
		ORDER BY created_at ASC, id ASC
		LIMIT $3`

	resolvedDisputeQueue = `
		SELECT ` + disputeSummaryColumns + `
		FROM disputes
		WHERE resolved_at IS NOT NULL
		  AND ($1::timestamptz IS NULL OR (resolved_at, id) < ($1, $2::uuid))
		ORDER BY resolved_at DESC, id DESC
		LIMIT $3`
)

// disputeQueue returns one page of Docs/04 §5's sixth moderation queue.
//
// The state chooses the statement; see the pair above for why there are two.
//
// An absent cursor is passed as NULL rather than as a zero value, because `> (NULL, …)` is NULL
// rather than true — the same trap the audit search and the account search both record, and the zero
// time would work until somebody backdated a fixture.
func (postgresStore) disputeQueue(
	ctx context.Context,
	r db.Runner,
	q DisputeQuery,
) ([]DisputeSummary, error) {
	query := openDisputeQueue
	if q.State == DisputeStateResolved {
		query = resolvedDisputeQueue
	}

	var (
		after   any
		afterID any
	)
	if !q.After.Zero() {
		after, afterID = q.After.At.UTC(), q.After.ID
	}

	rows, err := r.Query(ctx, query, after, afterID, q.Limit)
	if err != nil {
		return nil, fmt.Errorf("admin: reading the dispute queue: %w", err)
	}
	defer rows.Close()

	var out []DisputeSummary
	for rows.Next() {
		entry, err := scanDisputeSummary(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, entry)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("admin: reading the dispute queue: %w", err)
	}
	return out, nil
}

// scanDisputeSummary reads one row of [disputeSummaryColumns].
func scanDisputeSummary(row interface{ Scan(...any) error }) (DisputeSummary, error) {
	var (
		s          DisputeSummary
		resolvedAt *time.Time
		outcome    *string
		resolvedBy *uuid.UUID
	)

	if err := row.Scan(
		&s.ID, &s.JobID, &s.ComplainantID, &s.ComplainantParty, &s.Category,
		&s.OccurredAt, &s.RaisedAt, &resolvedAt, &outcome, &resolvedBy,
	); err != nil {
		return DisputeSummary{}, fmt.Errorf("admin: reading a dispute queue entry: %w", err)
	}

	// The three resolution columns are NULL together (`ck_disputes_resolution`), and each null
	// means what the zero value says: this dispute is still open.
	if resolvedAt != nil {
		s.ResolvedAt = *resolvedAt
	}
	if outcome != nil {
		s.Outcome = Outcome(*outcome)
	}
	if resolvedBy != nil {
		s.ResolvedBy = *resolvedBy
	}
	return s, nil
}

// disputeByID is one dispute with everything on it, open or resolved.
//
// Comma-ok rather than a sentinel, matching [postgresStore.openDispute]: which error a missing row
// deserves is the service's decision, and this layer reports what it found.
//
// **No FOR UPDATE.** This is the investigation read and it takes no lock, because holding one across
// however long an administrator spends reading would block the resolution of the very dispute they
// are looking at. The lock belongs to the act, and [postgresStore.lockDispute] is where it is taken.
func (postgresStore) disputeByID(
	ctx context.Context,
	r db.Runner,
	id uuid.UUID,
) (Dispute, bool, error) {
	const q = `
		SELECT ` + disputeColumns + `
		FROM disputes
		WHERE id = $1`

	d, err := scanDispute(r.QueryRow(ctx, q, id))
	switch {
	case errors.Is(err, db.ErrNoRows):
		return Dispute{}, false, nil
	case err != nil:
		return Dispute{}, false, fmt.Errorf("admin: reading dispute %s: %w", id, err)
	}
	return d, true, nil
}

// lockDispute reads a dispute for update, so that only one resolution of it can proceed.
//
// # This lock is what makes two moderators on one queue safe
//
// Docs/04 §5's queues are read by several people and the ordinary race is two of them opening the
// same entry. Without the lock both read an open dispute, both write `resolved_at`, both run a
// transition, and the second overwrites the first's outcome — with two audit entries recording two
// different findings against one delivery and nothing to say which one stands.
//
// With it the second waits, and at READ COMMITTED the `SELECT … FOR UPDATE` re-reads the row the
// first committed rather than the snapshot it started from. So the second sees `resolved_at` set and
// is answered [ErrDisputeAlreadyResolved] — "somebody got there first", which is the answer
// [Enforcement.SetStanding] and [Verifications.Decide] both give to the same situation.
//
// **Unlike `openDispute` there is a row to lock**, which is why the lock is right here and was wrong
// there: that read is looking for a row that may not exist, and locking the rows that do cannot stop
// the one that does not from being inserted — `uq_disputes_open_per_job` is what is right about that
// race. Here the row is named by its identifier and locking it is exactly the guarantee wanted.
func (postgresStore) lockDispute(
	ctx context.Context,
	r db.Runner,
	id uuid.UUID,
) (Dispute, bool, error) {
	const q = `
		SELECT ` + disputeColumns + `
		FROM disputes
		WHERE id = $1
		FOR UPDATE`

	d, err := scanDispute(r.QueryRow(ctx, q, id))
	switch {
	case errors.Is(err, db.ErrNoRows):
		return Dispute{}, false, nil
	case err != nil:
		return Dispute{}, false, fmt.Errorf("admin: locking dispute %s: %w", id, err)
	}
	return d, true, nil
}

// resolveDispute writes the outcome, the administrator and the instant, together.
//
// # The three columns move in one statement because `ck_disputes_resolution` requires it
//
// `000804`'s constraint is that all three are NULL or all three are set, so there is no order in
// which they could be written separately that the database would accept. That is the constraint
// doing its job: a half-resolved dispute — off `idx_disputes_open` with nothing documented about it
// — is unreachable rather than merely unwritten.
//
// # `WHERE resolved_at IS NULL` is a second lock, and it is not redundant
//
// [postgresStore.lockDispute] has already refused a resolved dispute one statement earlier, with a
// message a person can act on. This predicate is what is true of the *table* however it is written
// to — the same division of labour the intake statements record between a read that produces a good
// message and an index that is right about the race. A caller that skipped the lock updates nothing
// and is told so, rather than overwriting a settled outcome.
//
// The `updated_at` column is left to `disputes_set_updated_at` (`000800`), which is the trigger's to
// set; naming it here would be a second writer of one column.
func (postgresStore) resolveDispute(
	ctx context.Context,
	r db.Runner,
	id uuid.UUID,
	outcome Outcome,
	resolvedBy uuid.UUID,
	at time.Time,
) error {
	const q = `
		UPDATE disputes
		SET resolved_at = $2, outcome = $3, resolved_by = $4
		WHERE id = $1 AND resolved_at IS NULL`

	tag, err := r.Exec(ctx, q, id, at.UTC(), string(outcome), resolvedBy)
	if err != nil {
		return fmt.Errorf("admin: resolving dispute %s: %w", id, err)
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("admin: resolving dispute %s: %w", id, ErrDisputeAlreadyResolved)
	}
	return nil
}
