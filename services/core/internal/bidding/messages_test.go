package bidding

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"testing"
	"unicode"

	"github.com/google/uuid"

	"github.com/DulsaraNethmin/Shipper/services/core/internal/db"
	"github.com/DulsaraNethmin/Shipper/services/core/internal/httpx"
)

// SHIP-97 — "messages attach to a job and are visible only to its two parties and admins".
//
// The *Done when* is one sentence with three clauses, and each is tested where it actually holds:
//
//  1. **attach to a job** — the row carries the job and the negotiation, the conversation survives a
//     counter replacing the live offer, and it is reachable through any offer in the chain;
//  2. **visible only to its two parties** — a competing provider, a stranger and an unrelated
//     customer each get exactly what a missing bid gets, byte-identically;
//  3. **and admins** — [Audience] resolves an administrator through the same function SHIP-96 built,
//     which is tested here at the service and is **unreachable over the wire** because no credential
//     in this platform can set the flag. That clause is declared reduced in Docs/11 §3 rather than
//     quietly met.
//
// # The fourth thing these tests are for is the one no other file in this package needs
//
// This is the first response in the domain carrying **free text**, and wave 10 and wave 11 both
// established that a disclosure with no field, no value and no digit defeats a closed key set, a word
// search, a value search and an AST walk alike.
// [TestNothingTheCustomerTypedIsAddedToByThePlatform] is therefore a **word-level** check over
// rendered bytes: it decomposes the response into words and requires every one of them to have come
// either from the closed key set or from what a party actually typed. A sentence the platform
// introduced has nowhere to hide in that, whatever it says and whatever it omits.

// --- fixtures -------------------------------------------------------------------------------------

// conversation is a job with one live offer on it, and the two parties to that offer.
//
// Built through [market.place] rather than by writing rows, so the negotiation the messages attach to
// is one the endpoint actually produced.
type conversation struct {
	market
	bid Bid
}

func newConversation(t *testing.T, key string) conversation {
	t.Helper()

	m := newMarket(t)
	bid, _, err := m.place(t, m.provider, m.job, offer(key))
	if err != nil {
		t.Fatalf("placing the offer the conversation hangs off: %v", err)
	}
	return conversation{market: m, bid: bid}
}

// send writes one message as the caller named, on the pool, which is what the handler passes.
func (c conversation) send(t *testing.T, caller uuid.UUID, body, key string) (Message, bool, error) {
	t.Helper()
	return c.svc.SendMessage(t.Context(), c.pool, caller, c.job, c.bid.ID, Note{Body: body, Key: key})
}

// read is one page of the conversation as the caller named, and which reader the platform decided
// they are.
func (c conversation) read(t *testing.T, caller uuid.UUID, q MessageQuery) (MessagePage, Audience, error) {
	t.Helper()
	return c.svc.Messages(t.Context(), c.pool, Viewer{ID: caller}, c.job, c.bid.ID, q)
}

// bodies is what a page reads, in order, which is what almost every assertion below is about.
func bodies(page MessagePage) []string {
	said := make([]string, 0, len(page.Messages))
	for _, m := range page.Messages {
		said = append(said, m.Body)
	}
	return said
}

// --- the messages attach to the job, and to the negotiation rather than to an offer ---------------

// TestBothPartiesWriteIntoOneConversation is the first clause of the *Done when*, and the shape of
// the whole feature.
//
// One address, two callers, and the platform works out which side each is on — the arrangement
// SHIP-87's counter takes. The order is asserted as well as the membership: a conversation read
// newest first is not a conversation.
func TestBothPartiesWriteIntoOneConversation(t *testing.T) {
	c := newConversation(t, "msg-both-parties")

	if _, created, err := c.send(t, c.customer, "Is there a lift?", "msg-1"); err != nil || !created {
		t.Fatalf("the customer's message = %v (created %t), want it sent", err, created)
	}
	if _, created, err := c.send(t, c.provider, "There is, but it is out until Thursday.", "msg-2"); err != nil || !created {
		t.Fatalf("the provider's reply = %v (created %t), want it sent", err, created)
	}
	if _, created, err := c.send(t, c.customer, "Stairs are fine then.", "msg-3"); err != nil || !created {
		t.Fatalf("the customer's second message = %v (created %t), want it sent", err, created)
	}

	page, audience, err := c.read(t, c.customer, MessageQuery{})
	if err != nil {
		t.Fatalf("the customer reading the conversation: %v", err)
	}
	if audience != AudienceCustomer {
		t.Errorf("the job's customer reads as %s, want customer", audience)
	}

	want := []string{"Is there a lift?", "There is, but it is out until Thursday.", "Stairs are fine then."}
	if got := bodies(page); !equalStrings(got, want) {
		t.Errorf("the conversation reads %q, want %q — oldest first", got, want)
	}

	// Both parties read the same conversation. A per-caller filter would pass every assertion above
	// while showing each of them only their own half.
	theirs, audience, err := c.read(t, c.provider, MessageQuery{})
	if err != nil {
		t.Fatalf("the provider reading the conversation: %v", err)
	}
	if audience != AudienceProvider {
		t.Errorf("the bidding provider reads as %s, want provider", audience)
	}
	if got := bodies(theirs); !equalStrings(got, want) {
		t.Errorf("the provider reads %q, want the same conversation the customer reads, %q", got, want)
	}

	// And each message says who wrote it, which is the field the screen is built out of.
	wrote := []Party{PartyCustomer, PartyProvider, PartyCustomer}
	for i, m := range page.Messages {
		if m.SentBy != wrote[i] {
			t.Errorf("message %d says %s wrote it, want %s", i, m.SentBy, wrote[i])
		}
		if m.JobID != c.job {
			t.Errorf("message %d is on job %s, want %s", i, m.JobID, c.job)
		}
		if m.ProviderID != c.provider {
			t.Errorf("message %d names provider %s, want the negotiation's, %s", i, m.ProviderID, c.provider)
		}
	}
}

