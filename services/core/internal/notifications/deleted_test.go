package notifications

import (
	"errors"
	"testing"

	"github.com/google/uuid"

	"github.com/DulsaraNethmin/Shipper/services/core/internal/clock"
	"github.com/DulsaraNethmin/Shipper/services/core/internal/testsupport/pgtest"
)

// SHIP-171b: a pseudonymised account stops being a notification recipient.
//
// # What was wrong, measured rather than reported
//
// SHIP-171 replaces a person's name, address and number and retains the transaction (Docs/05 §3.1).
// It did not suppress notifications, and its own Docs/11 §3 entry says so. Measured on the tree
// before this file existed, with an account pseudonymised exactly as internal/identity does it:
//
//	Consume of one bid.placed  -> 1 row, status pending, address "deleted:01a01424-…"
//	one Dispatch pass          -> that row and the queued one both claimed,
//	                              status failed, attempts 1, last_error "550 no such recipient"
//
// `failed` is not terminal, so the second number is the one that matters: it climbs on every later
// pass, for ever, on somebody the platform has deleted.
//
// # The two halves are different and both are here
//
// Nothing new is *written*, which is a property of resolve(); and nothing already queued
// *dispatches*, which is a property of Dispatch() over rows that already exist. The first cannot
// fix the second — those rows were written before the deletion — and the second cannot fix the
// first, because a row retired at dispatch has already been written to a person the platform
// erased.

// TestNoNotificationIsWrittenAddressedToADeletedAccount is the first half of the *Done when*.
func TestNoNotificationIsWrittenAddressedToADeletedAccount(t *testing.T) {
	pool := pgtest.DB(t)

	customer := newUser(t, pool, "deleted-consume@example.com", "+61400009101", "customer")
	deletions := deletionsFor(customer)
	service := NewService(&stubParties{customer: customer}, deletions,
		clock.NewFixed(testInstant), Senders{})

	env := envelopeOf(t, "bid.placed", map[string]any{
		"job_id":      uuid.Must(uuid.NewV7()).String(),
		"provider_id": uuid.Must(uuid.NewV7()).String(),
	})

	written, err := consume(t, pool, service, env)
	if err != nil {
		t.Fatalf("consuming an event for a deleted account: %v", err)
	}
	if written != 0 {
		t.Errorf("consume wrote %d notification(s) for an account the platform has deleted, want 0", written)
	}
	if rows := rowsFor(t, pool, env.ID); len(rows) != 0 {
		t.Errorf("the table holds %d row(s) for the event, want none: %q",
			len(rows), rows[0].Address)
	}
	if deletions.calls == 0 {
		t.Error("the deletion lookup was never consulted, so nothing above is evidence of anything")
	}
}

// TestOnlyTheDeletedRecipientIsSuppressed is the fixture check this file would be worthless
// without.
//
// A test whose whole assertion is that nothing was written passes just as well when the rule
// resolved nobody, when the job was missing, or when the consumer was broken in some unrelated way.
// So the same event has two recipients and exactly one of them is deleted: the count has to fall
// from two to one, and the row that survives has to be the other person's.
func TestOnlyTheDeletedRecipientIsSuppressed(t *testing.T) {
	pool := pgtest.DB(t)

	customer := newUser(t, pool, "deleted-both-customer@example.com", "+61400009102", "customer")
	provider := newUser(t, pool, "deleted-both-provider@example.com", "+61400009103", "provider")

	service := NewService(&stubParties{customer: customer, provider: provider},
		deletionsFor(provider), clock.NewFixed(testInstant), Senders{})

	job := uuid.Must(uuid.NewV7())
	env := envelopeOf(t, "bid.accepted", map[string]any{
		"job_id":      job.String(),
		"bid_id":      uuid.Must(uuid.NewV7()).String(),
		"customer_id": customer.String(),
		"provider_id": provider.String(),
	})

	written, err := consume(t, pool, service, env)
	if err != nil {
		t.Fatalf("consuming the award: %v", err)
	}
	if written != 1 {
		t.Fatalf("the award wrote %d notification(s), want 1 — the customer's, with the deleted "+
			"provider suppressed", written)
	}

	rows := rowsFor(t, pool, env.ID)
	if len(rows) != 1 {
		t.Fatalf("the table holds %d rows for the event, want 1", len(rows))
	}
	if rows[0].Recipient != customer {
		t.Errorf("the surviving row is addressed to %s, want the customer %s", rows[0].Recipient, customer)
	}
	if rows[0].Address != "deleted-both-customer@example.com" {
		t.Errorf("the surviving row is addressed to %q, want the customer's own address", rows[0].Address)
	}
}

