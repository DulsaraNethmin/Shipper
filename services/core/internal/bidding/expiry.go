package bidding

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/DulsaraNethmin/Shipper/services/core/internal/db"
)

// Bid expiry (SHIP-89).
//
// Docs/01 §4.2 bounds a provider's three verbs "until it is accepted or expires", and Docs/01
// §4.5 lists "bid expiry" among the state changes that have to reach somebody. Nothing in this
// platform wrote [StatusExpired] before this file: the constant was declared, `ck_bids_status`
// accepted it, and SHIP-136 deliberately registered **no** `bid.expired` schema, because a line
// in cmd/api/events_golden.txt describing a payload no code marshals reads as coverage when it is
// not. Both halves land here together.
//
// # What an offer's "own terms" are, and why there is no column
//
// **The moment the offer commits to collecting.** A live offer names a `pickup_at`; once that
// instant has passed, awarding the offer would commit a provider to collecting in the past, which
// is exactly what [Offer.validate] refuses at placement and at revision. Expiry is that same rule
// read at a later instant rather than a second rule with a second source of truth, and migration
// 000503 argues the alternative — a `bids.expires_at` with a fixed lifetime — down: the default it
// chose would be product policy no document has decided, made by a migration.
//
// It is also the shape Docs/02 §6.3 gives the *job*: "the earlier of 14 days or the pickup date
// passing". The fourteen-day half belongs to the job's publication and has no counterpart on an
// offer; the pickup half does, and this is it.
//
// # The division between this file and cmd/worker is jobs/expiry.go's, deliberately copied
//
// **Which offers are due is this domain's rule** — it names this domain's table, this domain's
// status and this domain's timing column. **How work is claimed is the worker's**:
// cmd/worker.ClaimIDs refuses a query without `FOR UPDATE SKIP LOCKED`, because a claim missing
// either produces no error and no test failure, only two workers doing one worker's work or
// queueing behind each other. So the query is declared here and executed there.
//
// # This sweep does not reopen SHIP-15r's task-selector question
//
// The harness header names the reopening trigger: "a task that sweeps rows due by wall-clock
// alone, which is the one case fencing cannot cover". This is not that task. Every offer the API
// can create is **not due at the moment it is created** — [Offer.validate] refuses a `pickup_at`
// that is not in the future — so a verify section leaves nothing due by writing its fixtures the
// only way the endpoint permits. What it does mean is that a database kept between runs
// accumulates offers whose collection time has since passed, so an assertion about *this* run's
// expiries has to be fenced on ids rather than counted over the table. That is the rule already.

// ExpiryBatch is how many due offers one pass claims.
//
// The number [jobs.ExpiryBatch] uses, arrived at for the same reason rather than shared: a pass is
// one transaction, and a transaction that claimed every due row after a long outage would hold
// them — and their locks — for as long as the whole backlog took. A hundred at a time drains at
// the same rate over several passes, and each pass either commits or releases what it took.
const ExpiryBatch = 100

// ExpiryClaim selects the live offers whose collection time has passed, and locks them.
//
// $1 is the instant to judge against and $2 is the batch size. The instant is a parameter rather
// than `now()` so that the caller's clock decides — Docs/10 §6.3 puts every scheduled task behind
// an injected clock, and a query that asked the database for the time would be a task no test
// could move without waiting two days.
//
// `status = 'Submitted'` is the whole of "live", by construction rather than by convention:
// 000502's `ck_bids_superseded_is_not_live` makes the live offer and the head of its chain the
// same row, so this predicate misses no live offer. Everything else it excludes is already over —
// accepted, rejected, withdrawn, superseded, and offers this sweep expired on an earlier pass.
//
// `pickup_at IS NOT NULL` because the column is nullable and no constraint says otherwise:
// `ck_bids_offer_has_timing` was removed by 000501 and is still not written (Docs/11 §9). Nothing
// this platform writes past 'Draft' leaves it NULL, and a claim that relied on a validator holding
// would sweep a row it could not judge the moment some other writer skipped it.
//
// `ORDER BY pickup_at` drains the longest-overdue first, and `idx_bids_live_expiry` (000503) is a
// partial index on exactly this predicate in exactly this order.
const ExpiryClaim = `
	SELECT id
	FROM bids
	WHERE status = 'Submitted'
	  AND pickup_at IS NOT NULL
	  AND pickup_at <= $1
	ORDER BY pickup_at
	FOR UPDATE SKIP LOCKED
	LIMIT $2`

// Expire ends one claimed offer, as the platform (SHIP-89).
//
// The offer becomes [StatusExpired] and the row survives, exactly as a withdrawal's does: Docs/01
// §4.3 requires the platform to record every offer and Docs/02 §4 keeps the chain readable to both
// parties afterwards. There is no delete on this table.
//
// r must be the worker's transaction, and the identifier must have come from [ExpiryClaim] running
// in it — so the row is already held `FOR UPDATE SKIP LOCKED` and a second worker sweeping at the
// same instant was handed different rows. The transaction is checked rather than documented,
// because the status write and the event have to commit together or not at all (Docs/06 §4.0).
//
// # The write carries the claim's own predicate, and that is not belt and braces
//
// [postgresStore.expireBid] is a compare-and-set on the same three conditions the claim selected
// on, including the deadline. The caller that exists cannot fail it — the row is locked and was
// read a statement ago — so this is for the caller that does not exist yet, and for the mutation
// that removes the deadline from the claim. `jobs.Service.WarnOfExpiry` makes the same call for
// the same reason and states it: there is no guard behind this one, unlike a job transition, where
// 000402 refuses a status write that did not come through [Service.Transition].
//
// **A row that no longer matches is reported rather than skipped.** It is unreachable through the
// worker, and answering "nothing happened" as success would let a sweep report a pass it did not
// make — which is the failure `expireJobs`' own comment refuses for the same reason.
func (s *Service) Expire(ctx context.Context, r db.Runner, bidID uuid.UUID) (Bid, error) {
	if _, inTx := r.(pgx.Tx); !inTx {
		return Bid{}, fmt.Errorf("bidding: expiring %s: %w", bidID, ErrNotInTransaction)
	}
	if bidID == uuid.Nil {
		return Bid{}, fmt.Errorf("bidding: expiring nothing: %w", ErrBidNotFound)
	}

	expired, moved, err := s.store.expireBid(ctx, r, bidID, s.clock.Now())
	if err != nil {
		return Bid{}, err
	}
	if !moved {
		return Bid{}, fmt.Errorf(
			"bidding: %s was a live offer past its collection time when it was claimed and is not now",
			bidID)
	}

	// The event, in the transaction that wrote the status (SHIP-136). [EventBidExpired] rather
	// than a variant of the withdrawal's, because the payload carries the status and a consumer
	// deciding who to tell branches on what happened: a withdrawal is the provider's own act and
	// needs no notification back to them, while an expiry is the platform's and both parties are
	// hearing it for the first time.
	if err := s.emitClosed(ctx, r, EventBidExpired, expired); err != nil {
		return Bid{}, err
	}
	return expired, nil
}
