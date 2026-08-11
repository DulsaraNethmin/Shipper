// SHIP-43 and SHIP-46: listing device sessions, and ending one.
//
// Ending a session is one operation seen from two places. Signing out ends the session the caller
// is holding; revoking from the device list ends one they are not. The reasons are recorded
// distinctly — `signed_out` and `revoked_by_owner` — because a support conversation and SHIP-149's
// audit both ask which of the two happened, and a single "ended by the user" would answer neither.
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
// Every statement below carries `user_id`. Docs/07 §3 puts every authorisation decision on the
// platform, and a decision expressed as part of the query is one no later refactor can leave out —
// a caller naming somebody else's session identifier reads nothing and updates nothing, whatever
// the handler believed.
//
// The blank line below keeps this a file note rather than a second package comment.

package identity

import (
	"context"
	"log/slog"
	"time"

	"github.com/google/uuid"

	"github.com/DulsaraNethmin/Shipper/services/core/internal/httpx"
)

// SignOut ends the device session the caller's access token was issued against (SHIP-43).
//
// # Why it ends this session and no other
//
// The identifier comes from the `sid` claim of a token this platform signed, so it names the
// device the request came from and cannot name another. "Sign out everywhere" is a different
// action with a different consequence, and it is [Service.RevokeDevice] applied per row rather than
// something this endpoint should quietly also do — a person signing out at a shared terminal must
// not lose the session on the phone in their pocket.
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

// maxDevicesListed bounds the device list (SHIP-46).
//
// It is a bound on the *response*, not paging. A person has a handful of devices; what this
// refuses is the pathological account, which is reachable because every sign-in creates a session
// and nothing stops a client signing in a thousand times instead of refreshing. Docs/10 §4.5's
// keyset paging belongs to internal/pagination, which is registered in internal/boundaries and not
// yet written — and an account in that state has a support conversation rather than a paging
// problem, so `has_more` reporting the truncation honestly is the whole of what is owed here.
const maxDevicesListed = 100

// Device is one of the caller's live device sessions, as its owner sees it (SHIP-46).
//
// The refresh token hash is not here, in any form. Nor is the expiry: a session is on this list
// because it has not lapsed, and an instant that only ever gets compared with now invites a client
// to start doing arithmetic on it.
type Device struct {
	ID          uuid.UUID
	DeviceLabel string

	CreatedAt  time.Time
	LastSeenAt time.Time

	// Current marks the session the request was made from, so a client can label it and warn
	// before revoking it. It is derived from the caller's own token rather than from anything
	// stored, because "current" is a property of the request and not of the row.
	Current bool
}

// Devices lists the sessions that can currently act as the account, newest use first (SHIP-46).
//
// # What is deliberately not listed
//
// Revoked sessions, and sessions whose refresh token has lapsed. The list answers "which devices
// can act as me, and let me stop one", and neither kind can do the first or needs the second —
// offering them invites revoking something already dead, and pads a list whose whole value is that
// an unrecognised row stands out. The rows themselves stay: they are marked, not deleted.
//
// # What "last seen" means
//
// The last time the device rotated its refresh token, which happens at least every fifteen minutes
// of use. Reading this list deliberately does not update it: a read is not activity worth
// recording, and a display column that every read writes to is how a session's apparent age stops
// meaning anything. 000103's header makes the stronger version of the same point — nothing derived
// from last_seen_at may extend a credential.
//
// The bool reports that the list was truncated at [maxDevicesListed]; see the note there.
func (s *Service) Devices(ctx context.Context, userID, currentSessionID uuid.UUID) ([]Device, bool, error) {
	if s.pool == nil {
		return nil, false, errUnavailable
	}

	// One more than the bound, so truncation is observed rather than inferred from a full page.
	rows, err := s.store.liveDeviceSessionsByUser(
		ctx, s.pool, userID, s.clock.Now().UTC(), maxDevicesListed+1)
	if err != nil {
		return nil, false, err
	}

	truncated := len(rows) > maxDevicesListed
	if truncated {
		rows = rows[:maxDevicesListed]
	}

	devices := make([]Device, 0, len(rows))
	for _, row := range rows {
		devices = append(devices, Device{
			ID:          row.ID,
			DeviceLabel: row.DeviceLabel,
			CreatedAt:   row.CreatedAt,
			LastSeenAt:  row.LastSeenAt,
			Current:     row.ID == currentSessionID,
		})
	}
	return devices, truncated, nil
}

// RevokeDevice ends one of the caller's sessions, named by identifier (SHIP-46).
//
// # Why a session that is not the caller's is a 404
//
// [ErrSessionNotFound] covers both "no such session" and "not yours", and the two must stay
// indistinguishable: answering 403 to the second would turn this endpoint into a way of finding
// out which identifiers name real sessions, which is the same disclosure sign-in refuses to make
// about addresses.
//
// # Why revoking an already-revoked session succeeds
//
// The caller asked for a state and the state holds. Reporting a failure would make an honest
// retry — the one the idempotency middleware exists for, on a request whose response was lost —
// look like a mistake.
//
// # Why the current session may be revoked from here
//
// It is the same action a person takes when they sign out, and refusing it would mean a device
// list where exactly one row cannot be acted on. The reason recorded differs, which is the point
// of having two: `revoked_by_owner` says somebody went to their device list and ended it.
func (s *Service) RevokeDevice(ctx context.Context, userID, sessionID uuid.UUID) error {
	if s.pool == nil {
		return errUnavailable
	}

	owned, err := s.store.deviceSessionOwnedBy(ctx, s.pool, sessionID, userID)
	if err != nil {
		return err
	}
	if !owned {
		return ErrSessionNotFound
	}

	// Owner-scoped again rather than trusting the read above. The two statements are not in one
	// transaction, and they do not need to be — the predicate is what decides, and a row cannot
	// change owner: device_sessions.user_id has no path that updates it.
	ended, err := s.store.revokeOwnDeviceSession(
		ctx, s.pool, sessionID, userID, s.clock.Now().UTC(), revokedReasonByOwner)
	if err != nil {
		return err
	}

	httpx.LoggerFrom(ctx).LogAttrs(ctx, slog.LevelInfo, "device session revoked by its owner",
		slog.String("session_id", sessionID.String()),
		slog.Bool("was_live", ended))

	return nil
}
