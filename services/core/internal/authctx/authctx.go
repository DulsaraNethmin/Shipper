// Package authctx carries the authenticated subject on the request context.
//
// It exists as infrastructure rather than as part of the identity domain, and that placement
// is load-bearing rather than tidy-minded. Every domain needs to know who is calling: jobs
// checks ownership, bidding checks that the awarding user is the customer, delivery checks
// that a milestone came from the awarded provider. If the accessor lived in internal/identity,
// each of those would import identity, and the import lint in SHIP-11 fails a domain that
// imports a domain — so the first authenticated endpoint would either break the build or
// force the rule to be widened. Infrastructure is what every domain is allowed to sit on.
//
// The subject is put here by the authentication middleware (SHIP-44) and nowhere else. The
// context key is unexported, so there is no way to forge one from outside this package.
//
// Nothing here decides anything. Docs/07 §3 puts every authorisation decision on the
// platform, and this package only reports who the platform decided the caller is; whether
// that caller may act is the domain's judgement to make.
package authctx

import "context"

// Role is the account role, fixed at registration and immutable thereafter (SHIP-45).
type Role string

const (
	// RoleCustomer publishes jobs and awards bids.
	RoleCustomer Role = "customer"

	// RoleProvider bids on jobs and delivers them.
	RoleProvider Role = "provider"
)

// Valid reports whether r is a role this service issues.
//
// Administrators are deliberately absent: Docs/06 §5.2 and SHIP-147 make admin sign-in a
// separate system that cannot be reached with a user token, so an admin identity never
// arrives through this path.
func (r Role) Valid() bool { return r == RoleCustomer || r == RoleProvider }

// Subject is the authenticated caller.
//
// It holds what authorisation decisions are made from and nothing else. In particular it does
// not carry verification state: that changes during a session, and a customer who was
// unverified when they signed in must not be able to publish for the next fifteen minutes on
// the strength of a stale claim (Docs/10 §5). SHIP-63 reads verification when it needs it.
type Subject struct {
	// UserID identifies the account.
	UserID string

	// Role is what the account is, not what it may do.
	Role Role

	// SessionID identifies the device session the token was issued against, so a single
	// device can be revoked without signing the user out everywhere (Docs/06 §5.2).
	SessionID string
}

type contextKey int

const subjectKey contextKey = iota

// WithSubject returns a context carrying s.
//
// The authentication middleware is the only intended caller. Tests construct one directly,
// which is the point of it being writable at all.
func WithSubject(ctx context.Context, s Subject) context.Context {
	return context.WithValue(ctx, subjectKey, s)
}

// SubjectFrom returns the authenticated subject, and reports whether there was one.
//
// The boolean is not decoration. A handler on a public route legitimately has no subject, and
// callers must not be able to mistake the zero value for a real account — an empty UserID
// that compared equal to a job's owner would be a serious defect.
func SubjectFrom(ctx context.Context) (Subject, bool) {
	s, ok := ctx.Value(subjectKey).(Subject)
	return s, ok
}

// MustSubject returns the authenticated subject, panicking when there is none.
//
// It is for handlers registered with an auth class that guarantees a subject. Reaching it
// without one means a route was declared public and written as though it were protected,
// which is a wiring defect rather than a request the caller got wrong — a panic surfaces it
// in the first test that exercises the route, and httpx.Recover turns it into a 500 rather
// than something worse if it ever escapes.
func MustSubject(ctx context.Context) Subject {
	s, ok := SubjectFrom(ctx)
	if !ok {
		panic("authctx: no subject on the context — the route's auth class should have guaranteed one")
	}
	return s
}
