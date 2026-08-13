package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strconv"
	"time"

	"github.com/google/uuid"
	"github.com/segmentio/kafka-go"

	"github.com/DulsaraNethmin/Shipper/services/core/internal/events"
)

// SHIP-134's registration: the outbox drain, and the Kafka producer it publishes through.
//
// This is the only file in the service that imports a Kafka client, which is deliberate — see
// the header of outbox.go for why the drain is here rather than in internal/events.
//
// # The client, and why this one
//
// github.com/segmentio/kafka-go. There was no Kafka client in go.mod before this ticket, so the
// choice was open, and three things decided it:
//
//   - **Pure Go.** The alternative family — anything binding librdkafka — needs cgo, which costs
//     the static binary and complicates the Linux CI runner, and no document has chosen a
//     vendor. franz-go and sarama are pure Go too, so this narrowed rather than settled it.
//   - **Synchronous produce is the default and reads as one line.** An outbox may not mark a row
//     until the broker has acknowledged it, so what this task wants is a call that returns after
//     the acknowledgement. kafka-go's Writer with Async false is exactly that. franz-go is
//     faster and better maintained, but its produce path is a callback with a ProduceSync
//     wrapper, and throughput is not the constraint on a task draining a few hundred rows every
//     two seconds.
//   - **One module covers producing, consuming and the admin API.** SHIP-135 creates topics and
//     SHIP-137 consumes; both are served by this same dependency rather than a second one.
//
// It is behind EventPublisher, an interface of one method declared in outbox.go, so replacing it
// is a change to this file. That is not an adapter in CLAUDE.md's sense — there is one Kafka and
// no second implementation — it is the seam that lets the pass be tested without a broker.

// outboxTaskName identifies the task in the log, and is what the scheduler sorts on.
const outboxTaskName = "outbox-publisher"

// How often the outbox is drained, and how much of it at a time.
//
// Two seconds because the outbox is the latency floor for every notification in M5: an award
// email cannot arrive sooner than the drain that publishes the event behind it. Two hundred rows
// because the pass holds a transaction for its duration, and a batch large enough to take
// seconds is a batch that holds an aggregate's later events out of reach for seconds.
//
// Neither is configuration. They are tuning constants with no operational story yet, and
// Docs/10 §9.2 would have them cost an entry in internal/config and another in
// deploy/.env.example — two shared surfaces — to be changed by nobody.
const (
	outboxEvery = 2 * time.Second
	outboxBatch = 200

	// Shorter than defaultTaskTimeout, so a stalled broker surfaces as a publish error
	// naming the broker rather than as a pass cut off by its own deadline.
	outboxTimeout = 20 * time.Second
)

func init() {
	register(func(d Deps) Task {
		w, publish := newKafkaEventPublisher(d)
		drain := outboxDrain{to: publish, batch: outboxBatch}

		return Task{
			Name:    outboxTaskName,
			Every:   outboxEvery,
			Timeout: outboxTimeout,
			Run:     drain.run,

			// The reason Task.Close exists (SHIP-15g). A producer is a connection with
			// buffered messages behind it, and dropping it at shutdown would discard
			// events the database believes were published. The scheduler calls this
			// after the loop has stopped, on a fresh context bounded by
			// shutdownTimeout.
			Close: func(ctx context.Context) error { return closeWriter(ctx, w) },
		}
	})
}