// TestTheConversationSurvivesTheOfferItWasStartedOn is why 000506 records no `bid_id`.
//
// A counter supersedes the offer it answers and the negotiation continues on a **new row** (SHIP-88).
// A conversation keyed on the offer would restart at that moment — the question would be attached to
// a superseded bid and the answer to its successor — and neither party could read the exchange.
//
// It is also what makes "address it through any offer in the negotiation" true, which is what the
// contract tells clients.
func TestTheConversationSurvivesTheOfferItWasStartedOn(t *testing.T) {
	c := newConversation(t, "msg-survives")

	if _, _, err := c.send(t, c.customer, "Can you do Tuesday?", "msg-before"); err != nil {
		t.Fatalf("the message before the counter: %v", err)
	}

	countered, _, err := c.counter(t, c.customer, c.job, c.bid.ID, counterOf(40000, "msg-counter"))
	if err != nil {
		t.Fatalf("countering: %v", err)
	}
	if countered.ID == c.bid.ID {
		t.Fatal("the counter reused the offer's row, so this test is not about a chain")
	}

	// The same conversation, reached through the counter rather than through the original offer.
	after := conversation{market: c.market, bid: countered}
	if _, _, err := after.send(t, c.provider, "Tuesday works.", "msg-after"); err != nil {
		t.Fatalf("the message after the counter: %v", err)
	}

	want := []string{"Can you do Tuesday?", "Tuesday works."}
	for _, reached := range []struct {
		name string
		conv conversation
	}{
		{"through the original offer", c},
		{"through the counter that superseded it", after},
	} {
		page, _, err := reached.conv.read(t, c.customer, MessageQuery{})
		if err != nil {
			t.Fatalf("reading %s: %v", reached.name, err)
		}
		if got := bodies(page); !equalStrings(got, want) {
			t.Errorf("reading %s gives %q, want the one conversation %q", reached.name, got, want)
		}
	}
}

// TestTwoProvidersOnOneJobHaveTwoConversations is the privacy rule the `(job, provider)` key exists
// for, and it is the reason this is not a table keyed on the job.
//
// Docs/01 §4.3's second line makes a provider's price private from competing providers. A message is
// free text, so a room shared with a competitor discloses whatever anybody types into it — which no
// closed key set can prevent, because the disclosure is not a field.
func TestTwoProvidersOnOneJobHaveTwoConversations(t *testing.T) {
	c := newConversation(t, "msg-two-providers")

	rival := c.rival(t, 97)
	theirs, _, err := c.place(t, rival, c.job, offer("msg-two-providers-rival"))
	if err != nil {
		t.Fatalf("placing the rival's offer: %v", err)
	}
	other := conversation{market: c.market, bid: theirs}

	if _, _, err := c.send(t, c.customer, "I can pay a bit more for Thursday.", "msg-mine"); err != nil {
		t.Fatalf("the customer's message to the first provider: %v", err)
	}
	if _, _, err := other.send(t, c.customer, "Are you free on Thursday?", "msg-theirs"); err != nil {
		t.Fatalf("the customer's message to the rival: %v", err)
	}

	mine, _, err := c.read(t, c.provider, MessageQuery{})
	if err != nil {
		t.Fatalf("the first provider reading: %v", err)
	}
	if got := bodies(mine); !equalStrings(got, []string{"I can pay a bit more for Thursday."}) {
		t.Errorf("the first provider reads %q, want only their own conversation", got)
	}

	rivals, _, err := other.read(t, rival, MessageQuery{})
	if err != nil {
		t.Fatalf("the rival reading: %v", err)
	}
	if got := bodies(rivals); !equalStrings(got, []string{"Are you free on Thursday?"}) {
		t.Errorf("the rival reads %q, want only their own conversation", got)
	}

	// And neither can reach the other's, which is the refusal rather than the filter.
	if _, _, err := other.read(t, c.provider, MessageQuery{}); !errors.Is(err, ErrNotBidOwner) {
		t.Errorf("the first provider reading the rival's conversation = %v, want ErrNotBidOwner", err)
	}
	if _, _, err := c.read(t, rival, MessageQuery{}); !errors.Is(err, ErrNotBidOwner) {
		t.Errorf("the rival reading the first provider's conversation = %v, want ErrNotBidOwner", err)
	}
}

