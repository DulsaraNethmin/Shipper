package notifications

import (
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/google/uuid"
)

// SHIP-141's two guards, tested as two guards.
//
// redaction.go carries the argument for why there are two and where each is weak. The short form:
// the structural guard covers everything that would have to arrive through the event, the word-level
// guard covers everything somebody can type into rules.go, and neither is sufficient alone. Wave 10
// established the second half by isolating a sentence — "The customer has set a maximum." — that
// carried no field and no value, and watching a thirteen-test suite pass with it live.

// --- the guard the ticket is about -------------------------------------------------------------

// TestNoRuleCanRenderCopyThatBreaksTheRedactionRules is the one that has to hold.
//
// Every rule that tells somebody, on every channel it could ever route to, with the rendered subject
// and the rendered body both put through [Redactions]. A headline edited in rules.go fails here
// rather than on a handset — which is the whole reason the guard is over rendered output and not
// over a template source.
func TestNoRuleCanRenderCopyThatBreaksTheRedactionRules(t *testing.T) {
	job := uuid.Must(uuid.NewV7())

	for name, table := range map[string]map[string]Rule{"Rules": Rules, "StatusRules": StatusRules} {
		for key, rule := range table {
			if len(rule.To) == 0 {
				continue
			}
			// Every channel, not only the ones this rule routes to. A rule gaining a
			// channel must not be able to gain a disclosure with it.
			for _, channel := range Channels {
				subject, body, err := Render(channel, rule, job)
				if err != nil {
					t.Errorf("%s[%q] on %s: %v", name, key, channel, err)
					continue
				}
				for what, text := range map[string]string{"subject": subject, "body": body} {
					for _, problem := range Redactions(text) {
						t.Errorf("%s[%q]'s %s %s on %s: %s",
							name, key, channel, what, problem, text)
					}
				}
			}
		}
	}
}

// TestRenderRefusesCopyThatWouldReachAHandset is the runtime half.
//
// The table above is literals, so the test above would catch a leak before a deploy. This is what
// happens if one arrives another way: [Render] returns [ErrRedacted] and writes nothing, because
// Consume renders and inserts in the same loop and this is the only writer of a notification's text.
//
// The cases are the four rules, each in the shape it would actually take. **The first is the one
// SHIP-141 exists for**: a street address in a push body, which is the mutation this ticket was
// tested with.
func TestRenderRefusesCopyThatWouldReachAHandset(t *testing.T) {
	job := uuid.Must(uuid.NewV7())

	for _, tc := range []struct {
		name     string
		headline string
		wants    string
	}{
		{
			name:     "a street address, which is the disclosure the ticket is named for",
			headline: "Collect from 12 Collins Street.",
			wants:    "digit",
		},
		{
			name:     "an address with the number spelled out, so only the vocabulary catches it",
			headline: "Collect from the pickup address on collins street.",
			wants:    "street type",
		},
		{
			name:     "a customer's full name, which no field name appears in",
			headline: "Your delivery for John Smith is on its way.",
			wants:    "capitalised word",
		},
		{
			name:     "a suburb, which is most of an address once the number has gone",
			headline: "Your delivery has reached Bondi.",
			wants:    "capitalised word",
		},
		{
			name:     "a postcode and a state",
			headline: "Your delivery has reached NSW 2026.",
			wants:    "digit",
		},
		{
			name:     "a weight, which is a goods description in digits",
			headline: "Your 500 kg consignment is on its way.",
			wants:    "digit",
		},
		{
			name:     "an email address",
			headline: "Reply to support@example.com about this.",
			wants:    "@",
		},
		{
			name:     "a link, which makes a push a phishing surface",
			headline: "Open https://example.com to see the details.",
			wants:    "link",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rule := Rule{Category: CategoryDelivery, To: []Audience{ToJobCustomer},
				Channels: channels, Headline: tc.headline}

			// Every channel, because the headline reaches the subject on push and SMS and
			// the body on SMS and email — a guard on one of the two would miss half.
			for _, channel := range Channels {
				subject, body, err := Render(channel, rule, job)
				if !errors.Is(err, ErrRedacted) {
					t.Fatalf("%s rendered %q as subject %q / body %q with error %v, "+
						"want ErrRedacted", channel, tc.headline, subject, body, err)
				}
				if !strings.Contains(err.Error(), tc.wants) {
					t.Errorf("%s refused it without naming %q: %v", channel, tc.wants, err)
				}
				if subject != "" || body != "" {
					t.Errorf("%s returned copy alongside the refusal: %q / %q",
						channel, subject, body)
				}
			}
		})
	}
}

// TestTheRedactionRulesLetOrdinaryCopyThrough is the other half of a guard being useful.
//
// A rule that refuses everything protects nothing, because the answer to it is to switch it off. The
// cases here are the shapes the existing copy actually uses and the shapes a later ticket is likely
// to write, and each of them has to pass.
func TestTheRedactionRulesLetOrdinaryCopyThrough(t *testing.T) {
	for _, text := range []string{
		"A job has been awarded. Both parties can see it in the app.",
		"Your job is about to stop taking offers. You can extend it in the app.",
		"A dispute has been raised on a job. Support will be in touch.",
		"Open the Shipper app to see the details and to act on it.",
		"Job 0192f2c0-9c1a-7000-8000-0123456789ab",
		"There is a counter-offer waiting on one of your jobs.",
		"— Shipper",
		"Proof of delivery has been recorded for your job.",
	} {
		if problems := Redactions(text); len(problems) > 0 {
			t.Errorf("%q was refused: %s", text, strings.Join(problems, "; "))
		}
	}
}

