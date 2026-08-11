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
