package delivery

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/DulsaraNethmin/Shipper/services/core/internal/db"
)

// The twenty-four-hour rung of Docs/02 §3.1's escalation ladder (SHIP-128).
//
// The ladder has three rungs and this is the last one:
//
//	Immediately   the app shows how many updates are pending          SHIP-126
//	4 hours       the provider is nudged to find signal               SHIP-127
//	24 hours      operations alert; the job enters the delivery       SHIP-128
//	              exception queue (Docs/04 §5)
//
// The first two are the handset's, and they measure `enqueued_at` on the device. This one is the
// platform's, and it cannot measure that column because it has never seen it.
//
// # What the platform can measure, and 000601 named it before this ticket existed
//
// `milestones` records the actor's clock and the platform's separately (SHIP-110), and the
// difference between them **is** how long the update was unsynced. 000601 says so in as many words:
// `server_recorded_at` is "what makes an unsynced-milestone threshold (Docs/02 §6.5) measurable".
// So the fact this ticket owns is `server_recorded_at - actor_recorded_at`, and it needs no column,
// no flag and no second table — which is the same reading `admin.ExceptionQueue` (SHIP-117) takes of
// its own queue, in this same domain, for the same reason: **the rows are the queue.**
//
// # What that deliberately cannot see, stated rather than narrowed away
//
// An update still sitting on a handset has not arrived, so the platform cannot know it exists. This
// rung therefore fires when a long-unsynced update **lands**, not at the moment it turns twenty-four
// hours old, and a delivery whose driver never reconnects is invisible here.
//
// That gap is real and it is not this ticket's to close. A job that has *stopped moving* is a
// different fact with a different measurement — the last thing that happened to it rather than the
// gap inside one update — and it is "delayed delivery" in Docs/04 §5, which SHIP-177's alerting
// owns. Conflating the two would produce a queue that could not tell "the record is stale" from "the
// delivery is stuck", which are different problems with different responses.

// UnsyncedAlertThreshold is how long an update may be unsynced before the job is treated as at risk
// (Docs/02 §3.1).
//
// A constant rather than configuration, on exactly the reasoning jobs.ExpiryWarning gives: this is a
// lifecycle rule from a document, not an operational limit of the kind Docs/06 §5.3 requires to be
// changeable without a deploy. Docs/02 §3.1's table is the authority, and a value an operator could
// move would be a second one.
//
// **The four-hour rung is the opposite case and is deliberately not here.** It fires on a handset
// that Docs/07 says has no over-the-air update path, so it is exactly the kind of number CLAUDE.md
// puts server-side and hands to the client to cache — which is SHIP-167a's endpoint, not this
// constant.
const UnsyncedAlertThreshold = 24 * time.Hour

// UnsyncedFor is how long this update was in the actor's hands before the platform saw it.
//
// Zero when the actor's clock is at or after the platform's, which is not an error and is not
// corrected: a handset with a clock running fast is reporting honestly about a delivery it made, and
// 000601 refuses to bound `actor_recorded_at` for precisely that reason — "an implausible time is
// evidence, not an error". Clamping here rather than returning a negative duration keeps a fast
// clock out of the queue instead of putting it in with a nonsense figure.
func (rec Record) UnsyncedFor() time.Duration {
	if !rec.ServerRecordedAt.After(rec.ActorRecordedAt) {
		return 0
	}
	return rec.ServerRecordedAt.Sub(rec.ActorRecordedAt)
}

// Unsynced reports whether this update crossed Docs/02 §3.1's twenty-four-hour rung.
func (rec Record) Unsynced() bool {
	return rec.UnsyncedFor() >= UnsyncedAlertThreshold
}

// UnsyncedEntry is one job in the delivery exception queue on the unsynced-milestone ground
// (Docs/04 §5, SHIP-157).
//
// It carries the milestone rather than only the job, because a moderator's first question is what
// the driver was recording and how far behind the record ran — and both are already on the row, so
// answering them costs the join this query is already doing.
//
// There is no budget here and there never will be (Docs/01 §4.3), and no proof object key: an object
// key is reached only through a short-lived pre-signed URL (Docs/04 §3.1), and a queue listing is not
// the place one is issued.
type UnsyncedEntry struct {
	MilestoneID uuid.UUID
	JobID       uuid.UUID

	Milestone Milestone

	// Actor is the kind of actor, not who they are. A queue answers "was this the provider or
	// their driver", which is what Docs/02 §1 says support needs; naming the account is a
	// different disclosure and a different screen.
	Actor ActorType

	// UnsyncedFor is the gap the row crossed, computed in SQL from the two clocks so that a
	// caller cannot reach a different answer from the one the predicate selected on.
	UnsyncedFor time.Duration

	ActorRecordedAt  time.Time
	ServerRecordedAt time.Time

	// JobStatus is where the job stands now, as the stored string. **Not a jobs.Status** — this
	// domain may not name one (Docs/06 §4.1) — and it is here because the queue's whole purpose
	// is triage: a stale record on a job that has since completed is a different morning's work
	// from one on a job still in transit.
	JobStatus string
}

// UnsyncedBatch is how many entries one read returns when the caller names no limit.
const UnsyncedBatch = 100

// UnsyncedMilestones is the delivery exception queue on this ground: jobs carrying an update that
// reached the platform more than [UnsyncedAlertThreshold] after it was recorded (SHIP-128).
//
// Oldest arrival first, so the longest-standing problem is at the top after an outage — the same
// ordering `jobs.ExpiryClaim` takes and for the same reason.
//
// # It is a query and it writes nothing, which is the whole design
//
// No flag column marks a row as belonging to the queue. `admin.ExceptionQueue` argues this at length
// for its own queue and every word of it applies here: a flag is a second source of truth that a
// repair script or a rolled-back transaction can put out of step with the rows, and it "would have to
// be written where the exception is recorded, which is `internal/delivery`'s transaction, through a
// port it would have to declare — a cross-domain write for a fact that is already in the row". Here
// the fact is *two columns subtracted*, which is even less worth copying.
//
// # Ordered and filtered on the platform's clock, never on the actor's
//
// The gap uses both, but the ordering and the window use `server_recorded_at` alone. The actor's
// clock is a handset's, it syncs late and it can be wrong, so a queue ordered by it could be
// reordered by a device — which is the same call `admin.ExceptionQueue` makes about `p.created_at`,
// and Docs/04 §8's support targets are measured against the platform's clock in any case.
//
// # No cursor, deliberately, and SHIP-157 is the ticket that adds one
//
// A limit and no `after`. The screen this feeds does not exist, and a cursor shape chosen without a
// consumer is a guess that the first real caller has to live with — `admin.QueueQuery`'s cursor is
// `(created_at, id)` because somebody had a page to render. Whoever builds SHIP-157 knows what their
// screen pages by; until then a bounded read is the honest surface, and widening it is additive.
func (s *Service) UnsyncedMilestones(ctx context.Context, r db.Runner, limit int) ([]UnsyncedEntry, error) {
	if limit <= 0 {
		limit = UnsyncedBatch
	}

	entries, err := s.store.unsyncedMilestones(ctx, r, UnsyncedAlertThreshold, limit)
	if err != nil {
		return nil, fmt.Errorf("delivery: reading the unsynced-milestone queue: %w", err)
	}
	return entries, nil
}
