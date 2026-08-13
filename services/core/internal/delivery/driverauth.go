// SHIP-108: verifying the driver's job-scoped token, and the middleware a driver route is served
// behind.
//
// SHIP-107 signs the link and this is what spends it. Between them they are the pair Docs/11 §8 has
// always kept with one owner, because CLAUDE.md's invariant — "the driver's job-scoped token and the
// mobile auth token are separate systems; neither can be exchanged for the other" — is a statement
// about *both* verifiers and can only be proved by somebody holding both.
//
// # What is on the context afterwards is a [DriverGrant], and never an authctx.Subject
//
// cmd/api/driverauth.go carries this rule at the seam and it is worth restating where the code is.
// An [authctx.Subject] is what a signed-in account looks like to every domain in the service; a
// driver has no account, no role and no session, so resolving a driver token into one would be the
// exchange Docs/10 §5 forbids, performed by the platform on the driver's behalf. SHIP-107 made that
// structurally impossible rather than merely forbidden — the claim set has no `sub`, `role` or `sid`,
// so there is nothing to build a subject out of — and this file must not put the material back.
//
// The grant therefore lives here, in the consuming domain (Docs/06 §4.1), with an unexported context
// key and an unexported accessor. **Nothing outside this package can read a driver grant**, which is
// the strongest available form of "delivery is the only domain that serves a driver-token route".
//
// # Why the middleware is in the domain rather than in cmd/api
//
// It could be either — a guard is an ordinary `func(http.Handler) http.Handler` and the seam names
// no type, which is what let SHIP-15m leave the decision open. It is here because everything it does
// is this domain's: it answers with this domain's error codes, it reads this domain's path
// parameter, and it writes this domain's grant onto the context with this domain's key. Handlers
// live in the domain (Docs/10 §2.1) and a guard is the front half of one. What is left in cmd/api is
// the same three lines as newAccessTokenAuthenticator: build the keyset, build the verifier, hand
// back the middleware.
//
// It emphatically does **not** go in internal/httpx (SHIP-15c): every domain imports httpx, so one
// import of delivery from inside it would weld all eight to delivery through an edge that appears in
// no domain's own files.
//
// # The one-job check is in the middleware, and that is the design rather than a convenience
//
// The *Done when* is "grants access to exactly one job and nothing else". The token names a job; the
// request names a job in its path; something has to compare the two. Putting that comparison in each
// handler would make it a thing a handler can forget, and a handler that forgot it would answer
// somebody else's delivery with a 200 — no compile error, no failing test of its own, and no symptom
// until a driver held two links.
//
// So it is in the guard, which [attach] installs from the manifest's auth class. **A route cannot
// declare RequireDriverToken and skip the check**, because the check *is* the class. A route whose
// pattern carries no `{id}` fails closed rather than passing: there is no value to compare, so every
// request to it is refused, which is a route that visibly never works rather than one that quietly
// works too well.
//
// The blank line below keeps this a file note rather than a second package comment.

package delivery

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"

	"github.com/DulsaraNethmin/Shipper/services/core/internal/clock"
	"github.com/DulsaraNethmin/Shipper/services/core/internal/httpx"
)

// driverJobPathValue is the path parameter every driver-token route names its job with.
//
// One constant rather than a literal at two call sites, because the two are the guard and the route
// pattern, and a route that spelled it differently would be a route where the comparison silently
// had nothing to compare. [Handler.DriverJob]'s own tests pin the pattern.
const driverJobPathValue = "id"

// bearerScheme is the credential scheme, matched case-insensitively per RFC 9110 §11.1.
//
// A second copy of internal/httpx's, which is unexported there. Not a candidate for promotion: the
// header parsing httpx does is bound up with ResolveSubject's "never reject" posture, and this guard
// is the opposite — it rejects, per route, and has no interest in carrying a failure forward on the
// context for somebody else to report.
const bearerScheme = "bearer"

