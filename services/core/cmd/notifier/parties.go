package main

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"

	"github.com/DulsaraNethmin/Shipper/services/core/internal/db"
	"github.com/DulsaraNethmin/Shipper/services/core/internal/notifications"
)

// jobPartiesLookup implements notifications.Parties by reading the job and its accepted bid.
//
// # Why this query is here and not in the domain
//
// It spans `jobs` and `bidding`, and `notifications` may import neither — the boundary lint refuses
// it and doc.go states the rule from the other side: this domain "never reaches back into jobs,
// bidding, or delivery to ask what happened". The composition root is where a dependency between
// domains is visible to anyone reading how the service is wired, rather than buried in
// notifications/postgres.go where `jobs` and `bids` would read as tables notifications owns.
//
// **This is the third instance of the same arrangement**, and it is written the same way
// deliberately: cmd/api's jobPartiesLookup (SHIP-113) answers which side of a job an account is on,
// and its exceptionQueueLookup (SHIP-117) reads delivery's evidence tables for admin. A reader who
// has met one has met all three.
//
// It is a separate type from cmd/api's rather than a shared one, because sharing would need a
// package both binaries import and neither an entrypoint nor a domain is that. The statement is
// four lines and the duplication is visible in review; a shared "queries" package would be a place
// for cross-domain joins to accumulate, which is the thing the boundary rules exist to prevent.
//
// # 'Accepted' is the awarded provider, and the index is why that is safe to assume
//
// uq_bids_one_accepted_per_job is partial on `status = 'Accepted'`, so the LEFT JOIN cannot multiply
// rows however many bids a job carries (SHIP-80, SHIP-91). Docs/02 §3 — "awarding a job atomically
// marks one bid accepted and all others closed" — is the statement it enforces.
type jobPartiesLookup struct{}

// PartiesOn is the customer who owns the job and the provider awarded it, if either exists.
//
// No lock. Nothing here decides anything about the job: the caller is writing rows in another table
// about an event that has already happened, and a job awarded in the instant between this read and
// that write simply means the next event about it resolves the new provider.
//
// A job that does not exist is found=false with a nil error. That is a real answer — Docs/05 §3.1
// has SHIP-171 pseudonymising rather than deleting, but a notification consumer catching up after
// an outage may legitimately meet an event about a job that has since gone — and the consumer
// treats it as nobody to tell rather than as a failure that parks the partition.
func (jobPartiesLookup) PartiesOn(
	ctx context.Context, r db.Runner, jobID uuid.UUID,
) (customer, provider uuid.UUID, found bool, err error) {
	const q = `
		SELECT j.customer_id, b.provider_id
		FROM jobs j
		LEFT JOIN bids b ON b.job_id = j.id AND b.status = 'Accepted'
		WHERE j.id = $1`

	var awarded *uuid.UUID
	switch err := r.QueryRow(ctx, q, jobID).Scan(&customer, &awarded); {
	case errors.Is(err, db.ErrNoRows):
		return uuid.Nil, uuid.Nil, false, nil
	case err != nil:
		return uuid.Nil, uuid.Nil, false,
			fmt.Errorf("cmd/notifier: reading the parties on %s: %w", jobID, err)
	}

	if awarded != nil {
		provider = *awarded
	}
	return customer, provider, true, nil
}

// Compile-time proof that the adapter satisfies the port the domain declared, which is the only
// place in the build where that can be established — notifications names no type here and this type
// names no interface there, so nothing else links them.
var _ notifications.Parties = jobPartiesLookup{}
