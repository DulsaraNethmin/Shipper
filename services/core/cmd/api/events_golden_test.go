package main

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/DulsaraNethmin/Shipper/services/core/internal/events"
)

const eventsGoldenPath = "events_golden.txt"

// renderCatalogue is the whole event surface as one text table: topic, type, version, payload
// bound, and the payload's field set.
//
// One line per event, columns aligned, so a diff is readable at a glance — the same shape and the
// same reasoning as routes_golden.txt beside it.
func renderCatalogue() string {
	var b strings.Builder
	for _, s := range events.Catalogue() {
		fmt.Fprintf(&b, "%-18s %-22s v%-3d %-6d %s\n",
			s.Aggregate.Topic(), s.Type, s.Version, s.PayloadLimit(),
			strings.Join(s.Fields(), " "))
	}
	return b.String()
}

// TestEventCatalogueMatchesGolden renders every registered domain event and diffs it against a
// file in the repository (SHIP-135).
//
// # Why cmd/api holds it
//
// For the reason routes_golden.txt is here: a domain registers its own events from an init in its
// own package, so no single source file lists them, and cmd/api is the one binary that links every
// domain. This is the only place the whole surface exists at once.
//
// # What it is for, which is not "catching a typo"
//
// The failure worth preventing is a **stale event accepted as meaning something else** after a
// deployment — a consumer written against version 1 reading a version 1 label on a payload that
// has quietly changed shape. Nothing about that produces a compile error, a failing test, or a
// broker error: the message is valid JSON on the right topic, and every field the consumer reads
// is still there. It is the same class of silent loss routes_golden.txt exists for, and the same
// argument internal/pagination's cursor version prefix was built on (SHIP-66).
//
// Recording the field set beside the version turns it into a one-line diff. **A line whose fields
// changed without its `v` changing is the defect** — visible in review, beside the change that
// caused it, rather than in a consumer's logs a week later. Regenerating this file is therefore
// the moment to ask whether the version should go up, which is exactly when somebody is in a
// position to answer.
//
// Regenerate with `go test ./cmd/api -run TestEventCatalogueMatchesGolden -update`, and read the
// diff before committing it.
func TestEventCatalogueMatchesGolden(t *testing.T) {
	got := renderCatalogue()

	if *updateGolden {
		if err := os.WriteFile(eventsGoldenPath, []byte(got), 0o644); err != nil {
			t.Fatalf("writing %s: %v", eventsGoldenPath, err)
		}
		t.Logf("wrote %s", filepath.Clean(eventsGoldenPath))
		return
	}

	want, err := os.ReadFile(eventsGoldenPath)
	if err != nil {
		t.Fatalf("reading %s: %v\nregenerate with "+
			"`go test ./cmd/api -run TestEventCatalogueMatchesGolden -update`",
			eventsGoldenPath, err)
	}

	if got != string(want) {
		t.Errorf("the event catalogue does not match %s.\n\n"+
			"If a payload changed shape, ask whether the schema version should go up before "+
			"regenerating: a consumer reading v1 off a v1 label is entitled to the v1 shape.\n"+
			"    go test ./cmd/api -run TestEventCatalogueMatchesGolden -update\n\n"+
			"--- %s\n%s\n+++ actual\n%s", eventsGoldenPath, eventsGoldenPath, want, got)
	}
}