// TestMessagingOutlivesTheOffer is the decision this ticket takes deliberately and the reason there
// is no status gate anywhere in messages.go.
//
// **The moment two parties most need to arrange something is after the award.** A rule that closed
// the conversation when the negotiation closed would shut it exactly then. Every status is exercised
// rather than only the awarded one, because the rule is "no status rule" and a test naming one status
// would pass against an implementation that permitted that status alone.
func TestMessagingOutlivesTheOffer(t *testing.T) {
	for _, closed := range []Status{
		StatusAccepted, StatusRejected, StatusWithdrawn, StatusSuperseded, StatusExpired,
	} {
		t.Run(strings.ToLower(string(closed)), func(t *testing.T) {
			c := newConversation(t, "msg-outlives-"+string(closed))
			c.setStatus(t, c.bid.ID, closed)

			if _, created, err := c.send(t, c.customer, "Where should the driver park?", "msg-after-close"); err != nil || !created {
				t.Fatalf("the customer's message on a %s offer = %v (created %t), want it sent",
					closed, err, created)
			}
			if _, _, err := c.send(t, c.provider, "The loading dock is fine.", "msg-after-close-reply"); err != nil {
				t.Fatalf("the provider's reply on a %s offer: %v", closed, err)
			}

			page, _, err := c.read(t, c.provider, MessageQuery{})
			if err != nil {
				t.Fatalf("reading a %s negotiation's conversation: %v", closed, err)
			}
			if len(page.Messages) != 2 {
				t.Errorf("the conversation holds %d messages on a %s offer, want both", len(page.Messages), closed)
			}
		})
	}
}

// --- visible only to its two parties --------------------------------------------------------------

// TestNobodyOutsideTheNegotiationCanReadOrWrite is the second clause of the *Done when*, from every
// direction somebody could arrive from.
//
// **The refusal is the 404 a missing bid gets, byte-identically.** A competing provider told "not for
// you" would have learned that a negotiation exists on a job they are bidding against, which is the
// disclosure visibility.go's fourth rule is written for.
func TestNobodyOutsideTheNegotiationCanReadOrWrite(t *testing.T) {
	c := newConversation(t, "msg-outsiders")

	if _, _, err := c.send(t, c.customer, "Anybody there?", "msg-outsider-seed"); err != nil {
		t.Fatalf("seeding the conversation: %v", err)
	}

	rival := c.rival(t, 98)
	if _, _, err := c.place(t, rival, c.job, offer("msg-outsider-rival")); err != nil {
		t.Fatalf("placing the rival's offer: %v", err)
	}
	stranger := newCustomer(t, c.pool, "msg-stranger@example.com", "+61416000097")

	for _, outsider := range []struct {
		name string
		id   uuid.UUID
	}{
		{"a provider bidding on the same job", rival},
		{"a customer with no relationship to the job", stranger},
		{"an identifier belonging to nobody", uuid.Must(uuid.NewV7())},
	} {
		t.Run(outsider.name, func(t *testing.T) {
			if _, _, err := c.read(t, outsider.id, MessageQuery{}); !errors.Is(err, ErrNotBidOwner) {
				t.Errorf("reading = %v, want ErrNotBidOwner — one 404 for every outsider", err)
			}
			if _, _, err := c.send(t, outsider.id, "Let me in.", "msg-outsider-"+outsider.id.String()); !errors.Is(err, ErrNotBidOwner) {
				t.Errorf("writing = %v, want ErrNotBidOwner", err)
			}
		})
	}

	// And nothing they attempted was written.
	page, _, err := c.read(t, c.customer, MessageQuery{})
	if err != nil {
		t.Fatalf("reading the conversation back: %v", err)
	}
	if len(page.Messages) != 1 {
		t.Errorf("the conversation holds %d messages, want the one the customer wrote", len(page.Messages))
	}
}

// TestAMessageIsAddressedUnderItsOwnJob is the check that keeps the first half of the URL from being
// decorative.
//
// Without it a client pairing a real bid with any job at all would succeed, which is the shape where
// one resource has two addresses and a rule gets enforced at one of them.
func TestAMessageIsAddressedUnderItsOwnJob(t *testing.T) {
	c := newConversation(t, "msg-wrong-job")
	other := c.publish(t)

	if _, _, err := c.svc.SendMessage(t.Context(), c.pool, c.customer, other, c.bid.ID,
		Note{Body: "Wrong job.", Key: "msg-wrong-job-write"}); !errors.Is(err, ErrBidNotFound) {
		t.Errorf("writing under the wrong job = %v, want ErrBidNotFound", err)
	}
	if _, _, err := c.svc.Messages(t.Context(), c.pool, Viewer{ID: c.customer}, other, c.bid.ID,
		MessageQuery{}); !errors.Is(err, ErrBidNotFound) {
		t.Errorf("reading under the wrong job = %v, want ErrBidNotFound", err)
	}
}

// --- and admins -----------------------------------------------------------------------------------

