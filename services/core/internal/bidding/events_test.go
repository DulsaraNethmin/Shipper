package bidding

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/DulsaraNethmin/Shipper/services/core/internal/db"
)

// SHIP-136's *Done when* for this domain: every state change in Docs/01 §4.5 emits its event from
// the domain rather than from the API layer.
//
// # Everything below reads the outbox table, and none of it reads a recording sink
//
// A stub sink can be told anything. What the ticket has to demonstrate is that the row **commits
// with the change it describes** (Docs/06 §4.0), and only the real writer inside the real
// transaction can show that — so the fixtures pass `events.NewOutbox()` and these read what a
// consumer would.
//
// The rollback test is the one that could not be written any other way.

// emitted is every outbox row this domain wrote about one bid, oldest first.
//
// Scoped to `aggregate_type = 'bid'` and to one identifier, which is the fencing every assertion in
// this repository about a shared table needs: `jobs` writes into the same table in the same
// transaction on the award path, and a query that counted rows would count its `job.status_changed`
// too.
func emitted(t *testing.T, m market, bid uuid.UUID) []storedEvent {
	t.Helper()
	return eventsOn(t, m, "bid", bid)
}

type storedEvent struct {
	eventType string
	payload   map[string]any
}

func eventsOn(t *testing.T, m market, aggregateType string, id uuid.UUID) []storedEvent {
	t.Helper()

	rows, err := m.pool.Query(t.Context(), `
		SELECT event_type, payload::text
		FROM outbox
		WHERE aggregate_type = $1 AND aggregate_id = $2
		ORDER BY id ASC`, aggregateType, id)
	if err != nil {
		t.Fatalf("reading the outbox for %s %s: %v", aggregateType, id, err)
	}
	defer rows.Close()

	var found []storedEvent
	for rows.Next() {
		var (
			eventType string
			raw       string
		)
		if err := rows.Scan(&eventType, &raw); err != nil {
			t.Fatalf("scanning an outbox row: %v", err)
		}

		var payload map[string]any
		if err := json.Unmarshal([]byte(raw), &payload); err != nil {
			t.Fatalf("%s carries a payload that is not an object: %v", eventType, err)
		}
		found = append(found, storedEvent{eventType: eventType, payload: payload})
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("reading the outbox for %s %s: %v", aggregateType, id, err)
	}
	return found
}

// only is the single event about this bid, and it fails rather than indexing into nothing.
func only(t *testing.T, got []storedEvent, want string) storedEvent {
	t.Helper()

	if len(got) != 1 {
		t.Fatalf("the outbox holds %s for this bid, want exactly one %s", describe(got), want)
	}
	if got[0].eventType != want {
		t.Fatalf("the outbox holds %s, want %s", got[0].eventType, want)
	}
	return got[0]
}

func describe(got []storedEvent) string {
	if len(got) == 0 {
		return "nothing"
	}
	names := make([]string, 0, len(got))
	for _, e := range got {
		names = append(names, e.eventType)
	}
	return "[" + strings.Join(names, " ") + "]"
}

// field reads one payload value as a string, failing if it is missing.
func field(t *testing.T, e storedEvent, name string) string {
	t.Helper()

	value, ok := e.payload[name]
	if !ok {
		t.Fatalf("%s carries no %s", e.eventType, name)
	}
	return fmt.Sprint(value)
}

// TestAPlacementEmitsItsEvent is Docs/01 §4.5's "new bid".
func TestAPlacementEmitsItsEvent(t *testing.T) {
	m := newMarket(t)

	placed, _, err := m.place(t, m.provider, m.job, offer("key-event-placed"))
	if err != nil {
		t.Fatalf("placing: %v", err)
	}

	event := only(t, emitted(t, m, placed.ID), EventBidPlaced)
	if got := field(t, event, "bid_id"); got != placed.ID.String() {
		t.Errorf("bid_id is %s, want %s", got, placed.ID)
	}
	if got := field(t, event, "job_id"); got != m.job.String() {
		t.Errorf("job_id is %s, want %s", got, m.job)
	}
	if got := field(t, event, "provider_id"); got != m.provider.String() {
		t.Errorf("provider_id is %s, want %s", got, m.provider)
	}
	if got := field(t, event, "offered_by"); got != string(PartyProvider) {
		t.Errorf("offered_by is %s, want %s", got, PartyProvider)
	}
	if got := field(t, event, "amount_cents"); got != "45000" {
		t.Errorf("amount_cents is %s, want 45000", got)
	}
}

