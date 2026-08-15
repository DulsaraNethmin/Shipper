// What this domain needs of the world outside it (SHIP-137).
//
// Docs/10 §2.3 and Docs/06 §4.1: the consumer declares the interface. internal/platform/email knows
// nothing about notifications, internal/platform/push does not exist yet, and internal/jobs is a
// domain this one may not import at all — so everything below speaks the standard library plus this
// package's own types, and the implementations meet it in cmd/notifier.
//
// # Why there is a Parties port and not a jobs import
//
// Recipient resolution needs to know who is on a job. Two thirds of the catalogue answers that from
// the event payload alone — job.expiry_warned carries customer_id, every bid event carries
// provider_id, bid.accepted carries both — and the remaining third does not: job.status_changed
// names only the actor, and delivery.proof_recorded names only the job.
//
// The fact is a join of `jobs` and `bids`, which are two other domains' tables. doc.go states the
// rule this domain is held to: it "never reaches back into jobs, bidding, or delivery to ask what
// happened". A join written in this package's postgres.go would read as though `jobs` were a table
// notifications owns.
//
// So it is a port, and the query is the composition root's. That is exactly the arrangement
// admin.JobParties and cmd/api's jobPartiesLookup already use (SHIP-113, SHIP-117), for the same
// reason and with the same wording: the composition root is where a dependency between domains is
// visible to somebody reading how the service is wired.
//
// # What is deliberately absent
//
// There is no port for persistence and no port for the users table. Docs/10 §2.2 refuses a
// repository interface; and `users` is in the shared migration block, which migrations/blocks.go
// describes as "tables every domain reads" — identity, jobs, fleet, delivery and admin all read it
// directly from their own postgres.go, and reading an address out of it is not a domain boundary.
//
// There is no EventSink. This domain consumes events and emits none: an event saying "a
// notification was sent" would be an event with no consumer, and Docs/10 §6.1 has every domain
// emitting only what something downstream needs to hear.
//
// The blank line below keeps this a file note rather than a second package comment.

package notifications

import (
	"context"

	"github.com/google/uuid"

	"github.com/DulsaraNethmin/Shipper/services/core/internal/db"
)

// Parties is who is on a job: the customer who owns it and the provider awarded it, if one has
// been.
//
// Takes a db.Runner so the lookup runs inside the consumer's transaction — Docs/10 §3.2's second
// rule, and the reason `bidding` and `jobs` can share one transaction without importing each other.
// It reads rather than writes, so it takes no lock: nothing here decides anything about the job.
//
// found is false when the job does not exist, which is a real answer rather than an error. A job
// pruned or pseudonymised (Docs/05 §3.1, SHIP-171) between the event being written and the
// notification being resolved is a case this must survive, and the consumer treats it as "nobody to
// tell" rather than as a failure that stops the topic.
//
// provider is uuid.Nil when no bid has been accepted. An awarded provider is the one whose bid
// carries status Accepted, which uq_bids_one_accepted_per_job makes at most one of.
type Parties interface {
	PartiesOn(ctx context.Context, r db.Runner, jobID uuid.UUID) (customer, provider uuid.UUID, found bool, err error)
}

// EmailSender delivers a transactional message.
//
// The same shape identity declares, deliberately: satisfied by *email.Console in development and
// *email.Provider elsewhere, and the two declarations are structurally identical so one adapter
// value satisfies both. Two interfaces rather than a shared one because a shared one would have to
// live somewhere both domains import, and the only such place is infrastructure — which would put
// a transport contract in a package that has nothing to do with either.
type EmailSender interface {
	Send(ctx context.Context, to, subject, body string) error
}

// SMSSender delivers a text message.
//
// Declared, wired and dispatched to, with no rule routing to it — see [ChannelSMS] for why that is
// a product decision this ticket did not take rather than an unfinished half.
type SMSSender interface {
	Send(ctx context.Context, to, body string) error
}

// Pusher delivers a push notification to one device.
//
// SHIP-137 declared this port with nothing behind it; **SHIP-139 filled it** with
// internal/platform/push, which has an FCM implementation and a no-op one, and SHIP-140 supplied
// the device tokens the first argument takes. [Rules] routes to [ChannelPush] accordingly.
//
// The signature is deliberately the shape a device token takes rather than a user id: Docs/01 §4.5
// requires a push to deep-link to the job it concerns and to be delivered per device, and a port
// that took a user would push the resolution of "which devices" behind an interface this domain
// declares for exactly the wrong reason.
//
// # rejected is a return value rather than a sentinel error, and that is the ticket's main decision
//
// internal/platform/push/doc.go argues that a rejected token is normal traffic: FCM rejects one
// whenever an app is uninstalled or its data is cleared, and treating that as a dispatch failure
// produces an alert that fires forever and is eventually ignored — including on the day it means
// something.
//
// A sentinel error would have expressed that and would not have enforced it. errors.Is is something
// a caller can forget, and the failure of forgetting is invisible: every rejection counted as a
// failure, and every dead handset keeping a notification row that retries forever. It could not
// even have been the adapter's sentinel — this package may not import an adapter — so it would have
// had to be declared here and returned by a package that does not know this one exists.
//
// A first return value is none of those things. It has a name at every call site, the compiler
// notices its absence, and a reader of any implementation can see that "the token is dead" and "the
// send failed" are two different answers.
//
// rejected true with a nil error means: the message was not delivered and never will be to this
// device. The device is deregistered (SHIP-140) and the notification is complete for that address.
type Pusher interface {
	Push(ctx context.Context, deviceToken, title, body string, jobID uuid.UUID) (rejected bool, err error)
}
