// SHIP-161: the two statements that change an account's standing.
//
// Methods on the same unexported [postgresStore] as the rest of this domain's SQL (Docs/10 §2.2).
//
// # This domain writes `users`, having read it since SHIP-151
//
// postgres_users.go settled the reading half: `users` is in the shared migration block *because*
// most of the service reads it, `jobs`, `fleet` and `delivery` each select from it in their own
// stores, and what belongs in cmd/api is a statement spanning **two other domains'** tables. A write
// is a longer step and takes the same argument plus one more.
//
// **Account standing is the administrator's column, not registration's.** `000002`s own comment says
// so — "this is whether the account may be used at all", set apart there from provider verification,
// which it names as the profiles domain's with its own five states. `identity` writes `status` once,
// at registration, and never again; nothing in that domain changes it. So a port would be a hole
// punched through `identity` for a column it does not own, and the composition root would hold a
// statement neither domain wanted.
//
// **What `identity` does own is the enforcement**, and this ticket leaves it entirely alone.
// `User.CanSignIn` refuses a suspended account, the session service refuses one at refresh, and
// `User.CanBid` refuses a restricted provider. This writes the column those three read.
//
// # There is no DELETE here, and there will not be
//
// Docs/05 §3.1 keeps records beyond the account, and Docs/01 §5.2's in-app account deletion is a
// separate obligation with its own ticket and its own reconciliation against audit retention.
// Suspension is not deletion and must not become a way to perform one.
//
// The blank line below keeps this a file note rather than a second package comment.

package admin

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"

	"github.com/DulsaraNethmin/Shipper/services/core/internal/db"
)

// lockUserStanding reads an account's standing and holds the row until the transaction ends.
//
// # FOR UPDATE, and it is load-bearing rather than cautious
//
// The standing that comes back is written into an append-only audit entry as the `from` of the
// change. Without the lock two administrators acting at once both read `active`, both write their
// own standing, and the trail records two moves *from active* — one of which never happened, in a
// table with no way to correct it. The lock makes the second read the first one's result.
//
// It also makes [ErrStandingUnchanged] mean something: a no-op check against an unlocked read is a
// check against a value that may already be stale by the time the UPDATE runs.
//
// # NOWAIT is deliberately not used
//
// The contended case is two moderators on the same account, which is seconds apart at worst and
// resolves correctly by waiting. NOWAIT would turn that into a refusal a person cannot act on.
//
// found is false when there is no such account, which the caller turns into [ErrUserNotFound]. **Not
// a disclosure decision**, unlike the identical answer [JobParties] gives: the caller is an
// administrator holding `users.restrict` and every account is theirs to act on.
func (postgresStore) lockUserStanding(
	ctx context.Context,
	r db.Runner,
	userID uuid.UUID,
) (UserStanding, bool, error) {
	const q = `SELECT status FROM users WHERE id = $1 FOR UPDATE`

	var standing UserStanding
	err := r.QueryRow(ctx, q, userID).Scan(&standing)
	switch {
	case errors.Is(err, db.ErrNoRows):
		return "", false, nil
	case err != nil:
		return "", false, fmt.Errorf("admin: reading the standing of %s: %w", userID, err)
	}
	return standing, true, nil
}

// setUserStanding writes the new standing.
//
// # updated_at is left to the trigger, unlike created_at on an audit entry
//
// `000002` has `users_set_updated_at`, a BEFORE UPDATE trigger that stamps the column. Docs/11 §9's
// rule is *one row, one clock* — if the domain injects a clock, the column takes its value from the
// domain — and this row is the exception the rule allows for: `users` is written by `identity` at
// registration and by this statement, and the two would otherwise disagree about which clock stamps
// a shared column. The trigger is what makes it one clock for the whole table.
//
// **The audit entry is the row that has to sort**, and that one does take the injected clock
// ([Auditor.Record]). A support timeline is read from `audit_log`, not from `users.updated_at`.
//
// # No RETURNING and no row count check
//
// The row was locked by [postgresStore.lockUserStanding] in this transaction, so it exists and
// cannot have moved. A second existence check here would be a check against the same lock.
func (postgresStore) setUserStanding(
	ctx context.Context,
	r db.Runner,
	userID uuid.UUID,
	standing UserStanding,
) error {
	// No CHECK is restated here. `ck_users_status` is the constraint, [UserStanding.Valid] is the
	// Go half, and migrations/admin_user_search_test.go pairs them in both directions (Docs/10
	// §3.4) — so a standing this package could name and the database would refuse fails a test
	// rather than a request.
	const q = `UPDATE users SET status = $2 WHERE id = $1`

	if _, err := r.Exec(ctx, q, userID, standing.String()); err != nil {
		return fmt.Errorf("admin: setting the standing of %s to %s: %w", userID, standing, err)
	}
	return nil
}
