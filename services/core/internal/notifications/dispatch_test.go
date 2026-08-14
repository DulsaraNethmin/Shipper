package notifications

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/DulsaraNethmin/Shipper/services/core/internal/clock"
	"github.com/DulsaraNethmin/Shipper/services/core/internal/db"
	"github.com/DulsaraNethmin/Shipper/services/core/internal/testsupport/pgtest"
)

// SHIP-137's dispatch half: "dispatches per channel".

// pending writes a notification row directly, so a dispatch test does not depend on the consumer
// having resolved anybody.
func pending(t *testing.T, pool *pgxpool.Pool, recipient uuid.UUID, channel Channel, address string) uuid.UUID {
	t.Helper()

	id := uuid.Must(uuid.NewV7())
	if _, err := pool.Exec(t.Context(), `
		INSERT INTO notifications
		    (id, event_id, event_type, job_id, recipient_id, channel, category, essential,
		     address, subject, body)
		VALUES ($1, $2, 'bid.placed', $3, $4, $5, 'bidding', true, $6, 'You have a new offer.', 'Job body')`,
		id, uuid.Must(uuid.NewV7()), uuid.Must(uuid.NewV7()), recipient, string(channel), address,
	); err != nil {
		t.Fatalf("writing a pending notification: %v", err)
	}
	return id
}

// dispatchOnce runs one pass in a transaction.
func dispatchOnce(t *testing.T, pool *pgxpool.Pool, s *Service) (int, error) {
	t.Helper()

	var claimed int
	err := db.InTx(t.Context(), pool, func(ctx context.Context, r db.Runner) error {
		var err error
		claimed, err = s.Dispatch(ctx, r)
		return err
	})
	return claimed, err
}

// stateOf reads a notification's status, attempts and error back out of the table.
func stateOf(t *testing.T, pool *pgxpool.Pool, id uuid.UUID) (status string, attempts int, lastError string) {
	t.Helper()

	var reason *string
	if err := pool.QueryRow(t.Context(),
		`SELECT status, attempts, last_error FROM notifications WHERE id = $1`, id,
	).Scan(&status, &attempts, &reason); err != nil {
		t.Fatalf("reading the state of %s: %v", id, err)
	}
	if reason != nil {
		lastError = *reason
	}
	return status, attempts, lastError
}

// TestDispatchSendsOnTheChannelTheRowNames is the *Done when* clause, and the reason both channels
// are exercised rather than just the one any rule routes to.
//
// The dispatcher has no list of event types and no opinion about categories: it reads a column and
// calls the sender that column names. SMS has a working adapter and no routing rule (see
// [ChannelSMS]), so this is what keeps the second channel honest until a ticket decides otherwise.
func TestDispatchSendsOnTheChannelTheRowNames(t *testing.T) {
	pool := pgtest.DB(t)

	recipient := newUser(t, pool, "dispatch@example.com", "+61400007201", "customer")
	mail := &recordingSender{}
	text := &smsSender{}
	service := NewService(&stubParties{}, clock.NewFixed(testInstant),
		Senders{Email: mail, SMS: text})

	byEmail := pending(t, pool, recipient, ChannelEmail, "dispatch@example.com")
	bySMS := pending(t, pool, recipient, ChannelSMS, "+61400007201")

	claimed, err := dispatchOnce(t, pool, service)
	if err != nil {
		t.Fatalf("the dispatch pass failed: %v", err)
	}
	if claimed != 2 {
		t.Fatalf("the pass claimed %d notifications, want 2", claimed)
	}

	if len(mail.sent) != 1 || !strings.HasPrefix(mail.sent[0], "dispatch@example.com|") {
		t.Errorf("the email sender received %v", mail.sent)
	}
	if len(text.sent) != 1 || !strings.HasPrefix(text.sent[0], "+61400007201|") {
		t.Errorf("the SMS sender received %v", text.sent)
	}

	for _, id := range []uuid.UUID{byEmail, bySMS} {
		status, attempts, _ := stateOf(t, pool, id)
		if status != "sent" || attempts != 1 {
			t.Errorf("%s is %s after %d attempt(s), want sent after 1", id, status, attempts)
		}
	}
}

