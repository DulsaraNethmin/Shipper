// SHIP-159: which documents are about to lapse — Docs/04 §5's seventh moderation queue.
//
// §5 asks for a queue of "expiring or expired provider verification records", and §1 requires the
// verification record hold "every review, evidence item, decision, and **expiry date**". `000202`
// added the fourth of those; this is the read over it.
//
// # The platform must not enforce a renewal cadence, and this file is where that constraint lands
//
// Docs/04 §3's open question is unanswered and is not engineering's to settle: *"which documents are
// legally required rather than merely prudent, and **how often each must be renewed**. Owner: legal
// and insurance advisers… This determines expiry tracking (§5) and retention obligations."* It is
// Track-X row X-4. §3 permits building before it is answered — "needed before pilot users are
// invited, not before build begins" — so the binding constraint is not "wait" but **"do not enforce
// a number nobody decided"**.
//
// Two things follow, and they are different in kind:
//
//   - **An expiry is a fact about a document and comes from the document.** It is stated at
//     submission, held in `expires_at`, and a document with none never appears here. The platform
//     does not compute one from `submitted_at` plus anything.
//   - **A *lead time* — how far ahead of its expiry a document is worth chasing — is a policy, and
//     it lives in configuration with no default.** [NewExpiry] takes one duration per [Kind] and
//     accepts an empty map. A kind with no configured lead time has a horizon of *now*, so its
//     documents surface when they have actually lapsed and not a day before. That is the honest
//     floor: the queue reports what the document itself says, and nothing this platform invented.
//
// **The consequence worth stating plainly: with no configuration, "expiring" is empty and "expired"
// is not.** Both halves of Docs/04 §5's queue exist in the code; only one of them can be populated
// until somebody answers X-4, and that is the correct behaviour rather than a gap.
//
// # Only the current document of a kind is on the queue
//
// `000201` is append-only and the newest row of a kind is the current document. A licence that was
// re-photographed with a later expiry has a superseded row behind it whose date has passed, and
// putting that row on a queue would ask an administrator to chase a renewal that has already
// happened — every time, for ever, because nothing can delete it. `DISTINCT ON (provider_id, kind)`
// is what makes the queue about the provider's present standing rather than about their history.
//
// # It is a separate type from [Documents], and the split is the same one that file makes
//
// [Documents] holds a signer, because issuing and reading an image is what it is for. Nothing here
// needs one: the queue answers *when* things lapse, and a reviewer who wants to look at an image
// asks the viewer for it (SHIP-155) — which writes an access entry, and a queue load must not. A
// type that could do both would make every list of expiring insurance an unlogged read of the
// insurance itself.
//
// The blank line below keeps this a file note rather than a second package comment.

package profiles

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/DulsaraNethmin/Shipper/services/core/internal/clock"
	"github.com/DulsaraNethmin/Shipper/services/core/internal/db"
)

// Expiry is the read behind Docs/04 §5's expiry queue (SHIP-159).
//
// It owns no connection, for the reason [Service] and [Documents] do not: the transaction belongs to
// whoever owns the invariant being protected (Docs/10 §3.2), and a queue owns none.
type Expiry struct {
	clock clock.Clock
	store postgresStore

	// lead is how far ahead of its expiry a document of each kind is worth chasing.
	//
	// **Absent means zero, and zero means "when it has lapsed".** There is no fallback value and
	// there must not be one — see the file header. The map is copied at construction so that a
	// caller holding what it passed cannot widen the platform's horizons afterwards.
	lead map[Kind]time.Duration
}

// NewExpiry builds the queue's reader.
//
// # It accepts an empty lead-time map and refuses a nonsensical one
//
// Empty is the shipping default and is the whole of X-4's answer being absent: every kind's horizon
// is *now*, and only lapsed documents surface. That is a service doing less than it eventually will,
// not a mis-wire, so it is accepted silently.
//
// A **negative** duration is refused, because it is not a smaller policy — it is a horizon in the
// past, which would hide documents that have already lapsed. A queue that quietly stops reporting
// expired insurance is the failure this ticket exists to prevent, and it would look exactly like a
// quiet week.
//
// A kind the platform does not have is refused too. A typo in `VERIFICATION_EXPIRY_LEAD_TIMES` would
// otherwise be configuration that silently does nothing — set in staging, restarted, and the queue
// unchanged, with the obvious conclusion being that the feature is broken. It panics rather than
// returning an error for [NewDocuments]' reason: this is called once from the composition root, and a
// misconfiguration that is there at startup will still be there after a restart.
func NewExpiry(c clock.Clock, lead map[Kind]time.Duration) *Expiry {
	if c == nil {
		panic("profiles: NewExpiry needs a clock (Docs/10 §6.3)")
	}

	held := make(map[Kind]time.Duration, len(lead))
	for kind, ahead := range lead {
		if !kind.Valid() {
			panic(fmt.Sprintf(
				"profiles: %q is not one of Docs/04 §3's documents, so a lead time for it "+
					"would be configuration that silently does nothing", kind))
		}
		if ahead < 0 {
			panic(fmt.Sprintf(
				"profiles: the %s lead time is %s; a negative horizon hides documents that "+
					"have already lapsed, which is the failure this queue exists to prevent",
				kind, ahead))
		}
		held[kind] = ahead
	}

	return &Expiry{clock: c, store: postgresStore{}, lead: held}
}

