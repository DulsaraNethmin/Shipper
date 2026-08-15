// SHIP-147: verifying an administrator's session, and the middleware every administrative route is
// served behind.
//
// credentials.go issues the credential and this is what spends it. Between them they are one half
// of CLAUDE.md's third separation — "administrator sign-in is a separate system that a user token
// can never reach" — and the half that can be proved from inside this package. The other half is
// `identity.AccessTokenVerifier` refusing an administrator credential, which is proved in cmd/api
// where both verifiers are visible at once, exactly as SHIP-108 proved the driver pair.
//
// # What is on the context afterwards is a [Grant], and never an authctx.Subject
//
// cmd/api/adminauth.go carries this rule at the seam and it is worth restating where the code is.
// An [authctx.Subject] is what a signed-in *account holder* looks like to every domain in the
// service. An administrator is not one: there is no `users` row, no `device_sessions` row, no role
// from `ck_users_role` and no access token. Producing a Subject from an administrator session would
// be that exchange performed by the platform on the administrator's behalf, and every domain
// reading a subject would then be reading a moderator as though a customer had signed in.
//
// It is made structurally impossible rather than merely forbidden, in the two ways available:
//
//   - [Grant] has no `UserID` and no `Role` from identity's vocabulary, so there is nothing to
//     build a Subject out of;
//   - the context key and the accessor below are **unexported**, so nothing outside this package
//     can read an administrator grant at all. "admin is the only domain that serves a RequireAdmin
//     route" is therefore a fact about the build rather than a rule somebody follows.
//
// That is delivery/driverauth.go's arrangement, deliberately copied. It was right there and the
// reasoning transfers without modification.
//
// # Why the middleware is here rather than in cmd/api, and emphatically not in internal/httpx
//
// Everything it does is this domain's: it answers with this domain's error codes, it reads this
// domain's session table, and it writes this domain's grant onto the context with this domain's
// key. Handlers live in the domain (Docs/10 §2.1) and a guard is the front half of one. What is
// left in cmd/api is three statements — build the authenticator, hand back the middleware.
//
// It does **not** go in internal/httpx (SHIP-15c): every domain imports httpx, so one import of
// `admin` from inside it would weld all eight to admin through an edge that appears in no domain's
// own files.
//
// # Why the guard reads the database on every request
//
// This is the cost the design chose, and 000801's header argues it in full. In short: a signed
// token is valid until it expires whatever the database says, and both of the properties this
// credential exists to have — immediate revocation, and a role change that takes effect at once
// (SHIP-148) — are properties of a row rather than of a signature. One indexed read per
// administrative request buys both, and the admin panel is a browser talking to one service rather
// than a phone on a mobile network.
//
// The blank line below keeps this a file note rather than a second package comment.

package admin

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/DulsaraNethmin/Shipper/services/core/internal/clock"
	"github.com/DulsaraNethmin/Shipper/services/core/internal/httpx"
)

// bearerScheme is the credential scheme, matched case-insensitively per RFC 9110 §11.1.
//
// A third copy of the same three lines, after internal/httpx's and internal/delivery's, and it is
// not a candidate for promotion for the reason delivery gives: httpx's parsing is bound up with
// ResolveSubject's "never reject" posture, and a guard is the opposite — it rejects, per route, and
// carries nothing forward for somebody else to report.
const bearerScheme = "bearer"

// Grant is what a verified administrator session grants.
//
// **There is no user id, no `identity.Role`, no session id from `device_sessions`, and there is no
// method that produces any of them.** That is the invariant, expressed as a type — see the file
// note.
type Grant struct {
	// Administrator is who is acting, read fresh on this request.
	//
	// Fresh matters: it is what makes a role change and a disabling take effect at once rather
	// than at the next expiry, and it is what SHIP-150's audit entries will name. A copy taken at
	// sign-in and carried in a token would be a permission set as stale as the credential.
	Administrator Administrator

	// SessionID is which sign-in this request belongs to.
	//
	// Carried so that an audit entry can name the session as well as the account, and so that
	// "sign this session out" (SHIP-147) has something to name. A session is not a person: an
	// administrator signed in on two machines has two.
	SessionID uuid.UUID

	// ExpiresAt is when this session stops working, whichever limit comes first, in UTC.
	ExpiresAt time.Time
}

