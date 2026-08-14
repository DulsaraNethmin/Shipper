// SHIP-165: reading the immutable record.
//
// The other half of SHIP-150. `audit.read` has existed as a permission since SHIP-148, held by every
// role including the least privileged, and nothing served it — a trail written and never readable is
// a table, not a control. Docs/01 §4.6's last capability is "view an immutable history of important
// actions", and Docs/04 §9 requires that no ordinary administrator can delete from it.
//
// # This is a query, and there is deliberately no method on [Auditor]
//
// audit.go says so in as many words: "this is the write side and it has one verb". Keeping the
// reader out of that type is what makes the append-only invariant structural rather than a habit —
// a caller holding an [Auditor] can append and nothing else, and a caller holding an [AuditTrail]
// can read and nothing else. Neither can update or delete, because no such method is written and
// `000003`s triggers refuse both from any connection regardless.
//
// # The three search axes are the *Done when* and the fourth is `000003`s
//
// "Immutable history is searchable by actor, target, and date." Those three are below. **Action is a
// fourth**, and it is here because `000003` built the table around it: "a stable identifier rather
// than a sentence … free text would make the log unsearchable by action, which is how support will
// use it". A column created for a search, with no way to search it, would be the odd omission. It
// costs one `WHERE` clause and is validated against the closed catalogue, so a mistyped action is
// refused rather than answered with an empty page.
//
// # Why the filters are validated rather than passed through
//
// An unrecognised action, like SHIP-151's unrecognised standing, is **refused (422)** rather than
// ignored. An ignored filter answers with the whole trail, and "everything" and "the seventeen
// entries for this action" are not distinguishable to somebody who mistyped one — which for an
// audit search is the failure that matters, because the reader concludes an action never happened.
//
// # What a reader gets, and what nothing gives them
//
// Every column of `audit_log`, metadata included. There is no redaction and no field this endpoint
// withholds, which is a decision: the trail is what holds administrators to account (Docs/04 §9),
// and an audit viewer that shows a filtered version of the record is a second record. What keeps it
// safe is that **nothing commercial is ever written into it** — the metadata this service writes is
// the role granted, the standing set, the status a job moved to. A ticket tempted to put a budget in
// an audit entry has made the mistake one layer earlier than this file.
//
// The blank line below keeps this a file note rather than a second package comment.

package admin

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

// AuditQuery is one page of the trail.
//
// Cursor paged rather than offset paged, per Docs/10 §4.5. The trail grows while somebody is reading
// it — every request that reaches an administrative mutation appends to it — so an offset would show
// the same entry twice or skip one, and skipping one in this table is the failure it exists to make
// impossible.
type AuditQuery struct {
	// ActorID narrows to what one administrator did. Zero is every actor.
	//
	// The first question anybody asks of a trail, and the one Docs/04 §9's least-privilege
	// control is unauditable without: "what has this account been doing".
	ActorID uuid.UUID

	// TargetID narrows to what was done to one thing. Zero is every target.
	//
	// The second question, and it reads across kinds deliberately: `audit_log.target_id` has no
	// foreign key and holds an administrator, a user, a job or a note depending on the action, so
	// filtering by identifier alone answers "everything that ever happened to this" without the
	// caller having to know which kind it is. There is no target *type* filter, because an
	// identifier is already unique — a type filter could only ever narrow a result that has one
	// kind in it.
	TargetID uuid.UUID

	// Action narrows to one entry kind. Empty is every action.
	//
	// Validated against [AuditActions] before it gets here. See the file header.
	Action AuditAction

	// From and To bound the search by date, on the platform's clock.
	//
	// **From is inclusive and To is exclusive**, which is the only pair that makes consecutive
	// days tile without overlapping: a support engineer asking for "the 3rd" and then "the 4th"
	// must not see an entry twice, and an inclusive upper bound at midnight shows every entry
	// written in that instant on both days. Zero on either means unbounded on that side.
	From time.Time
	To   time.Time

	// Limit is how many entries to return. Bounded by internal/pagination before it gets here.
	Limit int

	// After is where the previous page stopped. The zero value is the first page.
	After AuditCursor
}