// TestARetriedPlacementEmitsOnce is the property an idempotency key is for, seen from the outbox.
//
// The middleware absorbs a retry that reaches it; this is the retry that outlives it and runs all
// the way into the transaction. It writes no second bid, and it must write no second event — a
// customer told twice about one offer is the symptom, and nothing in the response would show it.
func TestARetriedPlacementEmitsOnce(t *testing.T) {
	m := newMarket(t)

	placed, created, err := m.place(t, m.provider, m.job, offer("key-event-retry"))
	if err != nil || !created {
		t.Fatalf("placing: %v (created %v)", err, created)
	}

	again, created, err := m.place(t, m.provider, m.job, offer("key-event-retry"))
	if err != nil {
		t.Fatalf("retrying: %v", err)
	}
	if created {
		t.Fatal("the retry created a second bid")
	}
	if again.ID != placed.ID {
		t.Fatalf("the retry answered with %s, want %s", again.ID, placed.ID)
	}

	if got := emitted(t, m, placed.ID); len(got) != 1 {
		t.Errorf("the retry left %s in the outbox, want one bid.placed", describe(got))
	}
}

// TestARevisionEmitsItsEvent is Docs/01 §4.2's middle verb — a live offer at a new price.
func TestARevisionEmitsItsEvent(t *testing.T) {
	m := newMarket(t)

	placed, _, err := m.place(t, m.provider, m.job, offer("key-event-revise"))
	if err != nil {
		t.Fatalf("placing: %v", err)
	}
	if _, err := m.revise(t, m.provider, m.job, placed.ID,
		Revision{AmountCents: ptr(int64(39000))}); err != nil {
		t.Fatalf("revising: %v", err)
	}

	got := emitted(t, m, placed.ID)
	if len(got) != 2 || got[1].eventType != EventBidRevised {
		t.Fatalf("the outbox holds %s, want [%s %s]", describe(got), EventBidPlaced, EventBidRevised)
	}
	if amount := field(t, got[1], "amount_cents"); amount != "39000" {
		t.Errorf("bid.revised carries amount_cents %s, want the new 39000", amount)
	}
}

// TestAWithdrawalEmitsItsEvent is Docs/01 §4.5's "withdrawal".
//
// The second withdrawal is the interesting half: [Service.WithdrawBid] absorbs it and writes
// nothing, so it must emit nothing. An offer withdrawn twice is one withdrawal.
func TestAWithdrawalEmitsItsEventOnceHoweverOftenItIsAsked(t *testing.T) {
	m := newMarket(t)

	placed, _, err := m.place(t, m.provider, m.job, offer("key-event-withdraw"))
	if err != nil {
		t.Fatalf("placing: %v", err)
	}
	if _, err := m.withdraw(t, m.provider, m.job, placed.ID); err != nil {
		t.Fatalf("withdrawing: %v", err)
	}
	if _, err := m.withdraw(t, m.provider, m.job, placed.ID); err != nil {
		t.Fatalf("withdrawing again: %v", err)
	}

	got := emitted(t, m, placed.ID)
	if len(got) != 2 || got[1].eventType != EventBidWithdrawn {
		t.Fatalf("the outbox holds %s, want [%s %s]", describe(got), EventBidPlaced, EventBidWithdrawn)
	}
	if status := field(t, got[1], "status"); status != string(StatusWithdrawn) {
		t.Errorf("bid.withdrawn carries status %s, want %s", status, StatusWithdrawn)
	}
}

// TestACounterEmitsOneEventNamingWhatItDisplaced is Docs/01 §4.5's "counter-offer".
//
// The event is on the counter and names the offer it superseded, which is the whole of the design
// note in events.go: two rows change and one thing happened.
func TestACounterEmitsOneEventNamingWhatItDisplaced(t *testing.T) {
	m := newMarket(t)

	placed, _, err := m.place(t, m.provider, m.job, offer("key-event-counter"))
	if err != nil {
		t.Fatalf("placing: %v", err)
	}

	counter, _, err := m.counter(t, m.customer, m.job, placed.ID, counterOf(38000, "key-event-counter-c"))
	if err != nil {
		t.Fatalf("countering: %v", err)
	}

	// Nothing further on the displaced row: it is the party being countered, and one event told
	// them.
	if got := emitted(t, m, placed.ID); len(got) != 1 || got[0].eventType != EventBidPlaced {
		t.Errorf("the displaced offer's outbox holds %s, want one %s", describe(got), EventBidPlaced)
	}

	event := only(t, emitted(t, m, counter.ID), EventBidCountered)
	if got := field(t, event, "superseded_bid_id"); got != placed.ID.String() {
		t.Errorf("superseded_bid_id is %s, want %s", got, placed.ID)
	}
	if got := field(t, event, "offered_by"); got != string(PartyCustomer) {
		t.Errorf("offered_by is %s, want %s — the counter was the customer's", got, PartyCustomer)
	}
	if got := field(t, event, "amount_cents"); got != "38000" {
		t.Errorf("amount_cents is %s, want the counter's 38000", got)
	}
}

