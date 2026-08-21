package email

import (
	"context"
	"errors"
	"log/slog"

	"github.com/DulsaraNethmin/Shipper/services/core/internal/httpx"
)

// ErrNoRecipient is returned when a message is handed over with nowhere to go.
//
// Both implementations refuse it, so development behaves the way staging will rather than
// discovering the difference the first time a real provider rejects the call.
var ErrNoRecipient = errors.New("email: no recipient")

// Console renders a message to the log and sends nothing.
//
// It is the development implementation, and it is the default rather than an opt-in: an
// environment that silently sends real email to a real address is a mistake that only
// announces itself after it has happened, by which time it has reached somebody's inbox.
type Console struct{}

// NewConsole returns the console implementation.
//
// A constructor rather than a bare struct literal so the wiring in cmd/api reads the same
// shape for both implementations, and so this one can acquire a field later without every
// call site changing.
func NewConsole() *Console { return &Console{} }

// Send writes the message to the request-scoped log and returns.
//
// The body is logged in full and unredacted. That is the point of this implementation —
// the verification link in a development signup is read out of the log (SHIP-31, SHIP-33),
// and a redacted one would be useless. It is also exactly why config.Email.Transport must decide
// where this runs rather than a caller remembering to.
func (c *Console) Send(ctx context.Context, to, subject, body string) error {
	if to == "" {
		return ErrNoRecipient
	}

	// httpx.LoggerFrom is already bound to the request ID (SHIP-14) and falls back to the
	// default logger outside a request, so background work logs correctly too.
	httpx.LoggerFrom(ctx).LogAttrs(ctx, slog.LevelInfo, "email (console, not sent)",
		slog.String("to", to),
		slog.String("subject", subject),
		slog.String("body", body),
	)
	return nil
}
