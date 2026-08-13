package bidding

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/DulsaraNethmin/Shipper/services/core/internal/db"
)

// The bids table, in SQL (SHIP-84).
//
// **PostgreSQL is not abstracted here and is not going to be** (Docs/06 §4.1, CLAUDE.md). Two of
// this file's statements are written against a specific PostgreSQL mechanism and mean nothing
// without it: `ON CONFLICT … WHERE …` inferring a *partial* unique index, and the btree serialisation
// that makes two concurrent inserts of one key wait for each other rather than both succeed. A
// repository interface designed to keep the database swappable would hide exactly the mechanisms
// that make placing a bid correct, and a mock would accept precisely the writes those indexes exist
// to reject.
//
// Every method takes a db.Runner as its first argument after ctx, so the caller decides whether the
// work stands alone or joins a transaction it already opened (Docs/10 §3.2). [Service.PlaceBid]
// requires a transaction, and says why.
type postgresStore struct{}

// bidColumns is every column of a [Bid], in the order [scanBid] reads them.
//
// # The amount is converted in SQL, in one direction here and the other in [postgresStore.insertBid]
//
// Docs/10 §3.3 stores money as numeric(12,2) and holds it in Go as int64 minor units, never as a
// float. Multiplying by 100 and casting to bigint is exact for a column whose scale is 2, and it
// keeps the conversion in the two statements that cross the boundary rather than in a helper every
// caller has to remember. This is the same pair jobs' `budget` uses, written a second time because
// domains do not import each other and `internal/money` does not exist — see [maxOfferCents].
//
// COALESCE to 0 is safe for the reason it is on jobs' budget: ck_bids_amount refuses a
// non-positive amount, so 0 and NULL cannot be confused in either direction.
//
// The two instants and the two text columns are the exceptions in opposite directions.
// `pickup_at` and `deliver_by` are scanned into pointers because PostgreSQL's NULL has no
// representation in time.Time; `message` and `idempotency_key` are coalesced, because "" is what
// this domain means by "not given" and no constraint permits an empty string in either.
//
// **The columns are named rather than `SELECT *`.** A column added to `bids` by SHIP-89's expiry
// terms or SHIP-92's award record must not arrive in this package's shapes with somebody else's
// schema change; naming them makes admitting one a deliberate edit here. 000502's two columns were
// admitted that way, which is what this comment predicted.
const bidColumns = `
	id, job_id, provider_id, offered_by, status,
	COALESCE((amount * 100)::bigint, 0),
	pickup_at, deliver_by,
	COALESCE(message, ''), COALESCE(idempotency_key, ''),
	superseded_by,
	created_at, updated_at`

// scanBid reads one row of [bidColumns].
//
// One function rather than a copy per call site, for the reason jobs' and fleet's scanners are one:
// a column added to the list and not here is a scan mismatch at the first call, which is the failure
// worth having.
func scanBid(row pgx.Row) (Bid, error) {
	var (
		bid          Bid
		pickupAt     *time.Time
		deliverBy    *time.Time
		supersededBy *uuid.UUID
	)

	if err := row.Scan(
		&bid.ID, &bid.JobID, &bid.ProviderID, &bid.OfferedBy, &bid.Status,
		&bid.AmountCents,
		&pickupAt, &deliverBy,
		&bid.Message, &bid.Key,
		&supersededBy,
		&bid.CreatedAt, &bid.UpdatedAt,
	); err != nil {
		return Bid{}, err
	}

	// Never NULL on anything this package writes past 'Draft' — [Offer.validate] refuses an offer
	// naming neither instant — but read as nullable anyway, because the columns *are* nullable and a
	// scan that relied on a validator holding would be silently wrong the first time some other
	// writer skipped it. (There is deliberately no `ck_bids_offer_has_timing`; 000501 removed it and
	// 000502's header records that the design question it was waiting on is now answered.)
	if pickupAt != nil {
		bid.PickupAt = *pickupAt
	}
	if deliverBy != nil {
		bid.DeliverBy = *deliverBy
	}

	// uuid.Nil for the head of a chain, which is what [Bid.SupersededBy] means by "no successor" —
	// a total answer rather than a missing one, so the pointer stops at this boundary.
	if supersededBy != nil {
		bid.SupersededBy = *supersededBy
	}
	return bid, nil
}

