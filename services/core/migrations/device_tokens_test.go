package migrations_test

import (
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/DulsaraNethmin/Shipper/services/core/internal/notifications"
	"github.com/DulsaraNethmin/Shipper/services/core/internal/testsupport/pgtest"
	"github.com/DulsaraNethmin/Shipper/services/core/migrations"
)

// 000701 and 000702 — the device token registry (SHIP-140) and the fourth notification status
// (SHIP-139).
//
// What is tested here is what only the schema can be wrong about: the platform enumeration against
// its Go list in both directions (Docs/10 §3.4), the two partial unique indexes that make
// registration idempotent, the split of the notification deduplication rule, and the status a
// rejected push leaves behind. The registration logic and the rejection path are
// internal/notifications' and are tested there.

// tokenSession inserts a signed-in device a token can hang off.
func tokenSession(t *testing.T, pool *pgxpool.Pool, userID uuid.UUID, label string) uuid.UUID {
	t.Helper()

	id := uuid.Must(uuid.NewV7())
	if _, err := pool.Exec(t.Context(), `
		INSERT INTO device_sessions
		    (id, user_id, refresh_token_hash, device_label, refresh_token_expires_at)
		VALUES ($1, $2, $3, $4, now() + interval '30 days')`,
		id, userID, uuid.NewString(), label); err != nil {
		t.Fatalf("inserting the device session %q: %v", label, err)
	}
	return id
}

// insertDeviceToken writes one row directly, bypassing the domain, so the constraint is what is
// being tested rather than the code that usually satisfies it.
func insertDeviceToken(
	t *testing.T, pool *pgxpool.Pool, userID, sessionID uuid.UUID, platform, token string,
) error {
	t.Helper()

	_, err := pool.Exec(t.Context(), `
		INSERT INTO device_tokens
		    (id, user_id, device_session_id, platform, token, registered_at)
		VALUES ($1, $2, $3, $4, $5, now())`,
		uuid.Must(uuid.NewV7()), userID, sessionID, platform, token)
	return err
}

// TestDeviceTokenPlatformConstraintMatchesTheGoConstants is Docs/10 §3.4's pairing.
//
// A platform the database accepts and Go has no constant for is a row no handler could have
// written; one Go knows and the database refuses is a registration that fails at the constraint
// with a 500 rather than being validated.
func TestDeviceTokenPlatformConstraintMatchesTheGoConstants(t *testing.T) {
	pool := pgtest.DB(t)

	user := notifiedUser(t, pool, "platforms@example.com", "+61400007301")
	for _, platform := range notifications.Platforms {
		session := tokenSession(t, pool, user, platform.String())
		if err := insertDeviceToken(t, pool, user, session, platform.String(),
			"platform-token-"+platform.String()); err != nil {
			t.Errorf("the database refuses %q, which internal/notifications lists as a "+
				"platform: %v", platform, err)
		}
	}

	session := tokenSession(t, pool, user, "a platform nobody ships on")
	if err := insertDeviceToken(t, pool, user, session, "windows-phone", "nope"); err == nil {
		t.Error("the database accepted a platform Go has no constant for; a row nothing " +
			"could have written is a row nothing knows how to read")
	}
}

// TestOneLiveTokenPerDeviceSession is what makes registering twice idempotent without the handler
// remembering anything.
//
// SHIP-143 registers at every launch and FCM only sometimes hands back the same token, so a device
// addressed at two tokens at once is a notification delivered twice — for as long as both tokens
// live, which is forever.
func TestOneLiveTokenPerDeviceSession(t *testing.T) {
	pool := pgtest.DB(t)

	user := notifiedUser(t, pool, "one-per-session@example.com", "+61400007302")
	session := tokenSession(t, pool, user, "one handset")

	if err := insertDeviceToken(t, pool, user, session, "ios", "first"); err != nil {
		t.Fatalf("the first token was refused: %v", err)
	}
	if err := insertDeviceToken(t, pool, user, session, "ios", "second"); err == nil {
		t.Error("one device session holds two live tokens; every notification to it is two")
	}

	// Partial on revoked_at, so a device's token history stays readable and a replacement is
	// accepted once the previous one is marked.
	if _, err := pool.Exec(t.Context(),
		`UPDATE device_tokens SET revoked_at = now(), revoked_reason = 'replaced'
		  WHERE device_session_id = $1`, session); err != nil {
		t.Fatalf("revoking the first token: %v", err)
	}
	if err := insertDeviceToken(t, pool, user, session, "ios", "second"); err != nil {
		t.Errorf("a replacement was refused after the previous token was revoked: %v", err)
	}
}

