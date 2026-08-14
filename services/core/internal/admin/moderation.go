// SHIP-117: the delivery-exception moderation queue.
//
// Docs/04 §5's fourth queue — "delivery exceptions: overdue pickup, delayed delivery, **failed proof
// of delivery**" — and this ticket builds the third of those three. The other two are SHIP-157's,
// which is where the queue becomes one screen rather than one endpoint.
//
// # The queue is a query, and 000604 is where that was decided
//
// SHIP-116's migration wrote it down while building the index this reads through: "whether a job is
// queued for review is a fact about the *job*, and it belongs with the queue rather than with the
// evidence. What this migration owes that ticket is a cheap answer to 'which jobs completed through
// the exception path', and idx_proofs_exception below is it."
//
// So there is no flag column and nothing to write. Two consequences, both good:
//
//   - `internal/delivery` needs no change at all. A flag would have to be written where the
//     exception is recorded, which is that domain's transaction, through a port it would have to
//     declare — a cross-domain write for a fact that is already in the row.
//   - the queue cannot drift from the evidence. A flag is a second source of truth, and a repair
//     script, a backfill or a rolled-back transaction is all it takes to put two of them out of
//     step. The rows *are* the queue.
//
// # What this is not
//
// It is not SHIP-157, which surfaces four kinds of exception with the job in context. It is not a
// work queue: there is no claim, no assignment and no "done", because Docs/04 §6's process ends in a
// recorded decision (SHIP-150, SHIP-154, SHIP-161) rather than in a queue entry being ticked off.
// And it takes **no position on X-6** — see [ExceptionQueue].
//
// The blank line below keeps this a file note rather than a second package comment.

package admin

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
)

// Moderation serves the queues of Docs/04 §5.
//
// A type of its own rather than a fourth collaborator on [Service], because they are different
// concerns with different lifetimes: [Service] is the dispute workflow, which owns a transaction
// spanning two domains, and this is a paged read. Folding them together would mean every future
// queue widening `NewService`'s signature.
type Moderation struct {
	exceptions ExceptionQueue
	pool       *pgxpool.Pool
}

// NewModeration builds the queue service.
//
// The pool may be nil — the process starts with an unreachable database on purpose — and the port
// may not. A nil port is a queue that answers "nothing to review" to every request, which is the
// worst available failure for a moderation queue: indistinguishable from a quiet week, and silent
// for exactly as long as nobody checks.
func NewModeration(exceptions ExceptionQueue, pool *pgxpool.Pool) (*Moderation, error) {
	if exceptions == nil {
		return nil, errors.New("admin: the moderation queue needs an exception source; without " +
			"one it would report an empty queue, which reads exactly like a quiet week")
	}
	return &Moderation{exceptions: exceptions, pool: pool}, nil
}

// Exceptions returns one page of deliveries evidenced by a reason rather than a photograph.
//
// It is a read and it opens no transaction: nothing here writes, and a single statement is already
// consistent with itself. Docs/10 §3.2 puts a transaction with the domain that owns an invariant,
// and a queue owns none.
func (m *Moderation) Exceptions(ctx context.Context, q QueueQuery) ([]ExceptionEntry, error) {
	if m.pool == nil {
		return nil, ErrAdminUnavailable
	}

	entries, err := m.exceptions.ExceptionsAwaitingReview(ctx, m.pool, q)
	if err != nil {
		return nil, fmt.Errorf("admin: reading the delivery-exception queue: %w", err)
	}
	return entries, nil
}
