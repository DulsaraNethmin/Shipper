# The demonstration dataset (SHIP-186).
#
# Included by the root Makefile's `-include mk/*.mk`, so this track adds its targets without
# editing a file three tracks may have open (Docs/10 §9.2), exactly as mk/topics.mk does. `make
# help` greps $(MAKEFILE_LIST), which covers included files, so these appear in the listing with
# no further work.
#
# CORE and LDFLAGS come from the root Makefile. So do SEED_USER_PASSWORD and SEED_ADMIN_PASSWORD
# if they are set: the root Makefile's bare `export` passes every variable through to recipes,
# whether it came from deploy/.env or from the surrounding shell.
#
# # It needs a running API, which `migrate-up` and `topics` do not
#
# This is the one difference from the two targets it is modelled on, and it follows from what the
# seed is rather than from how it was built. A schema and a topic set are applied *to*
# infrastructure; a dataset is made *by* the product — every job here was published by a customer
# and every milestone recorded by a driver, through the endpoints they use. cmd/seed's header
# carries the argument for why that is the right shape and what the alternatives cost.
#
# So locally the order is:
#
#     make up && make migrate-up && make run          # in one terminal
#     SEED_USER_PASSWORD=… SEED_ADMIN_PASSWORD=… make seed
#
# and in a deployment it is the step after the API is serving rather than the step before.
#
# # Neither password has a default and the target does not invent one
#
# `make seed` with neither set fails with a message naming them. That is deliberate and cmd/seed's
# `credentials` type carries the reasoning: a committed default would be a working credential for
# every demonstration instance ever deployed from this repository, and M8's whole point is that the
# instance is reachable at a public hostname.

.PHONY: seed
seed: ## Populate the demonstration dataset — needs a running API (SHIP-186)
	cd $(CORE) && go run ./cmd/seed

.PHONY: seed-build
seed-build: ## Build bin/shipper-seed
	mkdir -p bin
	cd $(CORE) && go build -ldflags "$(LDFLAGS)" -o ../../bin/shipper-seed ./cmd/seed
	@echo "built bin/shipper-seed"
