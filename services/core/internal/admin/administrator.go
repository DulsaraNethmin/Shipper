// SHIP-147: what an administrator account is, and what an administrator session is.
//
// The types only. What they mean is in three other places and each is deliberately separate:
// credentials.go signs one in, adminauth.go verifies one on a request, and permissions.go
// (SHIP-148) says what the role below actually permits.
//
// # Nothing here has a counterpart in internal/identity, and that is the ticket
//
// The *Done when* is "admin sign-in is independent and cannot be reached with a user token". The
// most reliable way to hold that is for the two systems to have no type, no table and no key
// material in common — so [Administrator] is not a `User`, [Session] is not a `device_sessions`
// row, and neither can be built from the other because neither package can see the other. `admin`
// may not import `identity`, and the boundary lint refuses it.
//
// What the two *do* share is `internal/passwords`, which is infrastructure both sit on. That is
// the opposite of a shared credential: it is one argon2id implementation rather than two that
// agree by comment (SHIP-15r, Docs/10 §3.4).
//
// The blank line below keeps this a file note rather than a second package comment.

package admin

import (
	"slices"
	"time"

	"github.com/google/uuid"
)

// Role is the named bundle of permissions an administrator holds.
//
// A name in the database and a set in Go. The reasoning for the split is on [Permission]
// (SHIP-148): what a role grants is a security control read on every request, not data an operator
// edits, so it lives in a reviewable Go table rather than in a grant table somebody can UPDATE.
//
// The three below are ordered **least privileged first**, which is the order 000801's
// `ck_admin_users_role` lists them in and the order [Roles] holds. That is not decoration: it is
// how a reader tells at a glance which end of the list the default is at.
type Role string

const (
	// RoleSupport is the minimum, and it is what an account with no role stated becomes.
	//
	// Read-only across the console: search users and jobs, read the queues, read a dispute, read
	// the audit trail. Everything it holds is a permission to *look*, which is the whole of
	// Docs/01 §4.6's first two capabilities and none of the rest.
	//
	// 000801 makes this the column default for the reason Docs/04 §9 asks for least-privilege
	// administrative access: an INSERT that forgets the role should produce the account that can
	// do least, never the one that can do most.
	RoleSupport Role = "support"

	// RoleModerator is support plus the acting permissions of Docs/04 §6's step 4: unpublish a
	// job, restrict an account, record a verification decision, resolve a dispute, write an
	// internal note.
	//
	// It deliberately cannot manage administrators. Somebody who can create an administrator can
	// create one with any role, which makes every other boundary in this file advisory.
	RoleModerator Role = "moderator"

	// RoleOwner is moderator plus `admins.manage`.
	//
	// The role that can grant roles, and therefore the one there should be very few of. It is not
	// "all permissions" by construction — it holds a listed set like the other two, so a
	// permission added later reaches it only by being written into [rolePermissions].
	RoleOwner Role = "owner"
)

// Roles is every role, least privileged first.
//
// Ordered rather than a set because the order is meaningful (see [Role]) and because it is what
// TestAdminRoleConstraintMatchesTheGoConstants compares against `ck_admin_users_role`. Docs/10 §3.4
// requires that pairing in both directions: a role the database accepts and Go has no bundle for is
// an administrator whose permissions are undefined, and one Go knows and the database refuses is a
// role nobody can be given.
var Roles = []Role{RoleSupport, RoleModerator, RoleOwner}

// Valid reports whether r is one of [Roles].
func (r Role) Valid() bool { return slices.Contains(Roles, r) }

// String is the stored form, which is also the wire form.
//
// No translation table, for the reason delivery.ProofExceptionReason gives about its own: no
// document fixes these strings, so a second spelling would be a mapping with nothing on the other
// side of it.
func (r Role) String() string { return string(r) }

// Status is whether an administrator account may be used.
//
// Two values rather than users' four. An administrator is not verified, is not restricted, and has
// no commercial standing — the only question is whether the account is still in service.
type Status string