// DriverGrant is what a verified driver token grants: one job, for a while.
//
// **There is no user id, no role and no session id, and there is no method that produces one.** That
// is the invariant, expressed as a type: the fields simply are not here, so the conversion Docs/10 §5
// forbids has nothing to draw on. See the file note.
type DriverGrant struct {
	// JobID is the one job this link opens. It is inside the signature, so widening it means
	// re-signing the token, which means holding the key.
	JobID uuid.UUID

	// AssignmentID is the driver_assignments row the grant belongs to, and it is the driver's
	// whole identity — they have no account (000600, and [DriverClaims.AssignmentID]). It is what
	// a milestone recorded by a driver will be attributed to, and it is what [Service.Driver]
	// checks is still live.
	AssignmentID uuid.UUID

	// TokenID identifies this link among the several an assignment may have been issued.
	//
	// Carried rather than dropped because SHIP-109 reissues a link "invalidating the previous
	// one", and naming the outgoing one is the only way to express that. Nothing reads it yet.
	TokenID string

	// ExpiresAt is when the link stops working, in UTC.
	ExpiresAt time.Time
}

// driverGrantKey is the context key. Unexported, so the only way to populate it is the middleware in
// this file and the only thing that can read it is this package.
type driverGrantKey struct{}

func withDriverGrant(ctx context.Context, grant DriverGrant) context.Context {
	return context.WithValue(ctx, driverGrantKey{}, grant)
}

// driverGrantFrom is the verified grant this request is being served under, if any.
//
// Unexported deliberately, and it is the counterpart of authctx.Subject being infrastructure: a
// subject is read by every domain because every domain has authenticated users, and a driver grant is
// read by this one because this is the only domain that will ever serve a driver-token route.
// Exporting it would hand seven other domains the ability to act on a driver's link, which is exactly
// the widening the whole ticket is about.
func driverGrantFrom(ctx context.Context) (DriverGrant, bool) {
	grant, ok := ctx.Value(driverGrantKey{}).(DriverGrant)
	return grant, ok
}

// DriverTokenVerifier checks a driver's job-scoped token and reports what it grants.
//
// It holds the keyset and the clock and nothing else — no database. Whether the *assignment* behind a
// grant is still live is a question about a row, and it is asked by [Service.AssignmentFor] where
// there is a runner to ask it with. Keeping the two apart is what lets the guard run before any
// handler and without a connection.
type DriverTokenVerifier struct {
	keys  *Keyset
	clock clock.Clock
}

// NewDriverTokenVerifier builds the verifier.
//
// It returns an error rather than panicking for newAccessTokenAuthenticator's reason: the keyset
// comes from configuration, and a service that cannot verify any driver token should refuse to start
// beside the other configuration failures rather than answer 401 to every driver while reporting
// itself healthy.
func NewDriverTokenVerifier(keys *Keyset, clk clock.Clock) (*DriverTokenVerifier, error) {
	if keys == nil {
		return nil, fmt.Errorf("%w: no keyset", ErrInvalidKeyset)
	}
	if clk == nil {
		return nil, errors.New("delivery: a driver token verifier needs a clock (Docs/10 §6.3)")
	}
	return &DriverTokenVerifier{keys: keys, clock: clk}, nil
}

