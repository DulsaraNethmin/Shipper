package httpx

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"sync"

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
//	ResolveSubject   — outermost. Reads the credential if one was presented. Never rejects.
//	ResolvePrincipal — beside it. The same, for callers who are not account holders.
//	Idempotent       — now has a caller to scope keys by.
//	RequireSubject   — per route, from the manifest's auth class. Rejects.
//
// # Why there is a second resolver (SHIP-147b)
//
// The platform has three credential systems and only one of them produces an [authctx.Subject].
// An administrator's console session and a driver's job-scoped token are deliberately not
// exchangeable for a user's — CLAUDE.md's third separation — so each is verified by a guard that
// puts its own grant on its own domain's private context key.
//
// **Those guards are per route, so they run inside Idempotent, after the scope is computed.** The
// same ordering that makes SHIP-44's fix work put every administrative key in
// `idem:v1:anonymous:<key>`, and Docs/11 §9 recorded the hole twice before anything closed it. A
// guard cannot fix it: at the moment the scope is computed there is nothing on the context to
// read, whatever the guard would later put there.
//
// ResolvePrincipal is the fix §9 named — a second group-wide resolver, running outside
// Idempotent, with [SubjectScope] widened to read either. It takes a [PrincipalResolver] declared
// here and satisfied by a closure over the domain, supplied in cmd/api, for the same reason
// [Authenticator] is: infrastructure may import no domain (SHIP-15c), and every domain imports
// this package.
//
// # Why it resolves lazily
//
// It installs the resolvers on the context and calls none of them. [SubjectScope] does, and only
// when it has no subject to use instead — which means only on a state-changing request carrying a
// valid idempotency key, because that is the only thing Idempotent computes a scope for.
//
// That is not a micro-optimisation. Verifying an administrator session is a database read
// (internal/admin argues the cost in full), a console makes several requests a second, and
// resolving eagerly would pay for one on every GET the scope is never asked about — and on every
// public request carrying an unrecognised bearer token, which is a read an unauthenticated caller
// gets to trigger. Resolving where the answer is used costs a read per administrative *write*.
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

// PrincipalKind names a credential system whose callers are not account holders.
//
// **The set is closed, and that is the point of the type.** The scope is rendered as
// `<kind>:<id>`, so a resolver free to choose its own kind could return `"user"` and land an
// administrator in a scope an account holder is already using. A string constant declared here
// cannot: `user` and `anonymous` are not in this set and cannot be added to it from cmd/api.
type PrincipalKind string

const (
	// PrincipalAdmin is an administrator's console session (SHIP-147, SHIP-147b).
	PrincipalAdmin PrincipalKind = "admin"

	// PrincipalDriver is the driver portal's job-scoped token (SHIP-107, SHIP-108).
	//
	// **Declared ahead of a resolver, deliberately.** Docs/11 §9 records the identical hole for
	// the driver — `internal/delivery/http.go` names it twice — and the mechanism SHIP-147b
	// built takes a second resolver without any further change to this package. The name is here
	// so that closing it is a cmd/api closure and a route file, rather than a shared-surface
	// edit somebody has to wait a wave for. Nothing populates it today.
	PrincipalDriver PrincipalKind = "driver"
)

// valid reports whether this is a kind the scope may be built from.
func (k PrincipalKind) valid() bool {
	switch k {
	case PrincipalAdmin, PrincipalDriver:
		return true
	}
	return false
}

// Principal is a verified caller who is not an [authctx.Subject].
//
// It carries what namespacing needs and nothing else: no role, no permissions, no session. This
// package must not become a place where a second kind of caller is described, because every
// domain sits on it — the grant itself stays in the domain that verified it, behind an unexported
// context key, which is what makes "admin is the only domain serving a RequireAdmin route" a fact
// about the build rather than a rule somebody follows.
type Principal struct {
	// Kind is which credential system this caller came from.
	Kind PrincipalKind

	// ID identifies the caller within Kind, and must be stable across a retry — it is what
	// makes a repeated request the *same* caller's repeat.
	ID string
}

// PrincipalResolver turns a presented credential into the caller it belongs to.
//
// Declared here, by the consumer, and satisfied in cmd/api by a closure over the domain that
// issued the credential — the same arrangement [Authenticator] has and for the same reason
// (Docs/06 §4.1, SHIP-15c). An error means "not one of mine", and the next resolver is tried.
type PrincipalResolver func(ctx context.Context, credential string) (Principal, error)

type principalKey struct{}

