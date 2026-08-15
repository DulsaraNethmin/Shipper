// SHIP-166: the statements behind the two-person suspension review.
//
// Methods on the same unexported [postgresStore] as the rest of this domain's SQL (Docs/10 §2.2).
// `suspension_reviews` is this domain's own table, in its own migration block, so there is no port
// and nothing in cmd/api: the queue and the write both stay here, which postgres_users.go's rule
// predicts — a statement spanning **two other domains'** tables belongs in the composition root, and
// one over a table this domain owns belongs here.
//
// The blank line below keeps this a file note rather than a second package comment.

package admin

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"

	"github.com/DulsaraNethmin/Shipper/services/core/internal/db"
)

// suspensionReviewColumns is the projection every read of a review shares.
//
// One constant rather than repeated lists, so that a column added to [SuspensionReview] fails to
// compile in one place rather than silently returning a zero value from three query sites.
const suspensionReviewColumns = `id, user_id, requested_by, reason,
	coalesce(approved_by, '00000000-0000-0000-0000-000000000000'::uuid),
	coalesce(approved_at, 'epoch'::timestamptz), status, created_at`

// insertSuspensionReview records a request.
//
// RETURNING rather than a second SELECT: `created_at` comes from the database default, and a round
// trip to read back what was just written is a window in which it could have changed.
//
// A second pending review for one account is refused by `uq_suspension_reviews_one_pending`, and
// that refusal is turned into [ErrSuspensionReviewOutstanding] here rather than checked for first —
// the same arrangement `identity` uses for duplicate registration, and for the same reason: a SELECT
// then an INSERT is a race that two moderators on one account lose in practice rather than in theory.
func (postgresStore) insertSuspensionReview(
	ctx context.Context,
	r db.Runner,
	review SuspensionReview,
) (SuspensionReview, error) {
	row := r.QueryRow(ctx, `
		INSERT INTO suspension_reviews (id, user_id, requested_by, reason, status)
		VALUES ($1, $2, $3, $4, $5)
		RETURNING `+suspensionReviewColumns,
		review.ID, review.UserID, review.RequestedBy, review.Reason, review.Status)

	created, err := scanSuspensionReview(row)
	if err != nil {
		if db.IsUniqueViolation(err, "uq_suspension_reviews_one_pending") {
			return SuspensionReview{}, ErrSuspensionReviewOutstanding
		}
		return SuspensionReview{}, fmt.Errorf("admin: recording a suspension review: %w", err)
	}
	return created, nil
}

// lockSuspensionReview reads a review and holds the row until the transaction ends.
//
// FOR UPDATE, and it is load-bearing rather than cautious: without it two administrators approving
// the same request at once would both read `pending`, both write an approval, and the second would
// overwrite the first's name in the one row that records who agreed. The lock makes the second read
// the first one's result and answer [ErrSuspensionReviewSettled].
//
// **It is also what makes the two-person check meaningful.** The check compares `requested_by` with
// the acting administrator, and a comparison against an unlocked read is a comparison against a value
// that may have changed by the time the UPDATE runs.
func (postgresStore) lockSuspensionReview(
	ctx context.Context,
	r db.Runner,
	reviewID uuid.UUID,
) (SuspensionReview, bool, error) {
	row := r.QueryRow(ctx,
		`SELECT `+suspensionReviewColumns+`
		   FROM suspension_reviews WHERE id = $1 FOR UPDATE`, reviewID)

	review, err := scanSuspensionReview(row)
	switch {
	case errors.Is(err, db.ErrNoRows):
		return SuspensionReview{}, false, nil
	case err != nil:
		return SuspensionReview{}, false, fmt.Errorf("admin: reading suspension review %s: %w",
			reviewID, err)
	}
	return review, true, nil
}

// approveSuspensionReview writes the second administrator's agreement.
//
// **The `approved_by <> requested_by` predicate is in the WHERE clause as well as in the CHECK**, and
// that is not belt and braces: the CHECK raises, which aborts the transaction, and a raise is the
// right answer for a repair script and the wrong one for an endpoint that has a message to give. The
// predicate here makes the statement affect no rows instead, which the caller turns into
// [ErrSameAdministrator].
//
// `status = 'pending'` is in the WHERE for the same reason it is checked above: the read is locked,
// so the two cannot disagree, and a statement that would silently re-approve a settled review is
// worth making impossible rather than merely unreachable.
func (postgresStore) approveSuspensionReview(
	ctx context.Context,
	r db.Runner,
	reviewID, approverID uuid.UUID,
) (SuspensionReview, error) {
	row := r.QueryRow(ctx, `
		UPDATE suspension_reviews
		   SET status = 'approved', approved_by = $2, approved_at = now()
		 WHERE id = $1
		   AND status = 'pending'
		   AND requested_by <> $2
		RETURNING `+suspensionReviewColumns, reviewID, approverID)

	approved, err := scanSuspensionReview(row)
	switch {
	case errors.Is(err, db.ErrNoRows):
		// The row was read and locked a statement ago, so the only predicate that can have
		// excluded it is the one this ticket exists for.
		return SuspensionReview{}, ErrSameAdministrator
	case err != nil:
		return SuspensionReview{}, fmt.Errorf("admin: approving suspension review %s: %w",
			reviewID, err)
	}
	return approved, nil
}

// pendingSuspensionReviews reads the requests waiting for a second administrator, oldest first.
//
// Served by `idx_suspension_reviews_pending`, which is partial on `status = 'pending'` — the queue is
// a small window on a table that keeps every approved review for ever.
//
// No cursor. The pending set is bounded by how many accounts are under review at once, which is a
// handful rather than a page, and `admin.QueueQuery`'s cursor exists because somebody had a page to
// render. Widening it is additive when somebody does.
func (postgresStore) pendingSuspensionReviews(
	ctx context.Context,
	r db.Runner,
	limit int,
) ([]SuspensionReview, error) {
	if limit <= 0 {
		limit = defaultPendingReviewLimit
	}

	rows, err := r.Query(ctx,
		`SELECT `+suspensionReviewColumns+`
		   FROM suspension_reviews
		  WHERE status = 'pending'
		  ORDER BY created_at, id
		  LIMIT $1`, limit)
	if err != nil {
		return nil, fmt.Errorf("admin: reading the pending suspension reviews: %w", err)
	}
	defer rows.Close()

	var out []SuspensionReview
	for rows.Next() {
		review, err := scanSuspensionReview(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, review)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("admin: reading the pending suspension reviews: %w", err)
	}
	return out, nil
}

// defaultPendingReviewLimit is how many reviews one read returns when the caller names no limit.
const defaultPendingReviewLimit = 100

// scanSuspensionReview reads one row of [suspensionReviewColumns].
//
// The two nullable columns are coalesced in the projection rather than scanned through pointers, so
// "not yet approved" arrives as the zero value at every call site — the same arrangement
// `internal/identity` uses for `users.name`. `SuspensionReview.Pending` is what a caller asks
// instead of comparing either against zero.
func scanSuspensionReview(row interface{ Scan(...any) error }) (SuspensionReview, error) {
	var review SuspensionReview
	if err := row.Scan(&review.ID, &review.UserID, &review.RequestedBy, &review.Reason,
		&review.ApprovedBy, &review.ApprovedAt, &review.Status, &review.CreatedAt); err != nil {
		return SuspensionReview{}, err
	}
	return review, nil
}