// TestTheJobIdentifierIsTheOneNumberAllowed.
//
// Rule 1 is "no digits at all" rather than "no digits except these", and it can only be that because
// the identifier is removed before the rule runs. A digit outside one must still be caught with an
// identifier present, which is the case a naive "does it contain a UUID" check would let through.
func TestTheJobIdentifierIsTheOneNumberAllowed(t *testing.T) {
	job := uuid.Must(uuid.NewV7()).String()

	if problems := Redactions("Job " + job); len(problems) > 0 {
		t.Errorf("the job identifier alone was refused: %s", strings.Join(problems, "; "))
	}

	problems := Redactions("Job " + job + " is at unit 4.")
	if len(problems) == 0 {
		t.Error("a unit number beside a job identifier was accepted")
	}
}

// TestTheCapitalisationRuleIsAboutSentencesRatherThanCapitals.
//
// The rule has to distinguish "A job has been cancelled." from "Cancelled by Jane Doe.", and both
// start with a capital. Getting this wrong in the strict direction makes the guard fire on every
// sentence and get switched off; getting it wrong in the loose direction makes it catch nothing.
func TestTheCapitalisationRuleIsAboutSentencesRatherThanCapitals(t *testing.T) {
	clean := []string{
		"A job has been cancelled.",
		"Your job has moved on. Open the app for the details.",
		"Your job has moved on! Open the app.",
		"Your job has moved on? Open the app.",
	}
	for _, text := range clean {
		if problems := Redactions(text); len(problems) > 0 {
			t.Errorf("%q was refused: %s", text, strings.Join(problems, "; "))
		}
	}

	if problems := Redactions("Your delivery was taken by Jane."); len(problems) == 0 {
		t.Error("a capitalised name mid-sentence was accepted")
	}
}

// TestTheAllowedCapitalsAreTwo pins the allowlist.
//
// It is the one place this guard can be widened without touching a rule, so widening it is made a
// deliberate act. The question the next person has to answer is in redaction.go: what stops the new
// word being somebody's surname.
func TestTheAllowedCapitalsAreTwo(t *testing.T) {
	if len(allowedCapitals) != 2 || !allowedCapitals["Shipper"] || !allowedCapitals["Job"] {
		t.Errorf("the mid-sentence capital allowlist is %v; adding a word here is a privacy "+
			"decision rather than a copy one (SHIP-141)", allowedCapitals)
	}
}

// --- the structural guard, which covers what no textual rule can ---------------------------------

// TestTheClosedInputIsStillClosed.
//
// [content] is what a template may be handed, and SHIP-138 made the argument that a template cannot
// leak an address because there is no field one could arrive in. This is that argument as an
// assertion, by name rather than by count: a third field called anything is a decision, and a field
// called `Address` is the decision this ticket exists to prevent.
func TestTheClosedInputIsStillClosed(t *testing.T) {
	shape := reflect.TypeOf(content{})

	want := map[string]bool{"JobID": true, "Headline": true}
	if shape.NumField() != len(want) {
		t.Fatalf("content has %d fields and should have %d; adding one is a decision about "+
			"privacy rather than about copy (SHIP-141, and templates.go says what has to be "+
			"argued)", shape.NumField(), len(want))
	}
	for i := range shape.NumField() {
		if !want[shape.Field(i).Name] {
			t.Errorf("content has a field %q, which is not one a template may be handed",
				shape.Field(i).Name)
		}
	}
}

// TestTheFactsThisDomainReadsCarryNothingToLeak is the guard one layer further out.
//
// [content] can only be filled from what [Consume] has, and [Consume] has [facts]. Six fields, all
// of them identifiers or routing values, and **the goods description and the customer's name are
// absent from both** — which is the only guard those two halves of the *Done when* have, because no
// textual rule distinguishes "two pallets of copper piping" from ordinary prose.
//
// Checked by forbidden substring rather than by an exact field list, so a field added for a good
// reason passes and a field that could carry a disclosure does not.
func TestTheFactsThisDomainReadsCarryNothingToLeak(t *testing.T) {
	forbidden := []string{
		"address", "street", "suburb", "postcode", "location", "pickup", "dropoff",
		"description", "goods", "cargo", "item", "name", "phone", "email",
		"budget", "price", "amount", "maximum",
	}

	shape := reflect.TypeOf(facts{})
	for i := range shape.NumField() {
		name := strings.ToLower(shape.Field(i).Name)
		for _, word := range forbidden {
			if strings.Contains(name, word) {
				t.Errorf("facts has a field %q; this struct is the whole of what the "+
					"renderer can ever be given, and a description or a name in it is "+
					"the one disclosure no word-level rule can catch (SHIP-141)",
					shape.Field(i).Name)
			}
		}
	}
}
