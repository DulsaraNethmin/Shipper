package delivery

import (
	"context"
	"errors"
	"net/http"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/DulsaraNethmin/Shipper/services/core/internal/clock"
	"github.com/DulsaraNethmin/Shipper/services/core/internal/db"
	"github.com/DulsaraNethmin/Shipper/services/core/internal/testsupport/pgtest"
)

// SHIP-109 against a real PostgreSQL.
//
// # The *Done when* is about what stops working, so that is what these assert
//
// "Provider or admin can reissue a link, invalidating the previous one." The first half is an
// endpoint answering 200, which is easy and proves little; the half worth testing is the **previous
// link**, and the only way to see it is to hold both and present each in turn. Every test below
// keeps the outgoing token.
//
// Revocation here is a read rather than a denylist — `driver_assignments.link_token_id` names the one
// `jti` that opens the assignment (000606) — so the check that matters runs in
// [Service.AssignmentFor], which every driver route already goes through. That is why there is no
// test for "the reissue endpoint revokes": there is nothing to revoke, only a column to overwrite.

// reissue runs one reissue in its own transaction, which is what the handler does.
func reissue(
	t *testing.T,
	pool *pgxpool.Pool,
	svc *Service,
	provider, jobID uuid.UUID,
) (Assignment, DriverToken, error) {
	t.Helper()

	var (
		assignment Assignment
		token      DriverToken
	)
	err := db.InTx(t.Context(), pool, func(ctx context.Context, r db.Runner) error {
		var err error
		assignment, token, err = svc.ReissueDriverLink(ctx, r, provider, jobID)
		return err
	})
	return assignment, token, err
}

// TestReissuingALinkInvalidatesThePreviousOne is the *Done when*, over HTTP for the driver's half.
//
// The old link and the new one are presented to the same route in turn. The first opened the
// delivery a moment ago and now answers exactly what a job that does not exist answers; the second
// opens it. **The assignment is untouched** — same row, same driver — which is the distinction
// between reissuing a link and replacing a driver.
func TestReissuingALinkInvalidatesThePreviousOne(t *testing.T) {
	pool := pgtest.DB(t)
	router := newDriverRouter(t, pool, clock.NewFixed(testDriverIssuedAt))

	customer := newAccount(t, pool, "reissue-c@example.com", "+61400000760", "customer")
	provider := newAccount(t, pool, "reissue-p@example.com", "+61400000761", "provider")
	jobID := awardedJob(t, pool, customer, provider)

	assignment, _, first := driverOnJob(t, pool, provider, jobID)

	if rec := openLink(router, jobID, bearer(first.Value)); rec.Code != http.StatusOK {
		t.Fatalf("the original link answered %d before the reissue, want 200 (%s)", rec.Code, rec.Body)
	}

	reissued, second, err := reissue(t, pool, newTestService(), provider, jobID)
	if err != nil {
		t.Fatalf("ReissueDriverLink() = %v", err)
	}

	switch {
	case second.Value == first.Value:
		t.Fatal("the reissue handed back the same token, so nothing was invalidated")
	case reissued.ID != assignment.ID:
		t.Errorf("the assignment changed from %s to %s; a reissue replaces the link and not the "+
			"driver (000600)", assignment.ID, reissued.ID)
	case reissued.DriverName != assignment.DriverName:
		t.Errorf("driver_name changed to %q", reissued.DriverName)
	case reissued.LinkIssueCount != 2:
		t.Errorf("link_issue_count = %d, want 2 — one for the assignment and one for this",
			reissued.LinkIssueCount)
	}

	if rec := openLink(router, jobID, bearer(first.Value)); rec.Code != http.StatusNotFound {
		t.Errorf("the previous link still opens the delivery: %d, want 404 (%s)", rec.Code, rec.Body)
	}
	if rec := openLink(router, jobID, bearer(second.Value)); rec.Code != http.StatusOK {
		t.Errorf("the new link does not open the delivery: %d, want 200 (%s)", rec.Code, rec.Body)
	}
}

