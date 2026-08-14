package migrations_test

import (
	"testing"

	"github.com/DulsaraNethmin/Shipper/services/core/internal/admin"
	"github.com/DulsaraNethmin/Shipper/services/core/internal/testsupport/pgtest"
)

// SHIP-151's pairing, and the reason it is worth a test rather than a comment.
//
// [admin.UserStanding] is a **second Go copy** of the standings `ck_users_status` permits —
// `identity.Status` is the first. It has to be: `admin` may not import `identity` and the boundary
// lint refuses it in both directions. Docs/10 §3.4 is what makes a copy safe, and this is that
// mechanism applied.
//
// The failure it catches is specific and quiet. `status` is a *filter* on the account search, so a
// standing the database has and this list does not means a support engineer typing it gets a
// validation error for a standing that exists — and a standing in this list that the column refuses
// means a filter that matches nothing while looking as though it worked. Neither shows up anywhere
// else: the search would return an empty page, which is the same answer as "no such accounts".

// TestUserStandingConstraintMatchesTheGoConstants.
func TestUserStandingConstraintMatchesTheGoConstants(t *testing.T) {
	pool := pgtest.DB(t)

	inDatabase := constraintLiterals(t, pool, "ck_users_status")

	for _, standing := range admin.UserStandings {
		if !inDatabase[standing.String()] {
			t.Errorf("admin.UserStandings has %q and ck_users_status refuses it, so filtering "+
				"the account search on it can only ever return nothing", standing)
		}
		delete(inDatabase, standing.String())
	}
	for leftover := range inDatabase {
		t.Errorf("ck_users_status accepts %q and admin.UserStandings has no such standing, so "+
			"an administrator cannot search for the accounts that are in it", leftover)
	}
}
