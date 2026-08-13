package main

import (
	"flag"
	"io"
	"strings"
	"testing"

	"github.com/DulsaraNethmin/Shipper/services/core/internal/config"
	"github.com/DulsaraNethmin/Shipper/services/core/internal/events"
)

// What this file tests and what it deliberately does not.
//
// The plan — which topics, how many partitions, what configuration — is the part that cannot be
// changed once applied, so it is the part worth holding to a test that runs on every machine with
// no broker attached. Whether Kafka accepts it, whether a second run is idempotent, and whether a
// message survives the round trip are demonstrated against the real broker in
// scripts/verify/80-notifications.sh, which is where a claim about a cluster belongs.

func TestThePlanIsTheCatalogueTopicSet(t *testing.T) {
	got := plan(events.DefaultReplicationFactor)

	want := events.Topics()
	if len(got) != len(want) {
		t.Fatalf("the plan has %d topic(s) and the catalogue names %d", len(got), len(want))
	}

	for i, topic := range want {
		if got[i].Topic != topic {
			t.Errorf("topic %d is %q, want %q", i, got[i].Topic, topic)
		}

		// Three, and it is the partition count rather than the replication factor that is
		// irreversible: partitions can be added and never removed, and adding one rehashes
		// every key onto a different partition — which silently ends the per-aggregate
		// ordering the outbox publisher's advisory locks exist to keep.
		if got[i].NumPartitions != events.TopicPartitions {
			t.Errorf("%s asks for %d partitions, want %d",
				topic, got[i].NumPartitions, events.TopicPartitions)
		}

		// Stated rather than inherited, so the topic set is reproducible on a broker whose
		// defaults are not the compose stack's.
		var retention string
		for _, entry := range got[i].ConfigEntries {
			if entry.ConfigName == "retention.ms" {
				retention = entry.ConfigValue
			}
		}
		if retention != "604800000" {
			t.Errorf("%s sets retention.ms to %q, want seven days", topic, retention)
		}
	}
}

// The replication factor is the one number here that differs between a compose stack and a
// cluster. It is KAFKA_REPLICATION_FACTOR, with the -replication flag as an operator override
// (SHIP-15m); whichever supplies it, it is what the plan asks the broker for.
func TestTheReplicationFactorIsWhatTheOperatorAsksFor(t *testing.T) {
	for _, replication := range []int{1, 3} {
		for _, topic := range plan(replication) {
			if topic.ReplicationFactor != replication {
				t.Errorf("%s would be created with %d replicas, want %d",
					topic.Topic, topic.ReplicationFactor, replication)
			}
		}
	}
}

func TestARunWithNoReplicasIsRefusedBeforeItReachesTheBroker(t *testing.T) {
	err := run([]string{"-replication", "0"}, &strings.Builder{})
	if err == nil {
		t.Fatal("accepted -replication 0")
	}
	if !strings.Contains(err.Error(), "at least one replica") {
		t.Errorf("refused with %q", err)
	}
}

// KAFKA_REPLICATION_FACTOR supplies the number and the flag overrides it (SHIP-15m).
//
// The three cases are the whole of the contract, and the third is the one a "zero means unset"
// implementation would get wrong: an explicit -replication 0 must reach the refusal above rather
// than silently applying the configured factor.
func TestTheFlagOverridesConfigurationAndConfigurationIsTheDefault(t *testing.T) {
	parse := func(t *testing.T, args ...string) (*flag.FlagSet, int) {
		t.Helper()
		fs := flag.NewFlagSet("topics", flag.ContinueOnError)
		fs.SetOutput(io.Discard)
		value := fs.Int(replicationFlag, 0, "")
		if err := fs.Parse(args); err != nil {
			t.Fatalf("parsing %v: %v", args, err)
		}
		return fs, *value
	}

	const configured = 3

	t.Run("no flag takes the configured factor", func(t *testing.T) {
		fs, value := parse(t)
		if got := chosenReplication(fs, value, configured); got != configured {
			t.Errorf("chose %d, want the configured %d", got, configured)
		}
	})

	t.Run("the flag wins", func(t *testing.T) {
		fs, value := parse(t, "-replication", "5")
		if got := chosenReplication(fs, value, configured); got != 5 {
			t.Errorf("chose %d, want the flag's 5", got)
		}
	})

	t.Run("an explicit zero is not mistaken for an absent flag", func(t *testing.T) {
		fs, value := parse(t, "-replication", "0")
		if got := chosenReplication(fs, value, configured); got != 0 {
			t.Errorf("chose %d, want 0 so that the run is refused rather than silently "+
				"applying the configured %d", got, configured)
		}
	})
}

// The two constants that hold the same number, checked where they meet.
//
// internal/config declares its own default rather than importing this package — config has no
// internal dependencies and every binary loads it, so importing the catalogue would pull the
// database driver into the configuration of processes that publish nothing. cmd/topics is the one
// place that imports both, which makes it the one place the copies can be held together.
func TestConfigurationCarriesTheCatalogueDefault(t *testing.T) {
	// Blanked rather than unset, which loader.lookup treats the same way, so a value in the
	// developer's environment cannot decide the outcome.
	t.Setenv("KAFKA_REPLICATION_FACTOR", "")

	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("loading configuration: %v", err)
	}

	if cfg.Kafka.ReplicationFactor != events.DefaultReplicationFactor {
		t.Errorf("KAFKA_REPLICATION_FACTOR defaults to %d and the catalogue says %d.\n"+
			"config.DefaultKafkaReplicationFactor and events.DefaultReplicationFactor are two "+
			"copies of one number and have drifted.",
			cfg.Kafka.ReplicationFactor, events.DefaultReplicationFactor)
	}
}
