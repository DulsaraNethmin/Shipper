# Status enumeration codegen (SHIP-56a).
#
# Included by the root Makefile's `-include mk/*.mk`, so this edits nothing shared — the same
# mechanism mk/flutter.mk and mk/web.mk already use (Docs/10 §9.2). `make help` greps
# $(MAKEFILE_LIST), which covers included files, so the target below appears in the listing.
#
# `codegen` is the name Docs/10 §8.2 uses. It is deliberately unprefixed, unlike `flutter-codegen`
# beside it, because it is not one track's: it writes into services/core, apps/mobile and
# apps/driver-portal from one specification.

.PHONY: codegen
codegen: ## Write contracts/statuses.yaml into Go, Dart and TypeScript (SHIP-56a)
	cd $(CORE) && go run ./cmd/statusgen -root ../..

# Deliberately **not** added to CHECKS, and the reason is the whole second half of SHIP-56a's
# acceptance criterion.
#
# A target that regenerates is not a check that CI fails on. What catches a stale file is
# TestGeneratedFilesAreCurrent in services/core/cmd/statusgen: it renders the specification in
# memory and compares it with what is committed, so it already runs under `go test ./...` — which
# is to say under `make test`, under `make check`, and in the Go workflow.
#
# Running the generator from a check would be worse than redundant. It rewrites the working tree,
# and CLAUDE.md records what a tree-rewriting gate did to wave 5: a false failure on a tree where
# nothing was wrong, and a false pass that shipped. A test that only reads cannot do either, and it
# additionally fails on a generated file that has been deleted — which a `git diff` of tracked
# files does not see at all.
#
# The one thing the target is for is fixing what the test found:
#
#     make codegen && git diff
.PHONY: codegen-check
codegen-check: ## Report stale generated files without writing any (the test is the real gate)
	cd $(CORE) && go run ./cmd/statusgen -root ../.. -check
