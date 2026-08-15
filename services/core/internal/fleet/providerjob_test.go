package fleet

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

// SHIP-96a — a provider reads a job once it has left the open feed.
//
// # The gap this closes, which is one gap seen from both ends of a bid
//
// [eligible] matches `status IN ('Open','Negotiating')`, so before this ticket a provider's view of
// a job ended at the instant the job stopped being biddable. Two consequences, recorded separately
// and identical underneath:
//
//   - **The provider who wins a job loses their view of it by winning.** SHIP-129's milestone screen
//     could show a job identifier and no address, no goods description and no pickup window. Three
//     lanes recorded that independently in wave 7.
//   - **Every provider who lost it loses theirs in the same transaction**, at the moment they most
//     want to look at what they bid on.
//
// [readable] answers both with one clause — the caller already holds a bid — because both audiences
// are "a provider with a relationship to this job that is not eligibility".
//
// # What these tests hold that eligibility_test.go cannot
//
// That file's [newWorld] never places a bid, so every case in it is one where [eligible] and
// [readable] agree. This file is the divergence: it places bids, moves jobs out of the biddable
// statuses, and asserts in both directions — the read admits what the feed and the bid guard refuse,
// and it still refuses a provider with no relationship at all.

// negotiation is a marketplace with a second provider who has bid on nothing.
//
// The second provider is the control for every refusal below. A test that only ever asks on behalf
// of a provider *with* a relationship cannot tell "the predicate widened" from "the predicate is
// gone", and that is precisely the mistake a widening ticket makes.
type negotiation struct {
	marketplace
	stranger uuid.UUID
}

func newNegotiation(t *testing.T) negotiation {
	t.Helper()

	m := newMarketplace(t)

	// Verified, in area and with a vehicle, so the stranger is refused for having no relationship
	// to a *particular* job rather than for failing the eligibility filter generally. A stranger
	// who could not have bid on anything would make every 404 below ambiguous.
	stranger := newVerifiedProvider(t, m.pool, "provider-job-stranger@example.com", "+61400000830")
	declare(t, m.pool, stranger, ProfileFields{States: &[]string{"VIC"}})
	addVehicle(t, m.pool, stranger, "STRNG1",
		Capacity{MaxWeightKg: 1200, LengthCm: 300, WidthCm: 160, HeightCm: 180})

	return negotiation{marketplace: m, stranger: stranger}
}

// bid writes one row of `bids` directly.
//
// **SQL rather than `internal/bidding`, because domains do not import each other.** This package
// cannot call the bidding service, and a fake would prove nothing about the predicate — [readable]
// is SQL and what it reads is a real row. The columns are the ones 000500 makes NOT NULL plus the
// amount its `ck_bids_offer_has_an_amount` requires of anything past 'Draft'.
func (n negotiation) bid(t *testing.T, job, provider uuid.UUID, status string) uuid.UUID {
	t.Helper()

	id, err := uuid.NewV7()
	if err != nil {
		t.Fatalf("generating a bid id: %v", err)
	}

	var amount any
	if status != "Draft" {
		amount = 45000
	}

	exec(t, n.pool,
		`INSERT INTO bids (id, job_id, provider_id, status, amount) VALUES ($1, $2, $3, $4, $5)`,
		id, job, provider, status, amount)
	return id
}

// TestTheProviderReadIsTheFeedPlusTheCallersOwnBids is SHIP-96a's *Done when*, at the service.
//
// **Every bid status, live or closed**, because the row says "any bid on a job, live or closed, and
// the provider awarded it … for as long as the bid or the award exists". The award is not a case of
// its own: SHIP-92 records it by moving the winning bid to 'Accepted' and there is no
// `awarded_provider_id` column anywhere in `jobs`, so the awarded provider *is* a provider holding a
// bid. 'Accepted' appears in the list below for that reason rather than as a separate mechanism.
func TestTheProviderReadIsTheFeedPlusTheCallersOwnBids(t *testing.T) {
	service := newTestService()

	// Every status ck_bids_status permits. A test naming a subset would pass for the same reason
	// the defect would have shipped: the clause names no status, so a status it happens not to
	// cover is one nobody checked.
	statuses := []string{
		"Draft", "Submitted", "Countered", "Accepted",
		"Rejected", "Withdrawn", "Expired", "Superseded",
	}

	for _, status := range statuses {
		t.Run(strings.ToLower(status), func(t *testing.T) {
			n := newNegotiation(t)
			job := n.publish(t, richJob(1500))
			n.bid(t, job, n.provider, status)

			// Out of the feed by the only route that takes it there without deleting anything:
			// the customer cancels. The job is now in no biddable status at all.
			transition(t, n.pool, job, n.customer, "Open", "Cancelled")

			// The feed and the bid guard both refuse it — which is what makes the read below a
			// widening rather than a job that never left.
			if page, err := service.EligibleJobs(t.Context(), n.pool, n.provider, EligibilityQuery{}); err != nil {
				t.Fatalf("reading the feed: %v", err)
			} else if len(page.Jobs) != 0 {
				t.Fatalf("the feed still carries %d jobs, so this job never left it", len(page.Jobs))
			}
			if permitted, err := service.EligibleFor(t.Context(), n.pool, n.provider, job); err != nil {
				t.Fatalf("asking whether the provider may bid: %v", err)
			} else if permitted {
				t.Error("a cancelled job is still biddable — SHIP-96a widened the read and must " +
					"not have widened what a provider may bid on")
			}

			// And the read admits it.
			got, err := service.ProviderJobFor(t.Context(), n.pool, n.provider, job)
			if err != nil {
				t.Fatalf("a provider holding a %s bid could not read the job they bid on: %v",
					status, err)
			}
			if got.ID != job {
				t.Errorf("read %s, want %s", got.ID, job)
			}
			if got.Status != "Cancelled" {
				t.Errorf("status = %q, want Cancelled — the status is the part of this shape "+
					"that says what became of the job", got.Status)
			}

			// The stranger gets what a missing job gets, on the same job, in the same database.
			if _, err := service.ProviderJobFor(t.Context(), n.pool, n.stranger, job); !errors.Is(err, ErrJobNotOffered) {
				t.Errorf("a provider with no bid on this job got %v, want ErrJobNotOffered — the "+
					"clause admits the caller's own bids and nobody else's", err)
			}
		})
	}
}

