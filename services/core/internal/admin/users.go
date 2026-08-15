// SHIP-151: an administrator finding an account.
//
// Docs/01 §4.6's first capability — "search users, jobs, bids, and disputes" — and the first
// administrative *read* over another domain's rows. Docs/09 suggests pulling this forward as a
// debugging tool, and that is what it is: the screen support opens before anything else.
//
// # One clause of the *Done when* has nothing to search
//
// "Search users by email, phone, name, and status." Three of those four are columns of `users`.
// **There is no name.** `000002_users` has id, email, phone, password_hash, role, status, the two
// verification timestamps and the two bookkeeping ones, and nothing anywhere else in the schema
// holds a person's name against an account — `admin_users.name` is an administrator's and
// `driver_assignments.driver_name` is a driver's, captured at assignment and belonging to a job.
// Registration never asks for one (`identity.User` has no such field), so the platform does not know
// it.
//
// **This ticket does not invent one.** `users` is created in the shared migration block (1–99) and
// Docs/11 §6 strikes SHIP-169 for exactly that reason; a column added from this branch would be a
// shared-surface edit taken unilaterally, and the field belongs to registration rather than to
// search. So the gap is recorded in Docs/11 §4 as SHIP-118's and SHIP-77's is — the same shape,
// which §4 already names — and whoever adds a name to registration serves it here in one line: a
// third OR in [postgresStore.searchUsers].
//
// # Why this domain reads `users` directly
//
// postgres.go's header says this store queries neither `jobs`, `bids` nor `users`, and notes that
// `users` is "shared and readable, but this domain has no reason to". This ticket is the reason, and
// the header has been amended rather than worked around. `users` is in the shared block because most
// of the service reads it — `migrations/blocks.go` says so — and `internal/jobs`, `internal/fleet`
// and `internal/delivery` all select from it in their own stores today. A port would put the
// statement in cmd/api, which is where a query spanning **two domains' tables** belongs (see
// [JobParties] and the exception queue); this one spans none.
//
// # A customer's budget is not reachable from here, by construction
//
// Docs/01 §4.3 is absolute — a customer's maximum is never exposed to a provider in any form — and an
// administrative search that joined jobs would be a new place for it to escape. There is no join.
// [UserRecord] carries account facts and nothing commercial, and
// TestTheUserSearchResponseCarriesNothingCommercial holds the serialised shape to a closed set of
// keys rather than searching it for the word "budget", which is the axis SHIP-83 found a
// spelling-based check lacks.
//
// The blank line below keeps this a file note rather than a second package comment.

package admin

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

// UserStanding is whether an account may be used, and it is `ck_users_status`s three values.
//
// # Why this is not called Status, and why it is a second copy of a list
//
// [Status] in this package is an *administrator's*, which has two values and a different meaning.
// One name for two closed lists is how a `switch` ends up comparing an administrator against a
// customer's vocabulary, so the two are named apart.
//
// It is a copy of `identity.Status` and cannot be anything else: `admin` may not import `identity`
// and the boundary lint refuses it. What makes a copy safe is Docs/10 §3.4's pairing, and
// TestUserStandingConstraintMatchesTheGoConstants in `migrations` is it — read against
// `ck_users_status`, in both directions, so a standing either package gains and the other does not
// fails a test rather than becoming a filter that silently matches nothing.
//
// The alternative was to take the filter as a plain string, the way [ExceptionEntry.JobStatus]
// reports one. It is right there and wrong here: that field is *reported*, and this one is an
// *input*. A string filter would answer an empty page to a typo, and an empty page is the same
// answer as "no such accounts" — so a support engineer searching for `suspeneded` would conclude
// there are none.
type UserStanding string

const (
	// StandingActive is an ordinary account.
	StandingActive UserStanding = "active"

	// StandingRestricted narrows what an account may do without ending its access (SHIP-161).
	StandingRestricted UserStanding = "restricted"

	// StandingSuspended is an account that may no longer sign in.
	StandingSuspended UserStanding = "suspended"
)

// UserStandings is every standing, in the order `ck_users_status` lists them.
var UserStandings = []UserStanding{StandingActive, StandingRestricted, StandingSuspended}

// Valid reports whether s is one of [UserStandings].
func (s UserStanding) Valid() bool { return slices.Contains(UserStandings, s) }

// String is the stored form, which is also the wire form.
func (s UserStanding) String() string { return string(s) }

// standingNames is [UserStandings] as strings, for a validation message.
func standingNames() []string {
	out := make([]string, 0, len(UserStandings))
	for _, s := range UserStandings {
		out = append(out, s.String())
	}
	return out
}

// maxSearchTermLength bounds what may be submitted.
//
// 320 is the longest address `000002`s column will hold, and a term longer than the longest thing it
// could match is a client sending something else — a pasted document, or somebody probing. Bounded
// here rather than left to the statement because a leading-wildcard LIKE costs a scan, and the cost
// should not be a function of what a caller pasted.
const maxSearchTermLength = 320

