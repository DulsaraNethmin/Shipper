package jobs

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/DulsaraNethmin/Shipper/services/core/internal/db"
	"github.com/DulsaraNethmin/Shipper/services/core/internal/pagination"
)

// Reading a job, and reading a customer's own jobs (SHIP-65, SHIP-66).
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

// JobQuery is what a customer is asking of their own job list (SHIP-66).
//
// The zero value is a valid query: every job the customer owns, newest first, one default page.
type JobQuery struct {
	// Status narrows the list to one status. The zero value means every status.
	//
	// One rather than a set, because SHIP-66 asks for "filterable by status" and the client
	// that wants several — SHIP-76 shows the customer's jobs grouped — is better served by
	// reading the list once and grouping it than by one request per group. Widening this to
	// several is additive if a screen ever needs it.
	Status Status

	// Limit is how many jobs to return. Zero means the default, and anything above the
	// maximum is clamped rather than refused: a client asking for more than the platform will
	// give is asking for a page, not making a mistake.
	Limit int

	// After is the position to continue from — the last job of the previous page. The zero
	// value starts at the newest.
	After JobCursor
}

// JobCursor is a position in a customer's job list.
//
// Two fields rather than one, and both are load-bearing. `created_at` is what the list is ordered
// by and it is not unique — the job wizard writes a draft and the customer creates another in the
// same millisecond — so a cursor carrying only the timestamp repeats or skips a job at exactly the
// page boundary, which is the failure keyset pagination exists to avoid arriving by another route.
// The id breaks the tie and is unique by definition.
//
// A value rather than an encoded string, because the domain has no business knowing how a cursor
// is spelled on the wire. internal/pagination owns the encoding and http.go is where the two meet.
type JobCursor struct {
	CreatedAt time.Time
	ID        uuid.UUID
}

// IsZero reports whether the query starts at the newest job.
func (c JobCursor) IsZero() bool { return c.ID == uuid.Nil && c.CreatedAt.IsZero() }

// JobPage is one page of a customer's jobs.
type JobPage struct {
	Jobs []Job

	// Next is where the following page starts, and is the zero cursor on the last page.
	Next JobCursor

	// HasMore says whether asking again would return anything. Carried rather than inferred
	// from Next, because "no cursor" is also what the *first* request looks like and a caller
	// should not have to know the difference.
	HasMore bool
}

// Jobs is the customer's own jobs, newest first (SHIP-66).
//
// Newest first because that is the order a customer thinks about their jobs in, and because
// idx_jobs_customer (customer_id, created_at DESC) already serves it — 000400 created that index
// for exactly this read, so the list needs no index of its own.
//
// Keyset rather than offset, per Docs/10 §4.5. Offset duplicates and skips rows when the underlying
// set changes between pages, and a customer creating a job while paging through their list is not
// hypothetical.
//
// **A customer sees only their own jobs, and there is no argument for anything else.** The customer
// id comes from the token rather than from the request, so there is no parameter to widen and no
// filter to forget: a job belonging to somebody else is not refused here, it is never selected.
func (s *Service) Jobs(ctx context.Context, r db.Runner, customerID uuid.UUID, q JobQuery) (JobPage, error) {
	if customerID == uuid.Nil {
		return JobPage{}, fmt.Errorf("jobs: a job list names no customer: %w", ErrNotCustomer)
	}
	if q.Status != "" && !q.Status.Valid() {
		return JobPage{}, fmt.Errorf("jobs: %q: %w", q.Status, ErrInvalidStatus)
	}

	limit := q.Limit
	switch {
	case limit <= 0:
		limit = pagination.DefaultLimit
	case limit > pagination.MaxLimit:
		limit = pagination.MaxLimit
	}

	// One more row than was asked for, which answers "is there another page" without a second
	// query and without counting the whole set. The extra is dropped below and never reaches a
	// caller.
	found, err := s.store.jobsFor(ctx, r, customerID, q.Status, q.After, limit+1)
	if err != nil {
		return JobPage{}, err
	}

	page := JobPage{Jobs: found}
	if len(found) > limit {
		page.Jobs = found[:limit]

		last := page.Jobs[limit-1]
		page.Next = JobCursor{CreatedAt: last.CreatedAt, ID: last.ID}
		page.HasMore = true
	}
	return page, nil
}
