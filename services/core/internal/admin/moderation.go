// SHIP-117 and SHIP-157: the delivery-exception moderation queue.
//
// Docs/04 §5's fourth queue — "delivery exceptions: overdue pickup, delayed delivery, **failed proof
// of delivery**" — which SHIP-117 built the third of. SHIP-157 is the other two plus the unsynced
// milestones its own *Done when* names, and it is where the queue became one screen rather than one
// endpoint, exactly as the sentence this paragraph replaced predicted.
//
// # SHIP-157 widened this endpoint rather than adding three
//
// The alternative was `/moderation/overdue-pickups`, `/delayed-deliveries` and `/unsynced`, and it
// was rejected on the document: **Docs/04 §5 numbers seven queues and these four grounds are one of
// them.** Four endpoints would be four cursors and four pages, and the question a moderator actually
// asks — what is going wrong with deliveries, oldest first — would become a merge of four sorted
// streams performed by whoever writes the console. `UNION ALL` and one ordering do that once,
// server-side, where the cursor can stay total.
//
// The decision cost one thing worth naming: the entry shape is now a **union** shape, where three
// fields are empty on some grounds. [ExceptionEntry.Ground] is what makes that readable — an entry
// says why it is here rather than leaving a console to infer it from which fields are blank.
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
// It is not a work queue: there is no claim, no assignment and no "done", because Docs/04 §6's
// process ends in a recorded decision (SHIP-150, SHIP-154, SHIP-161) rather than in a queue entry
// being ticked off. An entry stays for as long as the fact that produced it is true — which for the
// two evidence grounds is for ever, and for the two window grounds is until the job moves on. That
// asymmetry is the queue reporting the rows rather than a bug: a delivery recorded with a reason
// happened and cannot un-happen, and a job that was late for its pickup window and has since been
// picked up is no longer late.
//
// And it takes **no position on X-6** — see [ExceptionQueue].
//
// The blank line below keeps this a file note rather than a second package comment.

package admin

import (
	"context"
	"errors"
	"fmt"
	"time"

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

	// unsyncedThreshold is Docs/02 §3.1's twenty-four-hour rung, supplied by cmd/api from
	// `delivery.UnsyncedAlertThreshold` rather than declared here.
	//
	// **A constant in this package would be a second authority for one number**, and the two
	// would agree by comment until somebody moved one — which is Docs/10 §3.4's failure applied
	// to a duration instead of a status list. The composition root reads it from the domain that
	// owns it, once.
	unsyncedThreshold time.Duration
}

// NewModeration builds the queue service.
//
// The pool may be nil — the process starts with an unreachable database on purpose — and the port
// may not. A nil port is a queue that answers "nothing to review" to every request, which is the
// worst available failure for a moderation queue: indistinguishable from a quiet week, and silent
// for exactly as long as nobody checks.
func NewModeration(
	exceptions ExceptionQueue,
	unsyncedThreshold time.Duration,
	pool *pgxpool.Pool,
) (*Moderation, error) {
	if exceptions == nil {
		return nil, errors.New("admin: the moderation queue needs an exception source; without " +
			"one it would report an empty queue, which reads exactly like a quiet week")
	}

	// A zero or negative threshold would put **every** milestone ever recorded on the unsynced
	// ground, because every gap is at least zero — a queue of the whole table, which is the
	// failure this constructor's other check exists to prevent in the opposite direction. It is
	// refused at wiring time rather than clamped, because a clamp would hide a mis-wire behind a
	// queue that looks plausible.
	if unsyncedThreshold <= 0 {
		return nil, errors.New("admin: the moderation queue needs Docs/02 §3.1's unsynced " +
			"threshold; at or below zero every milestone ever recorded is an exception")
	}
	return &Moderation{
		exceptions:        exceptions,
		pool:              pool,
		unsyncedThreshold: unsyncedThreshold,
	}, nil
}

// Exceptions returns one page of the delivery-exception queue, oldest first, across every ground.
//
// It is a read and it opens no transaction: nothing here writes, and a single statement is already
// consistent with itself. Docs/10 §3.2 puts a transaction with the domain that owns an invariant,
// and a queue owns none. **That the four grounds are one statement rather than four is what makes
// that true after SHIP-157** — four separate reads would need a transaction to agree with each
// other about the moment they describe.
//
// An unrecognised ground filter is refused rather than ignored, on the reasoning `Users.Search`
// records for the standing filter: an ignored filter answers with everything, which reads exactly
// like "everything is on this ground" to somebody who mistyped one.
func (m *Moderation) Exceptions(ctx context.Context, q QueueQuery) ([]ExceptionEntry, error) {
	if q.Ground != "" && !q.Ground.Valid() {
		return nil, fmt.Errorf("%w: %q", ErrExceptionGroundUnrecognised, q.Ground)
	}
	if m.pool == nil {
		return nil, ErrAdminUnavailable
	}

	entries, err := m.exceptions.ExceptionsAwaitingReview(ctx, m.pool, q, m.unsyncedThreshold)
	if err != nil {
		return nil, fmt.Errorf("admin: reading the delivery-exception queue: %w", err)
	}
	return entries, nil
}
