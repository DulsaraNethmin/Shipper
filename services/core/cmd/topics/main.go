// Command topics applies Shipper's Kafka topic set (SHIP-135).
//
// # Why a command, and not the compose stack or a worker start-up step
//
// Docs/11 §9 left this open in exactly these words: what creates the topics — "a migration-like
// step in the repository, the compose stack for local work, or a deployment action for anything
// else — and whether local and deployed answers may differ". This is the first of the three, and
// the answer to the second half is **no, they are the same step**.
//
// The topic set is a schema. Its partition count cannot be reduced once chosen, in the same way a
// dropped column cannot be un-dropped, and this repository already has a shape for that: a small
// binary that reads a declaration out of the code and applies it, run by `make` locally and by the
// deployment everywhere else. cmd/migrate is that shape for PostgreSQL and this is it for Kafka.
// One implementation, so local and deployed cannot drift, and it is derived from
// internal/events.Topics() rather than typed anywhere, so it cannot drift from the catalogue
// either.
//
// The compose stack was rejected because it would be a second, hand-maintained list of topics that
// only exists locally — the drift the previous paragraph is avoiding, written down.
//
// A start-up step in cmd/worker was rejected for two reasons. It runs once per process rather than
// once per deployment, which in the acceptance harness alone is three worker starts in
// scripts/verify/50-jobs.sh and another in 80-notifications.sh; and it needs Create permission on
// the cluster in a process whose whole job is producing, which is a privilege nobody should have
// to grant a producer. Applying a schema is an operator's action, not a process's.
//
// # It is idempotent, and that is a requirement rather than a nicety
//
// Kafka answers TOPIC_ALREADY_EXISTS for a topic that is already there, which this treats as
// success. Running it twice, or on every deploy, is the intended usage.
//
// What it will not do is change an existing topic. A partition count that disagrees with
// [events.TopicPartitions] is **reported and refused**, not corrected: adding partitions rehashes
// every key onto a different partition, which silently ends the per-aggregate ordering the
// publisher's whole design exists to keep. That is a decision with a migration behind it, and this
// command's job is to say the topic is wrong rather than to make it differently wrong.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/segmentio/kafka-go"

	"github.com/DulsaraNethmin/Shipper/services/core/internal/config"
	"github.com/DulsaraNethmin/Shipper/services/core/internal/events"
)

// applyTimeout bounds the whole run. Topic creation is a controller operation and a broker with a
// controller election in progress can take seconds; anything past this is a broker problem.
const applyTimeout = 30 * time.Second

func main() {
	if err := run(os.Args[1:], os.Stdout); err != nil {
		fmt.Fprintf(os.Stderr, "shipper-topics: %v\n", err)
		os.Exit(1)
	}
}

func run(args []string, out io.Writer) error {
	fs := flag.NewFlagSet("topics", flag.ContinueOnError)
	fs.SetOutput(out)

	// The one genuinely environment-dependent number in the topic set, and a flag rather than
	// an entry in internal/config — see events.DefaultReplicationFactor for why.
	replication := fs.Int("replication", events.DefaultReplicationFactor,
		"replication factor for created topics; a deployed cluster wants 3")
	if err := fs.Parse(args); err != nil {
		return err
	}

	cfg, err := config.Load()
	if err != nil {
		return err
	}
	if len(cfg.Kafka.Brokers) == 0 {
		return errors.New("KAFKA_BROKERS is empty, so there is no cluster to apply the topic set to")
	}
	if *replication < 1 {
		return fmt.Errorf("-replication is %d; a topic needs at least one replica", *replication)
	}

	ctx, cancel := context.WithTimeout(context.Background(), applyTimeout)
	defer cancel()

	client := &kafka.Client{Addr: kafka.TCP(cfg.Kafka.Brokers...), Timeout: applyTimeout}

	fmt.Fprintf(out, "applying %d topic(s) to %s\n",
		len(events.Topics()), strings.Join(cfg.Kafka.Brokers, ","))

	created, err := create(ctx, client, plan(*replication))
	if err != nil {
		return err
	}
	for _, topic := range events.Topics() {
		if created[topic] {
			fmt.Fprintf(out, "  %-20s created  (%d partitions, replication %d, retention %s)\n",
				topic, events.TopicPartitions, *replication, events.TopicRetention)
		} else {
			fmt.Fprintf(out, "  %-20s exists\n", topic)
		}
	}

	if err := verify(ctx, client, events.Topics()); err != nil {
		return err
	}
	fmt.Fprintf(out, "every topic exists with %d partitions\n", events.TopicPartitions)
	return nil
}