// TestAnAdministratorReadsAnyConversation is the third clause of SHIP-97's *Done when*, met in the
// domain and **unreachable from the wire**.
//
// SHIP-96 built [Service.audienceFor] and this endpoint reuses it rather than adding a second rule,
// which is what makes an administrative reader a mapping rather than a field list: the flag is the
// only thing that changes, and nothing about what a row shows moves with it.
//
// **`cmd/api` cannot set the flag.** `authctx.Subject` carries two roles and neither is an
// administrator (Docs/06 §5.2, SHIP-147), so every [Viewer] the served router builds has it false and
// there is no credential in this platform that could make it true. This test is therefore the whole
// of the clause's demonstration, and Docs/11 §3 declares it reduced rather than met.
func TestAnAdministratorReadsAnyConversation(t *testing.T) {
	c := newConversation(t, "msg-admin")

	if _, _, err := c.send(t, c.customer, "The address is wrong on the listing.", "msg-admin-seed"); err != nil {
		t.Fatalf("seeding the conversation: %v", err)
	}

	// An account that is neither party — the same identifier that is refused as an ordinary caller
	// two lines down, so what changes is the flag and nothing else.
	administrator := newCustomer(t, c.pool, "msg-admin@example.com", "+61416000098")

	if _, _, err := c.svc.Messages(t.Context(), c.pool, Viewer{ID: administrator},
		c.job, c.bid.ID, MessageQuery{}); !errors.Is(err, ErrNotBidOwner) {
		t.Fatalf("the same account without the flag = %v, want ErrNotBidOwner — otherwise this test "+
			"proves nothing about the flag", err)
	}

	page, audience, err := c.svc.Messages(t.Context(), c.pool,
		Viewer{ID: administrator, Administrator: true}, c.job, c.bid.ID, MessageQuery{})
	if err != nil {
		t.Fatalf("an administrator reading the conversation: %v", err)
	}
	if audience != AudienceAdministrator {
		t.Errorf("the reader resolves as %s, want administrator", audience)
	}
	if got := bodies(page); !equalStrings(got, []string{"The address is wrong on the listing."}) {
		t.Errorf("the administrator reads %q, want the conversation", got)
	}
}

// TestAnAdministratorMayReadAndNotWrite holds the asymmetry `ck_job_messages_sent_by` enforces.
//
// Docs/02 §4 makes an administrator a **reader** of a negotiation. There are two parties to a
// conversation and an administrator is neither, so a message from one would have to be attributed to
// somebody who did not write it — which is why the CHECK admits two values and this path has no third.
func TestAnAdministratorMayReadAndNotWrite(t *testing.T) {
	c := newConversation(t, "msg-admin-readonly")
	administrator := newCustomer(t, c.pool, "msg-admin-ro@example.com", "+61416000099")

	// SendMessage takes no Viewer at all, which is the mechanism: there is no argument through which
	// an administrator could present themselves to it, so the refusal is the ordinary outsider's.
	if _, _, err := c.send(t, administrator, "Sorting this out.", "msg-admin-write"); !errors.Is(err, ErrNotBidOwner) {
		t.Errorf("an administrator writing = %v, want ErrNotBidOwner: Docs/02 §4 makes them a reader", err)
	}
}

// --- the retry, which is the half the middleware cannot cover -------------------------------------

// TestARetriedMessageIsSentOnce is the column doing what Redis cannot.
//
// SHIP-15 replays a stored response while its entry lives. A phone that sent a message, never saw the
// response and retried an hour later outlives any TTL worth setting — and a duplicated message is
// worse than most duplicated writes, because the other party has already read it and cannot tell
// which sending was the mistake.
func TestARetriedMessageIsSentOnce(t *testing.T) {
	c := newConversation(t, "msg-retry")

	first, created, err := c.send(t, c.provider, "I can be there at nine.", "msg-retry-key")
	if err != nil || !created {
		t.Fatalf("the first send = %v (created %t), want it sent", err, created)
	}

	again, created, err := c.send(t, c.provider, "I can be there at nine.", "msg-retry-key")
	if err != nil {
		t.Fatalf("the retry: %v", err)
	}
	if created {
		t.Error("the retry reported a message created, so it sent a second one")
	}
	if again.ID != first.ID {
		t.Errorf("the retry answered with %s, want the message the key sent (%s)", again.ID, first.ID)
	}

	// A retry that arrives with different words does not overwrite what was delivered. Somebody who
	// changed their mind has written a new message, and replacing the read one would rewrite history.
	edited, created, err := c.send(t, c.provider, "Make that half past nine.", "msg-retry-key")
	if err != nil {
		t.Fatalf("the retry with different words: %v", err)
	}
	if created || edited.Body != first.Body {
		t.Errorf("a retry under a spent key answered %q (created %t), want the original %q — "+
			"DO NOTHING rather than DO UPDATE", edited.Body, created, first.Body)
	}

	page, _, err := c.read(t, c.customer, MessageQuery{})
	if err != nil {
		t.Fatalf("reading the conversation: %v", err)
	}
	if len(page.Messages) != 1 {
		t.Errorf("the conversation holds %d messages after two retries, want one", len(page.Messages))
	}
}