// TestATokenIsLiveInOnePlaceAcrossEveryAccount.
//
// FCM reissues a token to a handset restored from another device's backup, so the same string can
// arrive from a second session belonging to somebody else. Without this index a push meant for the
// previous owner reaches the new one — which is a privacy failure rather than a duplicate.
func TestATokenIsLiveInOnePlaceAcrossEveryAccount(t *testing.T) {
	pool := pgtest.DB(t)

	previous := notifiedUser(t, pool, "restored-from@example.com", "+61400007303")
	current := notifiedUser(t, pool, "restored-to@example.com", "+61400007304")

	if err := insertDeviceToken(t, pool, previous,
		tokenSession(t, pool, previous, "old"), "ios", "shared-token"); err != nil {
		t.Fatalf("the first registration was refused: %v", err)
	}
	if err := insertDeviceToken(t, pool, current,
		tokenSession(t, pool, current, "new"), "ios", "shared-token"); err == nil {
		t.Error("one token is live against two accounts; a notification for one of them " +
			"reaches the other's handset")
	}
}

// TestADeviceTokenNeedsARealSessionAndARealAccount. Docs/10 §3.3 makes both foreign keys RESTRICT:
// SHIP-171 pseudonymises rather than deletes, so a cascade would be a path by which a device
// disappears without anybody having signed out.
func TestADeviceTokenNeedsARealSessionAndARealAccount(t *testing.T) {
	pool := pgtest.DB(t)

	user := notifiedUser(t, pool, "real-session@example.com", "+61400007305")
	session := tokenSession(t, pool, user, "a phone")

	if err := insertDeviceToken(t, pool, user, uuid.Must(uuid.NewV7()), "ios", "no-session"); err == nil {
		t.Error("a token was registered against a device session that does not exist")
	}
	if err := insertDeviceToken(t, pool, uuid.Must(uuid.NewV7()), session, "ios", "no-user"); err == nil {
		t.Error("a token was registered against an account that does not exist")
	}

	if err := insertDeviceToken(t, pool, user, session, "ios", "restrict"); err != nil {
		t.Fatalf("registering: %v", err)
	}
	if _, err := pool.Exec(t.Context(), `DELETE FROM device_sessions WHERE id = $1`, session); err == nil {
		t.Error("a device session with a live token was deleted; the token would have gone " +
			"with it and nothing would say the device had ever been signed out")
	}
}

// TestARevokedDeviceTokenSaysWhy pairs the two columns, which is 000104's arrangement and for the
// same reason: a row that says it was revoked for no reason is a state no code writes and every
// report has to handle.
func TestARevokedDeviceTokenSaysWhy(t *testing.T) {
	pool := pgtest.DB(t)

	user := notifiedUser(t, pool, "revoked-why@example.com", "+61400007306")
	session := tokenSession(t, pool, user, "a phone")
	if err := insertDeviceToken(t, pool, user, session, "android", "why"); err != nil {
		t.Fatalf("registering: %v", err)
	}

	if _, err := pool.Exec(t.Context(),
		`UPDATE device_tokens SET revoked_at = now() WHERE token = 'why'`); err == nil {
		t.Error("a token was revoked with no reason")
	}
	if _, err := pool.Exec(t.Context(),
		`UPDATE device_tokens SET revoked_reason = 'deregistered' WHERE token = 'why'`); err == nil {
		t.Error("a live token carries a revocation reason")
	}
	if _, err := pool.Exec(t.Context(),
		`UPDATE device_tokens SET revoked_at = now(), revoked_reason = 'because' WHERE token = 'why'`); err == nil {
		t.Error("a reason nothing writes was accepted")
	}
}

// TestOnePushPerDevicePerEvent is 000701's split of the deduplication rule, and it is the reason
// the rule had to be split at all.
//
// 000700's uq_notifications_event_recipient_channel was right while every channel had one address
// per person. A customer signed in on a phone and a tablet has two, and both have to be told —
// under the old index the second row was refused and the tablet was never notified.
func TestOnePushPerDevicePerEvent(t *testing.T) {
	pool := pgtest.DB(t)

	recipient := notifiedUser(t, pool, "per-device@example.com", "+61400007307")
	event := uuid.Must(uuid.NewV7())

	if err := insertPush(t, pool, event, recipient, "phone-token"); err != nil {
		t.Fatalf("the first handset was refused: %v", err)
	}
	if err := insertPush(t, pool, event, recipient, "tablet-token"); err != nil {
		t.Errorf("the second handset was refused: %v — one person's other device is not a "+
			"duplicate, it is the other half of the notification", err)
	}
	if err := insertPush(t, pool, event, recipient, "phone-token"); err == nil {
		t.Error("the same event reached the same handset twice; a redelivered event would " +
			"push a second copy")
	}
}