// TestSuppressionDoesNotDependOnHowAnAddressReads is the row's other constraint, made a test.
//
// "Do not suppress by string-matching the pseudonym" cannot be demonstrated by a passing suite that
// happens not to do it, so both directions are driven from a lookup that answers by identifier
// alone:
//
//   - an account the platform has deleted whose stored address is an ordinary one is still
//     suppressed — which a `LIKE 'deleted:%'` implementation would notify;
//   - an account nobody has deleted whose address happens to read like a pseudonym is still
//     notified — which the same implementation would silently drop.
//
// The second is the one that matters more. An address is a value a person can influence, so an
// implementation keyed on its spelling is one a person could trigger.
func TestSuppressionDoesNotDependOnHowAnAddressReads(t *testing.T) {
	pool := pgtest.DB(t)

	// Deleted, and reachable-looking.
	erased := newUser(t, pool, "still-a-normal-address@example.com", "+61400009104", "customer")
	// Not deleted, and pseudonym-shaped. `deleted:` is what identity.PseudonymFor writes.
	lookalike := newUser(t, pool, "deleted:"+uuid.Must(uuid.NewV7()).String(), "+61400009105", "customer")

	for _, tc := range []struct {
		name string
		who  uuid.UUID
		want int
	}{
		{name: "deleted, ordinary address", who: erased, want: 0},
		{name: "not deleted, pseudonym-shaped address", who: lookalike, want: 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			service := NewService(&stubParties{customer: tc.who}, deletionsFor(erased),
				clock.NewFixed(testInstant), Senders{})

			env := envelopeOf(t, "bid.placed", map[string]any{
				"job_id":      uuid.Must(uuid.NewV7()).String(),
				"provider_id": uuid.Must(uuid.NewV7()).String(),
			})

			written, err := consume(t, pool, service, env)
			if err != nil {
				t.Fatalf("consuming: %v", err)
			}
			if written != tc.want {
				t.Errorf("consume wrote %d row(s), want %d — suppression is keyed on the "+
					"account having been deleted, never on how its address reads", written, tc.want)
			}
		})
	}
}

// TestAQueuedNotificationToADeletedAccountIsRetiredWithoutBeingAttempted is the second half of the
// *Done when*, and the clause it is written around is "so the attempts counter stops climbing".
//
// The row is written before the account is deleted, which is the only way these rows arise. The
// sender is a recorder rather than a failing one deliberately: a sender that failed would leave
// `failed` behind whether or not anything was suppressed, and what has to be shown is that nothing
// was handed to it at all.
func TestAQueuedNotificationToADeletedAccountIsRetiredWithoutBeingAttempted(t *testing.T) {
	pool := pgtest.DB(t)

	erased := newUser(t, pool, "queued-erased@example.com", "+61400009106", "customer")
	live := newUser(t, pool, "queued-live@example.com", "+61400009107", "customer")

	doomed := pending(t, pool, erased, ChannelEmail, "queued-erased@example.com")
	ordinary := pending(t, pool, live, ChannelEmail, "queued-live@example.com")

	mail := &recordingSender{}
	service := NewService(&stubParties{}, deletionsFor(erased), clock.NewFixed(testInstant),
		Senders{Email: mail})

	claimed, err := dispatchOnce(t, pool, service)
	if err != nil {
		t.Fatalf("dispatching: %v", err)
	}
	if claimed != 2 {
		t.Fatalf("the pass claimed %d row(s), want both", claimed)
	}

	status, attempts, lastError := stateOf(t, pool, doomed)
	if status != "undeliverable" {
		t.Errorf("the row for the deleted account is %s, want undeliverable — 000702's terminal "+
			"status, so the claim never takes it again", status)
	}
	if attempts != 0 {
		t.Errorf("the row for the deleted account has attempts=%d, want 0: nothing was attempted, "+
			"and the clause is that the counter stops climbing", attempts)
	}
	if lastError == "" {
		t.Error("the retired row records no reason, so nobody reading it can tell why it never went")
	}

	// The other person's message went out normally, which is what stops this being a test that
	// passes because the dispatcher is broken.
	if status, _, _ := stateOf(t, pool, ordinary); status != "sent" {
		t.Errorf("the live account's row is %s, want sent", status)
	}
	if len(mail.sent) != 1 || mail.sent[0] != "queued-live@example.com|Job body" {
		t.Errorf("the sender was handed %v, want the live account's message and nothing else", mail.sent)
	}
}

