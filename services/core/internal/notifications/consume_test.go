package notifications

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/DulsaraNethmin/Shipper/services/core/internal/clock"
	"github.com/DulsaraNethmin/Shipper/services/core/internal/db"
	"github.com/DulsaraNethmin/Shipper/services/core/internal/events"
	"github.com/DulsaraNethmin/Shipper/services/core/internal/testsupport/pgtest"
)

// SHIP-137's consumer half, against a real PostgreSQL.
//
// It has to be real, and for the usual reason plus one of its own. Docs/06 §4.1: "a mock happily
// accepts a write that the actual constraint would reject" — and here the constraint *is* the
// feature. uq_notifications_event_recipient_channel is the whole of the idempotence this domain
// claims, so a test that deduplicated against a fake would be testing the fake.

// testInstant is the service clock, stopped.
//
// Nothing in this domain measures an interval — a notification has one clock, the platform's, and
// 000700's sent_at is the only column it fills — so the fixed instant is for legibility rather than
// to avoid the two-clock trap that the seventy-two hour sweep has to worry about.
var testInstant = time.Date(2026, 8, 14, 9, 0, 0, 0, time.UTC)

// stubParties answers what a job's parties are without a jobs table.
//
// The real implementation is cmd/notifier's jobPartiesLookup and is a join across two other
// domains' tables; the port exists precisely so this package does not have to know that. What the
// stub cannot show is that the query is right, which is why the verify section runs the real one.
type stubParties struct {
	customer uuid.UUID
	provider uuid.UUID
	missing  bool
	calls    int
}

func (s *stubParties) PartiesOn(context.Context, db.Runner, uuid.UUID) (uuid.UUID, uuid.UUID, bool, error) {
	s.calls++
	if s.missing {
		return uuid.Nil, uuid.Nil, false, nil
	}
	return s.customer, s.provider, true, nil
}

// recordingSender counts what it was asked to send, and can be told to fail.
type recordingSender struct {
	sent []string
	fail error
}

func (r *recordingSender) Send(_ context.Context, to, _, body string) error {
	if r.fail != nil {
		return r.fail
	}
	r.sent = append(r.sent, to+"|"+body)
	return nil
}

// smsSender is the two-argument shape, so that "dispatches per channel" is more than one channel.
type smsSender struct {
	sent []string
	fail error
}

func (s *smsSender) Send(_ context.Context, to, body string) error {
	if s.fail != nil {
		return s.fail
	}
	s.sent = append(s.sent, to+"|"+body)
	return nil
}

// newUser inserts an account with an address, which is the only thing this domain reads out of
// `users`.
func newUser(t *testing.T, pool *pgxpool.Pool, email, phone, role string) uuid.UUID {
	t.Helper()

	id := uuid.Must(uuid.NewV7())
	if _, err := pool.Exec(t.Context(),
		`INSERT INTO users (id, email, phone, password_hash, role) VALUES ($1, $2, $3, 'x', $4)`,
		id, email, phone, role); err != nil {
		t.Fatalf("inserting %s: %v", email, err)
	}
	return id
}

// envelopeOf builds a message of the shape the publisher puts on the topic.
func envelopeOf(t *testing.T, eventType string, payload any) events.Envelope {
	t.Helper()

	body, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshalling the payload: %v", err)
	}
	return events.Envelope{
		ID:            uuid.Must(uuid.NewV7()),
		Type:          eventType,
		SchemaVersion: 1,
		AggregateType: "job",
		AggregateID:   uuid.Must(uuid.NewV7()),
		OccurredAt:    testInstant,
		Payload:       body,
	}
}

// consume runs one event through the service in a transaction, which is what Consume requires.
func consume(t *testing.T, pool *pgxpool.Pool, s *Service, env events.Envelope) (int, error) {
	t.Helper()

	var written int
	err := db.InTx(t.Context(), pool, func(ctx context.Context, r db.Runner) error {
		var err error
		written, err = s.Consume(ctx, r, env)
		return err
	})
	return written, err
}

