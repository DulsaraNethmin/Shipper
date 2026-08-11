// SHIP-43: ending a device session from the device itself.
//
// Signing out ends the session the caller is holding. SHIP-46 adds the other half — ending one
// they are not holding, from their device list — and records a different reason for it, because a
// support conversation and SHIP-149's audit both ask which of the two happened and a single
// "ended by the user" would answer neither.
//
// # Nothing here deletes a row
//
// Docs/10 §3.3: a session ends by being marked, never by being removed. "Signed out three weeks
// ago" is what makes a device list and an audit trail readable at all, and 000104's ON DELETE
// RESTRICT means the spent tokens of an ended session cannot be turned back into unknown ones by
// deleting the session that issued them.
//
// # The owner is a predicate on the write, not a check above it
//
// The statement below carries `user_id = $2`. Docs/07 §3 puts every authorisation decision on the
// platform, and a decision expressed as part of the write is one no later refactor can leave out —
// a caller naming somebody else's session identifier updates nothing, whatever the handler
// believed.
//
// The blank line below keeps this a file note rather than a second package comment.

package identity

import (
	"context"
	"log/slog"

	"github.com/google/uuid"

	"github.com/DulsaraNethmin/Shipper/services/core/internal/httpx"
)

// SignOut ends the device session the caller's access token was issued against (SHIP-43).
//
// # Why it ends this session and no other
//
// The identifier comes from the `sid` claim of a token this platform signed, so it names the
// device the request came from and cannot name another. "Sign out everywhere" is a different
// action with a different consequence, and it belongs to SHIP-46's device list rather than being
// something this endpoint quietly also does — a person signing out at a shared terminal must not
// lose the session on the phone in their pocket.
//
// # Why the live refresh token hash stays where it is
//
// SHIP-40's invariant is that a hash is live in device_sessions or spent in consumed_refresh_tokens
// and never both, and moving it here would put one hash in both places for no gain: rotation
// checks revoked_at before it checks anything else, so the token the device is still holding buys
// nothing either way.
//
// # Why an unknown session is not an error
//
// The client is discarding its tokens whatever this returns, so a sign-out that failed would only
// leave the person on a screen they cannot get past. A session already revoked, and an identifier
// naming nothing, are both simply the state the caller asked for.
func (s *Service) SignOut(ctx context.Context, userID, sessionID uuid.UUID) error {
	if s.pool == nil {
		return errUnavailable
	}

	ended, err := s.store.revokeOwnDeviceSession(
		ctx, s.pool, sessionID, userID, s.clock.Now().UTC(), revokedReasonSignedOut)
	if err != nil {
		return err
	}

	// Logged at debug rather than info: signing out is ordinary. What is worth having is the
	// distinction, because "the session was already over" is the shape a replayed or forged
	// sign-out takes and there is otherwise nothing to see it in.
	httpx.LoggerFrom(ctx).LogAttrs(ctx, slog.LevelDebug, "device session signed out",
		slog.String("session_id", sessionID.String()),
		slog.Bool("was_live", ended))

	return nil
}
