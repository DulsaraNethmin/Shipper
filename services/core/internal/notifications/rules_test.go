package notifications

import (
	"reflect"
	"strings"
	"testing"

	"github.com/google/uuid"
)

// The routing table's own properties — the ones that hold without a database, and the ones a
// reviewer would otherwise have to check by eye across two maps and thirteen entries.

// TestARuleThatTellsNobodySaysWhy is what keeps the table honest.
//
// Most of StatusRules tells nobody, on purpose, because something else already did. The failure
// this guards against is the other kind: an event added to the table with an empty audience because
// whoever added it had not decided yet, which is indistinguishable from a deliberate silence unless
// the deliberate ones are made to explain themselves.
func TestARuleThatTellsNobodySaysWhy(t *testing.T) {
	for name, table := range map[string]map[string]Rule{"Rules": Rules, "StatusRules": StatusRules} {
		for key, rule := range table {
			switch {
			case len(rule.To) == 0 && rule.Why == "":
				t.Errorf("%s[%q] tells nobody and does not say why", name, key)
			case len(rule.To) > 0 && rule.Why != "":
				t.Errorf("%s[%q] tells somebody and also explains why it does not", name, key)
			}
		}
	}
}

// TestEveryRuleThatTellsSomebodyIsComplete catches the halves of a rule that a copy-paste leaves
// out: a headline nobody wrote, a channel nothing can send, a category the CHECK will refuse.
func TestEveryRuleThatTellsSomebodyIsComplete(t *testing.T) {
	known := map[Audience]bool{ToJobCustomer: true, ToAwardedProvider: true, ToBidProvider: true}

	for name, table := range map[string]map[string]Rule{"Rules": Rules, "StatusRules": StatusRules} {
		for key, rule := range table {
			if !rule.Category.Valid() {
				t.Errorf("%s[%q] is categorised %q, which ck_notifications_category refuses",
					name, key, rule.Category)
			}
			if len(rule.To) == 0 {
				continue
			}

			if strings.TrimSpace(rule.Headline) == "" {
				t.Errorf("%s[%q] tells somebody and says nothing", name, key)
			}
			if len(rule.Channels) == 0 {
				t.Errorf("%s[%q] tells somebody on no channel", name, key)
			}
			for _, channel := range rule.Channels {
				if !channel.Valid() {
					t.Errorf("%s[%q] names channel %q", name, key, channel)
				}
			}
			for _, audience := range rule.To {
				if !known[audience] {
					t.Errorf("%s[%q] names audience %q, which resolve cannot resolve",
						name, key, audience)
				}
			}
		}
	}
}

// TestOnlyJobExpiryIsMutable is SHIP-142's half of the design, decided here so that ticket is a
// preference table and a filter rather than a re-reading of Docs/01 §4.5.
//
// §4.5 calls its list "essential events" and every category but one is on it. If a later ticket
// makes a second category mutable, this test is where the decision has to be re-taken.
func TestOnlyJobExpiryIsMutable(t *testing.T) {
	for _, category := range Categories {
		want := category != CategoryJobExpiry
		if category.Essential() != want {
			t.Errorf("%s.Essential() = %v, want %v", category, category.Essential(), want)
		}
	}
}

// TestARenderedBodyCarriesNothingButTheHeadlineAndTheJob is SHIP-141 held structurally.
//
// The rule is "no address, goods description, or full customer name appears in a notification
// body", and the way this ticket meets it is by giving the renderer nothing else to work with. The
// test asserts the shape of that rather than searching for particular strings: a body that is
// exactly the headline plus the job identifier plus fixed text cannot contain anything a search
// would have to look for.
func TestARenderedBodyCarriesNothingButTheHeadlineAndTheJob(t *testing.T) {
	job := uuid.Must(uuid.NewV7())

	for key, rule := range Rules {
		if len(rule.To) == 0 {
			continue
		}

		rendered := body(rule, job)
		remaining := strings.ReplaceAll(rendered, rule.Headline, "")
		remaining = strings.ReplaceAll(remaining, job.String(), "")

		for _, fixed := range []string{"Job", "Open the Shipper app for the details.",
			"Please do not reply to this message."} {
			remaining = strings.ReplaceAll(remaining, fixed, "")
		}
		if strings.TrimSpace(remaining) != "" {
			t.Errorf("the body for %q carries %q beyond its headline and the job identifier; "+
				"anything variable in a body is somewhere an address can end up (SHIP-141)",
				key, strings.TrimSpace(remaining))
		}
	}
}

// TestNoNotificationFieldCanCarryABudget holds the invariant against the fields somebody adds later
// rather than against the fields that are there now.
//
// Docs/01 §4.3 keeps the customer's maximum private from providers, and a notification travels
// further than any endpoint: it is read on a handset by whoever is holding it. SHIP-117's
// ExceptionEntry has the same test for the same reason.
func TestNoNotificationFieldCanCarryABudget(t *testing.T) {
	forbidden := []string{"budget", "price", "amount", "maximum", "cents"}

	shape := reflect.TypeOf(Notification{})
	for i := range shape.NumField() {
		name := strings.ToLower(shape.Field(i).Name)
		for _, word := range forbidden {
			if strings.Contains(name, word) {
				t.Errorf("Notification has a field %q; a notification is the last place a "+
					"customer's budget could leak from and the first place it would be "+
					"read (Docs/01 §4.3)", shape.Field(i).Name)
			}
		}
	}
}

// TestRuleForRoutesAStatusChangeByItsStatus covers the one event whose routing depends on its
// payload, including the case nobody has written a rule for yet.
func TestRuleForRoutesAStatusChangeByItsStatus(t *testing.T) {
	cases := map[string]struct {
		status string
		tells  int
	}{
		"a completed job tells both parties":      {"Completed", 2},
		"a cancelled job tells both parties":      {"Cancelled", 2},
		"an awarded job is announced by the bid":  {"Awarded", 0},
		"a milestone is announced by delivery":    {"Picked up", 0},
		"a status with no entry routes to nobody": {"Something new", 0},
	}

	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			rule, ok := RuleFor(EventJobStatusChanged, c.status)
			if !ok {
				t.Fatal("job.status_changed has no rule at all")
			}
			if len(rule.To) != c.tells {
				t.Errorf("it tells %d people, want %d", len(rule.To), c.tells)
			}
			if c.tells == 0 && rule.Why == "" {
				t.Error("it tells nobody and does not say why")
			}
		})
	}
}

// TestAnUnroutedEventTypeIsNotFound keeps RuleFor's two negatives apart: an event with no rule at
// all stops the consumer, and an event whose rule tells nobody does not.
func TestAnUnroutedEventTypeIsNotFound(t *testing.T) {
	if _, ok := RuleFor("job.nothing_registers_this", ""); ok {
		t.Error("an unregistered event type was routed")
	}
}