// principalLookup is the deferred resolution [ResolvePrincipal] puts on the context.
//
// sync.Once rather than a plain flag: one request is served by one goroutine today, and a
// memoised value read under -race by a handler that fans out would be a data race nobody would
// look for here.
type principalLookup struct {
	resolvers []PrincipalResolver
	once      sync.Once
	principal Principal
	found     bool
}

func (l *principalLookup) resolve(r *http.Request) (Principal, bool) {
	l.once.Do(func() {
		credential, presented := bearerCredential(r.Header.Get(HeaderAuthorization))
		if !presented || credential == "" {
			return
		}

		ctx := r.Context()
		for _, resolve := range l.resolvers {
			p, err := resolve(ctx, credential)
			if err != nil {
				// Debug, for ResolveSubject's reason: an administrator's console lapses
				// every morning, and logging that where anybody alerts would make the
				// alert meaningless inside a week. A credential presented on a public
				// route belongs to none of these systems and reaches here routinely.
				LoggerFrom(ctx).LogAttrs(ctx, slog.LevelDebug,
					"a credential was not attributed to a principal",
					slog.String("error", err.Error()))
				continue
			}
			if !p.Kind.valid() || p.ID == "" {
				// Error rather than debug, and it falls through to the anonymous scope:
				// this is a wiring defect in a resolver rather than anything the caller
				// did, and it silently reopens the hole SHIP-147b closed.
				LoggerFrom(ctx).LogAttrs(ctx, slog.LevelError,
					"a principal resolver returned a caller that cannot be namespaced",
					slog.String("kind", string(p.Kind)))
				continue
			}

			l.principal, l.found = p, true
			return
		}
	})
	return l.principal, l.found
}

// ResolvePrincipal makes non-subject callers available to [SubjectScope] (SHIP-147b).
//
// Applied to the version group **outside** [Idempotent], beside [ResolveSubject]. It writes no
// response, rejects nothing and — see the file comment — calls no resolver until a scope is
// actually wanted. Resolvers are tried in order and the first that answers wins.
//
// A nil resolver is a panic rather than a skip, exactly as [ResolveSubject] treats a nil
// [Authenticator]: it is called once from the composition root, and the failure it would
// otherwise produce is every administrator's idempotency key silently sharing one namespace while
// the service reports itself healthy. **No resolvers at all is legitimate** and means the
// deployment has no credential system but the access token.
func ResolvePrincipal(resolvers ...PrincipalResolver) func(http.Handler) http.Handler {
	for _, resolve := range resolvers {
		if resolve == nil {
			panic("httpx: ResolvePrincipal was given a nil PrincipalResolver; every caller it " +
				"was meant to namespace would fall back to the shared anonymous scope")
		}
	}

	if len(resolvers) == 0 {
		return func(next http.Handler) http.Handler { return next }
	}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			lookup := &principalLookup{resolvers: resolvers}
			next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), principalKey{}, lookup)))
		})
	}
}

// SubjectScope namespaces idempotency keys by the caller they belong to (SHIP-15, SHIP-44,
// SHIP-147b).
//
// This is what closes the hole the idempotency middleware shipped with. Every key used to land in
// `idem:v1:anonymous:<key>`, so a client that guessed another client's key was handed that
// client's stored response body — an entirely different person's job, address or bid.
//
// # Two callers, three answers
//
// An account holder scopes on their user identifier. A caller whose credential belongs to one of
// the other systems — an administrator today — scopes on `<kind>:<id>` from a [Principal], which
// is what SHIP-147b added: until then an administrator produced no subject, so every
// administrative key landed in the shared anonymous scope and two administrators could read each
// other's stored responses.
//
// **The scope is the account rather than the session, on both sides.** One user signed in on two
// devices shares a scope, and one administrator with two browsers open shares a scope, because
// idempotency answers "has this action already been performed" and the answer does not change
// with the device it was performed from. Two *different* administrators never share one.
//
// # Anonymous is still a shared scope, and still safe
//
// For the reason Docs/11 §6 sets out: replayOrRefuse fingerprints method, path and body, so
// reading a stranger's stored response requires sending their exact request — which, on every
// public state-changing route, means already holding the secret material in their body.
func SubjectScope(r *http.Request) string {
	if s, ok := authctx.SubjectFrom(r.Context()); ok && s.UserID != "" {
		return "user:" + s.UserID
	}
	if lookup, ok := r.Context().Value(principalKey{}).(*principalLookup); ok {
		if p, found := lookup.resolve(r); found {
			return string(p.Kind) + ":" + p.ID
		}
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
