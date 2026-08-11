package httpx

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"strings"

	"github.com/DulsaraNethmin/Shipper/services/core/internal/authctx"
)

// Authentication (SHIP-44).
//
// # Why this is two middlewares rather than one
//
// The obvious shape is one middleware that verifies a token and rejects when it cannot. It does
// not fit, and the reason is the idempotency scope — which is the whole security purpose of this
// ticket.
//
// httpx.Idempotent wraps the entire /v1 group, deliberately, so a state-changing endpoint cannot
// be added without it (Docs/10 §4.2). It needs the caller's identity to namespace keys, because
// a shared `anonymous` scope means a client that guesses another client's key gets that client's
// response body. But authentication is per-route — the manifest declares an auth class per
// endpoint — so a single per-route middleware would run *inside* Idempotent and produce the
// subject after the scope had already been computed.
//
// So identity is resolved once, at the group level, outside Idempotent:
//
//	ResolveSubject  — outermost. Reads the credential if one was presented. Never rejects.
//	Idempotent      — now has a subject to scope keys by.
//	RequireSubject  — per route, from the manifest's auth class. Rejects.
//
// # Why ResolveSubject never rejects
//
// It is tempting to refuse a bad credential immediately: a client that presents one is claiming
// to be somebody, and a false claim is an error whatever the route.
//
// That breaks refresh. POST /v1/auth/refresh is public and is called by exactly the client whose
// access token has just expired — and a client that attaches its stale access token to every
// request, which is the normal shape, would be locked out of the endpoint that fixes it. The
// endpoint that recovers from an expired credential cannot itself require a valid one.
//
// So a failed resolution records *why* on the context and carries on. RequireSubject reports it
// where refusing is correct, and a public route never notices.

// HeaderAuthorization is where a bearer token arrives. Spelled by RFC 9110 and not by us.
const HeaderAuthorization = "Authorization" // spelling:ok — HTTP header name, RFC 9110

// bearerScheme is the credential scheme, matched case-insensitively per RFC 9110 §11.1.
const bearerScheme = "bearer"

const (
	// CodeTokenExpired means the credential was genuine and has expired. The client should
	// refresh and retry rather than sign the user out.
	//
	// Distinct from CodeUnauthenticated because it is the one authentication failure a client
	// acts on differently, and it discloses nothing: `exp` sits in the token payload the
	// client already holds and can decode without any key.
	CodeTokenExpired Code = "token_expired"
)

// Authenticator turns a credential into the caller it belongs to.
//
// Declared here, by the consumer, and satisfied by a closure over internal/identity supplied in
// cmd/api — the same rule domains follow for their adapters (Docs/06 §4.1). httpx importing
// identity would be infrastructure importing a domain, which the boundary lint refuses since
// SHIP-15c: every domain imports httpx, so that one edge would weld all eight to identity.
type Authenticator func(ctx context.Context, credential string) (authctx.Subject, error)

// ErrCredentialExpired tells [ResolveSubject] that the credential was genuine and has lapsed, so
// that [RequireSubject] can answer with [CodeTokenExpired].
//
// Declared here rather than imported from identity for the reason above: this package cannot
// name identity's sentinels. The translation happens in cmd/api, which knows both.
var ErrCredentialExpired = errors.New("httpx: the credential has expired")

// authFailure is why resolution did not produce a subject.
type authFailure int

const (
	// authNone means no credential was presented at all.
	authNone authFailure = iota

	// authExpired means one was, and it had lapsed.
	authExpired

	// authRejected means one was, and it was not acceptable. Deliberately undifferentiated:
	// a caller can do nothing different about a bad signature than about a bad audience, and
	// saying which check failed is free help to somebody probing.
	authRejected
)

type authFailureKey struct{}