// grantKey is the context key. Unexported, so the only way to populate it is the middleware in this
// file and the only thing that can read it is this package.
type grantKey struct{}

func withGrant(ctx context.Context, g Grant) context.Context {
	return context.WithValue(ctx, grantKey{}, g)
}

// grantFrom is the verified grant this request is being served under, if any.
//
// Unexported deliberately, and it is the counterpart of authctx.Subject being infrastructure: a
// subject is read by every domain because every domain has authenticated users, and an
// administrator grant is read by this one because this is the only domain that will ever serve a
// RequireAdmin route.
func grantFrom(ctx context.Context) (Grant, bool) {
	g, ok := ctx.Value(grantKey{}).(Grant)
	return g, ok
}

// mustGrant is the grant a RequireAdmin handler is guaranteed to have.
//
// It returns an error rather than panicking, and the error becomes an opaque 500: reaching a
// RequireAdmin handler with no grant means the route was declared with the wrong auth class, which
// is a wiring defect the caller can do nothing about and must not be reported as a 401 — that would
// invite them to present a better credential for a problem no credential can fix.
func mustGrant(ctx context.Context) (Grant, error) {
	g, ok := grantFrom(ctx)
	if !ok {
		return Grant{}, httpx.NewError(http.StatusInternalServerError, httpx.CodeInternal,
			"Something went wrong at our end.").
			WithCause(errors.New("admin: a handler ran with no administrator grant on the " +
				"context, so its route is not declared RequireAdmin"))
	}
	return g, nil
}

// Authenticator resolves a presented administrator credential into a [Grant].
//
// It holds the pool and the clock and nothing else — no hasher and no rate limiter, because
// resolving a session is a lookup by digest rather than a password verification and there is
// nothing here to guess at. That is why cmd/api's `newAdminGuard` needs no Redis client and can be
// satisfied by the three parameters SHIP-15r's seam already gives it.
type Authenticator struct {
	pool  *pgxpool.Pool
	clock clock.Clock
	store postgresStore
}

// NewAuthenticator builds the session verifier.
//
// **The pool may be nil and that is not an error.** main.go warns and starts when PostgreSQL is
// unreachable, because a rolling deployment during a failover must not take every instance down at
// once, so the guard has to survive holding a nil pool — it answers 503 for as long as it lasts,
// exactly as every handler built from Deps.Pool already does. A nil *clock* is an error, because a
// verifier with no clock cannot tell a live session from a lapsed one and would serve both.
func NewAuthenticator(pool *pgxpool.Pool, clk clock.Clock) (*Authenticator, error) {
	if clk == nil {
		return nil, errors.New("admin: an administrator session verifier needs a clock (Docs/10 §6.3)")
	}
	return &Authenticator{pool: pool, clock: clk}, nil
}