// TestTheAwardedProviderKeepsTheirViewOfTheJob is the first of the two gaps, end to end.
//
// It is deliberately written the way the award actually happens — the bid moves to 'Accepted' and
// the job moves to 'Awarded' — rather than by asserting on the clause. A test that inserted an
// 'Accepted' bid and left the job Open would pass with the widening removed.
func TestTheAwardedProviderKeepsTheirViewOfTheJob(t *testing.T) {
	n := newNegotiation(t)
	job := n.publish(t, richJob(1500))

	// While it is still open, the provider can read it because they are eligible.
	before := n.get(t, "/v1/fleet/jobs/"+job.String())
	if before.Code != http.StatusOK {
		t.Fatalf("an eligible provider = %d, want 200 (%s)", before.Code, before.Body)
	}

	n.bid(t, job, n.provider, "Accepted")
	transition(t, n.pool, job, n.customer, "Open", "Awarded")

	after := n.get(t, "/v1/fleet/jobs/"+job.String())
	if after.Code != http.StatusOK {
		t.Fatalf("the awarded provider = %d, want 200. Before SHIP-96a winning a job was how a "+
			"provider lost sight of it, which left SHIP-129's milestone screen with an "+
			"identifier and no address (%s)", after.Code, after.Body)
	}

	// The same shape, not a fuller one. The widening is about who may ask.
	var was, now map[string]any
	if err := json.Unmarshal(before.Body.Bytes(), &was); err != nil {
		t.Fatalf("the first response is not JSON: %v", err)
	}
	if err := json.Unmarshal(after.Body.Bytes(), &now); err != nil {
		t.Fatalf("the second response is not JSON: %v", err)
	}
	for key := range now {
		if _, promised := was[key]; !promised {
			t.Errorf("the awarded provider's response carries %q and the feed's does not. One "+
				"shape, whatever the caller's relationship to the job — two would be two places "+
				"a budget field could be added.", key)
		}
	}
	if was["status"] != "open" || now["status"] != "awarded" {
		t.Errorf("status went %v -> %v, want open -> awarded", was["status"], now["status"])
	}
}

// TestALosingProviderKeepsTheirViewOfTheJob is the second gap, and the one nobody had a ticket for
// until SHIP-96a wrote them as one.
//
// SHIP-93 closes every other bid when a job is awarded, so this is the ordinary outcome for every
// provider but one: their offer becomes 'Rejected' and the job becomes somebody else's. Losing is
// not a reason to stop being able to see what was lost.
func TestALosingProviderKeepsTheirViewOfTheJob(t *testing.T) {
	n := newNegotiation(t)
	job := n.publish(t, richJob(1500))

	n.bid(t, job, n.provider, "Rejected")
	n.bid(t, job, n.stranger, "Accepted")
	transition(t, n.pool, job, n.customer, "Open", "Awarded")

	rec := n.get(t, "/v1/fleet/jobs/"+job.String())
	if rec.Code != http.StatusOK {
		t.Fatalf("a provider whose offer was rejected = %d, want 200 (%s)", rec.Code, rec.Body)
	}
}