// insertBid records an offer, or reports that this key already placed one.
//
// # ON CONFLICT DO NOTHING, and every word of it is load-bearing
//
// **ON CONFLICT** rather than an insert whose unique violation is caught, for the reason 000602's
// twin gives: a violation aborts the surrounding transaction, and the caller still has a row to read
// afterwards. Catching it would mean unwinding to a savepoint to ask a question the conflict has
// already answered.
//
// **DO NOTHING** rather than DO UPDATE, because a retry must return what was recorded rather than
// overwrite it with a second attempt's values. A provider whose phone retried an hour later with a
// clock that had moved must not have their committed offer silently repriced.
//
// **The arbiter is uq_bids_idempotency, inferred by its columns and its predicate**, because a
// partial unique index has no constraint name to name. The WHERE clause here is not a filter on
// rows; it is how PostgreSQL is told which index this statement expects.
//
// # And uq_bids_one_submitted_per_provider_per_job is deliberately *not* the arbiter
//
// It is the other index on this table that this insert can violate, and naming only one arbiter is
// what keeps the two outcomes apart. A conflict on the arbiter is absorbed silently and answered
// from the row; a conflict on the *other* index raises 23505 and becomes [ErrAlreadyBid]. That is
// exactly the distinction the endpoint has to make — "your request already went through" against
// "you already have an offer standing" — and it is made by PostgreSQL rather than by a check this
// code could get wrong under concurrency.
//
// The ordering is PostgreSQL's speculative-insertion protocol and is worth stating because a retry
// conflicts with *both*: the arbiter is checked first, so a repeated key never reaches the second
// index, and a retry is never mistaken for a second offer.
//
// Under a concurrent duplicate the second statement waits on the first's speculative insertion, then
// finds the committed row and returns none — which is why the caller's follow-up read is what
// answers, rather than this returning a partially written row.
//
// # The arbiter names `offered_by` since 000502, and that is 000501's own rule extended
//
// A negotiation now has two writers, so `(job_id, provider_id, idempotency_key)` no longer identifies
// one caller's request: a customer and a provider whose clients generated the same value would collide
// inside one negotiation, and the second would be answered from the first's row — a request that wrote
// nothing, reported as though it had. The arbiter has to name the index exactly, so this clause and
// 000502's `CREATE UNIQUE INDEX` move together or neither works.
func (postgresStore) insertBid(ctx context.Context, r db.Runner, b Bid) (Bid, bool, error) {
	const q = `
		INSERT INTO bids
			(id, job_id, provider_id, offered_by, status, amount, pickup_at, deliver_by, message,
			 idempotency_key)
		VALUES ($1, $2, $3, $4, $5, ($6::bigint)::numeric / 100, $7, $8, nullif($9, ''), nullif($10, ''))
		ON CONFLICT (job_id, provider_id, offered_by, idempotency_key)
			WHERE idempotency_key IS NOT NULL DO NOTHING
		RETURNING ` + bidColumns

	created, err := scanBid(r.QueryRow(ctx, q,
		b.ID, b.JobID, b.ProviderID, string(b.OfferedBy), string(b.Status),
		b.AmountCents, b.PickupAt, b.DeliverBy, b.Message, b.Key))
	switch {
	case errors.Is(err, db.ErrNoRows):
		return Bid{}, false, nil
	case db.IsUniqueViolation(err, "uq_bids_one_submitted_per_provider_per_job"):
		// Named rather than matched on SQLSTATE alone, so a unique violation from some future index
		// is not silently reported as a second offer — which would be a message telling the provider
		// to withdraw a bid that had nothing to do with it.
		return Bid{}, false, fmt.Errorf("bidding: %s on %s: %w", b.ProviderID, b.JobID, ErrAlreadyBid)
	case err != nil:
		return Bid{}, false, fmt.Errorf("bidding: placing %s's offer on %s: %w", b.ProviderID, b.JobID, err)
	}
	return created, true, nil
}

// bidPlacedUnder is what a key already placed on a job for a provider, if anything.
//
// **Scoped by provider as well as by job, and that is a privacy rule rather than a lookup detail.**
// A job has many bidders. A query keyed on the job and the idempotency key alone would hand one
// provider another provider's offer — including its price — to anyone who guessed or reused a key,
// which is Docs/01 §4.3's second privacy rule broken by a WHERE clause. The provider is taken from
// the authenticated subject and never from the request body, so there is no way to ask on somebody
// else's behalf. It matches uq_bids_idempotency's columns exactly, which is what makes the index
// serve the read as well as the constraint.
//
// Called twice by [Service.PlaceBid]: once before the insert, which is the ordinary retry, and once
// after ON CONFLICT declines, which is the concurrent duplicate. At READ COMMITTED each statement
// takes a fresh snapshot, so a row committed by the request that won a race is visible to this one
// even though the transaction around it began earlier.
// **Scoped by the offering party as well since 000502**, which is the identical rule applied to the
// negotiation's second writer. A customer's counter and a provider's offer are two callers inside one
// `(job_id, provider_id)` pair, so a lookup without `offered_by` would hand one of them the other's
// row on a key collision — a request that wrote nothing, answered as though it had.
func (postgresStore) bidPlacedUnder(
	ctx context.Context,
	r db.Runner,
	jobID, providerID uuid.UUID,
	by Party,
	key string,
) (Bid, bool, error) {
	const q = `
		SELECT ` + bidColumns + `
		FROM bids
		WHERE job_id = $1 AND provider_id = $2 AND offered_by = $3 AND idempotency_key = $4`

	bid, err := scanBid(r.QueryRow(ctx, q, jobID, providerID, string(by), key))
	switch {
	case errors.Is(err, db.ErrNoRows):
		return Bid{}, false, nil
	case err != nil:
		return Bid{}, false, fmt.Errorf("bidding: reading what %s wrote on %s: %w", key, jobID, err)
	}
	return bid, true, nil
}