const (
	// StatusActive is an account in service.
	StatusActive Status = "active"

	// StatusDisabled is an account that may no longer sign in.
	//
	// Disabled rather than deleted, because 000003's audit entries name this account for ever and
	// Docs/05 §3.1 keeps that history after its subject is gone.
	//
	// Disabling has to end the live sessions as well, and it does so without a second write:
	// [Authenticator.Resolve] reads the account on every request and refuses one that is not
	// active. An account that cannot sign in and can still act is not disabled, and a credential
	// read from a row is what makes that true at once rather than at the next expiry.
	StatusDisabled Status = "disabled"
)

// Statuses is every status.
//
// Paired with `ck_admin_users_status` by TestAdminStatusConstraintMatchesTheGoConstants, per
// Docs/10 §3.4.
var Statuses = []Status{StatusActive, StatusDisabled}

// Valid reports whether s is one of [Statuses].
func (s Status) Valid() bool { return slices.Contains(Statuses, s) }

// String is the stored form.
func (s Status) String() string { return string(s) }

// Administrator is one row of `admin_users`, without its credential.
//
// **There is no password hash field and there will not be one.** The hash is read inside
// credentials.go, compared, and discarded; a struct carrying it would travel into a handler, into a
// log record built from `%+v`, and eventually into a response. `identity.User` takes the same
// position and for the same reason.
type Administrator struct {
	ID    uuid.UUID
	Email string
	Name  string

	Role   Role
	Status Status

	CreatedAt time.Time
	UpdatedAt time.Time
}

// CanSignIn reports whether this account may start a session.
//
// A method rather than a comparison at each call site, so that a third status — should one ever be
// argued for — is one edit rather than a search for every `== StatusActive`. `identity.User` has
// the same method for the same reason.
func (a Administrator) CanSignIn() bool { return a.Status == StatusActive }

// Session is one row of `admin_sessions`, without its credential.
//
// The token is never here either: what is stored is a hash of it, and what is returned to the
// caller at sign-in is the token itself, once, from [Credentials.SignIn]. A session that could be
// read back into a usable credential would make every support query and every backup a way in.
type Session struct {
	ID      uuid.UUID
	AdminID uuid.UUID

	// IdleExpiresAt slides as the console is used. AbsoluteExpiresAt does not.
	//
	// Two fields rather than one because they answer different questions and lapse for different
	// reasons: the first says "nobody has been here for a while", the second says "this session
	// has been open long enough". [Session.ExpiresAt] is the one a client is told about.
	IdleExpiresAt     time.Time
	AbsoluteExpiresAt time.Time

	LastUsedAt time.Time

	// RevokedAt is zero while the session is live.
	RevokedAt time.Time

	CreatedAt time.Time
}

// ExpiresAt is when this session stops working, whichever limit comes first.
//
// The value a client is given, because a console that displayed the idle expiry alone would promise
// a session it may not get: an administrator two minutes from the absolute cap does not have
// another thirty minutes of idle window however busy they are. 000801's
// `ck_admin_sessions_idle_within_absolute` makes the two agree in the database; this makes them
// agree in the answer.
func (s Session) ExpiresAt() time.Time {
	if s.AbsoluteExpiresAt.Before(s.IdleExpiresAt) {
		return s.AbsoluteExpiresAt
	}
	return s.IdleExpiresAt
}

// Live reports whether the session may still be presented at the given instant.
//
// Revoked, idle out, or capped out — three ways to be finished and one answer, because a caller
// deciding whether to serve a request has no use for the distinction. [Authenticator.Resolve] keeps
// the distinction for the answer it writes, where an administrator whose session timed out is told
// something different from one whose session was signed out from another tab.
func (s Session) Live(now time.Time) bool {
	return s.RevokedAt.IsZero() && now.Before(s.ExpiresAt())
}
