package notifications

import (
	"context"
	"fmt"

	"github.com/DulsaraNethmin/Shipper/services/core/internal/db"
)

// The dispatch half of SHIP-137: take the rows the consumer wrote and send them, one channel at a
// time.
//
// # Why this is a second phase rather than a send inside Consume
//
// Because Docs/01 §4.5 says a notification failure must not lose the event, and a send that happens
// before anything is committed loses it twice over: the transaction rolls back on a failed send, so
// the row that recorded who had to be told disappears with it, and if the message *did* go out
// before the rollback the redelivery sends it again. Writing first and sending afterwards leaves
// exactly one bad outcome — a message sent whose row was not marked, which is a duplicate — and
// duplicates are the failure this whole domain is built to tolerate.
//
// It is also what makes a retry free. A channel that was down for a minute leaves rows at `failed`,
// and `failed` is not terminal (000700): the next pass claims them again. Nothing schedules that;
// the claim's own predicate is `status <> 'sent'`.
//
// # The send happens inside the pass's transaction, which is the same trade the outbox publisher makes
//
// cmd/worker/outbox.go publishes to Kafka inside its transaction and marks the rows afterwards, on
// the reasoning that a row must never be marked before the far end has acknowledged it. This is the
// same shape with a slower far end. The bound is [DispatchBatch] — a batch small enough that the
// transaction holding it is measured in the number of messages a provider will accept in a few
// seconds, not in the size of the backlog.

// DispatchBatch is how many notifications one pass claims.
//
// Smaller than the sweeps in cmd/worker, and for a reason they do not have: every row in this batch
// is an outbound HTTP request to somebody else's service, inside a transaction. Twenty is enough to
// amortise the round trip to the database and small enough that one slow provider holds a
// transaction for seconds rather than minutes.
const DispatchBatch = 20

// DispatchClaim takes the undelivered notifications, oldest first, and locks them.
//
// $1 is the batch size.
//
// # The two clauses, and why they are checked by a test in this package
//
// cmd/worker.ClaimIDs refuses a claim query that does not say FOR UPDATE SKIP LOCKED, and the
// argument in claim.go is exactly right: a claim missing SKIP LOCKED turns two workers into a
// queue, one missing FOR UPDATE has both of them send the same message, and neither failure
// produces an error. That helper is in cmd/worker and cmd/notifier cannot import it — an entrypoint
// may not import another entrypoint.
//
// Rather than copy the helper, the clauses are held here by TestTheDispatchClaimLocksWhatItTakes
// and the *outcome* is held by TestTwoDispatchersSendEachNotificationOnce. The second is the one
// that matters: a text check reads the string, and a text check is satisfied by the words appearing
// in a comment. Docs/11 §9 records that internal/bidding's equivalent lock has neither.
//
// idx_notifications_undelivered (000702, replacing 000700's) is partial on exactly this predicate,
// so a platform that has sent a million notifications reads the handful it has not.
//
// **Two terminal statuses rather than one, since SHIP-139.** `undeliverable` is what a rejected
// device token leaves behind (000702): the address is gone, no retry can help, and a row that
// stayed claimable would be retried against a handset that no longer exists on every pass forever
// — internal/platform/push/doc.go's "alert that fires forever and is eventually ignored".
const DispatchClaim = `
	SELECT id, event_id, event_type, job_id, recipient_id, channel, category, essential,
	       address, subject, body, status, attempts
	FROM notifications
	WHERE status NOT IN ('sent', 'undeliverable')
	ORDER BY created_at, id
	FOR UPDATE SKIP LOCKED
	LIMIT $1`

// Dispatch is one pass: claim what is undelivered and send each of it.
//
// It returns how many rows it claimed, which is what the caller logs. A row that could not be sent
// is counted as claimed — it was worked on — and is marked `failed` with the reason, so the count
// answers "how much did this pass do" rather than "how much of it worked". The log line that
// matters for the second question is the one written per failure.
//
// # A failure does not fail the pass
//
// This is the one place this domain departs from the sweeps in cmd/worker, and deliberately. There,
// a job that cannot be expired means the rule is not being applied and the right answer is to roll
// the whole pass back. Here, one address that bounces must not roll back nineteen messages that
// went out — the transaction rolling back would unmark them, and the next pass would send them all
// again. So the failure is recorded on its own row and the pass continues.
//
// What does fail the pass is a database error, or a channel with no sender at all ([ErrNoSender]):
// the first is not something a later row will survive either, and the second is a wiring fault that
// should be fixed and restarted rather than written into every row as a failure.
func (s *Service) Dispatch(ctx context.Context, r db.Runner) (int, error) {
	due, err := s.store.claimUndelivered(ctx, r, DispatchBatch)
	if err != nil {
		return 0, err
	}

	for _, n := range due {
		sender, err := s.senderFor(n.Channel)
		if err != nil {
			return 0, fmt.Errorf("notifications: %s (%s): %w", n.ID, n.Channel, err)
		}

		rejected, sendErr := sender(ctx, r, n)
		switch {
		case sendErr != nil:
			if err := s.store.markFailed(ctx, r, n.ID, sendErr.Error()); err != nil {
				return 0, err
			}

		case rejected:
			// The far end says this address no longer exists. See [Service.reject].
			if err := s.reject(ctx, r, n); err != nil {
				return 0, err
			}

		default:
			if err := s.store.markSent(ctx, r, n.ID, s.clock.Now().UTC()); err != nil {
				return 0, err
			}
		}
	}
	return len(due), nil
}

