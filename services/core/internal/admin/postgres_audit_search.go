// SHIP-165: the one statement that reads `audit_log`.
//
// A method on the same unexported [postgresStore] as the rest of this domain's SQL (Docs/10 §2.2),
// in a file of its own rather than beside the writer.
//
// # Why it is not in postgres_audit.go
//
// That file's header makes a claim about itself — "there is exactly one statement here, and that is
// the file's whole point. No SELECT, no UPDATE, no DELETE" — and names this ticket as the one that
// would want a read "shaped by the searches Docs/04 §9 asks for … rather than whatever this file
// happened to leave behind". Adding a SELECT there would have falsified the header while satisfying
// the ticket. Two files, two directions, and the property the writer's header asserts stays true.
//
// **There is still no UPDATE and no DELETE anywhere in this package**, which is the invariant both
// files exist to keep visible. `000003`s triggers are what make it true of a `psql` prompt.
//
// The blank line below keeps this a file note rather than a second package comment.

package admin

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/DulsaraNethmin/Shipper/services/core/internal/db"
)

// auditColumns is every column of an entry, in the order [scanAuditRecord] reads them.
//
// **All of them, and that is a decision.** The trail is what holds administrators to account
// (Docs/04 §9); an audit viewer that showed a filtered version of the record would be a second
// record, and the one somebody checks would be the wrong one. What makes publishing the whole row
// safe is upstream of here — nothing commercial is ever written into an entry, and a ticket tempted
// to put a budget in one has made the mistake a layer earlier.
const auditColumns = `id, actor_type, actor_id, action, target_type, target_id,
                      reason, metadata::text, created_at`

// searchAuditEntries returns one page of the trail, newest first.
//
// # Every filter is optional and none is built by concatenation
//
// One statement, one plan, and one place the disclosure rules can be read. A predicate assembled
// from the query string would be the injection this parameterisation makes impossible, and a
// statement per filter combination would be sixteen plans nobody can compare.
//
// Each clause is `$n IS NULL OR column = $n` rather than a nullable comparison, because `column =
// NULL` is NULL rather than true and would return nothing at all — an unfiltered search answering
// "no entries" is the worst failure this endpoint has, since an empty trail reads as an innocent
// system.
//
// # The date bounds tile
//
// `created_at >= from` and `created_at < to`. Consecutive days must not overlap: a reader asking for
// the 3rd and then the 4th must not see an entry twice, and an inclusive upper bound at midnight
// shows every entry written in that instant on both days.
//
// # The ordering is total, and the cursor is why
//
// `(created_at DESC, id DESC)`. `created_at` is emphatically not unique here — [Auditor.Record]
// gives every entry written in one transaction the same injected instant, by design — so a
// single-column cursor would skip an entry or repeat one on exactly the rows most worth reading
// together. The identifier breaks the tie and is a v7, so entries appended in one transaction sort
// in the order they were appended.
//
// # No index is added for this, and that is measured rather than assumed
//
// `000003` created three: `idx_audit_log_actor`, `idx_audit_log_target` and `idx_audit_log_created`,
// which are exactly the three axes the *Done when* names. The fourth filter — action — has none, and
// is a filter over a set the other three have already narrowed. A migration adding one belongs to
// whoever has a row count to point at.
func (postgresStore) searchAuditEntries(
	ctx context.Context,
	r db.Runner,
	q AuditQuery,
) ([]AuditRecord, error) {
	const query = `
		SELECT ` + auditColumns + `
		FROM audit_log
		WHERE ($1::uuid IS NULL OR actor_id = $1)
		  AND ($2::uuid IS NULL OR target_id = $2)
		  AND ($3 = '' OR action = $3)
		  AND ($4::timestamptz IS NULL OR created_at >= $4)
		  AND ($5::timestamptz IS NULL OR created_at <  $5)
		  AND ($6::timestamptz IS NULL OR (created_at, id) < ($6, $7))
		ORDER BY created_at DESC, id DESC
		LIMIT $8`

	// A nil rather than a zero value for every absent bound. `< (NULL, …)` is NULL rather than
	// true, so the predicate has to be skipped rather than satisfied; the same trap the account
	// search and the exception queue both record, and the zero time would work today and stop
	// working the first time somebody backdated a fixture.
	var (
		after   any
		afterID any
	)
	if !q.After.Zero() {
		after, afterID = q.After.CreatedAt, q.After.EntryID
	}

	rows, err := r.Query(ctx, query,
		nilUUID(q.ActorID), nilUUID(q.TargetID), q.Action.String(),
		nilTime(q.From), nilTime(q.To), after, afterID, q.Limit)
	if err != nil {
		return nil, fmt.Errorf("admin: reading the audit trail: %w", err)
	}
	defer rows.Close()

	var out []AuditRecord
	for rows.Next() {
		record, err := scanAuditRecord(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, record)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("admin: reading the audit trail: %w", err)
	}
	return out, nil
}

// scanAuditRecord reads one row of [auditColumns].
func scanAuditRecord(row interface{ Scan(...any) error }) (AuditRecord, error) {
	var (
		e       AuditRecord
		actorID *uuid.UUID
		reason  *string
	)

	if err := row.Scan(&e.ID, &e.ActorType, &actorID, &e.Action, &e.TargetType, &e.TargetID,
		&reason, &e.Metadata, &e.CreatedAt); err != nil {
		return AuditRecord{}, fmt.Errorf("admin: reading an audit entry: %w", err)
	}

	// NULL is a system actor, which has no account — `ck_audit_log_actor_id` requires it. The
	// zero uuid says the same thing in Go, so there is one representation of "nobody" rather
	// than a pointer every reader has to check.
	if actorID != nil {
		e.ActorID = *actorID
	}
	if reason != nil {
		e.Reason = *reason
	}
	return e, nil
}

// nilUUID is v as a parameter, or NULL where it is the zero value.
//
// Written out rather than passed inline so that the reason is stated once: the all-zeroes uuid is a
// *value*, not an absence, and passing it into `$1::uuid IS NULL OR actor_id = $1` would filter the
// search down to entries by an account that cannot exist — an empty page that looks exactly like a
// clean trail.
func nilUUID(v uuid.UUID) any {
	if v == uuid.Nil {
		return nil
	}
	return v
}

// nilTime is t as a parameter, or NULL where it is the zero value. See [nilUUID].
func nilTime(t time.Time) any {
	if t.IsZero() {
		return nil
	}
	return t.UTC()
}
