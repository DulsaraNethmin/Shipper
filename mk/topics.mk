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
# A deployed cluster wants three replicas rather than one:
#
#     go run ./cmd/topics -replication 3
#
# which is a flag rather than an entry in internal/config, because internal/config is a shared
# surface — see events.DefaultReplicationFactor.

.PHONY: topics
topics: ## Apply the Kafka topic set — one topic per aggregate (SHIP-135)
	cd $(CORE) && go run ./cmd/topics

.PHONY: topics-build
topics-build: ## Build bin/shipper-topics
	mkdir -p bin
	cd $(CORE) && go build -ldflags "$(LDFLAGS)" -o ../../bin/shipper-topics ./cmd/topics
	@echo "built bin/shipper-topics"