// TestEmailStillDeduplicatesOnTheChannelAlone is the other half, and the reason the fix was two
// partial indexes rather than adding `address` to one.
//
// An email address is resolved from `users` when the row is written. Deduplicating email on the
// address would mean a person who changed theirs between two deliveries of one event received it
// twice, which is precisely the duplicate the index exists to prevent.
func TestEmailStillDeduplicatesOnTheChannelAlone(t *testing.T) {
	pool := pgtest.DB(t)

	recipient := notifiedUser(t, pool, "still-once@example.com", "+61400007308")
	event := uuid.Must(uuid.NewV7())

	if err := insertNotificationAt(t, pool, event, recipient, "email", "before@example.com"); err != nil {
		t.Fatalf("the first email was refused: %v", err)
	}
	if err := insertNotificationAt(t, pool, event, recipient, "email", "after@example.com"); err == nil {
		t.Error("one event wrote two emails to one person because their address changed; " +
			"deduplicating email on the address is exactly what 000701 refused to do")
	}
}

// TestARejectedPushLeavesATerminalStatus is 000702, and the whole of why it exists.
//
// internal/platform/push/doc.go: a rejected token is normal traffic. `failed` is not terminal in
// this schema — the next pass claims it again — so a dead handset would be retried on every pass
// forever and counted into SHIP-176's alerting. `undeliverable` is terminal and truthful, and
// ck_notifications_sent_at keeps it from carrying an instant.
func TestARejectedPushLeavesATerminalStatus(t *testing.T) {
	pool := pgtest.DB(t)

	recipient := notifiedUser(t, pool, "terminal@example.com", "+61400007309")
	event := uuid.Must(uuid.NewV7())
	if err := insertPush(t, pool, event, recipient, "dead"); err != nil {
		t.Fatalf("inserting: %v", err)
	}

	if _, err := pool.Exec(t.Context(),
		`UPDATE notifications SET status = 'undeliverable' WHERE event_id = $1`, event); err != nil {
		t.Fatalf("the database refuses the status the dispatcher writes: %v", err)
	}
	if _, err := pool.Exec(t.Context(),
		`UPDATE notifications SET sent_at = now() WHERE event_id = $1`, event); err == nil {
		t.Error("an undeliverable notification carries a sent instant, which is what a " +
			"delivered one looks like in every report that counts by status")
	}

	// The claim's predicate and its partial index have to agree, or the claim stops being
	// served by it — a sequential scan over every notification the platform has ever sent.
	//
	// PostgreSQL rewrites `NOT IN (…)` as `<> ALL (ARRAY[…])`, so the definition is read for
	// the two status names rather than for the operator somebody typed.
	var indexdef string
	if err := pool.QueryRow(t.Context(), `
		SELECT indexdef FROM pg_indexes
		WHERE indexname = 'idx_notifications_undelivered'`).Scan(&indexdef); err != nil {
		t.Fatalf("reading the claim index: %v", err)
	}
	for _, terminal := range []string{"'sent'", "'undeliverable'"} {
		if !strings.Contains(indexdef, terminal) {
			t.Errorf("idx_notifications_undelivered does not exclude %s, so the dispatcher's "+
				"claim is not served by it: %s", terminal, indexdef)
		}
	}
}

// TestDeviceTokensMigrationsAreInTheNotificationsBlock is the block allocation, which is what stops
// two branches drawing the same migration number.
func TestDeviceTokensMigrationsAreInTheNotificationsBlock(t *testing.T) {
	for _, version := range []int{701, 702} {
		block, ok := migrations.BlockContaining(version)
		if !ok {
			t.Fatalf("version %d falls in no reserved block", version)
		}
		if block.Domain != "notifications" {
			t.Errorf("version %d is in the %q block, want notifications", version, block.Domain)
		}
	}
}

// insertPush writes one push notification row at a given device token.
func insertPush(t *testing.T, pool *pgxpool.Pool, eventID, recipient uuid.UUID, address string) error {
	t.Helper()
	return insertNotificationAt(t, pool, eventID, recipient, "push", address)
}

func insertNotificationAt(
	t *testing.T, pool *pgxpool.Pool, eventID, recipient uuid.UUID, channel, address string,
) error {
	t.Helper()

	_, err := pool.Exec(t.Context(), `
		INSERT INTO notifications
		    (id, event_id, event_type, job_id, recipient_id, channel, category, essential,
		     address, subject, body)
		VALUES ($1, $2, 'bid.placed', $3, $4, $5, 'bidding', true, $6, 's', 'b')`,
		uuid.Must(uuid.NewV7()), eventID, uuid.Must(uuid.NewV7()), recipient, channel, address)
	return err
}