// TestAProviderWithNoRelationshipGetsWhatAMissingJobGets is the *Done when*'s refusal clause, and it
// asserts the bytes rather than the status code.
//
// "A provider with neither gets exactly what a missing job gets" is a statement about the response,
// not about the number at the top of it. A 404 whose body differed — a different code, a message
// mentioning eligibility, anything at all — would disclose that the job exists, which is the whole
// thing the shared answer protects.
func TestAProviderWithNoRelationshipGetsWhatAMissingJobGets(t *testing.T) {
	n := newNegotiation(t)

	job := n.publish(t, richJob(1500))
	n.bid(t, job, n.provider, "Submitted")
	transition(t, n.pool, job, n.customer, "Open", "Awarded")

	absent, err := uuid.NewV7()
	if err != nil {
		t.Fatalf("generating an id: %v", err)
	}

	real := as(t, n.router, n.stranger, http.MethodGet, "/v1/fleet/jobs/"+job.String(), "")
	missing := as(t, n.router, n.stranger, http.MethodGet, "/v1/fleet/jobs/"+absent.String(), "")

	if real.Code != http.StatusNotFound {
		t.Fatalf("a job this provider has no relationship to = %d, want 404 (%s)", real.Code, real.Body)
	}

	got, want := normaliseRequestID(real.Body.String()), normaliseRequestID(missing.Body.String())
	if got != want {
		t.Errorf("the two refusals differ, so one of them discloses that the job exists:\n"+
			"  a job that is not yours: %s\n  a job that is not there:  %s", got, want)
	}
}

// normaliseRequestID removes the one field two responses are meant to differ in.
//
// The error contract puts the request id in the body (SHIP-12), so two responses to two requests
// are never byte-identical. Everything else has to be.
func normaliseRequestID(body string) string {
	var document map[string]any
	if err := json.Unmarshal([]byte(body), &document); err != nil {
		return body
	}
	delete(document, "request_id")
	if nested, ok := document["error"].(map[string]any); ok {
		delete(nested, "request_id")
	}

	out, err := json.Marshal(document)
	if err != nil {
		return body
	}
	return string(out)
}

// TestTheWidenedReadCarriesNoBudgetInAnyForm is the last clause of the *Done when*, against the
// path the widening opened.
//
// **This is not the same assertion as SHIP-83's.** That one runs against a job in the feed;
// [readable] serves jobs that are not, reached through a different branch of the same `OR`, and a
// clause that had gone looking for the budget to explain "why can I still see this" would leak on
// exactly the responses SHIP-83's test never asks for. It is also the first provider-facing read of
// a job in a *non-biddable* status, which is where a helpful "the customer had a maximum of…" is
// most tempting to write.
func TestTheWidenedReadCarriesNoBudgetInAnyForm(t *testing.T) {
	n := newNegotiation(t)

	const budget = 4321.99
	job := n.publish(t, richJob(budget))
	n.bid(t, job, n.provider, "Accepted")
	transition(t, n.pool, job, n.customer, "Open", "Awarded")

	// The fixture, verified rather than assumed — a privacy test against a job with nothing to
	// leak passes forever and proves nothing.
	assertJobHasABudget(t, n.pool, job, budget)

	rec := n.get(t, "/v1/fleet/jobs/"+job.String())
	if rec.Code != http.StatusOK {
		t.Fatalf("the awarded provider = %d, want 200 (%s)", rec.Code, rec.Body)
	}

	body := rec.Body.Bytes()
	if !strings.Contains(string(body), `"goods_description"`) {
		t.Fatalf("this response carries no job, so it proves nothing: %s", body)
	}
	assertNoBudget(t, body)
}

// assertJobHasABudget refuses to let a privacy test run against a job with nothing to disclose.
func assertJobHasABudget(t *testing.T, pool *pgxpool.Pool, job uuid.UUID, want float64) {
	t.Helper()

	var stored *float64
	if err := pool.QueryRow(t.Context(),
		`SELECT budget FROM jobs WHERE id = $1`, job).Scan(&stored); err != nil {
		t.Fatalf("reading the stored budget: %v", err)
	}
	if stored == nil || *stored != want {
		t.Fatalf("the job's budget is %v, want %v — this test asserts nothing against a job that "+
			"has no budget to leak", stored, want)
	}
}

// TestABidOnOneJobDoesNotOpenAnother is the parenthesisation, tested rather than trusted.
//
// [readable] is `(eligible) OR EXISTS(a bid)` and the store wraps the whole of it before adding
// `j.id = $3`. Written without those parentheses, SQL's precedence binds the identifier to the
// first branch alone — and every provider holding a bid on any job would read every job on the
// platform. That is a one-character defect with no compile error, no test failure anywhere else,
// and the worst possible blast radius.
func TestABidOnOneJobDoesNotOpenAnother(t *testing.T) {
	n := newNegotiation(t)

	mine := n.publish(t, richJob(1500))
	n.bid(t, mine, n.provider, "Accepted")
	transition(t, n.pool, mine, n.customer, "Open", "Awarded")

	// Somebody else's job, in a state this provider is not eligible for.
	elsewhere := richJob(1500)
	elsewhere.PickupState, elsewhere.PickupPostcode, elsewhere.PickupSuburb = "QLD", "4000", "Brisbane"
	theirs := n.publish(t, elsewhere)

	if _, err := newTestService().ProviderJobFor(t.Context(), n.pool, n.provider, theirs); !errors.Is(err, ErrJobNotOffered) {
		t.Errorf("a provider holding a bid on one job read a different job they are not eligible "+
			"for: %v. The parentheses around `eligible` in readable are what prevent that.", err)
	}
}
