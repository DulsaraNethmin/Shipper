package bidding

import (
	"errors"
	"net/http"
	"testing"

	"github.com/google/uuid"
)

// SHIP-96 — "customer, bidding provider, and admin each see only what Docs/02 §4 permits".
//
// Four rules, and the fourth is the one with a real defect behind it. Three audiences may read a
// negotiation and everybody else answers the 404 a bid that does not exist gets — so the tests below
// are two sets: one per permitted reader, and one per reader who is refused.
//
// **The administrator is exercised here and nowhere else on the wire, deliberately.** No
// administrator session exists until SHIP-147 and `authctx.Subject` cannot carry one, so
// `cmd/api` builds every [Viewer] with `Administrator` false and there is no route that would make
// it true. visibility.go records that; this file is what makes the third audience a mechanism that
// has been run rather than a branch that has been written.

// TestEachOfDocsTwoFoursReadersSeesTheNegotiation is the *Done when*, positive half.
//
// One negotiation with two rounds in it, read three ways: by the provider it is with, by the
// customer who owns the job, and by an administrator who is neither. All three get the same rows,
// which is the point — Docs/02 §4 says the history "remains visible" to all three and says nothing
// about any of them seeing a different version of it.
func TestEachOfDocsTwoFoursReadersSeesTheNegotiation(t *testing.T) {
	m := newMarket(t)

	placed, _, err := m.place(t, m.provider, m.job, offer("key-vis-placed"))
	if err != nil {
		t.Fatalf("placing: %v", err)
	}
	countered, _, err := m.counter(t, m.customer, m.job, placed.ID, counterOf(40000, "key-vis-counter"))
	if err != nil {
		t.Fatalf("countering: %v", err)
	}

	// An account that is neither party. As an administrator they read it; as themselves they do
	// not, which is the next test.
	administrator := m.rival(t, 9601)

	for name, tc := range map[string]struct {
		viewer Viewer
		want   Audience
	}{
		"the bidding provider":     {Viewer{ID: m.provider}, AudienceProvider},
		"the job's customer":       {Viewer{ID: m.customer}, AudienceCustomer},
		"an administrator":         {Viewer{ID: administrator, Administrator: true}, AudienceAdministrator},
		"an administrator who bid": {Viewer{ID: m.provider, Administrator: true}, AudienceAdministrator},
	} {
		t.Run(name, func(t *testing.T) {
			offers, audience, err := m.chainAs(t, tc.viewer, m.job, placed.ID)
			if err != nil {
				t.Fatalf("reading the chain: %v", err)
			}
			if audience != tc.want {
				t.Errorf("the platform decided %s, want %s", audience, tc.want)
			}
			if len(offers) != 2 || offers[0].ID != placed.ID || offers[1].ID != countered.ID {
				t.Errorf("the chain is %d rows, want the offer and the counter that displaced it",
					len(offers))
			}
		})
	}
}

// TestNobodyElseReachesANegotiation is the *Done when*'s "only", and Docs/01 §4.3's second privacy
// rule met on the response that would otherwise hand over a whole exchange at once.
//
// **The competing provider is the case that matters.** They hold a real credential, they are bidding
// on the same job, and the negotiation they are asking for is the one thing this API most needs to
// keep from them — another provider's price, timing and every counter it drew.
func TestNobodyElseReachesANegotiation(t *testing.T) {
	m := newMarket(t)

	placed, _, err := m.place(t, m.provider, m.job, offer("key-vis-private"))
	if err != nil {
		t.Fatalf("placing: %v", err)
	}

	competitor := m.rival(t, 9602)
	if _, _, err := m.place(t, competitor, m.job, offer("key-vis-competitor")); err != nil {
		t.Fatalf("the competing offer: %v", err)
	}
	stranger := newCustomer(t, m.pool, "vis-stranger@example.com", "+61400009603")

	for name, viewer := range map[string]Viewer{
		"a provider bidding on the same job": {ID: competitor},
		"a customer who owns no part of it":  {ID: stranger},
		"nobody at all":                      {},
	} {
		t.Run(name, func(t *testing.T) {
			offers, audience, err := m.chainAs(t, viewer, m.job, placed.ID)
			if !errors.Is(err, ErrNotBidOwner) && !errors.Is(err, ErrBidNotFound) {
				t.Fatalf("reading the chain answered %v, want the refusal a stranger gets", err)
			}
			if audience != AudienceNone {
				t.Errorf("the platform decided %s", audience)
			}
			if offers != nil {
				t.Errorf("a refused read returned %d rows", len(offers))
			}
		})
	}
}

