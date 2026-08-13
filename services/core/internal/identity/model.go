package identity

import (
	"time"

	"github.com/google/uuid"
)

// Role is which of the two shells an account uses, and it is fixed at registration
// (SHIP-45).
//
// There is no third value and no transition between the two. A provider's bid history and a
// customer's job history are not interchangeable, so changing role means a new account — and
// administrators sign in through a separate system entirely (SHIP-147), which is why "admin" is
// not here and is refused by ck_users_role in the database.
type Role string

const (
	RoleCustomer Role = "customer"
	RoleProvider Role = "provider"
)

// Valid reports whether r is one of the two roles.
//
// The values are exactly the strings ck_users_role permits in 000002_users, so that the Go
// constants and the database constraint cannot drift into disagreeing about what a role is.
func (r Role) Valid() bool {
	switch r {
	case RoleCustomer, RoleProvider:
		return true
	}
	return false
}

func (r Role) String() string { return string(r) }

// Status is whether the account may be used at all.
//
// It is not provider verification, which is a separate eligibility decision with five states of
// its own, owned by the profiles domain (Docs/04 §4). This says whether anyone may sign in.
//
// The values are exactly the strings ck_users_status permits in 000002_users. There is no
// 'deleted': Docs/05 §3.1 requires the transaction record to survive, so SHIP-171 pseudonymises
// rather than removes.
type Status string

const (
	StatusActive     Status = "active"
	StatusRestricted Status = "restricted"
	StatusSuspended  Status = "suspended"
)

// Valid reports whether s is one of the three account states.
func (s Status) Valid() bool {
	switch s {
	case StatusActive, StatusRestricted, StatusSuspended:
		return true
	}
	return false
}

func (s Status) String() string { return string(s) }

// User is one account, as the users table holds it.
//
// The password hash is deliberately not a field. Nothing outside this package has a use for it,
// and a struct that carries it is a struct that eventually gets logged or serialised — the
// verification handlers return a User straight to the caller.
type User struct {
	ID    uuid.UUID
	Email string
	Phone string

	Role   Role
	Status Status

	// EmailVerifiedAt and PhoneVerifiedAt record *when*, not *whether*.
	//
	// 000002_users argues the choice: a boolean answers "is it verified" and a timestamp
	// answers that and also "since when", which is what a support conversation and an audit
	// trail both actually ask. Nil means not yet.
	EmailVerifiedAt *time.Time
	PhoneVerifiedAt *time.Time

	CreatedAt time.Time
}

// EmailVerified reports whether the address has been confirmed (SHIP-33).
func (u User) EmailVerified() bool { return u.EmailVerifiedAt != nil }

// PhoneVerified reports whether the number has been confirmed (SHIP-36).
func (u User) PhoneVerified() bool { return u.PhoneVerifiedAt != nil }

// CanSignIn reports whether the account may hold a session at all (SHIP-39).
//
// Suspended is the only state that refuses. Restricted narrows what an account may *do* — which
// is the domain's decision at the point of doing it — and an account that cannot sign in cannot
// be told why it is restricted, or read the messages that say so.
//
// It is stated here rather than as a status comparison at each call site, because sign-in
// (SHIP-41), refresh (SHIP-39) and every later gate have to agree about it: three copies is how
// one path keeps honouring a session the other two have stopped issuing.
func (u User) CanSignIn() bool { return u.Status != StatusSuspended }

// CanPublish reports whether Docs/04 §2's baseline is met: both contact channels verified.
//
// It is stated here, once, rather than as two nil checks at each call site — SHIP-63 is the
// ticket that enforces it at publication, and the rule it enforces should not be re-derived
// there from the columns.
func (u User) CanPublish() bool {
	return u.Status == StatusActive && u.EmailVerified() && u.PhoneVerified()
}
