// SHIP-39: device sessions, refresh tokens, and rotation on every use.
//
// A device session is one row in device_sessions (SHIP-38) holding the hash of exactly one live
// refresh token. Presenting that token exchanges it for a new access token *and* a new refresh
// token; the one presented stops working the moment the exchange commits. That is the whole of
// "rotation", and Docs/07 §3 requires it: a refresh token is long-lived and sits on a phone, so
// the window in which a copied one is useful has to be the interval between two uses rather than
// its whole lifetime.
//
// # Why the token is opaque and stored hashed
//
// Docs/10 §5 fixes both. Opaque, because rotation and reuse detection need server-side state
// regardless — a self-describing token would add signing risk without saving the lookup. Hashed,
// because the row is a credential: this table is read by every support query, every backup and
// every replica, and a readable refresh token in any of those is a signed-in session somebody
// else can take over.
//
// SHA-256 rather than argon2id, which is the opposite choice from the phone OTP and the same one
// as the email verification token, for the same reason: a work factor exists to make a *small*
// search space expensive, and this one has 2^256 elements. Paying tens of milliseconds per
// verification would buy nothing and would be paid by every device on every refresh.
//
// The blank line below keeps this a file note rather than a second package comment.

package identity

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/DulsaraNethmin/Shipper/services/core/internal/db"
	"github.com/DulsaraNethmin/Shipper/services/core/internal/httpx"
	"github.com/DulsaraNethmin/Shipper/services/core/internal/validate"
)

const (
	// refreshTokenTTL is how long a refresh token stays usable, measured from the moment it
	// was issued and rewritten on every rotation.
	//
	// The window therefore slides: thirty days of *inactivity* ends a session, and a device in
	// daily use never reaches it. Docs/07 §3 asks for a "longer-lived" token rotated on every
	// use, and that is what a sliding window means — the alternative reading, an absolute cap
	// from sign-in, would sign a driver out mid-delivery on a schedule.
	//
	// A constant rather than configuration, and that is the same scope decision the password
	// limits in service.go name. internal/config is a shared surface (Docs/10 §9.2) and this
	// is not a lever anybody has asked to pull without a deploy; moving it there belongs to
	// whoever owns that file next, and 000103's comment says where it would go.
	refreshTokenTTL = 30 * 24 * time.Hour

	// refreshTokenBytes is the entropy in a refresh token: 32 bytes from crypto/rand,
	// base64url encoded to 43 characters. The same construction as the email verification
	// token, and what makes SHA-256 the right way to store it.
	refreshTokenBytes = 32

	// maxDeviceLabelLength mirrors ck_device_sessions_device_label. The database is the
	// authority; this exists so the caller is told which field was wrong rather than being
	// handed a constraint violation.
	maxDeviceLabelLength = 120
)

// The reasons a device session stops being usable. They are exactly the values
// ck_device_sessions_revoked_reason permits (000104), so the Go constants and the database
// constraint cannot drift into disagreeing (Docs/10 §3.4) — TestRevokedReasonsMatchTheConstraint
// is what holds the two together.
//
// Three, and each has a ticket behind it rather than being a value somebody might want later.
const (
	// revokedReasonTokenReused is SHIP-40: a token that had already been rotated away was
	// presented again.
	revokedReasonTokenReused = "refresh_token_reused"

	// revokedReasonSignedOut is SHIP-43: the person signed this device out.
	revokedReasonSignedOut = "signed_out"

	// revokedReasonByOwner is SHIP-46: the person revoked it from their device list.
	revokedReasonByOwner = "revoked_by_owner"
)

// revokedReasons is the closed list, for the test that reads the CHECK constraint back.
var revokedReasons = []string{
	revokedReasonTokenReused,
	revokedReasonSignedOut,
	revokedReasonByOwner,
}

// RefreshToken is an issued refresh token and the instant it stops being usable.
//
// The expiry travels with the value for the reason [AccessToken] gives: the endpoints that
// return one tell the client how long it has, and recomputing it from a TTL at each call site is
// how the two end up disagreeing.
type RefreshToken struct {
	Value     string
	ExpiresAt time.Time
}

// TokenPair is what a sign-in (SHIP-41) and a refresh (SHIP-42) both return.
//
// The session id is part of it because the access token carries it as `sid` and a caller that
// wants to record which device acted should not have to decode a token to find out.
type TokenPair struct {
	SessionID uuid.UUID
	Access    AccessToken
	Refresh   RefreshToken
}