// TestOneKeyPerPartyPerConversation is 000502's correction applied here from the start.
//
// Two writers sit inside one `(job_id, provider_id)` pair. A key scoped without `sent_by` would let
// the two clients collide on a value both happened to generate, and the second would be answered from
// the first's row — a message that was never sent, reported as sent, in the other party's voice.
func TestOneKeyPerPartyPerConversation(t *testing.T) {
	c := newConversation(t, "msg-key-party")

	const shared = "a-key-both-clients-generated"

	mine, created, err := c.send(t, c.provider, "Nine o'clock suits.", shared)
	if err != nil || !created {
		t.Fatalf("the provider's message = %v (created %t), want it sent", err, created)
	}
	theirs, created, err := c.send(t, c.customer, "Nine is fine.", shared)
	if err != nil {
		t.Fatalf("the customer's message under the same key was refused: %v — a key is scoped to the "+
			"party that sent it, so the two cannot collide", err)
	}
	if !created {
		t.Fatal("the customer's message was answered from the provider's row")
	}
	if theirs.ID == mine.ID {
		t.Fatal("both parties were handed one row under one key")
	}
	if theirs.SentBy != PartyCustomer || mine.SentBy != PartyProvider {
		t.Errorf("the two messages read %s and %s, want customer and provider", theirs.SentBy, mine.SentBy)
	}
}

// TestConcurrentSendsOfOneMessageWriteItOnce is the case `ON CONFLICT` covers and the middleware
// cannot: two requests under one key that both reach the service.
//
// SHIP-15 refuses the second with `idempotency_request_in_progress` while Redis is answering, so this
// is what is left when it is not — two API instances, a failover, or a retry whose cached entry has
// expired on both sides. The same case race_test.go covers for the award.
func TestConcurrentSendsOfOneMessageWriteItOnce(t *testing.T) {
	c := newConversation(t, "msg-concurrent")

	const racers = 6

	var (
		ready sync.WaitGroup
		wg    sync.WaitGroup
		start = make(chan struct{})
		mu    sync.Mutex
		ids   = map[uuid.UUID]int{}
		fails []error
	)
	ready.Add(racers)
	wg.Add(racers)
	for range racers {
		go func() {
			defer wg.Done()

			ready.Done()
			<-start

			sent, _, err := c.svc.SendMessage(t.Context(), c.pool, c.provider, c.job, c.bid.ID,
				Note{Body: "Nine o'clock suits.", Key: "msg-concurrent-key"})

			mu.Lock()
			defer mu.Unlock()
			if err != nil {
				fails = append(fails, err)
				return
			}
			ids[sent.ID]++
		}()
	}
	ready.Wait()
	close(start)
	wg.Wait()

	for _, err := range fails {
		t.Errorf("a concurrent send failed: %v", err)
	}
	if len(ids) != 1 {
		t.Errorf("the concurrent sends answered with %d different messages, want 1: %v", len(ids), ids)
	}

	page, _, err := c.read(t, c.customer, MessageQuery{})
	if err != nil {
		t.Fatalf("reading the conversation: %v", err)
	}
	if len(page.Messages) != 1 {
		t.Errorf("%d messages were written by %d concurrent sends of one, want 1", len(page.Messages), racers)
	}
}

// TestAMessageWithNoKeyIsRefused holds the check SHIP-15's middleware makes unreachable.
//
// Checked all the same, for [ErrNoIdempotencyKey]'s reason: the key is a column, and a message stored
// without one is a message a later retry cannot be matched to. A service reached from a test, a worker
// or a future administrative path must not be able to write one.
func TestAMessageWithNoKeyIsRefused(t *testing.T) {
	c := newConversation(t, "msg-no-key")

	if _, _, err := c.send(t, c.customer, "No key on this one.", ""); !errors.Is(err, ErrNoIdempotencyKey) {
		t.Errorf("sending with no key = %v, want ErrNoIdempotencyKey", err)
	}
	page, _, err := c.read(t, c.customer, MessageQuery{})
	if err != nil {
		t.Fatalf("reading the conversation: %v", err)
	}
	if len(page.Messages) != 0 {
		t.Errorf("%d messages were written, want none", len(page.Messages))
	}
}

// --- what may be written --------------------------------------------------------------------------

// TestAnEmptyMessageIsRefusedAndTheFieldIsNamed is the validator in front of `ck_job_messages_body`.
//
// The trim is in the domain rather than at the edge deliberately: a message of three spaces is empty,
// and which layer noticed that must not change the answer.
func TestAnEmptyMessageIsRefusedAndTheFieldIsNamed(t *testing.T) {
	c := newConversation(t, "msg-empty")

	for _, body := range []string{"", "   ", "\t\n "} {
		t.Run(fmt.Sprintf("%q", body), func(t *testing.T) {
			_, _, err := c.send(t, c.customer, body, "msg-empty-"+body)
			var apiErr *httpx.Error
			if !errors.As(err, &apiErr) {
				t.Fatalf("sending %q = %v, want a validation error", body, err)
			}
			if apiErr.Status != http.StatusUnprocessableEntity {
				t.Errorf("sending %q answered %d, want 422", body, apiErr.Status)
			}
			if !strings.Contains(fmt.Sprint(apiErr.Details), "body") {
				t.Errorf("the refusal does not name the field: %v", apiErr.Details)
			}
		})
	}
}

