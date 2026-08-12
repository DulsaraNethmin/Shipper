package main

import (
	"strings"
	"testing"

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
// cluster, and it is a flag rather than an entry in internal/config — see
// events.DefaultReplicationFactor for why.
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