// plan is the topic set as Kafka wants it, derived from the catalogue.
//
// Separated from the call so that a test can read the whole intention — the topic names, the
// partition count, the configuration entries — without a broker. What is in here is what cannot be
// changed afterwards, which is exactly the part worth holding to a test.
func plan(replication int) []kafka.TopicConfig {
	topics := events.Topics()
	out := make([]kafka.TopicConfig, 0, len(topics))
	for _, topic := range topics {
		out = append(out, kafka.TopicConfig{
			Topic:             topic,
			NumPartitions:     events.TopicPartitions,
			ReplicationFactor: replication,
			ConfigEntries: []kafka.ConfigEntry{
				// Stated rather than inherited, so the topic set is reproducible on a
				// broker whose defaults are not this one's.
				{
					ConfigName:  "retention.ms",
					ConfigValue: strconv.FormatInt(events.TopicRetention.Milliseconds(), 10),
				},
			},
		})
	}
	return out
}

// create applies the plan and reports which topics it made, treating "already exists" as success.
func create(ctx context.Context, client *kafka.Client, topics []kafka.TopicConfig) (map[string]bool, error) {
	res, err := client.CreateTopics(ctx, &kafka.CreateTopicsRequest{Topics: topics})
	if err != nil {
		return nil, fmt.Errorf("creating topics: %w", err)
	}

	created := map[string]bool{}
	var failures []string
	for topic, topicErr := range res.Errors {
		switch {
		case topicErr == nil:
			created[topic] = true
		case errors.Is(topicErr, kafka.TopicAlreadyExists):
			// The ordinary case on every run after the first, and on every deployment.
		default:
			failures = append(failures, fmt.Sprintf("%s: %v", topic, topicErr))
		}
	}
	if len(failures) > 0 {
		sort.Strings(failures)
		return nil, fmt.Errorf("could not create %s", strings.Join(failures, "; "))
	}
	return created, nil
}

// verify reads the cluster back and refuses a topic that is not the shape the catalogue asks for.
//
// Reading back rather than trusting the create is the point of the command being a step somebody
// runs: it is also the check that a topic somebody made by hand — with one partition, in a hurry,
// to unblock something — is found here rather than by a consumer noticing its ordering is gone.
func verify(ctx context.Context, client *kafka.Client, want []string) error {
	meta, err := client.Metadata(ctx, &kafka.MetadataRequest{Topics: want})
	if err != nil {
		return fmt.Errorf("reading topic metadata back: %w", err)
	}

	partitions := map[string]int{}
	for _, t := range meta.Topics {
		if t.Error != nil {
			return fmt.Errorf("%s did not come back from the broker: %w", t.Name, t.Error)
		}
		partitions[t.Name] = len(t.Partitions)
	}

	var wrong []string
	for _, topic := range want {
		switch n, ok := partitions[topic]; {
		case !ok:
			wrong = append(wrong, topic+" does not exist")
		case n != events.TopicPartitions:
			wrong = append(wrong, fmt.Sprintf(
				"%s has %d partitions and the catalogue asks for %d",
				topic, n, events.TopicPartitions))
		}
	}
	if len(wrong) > 0 {
		return fmt.Errorf("%s.\nPartitions can be added and never removed, and adding one "+
			"rehashes every key onto a different partition, which ends the per-aggregate "+
			"ordering the outbox publisher depends on. Fix this deliberately rather than by "+
			"re-running this command", strings.Join(wrong, "; "))
	}
	return nil
}