// deviceSession is one row of device_sessions.
//
// The raw refresh token is never a field. It exists in the response that carried it and on the
// device that received it, and nowhere else.
type deviceSession struct {
	ID     uuid.UUID
	UserID uuid.UUID

	RefreshTokenHash      string
	RefreshTokenExpiresAt time.Time

	DeviceLabel string
	LastSeenAt  time.Time

	// RevokedAt and RevokedReason are nil while the session is live (SHIP-40). The row is
	// marked rather than deleted: "signed out three weeks ago" is what makes a device list
	// readable, and Docs/10 §3.3 forbids deletion as a way of ending something.
	RevokedAt     *time.Time
	RevokedReason *string

	CreatedAt time.Time
}

func (s deviceSession) revoked() bool { return s.RevokedAt != nil }

func (s deviceSession) refreshExpiredAt(now time.Time) bool {
	return !now.Before(s.RefreshTokenExpiresAt)
}

// newRefreshToken produces a token and the hash to store against it.
//
// The two are produced together, by one function, because the property that matters is that the
// stored value is derived from the issued one. Splitting them is how a refactor ends up storing
// the token itself — the same argument newVerificationToken makes.
func newRefreshToken() (raw, hash string, err error) {
	buf := make([]byte, refreshTokenBytes)
	if _, err := rand.Read(buf); err != nil {
		return "", "", fmt.Errorf("identity: reading refresh token entropy: %w", err)
	}

	raw = base64.RawURLEncoding.EncodeToString(buf)
	return raw, hashRefreshToken(raw), nil
}

// hashRefreshToken is the one-way function device_sessions stores.
//
// It is deliberately its own function rather than a call to hashVerificationToken, which today
// computes the identical digest. They are two credentials with two lifetimes stored in two
// tables, and a shared helper named after one of them is how the other's storage silently
// changes when the first is revised. The reasoning for SHA-256 over argon2id is in this file's
// header and is the same for both.
func hashRefreshToken(raw string) string {
	sum := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(sum[:])
}

// normaliseDeviceLabel puts a client-supplied label into the form the row holds.
//
// Whitespace only, because the label is display text a person typed for themselves — "Nethmin's
// iPhone" — and it is never used to authenticate anything. 000100 is explicit that two phones
// may legitimately carry the same label.
func normaliseDeviceLabel(submitted string) string { return strings.TrimSpace(submitted) }

// validateDeviceLabel checks the label the way ck_device_sessions_device_label will.
//
// One function rather than a repeated pair of checks, so that sign-in (SHIP-41) and any later
// caller give the same answer about what a usable label is. The field path is the JSON name a
// client sends, so the message can be put beside the input that caused it (Docs/10 §4.6).
func validateDeviceLabel(label string) error {
	var v validate.Errors
	if v.Required("device_label", label) {
		v.Length("device_label", label, 1, maxDeviceLabelLength)
	}
	return v.Err()
}

// startSession creates a device session and issues its first token pair (SHIP-39).
//
// It takes a db.Runner rather than opening its own transaction, per Docs/10 §3.2: SHIP-41's
// sign-in verifies a password and creates a session, and those belong in one transaction decided
// by the caller rather than by this method.
//
// It is unexported on purpose. Creating a session is how a caller obtains a credential, and the
// only paths that may do it are the ones inside this package that have first established who is
// asking — sign-in, and nothing else. A handler in cmd/api must not be able to mint one.
func (s *Service) startSession(ctx context.Context, r db.Runner, user User, deviceLabel string) (TokenPair, error) {
	label := normaliseDeviceLabel(deviceLabel)
	if err := validateDeviceLabel(label); err != nil {
		return TokenPair{}, err
	}

	id, err := uuid.NewV7()
	if err != nil {
		return TokenPair{}, fmt.Errorf("identity: generating a device session id: %w", err)
	}

	raw, hash, err := newRefreshToken()
	if err != nil {
		return TokenPair{}, err
	}

	// One clock for both timestamps, for the reason insertEmailToken gives: created_at has a
	// database default and refresh_token_expires_at is computed here, so a row written from two
	// clocks can violate ck_device_sessions_refresh_expiry under ordinary skew.
	now := s.clock.Now().UTC()
	session := deviceSession{
		ID:                    id,
		UserID:                user.ID,
		RefreshTokenHash:      hash,
		RefreshTokenExpiresAt: now.Add(refreshTokenTTL),
		DeviceLabel:           label,
		LastSeenAt:            now,
		CreatedAt:             now,
	}

	if err := s.store.insertDeviceSession(ctx, r, session); err != nil {
		return TokenPair{}, err
	}

	return s.pairFor(user, session, raw)
}

