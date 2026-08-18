package main

import (
	"context"
	"fmt"

	"github.com/google/uuid"

	"github.com/DulsaraNethmin/Shipper/services/core/internal/db"
	"github.com/DulsaraNethmin/Shipper/services/core/internal/identity"
	"github.com/DulsaraNethmin/Shipper/services/core/internal/notifications"
)

// deletedAccountLookup implements notifications.Deletions by reading identity's
// account_deletion_requests (SHIP-171b).
//
// # Why this query is here and not in the domain
//
// `account_deletion_requests` belongs to internal/identity, in identity's migration block, and
// `notifications` may not import that domain — the boundary lint refuses it in both directions.
// The composition root is where a dependency between two domains is visible to anybody reading how
// the service is wired.
//
// **This is the fifth instance of the arrangement and it is written the same way deliberately** —
// [jobPartiesLookup] and [deviceSessionLookup] in this package, and cmd/api's SHIP-113 and SHIP-117
// lookups. A reader who has met one has met all five.
//
// # The state is the authority, and the pseudonym is not
//
// `completed` is written by the same transaction that replaces the person's name, address and
// number in five tables (internal/identity/pseudonymise.go), so there is no instant in which one is
// true and the other is not. It is a record of the **act**, which is why nothing here — and nothing
// in `internal/notifications` — reads an address or knows what a pseudonym looks like. The
// alternative was a test on `users.email` for identity.pseudonymPrefix, which would have made every
// caller depend on a rendering that file treats as a free choice, and would answer "does this look
// deleted" rather than "was this account deleted".
//
// The literal is identity's own exported constant rather than the string `'completed'` written a
// second time, which is Docs/10 §3.4's pairing rule applied across a composition root: a domain
// that renamed its state would break this build rather than quietly stop suppressing anything.
//
// The two **open** states are not deleted accounts and must not be treated as such. A person who
// has asked and is inside their thirty days is still a person: Docs/05 §3.1 gives them the window,
// and a platform that stopped notifying them at the request would deny them exactly the messages
// about the delivery that the window exists to let them finish.
//
// # No lock, and it reads inside the pass's transaction
//
// It takes a db.Runner so the read runs inside the consumer or dispatcher pass that is writing or
// claiming the rows beside it (Docs/10 §3.2). Nothing here decides anything about a request: an
// account pseudonymised in the instant between this read and that write means one notification is
// written that need not have been, and the next dispatch pass retires it — which is the same
// tolerance [jobPartiesLookup] records for an award.
type deletedAccountLookup struct{}

// DeletedAccounts returns the subset of ids whose accounts the platform has erased.
//
// An account with no request at all is simply absent from the answer, which is the same outcome as
// an open request and is the outcome this domain wants: address it normally. There is no error for
// an unknown identifier.
func (deletedAccountLookup) DeletedAccounts(
	ctx context.Context, r db.Runner, ids []uuid.UUID,
) (map[uuid.UUID]bool, error) {
	if len(ids) == 0 {
		return nil, nil
	}

	const q = `
		SELECT user_id
		FROM account_deletion_requests
		WHERE user_id = ANY($1)
		  AND state = $2`

	rows, err := r.Query(ctx, q, ids, identity.DeletionCompleted)
	if err != nil {
		return nil, fmt.Errorf("cmd/notifier: reading which of %d account(s) have been deleted: %w",
			len(ids), err)
	}
	defer rows.Close()

	deleted := make(map[uuid.UUID]bool, len(ids))
	for rows.Next() {
		var id uuid.UUID
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("cmd/notifier: reading a deleted account: %w", err)
		}
		deleted[id] = true
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("cmd/notifier: reading deleted accounts: %w", err)
	}
	return deleted, nil
}

// Compile-time proof that the adapter satisfies the port the domain declared, which is the only
// place in the build where that can be established.
var _ notifications.Deletions = deletedAccountLookup{}
