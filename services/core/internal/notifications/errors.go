package notifications

import (
	"errors"

	"github.com/DulsaraNethmin/Shipper/services/core/internal/httpx"
)

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

	// ErrRedacted is returned when rendered copy breaks SHIP-141's rules.
	//
	// Docs/01 §4.4: no address, no goods description and no full customer name in a
	// notification. redaction.go carries the four rules and what each is for.
	//
	// It stops the message rather than scrubbing it, and the two are not close. A scrub would
	// send something — a sentence with a hole in it, to somebody who cannot tell what was
	// removed — and would leave the copy that produced it in rules.go for the next event to
	// use. Refusing makes the defect visible in one place and sends nothing meanwhile.
	//
	// Reaching this at runtime should be impossible: every input to [Render] is a compile-time
	// literal, and TestNoRuleCanRenderCopyThatBreaksTheRedactionRules holds the whole routing
	// table on every channel. It is an error rather than a panic because [Consume] runs inside
	// a transaction holding a Kafka partition's progress — a panic there stops a consumer where
	// an error stops one message.
	ErrRedacted = errors.New("notifications: this copy cannot be sent to a handset")

	// ErrNoSender is returned when a row names a channel this process cannot send on.
	//
	// The row stays claimable rather than being marked failed, because the fault is in the
	// wiring rather than in the message: a process started without an email sender should be
	// fixed and restarted, and the notification should then go out. Marking it failed would
	// quietly retire a message nobody has received.
	ErrNoSender = errors.New("notifications: no sender is configured for this channel")
)

// The device registry's conditions (SHIP-140), and the codes its two endpoints answer with.
//
// Both endpoints belong to the caller's own device session, so there is no "somebody else's token"
// case and no 404 that has to be indistinguishable from a 403 — which is why this list is two
// sentinels and one code rather than the six a resource with owners would need.
var (
	// ErrUnknownPlatform is returned for a platform ck_device_tokens_platform would refuse.
	//
	// Checked in Go as well as in the database because the client is told which field was wrong,
	// and a constraint violation arriving at the handler is a 500 with a constraint name in it.
	ErrUnknownPlatform = errors.New("notifications: that is not a platform this app runs on")

	// ErrUnknownCategory is returned for a category ck_notifications_category does not know.
	//
	// Checked in Go as well as in the database for the reason [ErrUnknownPlatform] is: the
	// client is told which field was wrong, and a constraint violation arriving at the handler
	// is a 500 with a constraint name in it.
	ErrUnknownCategory = errors.New("notifications: that is not a notification category")

	// ErrEssentialCategory is returned when somebody tries to mute a category Docs/01 §4.5
	// calls essential (SHIP-142).
	//
	// **This is the courtesy and not the control.** ck_notification_preferences_category admits
	// only the mutable categories, so the row cannot exist however it is written; this exists so
	// that a client gets a field error naming `muted` rather than a 500 carrying a constraint
	// name. 000704 carries the argument for having both.
	ErrEssentialCategory = errors.New("notifications: this category cannot be muted")

	// ErrNoDeviceToken is returned when a registration names no token.
	//
	// Distinct from the adapter's error of the same name and deliberately not shared: this
	// package may not import internal/platform/push, and the two mean the same thing at two
	// different points on the path.
	ErrNoDeviceToken = errors.New("notifications: a device registration needs a token")
)

// CodeNoDeviceSession is returned when a caller registers a device token on a token that names no
// device session.
//
// It cannot happen with a token this platform issues — SHIP-38 puts a session on every one — so it
// is a 401 rather than a 422: the credential is not one this endpoint can act on, and the client's
// correct response is to sign in again rather than to fix a field.
//
// A distinct code because the alternative is a bare 401, which the Flutter client's interceptor
// answers by refreshing and replaying — a loop, against a session that will still not be there.
var CodeNoDeviceSession = httpx.RegisterCode("notifications_no_device_session",
	"That credential does not name a device session, so there is nothing to register a push "+
		"token against. Sign in again.")

// CodeCategoryEssential is the field error for muting a category Docs/01 §4.5 calls essential
// (SHIP-142).
//
// A code of its own rather than the generic "not one of the available options", because the two
// mean different things to a client and one of them is actionable. An unknown category is a bug in
// the app; an essential one is a switch the person should not have been offered, and the client's
// correct response is to redraw the screen from `essential` in the response it already has.
//
// The message says *cannot* rather than *may not*: this is not a permission somebody could be
// granted. ck_notification_preferences_category refuses the row.
var CodeCategoryEssential = httpx.RegisterCode("notifications_category_essential",
	"This kind of notification cannot be switched off. Docs/01 §4.5 lists the events every "+
		"account is told about.")