// Resolve verifies a presented credential and reports what it grants.
//
// # The order is the design
//
//  1. the digest is looked up. An unknown one is [ErrAdminSessionInvalid] and nothing else happens;
//  2. the session's own standing is checked — revoked, idle out, capped out;
//  3. the *account's* standing is checked, so that disabling an administrator ends their live
//     sessions without a second write;
//  4. only then is the idle window slid forward.
//
// Step 4 last is what stops a lapsed session being kept alive by the act of presenting it, which is
// the one bug a sliding window has available to it.
//
// # Two failures the caller can tell apart, and one answer on the wire
//
// [ErrAdminSessionExpired] is separated from [ErrAdminSessionInvalid] because an administrator
// whose console timed out gets different copy from one whose credential is not recognised. The
// *disabled account* case is deliberately folded into "invalid" on the wire and kept distinct in
// Go: the caller holds only a token, which may have been taken, and identity.Service.Refresh takes
// exactly this position for exactly this reason. Sign-in discloses a disabled account, because
// there the password has just been proved.
func (a *Authenticator) Resolve(ctx context.Context, presented string) (Grant, error) {
	if strings.TrimSpace(presented) == "" {
		return Grant{}, ErrNoAdminSession
	}
	if a.pool == nil {
		return Grant{}, ErrAdminUnavailable
	}

	session, administrator, err := a.store.sessionByTokenHash(ctx, a.pool, hashSessionToken(presented))
	switch {
	case isNoRows(err):
		return Grant{}, ErrAdminSessionInvalid
	case err != nil:
		return Grant{}, fmt.Errorf("admin: reading an administrator session: %w", err)
	}

	now := a.clock.Now().UTC()
	switch {
	case !session.RevokedAt.IsZero():
		// Signed out, from here or from another tab. Reported as invalid rather than expired:
		// "your session ended" is what the caller is told either way, and only a lapse is worth
		// distinguishing, because only a lapse is the platform's doing.
		return Grant{}, ErrAdminSessionInvalid
	case !now.Before(session.ExpiresAt()):
		return Grant{}, ErrAdminSessionExpired
	case !administrator.CanSignIn():
		return Grant{}, ErrAdminAccountDisabled
	}

	a.slide(ctx, session, now)

	return Grant{
		Administrator: administrator,
		SessionID:     session.ID,
		ExpiresAt:     session.ExpiresAt(),
	}, nil
}

// slide moves the idle window forward, and is deliberately allowed to fail.
//
// # Why a failure is logged rather than returned
//
// The request has already been authorised. Turning a write failure into a refusal would take a
// verified administrator and answer 503 to them because the platform could not record that they
// were here — the wrong direction for a control whose purpose is to end *inactive* sessions.
// SHIP-41's chargeSignInFailure makes the same trade in the other direction and for the same
// reason: the decision has been made, and the bookkeeping must not overturn it.
//
// # Why it does not write on every request
//
// A console makes several requests a second while somebody is working, and an UPDATE per request is
// write amplification for no gain: the window is thirty minutes and nothing depends on it being
// accurate to the second. [slideGranularity] is the smallest advance worth a write, so a burst of
// twenty parallel requests produces one.
//
// # Why there is no lock
//
// Two browser tabs sliding at once both write approximately the same value and the later one wins,
// which is the correct outcome — this is not a rotation, there is no token being consumed, and
// there is nothing a lost update could lose beyond a few seconds of window. `device_sessions` takes
// `FOR UPDATE` because rotation must have exactly one winner (SHIP-39); nothing here must.
func (a *Authenticator) slide(ctx context.Context, session Session, now time.Time) {
	target := now.Add(idleWindow)
	if target.After(session.AbsoluteExpiresAt) {
		// Clamped, so that the slide never asks for more than the cap allows. 000801's
		// ck_admin_sessions_idle_within_absolute refuses the write either way, which is what
		// makes the cap the database's rather than this function's.
		target = session.AbsoluteExpiresAt
	}
	if target.Sub(session.IdleExpiresAt) < slideGranularity {
		return
	}

	if err := a.store.slideSession(ctx, a.pool, session.ID, target, now); err != nil {
		httpx.LoggerFrom(ctx).LogAttrs(ctx, slog.LevelWarn,
			"an administrator session's idle window could not be extended",
			slog.String("session_id", session.ID.String()),
			slog.String("error", err.Error()))
	}
}

