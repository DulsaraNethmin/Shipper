package jobs

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/DulsaraNethmin/Shipper/services/core/internal/db"
	"github.com/DulsaraNethmin/Shipper/services/core/internal/events"
)

// Job expiry (SHIP-68) and the warning that precedes it (SHIP-69).
//
// Docs/02 §6.3: an Open job leaves Open at whichever comes first — fourteen days after
// publication, or the moment its own pickup date passes — and "the customer is warned 48 hours
// before expiry and can extend in one action". The deadline and the warning are one mechanism read
// twice, so they are in one file; the extension is the customer's answer to the warning and is in
// extend.go, because it is an endpoint rather than a sweep.
//
// Four pieces make the deadline true, and they are deliberately in four places:
//
//   - 000406 sets the deadline as the job becomes Open, so no route into Open can forget it and
//     the "earlier of" is computed once, in SQL, at the moment both inputs are known;
//   - 000407 marks a job warned and clears the mark whenever the deadline moves, so "once per
//     deadline" is attached to the change rather than to whoever made it;
//   - this file says which jobs are due, which are nearly due, and what happens to one;
//   - cmd/worker/tasks_jobs.go runs both sweeps on a ticker and claims the rows.
//
// # Why the claim queries live here and the loop does not
//
// Which jobs are due is this domain's rule — it names this domain's table, this domain's status
// and this domain's deadline column, and a copy of it in cmd/worker would be a second answer to a
// question 000406 already decided. How work is claimed is the worker's: cmd/worker.ClaimIDs
// refuses a query without FOR UPDATE SKIP LOCKED, because a claim missing either produces no
// error and no test failure, only two workers doing the same work or queueing behind each other.
//
// So the queries are declared here and executed there, which lets each half be exactly what it is.

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

// --- the warning, forty-eight hours ahead (SHIP-69) ---------------------------------------------

// ExpiryWarning is how far ahead of the deadline the owner is told (Docs/02 §6.3).
//
// In Go rather than in the claim's SQL, and that is the one decision worth stating. The claim takes
// the horizon as a parameter, so this constant is the only place forty-eight hours is written — the
// same arrangement that lets [ExpiryClaim] be judged against an injected clock rather than against
// the database's `now()`, and for the same reason: a test moves the horizon instead of waiting.
//
// It is a constant rather than configuration on the reasoning cmd/worker gives for the sweep
// interval. This is a lifecycle rule from Docs/02 §6.3 with a document behind it, not an
// operational limit of the kind Docs/06 §5.3 requires to be changeable without a deploy.
const ExpiryWarning = 48 * time.Hour

// EventExpiryWarned is emitted once per job per deadline, forty-eight hours ahead of it.
//
// An event of its own rather than a variant of [EventStatusChanged], because nothing has changed
// status: the job is Open before the warning and Open after it. Reusing the status event would mean
// emitting `Open -> Open`, which Docs/02 §2 has no row for and [Service.Transition] refuses on
// purpose. Reusing its *payload* without a transition would be worse — a consumer counting status
// changes would count one that never happened.
//
// It is `job.expiry_warned` rather than `job.expiring_soon` because Docs/06 §4 names events
// `<aggregate>.<past-tense>`: the event says the platform warned, which is a fact, rather than that
// the job is expiring, which is a condition a consumer can read off `expires_at`.
const EventExpiryWarned = "job.expiry_warned"

// ExpiryWarningClaim selects the Open jobs whose deadline is inside the warning window and which
// have not been told about the deadline they have now, and locks them.
//
// $1 is the instant to judge against, $2 is the horizon — [ExpiryWarning] ahead of it — and $3 is
// the batch size. Two instants rather than one plus an interval in SQL, so that the forty-eight
// hours is written once, in Go, where a test can move it.
//
// # Both ends of the window are bounded, and the lower bound is the interesting one
//
// `expires_at > $1` excludes a job whose deadline has already passed. Such a job is due for
// [Service.Expire] rather than for a warning, and both sweeps run in the same binary a few
// milliseconds apart — so without the lower bound a job that outlived its deadline between two
// passes would be warned and cancelled in the same minute. "Your job expires in two days" arriving
// alongside "your job has expired" is the sort of thing that costs a customer's trust in every
// later notification.
//
// `expiry_warned_at IS NULL` is what makes the warning happen once rather than every five minutes
// for two days. 000407 owns the other half of that: the mark is cleared whenever the deadline
// moves, so a job the customer extends is warned again against its new deadline.
//
// ORDER BY expires_at warns the most urgent first, which matters after an outage for the same
// reason [ExpiryClaim]'s ordering does — the jobs closest to dying are told first.
//
// idx_jobs_open_unwarned (000407) is a partial index on exactly this predicate.
const ExpiryWarningClaim = `
	SELECT id
	FROM jobs
	WHERE status = 'Open'
	  AND expiry_warned_at IS NULL
	  AND expires_at IS NOT NULL
	  AND expires_at > $1
	  AND expires_at <= $2
	ORDER BY expires_at
	FOR UPDATE SKIP LOCKED
	LIMIT $3`