// TestAnAwardEmitsTheAcceptanceAndOneRejectionPerClosedOffer is Docs/01 §4.5's "bid accepted", read
// to the end of the sentence: the sweep closes every other live offer and each of those providers
// is somebody to tell.
//
// This is the test the sweep returning its rows exists for. A count over `bids` afterwards would
// pass just as well if the events had been derived from a `SELECT ... WHERE status = 'Rejected'`,
// which would also have picked up anything an earlier award closed.
func TestAnAwardEmitsTheAcceptanceAndOneRejectionPerClosedOffer(t *testing.T) {
	m := newMarket(t)

	winner, _, err := m.place(t, m.provider, m.job, offer("key-event-award-winner"))
	if err != nil {
		t.Fatalf("the winning offer: %v", err)
	}

	var losers []uuid.UUID
	for n := 1; n <= 2; n++ {
		rival := m.rival(t, 9100+n)
		bid, _, err := m.place(t, rival, m.job, offer(fmt.Sprintf("key-event-award-rival-%d", n)))
		if err != nil {
			t.Fatalf("a competing offer: %v", err)
		}
		losers = append(losers, bid.ID)
	}

	if _, err := m.award(t, m.customer, m.job, winner.ID); err != nil {
		t.Fatalf("awarding: %v", err)
	}

	accepted := emitted(t, m, winner.ID)
	if len(accepted) != 2 || accepted[1].eventType != EventBidAccepted {
		t.Fatalf("the winner's outbox holds %s, want [%s %s]",
			describe(accepted), EventBidPlaced, EventBidAccepted)
	}
	if got := field(t, accepted[1], "customer_id"); got != m.customer.String() {
		t.Errorf("bid.accepted names customer %s, want %s", got, m.customer)
	}

	for _, loser := range losers {
		got := emitted(t, m, loser)
		if len(got) != 2 || got[1].eventType != EventBidRejected {
			t.Fatalf("a closed offer's outbox holds %s, want [%s %s]",
				describe(got), EventBidPlaced, EventBidRejected)
		}
		if status := field(t, got[1], "status"); status != string(StatusRejected) {
			t.Errorf("bid.rejected carries status %s, want %s", status, StatusRejected)
		}
	}

	// The job moved in the same transaction, through `jobs`' guard, which emits its own event on
	// its own aggregate. Asserted here because the two together are what a consumer sees, and
	// because a future refactor that moved the transition out of this transaction would break it.
	if got := eventsOn(t, m, "job", m.job); len(got) == 0 {
		t.Error("the award moved the job and emitted nothing about it")
	}
}

// TestTheAwardEmitsOnlyForTheOffersItItselfClosed.
//
// **This test exists because a mutation survived without it.** Replacing
// [postgresStore.rejectCompeting]'s `RETURNING` with a second `SELECT ... WHERE status = 'Rejected'`
// passed the entire suite, because nothing but the sweep writes 'Rejected' today and a job is
// awarded once — so on every fixture the two queries return the same rows.
//
// They stop agreeing the moment anything else closes an offer, which is a ticket rather than a
// hypothetical: `Rejected` is Docs/02 §4's "an offer the customer declined", and a customer
// declining one by hand is the obvious next writer. At that point a second `SELECT` would emit
// `bid.rejected` again for an offer closed last week, and the provider would be told twice.
//
// The pre-existing row is inserted directly, because no endpoint can produce one. That is the point:
// the test is about what the *statement* reports, not about how the row got there.
func TestTheAwardEmitsOnlyForTheOffersItItselfClosed(t *testing.T) {
	m := newMarket(t)

	winner, _, err := m.place(t, m.provider, m.job, offer("key-event-sweep-winner"))
	if err != nil {
		t.Fatalf("the winning offer: %v", err)
	}

	// An offer closed before this award, which the sweep must not name. Written straight to the
	// table: this is a state a later ticket produces and no endpoint produces today.
	alreadyClosed := uuid.Must(uuid.NewV7())
	rival := m.rival(t, 9106)
	if _, err := m.pool.Exec(t.Context(), `
		INSERT INTO bids (id, job_id, provider_id, offered_by, status, amount, pickup_at, deliver_by)
		VALUES ($1, $2, $3, 'provider', 'Rejected', 410.00, $4, $5)`,
		alreadyClosed, m.job, rival, testInstant.Add(48*time.Hour), testInstant.Add(56*time.Hour),
	); err != nil {
		t.Fatalf("inserting an already-closed offer: %v", err)
	}

	if _, err := m.award(t, m.customer, m.job, winner.ID); err != nil {
		t.Fatalf("awarding: %v", err)
	}

	if got := emitted(t, m, alreadyClosed); len(got) != 0 {
		t.Errorf("the award emitted %s about an offer that was already closed before it ran",
			describe(got))
	}
	if stored := m.row(t, alreadyClosed); stored.status != string(StatusRejected) {
		t.Errorf("the already-closed offer is now %s; the sweep rewrote a record it should not have "+
			"touched", stored.status)
	}
}

