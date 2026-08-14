package notifications

import "errors"

// The conditions this domain distinguishes.
//
// Short, because a consumer has fewer ways to fail than an endpoint: there is no caller to give a
// 4xx to, and Docs/10 §4.4's error registry is for HTTP. What matters here is which failures must
// stop a pass — leaving the message unacknowledged and the rows unwritten — and which are the
// ordinary shape of a topic carrying more than one consumer cares about.

var (
	// ErrNotInTransaction is returned when Consume is handed a pool rather than a transaction.
	//
	// Checked before anything is written, for the reason jobs.Transition checks: the whole
	// point is that every row for one event commits together, so that a duplicate delivery
	// meets a complete set of rows rather than half of one.
	ErrNotInTransaction = errors.New("notifications: a consumer pass must run in a transaction")

	// ErrNoRule is returned for an event type the routing table does not know.
	//
	// A distinguishable error rather than a silent zero, so the caller can decide. cmd/notifier
	// logs it and skips the message, because parking a Kafka partition forever is the wrong
	// answer on a broker that is shared across every worktree and legitimately carries messages
	// from a build with an event this one does not have.
	//
	// The guard is a deployment earlier, where it can name the line:
	// cmd/api/events_notifications_test.go fails the build if the catalogue registers an event
	// this table has no rule for. That is the same division internal/events/catalogue.go argues
	// for the dead-letter question — everything permanently unacceptable is refused where the
	// row is written, and what reaches a consumer is only the transient class.
	ErrNoRule = errors.New("notifications: no routing rule for this event type")

	// ErrNoSender is returned when a row names a channel this process cannot send on.
	//
	// The row stays claimable rather than being marked failed, because the fault is in the
	// wiring rather than in the message: a process started without an email sender should be
	// fixed and restarted, and the notification should then go out. Marking it failed would
	// quietly retire a message nobody has received.
	ErrNoSender = errors.New("notifications: no sender is configured for this channel")
)
