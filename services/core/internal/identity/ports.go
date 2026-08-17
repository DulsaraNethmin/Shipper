// What this domain needs of the world outside it.
//
// Docs/10 §2.3 and Docs/06 §4.1: **the consumer declares the interface.** internal/platform/email
// knows nothing about identity, and identity imports nothing from it — the two are written
// independently, satisfied structurally, and meet in cmd/api. The import lint fails the build in
// both directions, so this is enforced rather than remembered.
//
// # Why every signature here is standard-library types
//
// A message struct declared in the adapter would have to be named in the signature below, and
// naming it means importing the adapter — which is exactly the edge the lint refuses. So the
// whole contract is context, strings and error. The adapters' own documentation makes the same
// point from the other side.
//
// Wave 1 surfaced the cost of this rule in geocoding, where it produced a five-value return and
// a comma-ok instead of a sentinel error. Docs/11 §9 records the open question — whether a
// neutral infrastructure package should hold shared value types — to be decided at SHIP-60.
// Nothing here is wide enough for it to matter yet.
//
// # What is deliberately absent
//
// There is no port for persistence. Docs/10 §2.2 is explicit: postgres.go is concrete and
// unexported, because PostgreSQL is not abstracted in this service and a repository interface
// would hide the constraints that make the rules true.
//
// # SHIP-170 adds the first port over another domain rather than over an adapter
//
// [EmailSender] and [SMSSender] are both adapter ports — a transport this domain needs and does
// not own. [ActiveJobs] is the first thing this domain needs of another *domain*, and it follows
// admin.JobParties: `internal/identity` may not import `internal/jobs` or `internal/bidding`, the
// boundary lint refuses both, and cmd/api holds the statement because it spans two other domains'
// tables. Nothing about the rule changes; what changes is that a signature below now names
// db.Runner and uuid.UUID, because a port that has to participate in the caller's transaction
// takes a Runner (Docs/10 §3.2).
//
// There is no EventSink either, and that is a scope decision rather than an oversight.
// Docs/10 §6.1 has every domain writing events through one inside the transaction that changes
// state, but no ticket in SHIP-30…SHIP-36 specifies an event, and inventing `user.registered`
// here would fix a name that SHIP-136 and SHIP-137 have to honour. It arrives with the ticket
// that has a consumer for it.
//
// The blank line below keeps this a file note rather than a second package comment.

package identity

import (
	"context"

	"github.com/google/uuid"

	"github.com/DulsaraNethmin/Shipper/services/core/internal/db"
)

// EmailSender delivers a transactional message.
//
// Satisfied by *email.Console in development and *email.Provider in staging and production
// (SHIP-32). Which one exists is decided in cmd/api from SHIPPER_ENV; nothing here knows or asks
// — and the console implementation logging the body in full is the whole reason a developer can
// complete verification without a mailbox.
type EmailSender interface {
	Send(ctx context.Context, to, subject, body string) error
}

// SMSSender delivers a text message, which in the MVP means a phone verification code.
//
// Satisfied by *sms.Console in development and *sms.Provider in staging and production
// (SHIP-35). The adapter does not know it is carrying a one-time code and must not: generating
// the code, deciding its lifetime, rate-limiting it and comparing it on the way back are all
// domain rules, and a transport that knew about them would be a domain rule living in the wrong
// package.
type SMSSender interface {
	Send(ctx context.Context, to, body string) error
}

// ActiveJobs is whether this account is in the middle of a delivery (SHIP-170).
//
// # One method returning a yes or a no, and nothing else
//
// Docs/05 §3.1 asks one question of the lifecycle — "is erasing this person right now going to
// strand somebody?" — and that is a boolean. A port returning the job, or its status, or a list of
// them, would be handing this domain facts it has no business holding and no way to render: a
// customer's job carries a budget (Docs/01 §4.3), and a shape that never carried one cannot leak
// one. admin.JobParties states the rule this follows — "a port is what a domain needs rather than
// what the other domain has".
//
// # It takes a Runner because the answer and the write are one transaction
//
// Docs/10 §3.2: a port that must participate in the caller's transaction takes a Runner.
// [Service.RequestDeletion] reads this and then records or moves the request under the row lock,
// and the two have to be one statement of what was true — a deferral decided outside the
// transaction is a deferral decided against a job that may already have closed.
//
// # What it deliberately does not promise
//
// **Not a lock, and not a guarantee that lasts.** A job can be awarded a millisecond after this
// answers false, and nothing here prevents it. That is why the state is re-evaluated on every call
// rather than recorded once and trusted: the deletion request holds no job identifier, and the
// port is asked again each time the request is touched. SHIP-171 asks again before it executes,
// which is the check that actually protects the counterparty.
//
// # Which accounts count as "carrying" a delivery
//
// **Both sides of it.** Docs/05 §3.1 says "erasing a *party* mid-delivery would strand the
// counterparty" — the customer whose goods are moving and the provider whose bid was accepted are
// each the other's counterparty, and the deletion endpoint is RequireUser with no role predicate,
// so both reach it. The implementation in cmd/api is where that is expressed, because it spans
// `jobs` and `bids`.
type ActiveJobs interface {
	// HasActiveJob reports whether userID is party to any job between Awarded and Delivered
	// inclusive — Docs/02 §1's six committed statuses.
	//
	// A non-nil error is a failure of the mechanism. There is no "no such account" answer: an
	// account that owns nothing and has won nothing is not carrying a delivery, which is false
	// rather than an absence.
	HasActiveJob(ctx context.Context, r db.Runner, userID uuid.UUID) (bool, error)
}