// rowsFor is every notification written for one event, oldest first.
func rowsFor(t *testing.T, pool *pgxpool.Pool, eventID uuid.UUID) []Notification {
	t.Helper()

	rows, err := pool.Query(t.Context(), `
		SELECT id, event_id, event_type, job_id, recipient_id, channel, category, essential,
		       address, subject, body, status, attempts
		FROM notifications WHERE event_id = $1 ORDER BY created_at, id`, eventID)
	if err != nil {
		t.Fatalf("reading the notifications for %s: %v", eventID, err)
	}
	defer rows.Close()

	var out []Notification
	for rows.Next() {
		var n Notification
		if err := rows.Scan(&n.ID, &n.EventID, &n.EventType, &n.JobID, &n.Recipient, &n.Channel,
			&n.Category, &n.Essential, &n.Address, &n.Subject, &n.Body, &n.Status,
			&n.Attempts); err != nil {
			t.Fatalf("scanning a notification: %v", err)
		}
		out = append(out, n)
	}
	return out
}

// TestAnEventBecomesOneRowPerRecipientPerChannel is the first half of SHIP-137's *Done when*:
// "consumer reads events, resolves recipients".
//
// bid.accepted, because it is the one event with two audiences that need different resolutions —
// the customer comes from the Parties port and the provider comes out of the payload — so it
// exercises both paths in one pass.
func TestAnEventBecomesOneRowPerRecipientPerChannel(t *testing.T) {
	pool := pgtest.DB(t)

	customer := newUser(t, pool, "consume-customer@example.com", "+61400007101", "customer")
	provider := newUser(t, pool, "consume-provider@example.com", "+61400007102", "provider")
	parties := &stubParties{customer: customer, provider: provider}
	service := NewService(parties, clock.NewFixed(testInstant), Senders{})

	job := uuid.Must(uuid.NewV7())
	env := envelopeOf(t, "bid.accepted", map[string]any{
		"job_id":      job.String(),
		"bid_id":      uuid.Must(uuid.NewV7()).String(),
		"customer_id": customer.String(),
		"provider_id": provider.String(),
	})

	written, err := consume(t, pool, service, env)
	if err != nil {
		t.Fatalf("consuming the award: %v", err)
	}
	if written != 2 {
		t.Fatalf("the award wrote %d notifications, want one each for the customer and the "+
			"awarded provider", written)
	}

	rows := rowsFor(t, pool, env.ID)
	if len(rows) != 2 {
		t.Fatalf("the table holds %d rows for the event, want 2", len(rows))
	}
	for _, row := range rows {
		switch {
		case row.Channel != ChannelEmail:
			t.Errorf("a row is on %s; email is the only channel with a sender today", row.Channel)
		case row.Category != CategoryAward:
			t.Errorf("the award is categorised %s, want award", row.Category)
		case !row.Essential:
			t.Errorf("the award is mutable; Docs/01 §4.5 lists 'bid accepted' as essential")
		case row.JobID != job:
			t.Errorf("the row names job %s, want %s", row.JobID, job)
		case row.Status != "pending":
			t.Errorf("a freshly written notification is %s, want pending — nothing has been "+
				"sent yet", row.Status)
		}
	}
	if rows[0].Address != "consume-customer@example.com" || rows[1].Address != "consume-provider@example.com" {
		t.Errorf("the addresses are %q and %q, want the customer's then the provider's",
			rows[0].Address, rows[1].Address)
	}
}

// TestTheSameEventTwiceWritesNothingTheSecondTime is the rule doc.go states and the one this whole
// domain is shaped around: "delivery is at least once; every consumer is idempotent, because the
// alternative to a duplicate notification is a missing one".
//
// It is not a caveat about Kafka. cmd/worker/outbox.go names the exact window — publish succeeds,
// commit fails, the next pass republishes — so a duplicate is guaranteed rather than possible, and
// a consumer with no test for this is known-wrong on a schedule.
func TestTheSameEventTwiceWritesNothingTheSecondTime(t *testing.T) {
	pool := pgtest.DB(t)

	customer := newUser(t, pool, "twice-customer@example.com", "+61400007103", "customer")
	service := NewService(&stubParties{customer: customer}, clock.NewFixed(testInstant), Senders{})

	env := envelopeOf(t, "bid.placed", map[string]any{
		"job_id":      uuid.Must(uuid.NewV7()).String(),
		"provider_id": uuid.Must(uuid.NewV7()).String(),
	})

	first, err := consume(t, pool, service, env)
	if err != nil {
		t.Fatalf("the first delivery: %v", err)
	}
	if first != 1 {
		t.Fatalf("the first delivery wrote %d notifications, want 1", first)
	}

	second, err := consume(t, pool, service, env)
	if err != nil {
		t.Fatalf("the second delivery failed rather than being absorbed: %v", err)
	}
	if second != 0 {
		t.Errorf("the second delivery wrote %d notifications; at-least-once delivery makes a "+
			"redelivery certain, and each one is a second email to the same person", second)
	}

	if rows := rowsFor(t, pool, env.ID); len(rows) != 1 {
		t.Errorf("the table holds %d rows for one event, want 1", len(rows))
	}
}