// TestAnAdministratorIsNotAPartyAndDoesNotNeedToBe is the third rule stated as the thing it changes.
//
// The same account, the same negotiation, twice — once as themselves and once as an administrator.
// Without the flag they are a competing provider and get the 404; with it they read the chain. That
// is the whole of what SHIP-96 adds to SHIP-88's two-party read, and asserting it this way is what
// stops the administrator branch passing because the account happened to be a party.
func TestAnAdministratorIsNotAPartyAndDoesNotNeedToBe(t *testing.T) {
	m := newMarket(t)

	placed, _, err := m.place(t, m.provider, m.job, offer("key-vis-admin"))
	if err != nil {
		t.Fatalf("placing: %v", err)
	}
	outsider := m.rival(t, 9604)

	if _, _, err := m.chainAs(t, Viewer{ID: outsider}, m.job, placed.ID); !errors.Is(err, ErrNotBidOwner) {
		t.Fatalf("the outsider read the chain: %v", err)
	}

	offers, audience, err := m.chainAs(t, Viewer{ID: outsider, Administrator: true}, m.job, placed.ID)
	if err != nil {
		t.Fatalf("the administrator was refused: %v", err)
	}
	if audience != AudienceAdministrator || len(offers) != 1 {
		t.Errorf("the administrator read %d rows as %s, want the one offer as an administrator",
			len(offers), audience)
	}
}

// TestAnAdministratorWithNoAccountIsNobody keeps the flag from being the whole of the answer.
//
// CLAUDE.md's audit invariant wants the acting administrator named, and Docs/04 §6 requires the
// decision, the actor and the reason on every administrative action — so a viewer with the flag and
// no identifier is a request nothing could be attributed to. It answers the refusal rather than the
// chain, which is the safe direction and also the honest one.
func TestAnAdministratorWithNoAccountIsNobody(t *testing.T) {
	m := newMarket(t)

	placed, _, err := m.place(t, m.provider, m.job, offer("key-vis-anon-admin"))
	if err != nil {
		t.Fatalf("placing: %v", err)
	}

	if _, _, err := m.chainAs(t, Viewer{Administrator: true}, m.job, placed.ID); !errors.Is(err, ErrBidNotFound) {
		t.Errorf("an administrator with no account read the chain: %v", err)
	}
}

// TestABidPairedWithTheWrongJobIsUnreachableByEveryAudience is the addressing rule, applied to all
// three readers rather than to the two that could reach the endpoint before.
//
// A bid is addressed under its own job. Without this the first half of the URL would be decorative,
// and an administrator — the one reader who is not scoped by a row at all — is where that would show
// first.
func TestABidPairedWithTheWrongJobIsUnreachableByEveryAudience(t *testing.T) {
	m := newMarket(t)

	placed, _, err := m.place(t, m.provider, m.job, offer("key-vis-wrong-job"))
	if err != nil {
		t.Fatalf("placing: %v", err)
	}
	elsewhere := m.publish(t)

	for name, viewer := range map[string]Viewer{
		"the provider":     {ID: m.provider},
		"the customer":     {ID: m.customer},
		"an administrator": {ID: m.customer, Administrator: true},
	} {
		t.Run(name, func(t *testing.T) {
			if _, _, err := m.chainAs(t, viewer, elsewhere, placed.ID); !errors.Is(err, ErrBidNotFound) {
				t.Errorf("reading a bid under the wrong job answered %v, want ErrBidNotFound", err)
			}
		})
	}
}

// TestTheServedRouteCanNeverBuildAnAdministrator is the honest half of this ticket, pinned.
//
// `cmd/api` builds `Viewer{ID: callerID}` and nothing else, because the route is `RequireUser` and
// `authctx.Subject` cannot carry an administrator — Docs/06 §5.2 and SHIP-147 make admin sign-in a
// separate system. So the third audience is unreachable over HTTP today, and this test says so
// rather than leaving a reader to infer it from the absence of a check.
//
// It is asserted at the wire because that is where the claim is: a request through the served router
// carrying every credential a client can hold is still refused when it is neither party.
func TestTheServedRouteCanNeverBuildAnAdministrator(t *testing.T) {
	w := newWire(t)
	bid := w.placed(t, "key-vis-wire-admin")

	outsider := w.rival(t, 9605)
	rec := w.history(t, outsider, w.job, bid)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("an outsider read the chain over the wire: %d (%s)", rec.Code, rec.Body)
	}
	if got := decode[errorEnvelope](t, rec).Error.Code; got != "not_found" {
		t.Errorf("the refusal answered %q, want not_found — byte-identical to a bid that is not there", got)
	}

	// And the two who may, so that the test above is not passing against a broken route.
	for name, caller := range map[string]uuid.UUID{
		"the provider": w.provider,
		"the customer": w.customer,
	} {
		if got := w.history(t, caller, w.job, bid).Code; got != http.StatusOK {
			t.Errorf("%s reading the chain answered %d", name, got)
		}
	}
}