// reject records that an address is gone, and stops addressing it (SHIP-139, SHIP-140).
//
// # This is the branch internal/platform/push/doc.go is written around
//
// FCM rejects a device token whenever an app is uninstalled or its data is cleared, which happens
// across a real install base every hour of every day. It is **normal traffic**, and the two obvious
// things to do with it are both wrong:
//
//   - marking the row `failed` retries a handset that no longer exists on every pass forever, and
//     counts itself into whatever SHIP-176 alerts on — the alert that fires constantly and is
//     eventually ignored, including on the day it means something;
//   - marking it `sent` is a lie a support query cannot see through.
//
// So the row becomes `undeliverable` (000702) — terminal and truthful.
//
// **Half of the response is missing until SHIP-140**, and it is named here rather than left to be
// noticed: the platform should also stop addressing the handset, and it cannot, because there is no
// device token registry to deregister anything from. Until then a dead token keeps being resolved
// as an address and every event writes one more row that this branch immediately retires. That is
// bounded and visible — one terminal row per event rather than an unbounded retry — which is why it
// is worth landing the adapter ahead of the registry rather than after it.
//
// A failure here **does** fail the pass, unlike a failed send. A send that bounces is one row's
// business; a database error while recording that an address is gone is not something the next
// nineteen rows will survive either.
func (s *Service) reject(ctx context.Context, r db.Runner, n Notification) error {
	return s.store.markUndeliverable(ctx, r, n.ID)
}

// send is one channel's way of delivering one notification.
//
// It takes the runner because a rejection is recorded in the pass's transaction, and it returns
// `rejected` for the reason [Pusher] does: a caller cannot forget a value the compiler makes it
// name, and it could forget an errors.Is. Only push can reject today — see [Service.reject] on why
// an email bounce is deliberately not the same fact.
type send func(ctx context.Context, r db.Runner, n Notification) (rejected bool, err error)

// senderFor picks the implementation by the row's channel.
//
// This is the whole of "dispatches per channel": the dispatcher has no list of event types, no
// opinion about categories, and no way to tell an award from an expiry warning. It reads a column
// and calls the sender that column names. Adding push (SHIP-139) adds a case here and a line in
// [Rules], and touches nothing else.
func (s *Service) senderFor(c Channel) (send, error) {
	switch {
	case c == ChannelEmail && s.senders.Email != nil:
		return func(ctx context.Context, _ db.Runner, n Notification) (bool, error) {
			// Never rejects. An email that hard-bounces is learned about
			// asynchronously through a webhook this MVP does not have (Docs/01 §8),
			// so the synchronous answer here is accepted or not — see 000702.
			return false, s.senders.Email.Send(ctx, n.Address, n.Subject, n.Body)
		}, nil

	case c == ChannelSMS && s.senders.SMS != nil:
		return func(ctx context.Context, _ db.Runner, n Notification) (bool, error) {
			return false, s.senders.SMS.Send(ctx, n.Address, n.Body)
		}, nil

	case c == ChannelPush && s.senders.Push != nil:
		// SHIP-139 and SHIP-140 filled this in. The comment that used to be here said the
		// case existed so the ticket supplying both would delete a comment rather than
		// invent a shape, and that is what happened — except for the first return value,
		// which is the one thing the shape did not anticipate. [Pusher] says why.
		return func(ctx context.Context, _ db.Runner, n Notification) (bool, error) {
			return s.senders.Push.Push(ctx, n.Address, n.Subject, n.Body, n.JobID)
		}, nil

	default:
		return nil, fmt.Errorf("%w: %s", ErrNoSender, c)
	}
}