// TestAMessageOverTheLimitIsRefused holds [maxMessageLen] against `ck_job_messages_body`, and holds
// the boundary from both sides so an off-by-one is visible.
func TestAMessageOverTheLimitIsRefused(t *testing.T) {
	c := newConversation(t, "msg-long")

	if _, _, err := c.send(t, c.customer, strings.Repeat("a", maxMessageLen), "msg-long-ok"); err != nil {
		t.Errorf("a message of exactly %d characters was refused: %v", maxMessageLen, err)
	}
	if _, _, err := c.send(t, c.customer, strings.Repeat("a", maxMessageLen+1), "msg-long-no"); err == nil {
		t.Errorf("a message of %d characters was accepted", maxMessageLen+1)
	}
}

// TestAMessageIsStoredExactlyAsItWasWritten is the negative half of the disclosure rule, at the row.
//
// The platform trims surrounding whitespace and does nothing else. Anything that reworded, truncated
// or annotated somebody's sentence would be putting words in their mouth, and the word-level guard
// below would have no way to tell the platform's addition from the sender's own.
func TestAMessageIsStoredExactlyAsItWasWritten(t *testing.T) {
	c := newConversation(t, "msg-verbatim")

	const written = "It's on the second floor — no lift, and the stairwell is 800mm wide."
	sent, _, err := c.send(t, c.customer, "  "+written+"\n", "msg-verbatim-key")
	if err != nil {
		t.Fatalf("sending: %v", err)
	}
	if sent.Body != written {
		t.Errorf("the message was stored as %q, want %q", sent.Body, written)
	}

	var stored string
	if err := c.pool.QueryRow(t.Context(),
		`SELECT body FROM job_messages WHERE id = $1`, sent.ID).Scan(&stored); err != nil {
		t.Fatalf("reading the row back: %v", err)
	}
	if stored != written {
		t.Errorf("the row holds %q, want %q", stored, written)
	}
}

// --- paging ---------------------------------------------------------------------------------------

// TestTheConversationPagesWithoutRepeatingOrSkipping is the cursor, checked the only way a keyset
// cursor can be: by walking the whole set and comparing it to the set.
//
// The tie-break is the point. `idx_job_messages_conversation` orders on `(created_at, id)` because two
// parties answering each other inside one millisecond is the ordinary case rather than an edge one,
// and these fixtures write far faster than that — so every page boundary here lands inside a tie.
func TestTheConversationPagesWithoutRepeatingOrSkipping(t *testing.T) {
	c := newConversation(t, "msg-paging")

	const total = 11
	want := make([]string, 0, total)
	for i := range total {
		body := fmt.Sprintf("message %02d", i)
		if _, _, err := c.send(t, c.customer, body, fmt.Sprintf("msg-paging-%02d", i)); err != nil {
			t.Fatalf("sending %s: %v", body, err)
		}
		want = append(want, body)
	}

	var (
		got    []string
		cursor MessageCursor
		pages  int
	)
	for {
		page, _, err := c.read(t, c.provider, MessageQuery{Limit: 4, After: cursor})
		if err != nil {
			t.Fatalf("page %d: %v", pages, err)
		}
		got = append(got, bodies(page)...)
		pages++

		if !page.HasMore {
			if !page.Next.IsZero() {
				t.Error("the last page carries a cursor, so a client would ask for an empty page")
			}
			break
		}
		if page.Next.IsZero() {
			t.Fatal("a page says there is more and names no position to continue from")
		}
		cursor = page.Next

		if pages > total {
			t.Fatal("the cursor is not advancing")
		}
	}

	if !equalStrings(got, want) {
		t.Errorf("walking the conversation gave %d messages:\n%q\nwant %d:\n%q",
			len(got), got, len(want), want)
	}
	if pages != 3 {
		t.Errorf("the walk took %d pages of 4 over %d messages, want 3", pages, total)
	}
}

// --- the disclosure guard -------------------------------------------------------------------------

