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

// recordDocument writes one submitted document and reports it as stored (SHIP-81b).
//
// # It returns the row rather than the argument, and the difference is `submitted_at`
//
// The clock is the database's `DEFAULT now()`, which is what every other append-only table in this
// schema uses, so the only way for the caller to learn it is to be told. `RETURNING` is one round
// trip and a second SELECT would be two, with a window between them in which the row could be read
// by somebody else and nothing in it could have changed anyway.
//
// **There is no UPDATE and no upsert here, and there must not be one.** `000201` is append-only and
// enforces it with a trigger; a second submission of the same kind is a second row, and the newest
// of a kind is the current document. An `ON CONFLICT (provider_id, kind) DO UPDATE` would be
// refused by the trigger, and if the trigger were ever removed it would silently destroy the image
// an administrator had already reviewed.
func (postgresStore) recordDocument(ctx context.Context, r db.Runner, d Document) (Document, error) {
	const q = `
		INSERT INTO provider_verification_documents
			(id, provider_id, kind, object_key, content_type, content_length, etag)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
		RETURNING submitted_at`

	err := r.QueryRow(ctx, q,
		d.ID, d.ProviderID, string(d.Kind), d.ObjectKey,
		d.ContentType, d.ContentLength, d.ETag,
	).Scan(&d.SubmittedAt)

	if err != nil {
		return Document{}, fmt.Errorf("profiles: recording a %s document for %s: %w",
			d.Kind, d.ProviderID, alreadyRecorded(err))
	}
	return d, nil
}

// alreadyRecorded turns the object-key index's refusal into this domain's own error.
//
// Named by constraint rather than by SQLSTATE alone, for `fleet.duplicate`'s reason: a unique
// violation from some future index reported as "that image is already somebody's evidence" would be
// a message telling a provider to re-photograph a document that was never the problem.
//
// `uq_provider_verification_documents_object_key` is access control rather than tidiness — one
// object is evidence for at most one provider — so this is the layer underneath
// [keyBelongsToProvider] rather than a duplicate of it. They refuse different things: that one
// refuses a key minted for somebody else, and this one refuses a key already spent.
func alreadyRecorded(err error) error {
	if db.IsUniqueViolation(err, "uq_provider_verification_documents_object_key") {
		return ErrDocumentAlreadyRecorded
	}
	return err
}

// documentsFor reads every document one provider has submitted, newest first within each kind.
//
// # The ordering is `(kind, submitted_at DESC)` and it is what makes "current" answerable
//
// `000201` is append-only, so a retake is a second row of the same kind and the newest of a kind is
// the current document. Ordering by the kind first groups a provider's file the way both readers
// want it — their own list, and SHIP-155's administrator opening a submission — and
// `idx_provider_verification_documents_provider` serves the whole of it in one scan.
//
// `id` breaks the tie on `submitted_at`, because `DEFAULT now()` is the transaction's clock and two
// documents submitted in one transaction would share it. Nothing does that today; an ordering that
// is total regardless costs one column and removes a class of intermittent test failure.
//
// **No pagination.** There are four kinds and a provider retakes each a handful of times at most, so
// this is bounded by how many times somebody re-photographs their licence rather than by anything
// that grows with the platform. `internal/pagination` exists for the feeds where that is not true.
func (postgresStore) documentsFor(ctx context.Context, r db.Runner, providerID uuid.UUID) ([]Document, error) {
	const q = `
		SELECT id, provider_id, kind, object_key, content_type, content_length, etag, submitted_at
		FROM provider_verification_documents
		WHERE provider_id = $1
		ORDER BY kind, submitted_at DESC, id DESC`

	rows, err := r.Query(ctx, q, providerID)
	if err != nil {
		return nil, fmt.Errorf("profiles: reading the documents of %s: %w", providerID, err)
	}
	defer rows.Close()

	out := []Document{}
	for rows.Next() {
		var d Document
		if err := rows.Scan(&d.ID, &d.ProviderID, &d.Kind, &d.ObjectKey,
			&d.ContentType, &d.ContentLength, &d.ETag, &d.SubmittedAt); err != nil {
			return nil, fmt.Errorf("profiles: reading a document of %s: %w", providerID, err)
		}
		out = append(out, d)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("profiles: reading the documents of %s: %w", providerID, err)
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