// AuditCursor is the position of the last entry a caller saw.
//
// Two fields, for the reason every other cursor in this package has two: `created_at` is not unique,
// and two entries written in one transaction share it to the microsecond by design — [Auditor.Record]
// gives every entry in a transaction the same injected instant. The identifier breaks the tie, and
// it is a v7, so entries written in one transaction sort in the order they were appended.
type AuditCursor struct {
	CreatedAt time.Time
	EntryID   uuid.UUID
}

// Zero reports whether this is the first page.
func (c AuditCursor) Zero() bool { return c.EntryID == uuid.Nil && c.CreatedAt.IsZero() }

// AuditRecord is one entry as the console reads it.
//
// Deliberately not [AuditEntry]. That type is what a *caller means* when appending — no identifier
// and no instant, both of which [Auditor.Record] supplies precisely so a caller cannot choose them.
// This one carries both, because a reader needs them. Two types for the two directions is the same
// separation [auditRow] makes for the same reason.
type AuditRecord struct {
	ID uuid.UUID

	ActorType AuditActorType

	// ActorID is the account that acted. Zero for [AuditActorSystem], which has none.
	ActorID uuid.UUID

	Action AuditAction

	TargetType string
	TargetID   uuid.UUID

	// Reason is why, where the action recorded one. Empty where it has none — creating an
	// administrator has no reason beyond the act, and restricting an account is refused without.
	Reason string

	// Metadata is the extra facts the action recorded, as already-encoded JSON.
	//
	// Kept as JSON rather than decoded into a map, so that what a reader sees is byte for byte
	// what was written. A round trip through `map[string]any` reorders keys and turns every
	// number into a float64, which in a table whose purpose is being trusted is a gratuitous
	// difference between the record and the report of it. Never empty: [marshalMetadata] writes
	// `{}` for nothing and the column is NOT NULL.
	Metadata string

	// CreatedAt is the platform's clock at the moment the action committed.
	CreatedAt time.Time
}

// AuditTrail serves the immutable record (SHIP-165).
//
// **Read only, and structurally so.** One method, no update, no delete, and no route that could
// reach one — CLAUDE.md's invariant is that audit entries are append-only and ordinary
// administrators cannot delete them, permissions.go records that no permission authorises it, and
// `000003`s triggers refuse it from any connection. This type is the fourth place that holds, and
// the only one a console can see.
type AuditTrail struct {
	pool  *pgxpool.Pool
	store postgresStore
}

// NewAuditTrail builds the reader.
//
// The pool may be nil, for the reason every constructor in this package accepts one: the process
// starts with an unreachable database on purpose, and this answers [ErrAdminUnavailable] for as long
// as it lasts.
func NewAuditTrail(pool *pgxpool.Pool) (*AuditTrail, error) {
	return &AuditTrail{pool: pool}, nil
}

// Search returns one page of the trail, newest first.
//
// **Newest first, unlike the moderation queue and like the two searches.** A queue is oldest-first
// because Docs/04 §8 sets acknowledgement targets and the oldest entry is closest to breaching one.
// A trail has no target, and somebody opening it is almost always asking what happened recently —
// "who changed this account's standing" far more often than "what happened in the first week".
//
// A read, opening no transaction: nothing here writes, and one statement is already consistent with
// itself.
func (a *AuditTrail) Search(ctx context.Context, q AuditQuery) ([]AuditRecord, error) {
	if a.pool == nil {
		return nil, ErrAdminUnavailable
	}

	records, err := a.store.searchAuditEntries(ctx, a.pool, q)
	if err != nil {
		return nil, fmt.Errorf("admin: reading the audit trail: %w", err)
	}
	return records, nil
}
