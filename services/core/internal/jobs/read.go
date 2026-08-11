package jobs

import (
	"context"
	"fmt"

	"github.com/google/uuid"

	"github.com/DulsaraNethmin/Shipper/services/core/internal/db"
)

// Reading a job (SHIP-65).
//
// # Everything in this file is the owning customer's view
//
// There is no provider-facing read here and there must not be one added to it. Docs/01 §4.3 keeps
// the customer's maximum budget private from providers — not as an amount, a band, or a "budget
// supplied" flag — and SHIP-67 adds the column with the serialisation test that proves it cannot
// leak. SHIP-82's feed and SHIP-83's provider detail get their own functions and their own
// response type, because one shape with a redaction step somebody has to remember is exactly the
// arrangement that rule is hardest to keep with.
//
// **The budget column does not exist yet**, so SHIP-65's *Done when* — "returns full job including
// budget" — is met but for that field. It was deliberately not added here: Docs/11 §8 makes
// SHIP-67 and SHIP-83 single-owner precisely so the field and the proof land together, and a field
// that arrives before its proof is the one arrangement worse than a field that arrives late.

// Job is one job, for the customer who owns it (SHIP-65).
//
// Ownership is checked here rather than in the query's WHERE clause, deliberately: a
// `WHERE id = $1 AND customer_id = $2` that returns nothing cannot say whether the job is
// somebody else's or nobody's, and the two are different defects even though they are one answer
// on the wire. Keeping them apart lets a test assert "the stranger was refused" and fail if the
// job silently stopped existing instead.
//
// No lock and no transaction. This is a read, and [postgresStore.job] takes the row without
// FOR UPDATE for the reason recorded there.
func (s *Service) Job(ctx context.Context, r db.Runner, customerID, jobID uuid.UUID) (Job, error) {
	job, err := s.store.job(ctx, r, jobID)
	if err != nil {
		return Job{}, err
	}
	if job.CustomerID != customerID {
		return Job{}, fmt.Errorf("jobs: %s does not belong to %s: %w", jobID, customerID, ErrNotJobOwner)
	}
	return job, nil
}
