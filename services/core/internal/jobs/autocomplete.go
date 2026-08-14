package jobs

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/DulsaraNethmin/Shipper/services/core/internal/db"
)

// The seventy-two hour auto-complete (SHIP-119).
//
// Docs/02 §6.1: a job recorded as Delivered auto-completes seventy-two hours later if no dispute
// is raised. The window is 72 rather than 48 because a Friday-evening delivery would otherwise
// expire on a Sunday, when the customer it exists for is least able to use it — and since Shipper
// holds no money in the MVP, Completed carries no financial consequence, so the longer window
// costs the provider nothing.
//
// # Why this is a jobs sweep and not a delivery one
//
// Nothing here reads a milestone, a proof or an assignment, and this file is in internal/jobs
// rather than internal/delivery for a reason that is a reading of Docs/02 rather than a
// convenience.
//
// "A Delivered job with no dispute" is exactly "a job still in Delivered". Disputed is a status in
// its own right (Docs/02 §1), the transition table has "Awarded through Delivered -> Disputed",
// and §3 says a dispute freezes automatic completion until an administrator resolves it. So a job
// with a dispute has already left Delivered, and the sweep needs no second opinion about whether
// one exists: the status column is the answer. Docs/02 §2 also has no path back into Delivered, so
// the status is reached once and left once.
//
// What remains is a status, a deadline and a transition, all of which are this domain.
//
// # X-6, and what it changed here
//
// Whether a job delivered through the proof-exception path (Docs/01 §4.4, SHIP-116) may
// auto-complete was open for eight waves and was decided on 14 August 2026: it may, on this
// ordinary rule. The reasoning is in Docs/02 §6.1 and the short form is that SHIP-117 queues every
// exception-completed job for moderation, so human review happens either way — blocking
// auto-completion would add no review and would only strand the job in Delivered when the customer
// never acts, which is the single outcome this window exists to prevent.
//
// The consequence for this file is that there is nothing to exclude. The claim below asks about
// status and time and never about evidence, which is what the decision permits and also what makes
// it impossible to get subtly wrong: there is no proof lookup to forget to write and no join to
// leave off.
//
// # Where each piece lives, and it is the same division as expiry.go
//
//   - 000401 records when a job entered Delivered, on the platform clock, in a table that cannot
//     be edited afterwards;
//   - 000408 indexes the jobs currently Delivered, so the claim reads the deliveries in flight
//     rather than every job the platform has ever had;
//   - this file says which jobs are due and what happens to one;
//   - cmd/worker/tasks_jobs.go runs the sweep on a ticker and claims the rows, because
//     cmd/worker.ClaimIDs refuses a claim query that does not say FOR UPDATE SKIP LOCKED.

// AutoCompleteWindow is how long a Delivered job waits for its customer (Docs/02 §6.1).
//
// In Go rather than in the claim SQL, exactly as [ExpiryWarning] is: the claim takes the instant
// to judge against as a parameter, so seventy-two hours is written once, here, where a test can
// move it instead of waiting three days.
//
// A constant rather than configuration, on the same reasoning cmd/worker gives for the sweep
// intervals. This is a lifecycle rule from Docs/02 §6.1 with a document and an argument behind it,
// not an operational limit of the kind Docs/06 §5.3 requires to be changeable without a deploy.
// Docs/02 §6.1 expects it to shorten once payment flows exist and providers have a stake in being
// marked done; that is a document change and a deploy, which is the right weight for it.
const AutoCompleteWindow = 72 * time.Hour

// AutoCompleteReason is recorded against every auto-completion.
//
// A fixed string rather than a formatted one, for the reason [ExpiryReason] gives: it is read by
// support and shown in the customer status timeline (SHIP-77), and a sentence that varied by job
// would be one nobody could search for. When it happened is in job_status_history and who did it
// is the platform.
const AutoCompleteReason = "The delivery was not disputed within 72 hours (Docs/02 §6.1)."

// AutoCompleteBatch is how many due jobs one pass claims.
//
// The same hundred [ExpiryBatch] uses and for the same reason — a pass is one transaction, and a
// transaction that claimed the whole backlog after an outage would hold every one of those rows,
// and their locks, for as long as the backlog took. It is a separate constant rather than a reuse
// because the two sweeps are separate rules that happen to agree on a number today.
const AutoCompleteBatch = 100

