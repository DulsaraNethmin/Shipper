# Shipper — development commands.
#
# Everything runs from the repository root. The local stack runs in containers; the Go
# service runs on the host, which is why every container publishes its port.
#
#   make up && make migrate-up && make run
#
# Local overrides live in deploy/.env (gitignored). deploy/.env.example documents every
# variable and its default.

SHELL := /bin/bash
.DEFAULT_GOAL := help

CORE    := services/core
COMPOSE := docker compose -f deploy/docker-compose.yml

# Pinned, not left to default.
#
# Compose names a project after the directory it runs in. Work happening in a git worktree —
# which is how two branches are built at once — runs from a differently named directory, so each
# worktree would silently start its own Postgres, Redis and Kafka and then fight the others for
# ports 5432, 6379 and 29092. Pinning the name means every worktree shares one stack, and
# isolation comes from separate test databases instead (Docs/10 §7.1).
COMPOSE_PROJECT_NAME := shipper
export COMPOSE_PROJECT_NAME

# deploy/.env is optional: the defaults below match the compose defaults, so a fresh
# clone works without creating it. `export` passes everything through to recipes, which
# is how the Go service and the migration tool receive their configuration.
-include deploy/.env
export

POSTGRES_USER     ?= shipper
POSTGRES_PASSWORD ?= shipper
POSTGRES_DB       ?= shipper
POSTGRES_PORT     ?= 5432
REDIS_PORT        ?= 6379
KAFKA_PORT        ?= 29092
HTTP_PORT         ?= 8080

DATABASE_URL  ?= postgres://$(POSTGRES_USER):$(POSTGRES_PASSWORD)@localhost:$(POSTGRES_PORT)/$(POSTGRES_DB)?sslmode=disable
REDIS_URL     ?= redis://localhost:$(REDIS_PORT)/0
KAFKA_BROKERS ?= localhost:$(KAFKA_PORT)

# Homebrew keeps libpq unlinked because it collides with a full PostgreSQL install, so
# psql is frequently present but not on PATH. Find it rather than asking every developer
# to edit their shell profile.
PSQL := $(shell command -v psql 2>/dev/null || echo "$$(brew --prefix libpq 2>/dev/null)/bin/psql")

# Build stamps read by internal/buildinfo and served from GET /health (SHIP-6).
VERSION  ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
COMMIT   ?= $(shell git rev-parse HEAD 2>/dev/null || echo unknown)
BUILT_AT ?= $(shell date -u +%Y-%m-%dT%H:%M:%SZ)

BUILDINFO := github.com/DulsaraNethmin/Shipper/services/core/internal/buildinfo
LDFLAGS   := -X $(BUILDINFO).version=$(VERSION) -X $(BUILDINFO).commit=$(COMMIT) -X $(BUILDINFO).builtAt=$(BUILT_AT)

KAFKA_BIN := /opt/kafka/bin

.PHONY: help
help: ## Show this help
	@echo "Shipper — make targets"
	@echo
	@grep -hE '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) \
		| awk 'BEGIN {FS = ":.*?## "}; {printf "  \033[36m%-16s\033[0m %s\n", $$1, $$2}'
	@echo

# --- Local stack (SHIP-2, SHIP-3, SHIP-4) ---------------------------------------------

.PHONY: up
up: ## Start Postgres, Redis and Kafka, waiting until each is healthy
	$(COMPOSE) up -d --wait

.PHONY: down
down: ## Stop the stack, keeping data
	$(COMPOSE) down

.PHONY: reset
reset: ## Stop the stack and destroy all data
	$(COMPOSE) down -v

.PHONY: ps
ps: ## Show stack status
	$(COMPOSE) ps

.PHONY: logs
logs: ## Follow stack logs
	$(COMPOSE) logs -f

.PHONY: psql
psql: ## Open a psql shell against the local database
	@test -x "$(PSQL)" || { echo "psql not found. Install it with: brew install libpq"; exit 1; }
	"$(PSQL)" "$(DATABASE_URL)"

.PHONY: redis
redis: ## Open a redis-cli shell against the local cache
	redis-cli -u "$(REDIS_URL)"

.PHONY: kafka-smoke
kafka-smoke: ## Create a topic, produce a message, consume it, delete the topic (SHIP-4)
	@set -euo pipefail; \
	topic=shipper.smoke; \
	echo "--> creating $$topic"; \
	$(COMPOSE) exec -T kafka $(KAFKA_BIN)/kafka-topics.sh --bootstrap-server localhost:9092 \
		--create --if-not-exists --topic $$topic --partitions 1 --replication-factor 1; \
	echo "--> producing"; \
	echo "shipper-smoke-payload" | $(COMPOSE) exec -T kafka $(KAFKA_BIN)/kafka-console-producer.sh \
		--bootstrap-server localhost:9092 --topic $$topic; \
	echo "--> consuming"; \
	$(COMPOSE) exec -T kafka $(KAFKA_BIN)/kafka-console-consumer.sh --bootstrap-server localhost:9092 \
		--topic $$topic --from-beginning --max-messages 1 --timeout-ms 20000; \
	echo "--> deleting $$topic"; \
	$(COMPOSE) exec -T kafka $(KAFKA_BIN)/kafka-topics.sh --bootstrap-server localhost:9092 \
		--delete --topic $$topic; \
	echo "kafka smoke test passed"

