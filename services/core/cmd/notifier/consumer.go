package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/segmentio/kafka-go"

	"github.com/DulsaraNethmin/Shipper/services/core/internal/config"
	"github.com/DulsaraNethmin/Shipper/services/core/internal/db"
	"github.com/DulsaraNethmin/Shipper/services/core/internal/events"
	"github.com/DulsaraNethmin/Shipper/services/core/internal/notifications"
)

// The consumer loop: fetch, record, commit, dispatch.
//
// # The order of the four is the whole design
//
//  1. **Fetch** a bounded batch of messages, without committing anything.
//  2. **Record** them in one database transaction: resolve the recipients, write the rows.
//  3. **Commit the offsets**, but only once that transaction has committed.
//  4. **Dispatch** whatever is pending, which is these rows and anything an earlier pass failed on.
//
// Step 3 after step 2 is the reason this is a process rather than a cmd/worker task, and it is
// what makes the guarantee at-least-once rather than at-most-once. If the process dies between
// them, the same messages arrive again and Consume writes nothing, because
// uq_notifications_event_recipient_channel refuses the duplicate. If the offsets were committed
// first, a rollback would move past events nothing had recorded and nobody would ever be told.
//
// Step 4 after step 3, rather than inside step 2, is Docs/01 §4.5: a notification failure must not
// lose the event. The rows are durable before any channel is touched, so an email provider that is
// down leaves rows at `failed` and the next pass claims them again.

// DefaultConsumerGroup is the Kafka consumer group this service joins.
//
// A stable name, not a per-process one, and that is what makes horizontal scaling work: two
// notifier instances in one group are handed different partitions and each event is delivered to
// one of them. A per-process group would deliver every event to every instance, and the unique
// index would turn that into a silent halving of throughput rather than a visible fault.
//
// Namespaced the way every other shared name in this service is: `shipper-` here, `shipper:` in
// Redis, `shipper.` on the topics.
const DefaultConsumerGroup = "shipper-notifications"

// The -group flag overrides it, and the reason is a property of this repository rather than of
// Kafka.
//
// **Kafka has no per-worktree isolation and none is possible** (CLAUDE.md, Docs/11 §9). Five
// worktrees share one broker and one topic set, and a consumer group is cluster-scoped exactly as a
// database template name is — so two worktrees running `make verify` at once would join the *same*
// group, be handed different partitions, and each consume the messages the other was asserting on.
// That is the same class of failure as the shared `TEST_TEMPLATE_DB` this repository has already
// paid for once, and unlike the database there is nothing to split.
//
// So scripts/verify/81-notifier.sh runs with a group of its own, per run, and a deployment runs with
// no arguments.
//
// A flag rather than an entry in internal/config, and that is a precedent rather than an invention:
// `cmd/topics -replication` was a flag for exactly this reason — internal/config is a shared surface
// a domain branch may not edit (Docs/10 §9.2) and the track that needed it was one. SHIP-15m later
// folded that one into configuration and kept the flag as an override, and this should go the same
// way when somebody is next editing internal/config for a reason of their own.
var consumerGroup = flag.String("group", DefaultConsumerGroup,
	"the Kafka consumer group to join; per-run in scripts/verify because the broker is shared "+
		"across worktrees and a group is cluster-scoped")

// The loop's bounds.
const (
	// fetchWait is how long a pass waits for messages before going round again with none.
	//
	// It is not a latency budget — kafka-go returns as soon as a message arrives — it is how
	// often the dispatch half runs on an idle topic, which is what retries a failed send. Two
	// seconds matches the outbox publisher's interval, so a notification's floor is the drain
	// plus this rather than the drain plus something unrelated.
	fetchWait = 2 * time.Second

	// fetchBatch bounds how many messages one transaction records.
	//
	// Bounded rather than "whatever is available" because the transaction holds a partition's
	// progress for its duration, and a batch large enough to take seconds is a batch that
	// delays every later event on those partitions by seconds.
	fetchBatch = 100

	// recordTimeout bounds the database half of a pass, and dispatchTimeout the sending half.
	//
	// Separate because they fail differently: the first is a local transaction and a slow one
	// means the database is in trouble, while the second is somebody else's HTTP endpoint and
	// is allowed to be slow. Both are far shorter than shutdownTimeout, so a pass in flight
	// finishes rather than being cut off by a signal.
	recordTimeout   = 15 * time.Second
	dispatchTimeout = 30 * time.Second

	// shutdownTimeout bounds closing the reader, which commits nothing and only has to close a
	// connection and leave the group.
	shutdownTimeout = 10 * time.Second
)

