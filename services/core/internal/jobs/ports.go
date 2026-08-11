package jobs

import (
	"context"

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
