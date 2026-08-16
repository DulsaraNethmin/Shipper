package profiles

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/DulsaraNethmin/Shipper/services/core/internal/db"
)

// The PostgreSQL store for this domain.
//
// A struct with no fields, for the reason `fleet.postgresStore` has none: every method takes the
// db.Runner it is to use, so there is no connection to hold and no second place a transaction could
// come from (Docs/10 §3.2). It is unexported because nothing outside this package has any business
// reading these tables — `internal/fleet` reads `provider_verifications` in SQL, which crosses no Go
// boundary and needs nothing from here.
//
// **There is no repository interface and there will not be.** CLAUDE.md and Docs/06 §4.1 both say not
// to abstract PostgreSQL, and this domain is a clear case: the guard that makes a verification state
// unsettable is a trigger, and a mocked store would accept every write the trigger exists to refuse.
type postgresStore struct{}

// verification reads one provider's standing and the decision behind it.
//
// # One statement with a lateral join rather than two reads
//
// The state is on `provider_verifications` and the reason is on the newest
// `provider_verification_decisions` row, and reading them separately would let a decision land
// between the two — producing a state from before it and a reason from after. `LEFT JOIN LATERAL`
// keeps them one snapshot, and LEFT rather than inner because a provider nobody has decided anything
// about has no decision row at all and is not an error.
//
// A provider with no record is [ErrNoSuchProvider]. `000200` gives every provider one at
// registration, so this is either an account that never existed or one removed underneath a live
// session — and both are the same answer to whoever asked.
func (postgresStore) verification(ctx context.Context, r db.Runner, providerID uuid.UUID) (Verification, error) {
	const q = `
		SELECT v.provider_id, v.state, v.created_at,
		       COALESCE(d.reason, ''), d.decided_at
		FROM provider_verifications v
		LEFT JOIN LATERAL (
			SELECT reason, decided_at
			FROM provider_verification_decisions
			WHERE provider_id = v.provider_id
			ORDER BY decided_at DESC, id DESC
			LIMIT 1
		) d ON true
		WHERE v.provider_id = $1`

	var (
		found     Verification
		decidedAt *time.Time
	)

	err := r.QueryRow(ctx, q, providerID).Scan(
		&found.ProviderID, &found.State, &found.SubmittedAt, &found.Reason, &decidedAt)

	switch {
	case errors.Is(err, db.ErrNoRows):
		return Verification{}, fmt.Errorf("profiles: %s has no verification record: %w",
			providerID, ErrNoSuchProvider)
	case err != nil:
		return Verification{}, fmt.Errorf("profiles: read the verification of %s: %w", providerID, err)
	}

	if decidedAt != nil {
		found.DecidedAt = *decidedAt
	}
	return found, nil
}

// lockVerification takes the row's lock and reports the state it holds.
//
// `FOR UPDATE`, so two administrators deciding at once serialise rather than interleave: without it
// both would read the same state, and the second would write a decision describing a move that had
// already happened. `provider_verification_decide` takes the same lock, and taking it twice in one
// transaction is free — the second is a no-op on a lock this transaction already holds.
//
// It is a separate read from [postgresStore.verification] because the two want different things: this
// wants the state under a lock, and that wants the whole record for a reader. Making one serve both
// would mean locking on every read.
func (postgresStore) lockVerification(ctx context.Context, r db.Runner, providerID uuid.UUID) (State, error) {
	const q = `SELECT state FROM provider_verifications WHERE provider_id = $1 FOR UPDATE`

	var state State
	err := r.QueryRow(ctx, q, providerID).Scan(&state)

	switch {
	case errors.Is(err, db.ErrNoRows):
		return "", fmt.Errorf("profiles: %s has no verification record: %w", providerID, ErrNoSuchProvider)
	case err != nil:
		return "", fmt.Errorf("profiles: lock the verification of %s: %w", providerID, err)
	}
	return state, nil
}

// decide calls the one guarded transition.
//
// **This function contains no UPDATE and must not gain one.** `provider_verification_decide` writes
// the decision, names it to the trigger and moves the state, so what Go does here is supply the five
// values and read the failure. A statement here that set `state` would be refused by
// `provider_verification_change_is_guarded` — which is the point of the trigger — and the refusal
// would arrive as a 500 rather than as anything a caller could act on.
//
// The actor's identity is passed as a NULL for [ActorSystem] rather than as the nil UUID, because
// `ck_provider_verification_decisions_actor_id` distinguishes them and the nil UUID is a value.
func (postgresStore) decide(
	ctx context.Context,
	r db.Runner,
	providerID uuid.UUID,
	to State,
	by Actor,
	reason string,
) error {

	const q = `SELECT provider_verification_decide($1, $2, $3, $4, $5)`

	var actorID any
	if by.ID != uuid.Nil {
		actorID = by.ID
	}

	var decision uuid.UUID
	if err := r.QueryRow(ctx, q, providerID, string(to), string(by.Type), actorID, reason).Scan(&decision); err != nil {
		return fmt.Errorf("profiles: decide %s for %s: %w", to, providerID, missingProvider(err))
	}
	return nil
}

