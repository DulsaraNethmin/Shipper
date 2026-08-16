// SHIP-153: the verification review queue.
//
// Docs/04 §5's **first** moderation queue — "new or changed provider verification submissions" — and
// Docs/01 §4.6's second administrative capability, "review provider verification status".
//
// # This is the read half of the shortest path back to a working marketplace
//
// SHIP-81a backfilled every existing provider `Pending`, deliberately — nobody had reviewed anyone's
// documents, and Docs/04 §1 requires that table be an evidence trail rather than a convenience. The
// consequence was that **no provider on the platform was eligible to bid and nothing could change
// that through any API**: `profiles.Service.Decide` was exported and unrouted, because deciding
// somebody's standing is an administrator's act on the administrator credential. This is the screen
// that shows who is waiting; SHIP-154 is the act that moves them.
//
// # Why a service of its own rather than a method on [Moderation] or [Enforcement]
//
// [Moderation] is Docs/04 §5's *fourth* queue and holds the exception source and the unsynced
// threshold; [Enforcement] is §6 step 4's outcomes against a job or an account. This is a different
// queue over a different domain's tables, answering a different question: whether somebody may trade
// at all, which Docs/04 §1 calls "an eligibility decision, not a guarantee of delivery quality".
//
// **It takes the auditor even though a queue writes nothing**, because SHIP-154's decision hangs
// here: the queue and the decision are one screen — a reviewer reads a row and acts on it in the
// same sitting — and splitting them would be two services whose constructors take the same port.
//
// The blank line below keeps this a file note rather than a second package comment.

package admin

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"
)

// Verifications serves Docs/04 §5's first queue (SHIP-153).
type Verifications struct {
	records ProviderVerifications
	auditor *Auditor

	// states is Docs/04 §4's five outcomes, supplied by cmd/api from `profiles.States`.
	//
	// **Not a constant in this package**, which would be a hand-written copy of a list that is
	// already held to `ck_provider_verifications_state` by a test in both directions — Docs/10
	// §3.4's failure, where two lists agree by comment until somebody adds to one.
	// [JobConsole.Statuses] is the same arrangement for the job statuses and the same reasoning.
	states []string

	pool *pgxpool.Pool
}

// NewVerifications builds the service.
//
// The pool may be nil, which every constructor in this package accepts: the process starts with an
// unreachable database on purpose, and the endpoints answer [ErrAdminUnavailable] until it returns.
//
// The port, the auditor and the state list may not.
//
//   - A nil port would make the queue answer "nobody is waiting" to every request, which is the
//     worst available failure for a review queue: indistinguishable from a quiet week, and silent
//     for exactly as long as nobody checks. [NewModeration] refuses one for the same reason.
//   - A nil auditor is refused here rather than left to [Auditor.Record]'s own check, on
//     [NewEnforcement]'s argument: that method does refuse a nil receiver, but it refuses at the
//     moment somebody exercises a privileged action, which is precisely when a service must not be
//     discovering its own wiring.
//   - An empty state list would refuse **every** decision as unrecognised, which reads exactly like
//     a broken console rather than a mis-wire. Refused at startup, where the cause is visible.
func NewVerifications(
	records ProviderVerifications,
	states []string,
	auditor *Auditor,
	pool *pgxpool.Pool,
) (*Verifications, error) {

	if records == nil {
		return nil, errors.New("admin: the verification console needs a source of provider " +
			"verification records; without one the queue reports nobody waiting, which reads " +
			"exactly like a quiet week")
	}
	if auditor == nil {
		return nil, errors.New("admin: the verification console needs the audit writer; deciding " +
			"who may trade is a privileged action, and one that leaves no record cannot be " +
			"reconstructed afterwards")
	}
	if len(states) == 0 {
		return nil, errors.New("admin: the verification console needs Docs/04 §4's outcomes; with " +
			"none, every decision is refused as unrecognised")
	}

	held := make([]string, len(states))
	copy(held, states)

	return &Verifications{records: records, auditor: auditor, states: held, pool: pool}, nil
}

// States is Docs/04 §4's outcomes as this service was told them, sorted, as a fresh slice.
//
// A copy rather than the stored slice, for [Role.Permissions]' reason: a caller that appended to
// what it was handed would be widening the accepted set for the whole process.
//
// It exists so the handler can name the choices in a validation message without holding a second
// copy of the list — the same seam [JobConsole.Statuses] opened.
func (v *Verifications) States() []string {
	out := make([]string, len(v.states))
	copy(out, v.states)
	slices.Sort(out)
	return out
}

// KnowsState reports whether state is one of the outcomes cmd/api supplied.
//
// Exported for [JobConsole.KnowsStatus]'s reason: the handler validates a query parameter and a
// request body against it, and the alternative is a copy of Docs/04 §4's five outcomes in the
// transport layer.
func (v *Verifications) KnowsState(state string) bool { return slices.Contains(v.states, state) }

// AwaitingReview returns one page of the queue, oldest first (SHIP-153).
//
// It is a read and it opens no transaction: nothing here writes and a single statement is already
// consistent with itself. Docs/10 §3.2 puts a transaction with whoever owns an invariant, and a queue
// owns none.
//
// An unrecognised state is refused rather than ignored, on the reasoning [Users.Search] records for
// its standing filter — and it is sharpest here. An ignored filter answers an empty page, an empty
// page is what "nobody is waiting" looks like, and a review queue that quietly reads empty is one
// nobody opens again.
func (v *Verifications) AwaitingReview(ctx context.Context, q VerificationQuery) ([]VerificationEntry, error) {
	q.State = strings.TrimSpace(q.State)
	if !v.KnowsState(q.State) {
		return nil, fmt.Errorf("%w: %q", ErrVerificationStateUnrecognised, q.State)
	}
	if v.pool == nil {
		return nil, ErrAdminUnavailable
	}

	entries, err := v.records.VerificationsAwaitingReview(ctx, v.pool, q)
	if err != nil {
		return nil, fmt.Errorf("admin: reading the %s verification queue: %w", q.State, err)
	}
	return entries, nil
}