// TestASentNotificationIsNotSentAgain is the claim's own idempotence, by state rather than by a
// key: a sent row does not match `status <> 'sent'`, so nothing remembers anything.
func TestASentNotificationIsNotSentAgain(t *testing.T) {
	pool := pgtest.DB(t)

	recipient := newUser(t, pool, "once@example.com", "+61400007202", "customer")
	mail := &recordingSender{}
	service := NewService(&stubParties{}, clock.NewFixed(testInstant), Senders{Email: mail})

	pending(t, pool, recipient, ChannelEmail, "once@example.com")

	if _, err := dispatchOnce(t, pool, service); err != nil {
		t.Fatalf("the first pass: %v", err)
	}
	claimed, err := dispatchOnce(t, pool, service)
	if err != nil {
		t.Fatalf("the second pass: %v", err)
	}
	if claimed != 0 {
		t.Errorf("the second pass claimed %d notifications", claimed)
	}
	if len(mail.sent) != 1 {
		t.Errorf("the sender was called %d times for one notification", len(mail.sent))
	}
}

// TestAFailedSendLeavesTheRowClaimable is Docs/01 §4.5 — "a notification failure must not lose the
// event" — as a property of the table rather than a promise.
//
// The row is recorded failed with its reason and is claimed again on the next pass, because
// `failed` is not terminal (000700). What bounds the retries is somebody reading `attempts`, which
// is why that column is not a boolean.
func TestAFailedSendLeavesTheRowClaimable(t *testing.T) {
	pool := pgtest.DB(t)

	recipient := newUser(t, pool, "bounces@example.com", "+61400007203", "customer")
	mail := &recordingSender{fail: errors.New("the provider returned 503")}
	service := NewService(&stubParties{}, clock.NewFixed(testInstant), Senders{Email: mail})

	id := pending(t, pool, recipient, ChannelEmail, "bounces@example.com")

	if _, err := dispatchOnce(t, pool, service); err != nil {
		t.Fatalf("a failing channel failed the whole pass: %v", err)
	}

	status, attempts, reason := stateOf(t, pool, id)
	if status != "failed" || attempts != 1 {
		t.Errorf("the row is %s after %d attempt(s), want failed after 1", status, attempts)
	}
	if !strings.Contains(reason, "503") {
		t.Errorf("the recorded reason is %q and does not say what went wrong", reason)
	}

	// The channel comes back and the message goes out, with no intervention and nothing
	// scheduled: the claim's predicate is what retries it.
	mail.fail = nil
	claimed, err := dispatchOnce(t, pool, service)
	if err != nil {
		t.Fatalf("the retry pass: %v", err)
	}
	if claimed != 1 {
		t.Fatalf("the retry claimed %d notifications, want the failed one", claimed)
	}
	if status, attempts, _ := stateOf(t, pool, id); status != "sent" || attempts != 2 {
		t.Errorf("after the retry the row is %s on attempt %d, want sent on 2", status, attempts)
	}
}

// TestOneFailureDoesNotRollBackTheRestOfTheBatch is why Dispatch records a failure rather than
// returning it.
//
// A transaction that rolled back over one bad address would unmark every message that did go out,
// and the next pass would send all of them again. That is the trade this domain takes in the other
// direction from cmd/worker's sweeps, and it is stated in dispatch.go.
func TestOneFailureDoesNotRollBackTheRestOfTheBatch(t *testing.T) {
	pool := pgtest.DB(t)

	good := newUser(t, pool, "good@example.com", "+61400007204", "customer")
	bad := newUser(t, pool, "bad@example.com", "+61400007205", "customer")

	mail := &selectiveSender{failFor: "bad@example.com"}
	service := NewService(&stubParties{}, clock.NewFixed(testInstant), Senders{Email: mail})

	goodID := pending(t, pool, good, ChannelEmail, "good@example.com")
	badID := pending(t, pool, bad, ChannelEmail, "bad@example.com")

	if _, err := dispatchOnce(t, pool, service); err != nil {
		t.Fatalf("the pass failed: %v", err)
	}

	if status, _, _ := stateOf(t, pool, goodID); status != "sent" {
		t.Errorf("the good address is %s; one bad address rolled back a message that went out", status)
	}
	if status, _, _ := stateOf(t, pool, badID); status != "failed" {
		t.Errorf("the bad address is %s, want failed", status)
	}
}

