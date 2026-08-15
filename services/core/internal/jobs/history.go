package jobs

import (
	"context"
	"fmt"

	"github.com/google/uuid"

	"github.com/DulsaraNethmin/Shipper/services/core/internal/db"
)

// A job's status history, served to its parties (SHIP-65a).
//
// # What this closes
//
// `job_status_history` has recorded the actor, the reason and both clocks since SHIP-57a,
// append-only by trigger, and [Service.History] has read it in Go since the same ticket. Nothing
// exposed it over HTTP: `GET /v1/jobs/{id}` answers the `Job` schema, which is
// `additionalProperties: false` and carries one `status` with a `created_at` and an `updated_at`.
// So SHIP-77's "full job detail with status timeline" derived its timeline from the current status
// and refused to date a step it could not date, which is what has kept it in `Docs/11` §4 longer
// than any other ticket. This is the endpoint that row has been waiting for.
//
// # Two parties, decided from the database rather than from a role claim
//
// The customer who owns the job, and a provider holding a bid on it. Being either is a *fact about
// this job*; `role: provider` is an assertion the platform made about an account at registration
// and says nothing about whether that account has anything to do with this job. `delivery`'s
// [Service.partyTo] equivalent takes the same reading and `Docs/07` §3 requires it: the platform
// decides, and it decides from rows.
//
// **Not the awarded provider — any provider who bid.** A losing bidder priced this job, and what
// became of it is the answer to the only question they have about it. The narrower rule would also
// have made this endpoint useless for the case it most matters in: a job cancelled while three
// providers had live offers on it has no awarded provider at all.
//
// # Anybody else gets exactly what a missing job gets, and that is a disclosure rule
//
// A 403 would tell a stranger holding a guessed identifier that the job exists. Every endpoint in
// this domain already answers that way ([apiError]) and this one is held to the stronger form its
// *Done when* asks for: the two responses are **byte-identical**, which
// TestAStrangerAndAMissingJobAreTheSameBytes asserts rather than assumes.
//
// # The budget is not here, and the guard is word-level rather than structural
//
// `Docs/01` §4.3 keeps the customer's maximum private from providers, and this endpoint is the
// riskiest surface the rule has yet had: **`job_status_history.reason` is free text written by
// actors**, and a provider reads it. A closed key set, a search for the word "budget" and a search
// for the stored value are all defeated at once by a sentence — waves 10 and 11 each isolated
// *"The customer has set a maximum."*, which carries no field, no value and no digit and passed a
// thirteen-test suite in another domain. So the guard over this response is
// [historyProseIn]: a phrase list applied to the rendered bytes, with a check on the check.
//
// **What that guard cannot do is police a customer's own words**, and it deliberately does not
// try. A customer who types their maximum into a cancellation reason has disclosed it themselves,
// through a field the platform is only the transport for; distinguishing platform prose from
// customer prose inside one string would be guessing. `Docs/11` §3 records it as a live residual
// rather than pretending it away.

// HistoryFor is one job's recorded transitions, oldest first, for a reader entitled to see them
// (SHIP-65a).
//
// The ownership check and the bid check are asked in that order, which is an ordering rather than a
// preference: `ck_users_role` fixes an account's role at registration and SHIP-45's trigger keeps
// it fixed, so the two can never be the same account, and `delivery`'s partyTo records the same
// ordering for the same reason. Asking the cheaper question first also means the ordinary case —
// a customer opening their own job — costs one statement rather than two.
//
// A stranger and a job that does not exist are one answer, [ErrJobNotFound] and [ErrNotJobOwner]
// mapping to one 404 with one message. They stay two sentinels in Go so a test can tell "the
// stranger was refused" from "the job silently stopped existing", which are the same bytes to a
// client and very different defects.
//
// r is a reader rather than a transaction: two statements at most, no writes, nothing to keep
// consistent between them. `job_status_history` is append-only by trigger (000401), so a row
// cannot change under the second statement — only appear after it, which is a later page of the
// same story rather than a contradiction.
func (s *Service) HistoryFor(
	ctx context.Context,
	r db.Runner,
	readerID, jobID uuid.UUID,
) ([]StatusChange, error) {
	if readerID == uuid.Nil {
		return nil, fmt.Errorf("jobs: a history read names no reader: %w", ErrJobNotFound)
	}

	// Checked before the job is read rather than after, so a deployment wired without the lookup
	// fails on its first request instead of on its first provider. See [WithBidders] for why the
	// alternative — treating an absent lookup as "no bid" — is the dangerous default rather than
	// the cautious one.
	if s.bidders == nil {
		return nil, fmt.Errorf("jobs: history of %s: %w", jobID, ErrNoBidderLookup)
	}

	// The job first, and not a `WHERE id = $1 AND …` that folds the two questions together. A
	// query returning nothing cannot say whether the job is somebody else's or nobody's, and
	// [Service.Job] gives the same argument at greater length.
	job, err := s.store.job(ctx, r, jobID)
	if err != nil {
		return nil, err
	}

	if job.CustomerID != readerID {
		bid, err := s.bidders.HasBidOn(ctx, r, jobID, readerID)
		if err != nil {
			return nil, err
		}
		if !bid {
			return nil, fmt.Errorf("jobs: %s is not a party to %s: %w", readerID, jobID,
				ErrNotJobOwner)
		}
	}

	return s.store.history(ctx, r, jobID)
}