// pairFor signs the access token that goes with a freshly issued refresh token.
//
// One function rather than two call sites, because a sign-in and a refresh must produce
// identically shaped credentials — a pair issued one way and honoured the other is the defect
// that only shows up on the device that did the unusual one.
func (s *Service) pairFor(user User, session deviceSession, rawRefresh string) (TokenPair, error) {
	access, err := s.issuer.Issue(user.ID, session.ID, user.Role)
	if err != nil {
		return TokenPair{}, fmt.Errorf("identity: issuing an access token: %w", err)
	}

	return TokenPair{
		SessionID: session.ID,
		Access:    access,
		Refresh: RefreshToken{
			Value:     rawRefresh,
			ExpiresAt: session.RefreshTokenExpiresAt,
		},
	}, nil
}

// Refresh exchanges a refresh token for a new pair, invalidating the one presented (SHIP-39).
//
// # What "invalidates its predecessor" means here
//
// The session row holds exactly one hash. Rotation overwrites it, so the token that was
// presented matches nothing the moment the transaction commits — there is no separate
// revocation step that could be forgotten, and no window in which both tokens work. The spent
// hash is written to consumed_refresh_tokens in the same transaction (SHIP-40), which is what
// turns a second presentation from "unknown token" into "reuse".
//
// # Why every refusal is one error
//
// Unknown token, expired token, revoked session, suspended account: all
// [ErrRefreshTokenInvalid]. The remedy is identical for every one of them — sign in again — and
// naming which check refused a credential is help only somebody probing has a use for. The same
// reasoning as [CodeOTPInvalid], one credential along.
//
// [ErrRefreshTokenReused] is the one exception, and it is not a distinction the *caller* sees:
// apiError maps both to one code. It exists so the service can log a security event and so a
// test can tell "the session was revoked" from "the token was refused", which are different
// claims.
//
// # Why the transaction's outcome is separated from its error
//
// Detecting a reused token writes a revocation, and returning an error from inside db.InTx rolls
// the whole closure back — so the revocation would be undone by the very error that reports it.
// VerifyPhone documents the same trap in the same package: the closure returns nil having
// recorded what happened, and the outcome becomes an error after the commit. The obvious tidy-up
// reintroduces the defect silently, which is why it is written down twice.
func (s *Service) Refresh(ctx context.Context, presented string) (TokenPair, error) {
	raw := strings.TrimSpace(presented)
	if raw == "" {
		return TokenPair{}, ErrRefreshTokenInvalid
	}
	if s.pool == nil {
		return TokenPair{}, errUnavailable
	}

	var (
		pair    TokenPair
		outcome error
	)
	if err := db.InTx(ctx, s.pool, func(ctx context.Context, r db.Runner) error {
		var err error
		pair, outcome, err = s.rotate(ctx, r, hashRefreshToken(raw))
		return err
	}); err != nil {
		return TokenPair{}, err
	}
	if outcome != nil {
		return TokenPair{}, outcome
	}
	return pair, nil
}