// consumer is the process's one loop.
type consumer struct {
	reader  *kafka.Reader
	pool    *pgxpool.Pool
	log     *slog.Logger
	service *notifications.Service
}

// newConsumer builds the reader over every topic the catalogue knows about.
//
// # Every topic, derived rather than typed
//
// events.Topics() is the catalogue's own answer, so a fourth aggregate — which internal/events
// makes a deliberate decision recorded in one place — reaches this consumer with no edit here.
// Typing the three names out would be a second copy of a closed set, which is exactly what
// cmd/topics refuses to do for the same reason.
//
// # GroupTopics rather than three readers
//
// One reader across three topics means one group membership and one rebalance, and it means the
// dispatch half runs once per pass rather than three times. The cost is that ordering is per
// partition rather than per topic, which changes nothing: the promise 000004_outbox.up.sql makes is
// per aggregate, the key is the aggregate id, and that is preserved by the partitioning regardless
// of how many topics one reader subscribes to.
//
// # StartOffset is FirstOffset, and it only applies to a group that has never committed
//
// A new deployment reads the topic from the beginning rather than from now. That is the right
// default for this consumer: every message it has not seen is a person who has not been told, and
// Kafka's seven-day retention (internal/events.TopicRetention) bounds how far back that can go. A
// group with committed offsets ignores this entirely and resumes where it was.
func newConsumer(
	cfg *config.Config, log *slog.Logger, pool *pgxpool.Pool, service *notifications.Service,
) (*consumer, error) {
	topics := events.Topics()
	if len(topics) == 0 {
		return nil, errors.New("cmd/notifier: the event catalogue names no topics")
	}

	reader := kafka.NewReader(kafka.ReaderConfig{
		Brokers:     cfg.Kafka.Brokers,
		GroupID:     *consumerGroup,
		GroupTopics: topics,
		StartOffset: kafka.FirstOffset,

		// Manual commits. The default commits on read, which is precisely the ordering
		// this whole file exists to avoid.
		CommitInterval: 0,

		// A short poll so a pass that found nothing turns over promptly and runs the
		// dispatch half, which is what retries a failed send on an idle topic.
		MaxWait: fetchWait,

		ErrorLogger: kafka.LoggerFunc(func(msg string, args ...any) {
			log.Warn("kafka consumer: " + fmt.Sprintf(msg, args...))
		}),
	})

	return &consumer{reader: reader, pool: pool, log: log, service: service}, nil
}

// Run works until ctx is cancelled.
func (c *consumer) Run(ctx context.Context) error {
	c.log.Info("consuming domain events",
		slog.Any("topics", events.Topics()),
		slog.String("group", *consumerGroup))

	for ctx.Err() == nil {
		if err := c.pass(ctx); err != nil {
			if ctx.Err() != nil {
				break
			}

			// Logged and retried rather than returned. One transient database error or
			// one rebalance must not end the process: the alternative is a notifier that
			// stops consuming and reports itself as having exited cleanly, and the
			// symptom of that is nobody receiving anything, discovered a day later.
			c.log.Error("a consumer pass failed", slog.String("error", err.Error()))

			select {
			case <-ctx.Done():
			case <-time.After(fetchWait):
			}
		}
	}

	c.log.Info("stopped consuming")
	return nil
}

