// SHIP-152: an administrator finding a job, and opening it with everything that happened to it.
//
// Docs/01 §4.6's first capability again — "search users, jobs, bids, and disputes" — of which
// SHIP-151 served the first term. The *Done when* is "search and open any job with its full bid and
// status history", which is two endpoints rather than one: a collection somebody scans, and a
// detail view that carries the two histories.
//
// # Why this reaches other domains through a port when the account search did not
//
// SHIP-151 selects from `users` directly, and postgres_users.go argues why: `users` is in the shared
// migration block because most of the service reads it, and the statement spans **one** table. This
// one spans `jobs`, `bids` and `job_status_history` — three tables belonging to two domains this
// package may not import — so it takes the arrangement [JobParties] and [ExceptionQueue] take. The
// statements live in cmd/api, which is the only place `admin`, `jobs` and `bidding` meet, and this
// domain names what it needs rather than what those domains have.
//
// # A customer's budget is not on any shape below, and that is a decision rather than an oversight
//
// Docs/01 §4.3's invariant is about **providers** — "the customer's maximum budget is private;
// providers never see it" — and an administrator is not a provider, so nothing in the documents
// forbids showing one here. It is left out anyway, for two reasons.
//
// The first is that nothing asks for it. The *Done when* is the bid history and the status history,
// and a support engineer triaging a job is reading what was offered and what happened, not what the
// customer was willing to pay. The second is the argument [ExceptionEntry] and [UserRecord] both
// record: a shape that never carried a budget cannot leak one, and the ways an administrative shape
// escapes to the wrong audience — a field copied into a client response, a route re-declared under
// a different auth class — are all ways a *present* field travels.
//
// **The revisit trigger is named rather than left to judgement: SHIP-164**, where an administrator
// resolving a dispute about price may genuinely need the number the customer set. At that point it
// is a field on that ticket's own shape, decided by that ticket, and not something this one left
// lying about.
//
// # Bid amounts *are* here, and Docs/02 §4 is why
//
// "Bid history remains visible to the customer, bidding provider, and administrators." The third
// audience is this endpoint — SHIP-96 enumerated the three and could not serve the administrator
// because there was no administrator to serve. There is now.
//
// # No addresses, no contact details, no goods dimensions
//
// Docs/01 §5.1 asks the platform to minimise exposure of phone numbers, addresses and delivery
// details, and a console screen is exposure like any other. What a job's identity needs in order to
// be recognised is its description, its customer, its status and its dates; the rest is a
// second ticket's decision if a support conversation ever turns out to need it.
//
// The blank line below keeps this a file note rather than a second package comment.

package admin

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/DulsaraNethmin/Shipper/services/core/internal/db"
)

// maxJobTermLength bounds what may be submitted as a job search term.
//
// 200, which is shorter than [maxSearchTermLength] and deliberately: that one is the length of the
// longest email address `000002`s column will hold, and this one bounds a substring match against
// `goods_description`. A term longer than a sentence is a client sending something else, and a
// leading-wildcard LIKE costs a scan whose price should not be a function of what somebody pasted.
const maxJobTermLength = 200

// JobQuery is one page of an administrator's job search.
//
// Cursor paged rather than offset paged, per Docs/10 §4.5 and for the reason [UserQuery] gives:
// jobs are published while somebody is paging through them.
type JobQuery struct {
	// Term matches the goods description, anywhere within it.
	//
	// **The description rather than an identifier**, because a support engineer holding a job id
	// opens the job rather than searching for it — that is what the detail endpoint is. What
	// they hold when they are searching is a sentence off a ticket: "the piano", "two pallets of
	// tiles". Empty matches every job, which with the two filters below is how "every open job
	// for this customer" is expressed.
	Term string

	// Status narrows to one job status, in Docs/02 §1's stored form. Empty is every status.
	//
	// **A plain string, and not a closed type in this package.** `jobs.Status` is generated from
	// `contracts/statuses.yaml` (SHIP-56a) and `admin` may not import the package it is
	// generated into, so a copy here would be a hand-written list shadowing a generated one —
	// which is the trade [ExceptionEntry.JobStatus] refused for the same reason. What makes the
	// filter safe without a copy is that the closed list is *supplied* to [NewJobConsole] by
	// cmd/api, where `jobs.Statuses` is in scope. See [JobConsole.Statuses].
	Status string

	// CustomerID narrows to one customer's jobs. Zero is every customer.
	CustomerID uuid.UUID

	// Limit is how many to return. Bounded by internal/pagination before it gets here.
	Limit int

	// After is where the previous page stopped. The zero value is the first page.
	After JobCursor
}

// JobCursor is the position of the last job a caller saw.
//
// Two fields for the reason [UserCursor] records: `created_at` is not unique, and a single-column
// cursor over a non-unique key either skips a row or repeats one.
type JobCursor struct {
	CreatedAt time.Time
	JobID     uuid.UUID
}

// Zero reports whether this is the first page.
func (c JobCursor) Zero() bool { return c.JobID == uuid.Nil && c.CreatedAt.IsZero() }