# --- Migrations (SHIP-7) --------------------------------------------------------------

.PHONY: migrate-up
migrate-up: ## Apply all pending migrations
	cd $(CORE) && go run ./cmd/migrate up

.PHONY: migrate-down
migrate-down: ## Reverse the last migration (use `make migrate-down n=all` for everything)
	cd $(CORE) && go run ./cmd/migrate down $(n)

.PHONY: migrate-version
migrate-version: ## Print the current schema version
	cd $(CORE) && go run ./cmd/migrate version

.PHONY: migrate-create
migrate-create: ## Write the next migration pair, e.g. `make migrate-create name=users_table domain=shared`
	@test -n "$(name)" || { echo "usage: make migrate-create name=<snake_case_name> domain=<domain>"; exit 1; }
	@test -n "$(domain)" || { echo "usage: make migrate-create name=<snake_case_name> domain=<domain>"; \
		echo "domain picks the reserved number block — see services/core/migrations/blocks.go"; exit 1; }
	cd $(CORE) && go run ./cmd/migrate create $(name) -domain $(domain)

# --- Go service (SHIP-5, SHIP-6, SHIP-8, SHIP-9) --------------------------------------

.PHONY: run
run: ## Run the API on the host
	cd $(CORE) && go run -ldflags "$(LDFLAGS)" ./cmd/api

.PHONY: build
build: ## Build the API and migration binaries into bin/
	mkdir -p bin
	cd $(CORE) && go build -ldflags "$(LDFLAGS)" -o ../../bin/shipper-api ./cmd/api
	cd $(CORE) && go build -ldflags "$(LDFLAGS)" -o ../../bin/shipper-migrate ./cmd/migrate
	@echo "built bin/shipper-api and bin/shipper-migrate"

TEST_TEMPLATE_DB ?= shipper_test_template
TEST_DATABASE_URL ?= $(DATABASE_URL)
export TEST_TEMPLATE_DB
export TEST_DATABASE_URL

.PHONY: test-db-template
test-db-template: ## Build the template database every integration test is cloned from
	@# Dropped and rebuilt rather than migrated in place: a template that has drifted from
	@# the migration chain produces failures in whichever test happens to touch the drift,
	@# which is a long way from the cause. Rebuilding takes a second and is unambiguous.
	@# is_template must come off before the drop: PostgreSQL refuses to drop a template.
	@# This runs unguarded because the database legitimately may not exist yet.
	@$(PSQL) "$(DATABASE_URL)" -q -c \
		"ALTER DATABASE $(TEST_TEMPLATE_DB) WITH is_template = false" >/dev/null 2>&1 || true
	@$(PSQL) "$(DATABASE_URL)" -v ON_ERROR_STOP=1 -q -c \
		"DROP DATABASE IF EXISTS $(TEST_TEMPLATE_DB) WITH (FORCE)" >/dev/null
	@$(PSQL) "$(DATABASE_URL)" -v ON_ERROR_STOP=1 -q -c \
		"CREATE DATABASE $(TEST_TEMPLATE_DB)" >/dev/null
	@cd $(CORE) && DATABASE_URL="$(subst /$(POSTGRES_DB)?,/$(TEST_TEMPLATE_DB)?,$(DATABASE_URL))" \
		go run ./cmd/migrate up >/dev/null
	@$(PSQL) "$(DATABASE_URL)" -v ON_ERROR_STOP=1 -q -c \
		"ALTER DATABASE $(TEST_TEMPLATE_DB) WITH is_template = true" >/dev/null
	@echo "template $(TEST_TEMPLATE_DB) is at the current schema"

.PHONY: test
test: test-db-template ## Run the Go tests
	cd $(CORE) && go test ./... -race -count=1 -p 4

.PHONY: vet
vet: ## Run go vet
	cd $(CORE) && go vet ./...

.PHONY: lint-imports
lint-imports: ## Check the domain boundaries from Docs/06 §4.1 (SHIP-11)
	cd $(CORE) && go run ./cmd/lintboundaries

.PHONY: fmt
fmt: ## Format the Go code
	cd $(CORE) && go fmt ./...

.PHONY: tidy
tidy: ## Tidy go.mod and go.sum
	cd $(CORE) && go mod tidy

.PHONY: lint-spelling
lint-spelling: ## Check Australian English (CLAUDE.md, Docs/10 §9.3)
	@./scripts/check-spelling.sh

.PHONY: check
check: vet lint-imports lint-spelling test ## Everything CI will run for the Go service (SHIP-20)

# --- Acceptance -----------------------------------------------------------------------

.PHONY: verify
verify: ## Demonstrate the SHIP-1..15 acceptance criteria end to end
	./scripts/verify-foundation.sh

# --- Per-track targets ------------------------------------------------------------------
#
# Included last, and optional. A track working on Flutter, the web surfaces or the store
# pipelines adds mk/<track>.mk and gets its targets without editing this file — which matters
# because this file is shared and three tracks may be open at once (Docs/10 §9.2).
#
# `make help` greps $(MAKEFILE_LIST), which includes included files, so documented targets in
# an mk/ file appear in the listing with no further work.
-include mk/*.mk