// AutoCompleteClaim selects the Delivered jobs whose window has passed, and locks them.
//
// $1 is the instant to judge against and $2 is the batch size. A parameter rather than now() so
// that the caller clock decides (Docs/10 §6.3) — the same arrangement [ExpiryClaim] uses, and here
// it is what makes a seventy-two hour rule testable in milliseconds.
//
// # The deadline is derived rather than stored, and that is deliberate
//
// There is no jobs.delivered_at. 000401 already records when the job entered Delivered, as
// server_recorded_at on the transition, and 000408 says why a second copy of that fact was
// refused. The lateral aggregate below is the read of it.
//
// max() rather than a plain join, and the difference is not cosmetic. A join on
// to_status = 'Delivered' would return one row per matching history row, so a job that had somehow
// entered Delivered twice would be claimed twice and the second transition in the pass would fail
// on a job already Completed — failing the whole pass and rolling back the first. An aggregate
// returns exactly one row per job whatever the history holds. Docs/02 §2 says there is only ever
// one such row; this is what makes the sweep not depend on that being true.
//
// A NULL from the aggregate — a job in Delivered with no history row saying it got there — fails
// the comparison and is left alone. That state cannot be produced through 000402's guard, and
// silently completing a job whose history does not explain itself is the wrong answer to finding
// one that was.
//
// # FOR UPDATE OF j, not FOR UPDATE
//
// The lock is on the job and only on the job. Bare FOR UPDATE here would ask to lock whatever else
// the planner has in the row, and job_status_history is append-only by trigger (000401) — a lock
// taken on evidence is a lock taken on something no statement in the platform will ever update.
// cmd/worker.ClaimIDs still recognises this as a claim: it reads the query for the literal
// clauses, and both are here.
//
// ORDER BY the delivery instant drains the longest-waiting first, which matters after an outage
// for the reason [ExpiryClaim] gives: the customers who have been waiting longest for their job to
// close stop waiting first.
const AutoCompleteClaim = `
	SELECT j.id
	FROM jobs j
	JOIN LATERAL (
		SELECT max(h.server_recorded_at) AS delivered_at
		FROM job_status_history h
		WHERE h.job_id = j.id
		  AND h.to_status = 'Delivered'
	) d ON true
	WHERE j.status = 'Delivered'
	  AND d.delivered_at <= $1
	ORDER BY d.delivered_at
	FOR UPDATE OF j SKIP LOCKED
	LIMIT $2`

// AutoComplete closes one claimed delivery, as the platform (SHIP-119).
//
// Docs/02 §2 has Delivered -> Completed, described as "customer confirms, or 72 hours pass with no
// dispute". This is the second half of that row; the first is the customer endpoint and is not
// this ticket.
//
// Nothing here decides that the move is legal. [Service.Transition] asks [Permitted], writes the
// job_status_history row 000402 requires, and emits job.status_changed inside the caller
// transaction — so an auto-completion is indistinguishable in the record from a customer
// confirmation except in its actor and its reason, which is precisely the distinction support
// needs and precisely the one a consumer of the event can read.
//
// No new event type. job.status_changed carries `from`, `to`, the actor and the reason, which is
// everything a notification needs to say "your job is closed"; a job.auto_completed would be a
// second event describing the same transition, and Docs/10 §6.1 has the argument for one event
// with a from and a to rather than one per transition.
//
// # The caller already holds the lock
//
// r is the worker transaction and the id came from [AutoCompleteClaim] running in it under
// FOR UPDATE SKIP LOCKED. Transition locks the row again, which costs one statement against a lock
// this transaction already holds and buys the guarantee that the status it acts on is the status
// the job has now. A second worker sweeping at the same instant was handed different rows.
//
// # A job that was disputed between the claim and the transition is not an error
//
// It cannot happen while the claim holds the lock, and a job disputed in the moment before is
// simply not selected — its status is no longer Delivered. There is deliberately no re-check, for
// the reason [Service.Expire] gives: it would be a second copy of the claim predicate, and
// Transition refuses an impossible move on its own.
func (s *Service) AutoComplete(ctx context.Context, r db.Runner, jobID uuid.UUID) (Job, error) {
	completed, err := s.Transition(ctx, r, Move{
		JobID:  jobID,
		To:     StatusCompleted,
		Actor:  System(),
		Reason: AutoCompleteReason,
	})
	if err != nil {
		return Job{}, fmt.Errorf("jobs: auto-completing %s: %w", jobID, err)
	}
	return completed, nil
}