// TestARepeatedAwardEmitsNothingFurther.
//
// Docs/11 §3's SHIP-94 entry: the award is idempotent by state rather than by key, which means a
// retry that reaches this transaction returns before the accept. Since SHIP-93 it has a second
// write to not do — the sweep — and now a third: the events. A provider told twice that they have
// won is the symptom nothing in the response would show.
func TestARepeatedAwardEmitsNothingFurther(t *testing.T) {
	m := newMarket(t)

	winner, _, err := m.place(t, m.provider, m.job, offer("key-event-award-twice"))
	if err != nil {
		t.Fatalf("placing: %v", err)
	}
	rival := m.rival(t, 9103)
	loser, _, err := m.place(t, rival, m.job, offer("key-event-award-twice-rival"))
	if err != nil {
		t.Fatalf("the competing offer: %v", err)
	}

	if _, err := m.award(t, m.customer, m.job, winner.ID); err != nil {
		t.Fatalf("awarding: %v", err)
	}
	if _, err := m.award(t, m.customer, m.job, winner.ID); err != nil {
		t.Fatalf("awarding again: %v", err)
	}

	if got := emitted(t, m, winner.ID); len(got) != 2 {
		t.Errorf("the repeated award left %s on the winner, want [%s %s]",
			describe(got), EventBidPlaced, EventBidAccepted)
	}
	if got := emitted(t, m, loser.ID); len(got) != 2 {
		t.Errorf("the repeated award left %s on the closed offer, want [%s %s]",
			describe(got), EventBidPlaced, EventBidRejected)
	}
}

// TestAnEventWhoseTransactionRollsBackIsNotInTheOutbox is the whole reason the seam takes a
// db.Runner, and it is the one property a recording sink cannot show.
//
// Docs/06 §4.0 names the failure: publish then fail to commit, and a consumer acts on an award that
// never happened. The transaction below places a bid, which emits, and then fails.
func TestAnEventWhoseTransactionRollsBackIsNotInTheOutbox(t *testing.T) {
	m := newMarket(t)

	sentinel := errors.New("the caller changed its mind after the bid was written")

	var placed uuid.UUID
	err := db.InTx(t.Context(), m.pool, func(ctx context.Context, r db.Runner) error {
		bid, _, err := m.svc.PlaceBid(ctx, r, m.provider, m.job, offer("key-event-rollback"))
		if err != nil {
			return err
		}
		placed = bid.ID
		return sentinel
	})
	if !errors.Is(err, sentinel) {
		t.Fatalf("the transaction reported %v, want the sentinel", err)
	}

	if got := emitted(t, m, placed); len(got) != 0 {
		t.Errorf("the rolled-back transaction left %s in the outbox", describe(got))
	}

	var bids int
	if err := m.pool.QueryRow(t.Context(),
		`SELECT count(*) FROM bids WHERE id = $1`, placed).Scan(&bids); err != nil {
		t.Fatalf("counting the bid: %v", err)
	}
	if bids != 0 {
		t.Error("the bid survived a rolled-back transaction, so this test proves nothing")
	}
}

