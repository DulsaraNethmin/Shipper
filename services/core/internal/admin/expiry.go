// SHIP-159: the expiry queue — Docs/04 §5's seventh and last.
//
// §5 asks administrators for a queue of "expiring or expired provider verification records". The
// other six are built: verification submissions (SHIP-153), reported jobs and messages (SHIP-156),
// flagged jobs, delivery exceptions (SHIP-117, SHIP-157), post-award cancellations (SHIP-158) and
// open disputes (SHIP-164). This is the one whose subject is a *document* rather than a job, an
// account or a report.
//
// # What this service does not decide, and it is most of the interesting part
//
// **When a document lapses is the document's own fact**, stated at submission and held in
// `provider_verification_documents.expires_at`. **How far ahead of that a document is worth chasing
// is a policy**, it belongs to Docs/04 §3's unanswered X-4, and it lives in `internal/config` with no
// default. Neither number is in this package, and `internal/profiles` — which owns the table — is
// where both are applied. What lives here is the console's half: a permission, a page, and a shape.
//
// The consequence a reader should carry: **with no lead time configured, this queue reports lapsed
// documents and nothing else.** That is the honest floor rather than a gap. `profiles.Expiry` argues
// it where the constraint lands.
//
// # Why a service of its own rather than a method on [Moderation]
//
// [Moderation] is Docs/04 §5's *delivery exception* queue and takes the port that unions three
// grounds across `internal/delivery`'s and `internal/jobs`' tables. This takes a different port over
// a third domain's table and answers a different question. Folding it in would widen
// [NewModeration] with a collaborator none of its methods use — which is [Verifications]' own
// argument, one queue over.
//
// # It writes nothing, and that is deliberate rather than incidental
//
// SHIP-155's viewer records who was shown a provider's images, because a signed URL to somebody's
// driver licence cannot be revoked. **This queue is the console's own working surface** — dates and
// account contact details, no image and no credential — so it is under the rule
// [AuditActionNoteAdded] states: an entry per queue load would bury the actions in the reads. The
// two live one route apart and the line between them is what the response contains.
//
// The blank line below keeps this a file note rather than a second package comment.

package admin

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

// ExpiryQueue serves Docs/04 §5's seventh queue (SHIP-159).
type ExpiryQueue struct {
	documents ExpiringDocuments

	pool *pgxpool.Pool
}

// NewExpiryQueue builds the service.
//
// The pool may be nil, which every constructor in this package accepts: the process starts with an
// unreachable database on purpose, and the endpoints answer [ErrAdminUnavailable] until it returns.
//
// The port may not, on [NewModeration]'s and [NewVerifications]' shared argument: a nil would make
// the queue answer "nothing is expiring" to every request, which is the worst available failure for
// a queue of things running out — indistinguishable from a well-maintained supply side, and silent
// for exactly as long as nobody checks.
func NewExpiryQueue(documents ExpiringDocuments, pool *pgxpool.Pool) (*ExpiryQueue, error) {
	if documents == nil {
		return nil, errors.New("admin: the expiry queue needs a source of verification " +
			"documents; without one it reports nothing expiring, which reads exactly like a " +
			"supply side whose paperwork is all current")
	}
	return &ExpiryQueue{documents: documents, pool: pool}, nil
}

// Due is one page of documents that have lapsed or are about to, soonest first (SHIP-159).
//
// It is a read and it opens no transaction: nothing here writes and a single statement is already
// consistent with itself. Docs/10 §3.2 puts a transaction with whoever owns an invariant, and a
// queue owns none.
func (q *ExpiryQueue) Due(ctx context.Context, query DocumentExpiryQuery) ([]ExpiringDocumentEntry, error) {
	if q.pool == nil {
		return nil, ErrAdminUnavailable
	}

	entries, err := q.documents.DocumentsNearingExpiry(ctx, q.pool, query)
	if err != nil {
		return nil, fmt.Errorf("admin: reading the document expiry queue: %w", err)
	}
	return entries, nil
}

// LeadTimes is the configured horizon per document kind, as cmd/api supplied it.
//
// Exported so the response can carry it, which is not decoration: **a reviewer looking at a queue
// with no "expiring" entries needs to be able to tell "nothing is due" from "no lead time has been
// configured for that kind yet".** Those are the same empty page and different facts, and only one
// of them is a reason to go and ask legal for X-4's answer.
//
// A fresh map rather than the stored one, for [Role.Permissions]' reason: a caller that wrote into
// what it was handed would change what the console reports for the whole process.
func (q *ExpiryQueue) LeadTimes() map[string]time.Duration {
	return q.documents.ExpiryLeadTimes()
}

// ExpiringDocumentEntry is one current document on the queue.
//
// **No budget, no job and no bid**, which is structural in the way [VerificationEntry] is: there is
// nowhere on this struct to put one, so no change to the response mapping can acquire one.
//
// **No object key and no download URL.** This is a list of dates, not of images — a reviewer who
// wants to look opens SHIP-155's viewer, which writes an access entry. A credential here would make
// every queue load an unlogged read of everybody's identity documents at once.
type ExpiringDocumentEntry struct {
	DocumentID uuid.UUID
	ProviderID uuid.UUID

	// Name is what the account holder is called (SHIP-30a). Empty for an account created before
	// `000006`.
	Name  string
	Email string
	Phone string

	// VerificationState is where the provider stands, as a plain string.
	//
	// Not a closed type here, on [VerificationEntry.State]'s reasoning: `profiles.States` is that
	// domain's list, held to `ck_provider_verifications_state` by a test in both directions, and
	// a second copy in this package would shadow a checked one.
	VerificationState string

	// Kind is which of Docs/04 §3's four documents this is, likewise a plain string.
	Kind string

	// ExpiresAt is the date the document itself states. Never zero — a document that states none
	// is not on this queue.
	ExpiresAt time.Time

	// Expired is whether that date has passed, decided by the domain against one instant for the
	// whole page rather than by a console against its own clock.
	Expired bool

	SubmittedAt time.Time
}

// DocumentExpiryQuery is one page of the expiry queue.
//
// Cursor paged per Docs/10 §4.5. **No filter**, unlike [VerificationQuery]'s required `state`: this
// queue is already narrow — it holds only current documents that state an expiry and have reached
// their horizon — so there is no unfiltered answer that reads as "the whole supply side wearing a
// queue's name".
type DocumentExpiryQuery struct {
	// Limit is how many entries to return. Bounded by internal/pagination before it gets here.
	Limit int

	// After is where the previous page stopped. The zero value is the first page.
	After DocumentExpiryCursor
}

// DocumentExpiryCursor is the position of the last entry a caller saw.
//
// Two fields, because an expiry date is *printed on a document* rather than generated — two lapsing
// on the same day is the ordinary case rather than a coincidence, and a single-column cursor would
// skip a row or repeat one.
type DocumentExpiryCursor struct {
	ExpiresAt  time.Time
	DocumentID uuid.UUID
}

// Zero reports whether this is the first page.
func (c DocumentExpiryCursor) Zero() bool {
	return c.DocumentID == uuid.Nil && c.ExpiresAt.IsZero()
}