// TestARetiredNotificationIsNeverClaimedAgain is what "stops climbing" means over time.
//
// One pass proves the row was not attempted. It does not prove the counter has stopped, because a
// row left `pending` would also read as attempts=0 and would be claimed again on every later pass
// for ever — which is the defect, not the fix. So a second pass runs and has to find nothing.
func TestARetiredNotificationIsNeverClaimedAgain(t *testing.T) {
	pool := pgtest.DB(t)

	erased := newUser(t, pool, "retired-twice@example.com", "+61400009108", "customer")
	row := pending(t, pool, erased, ChannelEmail, "retired-twice@example.com")

	mail := &recordingSender{}
	service := NewService(&stubParties{}, deletionsFor(erased), clock.NewFixed(testInstant),
		Senders{Email: mail})

	if claimed, err := dispatchOnce(t, pool, service); err != nil || claimed != 1 {
		t.Fatalf("the first pass claimed %d row(s) (err %v), want 1", claimed, err)
	}

	// Far enough ahead that any backoff a `failed` row could have taken has elapsed, so a row
	// that had merely been deferred would be claimed here.
	later := NewService(&stubParties{}, deletionsFor(erased),
		clock.NewFixed(testInstant.Add(2*BackoffFor(20))), Senders{Email: mail})

	claimed, err := dispatchOnce(t, pool, later)
	if err != nil {
		t.Fatalf("the second pass: %v", err)
	}
	if claimed != 0 {
		t.Errorf("the second pass claimed %d row(s), want none — a retired row is terminal", claimed)
	}

	_, attempts, _ := stateOf(t, pool, row)
	if attempts != 0 {
		t.Errorf("after two passes the row has attempts=%d, want 0", attempts)
	}
	if len(mail.sent) != 0 {
		t.Errorf("the sender was handed %v across two passes, want nothing", mail.sent)
	}
}

// TestASentNotificationIsNotRewrittenByALaterDeletion.
//
// The guard on markRecipientDeleted's UPDATE, driven rather than read. A message that was delivered
// last month was delivered, and a deletion today does not make it undeliverable — a support query
// asking "did they ever receive it" must not be answered by a status this ticket wrote.
func TestASentNotificationIsNotRewrittenByALaterDeletion(t *testing.T) {
	pool := pgtest.DB(t)

	erased := newUser(t, pool, "already-sent@example.com", "+61400009109", "customer")
	row := pending(t, pool, erased, ChannelEmail, "already-sent@example.com")

	sent := NewService(&stubParties{}, noDeletions(), clock.NewFixed(testInstant),
		Senders{Email: &recordingSender{}})
	if _, err := dispatchOnce(t, pool, sent); err != nil {
		t.Fatalf("the delivering pass: %v", err)
	}
	if status, _, _ := stateOf(t, pool, row); status != "sent" {
		t.Fatalf("the row is %s before the deletion, want sent", status)
	}

	after := NewService(&stubParties{}, deletionsFor(erased), clock.NewFixed(testInstant),
		Senders{Email: &recordingSender{}})
	if claimed, err := dispatchOnce(t, pool, after); err != nil || claimed != 0 {
		t.Fatalf("the pass after the deletion claimed %d row(s) (err %v), want 0", claimed, err)
	}

	status, attempts, _ := stateOf(t, pool, row)
	if status != "sent" || attempts != 1 {
		t.Errorf("after the deletion the delivered row reads status=%s attempts=%d, want sent and 1",
			status, attempts)
	}
}

// TestADeletionLookupThatFailsStopsThePassRatherThanNotifying.
//
// Not a refinement. The failure mode this closes is the one that reads as success: a lookup that
// errored and was ignored answers "nobody is deleted", which is the defect restored inside the fix
// meant to close it. Both halves have to fail loudly, so both are driven.
func TestADeletionLookupThatFailsStopsThePassRatherThanNotifying(t *testing.T) {
	pool := pgtest.DB(t)

	customer := newUser(t, pool, "lookup-fails@example.com", "+61400009110", "customer")
	broken := &stubDeletions{err: errors.New("the deletion lookup is down")}

	consumer := NewService(&stubParties{customer: customer}, broken, clock.NewFixed(testInstant),
		Senders{})
	env := envelopeOf(t, "bid.placed", map[string]any{
		"job_id":      uuid.Must(uuid.NewV7()).String(),
		"provider_id": uuid.Must(uuid.NewV7()).String(),
	})
	if _, err := consume(t, pool, consumer, env); err == nil {
		t.Error("consume absorbed a failing deletion lookup and carried on resolving recipients")
	}
	if rows := rowsFor(t, pool, env.ID); len(rows) != 0 {
		t.Errorf("the failed consume left %d row(s) behind", len(rows))
	}

	pending(t, pool, customer, ChannelEmail, "lookup-fails@example.com")
	mail := &recordingSender{}
	dispatcher := NewService(&stubParties{}, broken, clock.NewFixed(testInstant),
		Senders{Email: mail})
	if _, err := dispatchOnce(t, pool, dispatcher); err == nil {
		t.Error("dispatch absorbed a failing deletion lookup and sent anyway")
	}
	if len(mail.sent) != 0 {
		t.Errorf("the sender was handed %v after the lookup failed, want nothing", mail.sent)
	}
}

// TestNewServiceRefusesAServiceThatCannotTellADeletedAccount.
//
// [Deletions] is positional and required rather than an [Option], because there is no safe default:
// a nil would address everybody, which is the defect, and the alternative default addresses nobody.
// The panic is what makes a process that forgets it fail at wiring rather than at the first event.
func TestNewServiceRefusesAServiceThatCannotTellADeletedAccount(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Error("NewService accepted a nil Deletions, so a process could consume events " +
				"and notify accounts the platform has deleted")
		}
	}()
	NewService(&stubParties{}, nil, clock.NewFixed(testInstant), Senders{})
}