// Verify checks a token and returns what it grants.
//
// It is [parseDriverToken] plus the translation of the library's errors into this domain's, and it is
// the *only* exported way to verify a driver token. There is one parser, shared with the issuer, so a
// verifier that had drifted from the thing signing the tokens is not a state this package can reach —
// which is the defect that would matter most here and the reason SHIP-107 wrote the parser rather
// than leaving it to this ticket.
//
// The audience is pinned inside that parser and is checked with the signature, before any claim is
// read (Docs/10 §5). A mobile session token therefore never gets as far as producing a grant, **even
// when the two systems have been handed the same key material** — which is the case
// TestTheTwoTokenSystemsCannotBeExchanged constructs, because it is the only one where the audience
// is doing the work alone.
func (v *DriverTokenVerifier) Verify(raw string) (DriverGrant, error) {
	claims, err := parseDriverToken(v.keys, v.clock, raw)
	if err != nil {
		// The one distinction worth carrying to the wire. Everything else — a bad signature, an
		// unknown key identifier, `alg: none`, the mobile audience — becomes one refusal, because
		// no legitimate driver could do anything differently about any of them.
		if errors.Is(err, jwt.ErrTokenExpired) {
			return DriverGrant{}, fmt.Errorf("%w: %w", ErrDriverTokenExpired, err)
		}
		return DriverGrant{}, fmt.Errorf("%w: %w", ErrDriverTokenRejected, err)
	}

	jobID, err := claims.Job()
	if err != nil {
		return DriverGrant{}, err
	}
	assignmentID, err := claims.Assignment()
	if err != nil {
		return DriverGrant{}, err
	}

	// [DriverTokenIssuer.Issue] refuses to sign either of these as nil, so a token that verifies
	// and says nil is one this service could not have produced. Refused here as well rather than
	// assumed away: a grant over the nil job would compare equal to a path of all zeroes, and the
	// point of this whole file is that a grant names exactly one real job.
	if jobID == uuid.Nil || assignmentID == uuid.Nil {
		return DriverGrant{}, fmt.Errorf("delivery: a driver token granting the nil job or the nil "+
			"assignment: %w", ErrMalformedDriverToken)
	}

	return DriverGrant{
		JobID:        jobID,
		AssignmentID: assignmentID,
		TokenID:      claims.TokenID,
		ExpiresAt:    time.Unix(claims.ExpiresAt, 0).UTC(),
	}, nil
}

// RequireDriverToken is the middleware behind the manifest's RequireDriverToken auth class
// (SHIP-108).
//
// It is supplied to the router by cmd/api/driverauth.go, which is the whole of what SHIP-15m's seam
// asked of this ticket: filling that constructor in maps the class, and a route may then declare it.
// Until something did, a route declaring the class stopped the process at startup — absent rather
// than refusing, which is the distinction guardsFor exists to keep.
//
// Three things happen, in this order, and the order is the design:
//
//  1. the credential is read, and its absence is a different answer from its being bad;
//  2. it is verified — signature, algorithm, issuer, **audience** and expiry, all inside
//     [parseDriverToken], so no claim is ever read out of a token that has not passed every one;
//  3. the job the token names is compared with the job the request names, and a mismatch is
//     refused before the handler is reached.
//
// Step 3 is the *Done when*. It is here rather than in a handler so that forgetting it is not
// something a route can do: the comparison is the auth class, and a route either declares the class
// and gets it or does not declare it and is not a driver route at all.
//
// It panics on a nil verifier, exactly as httpx.ResolveSubject does on a nil Authenticator: this is
// called once from the composition root, and a guard with nothing behind it would make every driver
// route permanently unreachable while the service reported itself healthy.
func RequireDriverToken(verifier *DriverTokenVerifier) func(http.Handler) http.Handler {
	if verifier == nil {
		panic("delivery: RequireDriverToken needs a verifier; without one every driver route " +
			"would answer 401 forever while the service reported itself healthy")
	}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			grant, err := verifier.grantFor(r)
			if err != nil {
				refuseDriverLink(w, r, err)
				return
			}
			next.ServeHTTP(w, r.WithContext(withDriverGrant(r.Context(), grant)))
		})
	}
}

