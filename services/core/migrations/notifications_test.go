package migrations_test

import (
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/DulsaraNethmin/Shipper/services/core/internal/notifications"
	"github.com/DulsaraNethmin/Shipper/services/core/internal/testsupport/pgtest"
	"github.com/DulsaraNethmin/Shipper/services/core/migrations"
)

// 000700 — the notifications table (SHIP-137).
//
// What is tested here is what only the schema can be wrong about: the two enumerations against
// their Go lists in both directions (Docs/10 §3.4), and the unique index that is the whole of the
// consumer's idempotence. The routing, the resolution and the dispatch are internal/notifications'
// and are tested there.

// notifiedUser inserts an account a notification can be addressed to.
func notifiedUser(t *testing.T, pool *pgxpool.Pool, email, phone string) uuid.UUID {
	t.Helper()

	id := uuid.Must(uuid.NewV7())
	if _, err := pool.Exec(t.Context(),
		`INSERT INTO users (id, email, phone, password_hash, role) VALUES ($1, $2, $3, 'x', 'customer')`,
		id, email, phone); err != nil {
		t.Fatalf("inserting %s: %v", email, err)
	}
	return id
}

// insertNotification writes one row directly, bypassing the domain, so the constraint is what is
// being tested rather than the code that usually satisfies it.
func insertNotification(
	t *testing.T, pool *pgxpool.Pool, eventID, recipient uuid.UUID, channel string,
) error {
	t.Helper()

	_, err := pool.Exec(t.Context(), `
		INSERT INTO notifications
		    (id, event_id, event_type, job_id, recipient_id, channel, category, essential,
		     address, subject, body)
		VALUES ($1, $2, 'bid.placed', $3, $4, $5, 'bidding', true, 'x@example.com', 's', 'b')`,
		uuid.Must(uuid.NewV7()), eventID, uuid.Must(uuid.NewV7()), recipient, channel)
	return err
}

// TestNotificationChannelConstraintMatchesTheGoConstants is Docs/10 §3.4's pairing.
//
// A channel the database accepts and Go has no sender case for is a row nothing can ever complete;
// a channel Go knows and the database refuses is one no rule can ever write. Neither is visible
// from either side alone.
func TestNotificationChannelConstraintMatchesTheGoConstants(t *testing.T) {
	pool := pgtest.DB(t)

	inDatabase := constraintLiterals(t, pool, "ck_notifications_channel")

	for _, channel := range notifications.Channels {
		if !inDatabase[channel.String()] {
			t.Errorf("notifications.Channels has %q and ck_notifications_channel refuses it", channel)
		}
		delete(inDatabase, channel.String())
	}
	for leftover := range inDatabase {
		t.Errorf("ck_notifications_channel accepts %q and notifications.Channels has no such "+
			"channel, so a row could be written that nothing knows how to send", leftover)
	}
}

// TestNotificationCategoryConstraintMatchesTheGoConstants is the same pairing for the other
// enumeration, and it is the one SHIP-142 will depend on: a category the database accepts and Go
// does not know is one a preference screen can never offer to mute.
func TestNotificationCategoryConstraintMatchesTheGoConstants(t *testing.T) {
	pool := pgtest.DB(t)

	inDatabase := constraintLiterals(t, pool, "ck_notifications_category")

	for _, category := range notifications.Categories {
		if !inDatabase[category.String()] {
			t.Errorf("notifications.Categories has %q and ck_notifications_category refuses it", category)
		}
		delete(inDatabase, category.String())
	}
	for leftover := range inDatabase {
		t.Errorf("ck_notifications_category accepts %q and notifications.Categories has no such "+
			"category", leftover)
	}
}

// TestOneNotificationPerRecipientPerChannelPerEvent is the index the whole domain rests on.
//
// Docs/06 §4.0 makes delivery at-least-once, so a consumer will see the same event twice — and this
// is what makes the second sighting write nothing. Tested here rather than only through the
// consumer because Docs/06 §4.1's argument applies exactly: a mock happily accepts the write the
// real constraint exists to reject.
func TestOneNotificationPerRecipientPerChannelPerEvent(t *testing.T) {
	pool := pgtest.DB(t)

	recipient := notifiedUser(t, pool, "dupe@example.com", "+61400007001")
	other := notifiedUser(t, pool, "dupe-other@example.com", "+61400007002")
	event := uuid.Must(uuid.NewV7())

	if err := insertNotification(t, pool, event, recipient, "email"); err != nil {
		t.Fatalf("the first notification was refused: %v", err)
	}
	if err := insertNotification(t, pool, event, recipient, "email"); err == nil {
		t.Error("the same event, recipient and channel were accepted twice; a redelivered " +
			"event would send a second copy of every message")
	}

	// The three things that legitimately differ. Each is a separate notification and none of
	// them is a duplicate of another.
	if err := insertNotification(t, pool, event, recipient, "sms"); err != nil {
		t.Errorf("the same event to the same person on another channel was refused: %v", err)
	}
	if err := insertNotification(t, pool, event, other, "email"); err != nil {
		t.Errorf("the same event to another person was refused: %v", err)
	}
	if err := insertNotification(t, pool, uuid.Must(uuid.NewV7()), recipient, "email"); err != nil {
		t.Errorf("another event to the same person was refused: %v", err)
	}
}

// TestASentNotificationAlwaysHasAnInstant keeps a state out of the table that would look exactly
// like a successful send in every report that counts by status.
func TestASentNotificationAlwaysHasAnInstant(t *testing.T) {
	pool := pgtest.DB(t)

	recipient := notifiedUser(t, pool, "sent-at@example.com", "+61400007003")
	event := uuid.Must(uuid.NewV7())
	if err := insertNotification(t, pool, event, recipient, "email"); err != nil {
		t.Fatalf("inserting the notification: %v", err)
	}

	if _, err := pool.Exec(t.Context(),
		`UPDATE notifications SET status = 'sent' WHERE event_id = $1`, event); err == nil {
		t.Error("a notification was marked sent with no instant; ck_notifications_sent_at is " +
			"what stops the dispatcher forgetting one of two assignments")
	}
	if _, err := pool.Exec(t.Context(),
		`UPDATE notifications SET status = 'failed', sent_at = now() WHERE event_id = $1`, event); err == nil {
		t.Error("a notification was marked failed and carries a sent instant")
	}
}

// TestANotificationNeedsARealRecipient is the one foreign key on this table.
//
// Everything else it could point at — the event, the job — is deliberately unconstrained, because a
// notification must outlive both. The recipient is different: a message addressed to an account
// that does not exist is a bug rather than a record worth keeping.
func TestANotificationNeedsARealRecipient(t *testing.T) {
	pool := pgtest.DB(t)

	if err := insertNotification(t, pool, uuid.Must(uuid.NewV7()), uuid.Must(uuid.NewV7()), "email"); err == nil {
		t.Error("a notification was accepted for an account that does not exist")
	}
}

// TestNotificationsMigrationIsInTheNotificationsBlock is the block allocation, which is what stops
// two branches drawing the same migration number.
func TestNotificationsMigrationIsInTheNotificationsBlock(t *testing.T) {
	block, ok := migrations.BlockContaining(700)
	if !ok {
		t.Fatal("version 700 falls in no reserved block")
	}
	if block.Domain != "notifications" {
		t.Errorf("version 700 is in the %q block, want notifications", block.Domain)
	}
}