// newKafkaEventPublisher builds the producer and the publisher over it.
//
// The writer is returned as well as the publisher because Close belongs to the task rather than
// to the interface: EventPublisher says what a pass needs, and a pass does not need to close
// anything.
//
// It returns a writer even with no brokers configured, so that the failure is one legible error
// per pass rather than a nil dereference. KAFKA_BROKERS defaults to localhost:29092 in
// internal/config, so that is reachable only by setting it to nothing on purpose.
//
// # Why nil fields on Deps are tolerated here rather than assumed away
//
// main.go always supplies all of them, so the guards below look like belt and braces. They are
// not: tasks() builds *every* registration, so any test asking the manifest a question about
// another task runs this closure with whatever Deps that test happened to need. SHIP-68's
// expiryTask passes a logger, a clock and a pool and no configuration, and the first version of
// this function dereferenced d.Config and took that test down with it — in a file the jobs track
// owns and this one may not edit. Docs/10 §9.2 already says the pool and the Redis client may be
// nil and must not be treated as a promise; a registration closure should extend the same
// courtesy to the rest of Deps.
func newKafkaEventPublisher(d Deps) (*kafka.Writer, EventPublisher) {
	log := d.Logger
	if log == nil {
		log = slog.Default()
	}
	log = log.With(slog.String("task", outboxTaskName))

	var brokers []string
	if d.Config != nil {
		brokers = d.Config.Kafka.Brokers
	}

	if len(brokers) == 0 {
		log.Error("KAFKA_BROKERS is empty; the outbox cannot be drained and events will " +
			"accumulate unpublished")
		return nil, brokerlessPublisher{}
	}

	w := &kafka.Writer{
		Addr: kafka.TCP(brokers...),

		// No Topic, so every message carries its own — one topic per aggregate type,
		// see topicFor.
		Topic: "",

		// The per-aggregate ordering promise, kept on the broker side: the key is the
		// aggregate, so one aggregate's events all land on one partition and Kafka keeps
		// their order within it. Hashing the key is what makes that stable across
		// restarts and across workers.
		Balancer: &kafka.Hash{},

		// "Acknowledged" in Docs/06 §4.0 means every in-sync replica has it. Anything
		// weaker acknowledges a write that a leader failover can still lose, and the
		// pass would mark the row on the strength of it.
		RequiredAcks: kafka.RequireAll,

		// The topic set is cmd/topics', applied like a migration. A topic conjured
		// here would take whatever partition count the broker defaults to, and
		// partitions cannot be reduced — see the header of outbox.go. The broker
		// refuses auto-creation as well (deploy/docker-compose.yml), so this is belt
		// and braces on a decision that matters.
		AllowAutoTopicCreation: false,

		// The whole claimed batch arrives in one WriteMessages call, so the writer
		// should send it rather than wait for more. Left at the one-second default, a
		// pass that claimed three rows would sit out that second inside its transaction.
		BatchSize:    outboxBatch,
		BatchTimeout: 10 * time.Millisecond,

		// Bounded well inside outboxTimeout, so an unreachable broker produces a publish
		// error the log can name rather than a pass killed by its own deadline.
		WriteTimeout: 5 * time.Second,
		ReadTimeout:  5 * time.Second,
		MaxAttempts:  3,

		// Synchronous. WriteMessages returns after the acknowledgement, which is the
		// only ordering an outbox can be built on.
		Async: false,

		ErrorLogger: kafka.LoggerFunc(func(msg string, args ...any) {
			log.Warn("kafka producer: " + fmt.Sprintf(msg, args...))
		}),
	}

	return w, kafkaEventPublisher{w: w}
}

// closeWriter flushes and closes the producer within the shutdown window it is given.
//
// kafka.Writer.Close takes no context, so the wait is done here. A Close that outlives the
// window is reported rather than waited on: holding a deployment open until the container
// runtime sends SIGKILL would discard the very buffer this is protecting.
func closeWriter(ctx context.Context, w *kafka.Writer) error {
	if w == nil {
		return nil
	}

	done := make(chan error, 1)
	go func() { done <- w.Close() }()

	select {
	case err := <-done:
		if err != nil {
			return fmt.Errorf("worker: closing the outbox producer: %w", err)
		}
		return nil
	case <-ctx.Done():
		return fmt.Errorf("worker: the outbox producer did not flush within the shutdown "+
			"window; anything still buffered will be published again from the outbox: %w",
			ctx.Err())
	}
}

// topicFor is where an event's aggregate type becomes a topic name.
//
// **This is the line Docs/11 §9 said SHIP-135 would change**, and what changed is where it lives:
// the mapping is now in internal/events beside the catalogue that decides the topic set, and
// cmd/topics creates exactly what events.Topics() lists. The rule is unchanged — one topic per
// aggregate type, shipper.job, shipper.bid, shipper.delivery — and internal/events/catalogue.go
// carries the reasoning for it together with the reason a topic name does not carry a version.
//
// The delegation stays rather than the call site being rewritten, because the interesting fact
// about this function from here is that the producer does not get to decide topics.
func topicFor(aggregateType string) string { return events.TopicFor(aggregateType) }

