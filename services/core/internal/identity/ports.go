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
// Satisfied by *email.Console, *email.Provider or *email.SMTP (SHIP-32, SHIP-187a, SHIP-187b).
// Which one exists is decided in cmd/api from EMAIL_TRANSPORT; nothing here knows or asks. The
// console implementation logs the body in full, which is how a developer completed verification
// before the development stack had a mailbox — SHIP-200 gives it one, and the code is then read
// from a browser rather than out of the API log.
type EmailSender interface {
	Send(ctx context.Context, to, subject, body string) error
}

// SMSSender delivers a text message, which in the MVP means a phone verification code.
//
// Satisfied by *sms.Console or *sms.Provider, chosen in cmd/api from SMS_TRANSPORT (SHIP-35,
// SHIP-187a). The adapter does not know it is carrying a one-time code and must not: generating
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

// PersonalDetails is every store outside internal/identity that keeps a copy of the account
// holder's own name, address or number (SHIP-171).
//
// # Why the copies have to be reached at all
//
// "Irreversibly replaced" is a claim about the whole database rather than about `users`. A row
// elsewhere holding the original address beside a foreign key to the account is a reverse mapping
// whatever the account row now says — `select n.address from notifications n where n.recipient_id
// = $1` recovers the person from a table nobody was thinking about. So the sweep replaces the
// copies in the same transaction as the original, or it has not replaced anything.
//
// # Why it is one port and not one per table
//
// The two implementations that exist today live in `provider_profiles` and `notifications`, which
// belong to two other domains. Two ports would be two interfaces describing one act — "replace this
// person's details wherever you keep them" — and would make the third copy somebody finds later a
// third interface rather than a line in one adapter. cmd/worker supplies it, which is where a
// dependency between domains is allowed to be visible; the measured precedents are
// `cmd/api/routes_identity.go`'s activeJobLookup and `cmd/api/routes_bidding.go`'s offerorDirectory.
//
// # What it deliberately does not cover
//
// Not `internal/identity`'s own tables. `email_verification_tokens.email` and `phone_otps.phone` are
// verbatim copies of the two contact channels and are replaced by [postgresStore] directly, because
// this package owns them and a port over its own schema would be the repository interface Docs/06
// §4.1 refuses.
//
// Not artefacts. Message bodies, device tokens, verification documents and attributable images are
// SHIP-172's, and Docs/05 §3.1 puts them in the same column as the contact details for a reason —
// but *removing* an artefact and *replacing* an identifier are different acts with different
// failure modes, and the ticket boundary is where Docs/09 put it.
type PersonalDetails interface {
	// Replace overwrites every identifying value it holds for userID and reports how many rows
	// it changed.
	//
	// The count is for the log and for a test, not for a decision: zero is an ordinary answer,
	// because a customer has no provider profile and an account that has never been notified
	// has no notifications. An implementation that cannot reach one of its stores returns an
	// error rather than a partial count — the caller is inside the transaction that also moves
	// the request to [DeletionCompleted], so a failure here means neither happens.
	Replace(ctx context.Context, r db.Runner, userID uuid.UUID, with Pseudonym) (int, error)
}
