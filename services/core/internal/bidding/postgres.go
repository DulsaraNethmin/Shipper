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
// **The columns are named rather than `SELECT *`.** A column added to `bids` by SHIP-87's supersede
// chain or SHIP-89's expiry terms must not arrive in this package's shapes with somebody else's
// schema change; naming them makes admitting one a deliberate edit here.
const bidColumns = `
	id, job_id, provider_id, status,
	COALESCE((amount * 100)::bigint, 0),
	pickup_at, deliver_by,
	COALESCE(message, ''), COALESCE(idempotency_key, ''),
	created_at, updated_at`

// scanBid reads one row of [bidColumns].
//
// One function rather than a copy per call site, for the reason jobs' and fleet's scanners are one:
// a column added to the list and not here is a scan mismatch at the first call, which is the failure
// worth having.
func scanBid(row pgx.Row) (Bid, error) {
	var (
		bid       Bid
		pickupAt  *time.Time
		deliverBy *time.Time
	)

	if err := row.Scan(
		&bid.ID, &bid.JobID, &bid.ProviderID, &bid.Status,
		&bid.AmountCents,
		&pickupAt, &deliverBy,
		&bid.Message, &bid.Key,
		&bid.CreatedAt, &bid.UpdatedAt,
	); err != nil {
		return Bid{}, err
	}

	// Never NULL on anything this package writes — ck_bids_offer_has_timing refuses an offer past
	// Draft that names neither — but read as nullable anyway, because the column *is* nullable and a
	// scan that relied on a constraint holding would be silently wrong if the constraint ever went.
	if pickupAt != nil {
		bid.PickupAt = *pickupAt
	}
	if deliverBy != nil {
		bid.DeliverBy = *deliverBy
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
func (postgresStore) insertBid(ctx context.Context, r db.Runner, b Bid) (Bid, bool, error) {
	const q = `
		INSERT INTO bids
			(id, job_id, provider_id, status, amount, pickup_at, deliver_by, message, idempotency_key)
		VALUES ($1, $2, $3, $4, ($5::bigint)::numeric / 100, $6, $7, nullif($8, ''), nullif($9, ''))
		ON CONFLICT (job_id, provider_id, idempotency_key) WHERE idempotency_key IS NOT NULL DO NOTHING
		RETURNING ` + bidColumns

	created, err := scanBid(r.QueryRow(ctx, q,
		b.ID, b.JobID, b.ProviderID, string(b.Status),
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
func (postgresStore) bidPlacedUnder(
	ctx context.Context,
	r db.Runner,
	jobID, providerID uuid.UUID,
	key string,
) (Bid, bool, error) {
	const q = `
		SELECT ` + bidColumns + `
		FROM bids
		WHERE job_id = $1 AND provider_id = $2 AND idempotency_key = $3`

	bid, err := scanBid(r.QueryRow(ctx, q, jobID, providerID, key))
	switch {
	case errors.Is(err, db.ErrNoRows):
		return Bid{}, false, nil
	case err != nil:
		return Bid{}, false, fmt.Errorf("bidding: reading what %s placed on %s: %w", key, jobID, err)
	}
	return bid, true, nil
}