// WarnOfExpiry tells one claimed job's owner that it is about to expire (SHIP-69).
//
// This is not a transition and deliberately does not pretend to be one. The job is Open before and
// Open after; what changes is a timestamp that exists so the warning is not repeated. So nothing
// here touches [Service.Transition], no job_status_history row is written, and 000402's guard is
// never involved — it returns early for any update that leaves the status alone.
//
// # The mark and the event commit together or not at all
//
// r is the worker's transaction. The mark is written and the event emitted inside it, in that
// order, which is the outbox contract (Docs/06 §4.0, Docs/10 §6.1): an event that commits when the
// mark does not would warn the customer again on the next pass, and a mark that commits when the
// event does not would mean they are never warned at all. Both failures are silent, and the
// transaction is what makes neither possible.
//
// # The write carries the claim's own predicate
//
// [ExpiryWarningClaim] already selected an Open, unwarned job and holds its lock, so the conditions
// on the UPDATE below can never fail for the caller that exists. They are there for the caller that
// does not exist yet — a future path that warns a job it chose some other way — and they cost one
// index probe against a row this transaction already has. That is a different thing from
// [Service.Expire], which needs no re-check because Transition refuses an impossible move on its
// own; there is no guard behind this one.
func (s *Service) WarnOfExpiry(ctx context.Context, r db.Runner, jobID uuid.UUID) (Job, error) {
	// pgx.Tx and *pgxpool.Pool both satisfy db.Runner and only one of them is a transaction.
	// Checked before anything is written, for the reason Transition checks: outside a
	// transaction the mark would commit on its own and the event might not follow.
	if _, inTx := r.(pgx.Tx); !inTx {
		return Job{}, fmt.Errorf("jobs: warning %s of expiry: %w", jobID, ErrNotInTransaction)
	}

	at := s.clock.Now().UTC()

	warned, err := s.store.markExpiryWarned(ctx, r, jobID, at)
	if err != nil {
		return Job{}, err
	}

	if err := s.emitExpiryWarning(ctx, r, warned, at); err != nil {
		return Job{}, err
	}
	return warned, nil
}

// expiryWarned is the event payload.
//
// The customer is named because the consumer's whole job is to reach them (SHIP-137), and reading
// the job back to find out who owns it would be a database round trip per notification for a fact
// that cannot change. Everything else a message needs — what the goods are, where they are going —
// is read from the job, per Docs/06 §4: the event carries what a consumer must have to act, and
// PostgreSQL remains the record of truth.
//
// There is no budget field here and there never will be. Docs/01 §4.3 keeps the customer's maximum
// private from providers, and an event travels further than any endpoint that could redact it.
type expiryWarned struct {
	JobID      string `json:"job_id"`
	CustomerID string `json:"customer_id"`

	// ExpiresAt is the deadline being warned against, and WarnedAt is when the platform said
	// so. Both, rather than one and an implied interval, because a consumer that retried after
	// an outage needs to know whether the warning it is holding is still about the deadline the
	// job has — Docs/02 §6.3's extension moves it, and 000407 re-arms this event when it does.
	ExpiresAt time.Time `json:"expires_at"`
	WarnedAt  time.Time `json:"warned_at"`
}

// emitExpiryWarning writes the domain event, inside the caller's transaction.
func (s *Service) emitExpiryWarning(ctx context.Context, r db.Runner, job Job, at time.Time) error {
	event, err := events.New("job", job.ID, EventExpiryWarned, at, expiryWarned{
		JobID:      job.ID.String(),
		CustomerID: job.CustomerID.String(),
		ExpiresAt:  job.ExpiresAt.UTC(),
		WarnedAt:   at,
	})
	if err != nil {
		return err
	}
	return s.events.Emit(ctx, r, event)
}
