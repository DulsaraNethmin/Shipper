package jobs

import (
	"context"

	"github.com/google/uuid"

	"github.com/DulsaraNethmin/Shipper/services/core/internal/db"
	"github.com/DulsaraNethmin/Shipper/services/core/internal/events"
)

// EventSink is where this domain's events go.
//
// Declared here rather than taken as *events.Outbox because Docs/06 §4.1 makes the consuming
// domain the one that names the interface. The concrete writer is infrastructure and this domain
// may import it either way — what the interface buys is that a test can watch what was emitted
// without a table, and that SHIP-134's publisher can change the writer without touching a domain.
//
// Emit takes the same db.Runner the state change is using, and that is the entire point of the
// outbox: an event written in a different transaction from the change it describes can commit
// when the change does not (Docs/06 §4.0, Docs/10 §6.1).
type EventSink interface {
	Emit(ctx context.Context, r db.Runner, e events.Event) error
}

// Geocoder resolves an address to a coordinate (SHIP-59a, SHIP-60).
//
// Declared here for the same reason [EventSink] is, and under a stricter rule: the boundary lint
// refuses a domain that imports an adapter at all. Go satisfies this structurally, so neither
// internal/platform/geocoding nor this package names the other, and the two meet in cmd/api.
//
// # Why the signature is five values wide
//
// Because a result struct declared in the adapter could not appear here without the import that
// rule forbids, and a sentinel error for not-found could not either — errors.Is against
// geocoding.ErrNotFound is an import too. So the seam speaks only standard-library types.
//
// Docs/11 §9 asked whether a neutral infrastructure package should hold a coordinate type and end
// this, and SHIP-60 settled it: not yet. The width is named once — here — and converted
// immediately into [Location], so it does not reach the store, the handlers or the tests.
// Docs/11 §3 records the reasoning and the trigger for revisiting it.
//
// # found is not err
//
// found is false with a nil error when the provider answered and did not recognise the address.
// err is non-nil only when the lookup did not complete. The two are different outcomes and this
// domain treats them differently only in what it logs: either way the address is stored
// unresolved and the job goes on, because SHIP-59a is explicit that a failed lookup does not fail
// the job.
type Geocoder interface {
	Lookup(ctx context.Context, address string) (lat, lng float64, formatted string, found bool, err error)
}

// Bidders answers whether a provider has offered on a job (SHIP-65a).
//
// # Why this domain has to ask somebody else
//
// `bids` belongs to `bidding`, and a domain may not import another domain. The question is
// nonetheless this domain's to ask: SHIP-65a serves a job's status history to *its parties*, and
// one of the two parties is "a provider holding a bid on that job". Declared here for the reason
// [EventSink] and [Geocoder] are — the consuming domain names the interface (Docs/06 §4.1) — and
// implemented in cmd/api, where knowing about more than one domain is the composition root's
// entire job. `delivery.Awards` is the same arrangement for the same reason, and its
// implementation sits four hundred lines from where this one will.
//
// # It asks about *any* bid, and deliberately holds no copy of Docs/02 §4's vocabulary
//
// Not "an accepted bid", not "a live bid", not "a bid that is not withdrawn". Two reasons, and
// the second is the one that decides it:
//
//   - A provider who bid and lost still has a legitimate interest in what became of the job they
//     priced. A provider who withdrew has one too — Docs/02 §6.2 makes a withdrawal a commercial
//     act with consequences, and the history is where they are recorded.
//   - Filtering on a status would put `bidding`'s eight-value enumeration in a `jobs` query, which
//     is a second copy of another domain's vocabulary in a file that cannot see it change
//     (Docs/10 §3.4). A bid row is a fact; which of the eight it is standing at is not this
//     domain's reading to take.
//
// # A boolean, not a set
//
// The caller is one account asking about one job. Returning the bids themselves would put an
// amount — the provider's own, but an amount — on a seam that has no use for one, and this
// domain's response type would then have something to remember not to render.
type Bidders interface {
	HasBidOn(ctx context.Context, r db.Runner, jobID, providerID uuid.UUID) (bool, error)
}