// pass is one turn of the loop.
func (c *consumer) pass(ctx context.Context) error {
	messages, err := c.fetch(ctx)
	if err != nil {
		return err
	}

	if len(messages) > 0 {
		if err := c.record(ctx, messages); err != nil {
			return err
		}

		// Only now. See the header: this line after that call is the difference between
		// at-least-once and at-most-once.
		commitCtx, cancel := context.WithTimeout(context.Background(), recordTimeout)
		err := c.reader.CommitMessages(commitCtx, messages...)
		cancel()
		if err != nil {
			// The rows are committed and the offsets are not, so the same messages
			// arrive again and write nothing. Reported rather than swallowed, because a
			// commit that keeps failing is a consumer making no progress.
			return fmt.Errorf("cmd/notifier: committing %d offset(s): %w", len(messages), err)
		}
	}

	return c.dispatch(ctx)
}

// fetch reads up to [fetchBatch] messages, returning as soon as the topic goes quiet.
//
// FetchMessage rather than ReadMessage, because ReadMessage commits — which is the one thing this
// consumer must not do before its transaction has.
//
// # Every fetch is bounded, including the first, and that was a correction
//
// The obvious shape waits for the first message on the caller's context, so an idle consumer blocks
// until something arrives. It is wrong here, and the reason is that the loop has a second half: an
// idle topic would mean the dispatch phase never ran, so a notification whose send failed would sit
// at `failed` until the next unrelated event happened to arrive. On a quiet marketplace that is
// hours, and the row exists precisely so that a channel outage costs a delay rather than a message.
//
// So the first fetch is bounded by [fetchWait] and an empty return is ordinary. Every message after
// it is fetched under a much shorter deadline, so a batch does not sit waiting to fill: a lone event
// is recorded immediately rather than after a full [fetchWait].
func (c *consumer) fetch(ctx context.Context) ([]kafka.Message, error) {
	waiting, cancelWait := context.WithTimeout(ctx, fetchWait)
	first, err := c.reader.FetchMessage(waiting)
	cancelWait()
	if err != nil {
		stopping := errors.Is(err, context.Canceled) // spelling:ok — standard library sentinel, not our word
		if ctx.Err() != nil || stopping || errors.Is(err, context.DeadlineExceeded) {
			// Nothing arrived, or the process is stopping. Both mean this pass has no
			// messages, and the first is the ordinary state of a quiet topic.
			return nil, nil
		}
		return nil, fmt.Errorf("cmd/notifier: fetching from %v: %w", events.Topics(), err)
	}

	messages := []kafka.Message{first}
	for len(messages) < fetchBatch {
		more, cancel := context.WithTimeout(ctx, 50*time.Millisecond)
		next, err := c.reader.FetchMessage(more)
		cancel()
		if err != nil {
			break
		}
		messages = append(messages, next)
	}
	return messages, nil
}

