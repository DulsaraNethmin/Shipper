package identity

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
