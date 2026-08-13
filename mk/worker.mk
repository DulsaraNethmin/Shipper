# The background worker (SHIP-67a).
#
# Included by the root Makefile's `-include mk/*.mk`, so this track adds its targets without
# editing a file three tracks may have open (Docs/10 §9.2). `make help` greps $(MAKEFILE_LIST),
# which covers included files, so everything documented here appears in the listing.
#
# The root `build` target still builds the API and the migration tool only. Adding a third line
# to it would have been the smaller change and the wrong one this wave: it is a shared surface,
# and `worker-build` costs nothing to keep here until somebody who owns that file folds it in.
#
# CORE and LDFLAGS come from the root Makefile — make variables are global, and this file is
# included after they are set.

.PHONY: worker-run
worker-run: ## Run the scheduled task worker on the host (SHIP-67a)
	cd $(CORE) && go run -ldflags "$(LDFLAGS)" ./cmd/worker

.PHONY: worker-build
worker-build: ## Build bin/shipper-worker
	mkdir -p bin
	cd $(CORE) && go build -ldflags "$(LDFLAGS)" -o ../../bin/shipper-worker ./cmd/worker
	@echo "built bin/shipper-worker"
