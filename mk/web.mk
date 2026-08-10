# Web surfaces — the admin panel and the driver portal (SHIP-22, SHIP-23).
#
# Included by the root Makefile's `-include mk/*.mk`, so this track adds its targets without
# editing a file three tracks may have open (Docs/10 §9.2). `make help` greps
# $(MAKEFILE_LIST), which covers included files, so everything documented here appears in
# the listing with no further work.
#
# Nothing here names an application. `pnpm -r` walks the workspace, so a third web surface
# is a line in pnpm-workspace.yaml and no edit to this file.

# pnpm is not installed globally and is not expected to be. Corepack ships with Node and
# reads the packageManager field in the root package.json, so every machine and every CI
# runner uses the same pnpm version without anybody installing one.
PNPM ?= corepack pnpm

.PHONY: web-install
web-install: ## Install the web workspace exactly as the lockfile pins it
	$(PNPM) install --frozen-lockfile

.PHONY: web-build
web-build: ## Build every web application (SHIP-22, SHIP-23)
	$(PNPM) -r run build

.PHONY: web-lint
web-lint: ## Run ESLint across the web applications
	$(PNPM) -r run lint

.PHONY: web-typecheck
web-typecheck: ## Type-check the web applications with TypeScript in strict mode
	$(PNPM) -r run typecheck

.PHONY: web-check
web-check: web-lint web-typecheck web-build ## Everything CI should run for the web surfaces

# One target rather than one per application, so adding a surface needs no edit here.
#
#   make web-dev app=admin          http://localhost:3001
#   make web-dev app=driver-portal  http://localhost:3002
#
# The ports are pinned per application in each package.json rather than left to Next's
# "3000, or the next free one", which silently moves when two are running at once.
.PHONY: web-dev
web-dev: ## Run one web application in development, e.g. `make web-dev app=admin`
	@test -n "$(app)" || { echo "usage: make web-dev app=<admin|driver-portal>"; exit 1; }
	@test -d "apps/$(app)" || { echo "no such application: apps/$(app)"; exit 1; }
	$(PNPM) --filter ./apps/$(app) run dev