// envelope is the wire form of an event.
//
// Self-describing rather than the bare payload, because the identifier a consumer deduplicates
// on has to survive being read by something that does not also read the headers — and because a
// message on a topic somebody is debugging should say what it is. The same identifiers are
// repeated as Kafka headers so a consumer can filter without deserialising the body at all.
//
// **SchemaVersion is SHIP-135's, and it is read out of the stored payload rather than looked up in
// the catalogue.** The distinction matters exactly once, and that once is the case the versioning
// exists for: a row written before a deployment and drained after one carries the version it was
// written under, while the catalogue by then holds a different answer. Looking it up here would
// relabel that row as the newer shape — a stale event accepted as meaning something else, which is
// the failure internal/pagination's cursor prefix was built against and this is built against too.
// Zero, omitted from both the envelope and the headers, means the row was written by hand or
// predates its schema; that is a true statement and a better one than a guess.
type envelope struct {
	ID            uuid.UUID       `json:"id"`
	Type          string          `json:"type"`
	SchemaVersion int             `json:"schema_version,omitempty"`
	AggregateType string          `json:"aggregate_type"`
	AggregateID   uuid.UUID       `json:"aggregate_id"`
	OccurredAt    time.Time       `json:"occurred_at"`
	Payload       json.RawMessage `json:"payload"`
}

// kafkaEventPublisher is the EventPublisher the worker actually runs with.
type kafkaEventPublisher struct{ w *kafka.Writer }

// Publish writes the whole batch and returns only once the broker has acknowledged all of it.
//
// One call rather than one per event, because the writer groups by partition and preserves the
// order it was given within each — which is the per-aggregate order the claim read them in. A
// partial failure comes back as an error for the batch, and outboxDrain.run explains why that
// fails the pass rather than salvaging the half that landed.
func (p kafkaEventPublisher) Publish(ctx context.Context, batch []events.Event) error {
	messages := make([]kafka.Message, len(batch))
	for i, e := range batch {
		version := events.SchemaVersionOf(e.Payload)

		body, err := json.Marshal(envelope{
			ID:            e.ID,
			Type:          e.Type,
			SchemaVersion: version,
			AggregateType: e.AggregateType,
			AggregateID:   e.AggregateID,
			OccurredAt:    e.OccurredAt,
			Payload:       e.Payload,
		})
		if err != nil {
			return fmt.Errorf("marshalling %s (%s): %w", e.Type, e.ID, err)
		}

		headers := []kafka.Header{
			{Key: "event-id", Value: []byte(e.ID.String())},
			{Key: "event-type", Value: []byte(e.Type)},
			{Key: "aggregate-type", Value: []byte(e.AggregateType)},
			{Key: "aggregate-id", Value: []byte(e.AggregateID.String())},
		}
		if version > 0 {
			// A consumer routes on this without deserialising the body, which is what
			// makes "refuse a version I was not built for" a cheap thing to do rather
			// than a thing everybody skips.
			headers = append(headers,
				kafka.Header{Key: "schema-version", Value: []byte(strconv.Itoa(version))})
		}

		messages[i] = kafka.Message{
			Topic:   topicFor(e.AggregateType),
			Key:     []byte(e.AggregateID.String()),
			Value:   body,
			Headers: headers,
		}
	}

	if err := p.w.WriteMessages(ctx, messages...); err != nil {
		return fmt.Errorf("writing %d event(s) to kafka: %w", len(messages), err)
	}
	return nil
}

// brokerlessPublisher stands in when KAFKA_BROKERS is empty, and refuses every pass.
//
// The worker still starts. Every task it runs is a claim against PostgreSQL and the others do
// not need a broker, so taking the process down over this would stop job expiry as well.
type brokerlessPublisher struct{}

func (brokerlessPublisher) Publish(context.Context, []events.Event) error {
	return errors.New("no Kafka brokers are configured (KAFKA_BROKERS)")
}

// Compile-time proof that a pass can be given either of them, and that a drain is a Work.
var (
	_ EventPublisher = kafkaEventPublisher{}
	_ EventPublisher = brokerlessPublisher{}
	_ Work           = outboxDrain{}.run
)
