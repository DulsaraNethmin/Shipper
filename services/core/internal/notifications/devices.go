package notifications

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/DulsaraNethmin/Shipper/services/core/internal/db"
)

// The device token registry (SHIP-140): what a push notification is addressed to.
//
// 000701 carries the schema argument — why the grain is the device session, why sign-out clears a
// token without anything writing here, and why the token is stored in the clear. This file is the
// three operations over it.

// Platform is which store the app came from.
//
// Not how a message is routed — FCM fronts APNs, so one adapter reaches both (Docs/06 §2). It is
// what lets support answer "which handsets is this person signed in on", and it is paired against
// ck_device_tokens_platform in both directions by a test in migrations/ (Docs/10 §3.4).
type Platform string

const (
	PlatformIOS     Platform = "ios"
	PlatformAndroid Platform = "android"
)

// Platforms is the closed set, in the order 000701's CHECK lists them.
var Platforms = []Platform{PlatformIOS, PlatformAndroid}

// Valid reports whether p is one of [Platforms].
func (p Platform) Valid() bool {
	for _, known := range Platforms {
		if p == known {
			return true
		}
	}
	return false
}

func (p Platform) String() string { return string(p) }

// The reasons a token stops being addressed, matching ck_device_tokens_revoked_reason.
//
// Three, and each has a writer here rather than being a value somebody might want later.
const (
	// RevokedDeregistered is the client asking, which SHIP-143 does at sign-out.
	RevokedDeregistered = "deregistered"

	// RevokedReplaced is the same device registering a different token. FCM rotates one on
	// reinstall, on restore to a new handset, and periodically of its own accord.
	RevokedReplaced = "replaced"

	// RevokedRejected is the push provider saying the token is dead — an uninstall, or a token
	// belonging to another project. **This is the one that must not be an error**: see
	// internal/platform/push/doc.go, and [Service.Dispatch] for what happens when it arrives.
	RevokedRejected = "rejected"
)

// DeviceToken is one row of the device_tokens table.
//
// SessionID is what makes it clear on sign-out and is therefore the field this whole ticket is
// about; see 000701 and [Sessions].
type DeviceToken struct {
	ID           uuid.UUID
	UserID       uuid.UUID
	SessionID    uuid.UUID
	Platform     Platform
	Token        string
	RegisteredAt time.Time
	RevokedAt    *time.Time
}

// RegisterDevice records the token this device is addressable at, and returns it.
//
// # Registering twice is the ordinary case, not the exception
//
// SHIP-143 registers at every launch, because FCM hands the app a token each time and only
// sometimes the same one. So three things have to be true at once and all three are one statement
// in postgres.go rather than a read followed by a decision:
//
//   - the same token presented again for the same session changes nothing but registered_at;
//   - a different token for the same session replaces the previous one, which is revoked
//     `replaced` rather than deleted;
//   - the same token arriving from a *different* session — a handset restored from another
//     device's backup — leaves exactly one live row, so a push meant for the previous owner
//     cannot reach the new one.
//
// The last of those is the one that would be easy to get wrong, and uq_device_tokens_live_token is
// what makes it structural.
//
// # It must run in a transaction
//
// Revoking the previous row and inserting the new one are one change: committed apart, a crash
// between them leaves a device with no live token and nothing to say so. Checked rather than
// documented, for the reason [Consume] checks.
func (s *Service) RegisterDevice(
	ctx context.Context, r db.Runner, userID, sessionID uuid.UUID, platform Platform, token string,
) (DeviceToken, error) {
	if _, inTx := r.(pgx.Tx); !inTx {
		return DeviceToken{}, fmt.Errorf("notifications: registering a device: %w", ErrNotInTransaction)
	}
	if !platform.Valid() {
		return DeviceToken{}, fmt.Errorf("%w: %q", ErrUnknownPlatform, platform)
	}
	if token == "" {
		return DeviceToken{}, ErrNoDeviceToken
	}

	now := s.clock.Now().UTC()

	// Everything live that this registration displaces: this session's previous token, and the
	// same token wherever else it is live. Both are `replaced` — the row is not wrong, it has
	// been superseded, and 000701 keeps it so a device's token history stays readable.
	if err := s.store.revokeDisplaced(ctx, r, sessionID, token, RevokedReplaced, now); err != nil {
		return DeviceToken{}, err
	}

	id, err := uuid.NewV7()
	if err != nil {
		return DeviceToken{}, fmt.Errorf("notifications: generating an id: %w", err)
	}

	registered := DeviceToken{
		ID:           id,
		UserID:       userID,
		SessionID:    sessionID,
		Platform:     platform,
		Token:        token,
		RegisteredAt: now,
	}
	if err := s.store.insertDeviceToken(ctx, r, registered); err != nil {
		return DeviceToken{}, err
	}
	return registered, nil
}

// DeregisterDevice stops addressing whatever this device session has registered.
//
// It reports whether anything was live, which is what lets the endpoint answer honestly rather than
// pretending. Deregistering twice is not an error — a client retrying after a dropped connection
// must not be told it did something wrong — but "there was nothing registered" and "the token was
// revoked" are different facts and the caller gets both.
//
// # This is a courtesy, not the control
//
// Sign-out clears a token whether or not this is called, because nothing addresses a device whose
// session has been revoked ([Sessions]). A handset switched off between the sign-out and the next
// notification never makes this call, and the platform's guarantee cannot depend on it. What this
// buys is a row that says `deregistered` rather than one that stays live-looking until somebody
// reads the session beside it.
func (s *Service) DeregisterDevice(ctx context.Context, r db.Runner, sessionID uuid.UUID) (bool, error) {
	return s.store.revokeSessionTokens(ctx, r, sessionID, RevokedDeregistered, s.clock.Now().UTC())
}

// DeregisterToken stops addressing one token, wherever it is registered.
//
// Called when the push provider says the token is dead (SHIP-139), which is why it takes the token
// rather than a session: the dispatcher holds an address and not a device. It is the one path here
// with no request behind it — nobody asked, the far end simply stopped accepting — and it is
// deliberately silent about having found nothing, because a token rejected twice in one batch is
// ordinary.
func (s *Service) DeregisterToken(ctx context.Context, r db.Runner, token string) error {
	_, err := s.store.revokeToken(ctx, r, token, RevokedRejected, s.clock.Now().UTC())
	return err
}
