# The Flutter client (SHIP-16 onwards).
#
# Included by the root Makefile's `-include mk/*.mk`, so this track adds its targets without
# editing a file three tracks may have open (Docs/10 §9.2). `make help` greps
# $(MAKEFILE_LIST), which covers included files, so everything documented here is listed
# automatically.
#
# Every target is prefixed `flutter-` because the root Makefile already owns `build`, `run`,
# `test` and `check` for the Go service, and a make target silently redefined by an include
# is a bad afternoon.

MOBILE := apps/mobile
FLUTTER ?= flutter

.PHONY: flutter-deps
flutter-deps: ## Resolve Dart package dependencies exactly as pubspec.lock pins them
	cd $(MOBILE) && $(FLUTTER) pub get

.PHONY: flutter-analyze
flutter-analyze: ## Static analysis; fails on any analyzer error, warning or lint
	cd $(MOBILE) && $(FLUTTER) analyze

.PHONY: flutter-test
flutter-test: ## Run the Dart tests, including the feature-boundary test
	cd $(MOBILE) && $(FLUTTER) test

# SHIP-18's acceptance criterion is that the client targets three environments *by build
# flavour*. Nothing a single test run can assert proves that, because a single run compiles
# with one set of defines — so the same test file runs three times, once per environment, and
# checks that what arrived is what was asked for.
.PHONY: flutter-test-defines
flutter-test-defines: ## Prove --dart-define selects each environment (SHIP-18)
	@for env in local staging production; do \
		echo "  SHIPPER_ENV=$$env"; \
		cd $(MOBILE) && $(FLUTTER) test --dart-define=SHIPPER_ENV=$$env \
			test/core/api/api_environment_define_test.dart || exit 1; \
		cd - >/dev/null; \
	done
	@echo "  SHIPPER_API_PORT=$(HTTP_PORT)"
	@cd $(MOBILE) && $(FLUTTER) test --dart-define=SHIPPER_API_PORT=$(HTTP_PORT) \
		test/core/api/api_environment_define_test.dart

.PHONY: flutter-check
flutter-check: flutter-analyze flutter-test flutter-test-defines ## What CI runs for the client (SHIP-21)

# The build flavour, and the port the API is on. HTTP_PORT comes from deploy/.env, which is
# per-worktree — so `make flutter-run` points at *this* checkout's API without anybody editing
# Dart. The Android emulator's 10.0.2.2 is handled inside ApiEnvironment; only the port varies
# by machine.
flavour ?= local
FLUTTER_DEFINES := --dart-define=SHIPPER_ENV=$(flavour) --dart-define=SHIPPER_API_PORT=$(HTTP_PORT)

.PHONY: flutter-run
flutter-run: ## Run the client, e.g. `make flutter-run d=emulator-5554 flavour=staging`
	cd $(MOBILE) && $(FLUTTER) run $(if $(d),-d $(d),) $(FLUTTER_DEFINES)

.PHONY: flutter-build
flutter-build: ## Build a debug APK and a simulator .app, proving both platforms compile
	cd $(MOBILE) && $(FLUTTER) build apk --debug
	cd $(MOBILE) && $(FLUTTER) build ios --simulator --debug --no-codesign