// record writes the notification rows for a batch, in one transaction.
//
// # A message this consumer cannot read is dropped rather than allowed to stop the topic
//
// A message whose envelope will not decode, or whose payload names a job that is not an identifier,
// is a permanent condition: it will fail the same way on every redelivery, and returning an error
// would park the partition on it forever. So it is logged loudly and skipped, and the offset moves
// past it.
//
// That is the same position internal/events takes from the other end, and it is only defensible
// because of it: SHIP-135 made every permanently unacceptable event *unwritable*, inside the
// transaction making the state change, so a message reaching here that cannot be read did not come
// through this platform.
//
// # An unrouted event type is in the same class, and that was a decision rather than an oversight
//
// The first version of this returned [notifications.ErrNoRule] and failed the pass, on the
// reasoning that an event nobody routed is silence and should be seen. It parks the partition
// forever, and on this broker that is not hypothetical: Kafka is shared across every worktree with
// no isolation possible, so a topic legitimately carries messages from a build that has an event
// this one does not. One such message would stop the consumer on every developer machine at once.
//
// The guard belongs where the event is *written*, which is the argument
// internal/events/catalogue.go already makes about the dead-letter question: everything that can
// permanently reject an event is checked inside the transaction making the state change.
// cmd/api/events_notifications_test.go is that check for routing — an event registered in the
// catalogue with no rule fails the build, before anything can emit it. So this end logs and moves
// on, and the failure is caught a deployment earlier by something that can name the line.
func (c *consumer) record(ctx context.Context, messages []kafka.Message) error {
	ctx, cancel := context.WithTimeout(ctx, recordTimeout)
	defer cancel()

	return db.InTx(ctx, c.pool, func(ctx context.Context, r db.Runner) error {
		for _, message := range messages {
			env, err := events.DecodeEnvelope(message.Value)
			if err != nil {
				c.log.Error("a message on the topic could not be read and was skipped",
					slog.String("topic", message.Topic),
					slog.Int("partition", message.Partition),
					slog.Int64("offset", message.Offset),
					slog.String("error", err.Error()))
				continue
			}

			written, err := c.service.Consume(ctx, r, env)
			switch {
			case errors.Is(err, notifications.ErrNoRule):
				// Logged at error level, not warn: on a broker this service owns it
				// means somebody registered an event and did not decide who hears
				// about it, which cmd/api's exhaustiveness test would have caught.
				c.log.Error("an event on the topic has no routing rule and was skipped",
					slog.String("event_id", env.ID.String()),
					slog.String("event_type", env.Type),
					slog.String("hint", "add a rule to notifications.Rules; "+
						"cmd/api/events_notifications_test.go fails on an "+
						"event this platform registers without one"))
				continue
			case err != nil:
				c.log.Error("an event could not be turned into notifications and was skipped",
					slog.String("event_id", env.ID.String()),
					slog.String("event_type", env.Type),
					slog.String("error", err.Error()))
				continue
			case written > 0:
				c.log.Info("notifications recorded",
					slog.String("event_id", env.ID.String()),
					slog.String("event_type", env.Type),
					slog.Int("recorded", written))
			default:
				// Either nobody to tell, or this event has been seen before. Both are
				// ordinary, and the second is the at-least-once guarantee working.
				c.log.Debug("no new notifications for this event",
					slog.String("event_id", env.ID.String()),
					slog.String("event_type", env.Type))
			}
		}
		return nil
	})
}

// dispatch sends whatever is pending, in batches, until a pass claims nothing.
//
// Its own transaction per batch, separate from the recording one, so a channel failure cannot roll
// back the rows that say who has to be told. The loop is bounded by the claim finding nothing
// rather than by a count: after an outage there may be a backlog, and stopping after one batch
// would drain it at [notifications.DispatchBatch] per [fetchWait] however long it is.
func (c *consumer) dispatch(ctx context.Context) error {
	for ctx.Err() == nil {
		// A derived context per batch, named rather than shadowing: an earlier version
		// assigned it back to ctx, so the loop condition read the *cancelled* one on its
		// second turn and a backlog drained one batch per pass instead of continuously.
		batchCtx, cancel := context.WithTimeout(ctx, dispatchTimeout)

		var sent int
		err := db.InTx(batchCtx, c.pool, func(ctx context.Context, r db.Runner) error {
			var err error
			sent, err = c.service.Dispatch(ctx, r)
			return err
		})
		cancel()

		if err != nil {
			return fmt.Errorf("cmd/notifier: dispatching: %w", err)
		}
		if sent == 0 {
			return nil
		}
		c.log.Info("notifications dispatched", slog.Int("claimed", sent))
	}
	return nil
}

// Close leaves the consumer group and closes the connection.
//
// It commits nothing: everything this process had recorded was committed by the pass that recorded
// it, and anything fetched but not recorded is deliberately left uncommitted so the next start
// reads it again.
func (c *consumer) Close(ctx context.Context) error {
	done := make(chan error, 1)
	go func() { done <- c.reader.Close() }()

	select {
	case err := <-done:
		if err != nil {
			return fmt.Errorf("cmd/notifier: closing the consumer: %w", err)
		}
		return nil
	case <-ctx.Done():
		return fmt.Errorf("cmd/notifier: the consumer did not leave its group within the "+
			"shutdown window: %w", ctx.Err())
	}
}
