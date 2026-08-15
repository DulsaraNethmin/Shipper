package notifications

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/DulsaraNethmin/Shipper/services/core/internal/db"
)

// The SQL, hand-written per Docs/10 §3.1, and unexported per Docs/10 §2.2 — there is no repository
// interface, because PostgreSQL is not abstracted in this service and the one statement that makes
// this domain correct is a unique index a mock would happily accept a duplicate past.

type postgresStore struct{}

// insert writes one pass's notification rows, and refuses the ones that already exist.
//
// # ON CONFLICT DO NOTHING is the idempotence, and the count is how it is observed
//
// A redelivered event resolves exactly the same recipients and produces exactly the same
// (event_id, recipient_id, channel) triples, so uq_notifications_event_recipient_channel refuses
// every insert and this returns zero. Nothing had to remember that the event had been seen:
// the rows are the memory, and they are the same rows a second consumer instance would be racing to
// write.
//
// The alternative — SELECT, then INSERT what is missing — is the version that looks equivalent and
// is not. Two consumers reading the same message both find nothing, both insert, and one of them
// gets a unique violation anyway; handling that is this statement, written out longhand and one
// round trip later.
//
// One statement rather than a loop, so that a batch of rows for one event is one round trip inside
// a transaction that is holding a Kafka partition's progress.
func (postgresStore) insert(ctx context.Context, r db.Runner, rows []Notification) (int, error) {
	if len(rows) == 0 {
		return 0, nil
	}

	const q = `
		INSERT INTO notifications
		    (id, event_id, event_type, job_id, recipient_id, channel, category, essential,
		     address, subject, body)
		SELECT * FROM unnest(
		    $1::uuid[], $2::uuid[], $3::text[], $4::uuid[], $5::uuid[], $6::text[],
		    $7::text[], $8::boolean[], $9::text[], $10::text[], $11::text[])
		ON CONFLICT (event_id, recipient_id, channel) DO NOTHING`

	n := len(rows)
	ids := make([]uuid.UUID, n)
	eventIDs := make([]uuid.UUID, n)
	eventTypes := make([]string, n)
	jobIDs := make([]uuid.UUID, n)
	recipients := make([]uuid.UUID, n)
	channels := make([]string, n)
	categories := make([]string, n)
	essential := make([]bool, n)
	addresses := make([]string, n)
	subjects := make([]string, n)
	bodies := make([]string, n)

	for i, row := range rows {
		ids[i] = row.ID
		eventIDs[i] = row.EventID
		eventTypes[i] = row.EventType
		jobIDs[i] = row.JobID
		recipients[i] = row.Recipient
		channels[i] = string(row.Channel)
		categories[i] = string(row.Category)
		essential[i] = row.Essential
		addresses[i] = row.Address
		subjects[i] = row.Subject
		bodies[i] = row.Body
	}

	tag, err := r.Exec(ctx, q, ids, eventIDs, eventTypes, jobIDs, recipients, channels,
		categories, essential, addresses, subjects, bodies)
	if err != nil {
		return 0, fmt.Errorf("notifications: writing %d notification(s): %w", n, err)
	}
	return int(tag.RowsAffected()), nil
}

// contacts reads the addresses of the accounts a rule resolved to.
//
// `users` is in the shared migration block, which migrations/blocks.go describes as the tables
// every domain reads — identity, jobs, fleet, delivery and admin all read it from their own
// postgres.go. Reading an email address out of it is not a boundary crossing; joining `jobs` to
// `bids` would be, which is why that one is a port (ports.go).
//
// An identifier with no row is dropped rather than reported. The foreign key on the table would
// refuse the insert anyway, and the case that produces it — an account pseudonymised between the
// event and the notification (Docs/05 §3.1, SHIP-171) — is somebody who should not be emailed.
//
// The roles map decides what each identifier was resolved as. It is passed in rather than read from
// users.role because the audience is what the rule asked for, and a mismatch between the two would
// be a routing bug worth seeing rather than one to silently correct.
func (postgresStore) contacts(
	ctx context.Context, r db.Runner, ids []uuid.UUID, roles map[uuid.UUID]Role,
) ([]Recipient, error) {
	const q = `
		SELECT id, email, phone
		FROM users
		WHERE id = ANY($1)
		  AND status <> 'suspended'`

	rows, err := r.Query(ctx, q, ids)
	if err != nil {
		return nil, fmt.Errorf("notifications: reading %d recipient(s): %w", len(ids), err)
	}

	found, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (Recipient, error) {
		var out Recipient
		if err := row.Scan(&out.UserID, &out.Email, &out.Phone); err != nil {
			return out, err
		}
		out.Role = roles[out.UserID]
		return out, nil
	})
	if err != nil {
		return nil, fmt.Errorf("notifications: reading recipients: %w", err)
	}

	// Back into the order the rule asked for, so a two-recipient event writes the customer's
	// row before the provider's and a test can say so without sorting.
	byID := make(map[uuid.UUID]Recipient, len(found))
	for _, recipient := range found {
		byID[recipient.UserID] = recipient
	}
	ordered := make([]Recipient, 0, len(found))
	for _, id := range ids {
		if recipient, ok := byID[id]; ok {
			ordered = append(ordered, recipient)
		}
	}
	return ordered, nil
}

