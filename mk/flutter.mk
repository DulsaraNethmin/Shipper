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

.PHONY: flutter-check
flutter-check: flutter-analyze flutter-test ## What CI runs for the client (SHIP-21)

.PHONY: flutter-run
flutter-run: ## Run the client on the attached device, e.g. `make flutter-run d=emulator-5554`
	cd $(MOBILE) && $(FLUTTER) run $(if $(d),-d $(d),)

.PHONY: flutter-build
flutter-build: ## Build a debug APK and a simulator .app, proving both platforms compile
	cd $(MOBILE) && $(FLUTTER) build apk --debug
	cd $(MOBILE) && $(FLUTTER) build ios --simulator --debug --no-codesign
