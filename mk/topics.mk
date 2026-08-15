# The Kafka topic set (SHIP-135).
#
# Included by the root Makefile's `-include mk/*.mk`, so this track adds its target without
# editing a file three tracks may have open (Docs/10 §9.2). `make help` greps $(MAKEFILE_LIST),
# which covers included files, so this appears in the listing with no further work.
#
# CORE, LDFLAGS and KAFKA_BROKERS come from the root Makefile — make variables are global, and
# this file is included after they are set.
#
# `topics` is to Kafka what `migrate-up` is to PostgreSQL: a schema, applied by a step somebody
# runs, from a declaration in the code. It is idempotent, so running it on every deploy and twice
# by accident are both fine. cmd/topics carries the argument for why it is a command rather than
# something the compose stack or the worker's start-up does.
#
# A deployed cluster wants three replicas rather than one, and sets it as configuration:
#
#     KAFKA_REPLICATION_FACTOR=3
#
# with the flag kept as an operator override for a one-off run:
#
#     go run ./cmd/topics -replication 3
#
# This paragraph said the opposite until the wave-10 reconciliation — "a flag rather than an entry
# in internal/config, because internal/config is a shared surface". That was true when SHIP-135
# wrote it and false from SHIP-15m onwards, which folded the setting in: it is
# config.Kafka.ReplicationFactor, documented in deploy/.env.example, and cmd/topics reconciles the
# two in chosenReplication with the flag winning. The reasoning the old sentence recorded is worth
# keeping even though its conclusion flipped — a track parking work against a shared surface is
# exactly what a prep ticket exists to absorb, and this one was absorbed rather than left standing.
# See config.DefaultKafkaReplicationFactor and events.DefaultReplicationFactor, which a test in
# cmd/topics holds equal so the catalogue and the configuration cannot drift apart.

.PHONY: topics
topics: ## Apply the Kafka topic set — one topic per aggregate (SHIP-135)
	cd $(CORE) && go run ./cmd/topics

.PHONY: topics-build
topics-build: ## Build bin/shipper-topics
	mkdir -p bin
	cd $(CORE) && go build -ldflags "$(LDFLAGS)" -o ../../bin/shipper-topics ./cmd/topics
	@echo "built bin/shipper-topics"