// claimUndelivered runs [DispatchClaim] and reads what it locked.
func (postgresStore) claimUndelivered(ctx context.Context, r db.Runner, batch int) ([]Notification, error) {
	rows, err := r.Query(ctx, DispatchClaim, batch)
	if err != nil {
		return nil, fmt.Errorf("notifications: claiming undelivered notifications: %w", err)
	}

	out, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (Notification, error) {
		var n Notification
		err := row.Scan(&n.ID, &n.EventID, &n.EventType, &n.JobID, &n.Recipient, &n.Channel,
			&n.Category, &n.Essential, &n.Address, &n.Subject, &n.Body, &n.Status, &n.Attempts)
		return n, err
	})
	if err != nil {
		return nil, fmt.Errorf("notifications: reading claimed notifications: %w", err)
	}
	return out, nil
}

// markSent records a delivery the channel acknowledged.
//
// attempts goes up on a success as well as on a failure, because it counts attempts rather than
// failures — a message that went out on the third try is a different operational fact from one that
// went out first time, and ck_notifications_sent_at is what keeps `sent` and the instant together.
func (postgresStore) markSent(ctx context.Context, r db.Runner, id uuid.UUID, at time.Time) error {
	const q = `
		UPDATE notifications
		   SET status = 'sent', sent_at = $2, attempts = attempts + 1, last_error = NULL
		 WHERE id = $1`

	if _, err := r.Exec(ctx, q, id, at); err != nil {
		return fmt.Errorf("notifications: marking %s sent: %w", id, err)
	}
	return nil
}

// markFailed records an attempt that did not land, leaving the row claimable.
//
// `failed` rather than a retry counter that eventually gives up: Docs/01 §4.5 says the event must
// not be lost, and a row that retires itself is a notification nobody receives and nobody is told
// about. What bounds the retries is a person reading `attempts`, which is why SHIP-176's alerting
// has a column to count.
func (postgresStore) markFailed(ctx context.Context, r db.Runner, id uuid.UUID, reason string) error {
	const q = `
		UPDATE notifications
		   SET status = 'failed', attempts = attempts + 1, last_error = $2
		 WHERE id = $1`

	if _, err := r.Exec(ctx, q, id, reason); err != nil {
		return fmt.Errorf("notifications: marking %s failed: %w", id, err)
	}
	return nil
}

// markUndeliverable records an address that no longer exists (SHIP-139, 000702).
//
// Terminal: the claim's predicate excludes this status, so the row is never worked again. attempts
// goes up because one was made, and last_error carries the reason so that "why did this person
// never receive it" is answerable from the row rather than from a log that has rotated.
//
// sent_at stays NULL, which ck_notifications_sent_at requires of anything that is not `sent` — the
// message was not delivered, and a timestamp here would make it look as though it had been.
func (postgresStore) markUndeliverable(ctx context.Context, r db.Runner, id uuid.UUID) error {
	const q = `
		UPDATE notifications
		   SET status = 'undeliverable', attempts = attempts + 1,
		       last_error = 'the address was rejected by the channel and has been deregistered'
		 WHERE id = $1`

	if _, err := r.Exec(ctx, q, id); err != nil {
		return fmt.Errorf("notifications: marking %s undeliverable: %w", id, err)
	}
	return nil
}