// RequireAdmin is the middleware behind the manifest's RequireAdmin auth class (SHIP-147).
//
// It is supplied to the router by cmd/api/adminauth.go, which is the whole of what SHIP-15r's seam
// asked of this ticket: filling that constructor in maps the class, and a route may then declare
// it. Until something did, a route declaring the class stopped the process at startup — absent
// rather than refusing, which is the distinction guardsFor exists to keep.
//
// It panics on a nil authenticator, exactly as delivery.RequireDriverToken and httpx.ResolveSubject
// do: this is called once from the composition root, and a guard with nothing behind it would make
// every administrative route permanently unreachable while the service reported itself healthy.
func RequireAdmin(auth *Authenticator) func(http.Handler) http.Handler {
	if auth == nil {
		panic("admin: RequireAdmin needs an authenticator; without one every administrative " +
			"route would answer 401 forever while the service reported itself healthy")
	}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			credential, presented := bearerCredential(r.Header.Get(httpx.HeaderAuthorization))
			if !presented {
				refuseAdmin(w, r, ErrNoAdminSession)
				return
			}

			grant, err := auth.Resolve(r.Context(), credential)
			if err != nil {
				refuseAdmin(w, r, err)
				return
			}

			next.ServeHTTP(w, r.WithContext(withGrant(r.Context(), grant)))
		})
	}
}

// refuseAdmin writes the refusal, in the error contract's shape.
//
// # What reaches the wire, and what deliberately does not
//
// Five sentinels, three answers. A lapsed session gets its own code because it is the one an
// administrator can act on without wondering whether something is broken; a disabled account is
// folded into "not valid" because the caller holds only a token; and an unreachable database is a
// 503 rather than a 401, because telling somebody their credential is bad when the platform simply
// cannot read it sends them to reset a password that was never wrong.
//
// # The credential is never logged
//
// It is the credential itself, so a log line carrying one is a live administrator session in
// whatever collects the logs. What is logged is the cause, at debug: a console left open overnight
// lapses every morning, and logging that where anybody alerts would make the alert meaningless
// inside a week. The record carries the request id already (SHIP-14).
func refuseAdmin(w http.ResponseWriter, r *http.Request, err error) {
	httpx.LoggerFrom(r.Context()).LogAttrs(r.Context(), slog.LevelDebug,
		"an administrator session was refused", slog.String("error", err.Error()))

	switch {
	case errors.Is(err, ErrAdminUnavailable):
		httpx.WriteError(w, r, httpx.NewError(http.StatusServiceUnavailable, httpx.CodeUnavailable,
			"This cannot be completed right now. Try again shortly.").WithCause(err))

	case errors.Is(err, ErrAdminSessionExpired):
		w.Header().Set("WWW-Authenticate",
			`Bearer error="invalid_token", error_description="the administrator session has expired"`)
		httpx.WriteError(w, r, httpx.NewError(http.StatusUnauthorized, CodeAdminSessionExpired,
			"Your administrator session has ended. Sign in again.").WithCause(err))

	case errors.Is(err, ErrNoAdminSession):
		// RFC 9110 requires a challenge on a 401, and a bare scheme is the right one when
		// nothing was presented: there is no error to report yet.
		w.Header().Set("WWW-Authenticate", "Bearer")
		httpx.WriteError(w, r, httpx.NewError(http.StatusUnauthorized, httpx.CodeUnauthenticated,
			"Sign in to the administration console.").WithCause(err))

	default:
		w.Header().Set("WWW-Authenticate", `Bearer error="invalid_token"`)
		httpx.WriteError(w, r, httpx.NewError(http.StatusUnauthorized, httpx.CodeUnauthenticated,
			"That administrator credential is not valid. Sign in again.").WithCause(err))
	}
}

// bearerCredential extracts the token from the credential header's value.
//
// Anything that is not a bearer credential is reported as none rather than as a bad one: this
// service issues nothing else, so a Basic header is a client talking to the wrong server and "your
// session is not valid" would send them looking in the wrong place.
//
// "Bearer" with nothing after it is presented-but-empty rather than absent, for the same reason
// httpx's copy makes that distinction: a client that named the scheme meant to send something.
func bearerCredential(header string) (credential string, presented bool) {
	value := strings.TrimSpace(header)
	if value == "" {
		return "", false
	}

	scheme, rest, _ := strings.Cut(value, " ")
	if !strings.EqualFold(scheme, bearerScheme) {
		return "", false
	}
	return strings.TrimSpace(rest), true
}