// rotate is the body of a refresh, inside the caller's transaction.
//
// It returns three things and the split is deliberate: a pair on success, a *domain refusal* that
// must survive the commit, and an *infrastructure failure* that must roll it back.
func (s *Service) rotate(ctx context.Context, r db.Runner, hash string) (pair TokenPair, refusal, failure error) {
	now := s.clock.Now().UTC()

	// FOR UPDATE, and it is load-bearing. Two refreshes presenting the same token arrive
	// together on a flaky connection; the lock serialises them, and PostgreSQL re-evaluates the
	// predicate after the lock is released, so the second one finds no live session rather than
	// rotating a hash the first has already replaced. Without it both would rotate, and the
	// device would end up holding a token the session no longer knows about.
	session, err := s.store.deviceSessionByRefreshHash(ctx, r, hash)
	if err != nil {
		if isNoRows(err) {
			// No session holds this token. It was never issued, or it has already been
			// rotated away, and those are answered very differently (SHIP-40).
			return s.detectReuse(ctx, r, hash, now)
		}
		return TokenPair{}, nil, fmt.Errorf("identity: reading a device session: %w", err)
	}

	switch {
	case session.revoked():
		// SHIP-40's revocation, SHIP-43's sign-out and SHIP-46's revoke all land here. The
		// session is over, and the token that survives on the device buys nothing.
		return TokenPair{}, ErrRefreshTokenInvalid, nil

	case session.refreshExpiredAt(now):
		return TokenPair{}, ErrRefreshTokenInvalid, nil
	}

	user, err := s.store.userByID(ctx, r, session.UserID)
	if err != nil {
		if isNoRows(err) {
			// fk_device_sessions_user is ON DELETE RESTRICT, so this is unreachable short of
			// a manual deletion the constraint also refuses. Reported as invalid rather than
			// as a 500, because from the caller's side it is: the session names nobody.
			return TokenPair{}, ErrRefreshTokenInvalid, nil
		}
		return TokenPair{}, nil, fmt.Errorf("identity: reading the account for a refresh: %w", err)
	}
	if !user.CanSignIn() {
		// A suspended account is told exactly what an unknown token is told. The account
		// holder learns why at sign-in (SHIP-41), which is the endpoint that can say it to
		// somebody who has just proved they own the account; saying it here would disclose
		// account standing to whoever is holding a stolen token.
		return TokenPair{}, ErrRefreshTokenInvalid, nil
	}

	raw, next, err := newRefreshToken()
	if err != nil {
		return TokenPair{}, nil, err
	}

	// The spent hash is recorded before the session takes the new one, and both are in the
	// caller's transaction. A rotation that committed without the ledger row would leave a
	// token nothing can recognise as spent — which is the whole of reuse detection, lost with
	// no symptom until somebody uses a copy.
	if err := s.store.insertConsumedRefreshToken(ctx, r, session.ID, session.RefreshTokenHash, now); err != nil {
		return TokenPair{}, nil, err
	}

	rotated := session
	rotated.RefreshTokenHash = next
	rotated.RefreshTokenExpiresAt = now.Add(refreshTokenTTL)
	rotated.LastSeenAt = now

	if err := s.store.rotateDeviceSession(ctx, r, rotated, session.RefreshTokenHash); err != nil {
		return TokenPair{}, nil, err
	}

	pair, err = s.pairFor(user, rotated, raw)
	if err != nil {
		return TokenPair{}, nil, err
	}
	return pair, nil, nil
}

// detectReuse decides what a token that matches no live session actually is (SHIP-40).
//
// Two possibilities, and they are not the same event:
//
//   - **Nobody's token.** A guess, a stale value from a reinstalled app, a copy-paste. Refused,
//     and nothing else happens. Revoking on this path would hand anybody a way to end sessions
//     by guessing, which is a denial of service dressed as a security control.
//   - **A token this platform issued and has already rotated away.** Docs/07 §3: presenting one
//     twice invalidates the whole device session. Either the device replayed it — SHIP-50's
//     interceptor exists to stop that, and a client bug is not something the platform can tell
//     from a theft — or somebody else has a copy, in which case one of the two holders is about
//     to be signed out and both should be.
//
// The revocation is the point, so it must survive: this returns a refusal rather than an error,
// and the caller commits. See the note on [Service.Refresh].
func (s *Service) detectReuse(ctx context.Context, r db.Runner, hash string, now time.Time) (TokenPair, error, error) {
	sessionID, err := s.store.consumedRefreshTokenSession(ctx, r, hash)
	if err != nil {
		if isNoRows(err) {
			return TokenPair{}, ErrRefreshTokenInvalid, nil
		}
		return TokenPair{}, nil, fmt.Errorf("identity: reading a consumed refresh token: %w", err)
	}

	// Guarded on revoked_at IS NULL, so a token replayed a third time does not rewrite the
	// reason or move the instant the session ended.
	revoked, err := s.store.revokeDeviceSession(ctx, r, sessionID, now, revokedReasonTokenReused)
	if err != nil {
		return TokenPair{}, nil, err
	}

	// Logged rather than written to audit_log, and that is a scope decision worth naming:
	// SHIP-149 built the table and its append-only triggers but not the Go write helper
	// (Docs/11 §4), so there is nothing to call. This is the event that most deserves one, and
	// whoever finishes SHIP-149 should start here.
	//
	// The token is not logged, in any form. A log aggregator holding refresh tokens is the
	// exposure hashing the column exists to prevent, one system along.
	httpx.LoggerFrom(ctx).LogAttrs(ctx, slog.LevelWarn,
		"a consumed refresh token was presented; the device session has been revoked",
		slog.String("session_id", sessionID.String()),
		slog.Bool("first_detection", revoked))

	return TokenPair{}, ErrRefreshTokenReused, nil
}
