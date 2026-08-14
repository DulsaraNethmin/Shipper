package main

import (
	"testing"

	"github.com/DulsaraNethmin/Shipper/services/core/internal/events"
	"github.com/DulsaraNethmin/Shipper/services/core/internal/notifications"
)

// The routing table, held against the catalogue it routes (SHIP-137).
//
// # Why this test is in cmd/api
//
// For the reason events_golden.txt is. The catalogue is populated by init functions in `jobs`,
// `bidding` and `delivery`, and internal/notifications may import none of them — so a test inside
// that package would run against an empty catalogue and pass by seeing nothing. cmd/api is the one
// binary that links every domain, which makes it the only place the whole surface exists at once.
//
// cmd/notifier links notifications and not the emitting domains, deliberately: a consumer routes on
// the event type string it read off the topic and has no reason to link the code that produced it.
// So this check belongs here rather than there too.
//
// # What it is for
//
// A domain event with no routing rule is an event nobody is ever told about, and there is no other
// symptom: the emitting domain works, the outbox drains, the message lands on the topic, and the
// consumer refuses it with notifications.ErrNoRule in a log somebody reads a week later. This turns
// that into a compile-time-shaped failure at the moment the event is added.

// TestEveryRegisteredEventHasANotificationRule is the one direction that matters.
//
// The other direction — a rule for an event nothing emits — is deliberately allowed. An event type
// can be retired from a domain before the rule is cleaned up, and a rule with no event is inert,
// whereas an event with no rule is silence.
func TestEveryRegisteredEventHasANotificationRule(t *testing.T) {
	catalogue := events.Catalogue()
	if len(catalogue) == 0 {
		t.Fatal("the catalogue is empty; this binary links every domain and should see every " +
			"registered event")
	}

	for _, schema := range catalogue {
		if _, ok := notifications.RuleFor(schema.Type, ""); !ok {
			t.Errorf(`%s is registered in the event catalogue and internal/notifications has
no routing rule for it.

Add one to notifications.Rules in the same change that registers the event. A rule that
deliberately tells nobody is a one-line entry with a Why — see the job.status_changed
transitions that are already announced by the bid or delivery event that caused them.

Without a rule the event reaches the consumer, is refused with ErrNoRule, and the person it
concerned is never told; nothing else fails.`, schema.Type)
		}
	}
}
