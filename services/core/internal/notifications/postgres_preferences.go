package notifications

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/DulsaraNethmin/Shipper/services/core/internal/db"
)

// The preference table's SQL (SHIP-142), hand-written per Docs/10 §3.1 and unexported per §2.2.
//
// Three statements and no repository interface: the constraint that makes this correct is
// ck_notification_preferences_category, which a mock would happily accept a muted award past.

// mutedOf is one account's muted set, for the preference screen.
func (postgresStore) mutedOf(
	ctx context.Context, r db.Runner, userID uuid.UUID,
) (map[Category]bool, error) {
	const q = `SELECT category FROM notification_preferences WHERE user_id = $1`

	rows, err := r.Query(ctx, q, userID)
	if err != nil {
		return nil, fmt.Errorf("notifications: reading the preferences of %s: %w", userID, err)
	}

	categories, err := pgx.CollectRows(rows, pgx.RowTo[string])
	if err != nil {
		return nil, fmt.Errorf("notifications: reading preferences: %w", err)
	}

	muted := make(map[Category]bool, len(categories))
	for _, category := range categories {
		muted[Category(category)] = true
	}
	return muted, nil
}

// mutedBy is which of these accounts has muted one category, for the consumer.
//
// One query for the whole batch rather than one per recipient: an award notifies two people and a
// per-recipient lookup would be two round trips inside a transaction holding a Kafka partition's
// progress. A set rather than a map of maps, because the caller already knows which category it is
// asking about — [Service.filterMuted] is called with one.
//
// The category is a parameter rather than interpolated, so a value that somehow reached here from
// outside the closed set reads nothing rather than becoming SQL.
func (postgresStore) mutedBy(
	ctx context.Context, r db.Runner, ids []uuid.UUID, category Category,
) (map[uuid.UUID]bool, error) {
	const q = `
		SELECT user_id
		FROM notification_preferences
		WHERE user_id = ANY($1)
		  AND category = $2`

	rows, err := r.Query(ctx, q, ids, string(category))
	if err != nil {
		return nil, fmt.Errorf("notifications: reading who has muted %s: %w", category, err)
	}

	users, err := pgx.CollectRows(rows, pgx.RowTo[uuid.UUID])
	if err != nil {
		return nil, fmt.Errorf("notifications: reading who has muted %s: %w", category, err)
	}

	muted := make(map[uuid.UUID]bool, len(users))
	for _, id := range users {
		muted[id] = true
	}
	return muted, nil
}

// replaceMuted makes this account's muted set exactly `wanted`.
//
// # Two statements, in the caller's transaction, and the order matters less than the atomicity
//
// The delete removes what is no longer wanted and the insert adds what now is; between them the
// account is momentarily under-muted, which is why [Service.SetMuted] refuses to run outside a
// transaction. Nothing else can observe the gap.
//
// # ON CONFLICT rather than delete-everything-then-insert
//
// A category that was already muted keeps its row and moves its timestamp, so "when did they turn
// this off" survives a screen being saved again — which a client does on every visit. Deleting the
// row and writing a new one would reset that date to the last time somebody opened a settings page,
// which is a support answer that is technically a fact and practically a lie.
//
// # The delete is scoped by NOT the wanted set, which is what makes an empty list mean something
//
// `wanted` empty is "unmute everything", and `<> ALL('{}')` is true for every row — so the delete
// removes all of this account's rows and the insert writes none. That is the behaviour the empty
// case needs and it falls out of the general statement rather than being a branch, which is worth
// saying because an implementation that special-cased it would be one `if` away from an empty list
// meaning "change nothing" instead.
func (postgresStore) replaceMuted(
	ctx context.Context, r db.Runner, userID uuid.UUID, wanted []Category, at time.Time,
) error {
	names := make([]string, 0, len(wanted))
	for _, category := range wanted {
		names = append(names, string(category))
	}

	const clear = `
		DELETE FROM notification_preferences
		 WHERE user_id = $1
		   AND category <> ALL($2::text[])`

	if _, err := r.Exec(ctx, clear, userID, names); err != nil {
		return fmt.Errorf("notifications: clearing the preferences of %s: %w", userID, err)
	}
	if len(names) == 0 {
		return nil
	}

	const set = `
		INSERT INTO notification_preferences (user_id, category, muted_at)
		SELECT $1, category, $3 FROM unnest($2::text[]) AS category
		ON CONFLICT (user_id, category) DO UPDATE SET muted_at = EXCLUDED.muted_at`

	if _, err := r.Exec(ctx, set, userID, names, at); err != nil {
		return fmt.Errorf("notifications: muting %d category(ies) for %s: %w",
			len(names), userID, err)
	}
	return nil
}