// JobRecord is one job as an administrator sees it in a list.
//
// **No budget** — see the file header. **No addresses and no contact details**, per Docs/01 §5.1.
type JobRecord struct {
	ID         uuid.UUID
	CustomerID uuid.UUID

	// Status is Docs/02 §1's stored form, reported rather than translated. The same position
	// [ExceptionEntry.JobStatus] takes and for the same reason: the vocabulary is `jobs`', the
	// wire mapping is Docs/10 §4.7's, and neither is this domain's to restate.
	Status string

	// GoodsDescription is what the customer said they are sending. Empty on a draft that has not
	// reached the details step — `000404` makes every draft column nullable.
	GoodsDescription string

	// BidCount is how many bids the job carries, in every status.
	//
	// A count rather than the bids themselves, because a list of jobs each carrying its offers
	// would be one query per row on the screen. Every status rather than the live ones: what a
	// scan of this list answers is "did anybody engage with this job", and a job whose three
	// offers were all withdrawn is a different thing from one nobody looked at.
	BidCount int

	// ExpiresAt is when an unclaimed job lapses (SHIP-68). Zero where none is set.
	ExpiresAt time.Time

	CreatedAt time.Time
	UpdatedAt time.Time
}

// BidRecord is one offer as an administrator sees it.
//
// Docs/02 §4: "bid history remains visible to the customer, bidding provider, and administrators".
// This is the third reader, and it sees every offer on the job rather than one provider's chain —
// which is the whole difference between this and the two views SHIP-96 already serves.
type BidRecord struct {
	ID         uuid.UUID
	ProviderID uuid.UUID

	// Status is Docs/02 §4's stored form, reported rather than translated.
	Status string

	// OfferedBy is which side made this offer — `provider` or `customer` (`000502`). A
	// counter-offer from the customer is a bid row like any other, and a reader who could not
	// tell the two apart would read a negotiation as one party bidding against themselves.
	OfferedBy string

	// AmountCents is the offer, in cents. Zero where the row carries none, which `000500` allows
	// on a draft bid and `ck_bids_amount` makes unambiguous by refusing a non-positive amount.
	//
	// Cents rather than a decimal, matching `bidding`s wire shape: a field called `amount`
	// holding 45000 is the ambiguity the unit in the name exists to remove.
	AmountCents int64

	// PickupAt and DeliverBy are the timing offered. Zero where the row carries none.
	PickupAt  time.Time
	DeliverBy time.Time

	// Message is the provider's note to the customer, where they left one.
	Message string

	// SupersededBy names the offer that displaced this one (`000502`), so a reader can follow a
	// negotiation in the order it happened rather than by comparing timestamps. Zero on an offer
	// nothing displaced.
	SupersededBy uuid.UUID

	CreatedAt time.Time
	UpdatedAt time.Time
}

// StatusEvent is one recorded transition, as `job_status_history` holds it.
//
// **Both clocks, never one.** Docs/02 §3.1 keeps the actor's claim and the platform's acceptance
// apart because a driver records a milestone out of signal and the device syncs later; a support
// timeline that showed only one of them would be unable to answer the question it exists for, which
// is why an update arrived when it did.
type StatusEvent struct {
	ID uuid.UUID

	From string
	To   string

	// ActorType is `customer`, `provider`, `driver`, `admin` or `system` (`000401`). Finer than
	// audit_log's three kinds, and reported as stored for the same reason the statuses are.
	ActorType string

	// ActorID is the account that acted, or the driver assignment for a driver. Zero for the
	// platform, which has no account.
	ActorID uuid.UUID

	// Reason is why, where one was given. Required of an administrator by
	// `ck_job_status_history_admin_reason`, optional for everybody else.
	Reason string

	ActorRecordedAt  time.Time
	ServerRecordedAt time.Time
}

// JobDetail is one job opened, with everything recorded against it.
//
// One shape rather than three endpoints. The *Done when* is "open any job **with** its full bid and
// status history", and a console that had to make three calls to render one screen would be three
// chances for a page to show a job beside somebody else's bids.
type JobDetail struct {
	Job     JobRecord
	Bids    []BidRecord
	History []StatusEvent
}

// JobConsole is the administrator's view of the marketplace (SHIP-152).
//
// A service of its own rather than a method on [Users] or [Moderation], matching the shape this
// package already uses: [Moderation] is the queues, [Users] is accounts, and this is jobs and their
// offers. Folding them together would widen one constructor per screen.
type JobConsole struct {
	directory JobDirectory

	// statuses is Docs/02 §1's closed list, supplied by cmd/api.
	//
	// **Supplied rather than declared here, and that is the whole trick.** A copy in this package
	// would be a hand-written twelve-value list shadowing one generated from
	// `contracts/statuses.yaml`, which users.go's [UserStanding] note explains is a trade worth
	// making for a *hand-written* CHECK constraint and not for a generated vocabulary. cmd/api
	// has `jobs.Statuses` in scope and passes it, which is exactly what the composition root is
	// for — the same place `actorFor` maps one domain's actor kinds onto another's.
	statuses []string

	pool *pgxpool.Pool
}

