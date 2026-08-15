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
//
// # The replication factor is read back too (SHIP-134a)
//
// It was not, and the gap was invisible because the only thing that ever exercised the refusal was
// a harness section that broke a topic on purpose to watch it complain. Creation carries the factor
// — [plan] puts it on every [kafka.TopicConfig] — so a topic *this command* made has the right one;
// a topic somebody made by hand does not, and that is the same failure the partition check exists
// for. A topic created with one replica on a cluster that has three brokers survives a single
// machine going away exactly as well as no topic at all, and nothing before this said so.
//
// It is refused rather than repaired for the same reason and a different mechanism: changing a
// replication factor is a partition reassignment, which moves data between brokers under load and
// is an operator's decision with a maintenance window behind it, not a side effect of applying a
// schema.
//
// **It is also what lets the acceptance harness demonstrate the refusal without destroying
// anything.** `scripts/verify/80-notifications.sh` used to delete `shipper.delivery` and recreate
// it with one partition — on a broker every git worktree on the machine shares, which made this
// repository's own harness the cause of the failure mode SHIP-134a exists to close. Asking for a
// replication factor the local stack cannot be holding drifts the *request* rather than the
// cluster, so the refusal is demonstrated end to end against a topic set nothing has touched.
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

// replicationFlag is the operator's override of KAFKA_REPLICATION_FACTOR.
const replicationFlag = "replication"

// chosenReplication picks the flag when the operator gave one and the configuration otherwise
// (SHIP-15m).
//
// It asks which flags were **set** rather than which are non-zero, and that distinction is the
// whole function. A flag's default cannot be the configured value — the flag set is built before
// config.Load runs — so "was it given" has to come from fs.Visit. Treating zero as "not given"
// instead would make `-replication 0` silently apply the configured factor, when an operator who
// typed a number meant it and is owed the refusal that follows.
func chosenReplication(fs *flag.FlagSet, flagValue, configured int) int {
	chosen := configured
	fs.Visit(func(f *flag.Flag) {
		if f.Name == replicationFlag {
			chosen = flagValue
		}
	})
	return chosen
}

func main() {
	if err := run(os.Args[1:], os.Stdout); err != nil {
		fmt.Fprintf(os.Stderr, "shipper-topics: %v\n", err)
		os.Exit(1)
	}
}

