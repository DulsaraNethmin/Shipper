package sms

import (
	"context"
	"errors"
	"log/slog"

	"github.com/DulsaraNethmin/Shipper/services/core/internal/httpx"
)

// ErrNoRecipient is returned when a message is handed over with no number to reach.
//
// Both implementations refuse it, so development behaves the way staging will rather than
// discovering the difference the first time a real gateway rejects the call.
var ErrNoRecipient = errors.New("sms: no recipient")

// Console renders a message to the log and sends nothing.
//
// It is the development implementation and the default. That matters more here than it
// does for email: an SMS costs money per message and arrives on a real handset, so a retry
// loop against a live gateway is a bill as well as a nuisance — and the handset it wakes up
// belongs to whoever last used that number for testing.
type Console struct{}

// NewConsole returns the console implementation.
func NewConsole() *Console { return &Console{} }

// Send writes the message to the request-scoped log and returns.
//
// The body is logged in full, which for the MVP means the OTP is legible in the log. That
// is deliberate: reading the code out of the log is how a developer completes phone
// verification without a handset (SHIP-34, SHIP-36). It is also exactly why the transport setting
// decides where this runs rather than a caller remembering to.
func (c *Console) Send(ctx context.Context, to, body string) error {
	if to == "" {
		return ErrNoRecipient
	}

	// httpx.LoggerFrom is already bound to the request ID (SHIP-14) and falls back to the
	// default logger outside a request, so background work logs correctly too.
	httpx.LoggerFrom(ctx).LogAttrs(ctx, slog.LevelInfo, "sms (console, not sent)",
		slog.String("to", to),
		slog.String("body", body),
	)
	return nil
}
