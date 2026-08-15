package events

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
)

// The wire form of an event: what the publisher writes onto a topic and what a consumer reads back
// off it.
//
// It was a private struct in cmd/worker/tasks_outbox.go until SHIP-137 needed to read one. Moved
// here rather than copied, because a wire format with two independent declarations is a wire format
// that drifts — and the failure of that drift is silent: a consumer that decodes a field the
// publisher renamed gets a zero value, not an error.
//
// It sits in internal/events rather than in either end for the reason the catalogue does. The
// catalogue decides what may travel; this decides how it travels; and both are read by a producer
// in one binary and a consumer in another, neither of which may import the other.

// Envelope is one event as it appears on a Kafka topic.
//
// Self-describing rather than the bare payload, because the identifier a consumer deduplicates on
// has to survive being read by something that does not also read the message headers — and because
// a message on a topic somebody is debugging should say what it is. The same identifiers are
// repeated as Kafka headers so a consumer can filter without deserialising the body at all; see
// [HeaderEventID] and the constants beside it.
type Envelope struct {
	ID   uuid.UUID `json:"id"`
	Type string    `json:"type"`

	// SchemaVersion is read out of the stored payload rather than looked up in the catalogue
	// at publish time, and the distinction matters exactly once: a row written before a
	// deployment and drained after one carries the version it was written under, while the
	// catalogue by then holds a different answer. Zero, and omitted, means the row was written
	// by hand or predates its schema — a true statement and a better one than a guess.
	SchemaVersion int `json:"schema_version,omitempty"`

	AggregateType string    `json:"aggregate_type"`
	AggregateID   uuid.UUID `json:"aggregate_id"`
	OccurredAt    time.Time `json:"occurred_at"`

	Payload json.RawMessage `json:"payload"`
}

// The Kafka header names, so that a producer and a consumer spell them the same way.
//
// A consumer routes on the type and the version without deserialising the body, which is what makes
// "refuse a version I was not built for" a cheap thing to do rather than a thing everybody skips.
const (
	HeaderEventID       = "event-id"
	HeaderEventType     = "event-type"
	HeaderAggregateType = "aggregate-type"
	HeaderAggregateID   = "aggregate-id"
	HeaderSchemaVersion = "schema-version"
)

// EnvelopeOf is the publisher's half: an outbox row as it goes onto the topic.
func EnvelopeOf(e Event) Envelope {
	return Envelope{
		ID:            e.ID,
		Type:          e.Type,
		SchemaVersion: SchemaVersionOf(e.Payload),
		AggregateType: e.AggregateType,
		AggregateID:   e.AggregateID,
		OccurredAt:    e.OccurredAt,
		Payload:       e.Payload,
	}
}

// DecodeEnvelope is the consumer's half.
//
// It refuses a message with no identifier, no type or no payload rather than accepting a
// half-decoded one. A consumer deduplicates on the identifier, so a message without one cannot be
// processed exactly once by anything; and an empty payload is a marshalling fault upstream, not a
// notification with nothing in it.
func DecodeEnvelope(message []byte) (Envelope, error) {
	var e Envelope
	if err := json.Unmarshal(message, &e); err != nil {
		return Envelope{}, fmt.Errorf("events: reading a message off the topic: %w", err)
	}
	switch {
	case e.ID == uuid.Nil:
		return Envelope{}, fmt.Errorf("events: a message on %s carries no event id, so "+
			"nothing can process it exactly once", TopicFor(e.AggregateType))
	case e.Type == "":
		return Envelope{}, fmt.Errorf("events: message %s carries no event type", e.ID)
	case len(e.Payload) == 0:
		return Envelope{}, fmt.Errorf("events: %s (%s) carries an empty payload", e.Type, e.ID)
	}
	return e, nil
}
