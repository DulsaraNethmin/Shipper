package notifications

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"

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
// # The instant is a parameter, and that is what stops twenty bad rows stopping the queue
//
// SHIP-138 added `next_attempt_at` (000703). Without it, twenty rows that will never succeed are
// claimed on every pass in perpetuity and nothing written after them is ever sent — the queue is
// stopped rather than slow, and every counter reports a healthy platform dispatching twenty
// messages a pass. See [BackoffFor].
//
// $2 is the injected clock rather than `now()`, for Docs/10 §6.3's reason: a test that has to wait
// out a real backoff is a test nobody runs. **NULL means nothing has deferred this row** — see
// 000703 on why the column is not `NOT NULL DEFAULT now()`, which is the shape that looks tidier
// and silently stops a fixed-clock service from dispatching anything at all.
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
	  AND (next_attempt_at IS NULL OR next_attempt_at <= $2)
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
	due, err := s.store.claimUndelivered(ctx, r, DispatchBatch, s.clock.Now().UTC())
	if err != nil {
		return 0, err
	}

	// SHIP-171b's second half. One lookup for the whole batch rather than one per row, for the
	// reason resolve() gives: this is a round trip inside a transaction holding a claim.
	deleted, err := s.deletedIn(ctx, r, due)
	if err != nil {
		return 0, err
	}

	for _, n := range due {
		if deleted[n.Recipient] {
			// Retired without being sent and without being attempted. See [Service.retire].
			if err := s.retire(ctx, r, n); err != nil {
				return 0, err
			}
			continue
		}

		sender, err := s.senderFor(n.Channel)
		if err != nil {
			return 0, fmt.Errorf("notifications: %s (%s): %w", n.ID, n.Channel, err)
		}

		rejected, sendErr := sender(ctx, r, n)
		switch {
		case sendErr != nil:
			retryAt := s.clock.Now().UTC().Add(BackoffFor(n.Attempts + 1))
			if err := s.store.markFailed(ctx, r, n.ID, sendErr.Error(), retryAt); err != nil {
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

// deletedIn is which of a claimed batch's recipients the platform has deleted.
//
// The identifiers are de-duplicated first: an event with two channels and two handsets is several
// rows for one person, and a claim of twenty rows can be a claim about three accounts.
func (s *Service) deletedIn(
	ctx context.Context, r db.Runner, due []Notification,
) (map[uuid.UUID]bool, error) {
	if len(due) == 0 {
		return nil, nil
	}

	seen := make(map[uuid.UUID]bool, len(due))
	ids := make([]uuid.UUID, 0, len(due))
	for _, n := range due {
		if seen[n.Recipient] {
			continue
		}
		seen[n.Recipient] = true
		ids = append(ids, n.Recipient)
	}

	deleted, err := s.deletions.DeletedAccounts(ctx, r, ids)
	if err != nil {
		return nil, fmt.Errorf("notifications: resolving which claimed recipients have been "+
			"deleted: %w", err)
	}
	return deleted, nil
}

// retire ends a notification addressed to an account the platform has deleted (SHIP-171b).
//
// # These rows exist, and nothing else was ever going to end them
//
// A row written before the deletion keeps the address it copied out of `users` when it was written
// — 000700 says so — and SHIP-171 then overwrote that copy with the pseudonym. Either way the row
// stays claimable: `failed` is deliberately not terminal, so the dispatcher takes it on every pass,
// the send does not land, and `attempts` climbs on somebody who no longer exists. Measured on this
// tree before the fix: one pass took such a row to `failed` with `attempts` at 1, and nothing in
// the platform would ever have stopped it.
//
// # `undeliverable` rather than a fifth status, and the argument is 000702's own
//
// That migration defines the status as "the address is gone, no retry can help, and nobody needs
// telling", which is true of this row more literally than of the rejected device token it was
// written for. It also says the status is "deliberately not reachable from email", and that
// paragraph is about **bounces**: a hard bounce today may be a full mailbox, the platform learns
// about it asynchronously through a webhook this MVP does not have, and it must not be treated as
// final. A deletion is not that. The platform destroyed the address itself, in a transaction, and
// knows it synchronously — so the ambiguity the paragraph protects against does not exist here.
//
// A fifth status would have needed a migration in the notifications block widening
// ck_notifications_status, to express a fact the fourth already expresses, and would have left every
// report that counts terminal rows needing to know about both.
//
// # attempts is not incremented, and that is the clause rather than a detail
//
// [postgresStore.markUndeliverable] increments it "because one was made". None is made here:
// nothing is handed to a sender, and the row is retired in front of the dispatch rather than after
// a failure. The ticket's own words are that "the attempts counter stops climbing on a person the
// platform has deleted", so a final increment would be the clause met in reduced form. A row
// retired this way carries whatever count it had already earned, which is the operational history
// somebody reading it wants.
//
// # Every channel, and the device token is left alone
//
// Push rows are retired too. SHIP-171 revokes every device session of a deleted account, so nothing
// resolves a new push address for one; a row already queued is a message aimed at the handset of
// somebody the platform has erased. The token is **not** deregistered here — [Service.reject] does
// that because FCM said the token was dead, which is a fact about the device. This is a fact about
// the account, `device_tokens.token` is named in SHIP-172's own *Done when*, and the row keeps its
// address so that what was aimed where stays answerable.
func (s *Service) retire(ctx context.Context, r db.Runner, n Notification) error {
	return s.store.markRecipientDeleted(ctx, r, n.ID)
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
// So the row becomes `undeliverable` (000702) — terminal and truthful — and the device is
// deregistered so that nothing addresses it again. Both happen in the dispatch pass's transaction,
// which means the two facts commit together: there is no window in which the platform has recorded
// that a token is dead and still holds it as live.
//
// A failure here **does** fail the pass, unlike a failed send. A send that bounces is one row's
// business; a database error while recording that a device is gone is not something the next
// nineteen rows will survive either.
func (s *Service) reject(ctx context.Context, r db.Runner, n Notification) error {
	if n.Channel == ChannelPush {
		if err := s.DeregisterToken(ctx, r, n.Address); err != nil {
			return err
		}
	}
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

// BackoffFor is how long a notification waits before its next attempt (SHIP-138).
//
// Exponential from a minute, flattening at an hour, and it **never gives up**: Docs/01 §4.5 says a
// notification failure must not lose the event, so a row that retired itself would be a message
// nobody receives and nobody is told about. What bounds the retries is a person reading `attempts`,
// which is 000700's own answer and the column SHIP-176 alerts on.
//
// No jitter. Jitter spreads a thundering herd across a shared far end, and there is no herd here:
// one dispatcher claims twenty rows at a time under SKIP LOCKED, and two instances already
// interleave rather than collide.
func BackoffFor(attempts int) time.Duration {
	const (
		base = time.Minute
		max  = time.Hour
	)

	if attempts < 1 {
		return base
	}
	wait := base
	for range attempts - 1 {
		wait *= 2
		if wait >= max {
			return max
		}
	}
	return wait
}
