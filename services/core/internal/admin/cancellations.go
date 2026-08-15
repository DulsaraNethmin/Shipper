// SHIP-158: the post-award cancellation queue.
//
// Docs/04 §5's **fifth** queue — "cancellations after award" — and it is a different endpoint from
// the fourth for exactly the reason the fourth is one endpoint with four grounds: the document
// numbers its queues, and a queue is a screen somebody opens. A cancellation after award is not a
// delivery exception; nobody is late, and nothing is unrecorded. Somebody withdrew from a commitment,
// and the question it raises is about the *provider* rather than about the delivery.
//
// # Why "after award" is a fact about the history rather than about the job
//
// A cancelled job's row says `Cancelled` and nothing else. Whether it was cancelled before anybody
// committed to it — an ordinary customer changing their mind, or SHIP-68's expiry sweep — or after a
// provider had undertaken to carry it is a fact about **which status it left**, and that lives only
// in `job_status_history`.
//
// So the queue reads the transition rather than the job, and Docs/02 §2 is what makes the status set
// meaningful: `Open / Negotiating → Cancelled` is the ordinary route out and is deliberately not
// here, while every transition from `Awarded` onwards is a commitment being unwound. Docs/02 §6.2
// makes ending a job after award a support case, which is precisely what this queue is a list of.
//
// **There is no flag and no table**, which is the position `admin.ExceptionQueue` argues at length
// for its own queue and every word of it applies: a flag would have to be written where the
// cancellation happens — `jobs`' guarded transition, through a port that domain would have to declare
// — and it would be a second source of truth a repair script could put out of step. The rows are the
// queue.
//
// # "With the provider's history" is the clause that makes this worth building
//
// The *Done when* is "cancellations **are listed with the provider's history**", and the second half
// is the one a queue of job identifiers would not meet. One cancellation is an event; the question a
// moderator has is whether it is a pattern, and answering it by opening each provider's account in
// turn is the work this screen exists to remove.
//
// So an entry carries two counts about the provider who was carrying this job: how many post-award
// cancellations they have in total, and how many jobs they have completed. **The pair is deliberate
// and the ratio is the point** — three cancellations against four hundred deliveries is a different
// provider from three against five, and a single number cannot tell them apart. Neither figure is a
// judgement: this domain reports and Docs/04 §6's process is where somebody decides.
//
// The blank line below keeps this a file note rather than a second package comment.

package admin

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
)

// Cancellations serves Docs/04 §5's fifth queue (SHIP-158).
//
// A type of its own beside [Moderation] rather than a second method on it, on the same reasoning
// [Moderation] gives for not being a fourth collaborator on [Service]: they are different screens
// with different sources, and folding them together would mean every future queue widening one
// constructor. They are both paged reads and that is the extent of what they share.
type Cancellations struct {
	queue CancellationQueue
	pool  *pgxpool.Pool
}

// NewCancellations builds the queue service.
//
// The pool may be nil — the process starts with an unreachable database on purpose — and the port
// may not, for the reason [NewModeration] gives: a nil port is a queue that answers "nothing to
// review" to every request, which is indistinguishable from a quiet week and silent for exactly as
// long as nobody checks.
func NewCancellations(queue CancellationQueue, pool *pgxpool.Pool) (*Cancellations, error) {
	if queue == nil {
		return nil, errors.New("admin: the cancellation queue needs a source; without one it " +
			"would report an empty queue, which reads exactly like a fortnight in which " +
			"nobody walked away from a job")
	}
	return &Cancellations{queue: queue, pool: pool}, nil
}

// AfterAward returns one page of jobs cancelled after a provider had committed to them.
//
// A read, and it opens no transaction: nothing here writes, and a single statement is consistent
// with itself. Docs/10 §3.2 puts a transaction with the domain that owns an invariant, and a queue
// owns none.
func (c *Cancellations) AfterAward(ctx context.Context, q QueueQuery) ([]CancellationEntry, error) {
	if c.pool == nil {
		return nil, ErrAdminUnavailable
	}

	entries, err := c.queue.CancelledAfterAward(ctx, c.pool, q)
	if err != nil {
		return nil, fmt.Errorf("admin: reading the post-award cancellation queue: %w", err)
	}
	return entries, nil
}