// TestARevokedLinkRecordsNothingEither is the half that matters more than the read.
//
// A stood-down driver who could still see the job is a disclosure; one who could still record
// milestones is a delivery being written by somebody the provider has taken off it. Both go through
// [Service.AssignmentFor], which is the whole reason the check lives there rather than in a handler.
func TestARevokedLinkRecordsNothingEither(t *testing.T) {
	pool := pgtest.DB(t)
	router := driverMilestoneRouter(t, pool)

	customer := newAccount(t, pool, "reissue-w-c@example.com", "+61400000762", "customer")
	provider := newAccount(t, pool, "reissue-w-p@example.com", "+61400000763", "provider")
	jobID := awardedJob(t, pool, customer, provider)

	_, _, first := driverOnJob(t, pool, provider, jobID)

	if _, _, err := reissue(t, pool, newTestService(), provider, jobID); err != nil {
		t.Fatalf("ReissueDriverLink() = %v", err)
	}

	rec := postMilestoneAs(router, jobID, bearer(first.Value), "reissue-write",
		`{"milestone":"en_route_to_pickup"}`)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("a superseded link recorded a milestone: %d, want 404 (%s)", rec.Code, rec.Body)
	}
	if n := milestoneCount(t, pool, jobID); n != 0 {
		t.Errorf("%d milestones, want 0", n)
	}
}

// TestOnlyTheAwardedProviderMayReissue keeps the link a credential the provider controls.
//
// A stranger and a job that does not exist are one answer, as everywhere else in this domain — and
// the important consequence is that a *driver* cannot reissue their own link either: this route
// takes a mobile session, and a driver has none.
func TestOnlyTheAwardedProviderMayReissue(t *testing.T) {
	pool := pgtest.DB(t)

	customer := newAccount(t, pool, "reissue-x-c@example.com", "+61400000764", "customer")
	provider := newAccount(t, pool, "reissue-x-p@example.com", "+61400000765", "provider")
	stranger := newAccount(t, pool, "reissue-x-s@example.com", "+61400000766", "provider")
	jobID := awardedJob(t, pool, customer, provider)

	_, _, original := driverOnJob(t, pool, provider, jobID)
	svc := newTestService()

	for name, caller := range map[string]uuid.UUID{
		"another provider":        stranger,
		"the customer of the job": customer,
	} {
		t.Run(name, func(t *testing.T) {
			_, _, err := reissue(t, pool, svc, caller, jobID)
			if !errors.Is(err, ErrNotAwardedProvider) && !errors.Is(err, ErrJobNotFound) {
				t.Fatalf("ReissueDriverLink() = %v, want a refusal", err)
			}
		})
	}

	// And the original still works, because a refused reissue must not have written the column.
	router := newDriverRouter(t, pool, clock.NewFixed(testDriverIssuedAt))
	if rec := openLink(router, jobID, bearer(original.Value)); rec.Code != http.StatusOK {
		t.Errorf("a refused reissue invalidated the live link anyway: %d (%s)", rec.Code, rec.Body)
	}
}

// TestReissuingOnAJobWithNoDriverIsAMissingDelivery holds the one refusal that could reasonably have
// been a code of its own.
//
// There is no link to reissue. It answers what a missing delivery answers, and what the provider has
// to do next is assign a driver — see [Service.ReissueDriverLink] for why that is not a third thing
// for a client to branch on.
func TestReissuingOnAJobWithNoDriverIsAMissingDelivery(t *testing.T) {
	pool := pgtest.DB(t)

	customer := newAccount(t, pool, "reissue-nd-c@example.com", "+61400000767", "customer")
	provider := newAccount(t, pool, "reissue-nd-p@example.com", "+61400000768", "provider")
	jobID := awardedJob(t, pool, customer, provider)

	if _, _, err := reissue(t, pool, newTestService(), provider, jobID); !errors.Is(err, ErrJobNotFound) {
		t.Fatalf("ReissueDriverLink() on a driverless job = %v, want ErrJobNotFound", err)
	}
}

// TestAStoodDownDriversLinkCannotBeReissued is the interaction between SHIP-109 and 000600.
//
// A provider who has taken a driver off the job has no assignment to reissue against, and reviving
// one would revive a driver the trigger refuses to revive. The answer is the same missing-delivery
// 404 the driverless job gets, which is correct: from the caller's side those are the same state.
func TestAStoodDownDriversLinkCannotBeReissued(t *testing.T) {
	pool := pgtest.DB(t)

	customer := newAccount(t, pool, "reissue-sd-c@example.com", "+61400000769", "customer")
	provider := newAccount(t, pool, "reissue-sd-p@example.com", "+61400000770", "provider")
	jobID := awardedJob(t, pool, customer, provider)
	driverOnJob(t, pool, provider, jobID)

	if _, err := pool.Exec(t.Context(),
		`UPDATE driver_assignments SET unassigned_at = now() WHERE job_id = $1`, jobID); err != nil {
		t.Fatalf("standing the driver down: %v", err)
	}

	if _, _, err := reissue(t, pool, newTestService(), provider, jobID); !errors.Is(err, ErrJobNotFound) {
		t.Fatalf("ReissueDriverLink() after a stand-down = %v, want ErrJobNotFound", err)
	}
}

