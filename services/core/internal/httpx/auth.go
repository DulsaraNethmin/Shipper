package httpx

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
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
//	ResolveSubject — outermost. Reads the credential if one was presented. Never rejects.
//	Idempotent     — now has a caller to scope keys by.
//	RequireSubject — per route, from the manifest's auth class. Rejects.
//
// # The other two credential systems, and why their scope is the credential (SHIP-147b)
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
// The first form of the fix was a second group-wide resolver beside [ResolveSubject], turning a
// presented credential into the administrator it named. **It shipped, and it broke a documented
// retry contract**, which is the argument the current shape is built on and worth stating in full
// because it is not obvious:
//
//	DELETE /v1/admin/sessions/current ends the session it is presented with. On the retry
//	that a dropped connection produces, the credential is dead — so a resolver that reads it
//	fails, the scope falls back to `anonymous`, the middleware misses the response it stored
//	under the administrator's scope, and the request reaches the guard, which refuses it. A
//	sign-out that succeeded reports 401 to the retry that could not hear the 204.
//
// Generalised: **a scope computed by resolving a credential is not stable across that
// credential's own lifecycle, and a retry is exactly the window in which the lifecycle moves.**
// Sign-out is the sharp case because it is the endpoint whose *purpose* is to invalidate what it
// was called with, but a session that lapses between an attempt and its retry has the same shape.
// Any resolution that can fail can fail *between* the two halves of one logical request.
//
// So the scope for a non-subject caller is a digest of the credential itself. It is stable across
// revocation and expiry, it separates two administrators exactly — two sessions are two
// credentials — it needs no database read at all, and it closes the driver half of Docs/11 §9 in
// the same stroke without a second mechanism, because a job-scoped token is a bearer credential
// like any other.
//
// The cost, stated rather than hidden: one administrator signed in twice has two scopes, so the
// same key reused across their two browsers is two actions rather than one. That is a scope
// narrower than the account, and narrower is the safe direction — a replay hands back a stored
// response body, so the scope is a read boundary and not only a deduplication key. It is also
// unobservable in practice: an idempotency key is generated per action by the client, so two
// browsers colliding on one is not a thing an honest client does.
//
// # Why the subject still wins, and why it is not a digest
//
// An account holder keeps `user:<id>`, which is the account rather than the credential, because
// **identity has a refresh and the other two systems do not**: a mobile client whose access token
// expires mid-retry obtains a new one and retries with it, and scoping on the credential would
// make that retry a different caller and execute the request twice. That is the duplicate bid the
// idempotency invariant exists to prevent. An administrator cannot refresh — sign-out is terminal
// — so the same reasoning points the other way for them.
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

// credentialScopeKind names the namespace a bearer credential that produced no [authctx.Subject]
// is scoped into.
//
// It is deliberately one namespace rather than one per credential system. Telling an
// administrator's session from a driver's job token from a stale access token needs a lookup in
// the domain that issued it, and the whole point of the digest is that the scope must not depend
// on a lookup that can fail — see the file comment. Two callers are separated by the digest, not
// by the label.
const credentialScopeKind = "credential"

// credentialScopeSalt domain-separates this digest from every other SHA-256 of the same input.
//
// `admin_sessions.token_hash` and identity's refresh-token column both store `sha256(credential)`.
// Without the salt this scope would render that exact value into a Redis key — putting the
// database's stored verifier into the cache, into a log line, and into the reach of anything that
// can run `SCAN`. It does not make the credential recoverable either way; it stops one system's
// stored secret becoming another system's routine output.
const credentialScopeSalt = "shipper:idempotency-scope:v1\x00"

// credentialScope names a caller by what they presented rather than by who it belongs to.
//
// The digest is of the raw credential, so it is stable for exactly as long as the client keeps
// sending the same one — across the session's revocation and across its expiry, which is the
// property the retry contract needs and the reason this is not a resolved identifier.
func credentialScope(credential string) string {
	sum := sha256.Sum256([]byte(credentialScopeSalt + credential))
	return credentialScopeKind + ":" + hex.EncodeToString(sum[:])
}

// SubjectScope namespaces idempotency keys by the caller they belong to (SHIP-15, SHIP-44,
// SHIP-147b).
//
// This is what closes the hole the idempotency middleware shipped with. Every key used to land in
// `idem:v1:anonymous:<key>`, so a client that guessed another client's key was handed that
// client's stored response body — an entirely different person's job, address or bid.
//
// # Three answers
//
//	user:<id>          an account holder, from the verified [authctx.Subject]
//	credential:<hash>  a bearer credential that produced no subject — an administrator's
//	                   console session, a driver's job-scoped token, or an access token this
//	                   service will not accept
//	anonymous          nothing was presented
//
// The middle one is SHIP-147b. Until it existed every administrative key landed in the shared
// anonymous scope, so two administrators could read each other's stored responses; the file
// comment argues why it is a digest of the credential rather than the administrator it names.
//
// # Anonymous is still a shared scope, and still safe
//
// For the reason Docs/11 §6 sets out: replayOrRefuse fingerprints method, path and body, so
// reading a stranger's stored response requires sending their exact request — which, on every
// public state-changing route, means already holding the secret material in their body. Every
// caller who presents *something* now leaves that scope, so what is left in it is the traffic
// that argument was written about.
//
// # A caller can choose their own scope, and that is not a weakness
//
// Anybody may invent a bearer token and get a private namespace of their own. They cannot reach
// anyone else's with it — a scope nobody else can compute is a scope nobody else is in — and they
// could already occupy unlimited entries in `anonymous` by varying the key. What no caller can do
// is land in a namespace somebody else is using: `user:` is written from a verified subject and
// from nothing else, and no presented value reaches a scope unhashed.
func SubjectScope(r *http.Request) string {
	if s, ok := authctx.SubjectFrom(r.Context()); ok && s.UserID != "" {
		return "user:" + s.UserID
	}
	if credential, presented := bearerCredential(r.Header.Get(HeaderAuthorization)); presented && credential != "" {
		return credentialScope(credential)
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