// grantFor is the whole decision: read the credential, verify it, and check it grants the job being
// asked for.
//
// # Why r.PathValue works here
//
// The guard is wrapped around the handler *before* the pair is registered on the mux (see attach in
// cmd/api/manifest.go), so what ServeMux dispatches to is this function — after it has matched the
// pattern and bound its path values. The same nesting is why delivery's other handlers can read
// {id} from inside RequireSubject.
//
// # A pattern with no {id} refuses everything, and that is the intended failure
//
// PathValue answers "" for a parameter the pattern does not declare, which is not a job identifier,
// so every request to such a route is refused. A route declaring this class without a job in its path
// is therefore a route that visibly never works — the loud direction — rather than one served with
// no scope check at all.
func (v *DriverTokenVerifier) grantFor(r *http.Request) (DriverGrant, error) {
	credential, presented := bearerCredential(r.Header.Get(httpx.HeaderAuthorization))
	if !presented {
		return DriverGrant{}, ErrNoDriverToken
	}

	grant, err := v.Verify(credential)
	if err != nil {
		return DriverGrant{}, err
	}

	requested := r.PathValue(driverJobPathValue)
	asked, err := uuid.Parse(requested)
	if err != nil {
		return DriverGrant{}, fmt.Errorf("delivery: %q is not a job a link could grant: %w",
			requested, ErrDriverTokenWrongJob)
	}

	if asked != grant.JobID {
		return DriverGrant{}, fmt.Errorf("delivery: a link for %s was presented on %s: %w",
			grant.JobID, asked, ErrDriverTokenWrongJob)
	}

	return grant, nil
}

// refuseDriverLink writes the refusal, in the error contract's shape.
//
// # What reaches the wire, and what deliberately does not
//
// Four sentinels, three answers. Expired is its own code because it is the one a driver can act on;
// "you presented nothing" and "what you presented is not good" are both `unauthenticated` with
// different copy, because the *code* a client branches on should not distinguish them; and the wrong
// job is `not_found`, because 403 would confirm that the other job exists to somebody holding a valid
// link and probing for their competitors' work.
//
// # The token is never logged
//
// It is the credential itself, so a log line carrying one is a credential in whatever collects the
// logs — [Handler.AssignDriver] makes the same point about the response. What is logged is the cause,
// at debug: an expired link is the ordinary consequence of a seven-day window, and logging it where
// anybody alerts would make the alert meaningless inside a week. The record carries the request id
// already, because httpx.LoggerFrom is bound to it (SHIP-14).
func refuseDriverLink(w http.ResponseWriter, r *http.Request, err error) {
	httpx.LoggerFrom(r.Context()).LogAttrs(r.Context(), slog.LevelDebug,
		"a driver link was refused", slog.String("error", err.Error()))

	switch {
	case errors.Is(err, ErrDriverTokenWrongJob):
		// No challenge header: nothing about the caller's credential is wrong, so inviting
		// them to present a better one would be misleading.
		httpx.WriteError(w, r, httpx.NewError(http.StatusNotFound, httpx.CodeNotFound,
			"No such delivery.").WithCause(err))

	case errors.Is(err, ErrDriverTokenExpired):
		w.Header().Set("WWW-Authenticate",
			`Bearer error="invalid_token", error_description="the delivery link has expired"`)
		httpx.WriteError(w, r, httpx.NewError(http.StatusUnauthorized, CodeDriverLinkExpired,
			"This delivery link has expired. Ask the transport provider to send you a new one.").
			WithCause(err))

	case errors.Is(err, ErrNoDriverToken):
		// RFC 9110 requires a challenge on a 401, and a bare scheme is the right one when
		// nothing was presented: there is no error to report yet.
		w.Header().Set("WWW-Authenticate", "Bearer")
		httpx.WriteError(w, r, httpx.NewError(http.StatusUnauthorized, httpx.CodeUnauthenticated,
			"Open this delivery from the link you were sent.").WithCause(err))

	default:
		w.Header().Set("WWW-Authenticate", `Bearer error="invalid_token"`)
		httpx.WriteError(w, r, httpx.NewError(http.StatusUnauthorized, httpx.CodeUnauthenticated,
			"This delivery link is not valid. Ask the transport provider to send you a new one.").
			WithCause(err))
	}
}

// bearerCredential extracts the token from the credential header's value.
//
// Matched case-insensitively because RFC 9110 §11.1 says the scheme is, and a client sending
// "bearer" is not wrong. Anything that is not a bearer credential is reported as none rather than as
// a bad one: this service issues nothing else, so a Basic header is a client talking to the wrong
// server and "your link is not valid" would send them looking in the wrong place.
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