// TestAnAssignmentRecordsTheLinkItIssued is the invariant every check above rests on: a link that
// was returned but never recorded would verify and open nothing.
func TestAnAssignmentRecordsTheLinkItIssued(t *testing.T) {
	pool := pgtest.DB(t)

	customer := newAccount(t, pool, "reissue-rec-c@example.com", "+61400000771", "customer")
	provider := newAccount(t, pool, "reissue-rec-p@example.com", "+61400000772", "provider")
	jobID := awardedJob(t, pool, customer, provider)

	assignment, _, token := driverOnJob(t, pool, provider, jobID)

	switch {
	case token.ID == "":
		t.Fatal("the issued token carries no id, so nothing can be recorded against the assignment")
	case assignment.LinkTokenID != token.ID:
		t.Errorf("the row names link %q and the token issued is %q", assignment.LinkTokenID, token.ID)
	case assignment.LinkIssueCount != 1:
		t.Errorf("link_issue_count = %d after an assignment, want 1", assignment.LinkIssueCount)
	case assignment.LinkIssuedAt.IsZero():
		t.Error("link_issued_at is zero")
	}
}

// TestReissueRefusesAPool is the same guard every other write in this domain holds: the link and the
// count it increments are one act.
func TestReissueRefusesAPool(t *testing.T) {
	pool := pgtest.DB(t)

	_, _, err := newTestService().ReissueDriverLink(
		context.Background(), pool, uuid.New(), uuid.New())
	if !errors.Is(err, ErrNotInTransaction) {
		t.Fatalf("ReissueDriverLink() on a pool = %v, want ErrNotInTransaction", err)
	}
}

// TestNominatingTheSameDriverAgainDoesNotRevokeTheLink is the hazard SHIP-109 introduces and the
// reason a repeat re-signs rather than mints.
//
// The phone that lost the first response and asked again (Docs/02 §3.1) writes no row, moves no job
// and emits no event — and it must not cut off a driver who is already holding the link either. A
// provider **asks** for a revocation through the reissue endpoint; a retry does not cause one.
func TestNominatingTheSameDriverAgainDoesNotRevokeTheLink(t *testing.T) {
	pool := pgtest.DB(t)
	router := newDriverRouter(t, pool, clock.NewFixed(testDriverIssuedAt))

	customer := newAccount(t, pool, "reissue-rep-c@example.com", "+61400000773", "customer")
	provider := newAccount(t, pool, "reissue-rep-p@example.com", "+61400000774", "provider")
	jobID := awardedJob(t, pool, customer, provider)

	assignment, _, first := driverOnJob(t, pool, provider, jobID)

	// The same nomination again, with a fresh key: the domain absorbs it.
	repeated, again, created, err := assignGranting(t, pool, newTestService(), provider, jobID, Nomination{
		DriverName:   "Sam Patel",
		DriverMobile: "0412 345 678",
	})
	if err != nil {
		t.Fatalf("the repeated nomination: %v", err)
	}
	if created {
		t.Fatal("the repeat wrote a second assignment; this is not exercising absorption")
	}
	if repeated.LinkIssueCount != assignment.LinkIssueCount {
		t.Errorf("link_issue_count moved from %d to %d on a repeat; nothing was written, so nothing "+
			"was issued", assignment.LinkIssueCount, repeated.LinkIssueCount)
	}
	if again.ID != first.ID {
		t.Errorf("the repeat carries link %q and the assignment holds %q; a retry must not mint a "+
			"new link identifier, because that is what revokes the previous one", again.ID, first.ID)
	}

	if rec := openLink(router, jobID, bearer(first.Value)); rec.Code != http.StatusOK {
		t.Errorf("the link the provider already forwarded stopped working after a retry: %d (%s)",
			rec.Code, rec.Body)
	}
	if rec := openLink(router, jobID, bearer(again.Value)); rec.Code != http.StatusOK {
		t.Errorf("the link the retry answered with does not open the delivery: %d (%s)",
			rec.Code, rec.Body)
	}
}