// ResolveSubject reads the credential, if there is one, and puts the caller on the context.
//
// It never writes a response. See the file comment: the endpoint that recovers from an expired
// credential cannot itself require a valid one, and this middleware wraps public and protected
// routes alike.
func ResolveSubject(verify Authenticator) func(http.Handler) http.Handler {
	if verify == nil {
		panic("httpx: ResolveSubject needs an Authenticator; without one every protected route " +
			"would be permanently unreachable")
	}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			credential, presented := bearerCredential(r.Header.Get(HeaderAuthorization))
			if !presented {
				next.ServeHTTP(w, withFailure(r, authNone))
				return
			}

			subject, err := verify(r.Context(), credential)
			if err != nil {
				failure := authRejected
				if errors.Is(err, ErrCredentialExpired) {
					failure = authExpired
				}

				// Logged at debug rather than warn: an expired token is the ordinary
				// consequence of a fifteen-minute TTL, and logging it at a level anybody
				// alerts on would make the alert meaningless within a day. The cause is
				// kept, because the response deliberately withholds it.
				LoggerFrom(r.Context()).LogAttrs(r.Context(), slog.LevelDebug,
					"credential not accepted", slog.String("error", err.Error()))

				next.ServeHTTP(w, withFailure(r, failure))
				return
			}

			next.ServeHTTP(w, r.WithContext(authctx.WithSubject(r.Context(), subject)))
		})
	}
}

// RequireSubject refuses a request that [ResolveSubject] could not attribute to anybody.
//
// Applied per route from the manifest's auth class, so "which endpoints need a credential" stays
// answerable by reading the route table rather than by tracing middleware.
func RequireSubject() func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if _, ok := authctx.SubjectFrom(r.Context()); ok {
				next.ServeHTTP(w, r)
				return
			}

			switch failureFrom(r.Context()) {
			case authExpired:
				w.Header().Set("WWW-Authenticate",
					`Bearer error="invalid_token", error_description="the access token has expired"`)
				WriteError(w, r, NewError(http.StatusUnauthorized, CodeTokenExpired,
					"Your session has expired. Refresh it and try again."))

			case authRejected:
				w.Header().Set("WWW-Authenticate", `Bearer error="invalid_token"`)
				WriteError(w, r, NewError(http.StatusUnauthorized, CodeUnauthenticated,
					"That sign-in is no longer valid. Sign in again."))

			default:
				// RFC 9110 requires a challenge on a 401, and a bare scheme is the right
				// one when nothing was presented: there is no error to report yet.
				w.Header().Set("WWW-Authenticate", "Bearer")
				WriteError(w, r, NewError(http.StatusUnauthorized, CodeUnauthenticated,
					"Sign in to continue."))
			}
		})
	}
}

// SubjectScope namespaces idempotency keys by the caller they belong to (SHIP-15, SHIP-44).
//
// This is what closes the hole the idempotency middleware shipped with. Every key used to land in
// `idem:v1:anonymous:<key>`, so a client that guessed another client's key was handed that
// client's stored response body — an entirely different person's job, address or bid.
//
// Anonymous callers still share a scope, and that is safe for the reason Docs/11 §6 sets out:
// replayOrRefuse fingerprints method, path and body, so reading a stranger's stored response
// requires sending their exact request — which, on every public state-changing route, means
// already holding the secret material in their body.
func SubjectScope(r *http.Request) string {
	if s, ok := authctx.SubjectFrom(r.Context()); ok && s.UserID != "" {
		return "user:" + s.UserID
	}
	return "anonymous"
}

// bearerCredential extracts the token from the credential header's value.
//
// The scheme is matched case-insensitively because RFC 9110 §11.1 says it is case-insensitive,
// and a client sending "bearer" is not wrong. Anything that is not a bearer credential is
// reported as no credential rather than as a bad one: this service issues nothing else, so a
// Basic header is a client talking to the wrong server, and answering "your token is invalid"
// would send them looking in the wrong place.
func bearerCredential(header string) (credential string, presented bool) {
	value := strings.TrimSpace(header)
	if value == "" {
		return "", false
	}

	scheme, rest, _ := strings.Cut(value, " ")
	if !strings.EqualFold(scheme, bearerScheme) {
		return "", false
	}

	// "Bearer" with nothing after it is reported as presented-but-empty rather than absent: a
	// client that names the scheme meant to send a token, and telling it "sign in" when the
	// real problem is that it sent an empty string sends it looking in the wrong place.
	return strings.TrimSpace(rest), true
}

func withFailure(r *http.Request, failure authFailure) *http.Request {
	return r.WithContext(context.WithValue(r.Context(), authFailureKey{}, failure))
}

func failureFrom(ctx context.Context) authFailure {
	failure, _ := ctx.Value(authFailureKey{}).(authFailure)
	return failure
}

func init() {
	registerProtocolCode(CodeTokenExpired,
		"The access token was genuine and has expired. Refresh it and retry; do not sign the user out.")
}
