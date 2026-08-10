package identity

import "testing"

// TestRoleMatchesTheDatabaseConstraint pins the two strings ck_users_role permits in
// 000002_users.
//
// Docs/10 §3.4 asks for a test that reads the constraint out of pg_constraint and compares it to
// the Go constants. That belongs with the migration that owns the constraint, and this package's
// tests deliberately need no database — so what is checked here is the narrower half: that the
// constants say exactly what the migration was written against. The values are one-word strings
// in two places, and this is what stops the second one drifting.
func TestRoleMatchesTheDatabaseConstraint(t *testing.T) {
	if string(RoleCustomer) != "customer" {
		t.Errorf("RoleCustomer is %q; ck_users_role permits 'customer'", RoleCustomer)
	}
	if string(RoleProvider) != "provider" {
		t.Errorf("RoleProvider is %q; ck_users_role permits 'provider'", RoleProvider)
	}

	for _, valid := range []Role{RoleCustomer, RoleProvider} {
		if !valid.Valid() {
			t.Errorf("%q is not reported as a role", valid)
		}
	}

	// 'admin' in particular: administrators sign in through a separate system (SHIP-147), and
	// the users table refuses the value outright.
	for _, invalid := range []Role{"", "admin", "Customer", "PROVIDER", "driver"} {
		if invalid.Valid() {
			t.Errorf("%q was accepted as a role", invalid)
		}
	}
}
