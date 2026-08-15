package main

import (
	"context"
	"fmt"

	"github.com/google/uuid"

	"github.com/DulsaraNethmin/Shipper/services/core/internal/db"
	"github.com/DulsaraNethmin/Shipper/services/core/internal/notifications"
)

// deviceSessionLookup implements notifications.Sessions by reading identity's device_sessions
// (SHIP-140).
//
// # Why this query is here and not in the domain
//
// `device_sessions` belongs to internal/identity, in identity's migration block, and
// `notifications` may not import that domain — the boundary lint refuses it in both directions.
// The composition root is where a dependency between two domains is visible to anybody reading how
// the service is wired, which is the same argument [jobPartiesLookup] makes two files over and the
// same one cmd/api's SHIP-113 and SHIP-117 lookups make.
//
// **This is the fourth instance of the arrangement and it is written the same way deliberately.** A
// reader who has met one has met all four.
//
// # What "live" means, and why it is two conditions rather than one
//
// A session is usable when it has not been revoked *and* its refresh token has not lapsed. Both are
// needed and neither implies the other:
//
//   - revoked_at is set by signing out (SHIP-43), by revoking a device from the device list
//     (SHIP-46), and by reuse detection (SHIP-40). That is the condition SHIP-140's *Done when*
//     names — "clear on sign-out" — and it is the one a person causes.
//   - refresh_token_expires_at is the sliding thirty-day window 000103 added. A handset that has
//     not been opened for a month cannot refresh, so its owner is signed out in every sense that
//     matters to them; continuing to push to it would be notifying a device that can no longer
//     open the job the notification points at.
//
// # No lock, and it reads inside the consumer's transaction
//
// It takes a db.Runner so the read runs inside the pass that is writing the notification rows
// (Docs/10 §3.2). Nothing here decides anything about a session: a device signed out in the instant
// between this read and that write simply means one push is sent that need not have been, which is
// the same tolerance jobPartiesLookup's comment records for an award.
type deviceSessionLookup struct{}

// LiveSessions returns the subset of ids whose sessions are usable.
//
// A session that does not exist is simply absent from the answer, which is the same outcome as
// revoked and is the outcome this domain wants: do not address it. There is no error for an unknown
// identifier — a device token whose session row has been pruned is a device nobody should push to,
// not a failure that should stop the topic.
func (deviceSessionLookup) LiveSessions(
	ctx context.Context, r db.Runner, ids []uuid.UUID,
) (map[uuid.UUID]bool, error) {
	if len(ids) == 0 {
		return nil, nil
	}

	const q = `
		SELECT id
		FROM device_sessions
		WHERE id = ANY($1)
		  AND revoked_at IS NULL
		  AND refresh_token_expires_at > now()`

	rows, err := r.Query(ctx, q, ids)
	if err != nil {
		return nil, fmt.Errorf("cmd/notifier: reading which of %d device session(s) are live: %w",
			len(ids), err)
	}
	defer rows.Close()

	live := make(map[uuid.UUID]bool, len(ids))
	for rows.Next() {
		var id uuid.UUID
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("cmd/notifier: reading a device session: %w", err)
		}
		live[id] = true
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("cmd/notifier: reading device sessions: %w", err)
	}
	return live, nil
}

// Compile-time proof that the adapter satisfies the port the domain declared, which is the only
// place in the build where that can be established.
var _ notifications.Sessions = deviceSessionLookup{}