// TestNothingTheCustomerTypedIsAddedToByThePlatform is Docs/01 §4.3 held the only way it can be held
// over a response carrying free text.
//
// # Why the guards this package already has are not enough here
//
// Every other response in this domain is numbers and instants, so a closed key set is a complete
// answer: there is no budget field because there is no job in the shape. **This one carries a
// sentence.** Wave 10 isolated the disclosure *"The customer has set a maximum."* — no field, no
// value, no digit — and a thirteen-test suite passed with it live, defeating a closed key set, a word
// search, a value search, an AST walk and a SQL column guard in turn.
//
// # So the assertion is inverted: every word must be accounted for
//
// The response is rendered, decomposed into words, and each one is required to have come from
// somewhere legitimate — a key of the closed set, an identifier, an instant, a party name, or the
// bodies the test itself typed. **Nothing else may be present.** A sentence the platform introduced
// fails this whatever it says, because the check does not need to recognise the disclosure: it only
// needs to notice a word nobody put there.
//
// The fixture makes the subject reachable, which is wave 11's third finding: the job carries a real
// budget of 4321.99 (see [market.publish], whose own comment says why), so a response that leaked it
// would have something to leak.
func TestNothingTheCustomerTypedIsAddedToByThePlatform(t *testing.T) {
	c := newConversation(t, "msg-disclosure")

	said := []string{
		"Is there parking at the pickup end?",
		"There is a loading zone out the front.",
	}
	if _, _, err := c.send(t, c.customer, said[0], "msg-disclosure-1"); err != nil {
		t.Fatalf("the customer's message: %v", err)
	}
	if _, _, err := c.send(t, c.provider, said[1], "msg-disclosure-2"); err != nil {
		t.Fatalf("the provider's message: %v", err)
	}

	// The budget is really on the job, so this is a search for a value that exists.
	var budget string
	if err := c.pool.QueryRow(t.Context(),
		`SELECT budget::text FROM jobs WHERE id = $1`, c.job).Scan(&budget); err != nil {
		t.Fatalf("reading the job's budget: %v", err)
	}
	if budget == "" {
		t.Fatal("the fixture job carries no budget, so this test proves nothing")
	}

	page, _, err := c.read(t, c.provider, MessageQuery{})
	if err != nil {
		t.Fatalf("reading the conversation: %v", err)
	}

	rendered := make([]messageResponse, 0, len(page.Messages))
	for _, m := range page.Messages {
		rendered = append(rendered, messageFrom(m))
	}
	body, err := json.Marshal(rendered)
	if err != nil {
		t.Fatalf("rendering the conversation: %v", err)
	}

	// Everything a legitimate response may be made of, decomposed by the **same** splitter the
	// response is — so `sent_by` contributes `sent` and `by`, and a UUID contributes its five
	// hyphen-separated runs. Decomposing both sides identically is what makes "accounted for" mean
	// the same thing on each.
	allowed := map[string]bool{}
	allow := func(tokens ...string) {
		for _, token := range tokens {
			for _, word := range words(token) {
				allowed[word] = true
			}
		}
	}

	// The closed key set, which is [messageResponse]'s JSON tags and nothing else.
	allow("id", "sent_by", "body", "created_at")

	// The party enumeration, which is the one vocabulary the platform contributes to this response.
	for _, party := range Parties {
		allow(string(party))
	}

	// What the two parties actually typed.
	allow(said...)

	// The identifiers and the instants — values rather than prose. Added by their own rendered form
	// rather than by a pattern, so an identifier or an instant belonging to something *else* would
	// still be a word nobody put there.
	for _, m := range page.Messages {
		allow(m.ID.String(), timestamp(m.CreatedAt))
	}

	for _, word := range words(string(body)) {
		if !allowed[word] {
			t.Errorf("the rendered conversation carries the word %q, which neither party typed and "+
				"no field of the closed set accounts for. A disclosure needs no field name and no "+
				"digit — wave 10 isolated one carrying neither — so any word the platform introduced "+
				"here is the failure this test exists for.\nrendered: %s", word, body)
		}
	}

	// And the budget is not in it by any spelling, which is the cheap check kept beside the
	// exhaustive one because it names what is being protected.
	for _, spelling := range []string{"budget", "maximum", "4321", "432199", "4321.99"} {
		if strings.Contains(strings.ToLower(string(body)), spelling) {
			t.Errorf("the rendered conversation contains %q: %s", spelling, body)
		}
	}
}

// words splits a string into lower-case runs of letters and digits.
//
// Punctuation, JSON syntax and whitespace are separators rather than content: the question is which
// *words* are present, and `{"body":"Is` is three of them. Lower-cased so that a sentence introduced
// at the start of a field is not hidden by its capital.
func words(s string) []string {
	fields := strings.FieldsFunc(strings.ToLower(s), func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r)
	})
	return fields
}

// equalStrings compares two slices element by element.
//
// Written out rather than reached for, because the assertions above are about *order* as much as
// membership and a helper that sorted would quietly hide a conversation read backwards.
func equalStrings(got, want []string) bool {
	if len(got) != len(want) {
		return false
	}
	for i := range got {
		if got[i] != want[i] {
			return false
		}
	}
	return true
}

// --- the store, where the domain cannot look --------------------------------------------------------

// TestTheDatabaseRefusesAMessageFromNeitherParty is `ck_job_messages_sent_by`, which is the schema's
// half of the rule the service keeps in Go.
//
// Docs/02 §4 has two parties to a negotiation and an administrator who reads it. A row attributed to
// anybody else is a message in somebody's voice that they did not write, and it is refused by the
// table rather than only by the path that writes to it.
func TestTheDatabaseRefusesAMessageFromNeitherParty(t *testing.T) {
	c := newConversation(t, "msg-check")

	_, err := c.pool.Exec(t.Context(), `
		INSERT INTO job_messages (id, job_id, provider_id, sent_by, body)
		VALUES ($1, $2, $3, 'administrator', 'Sorting this out.')`,
		uuid.Must(uuid.NewV7()), c.job, c.provider)
	if err == nil {
		t.Fatal("a message from an administrator was written")
	}
	if !strings.Contains(err.Error(), "ck_job_messages_sent_by") {
		t.Errorf("expected ck_job_messages_sent_by to refuse it, got: %v", err)
	}
}

