package push

import (
	"context"
	"errors"
	"log/slog"
	"sync"

	"github.com/google/uuid"

	"github.com/DulsaraNethmin/Shipper/services/core/internal/httpx"
)

// ErrNoDeviceToken is returned when a message is handed over with no device to send it to.
//
// Both implementations refuse it, so development behaves the way staging will rather than
// discovering the difference the first time FCM answers 400 — the same pairing
// email.ErrNoRecipient makes.
//
// It is deliberately **not** a rejection. A rejection is the far end saying a real token is
// dead; this is the platform having produced a row with no address at all, which
// ck_notifications_address should have made impossible and which deregistering nothing would
// hide.
var ErrNoDeviceToken = errors.New("push: no device token")

// Noop renders a push to the log and sends nothing.
//
// It is the development implementation and the default (see [UseNoop]). Unlike the email
// console it is also the implementation every environment falls back to when no Firebase
// project is configured, which is the ordinary state of this repository: no project exists,
// no service-account key may be committed, and a channel that pretended to send would report a
// success it did not have.
type Noop struct {
	mu   sync.Mutex
	sent []Sent
}

// Sent is one push this implementation was asked to deliver.
//
// Recorded so that a test — and scripts/verify — can assert what would have gone out without a
// Firebase project. It is the same job email.Console does with a log line, kept in memory as
// well because a push has no address a harness could inspect afterwards.
type Sent struct {
	DeviceToken string
	Title       string
	Body        string
	JobID       uuid.UUID
}

// NewNoop returns the development implementation.
func NewNoop() *Noop { return &Noop{} }

// Push records the message and reports it delivered.
//
// It never rejects. A no-op that rejected tokens would deregister real devices in development
// on the strength of nothing having happened, and the deregistration path
// (notifications.Service.Dispatch) is the same code in every environment.
func (n *Noop) Push(
	ctx context.Context, deviceToken, title, body string, jobID uuid.UUID,
) (rejected bool, err error) {
	if deviceToken == "" {
		return false, ErrNoDeviceToken
	}

	n.mu.Lock()
	n.sent = append(n.sent, Sent{DeviceToken: deviceToken, Title: title, Body: body, JobID: jobID})
	n.mu.Unlock()

	// httpx.LoggerFrom is already bound to the request ID (SHIP-14) and falls back to the
	// default logger outside a request, which is where a consumer always is.
	//
	// The device token is logged truncated. It is not a credential — it addresses a handset
	// rather than authenticating anybody — but it is the one value here that identifies a
	// particular person's phone, and a development log is the least protected place in this
	// system.
	httpx.LoggerFrom(ctx).LogAttrs(ctx, slog.LevelInfo, "push (noop, not sent)",
		slog.String("device", Fingerprint(deviceToken)),
		slog.String("title", title),
		slog.String("body", body),
		slog.String("job_id", jobID.String()),
	)
	return false, nil
}

// Sent returns what this implementation was asked to deliver, oldest first.
func (n *Noop) Sent() []Sent {
	n.mu.Lock()
	defer n.mu.Unlock()
	out := make([]Sent, len(n.sent))
	copy(out, n.sent)
	return out
}

// fingerprintLength is how much of a device token is legible in a log.
const fingerprintLength = 8

// Fingerprint is the leading characters of a device token, for a log line.
//
// Exported because the domain logs a deregistration too and the two records have to be
// matchable by eye. Not a hash: the point is to correlate two lines about one device within one
// process's logs, and a prefix does that while staying useless to anyone who only has the log.
func Fingerprint(deviceToken string) string {
	if len(deviceToken) <= fingerprintLength {
		return deviceToken
	}
	return deviceToken[:fingerprintLength] + "…"
}