func run(args []string, out io.Writer) error {
	fs := flag.NewFlagSet("topics", flag.ContinueOnError)
	fs.SetOutput(out)

	// The one genuinely environment-dependent number in the topic set. It is configuration —
	// KAFKA_REPLICATION_FACTOR, folded in at SHIP-15m — and this flag overrides it.
	//
	// **Both, rather than one.** A deployment sets the variable once and runs the step with no
	// arguments, the same way it runs cmd/migrate; an operator applying the set to one cluster
	// by hand types the number and needs nothing else in their environment. The flag declares no
	// default of its own because the default is the configuration, which is not loaded yet when
	// the flag set is built — see chosenReplication.
	replication := fs.Int(replicationFlag, 0,
		"replication factor for created topics, overriding KAFKA_REPLICATION_FACTOR; "+
			"a deployed cluster wants 3")
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

	factor := chosenReplication(fs, *replication, cfg.Kafka.ReplicationFactor)

	// Only reachable from the flag: config.Load refuses a variable below one, with the variable
	// named. So the message names the flag.
	if factor < 1 {
		return fmt.Errorf("-replication is %d; a topic needs at least one replica", factor)
	}

	ctx, cancel := context.WithTimeout(context.Background(), applyTimeout)
	defer cancel()

	client := &kafka.Client{Addr: kafka.TCP(cfg.Kafka.Brokers...), Timeout: applyTimeout}

	fmt.Fprintf(out, "applying %d topic(s) to %s\n",
		len(events.Topics()), strings.Join(cfg.Kafka.Brokers, ","))

	created, err := create(ctx, client, plan(factor))
	if err != nil {
		return err
	}
	for _, topic := range events.Topics() {
		if created[topic] {
			fmt.Fprintf(out, "  %-20s created  (%d partitions, replication %d, retention %s)\n",
				topic, events.TopicPartitions, factor, events.TopicRetention)
		} else {
			fmt.Fprintf(out, "  %-20s exists\n", topic)
		}
	}

	if err := verify(ctx, client, events.Topics(), factor); err != nil {
		return err
	}
	fmt.Fprintf(out, "every topic exists with %d partitions and %d replica(s)\n",
		events.TopicPartitions, factor)
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

// shape is what a topic on the cluster actually is, as against what the catalogue asks for.
//
// Two numbers rather than the whole [kafka.Topic], because those are the two this command has an
// opinion about and separating them from the broker call is what lets [refuseDrift] be tested on
// every machine with no cluster attached — which is where the partition branch is now held, since
// SHIP-134a stopped the acceptance harness breaking a real topic to reach it.
type shape struct {
	partitions  int
	replication int
}

// verify reads the cluster back and refuses a topic that is not the shape the catalogue asks for.
//
// Reading back rather than trusting the create is the point of the command being a step somebody
// runs: it is also the check that a topic somebody made by hand — with one partition, in a hurry,
// to unblock something — is found here rather than by a consumer noticing its ordering is gone.
func verify(ctx context.Context, client *kafka.Client, want []string, replication int) error {
	observed, err := describe(ctx, client, want)
	if err != nil {
		return err
	}
	return refuseDrift(want, observed, replication)
}

// describe is the broker half: the metadata call and nothing else.
func describe(ctx context.Context, client *kafka.Client, want []string) (map[string]shape, error) {
	meta, err := client.Metadata(ctx, &kafka.MetadataRequest{Topics: want})
	if err != nil {
		return nil, fmt.Errorf("reading topic metadata back: %w", err)
	}

	observed := map[string]shape{}
	for _, t := range meta.Topics {
		if t.Error != nil {
			return nil, fmt.Errorf("%s did not come back from the broker: %w", t.Name, t.Error)
		}

		// The assigned replica list rather than the in-sync one. A broker that is down
		// shrinks `Isr` and leaves `Replicas` alone, so this reports how the topic was
		// *created* — which is what drifted — rather than how the cluster is feeling.
		//
		// The minimum across partitions, because a reassignment interrupted halfway leaves
		// some partitions at the new factor and some at the old, and the weakest partition is
		// what the topic actually guarantees.
		lowest := 0
		for i, p := range t.Partitions {
			if i == 0 || len(p.Replicas) < lowest {
				lowest = len(p.Replicas)
			}
		}
		observed[t.Name] = shape{partitions: len(t.Partitions), replication: lowest}
	}
	return observed, nil
}

// refuseDrift is the decision half: what the cluster holds against what was asked for.
//
// It repairs nothing and says so, and the two reasons differ. A partition count can be *increased*
// and never reduced, and increasing it rehashes every key onto a different partition — which ends
// the per-aggregate ordering the outbox publisher's whole design keeps. A replication factor can be
// changed in either direction, and doing so is a partition reassignment that moves data between
// brokers under load. The first is irreversible and the second is expensive; neither is something a
// command that applies a schema should do to a cluster on its way past.
func refuseDrift(want []string, observed map[string]shape, replication int) error {
	var wrong []string
	for _, topic := range want {
		got, ok := observed[topic]
		if !ok {
			wrong = append(wrong, topic+" does not exist")
			continue
		}
		if got.partitions != events.TopicPartitions {
			wrong = append(wrong, fmt.Sprintf(
				"%s has %d partitions and the catalogue asks for %d",
				topic, got.partitions, events.TopicPartitions))
		}
		if got.replication != replication {
			wrong = append(wrong, fmt.Sprintf(
				"%s has a replication factor of %d and this run asks for %d",
				topic, got.replication, replication))
		}
	}
	if len(wrong) == 0 {
		return nil
	}
	return fmt.Errorf("%s.\nNothing here is repaired. Partitions can be added and never removed, "+
		"and adding one rehashes every key onto a different partition, which ends the "+
		"per-aggregate ordering the outbox publisher depends on; a replication factor is changed "+
		"by a partition reassignment, which moves data between brokers and is an operator's "+
		"decision with a maintenance window behind it. Fix this deliberately rather than by "+
		"re-running this command", strings.Join(wrong, "; "))
}
