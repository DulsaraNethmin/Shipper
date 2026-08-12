package jobs

import (
	"context"
	"fmt"

	"github.com/google/uuid"

	"github.com/DulsaraNethmin/Shipper/services/core/internal/db"
)

// Job expiry (SHIP-68).
//
// Docs/02 §6.3: an Open job leaves Open at whichever comes first — fourteen days after
// publication, or the moment its own pickup date passes. Three pieces make that true, and they
// are deliberately in three places:
//
//   - 000406 sets the deadline as the job becomes Open, so no route into Open can forget it and
//     the "earlier of" is computed once, in SQL, at the moment both inputs are known;
//   - this file says which jobs are due and what happens to one;
//   - cmd/worker/tasks_jobs.go runs it on a ticker and claims the rows.
//
// # Why the claim query lives here and the loop does not
//
// Which jobs are due is this domain's rule — it names this domain's table, this domain's status
// and this domain's deadline column, and a copy of it in cmd/worker would be a second answer to a
// question 000406 already decided. How work is claimed is the worker's: cmd/worker.ClaimIDs
// refuses a query without FOR UPDATE SKIP LOCKED, because a claim missing either produces no
// error and no test failure, only two workers doing the same work or queueing behind each other.
//
// So the query is declared here and executed there, which lets each half be exactly what it is.

// ExpiryReason is recorded against every expiry transition.
//
// A fixed string rather than a formatted one. It is read by support and shown in the customer's
// status timeline (SHIP-77), and a reason that varied by job would be a sentence nobody could
// search for. The job's own expires_at says when, and job_status_history says who: the platform.
const ExpiryReason = "The job expired without being awarded (Docs/02 §6.3)."

// ExpiryBatch is how many due jobs one pass claims.
//
// Bounded rather than unlimited because a pass is one transaction, and a transaction that claimed
// every due row after a long outage would hold them all — and its locks — for as long as the
// whole backlog took. A hundred at a time drains at the same rate over several passes, and each
// pass either commits or releases what it took.
const ExpiryBatch = 100

// ExpiryClaim selects the Open jobs whose deadline has passed, and locks them.
//
// $1 is the instant to judge against and $2 is the batch size. The instant is a parameter rather
// than now() so that the caller's clock decides — Docs/10 §6.3 puts every scheduled task behind an
// injected clock, and a query that asked the database for the time would be a task no test could
// move without waiting fourteen days.
//
// ORDER BY expires_at drains the longest-overdue first, which matters after an outage: the jobs
// that have been misleading providers longest stop doing so first.
//
// idx_jobs_open_expiry (000406) is a partial index on exactly this predicate, so the sweep reads
// the live marketplace rather than every job the platform has ever had.
const ExpiryClaim = `
	SELECT id
	FROM jobs
	WHERE status = 'Open'
	  AND expires_at IS NOT NULL
	  AND expires_at <= $1
	ORDER BY expires_at
	FOR UPDATE SKIP LOCKED
	LIMIT $2`

// Expire ends one claimed job, as the platform (SHIP-68).
//
// Docs/02 §2's table has Open → Cancelled and describes it as "job expires unclaimed", which is
// exactly this. Nothing here decides that the move is legal: [Service.Transition] asks
// [Permitted], writes the history row 000402 requires, and emits the domain event inside the
// caller's transaction. An expiry is therefore indistinguishable, in the record, from any other
// transition — except that its actor is the platform and its reason says why.
//
// # The caller has already locked the row, and that is the whole design
//
// r is the worker's transaction, and the job's id came from [ExpiryClaim] running in it under
// FOR UPDATE SKIP LOCKED. Transition locks the row again, which costs one statement against a
// lock this transaction already holds and buys the guarantee that the status it acts on is the
// status the job has now. A second worker sweeping at the same time was handed different rows.
//
// # A job that moved between the claim and the transition is not an error
//
// It cannot happen while the claim holds the lock — but a job that was cancelled by its owner in
// the moment before, or awarded, simply is not selected by the claim any more. There is
// deliberately no re-check here: adding one would be a second copy of the claim's predicate, and
// [Service.Transition] refuses an impossible move on its own.
func (s *Service) Expire(ctx context.Context, r db.Runner, jobID uuid.UUID) (Job, error) {
	expired, err := s.Transition(ctx, r, Move{
		JobID:  jobID,
		To:     StatusCancelled,
		Actor:  System(),
		Reason: ExpiryReason,
	})
	if err != nil {
		return Job{}, fmt.Errorf("jobs: expiring %s: %w", jobID, err)
	}
	return expired, nil
}