// TestAConversationBelongsToARealJobAndProvider is Docs/10 §3.3's foreign key rule.
//
// ON DELETE RESTRICT rather than a cascade, for 000500's reason: a job is cancelled rather than
// removed, and Docs/05 §3.1 requires the commercial record retained.
func TestAConversationBelongsToARealJobAndProvider(t *testing.T) {
	c := newConversation(t, "msg-fk")

	if _, _, err := c.send(t, c.customer, "Still here.", "msg-fk-seed"); err != nil {
		t.Fatalf("seeding the conversation: %v", err)
	}

	t.Run("an unknown job is refused", func(t *testing.T) {
		_, err := c.pool.Exec(t.Context(), `
			INSERT INTO job_messages (id, job_id, provider_id, sent_by, body)
			VALUES ($1, $2, $3, 'customer', 'Nowhere.')`,
			uuid.Must(uuid.NewV7()), uuid.Must(uuid.NewV7()), c.provider)
		if err == nil || !strings.Contains(err.Error(), "fk_job_messages_job") {
			t.Errorf("expected fk_job_messages_job to refuse it, got: %v", err)
		}
	})

	t.Run("an unknown provider is refused", func(t *testing.T) {
		_, err := c.pool.Exec(t.Context(), `
			INSERT INTO job_messages (id, job_id, provider_id, sent_by, body)
			VALUES ($1, $2, $3, 'customer', 'Nobody.')`,
			uuid.Must(uuid.NewV7()), c.job, uuid.Must(uuid.NewV7()))
		if err == nil || !strings.Contains(err.Error(), "fk_job_messages_provider") {
			t.Errorf("expected fk_job_messages_provider to refuse it, got: %v", err)
		}
	})

	t.Run("deleting the job is refused while a message exists", func(t *testing.T) {
		if _, err := c.pool.Exec(t.Context(), `DELETE FROM bids WHERE job_id = $1`, c.job); err != nil {
			t.Fatalf("clearing the bids so the job's only reference is the message: %v", err)
		}
		if _, err := c.pool.Exec(t.Context(), `DELETE FROM jobs WHERE id = $1`, c.job); err == nil {
			t.Error("a job was deleted, taking the conversation on it with it")
		}
	})
}

// TestAMessageIsNotWrittenOutsideAConversationTheCallerCanReach is the service's own refusal read off
// the table rather than off the error.
//
// A test that checked only the returned sentinel would pass against an implementation that refused
// *and wrote anyway* — which is not a shape anybody would write deliberately and is exactly what an
// early return in the wrong place produces.
func TestAMessageIsNotWrittenOutsideAConversationTheCallerCanReach(t *testing.T) {
	c := newConversation(t, "msg-nothing-written")
	stranger := newCustomer(t, c.pool, "msg-nothing@example.com", "+61416000096")

	if _, _, err := c.send(t, stranger, "Let me in.", "msg-nothing-key"); !errors.Is(err, ErrNotBidOwner) {
		t.Fatalf("the stranger's message = %v, want ErrNotBidOwner", err)
	}

	var rows int
	if err := c.pool.QueryRow(t.Context(),
		`SELECT count(*) FROM job_messages WHERE job_id = $1`, c.job).Scan(&rows); err != nil {
		t.Fatalf("counting the messages on %s: %v", c.job, err)
	}
	if rows != 0 {
		t.Errorf("%d messages were written by a caller who was refused, want none", rows)
	}
}

// TestMessagesOutsideATransactionAreFine is the deliberate absence of [ErrNotInTransaction], asserted
// so that its absence is a decision rather than an omission.
//
// [Service.ReviseBid], [Service.WithdrawBid], [Service.CounterOffer] and [Service.AwardBid] all refuse
// a pool, because each reads a status, decides against it and writes — and outside a transaction the
// lock that made the decision safe is released before the write. **Nothing here reads a status at
// all.** The two facts a message rests on, `bids.provider_id` and `jobs.customer_id`, are immutable
// for the life of the row, and the retry guarantee is `ON CONFLICT`'s, which holds statement by
// statement. That is [Service.PlaceBid]'s position exactly.
func TestMessagesOutsideATransactionAreFine(t *testing.T) {
	c := newConversation(t, "msg-no-tx")

	if _, _, err := c.svc.SendMessage(t.Context(), c.pool, c.customer, c.job, c.bid.ID,
		Note{Body: "On the pool.", Key: "msg-no-tx-key"}); err != nil {
		t.Fatalf("sending on the pool = %v, want it to succeed", err)
	}

	// And in one, because a caller who has opened a transaction for other reasons must be able to
	// join it — which is the whole of why every store method takes a db.Runner.
	err := db.InTx(t.Context(), c.pool, func(ctx context.Context, r db.Runner) error {
		_, _, err := c.svc.SendMessage(ctx, r, c.provider, c.job, c.bid.ID,
			Note{Body: "In a transaction.", Key: "msg-tx-key"})
		return err
	})
	if err != nil {
		t.Fatalf("sending inside a transaction: %v", err)
	}

	page, _, err := c.read(t, c.customer, MessageQuery{})
	if err != nil {
		t.Fatalf("reading the conversation: %v", err)
	}
	if len(page.Messages) != 2 {
		t.Errorf("the conversation holds %d messages, want both", len(page.Messages))
	}
}
