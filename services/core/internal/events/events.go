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
	"bytes"
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
	// "bid.accepted", "delivery.milestone_recorded". It is the key into the catalogue, and
	// the version belongs to its [Schema] rather than to this string. See catalogue.go for
	// why the version is in neither the name nor the topic.
	Type string

	// Payload is the event body. It is stored as jsonb, and [New] writes
	// [SchemaVersionField] into it from the catalogue — so the row records the version it
	// was written under rather than whichever the catalogue holds when it is published.
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
	if err := checkAgainstCatalogue(e); err != nil {
		return err
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
//
// # The aggregate type is not a parameter, and that is the point
//
// It used to be, and a caller could therefore emit "job.status_changed" on the "bid" aggregate.
// Nothing would have complained: the row is valid, the publisher would route it to shipper.bid,
// and the notification nobody received would be the only symptom. The catalogue knows which
// aggregate an event belongs to, so it supplies it and the mistake is unavailable (SHIP-135).
//
// # What this refuses, and why it refuses it here
//
// An unregistered event type, a payload that is not a JSON object, and a payload over the
// schema's bound. All three are permanent conditions rather than transient ones, and catalogue.go
// sets out why every permanent condition is checked at this end: this runs inside the transaction
// making the state change, so refusing rolls the change back and tells the caller, rather than
// leaving a row the publisher can never drain.
func New(eventType string, aggregateID uuid.UUID, occurredAt time.Time, payload any) (Event, error) {
	schema, ok := Lookup(eventType)
	if !ok {
		return Event{}, fmt.Errorf("events: %s is not in the catalogue; register it from an "+
			"init in the domain that emits it, so that it has a topic, a version and a "+
			"recorded shape", eventType)
	}

	id, err := uuid.NewV7()
	if err != nil {
		return Event{}, fmt.Errorf("events: generate id for %s: %w", eventType, err)
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return Event{}, fmt.Errorf("events: marshal %s payload: %w", eventType, err)
	}

	versioned, err := withSchemaVersion(body, schema.Version)
	if err != nil {
		return Event{}, fmt.Errorf("events: %s: %w", eventType, err)
	}
	if len(versioned) > schema.PayloadLimit() {
		return Event{}, fmt.Errorf("events: %s has a %d byte payload and its schema permits "+
			"%d; an event large enough to argue about belongs in object storage with the "+
			"event carrying its key", eventType, len(versioned), schema.PayloadLimit())
	}

	return Event{
		ID:            id,
		AggregateType: string(schema.Aggregate),
		AggregateID:   aggregateID,
		Type:          eventType,
		Payload:       versioned,
		OccurredAt:    occurredAt.UTC(),
	}, nil
}

// checkAgainstCatalogue is [Outbox.Emit]'s half of the same guard.
//
// [New] has already made all three of these true of anything it built. This is for an Event
// assembled by hand — which is legal, since Event has no unexported field — and it is the check
// that actually reaches the database, so it is the one that decides what can be in the table.
func checkAgainstCatalogue(e Event) error {
	schema, ok := Lookup(e.Type)
	if !ok {
		return fmt.Errorf("events: %s is not in the catalogue and cannot be written to the "+
			"outbox; a row the publisher has no schema for is a row it can never drain",
			e.Type)
	}
	if e.AggregateType != string(schema.Aggregate) {
		return fmt.Errorf("events: %s is an event about a %s and was emitted on aggregate "+
			"%q, so it would publish to %s and be read by nobody",
			e.Type, schema.Aggregate, e.AggregateType, TopicFor(e.AggregateType))
	}
	if len(e.Payload) > schema.PayloadLimit() {
		return fmt.Errorf("events: %s has a %d byte payload and its schema permits %d",
			e.Type, len(e.Payload), schema.PayloadLimit())
	}
	return nil
}

// withSchemaVersion writes the schema version into a marshalled payload.
//
// Spliced onto the front rather than round-tripped through a map, so the domain's field order
// survives and the version is the first thing anybody reading the row sees. A payload that is not
// a JSON object is refused rather than wrapped: an event body is an object in every schema in the
// catalogue, [Register] enforces that the struct behind it is one, and a caller passing an array
// or a bare string has made a mistake that is better reported than accommodated.
func withSchemaVersion(body []byte, version int) ([]byte, error) {
	trimmed := bytes.TrimSpace(body)
	if len(trimmed) < 2 || trimmed[0] != '{' || trimmed[len(trimmed)-1] != '}' {
		return nil, fmt.Errorf("its payload marshalled to %s, and an event payload is a JSON "+
			"object", firstBytes(trimmed))
	}

	prefix := fmt.Sprintf(`{%q:%d`, SchemaVersionField, version)
	if bytes.Equal(trimmed, []byte("{}")) {
		return []byte(prefix + "}"), nil
	}

	// A struct that already declared the field would produce a duplicate key, which most JSON
	// readers resolve silently and differently from each other. Register refuses such a struct,
	// so reaching this means a map payload built by hand.
	var probe map[string]json.RawMessage
	if err := json.Unmarshal(trimmed, &probe); err != nil {
		return nil, fmt.Errorf("its payload is not readable as a JSON object: %w", err)
	}
	if _, taken := probe[SchemaVersionField]; taken {
		return nil, fmt.Errorf("its payload already carries %s, and the catalogue is the only "+
			"thing that may set it", SchemaVersionField)
	}

	out := make([]byte, 0, len(prefix)+len(trimmed))
	out = append(out, prefix...)
	out = append(out, ',')
	return append(out, trimmed[1:]...), nil
}

// SchemaVersionOf reads the version back out of a stored payload, or zero if it has none.
//
// The publisher uses it to put the version on the wire. Zero is a real answer rather than a
// failure: a row written by hand — which is how the acceptance harness arranges a transaction
// that rolls back — has no version, and saying so is better than inventing one from the
// catalogue, which would be the catalogue's answer today rather than the row's answer when it
// was written.
func SchemaVersionOf(payload json.RawMessage) int {
	var probe struct {
		Version int `json:"schema_version"`
	}
	if err := json.Unmarshal(payload, &probe); err != nil {
		return 0
	}
	return probe.Version
}

// firstBytes is a short, safe rendering of a payload for an error message.
func firstBytes(b []byte) string {
	const limit = 40
	if len(b) == 0 {
		return "nothing"
	}
	if len(b) > limit {
		return string(b[:limit]) + "…"
	}
	return string(b)
}
