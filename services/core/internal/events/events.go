// Package events writes domain events into the transactional outbox.
//
// Docs/06 §4.0 sets out why the outbox exists rather than a direct publish. Publishing to
// Kafka from inside a database transaction cannot be made atomic, and both halves of getting
// it wrong are real: publish then fail to commit and a consumer acts on an award that never
// happened; commit then fail to publish and the winning provider is never told. Writing the
// event to a table in the same transaction as the state change makes the transaction the only
// thing that has to be atomic, which it already is.
//
// # Why this is infrastructure and not the notifications domain
//
// The outbox is written by jobs, bidding and delivery, and read by notifications. If the
// writer belonged to the notifications domain, every emitting domain would import it, and the
// import lint fails a domain that imports a domain. The seam has to sit underneath all of them.
//
// # Why it exists before anything reads it
//
// SHIP-57, SHIP-69 and SHIP-89 all specify that a transition emits an event, and all three
// land well before the publisher in SHIP-134. Without a seam in place each would invent its
// own mechanism and SHIP-136 would rewrite all three. An unused table is cheaper than three
// incompatible answers.
package events

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/DulsaraNethmin/Shipper/services/core/internal/db"
)

// Event is one thing that happened, recorded at the moment it happened.
//
// It carries what a consumer needs to act without reading back the database, but it is not
// itself the record of truth — PostgreSQL is (Docs/06 §4). A consumer that needs the current
// state of a job reads the job.
type Event struct {
	// ID identifies this event. It is UUIDv7, so events sort by time without a separate
	// sequence, and it is what makes a consumer idempotent: delivery is at-least-once, so
	// a consumer will see some events twice and must recognise them.
	ID uuid.UUID

	// AggregateType and AggregateID say what the event is about — "job" and the job's id.
	//
	// Ordering is promised per aggregate and not globally. Events for one job publish in
	// the order they were written; nothing in the MVP needs more than that, and a global
	// order would mean a single partition.
	AggregateType string
	AggregateID   uuid.UUID

	// Type is the event name, `<aggregate>.<past-tense>` — "job.published",
	// "bid.accepted", "delivery.milestone_recorded". Versioning belongs to the schema in
	// SHIP-135, not to this string.
	Type string

	// Payload is the event body. It is stored as jsonb.
	Payload json.RawMessage

	// OccurredAt is when the state change happened, from the injected clock rather than
	// from the database, so a test can control it.
	OccurredAt time.Time
}

// Outbox writes events into the outbox table.
//
// It holds no connection of its own. Every method takes a db.Runner, because an event that is
// not written in the same transaction as the state change it describes defeats the entire
// point of the pattern.
type Outbox struct{}

// NewOutbox returns an outbox writer.
//
// There is deliberately nothing to configure. A writer with options would invite one domain to
// write events differently from another, and the publisher in SHIP-134 has to read them all.
func NewOutbox() *Outbox { return &Outbox{} }

// Emit writes e through r.
//
// r must be the transaction that is making the state change. Passing a pool here compiles and
// runs and is almost always wrong: the event would commit independently of the change it
// describes, which is the failure the outbox exists to prevent. There is no way to check that
// at compile time, which is why it is said plainly here and in Docs/10 §6.1.
func (o *Outbox) Emit(ctx context.Context, r db.Runner, e Event) error {
	if e.ID == uuid.Nil {
		return fmt.Errorf("events: %s has no ID", e.Type)
	}
	if e.Type == "" || e.AggregateType == "" {
		return fmt.Errorf("events: event for aggregate %q has no type", e.AggregateID)
	}
	if len(e.Payload) == 0 {
		// An event with no payload is almost certainly a marshalling mistake upstream,
		// and jsonb will not accept an empty string. Say so here rather than surfacing a
		// syntax error from PostgreSQL.
		return fmt.Errorf("events: %s has an empty payload", e.Type)
	}

	const q = `
		INSERT INTO outbox (id, aggregate_type, aggregate_id, event_type, payload, occurred_at)
		VALUES ($1, $2, $3, $4, $5, $6)`

	if _, err := r.Exec(ctx, q,
		e.ID, e.AggregateType, e.AggregateID, e.Type, []byte(e.Payload), e.OccurredAt,
	); err != nil {
		return fmt.Errorf("events: write %s to outbox: %w", e.Type, err)
	}
	return nil
}

// New builds an event, marshalling payload and assigning a UUIDv7 identifier.
//
// occurredAt comes from the caller's clock rather than from time.Now, so that the four
// scheduled tasks that emit events remain testable (Docs/10 §6.3).
func New(aggregateType string, aggregateID uuid.UUID, eventType string, occurredAt time.Time, payload any) (Event, error) {
	id, err := uuid.NewV7()
	if err != nil {
		return Event{}, fmt.Errorf("events: generate id for %s: %w", eventType, err)
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return Event{}, fmt.Errorf("events: marshal %s payload: %w", eventType, err)
	}

	return Event{
		ID:            id,
		AggregateType: aggregateType,
		AggregateID:   aggregateID,
		Type:          eventType,
		Payload:       body,
		OccurredAt:    occurredAt.UTC(),
	}, nil
}