// TestNoBidEventCarriesAnythingOfTheCustomers is CLAUDE.md's budget invariant applied to the one
// surface that travels furthest.
//
// **A closed set of keys rather than a search for "budget"**, which is SHIP-83's finding brought to
// the outbox: a search catches `budget_cents` and misses `max_price`. A field added to any payload
// in this package fails this whatever it is called, and adding it to the list is where somebody has
// to think about what they are putting on a topic.
//
// `amount_cents` is in the list and is not an exception to the rule. It is the provider's own
// number, or — on a counter — an amount the customer deliberately offered to that provider, which
// `GET /v1/jobs/{id}/bids/{bid_id}/chain` has served since SHIP-88. Neither is the private maximum
// Docs/01 §4.3 protects.
func TestNoBidEventCarriesAnythingOfTheCustomers(t *testing.T) {
	permitted := map[string]bool{
		"schema_version": true,

		"bid_id": true, "job_id": true, "provider_id": true, "customer_id": true,
		"offered_by": true, "status": true, "superseded_bid_id": true,

		"amount_cents": true, "pickup_at": true, "deliver_by": true,
	}

	m := newMarket(t)

	placed, _, err := m.place(t, m.provider, m.job, offer("key-event-keys"))
	if err != nil {
		t.Fatalf("placing: %v", err)
	}
	if _, err := m.revise(t, m.provider, m.job, placed.ID,
		Revision{AmountCents: ptr(int64(41000))}); err != nil {
		t.Fatalf("revising: %v", err)
	}
	counter, _, err := m.counter(t, m.customer, m.job, placed.ID, counterOf(37000, "key-event-keys-c"))
	if err != nil {
		t.Fatalf("countering: %v", err)
	}
	rival := m.rival(t, 9104)
	losing, _, err := m.place(t, rival, m.job, offer("key-event-keys-rival"))
	if err != nil {
		t.Fatalf("the competing offer: %v", err)
	}
	if _, err := m.withdraw(t, rival, m.job, losing.ID); err != nil {
		t.Fatalf("withdrawing: %v", err)
	}
	// A live competing offer for the award to sweep, so that bid.rejected is exercised too.
	second := m.rival(t, 9105)
	if _, _, err := m.place(t, second, m.job, offer("key-event-keys-swept")); err != nil {
		t.Fatalf("the second competing offer: %v", err)
	}
	if _, err := m.award(t, m.customer, m.job, counter.ID); err == nil {
		t.Fatal("a customer's own counter was awarded")
	}

	provider, _, err := m.counter(t, m.provider, m.job, counter.ID, counterOf(40000, "key-event-keys-p"))
	if err != nil {
		t.Fatalf("the provider's counter: %v", err)
	}
	if _, err := m.award(t, m.customer, m.job, provider.ID); err != nil {
		t.Fatalf("awarding: %v", err)
	}

	// Every event this domain wrote in the whole exchange, whichever bid it is about.
	rows, err := m.pool.Query(t.Context(),
		`SELECT event_type, payload::text FROM outbox WHERE aggregate_type = 'bid'`)
	if err != nil {
		t.Fatalf("reading the outbox: %v", err)
	}
	defer rows.Close()

	seen := map[string]bool{}
	for rows.Next() {
		var eventType, raw string
		if err := rows.Scan(&eventType, &raw); err != nil {
			t.Fatalf("scanning an outbox row: %v", err)
		}
		seen[eventType] = true

		var payload map[string]any
		if err := json.Unmarshal([]byte(raw), &payload); err != nil {
			t.Fatalf("%s carries a payload that is not an object: %v", eventType, err)
		}
		for key := range payload {
			if !permitted[key] {
				t.Errorf("%s carries %q, which is not in the permitted set. If it belongs on a "+
					"topic, add it here — and read Docs/01 §4.3 first, because an event travels "+
					"past every point a response body could have redacted it", eventType, key)
			}
		}
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("reading the outbox: %v", err)
	}

	// The exchange above is only worth anything if it produced all six. Listed rather than counted
	// so that a rewrite of the fixture that quietly stopped exercising one is reported.
	want := []string{
		EventBidPlaced, EventBidRevised, EventBidWithdrawn,
		EventBidCountered, EventBidAccepted, EventBidRejected,
	}
	sort.Strings(want)
	var missing []string
	for _, eventType := range want {
		if !seen[eventType] {
			missing = append(missing, eventType)
		}
	}
	if len(missing) > 0 {
		t.Fatalf("the exchange emitted no %s, so this test proves nothing about them",
			strings.Join(missing, ", "))
	}
}

// TestNewServiceRefusesAMissingSink.
//
// A nil sink is the failure that reports itself least: every row is written, every response is
// correct, and the only symptom is a notification nobody receives. The same call jobs.NewService
// and delivery.NewService make.
func TestNewServiceRefusesAMissingSink(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Error("NewService returned a service that emits nothing")
		}
	}()

	c := newTestService().clock
	NewService(nil, nil, nil, nil, nil, c)
}