// lockBid takes one bid by its identifier and holds it for the rest of the transaction (SHIP-85,
// SHIP-86).
//
// **Scoped by the identifier alone, and nothing else — which is the opposite of
// [postgresStore.bidPlacedUnder] and is deliberate.** That read is a *lookup*: it answers "what did
// this key place", a question only the caller who sent the key may ask, so scoping it by provider is
// the privacy rule. This one is an *addressed* read: the caller named a row, and the platform has to
// say what is wrong with reaching it. A `WHERE id = $1 AND provider_id = $2` that returned nothing
// could not tell a bid belonging to somebody else from a bid that does not exist, and only one of
// those is a caller probing for another provider's offers. [Service.ownBid] makes the comparison in Go
// and raises [ErrNotBidOwner]; both become the same 404 on the wire, so the distinction costs the
// caller nothing and buys the platform a fact it can act on.
//
// **FOR UPDATE, and it is load-bearing rather than defensive.** Both callers read a status, decide
// against it, and write — and the decision is "is this offer still the provider's to change". Without
// the lock, an award committing in the window between the SELECT and the UPDATE leaves a withdrawal
// unpicking a bid uq_bids_one_accepted_per_job says two parties are committed to. The lock is only
// held inside a transaction, which is why [ErrNotInTransaction] exists.
func (postgresStore) lockBid(ctx context.Context, r db.Runner, id uuid.UUID) (Bid, error) {
	const q = `
		SELECT ` + bidColumns + `
		FROM bids
		WHERE id = $1
		FOR UPDATE`

	bid, err := scanBid(r.QueryRow(ctx, q, id))
	switch {
	case errors.Is(err, db.ErrNoRows):
		return Bid{}, fmt.Errorf("bidding: %s: %w", id, ErrBidNotFound)
	case err != nil:
		return Bid{}, fmt.Errorf("bidding: reading bid %s: %w", id, err)
	}
	return bid, nil
}

// reviseOffer writes a revised price, timing and message (SHIP-85).
//
// # Three columns are absent from the SET list and each absence is a decision
//
// **`idempotency_key` is not written, and must never be.** It holds the key the offer was *placed*
// under, and that is how a placement retried after the middleware has forgotten it is matched back to
// its own row. Overwriting it with a revision's key would leave that retry unable to find anything,
// meeting uq_bids_one_submitted_per_provider_per_job instead and being told the provider already had a
// live offer — a 409 for a request that succeeded, which is the exact failure 000501 added the column
// to prevent. See [Revision].
//
// **`status` is not written**, because a revision does not move an offer: a revised bid is the same
// live offer at a different number. Leaving it out also leaves
// uq_bids_one_submitted_per_provider_per_job undisturbed — the row stays inside the index's predicate
// throughout, so a revision cannot open a window in which a second offer would be accepted.
//
// **`updated_at` is not written**, because `bids_set_updated_at` in 000500 does it. A statement setting
// it by hand would be a second answer to what "changed" means.
//
// No ON CONFLICT and no arbiter. The only unique indexes on this table are partial on statuses this
// statement does not touch, so there is nothing here to conflict with — which is a property of the two
// omissions above rather than a coincidence.
func (postgresStore) reviseOffer(ctx context.Context, r db.Runner, id uuid.UUID, o Offer) (Bid, error) {
	const q = `
		UPDATE bids
		SET amount     = ($2::bigint)::numeric / 100,
		    pickup_at  = $3,
		    deliver_by = $4,
		    message    = nullif($5, '')
		WHERE id = $1
		RETURNING ` + bidColumns

	bid, err := scanBid(r.QueryRow(ctx, q, id, o.AmountCents, o.PickupAt, o.DeliverBy, o.Message))
	if err != nil {
		return Bid{}, fmt.Errorf("bidding: revising bid %s: %w", id, err)
	}
	return bid, nil
}