// TestNoDomainEventCarriesABudget is the budget-privacy invariant applied to the whole event
// surface rather than to one domain's tests.
//
// Docs/01 §4.3: a customer's budget is never exposed to a provider, as an amount, a band, or a
// "budget supplied" flag. An **event** is the worst place for it to appear, because it is a copy
// of a job that travels further than any endpoint — through the outbox, onto a topic, into every
// consumer there will ever be, and past every place a response body could have redacted it. By the
// time it is wrong it is in somebody else's database.
//
// internal/jobs already reads its own events back out of the outbox and fails on a budget
// (expiry_test.go, budget_test.go). This is the version of that check that covers bidding and
// delivery before they have written a line, and every event added after them: the catalogue is
// where an event's fields are recorded, so it is where the rule can be enforced once for all of
// them.
func TestNoDomainEventCarriesABudget(t *testing.T) {
	catalogue := events.Catalogue()
	if len(catalogue) == 0 {
		t.Fatal("the catalogue is empty, so this test proves nothing; cmd/api must link every " +
			"domain that emits")
	}

	for _, s := range catalogue {
		for _, field := range s.Fields() {
			name, _, _ := strings.Cut(field, ":")
			if strings.Contains(strings.ToLower(name), "budget") {
				t.Errorf("%s declares %q. Docs/01 §4.3 keeps the customer's maximum "+
					"private from providers, and an event reaches every consumer "+
					"there will ever be — there is no redaction point downstream of "+
					"this", s.Type, name)
			}
		}
	}
}

// TestEveryEventIsPublishable holds the catalogue to the properties cmd/topics and the outbox
// publisher assume of it.
//
// These are the conditions internal/events refuses at registration, restated over the surface that
// actually exists. Registration panics, so a violation cannot reach here — which makes this a
// statement about the whole catalogue being the thing that was checked, and the place a later
// aggregate arriving without a topic would be caught.
func TestEveryEventIsPublishable(t *testing.T) {
	topics := map[string]bool{}
	for _, topic := range events.Topics() {
		topics[topic] = true
	}

	seen := map[string]bool{}
	for _, s := range events.Catalogue() {
		if seen[s.Type] {
			t.Errorf("%s appears twice in the catalogue", s.Type)
		}
		seen[s.Type] = true

		if !topics[s.Aggregate.Topic()] {
			t.Errorf("%s is on aggregate %q, whose topic %s is not in the set cmd/topics "+
				"creates", s.Type, s.Aggregate, s.Aggregate.Topic())
		}
		if s.Version < 1 {
			t.Errorf("%s is at version %d", s.Type, s.Version)
		}
		if len(s.Fields()) == 0 {
			t.Errorf("%s has an empty payload shape, so a consumer has nothing to read", s.Type)
		}
		if s.PayloadLimit() >= events.BrokerMessageLimitBytes {
			t.Errorf("%s permits a %d byte payload against a broker limit of %d; an event "+
				"that can be refused permanently is the one the outbox has no "+
				"dead-letter path for", s.Type, s.PayloadLimit(), events.BrokerMessageLimitBytes)
		}
	}
}

// TestEveryEventTypeTheServiceNamesIsRegistered is the other direction of the golden: the exported
// event-type constants a domain publishes are the ones the catalogue knows about.
//
// Held as a literal list rather than discovered, because there is no way to enumerate constants at
// runtime and a list that grew by itself would not catch the thing worth catching — an event
// emitted from code that forgot to register it, which fails at the first emission rather than at
// build time.
func TestEveryEventTypeTheServiceNamesIsRegistered(t *testing.T) {
	want := []string{
		"job.status_changed", "job.expiry_warned", "job.expiry_extended",

		// SHIP-136. `bid.expired` is deliberately absent: bidding.StatusExpired is declared and
		// nothing writes it, and SHIP-89's scheduled task is the ticket that both starts writing
		// it and emits its event. A schema registered here for an event nothing emits would put a
		// line in the golden file describing a payload no code marshals, which reads as covered.
		"bid.placed", "bid.revised", "bid.withdrawn",
		"bid.countered", "bid.accepted", "bid.rejected",

		"delivery.driver_assigned", "delivery.milestone_recorded", "delivery.proof_recorded",
	}
	sort.Strings(want)

	got := make([]string, 0, len(events.Catalogue()))
	for _, s := range events.Catalogue() {
		got = append(got, s.Type)
	}
	sort.Strings(got)

	if strings.Join(got, " ") != strings.Join(want, " ") {
		t.Errorf("the catalogue holds [%s], and the service's event constants are [%s].\n"+
			"If a domain added an event, add it here too — the point of this list is that "+
			"it is written by whoever added the event rather than derived from what they did.",
			strings.Join(got, " "), strings.Join(want, " "))
	}
}
