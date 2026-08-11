package main

import (
	"context"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/DulsaraNethmin/Shipper/services/core/internal/db"
)

// ClaimIDs runs a task's claim query and returns the identifiers it locked.
//
// The query is the task's own, hand-written like every other query in this service (Docs/10
// §3.1). What this adds is one line of ceremony and one check:
//
//	SELECT id FROM jobs
//	 WHERE status = 'Open' AND expires_at <= now()
//	 ORDER BY expires_at
//	 FOR UPDATE SKIP LOCKED
//	 LIMIT 100
//
// # Why the check exists
//
// A claim query missing SKIP LOCKED still works. That is the entire problem. It returns the
// right rows, the tests pass, and the defect appears only when a second worker runs: instead of
// taking the rows the first one left, it blocks behind the first one's locks until that
// transaction commits — so two workers do the work of one, slowly, and a long pass becomes a
// queue rather than a share.
//
// A claim missing FOR UPDATE is worse and just as quiet. The rows are read but not locked, so
// two workers claim the same rows and both act on them: two expiry events for one job, two
// notifications, two of whatever the task does.
//
// Neither failure produces an error, and neither is visible in a single-worker test. Refusing
// the query outright is the only place the mistake can be caught cheaply, and it is caught on
// the first pass rather than on the first busy day.
func ClaimIDs(ctx context.Context, r db.Runner, query string, args ...any) ([]uuid.UUID, error) {
	if err := checkClaim(query); err != nil {
		return nil, err
	}

	rows, err := r.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("worker: claim: %w", err)
	}

	ids, err := pgx.CollectRows(rows, pgx.RowTo[uuid.UUID])
	if err != nil {
		return nil, fmt.Errorf("worker: collecting claimed ids: %w", err)
	}
	return ids, nil
}

// checkClaim reads the query looking for the two clauses that make a claim a claim.
//
// It is a text check, which is blunt: a query that mentions the words in a comment satisfies it,
// and one that spreads them across a construction the reader would not recognise does too.
// That is an acceptable trade, because this is not a security control — it is a check against
// forgetting, and the thing being forgotten is always the literal clause.
func checkClaim(query string) error {
	normalised := strings.Join(strings.Fields(strings.ToLower(query)), " ")

	for _, clause := range []string{"for update", "skip locked"} {
		if !strings.Contains(normalised, clause) {
			return fmt.Errorf("worker: a claim query must say %q, and this one does not: %s\n"+
				"without it two workers do not share the due rows — they either queue behind "+
				"each other or claim the same work twice, and neither shows up as an error "+
				"(Docs/10 §6.2)", clause, query)
		}
	}
	return nil
}