// TestARuleThatTellsNobodyWritesNothing covers the transitions something else already announced.
//
// A job reaching Awarded emits bid.accepted in the same transaction, so routing the status change
// as well would be two messages about one thing. That is a decision StatusRules records with a Why,
// and this is it holding.
func TestARuleThatTellsNobodyWritesNothing(t *testing.T) {
	pool := pgtest.DB(t)

	customer := newUser(t, pool, "nobody@example.com", "+61400007104", "customer")
	parties := &stubParties{customer: customer}
	service := NewService(parties, clock.NewFixed(testInstant), Senders{})

	env := envelopeOf(t, EventJobStatusChanged, map[string]any{
		"job_id": uuid.Must(uuid.NewV7()).String(),
		"from":   "Open",
		"to":     "Awarded",
	})

	written, err := consume(t, pool, service, env)
	if err != nil {
		t.Fatalf("consuming the status change: %v", err)
	}
	if written != 0 {
		t.Errorf("a job reaching Awarded wrote %d notifications; bid.accepted already told "+
			"both parties", written)
	}
	if parties.calls != 0 {
		t.Errorf("the parties were looked up %d times for an event that tells nobody; a rule "+
			"with no audience should cost no query", parties.calls)
	}
}

// TestAJobReachingCompletedTellsBothParties is where SHIP-119 and SHIP-137 meet.
//
// The seventy-two hour auto-complete moves a job to Completed with the platform as its actor, and
// nothing else announces that transition — so this is the one rule that turns a sweep nobody
// watches into a message somebody receives.
func TestAJobReachingCompletedTellsBothParties(t *testing.T) {
	pool := pgtest.DB(t)

	customer := newUser(t, pool, "completed-customer@example.com", "+61400007105", "customer")
	provider := newUser(t, pool, "completed-provider@example.com", "+61400007106", "provider")
	service := NewService(&stubParties{customer: customer, provider: provider},
		clock.NewFixed(testInstant), Senders{})

	env := envelopeOf(t, EventJobStatusChanged, map[string]any{
		"job_id":     uuid.Must(uuid.NewV7()).String(),
		"from":       "Delivered",
		"to":         "Completed",
		"actor_type": "system",
		"reason":     "The delivery was not disputed within 72 hours (Docs/02 §6.1).",
	})

	written, err := consume(t, pool, service, env)
	if err != nil {
		t.Fatalf("consuming the auto-completion: %v", err)
	}
	if written != 2 {
		t.Errorf("the auto-completion wrote %d notifications, want one each for the customer "+
			"and the awarded provider", written)
	}
}

// TestNobodyIsToldWhatTheyJustDid covers both suppressions, which are the same rule reached two
// ways: by identifier where the event names an actor, and by role where it names only a side.
func TestNobodyIsToldWhatTheyJustDid(t *testing.T) {
	pool := pgtest.DB(t)

	customer := newUser(t, pool, "suppress-customer@example.com", "+61400007107", "customer")
	provider := newUser(t, pool, "suppress-provider@example.com", "+61400007108", "provider")
	service := NewService(&stubParties{customer: customer, provider: provider},
		clock.NewFixed(testInstant), Senders{})

	t.Run("the party who countered is not told about their own counter", func(t *testing.T) {
		env := envelopeOf(t, "bid.countered", map[string]any{
			"job_id":      uuid.Must(uuid.NewV7()).String(),
			"provider_id": provider.String(),
			"offered_by":  "provider",
		})

		if _, err := consume(t, pool, service, env); err != nil {
			t.Fatalf("consuming the counter: %v", err)
		}
		rows := rowsFor(t, pool, env.ID)
		if len(rows) != 1 || rows[0].Recipient != customer {
			t.Errorf("a provider counter told %d people; it should tell the customer alone",
				len(rows))
		}
	})

	t.Run("the actor named by identifier is not told", func(t *testing.T) {
		env := envelopeOf(t, EventJobStatusChanged, map[string]any{
			"job_id":     uuid.Must(uuid.NewV7()).String(),
			"from":       "Awarded",
			"to":         "Cancelled",
			"actor_type": "customer",
			"actor_id":   customer.String(),
		})

		if _, err := consume(t, pool, service, env); err != nil {
			t.Fatalf("consuming the cancellation: %v", err)
		}
		rows := rowsFor(t, pool, env.ID)
		if len(rows) != 1 || rows[0].Recipient != provider {
			t.Errorf("a customer cancellation told %d people; the customer who cancelled "+
				"does not need an email about it", len(rows))
		}
	})
}