// TestAChannelWithNoSenderStopsThePassAndKeepsTheRow is the wiring fault, kept apart from the
// delivery fault.
//
// A process started without an email sender should be fixed and restarted; writing "no sender" into
// every row as a failure would quietly retire messages nobody has received.
func TestAChannelWithNoSenderStopsThePassAndKeepsTheRow(t *testing.T) {
	pool := pgtest.DB(t)

	recipient := newUser(t, pool, "unwired@example.com", "+61400007206", "customer")
	service := NewService(&stubParties{}, clock.NewFixed(testInstant), Senders{})

	id := pending(t, pool, recipient, ChannelEmail, "unwired@example.com")

	if _, err := dispatchOnce(t, pool, service); !errors.Is(err, ErrNoSender) {
		t.Fatalf("a pass with no email sender returned %v, want ErrNoSender", err)
	}
	if status, attempts, _ := stateOf(t, pool, id); status != "pending" || attempts != 0 {
		t.Errorf("the row is %s after %d attempt(s); a wiring fault must not be recorded "+
			"against the message", status, attempts)
	}
}

// TestTwoDispatchersSendEachNotificationOnce is what FOR UPDATE SKIP LOCKED buys, run rather than
// asserted.
//
// This is the test dispatch.go's comment promises. cmd/worker.ClaimIDs cannot be reached from
// another binary, so the clauses are held by a text check below — and a text check is satisfied by
// the words appearing in a comment, which is why the outcome is held here too. Docs/11 §9 records
// that internal/bidding's equivalent lock has neither.
func TestTwoDispatchersSendEachNotificationOnce(t *testing.T) {
	pool := pgtest.DB(t)

	recipient := newUser(t, pool, "race@example.com", "+61400007207", "customer")

	const due = 8
	for range due {
		pending(t, pool, recipient, ChannelEmail, "race@example.com")
	}

	senders := []*recordingSender{{}, {}}
	var wg sync.WaitGroup
	claimed := make([]int, len(senders))
	for i := range senders {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			service := NewService(&stubParties{}, clock.NewFixed(testInstant),
				Senders{Email: senders[n]})

			// Twice each, so that between them they drain a backlog larger than one
			// batch would be if DispatchBatch were smaller than `due`.
			for range 2 {
				got, err := dispatchOnce(t, pool, service)
				if err != nil {
					t.Errorf("dispatcher %d: %v", n, err)
					return
				}
				claimed[n] += got
			}
		}(i)
	}
	wg.Wait()

	if total := claimed[0] + claimed[1]; total != due {
		t.Errorf("two dispatchers claimed %d notifications between them (%v), want %d",
			total, claimed, due)
	}
	if sent := len(senders[0].sent) + len(senders[1].sent); sent != due {
		t.Errorf("%d messages went out for %d notifications; each extra one is a duplicate "+
			"in somebody's inbox", sent, due)
	}

	var unsent int
	if err := pool.QueryRow(t.Context(),
		`SELECT count(*) FROM notifications WHERE status <> 'sent'`).Scan(&unsent); err != nil {
		t.Fatalf("counting what is left: %v", err)
	}
	if unsent != 0 {
		t.Errorf("%d notifications are still undelivered after both dispatchers finished", unsent)
	}
}

// TestTheDispatchClaimLocksWhatItTakes is the text half, and it is here because cmd/notifier cannot
// import cmd/worker.ClaimIDs.
//
// It is the weaker of the two checks by construction — the words could appear in a comment and it
// would pass — which is exactly why TestTwoDispatchersSendEachNotificationOnce exists above it.
func TestTheDispatchClaimLocksWhatItTakes(t *testing.T) {
	normalised := strings.Join(strings.Fields(strings.ToLower(DispatchClaim)), " ")

	for _, clause := range []string{"for update", "skip locked"} {
		if !strings.Contains(normalised, clause) {
			t.Errorf("DispatchClaim does not say %q; without it two dispatchers either "+
				"queue behind each other or send the same message twice, and neither "+
				"shows up as an error (Docs/10 §6.2)", clause)
		}
	}
}

// selectiveSender fails for one address and succeeds for the rest.
type selectiveSender struct {
	failFor string
	sent    []string
}

func (s *selectiveSender) Send(_ context.Context, to, _, _ string) error {
	if to == s.failFor {
		return errors.New("mailbox does not exist")
	}
	s.sent = append(s.sent, to)
	return nil
}