// UserQuery is one page of an administrator's user search.
//
// Cursor paged rather than offset paged, per Docs/10 §4.5. Accounts are created while somebody is
// paging through them, and an offset would show the same account twice or skip one — which matters
// more here than on most feeds, because the reason to page through users at all is usually that
// somebody is looking for one in particular.
type UserQuery struct {
	// Term matches an email address or a phone number, anywhere within either.
	//
	// **One field for both rather than two**, because a support engineer has a string off a
	// ticket and does not always know which it is — and a console that made them choose would be
	// asking a question the platform can answer by trying both. Empty matches every account,
	// which with [UserQuery.Standing] is how "list the suspended accounts" is expressed.
	//
	// Substring rather than prefix. "Every address at this company" and "the number ending 345"
	// are the two searches support actually runs, and neither is a prefix.
	Term string

	// Standing narrows to one account standing. Empty is every standing.
	Standing UserStanding

	// Limit is how many to return. Bounded by internal/pagination before it gets here.
	Limit int

	// After is where the previous page stopped. The zero value is the first page.
	After UserCursor
}

// UserCursor is the position of the last account a caller saw.
//
// Two fields, because `created_at` is not unique — two accounts registered in the same millisecond
// would make a single-column cursor either skip one or repeat it. The identifier breaks the tie and
// is what makes the ordering total.
type UserCursor struct {
	CreatedAt time.Time
	UserID    uuid.UUID
}

// Zero reports whether this is the first page.
func (c UserCursor) Zero() bool { return c.UserID == uuid.Nil && c.CreatedAt.IsZero() }

// UserRecord is one account as an administrator sees it.
//
// # What is deliberately not here
//
// **No password hash**, which is structural rather than careful: [postgresStore.searchUsers] does
// not select the column and there is nowhere on this struct to put it. `identity.User` takes the
// same position for the same reason.
//
// **No jobs, no bids, and nothing commercial.** A customer's budget is never exposed to a provider
// in any form (Docs/01 §4.3), and the surest way to keep an administrative shape clear of one is for
// it never to have carried anything from that side of the platform. Opening a user's jobs is
// SHIP-152, which is a different endpoint with its own disclosure decisions to take.
//
// **No verification state for a provider.** Account standing and provider verification are different
// facts owned by different domains — `000002`s own comment says so — and this is the first. The
// verification queue is SHIP-153.
type UserRecord struct {
	ID uuid.UUID

	Email string
	Phone string

	// Role is `customer` or `provider`, fixed at registration and immutable (SHIP-45).
	//
	// Reported as the stored string rather than translated. It is `identity`s closed list and
	// this domain has no opinion about it — the same position [ExceptionEntry.JobStatus] takes,
	// and it holds here because the role is reported and never filtered on.
	Role string

	// Standing is whether the account may be used.
	Standing UserStanding

	// EmailVerifiedAt and PhoneVerifiedAt record *when*, not *whether*, which is `000002`s
	// decision and the one a support conversation actually needs. Zero means not yet.
	EmailVerifiedAt time.Time
	PhoneVerifiedAt time.Time

	CreatedAt time.Time
}

// Users is the account search (SHIP-151).
//
// A small service rather than the handler calling the store, matching [Moderation]: it is where the
// pool being nil becomes [ErrAdminUnavailable] rather than a nil-pointer dereference, and where a
// second reader — SHIP-152 opening one account — will hang its method.
type Users struct {
	pool  *pgxpool.Pool
	store postgresStore
}

// NewUsers builds the search service.
//
// The pool may be nil, which is the same condition every other constructor in this package accepts:
// the process starts with an unreachable database on purpose, so a failover does not take the fleet
// down, and this answers [ErrAdminUnavailable] for as long as it lasts.
func NewUsers(pool *pgxpool.Pool) (*Users, error) {
	return &Users{pool: pool}, nil
}

// Search returns one page of accounts, newest first.
//
// **Newest first, unlike the moderation queue.** That queue is oldest-first because Docs/04 §8 sets
// acknowledgement targets and the oldest entry is closest to breaching one. A search is not a queue
// and has no target: the accounts somebody is looking for are overwhelmingly recent ones, and the
// tail of a search is where a support engineer stops reading rather than where the work is.
func (u *Users) Search(ctx context.Context, q UserQuery) ([]UserRecord, error) {
	if u.pool == nil {
		return nil, ErrAdminUnavailable
	}
	q.Term = strings.TrimSpace(q.Term)

	// A read, and it opens no transaction. Nothing here writes and a single statement is already
	// consistent with itself; Docs/10 §3.2 puts a transaction with whoever owns an invariant, and
	// a search owns none.
	records, err := u.store.searchUsers(ctx, u.pool, q)
	if err != nil {
		return nil, fmt.Errorf("admin: searching accounts: %w", err)
	}
	return records, nil
}