// TestAnEventAboutAJobThatIsGoneTellsNobody keeps a consumer catching up after an outage from
// parking a partition on a job that has since been pseudonymised (Docs/05 §3.1, SHIP-171).
func TestAnEventAboutAJobThatIsGoneTellsNobody(t *testing.T) {
	pool := pgtest.DB(t)

	service := NewService(&stubParties{missing: true}, clock.NewFixed(testInstant), Senders{})

	env := envelopeOf(t, EventJobStatusChanged, map[string]any{
		"job_id": uuid.Must(uuid.NewV7()).String(),
		"from":   "Delivered",
		"to":     "Completed",
	})

	written, err := consume(t, pool, service, env)
	if err != nil {
		t.Fatalf("an event about a job that no longer exists failed rather than being "+
			"absorbed: %v", err)
	}
	if written != 0 {
		t.Errorf("it wrote %d notifications for a job nobody owns", written)
	}
}

// TestAnUnknownEventTypeIsRefused is the deliberate exception to "absorb everything".
//
// cmd/api holds Rules against the live catalogue, so an unrouted type on the topic means either a
// deployment older than the events it is reading or an event somebody added with no routing
// decision. Both should stop and be seen rather than be skipped into a log nobody reads.
func TestAnUnknownEventTypeIsRefused(t *testing.T) {
	pool := pgtest.DB(t)

	service := NewService(&stubParties{}, clock.NewFixed(testInstant), Senders{})
	env := envelopeOf(t, "job.something_nobody_routed", map[string]any{
		"job_id": uuid.Must(uuid.NewV7()).String(),
	})

	if _, err := consume(t, pool, service, env); !errors.Is(err, ErrNoRule) {
		t.Errorf("an unrouted event type returned %v, want ErrNoRule", err)
	}
}

// TestConsumeRefusesAPool is the same guard jobs.Transition has, for the same reason: outside a
// transaction, half a set of rows could commit on its own, and a redelivery would complete it with
// a duplicate rather than being refused by the index.
func TestConsumeRefusesAPool(t *testing.T) {
	pool := pgtest.DB(t)

	service := NewService(&stubParties{}, clock.NewFixed(testInstant), Senders{})
	env := envelopeOf(t, "bid.placed", map[string]any{
		"job_id":      uuid.Must(uuid.NewV7()).String(),
		"provider_id": uuid.Must(uuid.NewV7()).String(),
	})

	if _, err := service.Consume(t.Context(), pool, env); !errors.Is(err, ErrNotInTransaction) {
		t.Errorf("Consume against a pool returned %v, want ErrNotInTransaction", err)
	}
}

// TestASuspendedAccountIsNotEmailed is a small thing the read does that is easy to leave out.
//
// ck_users_status has three values and `suspended` means the account may not be used at all
// (000002). Continuing to email somebody the platform has suspended is the kind of detail that
// turns a moderation decision into a complaint.
func TestASuspendedAccountIsNotEmailed(t *testing.T) {
	pool := pgtest.DB(t)

	customer := newUser(t, pool, "suspended@example.com", "+61400007109", "customer")
	if _, err := pool.Exec(t.Context(),
		`UPDATE users SET status = 'suspended' WHERE id = $1`, customer); err != nil {
		t.Fatalf("suspending the account: %v", err)
	}

	service := NewService(&stubParties{customer: customer}, clock.NewFixed(testInstant), Senders{})
	env := envelopeOf(t, "bid.placed", map[string]any{
		"job_id":      uuid.Must(uuid.NewV7()).String(),
		"provider_id": uuid.Must(uuid.NewV7()).String(),
	})

	written, err := consume(t, pool, service, env)
	if err != nil {
		t.Fatalf("consuming for a suspended account: %v", err)
	}
	if written != 0 {
		t.Errorf("a suspended account was sent %d notifications", written)
	}
}