// LeadTimes is the configured horizons, as a fresh map.
//
// A copy rather than the stored map, for [Role.Permissions]' reason one package over: a caller that
// wrote into what it was handed would be changing the platform's horizons for the whole process.
//
// It exists so that a caller can report what was configured — the console shows a reviewer why a
// document is on the queue, and "there is no configured lead time for this kind" is the answer that
// otherwise looks like a bug.
func (e *Expiry) LeadTimes() map[Kind]time.Duration {
	out := make(map[Kind]time.Duration, len(e.lead))
	for kind, ahead := range e.lead {
		out[kind] = ahead
	}
	return out
}

// ExpiringDocument is one current document that has lapsed or is about to.
//
// **No budget, no job and no bid**, which is structural in the way `admin.VerificationEntry` is:
// there is nowhere on this struct to put one, so no change to a response mapping can acquire one.
type ExpiringDocument struct {
	// DocumentID is the `provider_verification_documents` row. It is the queue's tie-break and
	// the value a cursor carries.
	DocumentID uuid.UUID

	ProviderID uuid.UUID

	// Name is what the account holder is called (SHIP-30a). Empty for an account created before
	// `000006` — a name cannot be backfilled, so the console shows that it has none.
	Name  string
	Email string
	Phone string

	// VerificationState is where the provider stands today, one of Docs/04 §4's five.
	//
	// On the entry and **not** a filter, deliberately. A suspended provider's lapsed insurance is
	// not worth chasing and a verified provider's is urgent, but which of the five deserve
	// attention is a triage decision an administrator makes — and a filter here would be this
	// package deciding it for them, from a document that says nothing about it.
	VerificationState State

	Kind Kind

	// ExpiresAt is the date the document itself states. Never zero: a document with no stated
	// expiry is not on this queue at all.
	ExpiresAt time.Time

	// Expired is whether that date has passed, measured against the injected clock.
	//
	// Computed here rather than left to a reader, because the comparison has to be made against
	// one instant for the whole page — a console deciding per row from its own clock would render
	// a page in which the boundary moved while it was being drawn.
	Expired bool

	SubmittedAt time.Time
}

// ExpiryQuery is one page of the expiry queue.
//
// Cursor paged rather than offset paged, per Docs/10 §4.5, and for the reason [QueueQuery] records:
// documents are submitted while somebody is working through the queue, and a skipped row here is a
// provider whose lapsed insurance nobody chased.
type ExpiryQuery struct {
	// Limit is how many entries to return. Bounded by internal/pagination before it gets here.
	Limit int

	// After is where the previous page stopped. The zero value is the first page.
	After ExpiryCursor
}

// ExpiryCursor is the position of the last entry a caller saw.
//
// Two fields, because an expiry date is not unique — two documents lapsing on the same day is the
// ordinary case rather than a coincidence, since the dates are printed on documents rather than
// generated. `idx_provider_verification_documents_expiry` is `(expires_at, id)` for this.
type ExpiryCursor struct {
	ExpiresAt  time.Time
	DocumentID uuid.UUID
}

// Zero reports whether this is the first page.
func (c ExpiryCursor) Zero() bool {
	return c.DocumentID == uuid.Nil && c.ExpiresAt.IsZero()
}

// Due is one page of documents at or past their horizon, soonest first (SHIP-159).
//
// # What "due" means, per kind, and where each half of it comes from
//
// A document is due when `expires_at <= now + lead(kind)`. The date is the document's own and the
// lead time is configuration; with none configured the horizon is *now* and only lapsed documents
// come back. See the file header for why there is no default.
//
// Soonest first, because the queue's purpose is that something is running out: the entry at the top
// is the one that has been out of date longest, exactly as [Service.AwaitingReview] puts the
// longest-waiting provider first.
//
// r is a reader rather than a transaction. One statement, no writes, nothing to keep consistent —
// and deliberately no audit entry: this is the console's own working surface, not somebody's
// identity documents. SHIP-155's viewer is the read that records who looked.
func (e *Expiry) Due(ctx context.Context, r db.Runner, q ExpiryQuery) ([]ExpiringDocument, error) {
	now := e.clock.Now()

	// One horizon per kind, computed here so the SQL takes a value rather than a policy. Every
	// kind is listed, including the ones with no configured lead time — their horizon is `now`,
	// which is what makes "expired" answerable with no configuration at all.
	kinds := make([]string, 0, len(Kinds))
	horizons := make([]time.Time, 0, len(Kinds))
	for _, kind := range Kinds {
		kinds = append(kinds, string(kind))
		horizons = append(horizons, now.Add(e.lead[kind]))
	}

	found, err := e.store.documentsDue(ctx, r, kinds, horizons, q)
	if err != nil {
		return nil, err
	}

	for i := range found {
		found[i].Expired = !found[i].ExpiresAt.After(now)
	}
	return found, nil
}
