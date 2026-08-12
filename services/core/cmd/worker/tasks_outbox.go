package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
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
// internal/config, so this is reachable only by setting it to nothing on purpose.
func newKafkaEventPublisher(d Deps) (*kafka.Writer, EventPublisher) {
	brokers := d.Config.Kafka.Brokers
	log := d.Logger.With(slog.String("task", outboxTaskName))

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

		// SHIP-135 owns the topic set. A topic conjured here would take whatever
		// partition count the broker defaults to, and partitions cannot be reduced —
		// see the header of outbox.go. The broker refuses auto-creation as well
		// (deploy/docker-compose.yml), so this is belt and braces on a decision that
		// matters.
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
// One topic per aggregate type — shipper.job, shipper.bid, shipper.delivery — rather than one
// topic for everything, so that a consumer can subscribe to what it cares about and retention
// can differ per aggregate. It costs nothing today and cannot be undone cheaply later, because
// moving an event to a different topic means every consumer reads two of them for a while.
//
// **SHIP-135 owns the topic set and the schema**, and this function is the single place it will
// change. Nothing creates these topics yet: until SHIP-135 does, a pass against a broker that
// does not have them fails and the rows stay claimable, which is the correct direction to fail.
func topicFor(aggregateType string) string { return "shipper." + aggregateType }

// envelope is the wire form of an event.
//
// Self-describing rather than the bare payload, because the identifier a consumer deduplicates
// on has to survive being read by something that does not also read the headers — and because a
// message on a topic somebody is debugging should say what it is. The same four identifiers are
// repeated as Kafka headers so a consumer can filter without deserialising the body at all.
//
// **The versioned schema is SHIP-135's**, not this struct's. This is the minimum an event needs
// to be publishable and deduplicable; that ticket decides how it is versioned and where the
// definition lives.
type envelope struct {
	ID            uuid.UUID       `json:"id"`
	Type          string          `json:"type"`
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
		body, err := json.Marshal(envelope{
			ID:            e.ID,
			Type:          e.Type,
			AggregateType: e.AggregateType,
			AggregateID:   e.AggregateID,
			OccurredAt:    e.OccurredAt,
			Payload:       e.Payload,
		})
		if err != nil {
			return fmt.Errorf("marshalling %s (%s): %w", e.Type, e.ID, err)
		}

		messages[i] = kafka.Message{
			Topic: topicFor(e.AggregateType),
			Key:   []byte(e.AggregateID.String()),
			Value: body,
			Headers: []kafka.Header{
				{Key: "event-id", Value: []byte(e.ID.String())},
				{Key: "event-type", Value: []byte(e.Type)},
				{Key: "aggregate-type", Value: []byte(e.AggregateType)},
				{Key: "aggregate-id", Value: []byte(e.AggregateID.String())},
			},
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