// missingProvider turns the function's own "no such provider" into this domain's sentinel.
//
// `provider_verification_decide` raises `no_data_found` when the record is not there, which is the
// one refusal it makes that a caller can do something about. Everything else it can raise is a
// constraint the domain has already checked, so it stays an opaque failure with its cause logged.
//
// Matched on the SQLSTATE rather than on the message, for the reason `fleet.duplicate` matches on a
// constraint name: a message is a string somebody may reword, and a code is part of the contract.
func missingProvider(err error) error {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "P0002" {
		return ErrNoSuchProvider
	}
	return err
}

// awaitingReview reads one page of the review queue, oldest first (SHIP-153).
//
// # The ordering is total and the cursor is why it needs two columns
//
// `(created_at, provider_id)`. `created_at` is not unique — two providers registering in the same
// millisecond would make a single-column cursor either skip a row or repeat one, and a review queue
// that can hide a row is worse than one that shows it twice, because the hidden row is a person
// waiting.
//
// `idx_provider_verifications_state` is `(state, created_at)` and serves the predicate and the
// leading ordering column; `000200` built it for this read and said so.
//
// # An INNER JOIN on `users`, and it cannot lose a row
//
// `fk_provider_verifications_provider` is `ON DELETE RESTRICT`, so a verification record cannot
// outlive the account it belongs to. The join is therefore total, and an outer join would only be
// insurance against a foreign key the schema does not permit to be violated.
//
// `name` is coalesced because `000006` made the column nullable deliberately: an account created
// before it has none, a name cannot be backfilled, and the empty string is the honest report of
// "the platform was never told".
func (postgresStore) awaitingReview(ctx context.Context, r db.Runner, q QueueQuery) ([]QueueEntry, error) {
	const query = `
		SELECT v.provider_id, coalesce(u.name, ''), u.email, u.phone, v.state, v.created_at
		FROM provider_verifications v
		JOIN users u ON u.id = v.provider_id
		WHERE v.state = $1
		  AND ($2::timestamptz IS NULL OR (v.created_at, v.provider_id) > ($2, $3))
		ORDER BY v.created_at, v.provider_id
		LIMIT $4`

	// A nil rather than a zero time for the first page: `> (NULL, …)` is NULL rather than true, so
	// the predicate has to be skipped rather than satisfied. Passing the zero time would work today
	// and would stop working the first time somebody backdated a fixture.
	var after, afterID any
	if !q.After.Zero() {
		after, afterID = q.After.SubmittedAt, q.After.ProviderID
	}

	rows, err := r.Query(ctx, query, string(q.State), after, afterID, q.Limit)
	if err != nil {
		return nil, fmt.Errorf("profiles: reading the %s verification queue: %w", q.State, err)
	}
	defer rows.Close()

	out := []QueueEntry{}
	for rows.Next() {
		var e QueueEntry
		if err := rows.Scan(&e.ProviderID, &e.Name, &e.Email, &e.Phone,
			&e.State, &e.SubmittedAt); err != nil {
			return nil, fmt.Errorf("profiles: reading a verification queue entry: %w", err)
		}
		out = append(out, e)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("profiles: reading the %s verification queue: %w", q.State, err)
	}
	return out, nil
}

// isProvider reports whether the account exists and is a provider account.
//
// This domain reading `users` is sanctioned rather than a boundary crossed, the reading
// `fleet.postgresStore.isProvider` already records: the table is in the shared migration block
// precisely because it is read across the whole service (Docs/10 §9.2), and
// `provider_verifications.provider_id` already references it. What is not sanctioned — and is not
// done — is importing `internal/identity` to ask.
//
// A missing account reports false rather than an error, for the same reason it does there: the caller
// holds a token naming that account, so a missing row means it has gone underneath a live session.
func (postgresStore) isProvider(ctx context.Context, r db.Runner, id uuid.UUID) (bool, error) {
	const q = `SELECT role = 'provider' FROM users WHERE id = $1`

	var provider bool
	err := r.QueryRow(ctx, q, id).Scan(&provider)
	switch {
	case errors.Is(err, db.ErrNoRows):
		return false, nil
	case err != nil:
		return false, fmt.Errorf("profiles: read the role of %s: %w", id, err)
	}
	return provider, nil
}
