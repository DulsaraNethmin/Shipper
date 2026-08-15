package notifications

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/DulsaraNethmin/Shipper/services/core/internal/db"
)

// The device_tokens statements (SHIP-140), hand-written per Docs/10 §3.1 and unexported per
// Docs/10 §2.2 — no repository interface, for the reason postgres.go gives: the partial unique
// indexes are what make this correct and a mock would accept a duplicate past either of them.

// revokeDisplaced marks everything one registration supersedes.
//
// Two conditions in one statement rather than two round trips, and the OR is what makes the second
// one visible: a token can be displaced because this *device* had a different one, or because this
// *token* was live somewhere else. The second is a handset restored from another device's backup,
// and missing it would leave a push meant for the previous owner reaching the new one.
//
// Nothing is deleted. 000701 keeps the row so a device's token history stays readable and so
// SHIP-176 can count rejections rather than find them absent.
func (postgresStore) revokeDisplaced(
	ctx context.Context, r db.Runner, sessionID uuid.UUID, token, reason string, at time.Time,
) error {
	const q = `
		UPDATE device_tokens
		   SET revoked_at = $3, revoked_reason = $4
		 WHERE revoked_at IS NULL
		   AND (device_session_id = $1 OR token = $2)`

	if _, err := r.Exec(ctx, q, sessionID, token, at, reason); err != nil {
		return fmt.Errorf("notifications: revoking the tokens this registration displaces: %w", err)
	}
	return nil
}

// insertDeviceToken writes the live row.
//
// No ON CONFLICT. Every live row this could collide with was revoked by revokeDisplaced in the same
// transaction, so a unique violation here means two registrations raced — and the loser must be
// told rather than silently do nothing, because it is the one holding the token the client will
// expect to be addressable.
func (postgresStore) insertDeviceToken(ctx context.Context, r db.Runner, d DeviceToken) error {
	const q = `
		INSERT INTO device_tokens
		    (id, user_id, device_session_id, platform, token, registered_at)
		VALUES ($1, $2, $3, $4, $5, $6)`

	if _, err := r.Exec(ctx, q, d.ID, d.UserID, d.SessionID, string(d.Platform), d.Token, d.RegisteredAt); err != nil {
		return fmt.Errorf("notifications: registering a device token: %w", err)
	}
	return nil
}

// revokeSessionTokens ends push delivery to one signed-in device, reporting whether there was any.
func (postgresStore) revokeSessionTokens(
	ctx context.Context, r db.Runner, sessionID uuid.UUID, reason string, at time.Time,
) (bool, error) {
	const q = `
		UPDATE device_tokens
		   SET revoked_at = $2, revoked_reason = $3
		 WHERE device_session_id = $1
		   AND revoked_at IS NULL`

	tag, err := r.Exec(ctx, q, sessionID, at, reason)
	if err != nil {
		return false, fmt.Errorf("notifications: deregistering the tokens on session %s: %w", sessionID, err)
	}
	return tag.RowsAffected() > 0, nil
}

// revokeToken ends push delivery to one token, wherever it is live.
func (postgresStore) revokeToken(
	ctx context.Context, r db.Runner, token, reason string, at time.Time,
) (bool, error) {
	const q = `
		UPDATE device_tokens
		   SET revoked_at = $2, revoked_reason = $3
		 WHERE token = $1
		   AND revoked_at IS NULL`

	tag, err := r.Exec(ctx, q, token, at, reason)
	if err != nil {
		return false, fmt.Errorf("notifications: revoking a device token: %w", err)
	}
	return tag.RowsAffected() > 0, nil
}

// registeredDevice is one live token and the session it hangs off.
//
// The session travels beside the token because the caller has to ask [Sessions] whether it is still
// usable before addressing it — which is the whole of "clears on sign-out" (000701).
type registeredDevice struct {
	SessionID uuid.UUID
	Token     string
}

// liveDevicesOf reads the registered tokens of the accounts a rule resolved to.
//
// Keyed by account, because the caller is resolving recipients and a recipient may be signed in on
// several handsets. Ordered by registration so a two-device customer's rows are written in a stable
// order and a test can say so without sorting.
//
// It does **not** filter on the session being live: that fact is identity's and is asked through a
// port. Reading a revoked session's token and then discarding it is one query more than a join
// would be and one boundary fewer — see ports.go.
func (postgresStore) liveDevicesOf(
	ctx context.Context, r db.Runner, ids []uuid.UUID,
) (map[uuid.UUID][]registeredDevice, error) {
	const q = `
		SELECT user_id, device_session_id, token
		FROM device_tokens
		WHERE user_id = ANY($1)
		  AND revoked_at IS NULL
		ORDER BY registered_at, id`

	rows, err := r.Query(ctx, q, ids)
	if err != nil {
		return nil, fmt.Errorf("notifications: reading the devices of %d recipient(s): %w", len(ids), err)
	}

	type row struct {
		user   uuid.UUID
		device registeredDevice
	}
	found, err := pgx.CollectRows(rows, func(cr pgx.CollectableRow) (row, error) {
		var out row
		err := cr.Scan(&out.user, &out.device.SessionID, &out.device.Token)
		return out, err
	})
	if err != nil {
		return nil, fmt.Errorf("notifications: reading device tokens: %w", err)
	}

	byUser := make(map[uuid.UUID][]registeredDevice, len(found))
	for _, f := range found {
		byUser[f.user] = append(byUser[f.user], f.device)
	}
	return byUser, nil
}
