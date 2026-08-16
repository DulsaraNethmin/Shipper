// SHIP-153: the reviewer's read of this domain's records.
//
// Docs/04 §5's first moderation queue — "new or changed provider verification submissions" — and
// SHIP-153's *Done when*: "pending provider verifications listed oldest first".
//
// # This is in `internal/profiles` and the endpoint is in `internal/admin`, which is not a
// contradiction
//
// `provider_verifications` is this domain's table and no other domain names it. `internal/admin`
// declares what it needs of it in `admin/ports.go` and cmd/api supplies an adapter over this
// method, which is the arrangement `disputeLifecycle` already uses for the job lifecycle: the
// statement stays with the domain that owns the rows, and the composition root is the only place
// the two packages meet.
//
// **It is deliberately not a statement written in cmd/api.** That is where a query spanning *two
// other domains'* tables belongs — `jobPartiesLookup` and `exceptionQueueLookup` are both that
// shape — and this one spans one domain's table plus `users`, which is shared and which this
// package already reads (see [postgresStore.isProvider]). A copy of it in cmd/api would be SQL over
// a table `internal/profiles` owns, sitting where nobody maintaining this domain would look for it.
//
// # Why the queue carries the provider's contact details
//
// A reviewer working Docs/04 §3 is looking at a person's licence and registration and checking that
// the name matches the account. A queue of bare identifiers would make that two requests per row.
// Nothing commercial is on the shape and nothing can be: [QueueEntry] has no field for a budget, a
// bid or a job, so no change to the mapping can acquire one.
//
// The blank line below keeps this a file note rather than a second package comment.

package profiles

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/DulsaraNethmin/Shipper/services/core/internal/db"
)

// QueueQuery is one page of the verification review queue.
//
// Cursor paged rather than offset paged, per Docs/10 §4.5. Providers register while somebody is
// working through the queue, and an offset would show the same provider twice or skip one — which
// matters more here than on most feeds, because a skipped row is a person waiting for a decision
// that nobody is going to take.
type QueueQuery struct {
	// State narrows to one of [States]. **Required rather than optional**, and the caller's
	// choice rather than a default hidden here: SHIP-153 asks for the Pending ones, Docs/04 §5's
	// first queue is "new or changed" submissions, and a query with no state would be a list of
	// every provider on the platform wearing a queue's name.
	State State

	// Limit is how many entries to return. Bounded by internal/pagination before it gets here.
	Limit int

	// After is where the previous page stopped. The zero value is the first page.
	After QueueCursor
}

// QueueCursor is the position of the last entry a caller saw.
//
// Two fields, because `created_at` is not unique — two providers registering in the same
// millisecond would make a single-column cursor either skip one or repeat it. The provider
// identifier breaks the tie and is what makes the ordering total.
type QueueCursor struct {
	SubmittedAt time.Time
	ProviderID  uuid.UUID
}

// Zero reports whether this is the first page.
func (c QueueCursor) Zero() bool { return c.ProviderID == uuid.Nil && c.SubmittedAt.IsZero() }

// QueueEntry is one provider waiting for, or carrying, a verification decision.
//
// **No budget, no job and no bid**, which is structural rather than careful: there is nowhere on
// this struct to put one, and the statement behind it joins nothing commercial.
//
// **No decision reason and no decided-at.** A queue entry is what somebody triaging needs in order
// to decide whom to open next, and the reason behind a *previous* decision is a fact about a review
// that has already happened. `GET /v1/provider/verification` carries it to the person it concerns
// and SHIP-155's document viewer is where a reviewer opens the case.
type QueueEntry struct {
	ProviderID uuid.UUID

	// Name is what the account holder is called (SHIP-30a). **Empty for an account created
	// before `000006`**, which is a real state rather than a defect: a name cannot be
	// backfilled, so an older provider has none and the console shows it has none.
	Name string

	Email string
	Phone string

	// State is where this provider stands — one of [States], and always the state that was
	// asked for.
	State State

	// SubmittedAt is when the record was created, which is what "oldest first" orders by.
	//
	// **Deliberately not the newest decision's clock.** Oldest-first in a review queue means
	// whoever has been waiting longest, and a provider whose state was corrected twice has not
	// gone to the back of the line by being corrected — `000200` says so in the column's own
	// comment, and this is the read that depends on it.
	SubmittedAt time.Time
}

// AwaitingReview is one page of providers in a given state, oldest first (SHIP-153).
//
// It is a read and it opens no transaction: nothing here writes and a single statement is already
// consistent with itself. Docs/10 §3.2 puts a transaction with whoever owns an invariant, and a
// queue owns none.
//
// An unrecognised state is refused rather than answered with an empty page, on the reasoning
// `admin.Users.Search` records for its standing filter: an empty page reads exactly like "nobody is
// waiting" to somebody who mistyped, which on a review queue is the answer that costs a person a
// week.
func (s *Service) AwaitingReview(ctx context.Context, r db.Runner, q QueueQuery) ([]QueueEntry, error) {
	if !q.State.Valid() {
		return nil, fmt.Errorf("profiles: %q: %w", q.State, ErrStateUnrecognised)
	}
	return s.store.awaitingReview(ctx, r, q)
}