// NewJobConsole builds the job and bid search.
//
// The pool may be nil, which every constructor in this package accepts: the process starts with an
// unreachable database on purpose so a failover does not take the fleet down, and the endpoints
// answer [ErrAdminUnavailable] for as long as it lasts.
//
// The directory may not be nil and the status list may not be empty. A nil directory is a search
// that answers "no such jobs" to everything, which is the failure [NewModeration] refuses for the
// same reason — indistinguishable from a quiet marketplace. An empty status list would make every
// status filter a 422 naming no alternatives, which reads as the endpoint being broken rather than
// as the caller having mistyped something.
func NewJobConsole(directory JobDirectory, statuses []string, pool *pgxpool.Pool) (*JobConsole, error) {
	if directory == nil {
		return nil, errors.New("admin: the job console needs a directory; without one it would " +
			"report that no job exists, which reads exactly like a quiet marketplace")
	}
	if len(statuses) == 0 {
		return nil, errors.New("admin: the job console needs Docs/02 §1's status list; without " +
			"it every status filter is refused and no alternative can be named")
	}

	held := make([]string, len(statuses))
	copy(held, statuses)
	return &JobConsole{directory: directory, statuses: held, pool: pool}, nil
}

// Statuses is the closed list a status filter is validated against, as a fresh slice.
//
// A copy rather than the stored slice, for the reason [Role.Permissions] returns one: the stored
// slice is the service's, and a caller that appended to what it was handed would widen the filter
// for the whole process.
func (c *JobConsole) Statuses() []string {
	out := make([]string, len(c.statuses))
	copy(out, c.statuses)
	return out
}

// KnowsStatus reports whether s is one of Docs/02 §1's statuses.
func (c *JobConsole) KnowsStatus(s string) bool {
	for _, known := range c.statuses {
		if known == s {
			return true
		}
	}
	return false
}

// Search returns one page of jobs, newest first.
//
// Newest first for the reason [Users.Search] gives: a search is not a queue and has no
// acknowledgement target, and the jobs somebody is looking for are overwhelmingly recent.
//
// A read, opening no transaction. Docs/10 §3.2 puts a transaction with whoever owns an invariant,
// and a search owns none.
func (c *JobConsole) Search(ctx context.Context, q JobQuery) ([]JobRecord, error) {
	if c.pool == nil {
		return nil, ErrAdminUnavailable
	}
	q.Term = strings.TrimSpace(q.Term)

	records, err := c.directory.SearchJobs(ctx, c.pool, q)
	if err != nil {
		return nil, fmt.Errorf("admin: searching jobs: %w", err)
	}
	return records, nil
}

// Open is one job with its full bid and status history.
//
// # It runs in a transaction, and a read does not usually need one
//
// Three statements — the job, its bids, its history — and a job moves while they run. Without a
// transaction the console can render a job showing `Awarded` beside a bid list in which nothing is
// accepted, or a status history whose last row is a transition the job header does not reflect. A
// support screen that contradicts itself is worse than a stale one, because somebody acts on it.
//
// Read-only and it takes no locks: the three statements see one snapshot, and a job that moved
// during them is simply the next page load.
//
// Answers [ErrJobNotFound] for a job that does not exist. **Unlike the dispute intake endpoint,
// there is no disclosure decision hiding behind that**: the caller is an authenticated
// administrator holding `jobs.read`, and every job is theirs to open. The refusal on intake is a
// customer being told nothing about somebody else's job, which is a different question with a
// different asker.
func (c *JobConsole) Open(ctx context.Context, jobID uuid.UUID) (JobDetail, error) {
	if c.pool == nil {
		return JobDetail{}, ErrAdminUnavailable
	}

	var detail JobDetail
	err := db.InTx(ctx, c.pool, func(ctx context.Context, tx db.Runner) error {
		var found bool
		var err error
		if detail, found, err = c.directory.OpenJob(ctx, tx, jobID); err != nil {
			return err
		}
		if !found {
			return fmt.Errorf("admin: %s: %w", jobID, ErrJobNotFound)
		}
		return nil
	})
	if err != nil {
		return JobDetail{}, err
	}
	return detail, nil
}

// LikePattern turns a search term into a pattern matching it anywhere, with the wildcards escaped.
//
// Exported because the statement behind [JobDirectory] lives in cmd/api — it spans two other
// domains' tables and may not live here — and the rule about **what a search term means** is this
// domain's rather than the composition root's. A second copy of the escaping in that package would
// be the shape this function exists to prevent: two search endpoints in one console, one of which
// treats `%` as a character and the other as a wildcard.
//
// The escaping is the whole function and leaving it out is not a subtle bug. `%` is LIKE's
// "anything", so an unescaped term of `%` returns every job in the marketplace to the
// least-privileged role from one character in a search box; `_` is quieter and worse, matching more
// than was asked for while looking as though it worked. The backslash is escaped first so that
// escaping does not undo itself.
//
// See [likeContains], which this delegates to — the account search reached the same rule first and
// there is one implementation of it.
func LikePattern(term string) string { return likeContains(term) }