// withdrawBid moves an offer to Withdrawn (SHIP-86).
//
// A plain `UPDATE`, and the contrast with a job's status is worth stating rather than leaving to be
// noticed. Docs/02 §2 gives the *job* lifecycle one entry point, and 000402 has a trigger refusing any
// status write that did not come through it. Docs/02 §4 says no such thing about bids, and 000500
// declined to invent the mechanism: "a provider who fills the form in and sends it has legitimately
// created a row at 'Submitted', and a customer's counter-offer arrives as a new row that was never a
// draft". So a bid's status is written directly, and what makes it safe is that the row is already
// locked by [postgresStore.lockBid] and the caller has decided against the status it read.
//
// The status is a parameter rather than a literal so that the value comes from [StatusWithdrawn] —
// Docs/02 §4's own string, held to `ck_bids_status` by
// TestEveryBidStatusConstraintMatchesTheGoConstants — rather than from a second spelling of it in SQL.
//
// **The row survives, and that is the point of a withdrawal rather than a delete.** Docs/01 §4.3
// requires the platform to "record all offers, counter-offers, withdrawals, and acceptances", and
// Docs/02 §4 keeps bid history visible to the customer, the bidding provider and administrators. The
// same reading fleet gives a deactivated vehicle: there is no delete on this table and there is not
// going to be one.
func (postgresStore) withdrawBid(ctx context.Context, r db.Runner, id uuid.UUID) (Bid, error) {
	const q = `
		UPDATE bids
		SET status = $2
		WHERE id = $1
		RETURNING ` + bidColumns

	bid, err := scanBid(r.QueryRow(ctx, q, id, string(StatusWithdrawn)))
	if err != nil {
		return Bid{}, fmt.Errorf("bidding: withdrawing bid %s: %w", id, err)
	}
	return bid, nil
}

// supersedeHead moves the offer being answered out of the live predicate (SHIP-87).
//
// # It is a compare-and-set, and the WHERE clause is the whole of that
//
// `status = 'Submitted' AND superseded_by IS NULL` is not a filter narrowing a row already chosen; it
// is the check itself. Under READ COMMITTED an `UPDATE` re-evaluates its `WHERE` after taking the row
// lock, so a transaction that waited on somebody else's counter sees the committed result and matches
// nothing — which is why this reports whether it matched rather than assuming it did.
//
// The caller has already taken the same row `FOR UPDATE` and read its status, so a false here is
// unreachable through [Service.CounterOffer] and is reported rather than ignored. **That redundancy
// is the point.** [postgresStore.lockBid] makes the refusal legible; this makes it true, and a future
// writer that forgets the lock still cannot fork a chain.
//
// # `superseded_by` is deliberately not written here, and that is not tidiness
//
// The successor does not exist yet, and `fk_bids_superseded_by` is checked at the end of the
// statement rather than at commit — this migration takes no deferrable constraint, so every
// invariant holds at every instant rather than only at the end of the transaction. Writing the
// status first is also what frees `uq_bids_one_submitted_per_provider_per_job` for the insert that
// follows: the head leaves the predicate before the successor enters it, so the two never coexist
// inside it. `ck_bids_superseded_is_not_live` is satisfied throughout, because `superseded_by` is
// still NULL while the status changes and the status is no longer 'Submitted' when the link lands.
func (postgresStore) supersedeHead(ctx context.Context, r db.Runner, id uuid.UUID) (bool, error) {
	const q = `
		UPDATE bids
		SET status = $2
		WHERE id = $1 AND status = $3 AND superseded_by IS NULL`

	tag, err := r.Exec(ctx, q, id, string(StatusSuperseded), string(StatusSubmitted))
	if err != nil {
		return false, fmt.Errorf("bidding: superseding bid %s: %w", id, err)
	}
	return tag.RowsAffected() == 1, nil
}

// linkSuccessor records which offer displaced this one, completing the chain (SHIP-88).
//
// The third statement of a counter and the one that makes the history readable. `superseded_by IS
// NULL` again, so a second attempt matches nothing rather than relinking a row somebody else has
// already answered. `uq_bids_one_successor` guards the *other* direction — two offers cannot name one
// counter as what displaced them, which would be a negotiation that merged rather than a chain.
//
// It returns the displaced row so the caller can assert what was written rather than what it meant to
// write — the property fixtures_test.go's `row` helper exists for, made available to the service
// itself.
func (postgresStore) linkSuccessor(ctx context.Context, r db.Runner, id, successor uuid.UUID) (Bid, error) {
	const q = `
		UPDATE bids
		SET superseded_by = $2
		WHERE id = $1 AND superseded_by IS NULL
		RETURNING ` + bidColumns

	bid, err := scanBid(r.QueryRow(ctx, q, id, successor))
	if err != nil {
		return Bid{}, fmt.Errorf("bidding: linking %s to its successor %s: %w", id, successor, err)
	}
	return bid, nil
}
