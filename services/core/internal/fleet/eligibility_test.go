package fleet

import (
	"context"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/DulsaraNethmin/Shipper/services/core/internal/db"
	"github.com/DulsaraNethmin/Shipper/services/core/internal/testsupport/pgtest"
)

// SHIP-81 against a real PostgreSQL, which for this ticket is not a preference.
//
// The eligibility filter is one SQL predicate reaching across four tables owned by three domains
// (see the header of eligibility.go for why). A mocked repository would be a second implementation
// of that predicate written in Go, and every test below would then be proving that the second
// implementation agrees with itself. Docs/06 §4.1 forbids it; here it would also be pointless.
//
// # What these tests have to show
//
// SHIP-81's *Done when* is "filters by service area, vehicle capability, verification state, and
// job status", so the burden is four filters and **each one shown to exclude something** —
// [TestEachFilterExcludesSomething] is that, one subtest per filter, each starting from a world
// where everything passes and breaking exactly one thing.
//
// Two further properties matter as much and are not in the *Done when*:
//
//   - **The opt-in rule.** A provider who has declared nothing matches nothing
//     ([TestAProviderWhoHasDeclaredNothingSeesNothing]). SHIP-79 settled it and this is where it
//     is enforced.
//   - **One definition of eligibility.** [TestEligibleForAgreesWithTheFeed] holds the single-job
//     check to the feed across every case, which is the claim the shared predicate makes.

// publishInstant is when the test world's jobs become Open.
//
// Deliberately fixed rather than taken from the database. `jobs_open_gets_a_deadline` computes
// expires_at from the *database's* now(), and the domain compares it against the *injectable*
// clock (Docs/10 §6.3) — so a test that let the two drift apart would pass or fail on how far the
// machine's clock had moved from [testInstant]. Every job here is given an explicit deadline
// relative to the clock the filter actually reads.
var (
	publishInstant = testInstant.Add(-24 * time.Hour)
	farFuture      = testInstant.Add(14 * 24 * time.Hour)
	alreadyPast    = testInstant.Add(-time.Hour)
)

// --- the world these tests run in ---------------------------------------------------------------

// world is one provider and one job, arranged so that every filter accepts.
//
// Each test then breaks exactly one thing, which is what makes a failure legible: if the happy
// path and one exclusion both fail, the setup is wrong, and if only the exclusion fails, the
// filter is.
type world struct {
	pool     *pgxpool.Pool
	provider uuid.UUID
	customer uuid.UUID
	job      uuid.UUID
}

// newWorld builds the eligible case: a verified provider serving Victoria with a van in service,
// and an Open job picking up in Richmond that the van can carry.
func newWorld(t *testing.T) world {
	t.Helper()

	pool := pgtest.DB(t)

	w := world{
		pool:     pool,
		provider: newVerifiedProvider(t, pool, "eligible-provider@example.com", "+61400000801"),
		customer: newCustomer(t, pool, "eligible-customer@example.com", "+61400000802"),
	}

	declare(t, pool, w.provider, ProfileFields{States: &[]string{"VIC"}})
	addVehicle(t, pool, w.provider, "EL1GBL", Capacity{MaxWeightKg: 1200, LengthCm: 300, WidthCm: 160, HeightCm: 180})

	w.job = publishJob(t, pool, w.customer, jobFields{
		PickupState:      "VIC",
		PickupPostcode:   "3121",
		PickupSuburb:     "Richmond",
		GoodsDescription: "Two-seater sofa",
		WeightKg:         80,
		LengthCm:         190,
		WidthCm:          90,
		HeightCm:         80,
	})
	return w
}

// feed is what the provider sees, through the service rather than the store, so the page shape and
// the clamping are exercised too.
func (w world) feed(t *testing.T) []EligibleJob {
	t.Helper()

	page, err := newTestService().EligibleJobs(t.Context(), w.pool, w.provider, EligibilityQuery{})
	if err != nil {
		t.Fatalf("reading the feed: %v", err)
	}
	return page.Jobs
}

// sees reports whether the world's job is in the world's feed.
func (w world) sees(t *testing.T) bool {
	t.Helper()

	for _, job := range w.feed(t) {
		if job.ID == w.job {
			return true
		}
	}
	return false
}

// --- the acceptance criterion --------------------------------------------------------------------

// TestTheFeedShowsAJobEveryFilterAccepts is the positive half, and it has to come first.
//
// Every exclusion below is only meaningful because this passes: a filter that excluded everything
// would satisfy all four negative tests and be entirely broken.
func TestTheFeedShowsAJobEveryFilterAccepts(t *testing.T) {
	w := newWorld(t)

	jobs := w.feed(t)
	if len(jobs) != 1 {
		t.Fatalf("the feed carries %d jobs, want exactly the one every filter accepts", len(jobs))
	}

	job := jobs[0]
	if job.ID != w.job {
		t.Errorf("the feed carries %s, want %s", job.ID, w.job)
	}
	if job.Status != "Open" {
		t.Errorf("the job's status is %q, want Open", job.Status)
	}
	if job.Pickup.Suburb != "Richmond" || job.Pickup.State != "VIC" || job.Pickup.Postcode != "3121" {
		t.Errorf("the pickup reads %+v, want Richmond VIC 3121", job.Pickup)
	}
	if job.GoodsDescription != "Two-seater sofa" {
		t.Errorf("the goods description is %q", job.GoodsDescription)
	}
	if job.WeightKg != 80 || job.LengthCm != 190 {
		t.Errorf("the dimensions read %g kg / %d cm, want 80 / 190", job.WeightKg, job.LengthCm)
	}
	if job.ExpiresAt.IsZero() {
		t.Error("the job carries no deadline, so a provider cannot tell how long they have")
	}
}

// TestEachFilterExcludesSomething is SHIP-81's *Done when*, one subtest per filter.
//
// Each starts from the world every filter accepts and breaks exactly one thing. `what` says what
// was broken and the failure message reads it back, so a subtest reporting "service area — the
// provider withdrew from the state the job picks up in: the job was not excluded" needs no
// cross-referencing to act on.
//
// `disrupt` takes the world by pointer because one case replaces the job it is watching: a draft
// is never published, so the only way to ask "is a draft offered" is to remove the published job
// and point the world at the draft instead.
func TestEachFilterExcludesSomething(t *testing.T) {
	cases := []struct {
		filter  string
		what    string
		disrupt func(t *testing.T, w *world)
	}{
		{
			filter: "job status",
			what:   "the job was cancelled",
			disrupt: func(t *testing.T, w *world) {
				transition(t, w.pool, w.job, w.customer, "Open", "Cancelled")
			},
		},
		{
			filter: "job status",
			what:   "the job is still a draft",
			disrupt: func(t *testing.T, w *world) {
				// A second job, never published. The world's own job is removed from the feed's
				// reach by cancelling it, so the draft is the only candidate left.
				transition(t, w.pool, w.job, w.customer, "Open", "Cancelled")
				w.job = draftJob(t, w.pool, w.customer, jobFields{
					PickupState: "VIC", PickupPostcode: "3121",
				})
			},
		},
		{
			filter: "job status",
			what:   "the job's deadline has passed but the sweep has not reached it",
			disrupt: func(t *testing.T, w *world) {
				setExpiry(t, w.pool, w.job, alreadyPast)
			},
		},
		{
			filter: "verification state",
			what:   "the provider has not verified their email",
			disrupt: func(t *testing.T, w *world) {
				exec(t, w.pool, `UPDATE users SET email_verified_at = NULL WHERE id = $1`, w.provider)
			},
		},
		{
			filter: "verification state",
			what:   "the provider has not verified their phone",
			disrupt: func(t *testing.T, w *world) {
				exec(t, w.pool, `UPDATE users SET phone_verified_at = NULL WHERE id = $1`, w.provider)
			},
		},
		{
			filter: "verification state",
			what:   "the account is suspended",
			disrupt: func(t *testing.T, w *world) {
				exec(t, w.pool, `UPDATE users SET status = 'suspended' WHERE id = $1`, w.provider)
			},
		},
		{
			filter: "verification state",
			what:   "the account is restricted pending clarification",
			disrupt: func(t *testing.T, w *world) {
				exec(t, w.pool, `UPDATE users SET status = 'restricted' WHERE id = $1`, w.provider)
			},
		},
		{
			filter: "service area",
			what:   "the provider withdrew from the state the job picks up in",
			disrupt: func(t *testing.T, w *world) {
				declare(t, w.pool, w.provider, ProfileFields{States: &[]string{"NSW"}})
			},
		},
		{
			filter: "service area",
			what:   "the provider named a postcode that is not this job's",
			disrupt: func(t *testing.T, w *world) {
				declare(t, w.pool, w.provider, ProfileFields{
					States: &[]string{}, Postcodes: &[]string{"3000"},
				})
			},
		},
		{
			filter: "vehicle capability",
			what:   "the only vehicle that could carry it was deactivated",
			disrupt: func(t *testing.T, w *world) {
				exec(t, w.pool, `UPDATE vehicles SET deactivated_at = now() WHERE provider_id = $1`, w.provider)
			},
		},
		{
			filter: "vehicle capability",
			what:   "the job is heavier than anything in the fleet",
			disrupt: func(t *testing.T, w *world) {
				exec(t, w.pool, `UPDATE jobs SET weight_kg = 5000 WHERE id = $1`, w.job)
			},
		},
		{
			filter: "vehicle capability",
			what:   "the job is longer than the load space",
			disrupt: func(t *testing.T, w *world) {
				exec(t, w.pool, `UPDATE jobs SET length_cm = 900 WHERE id = $1`, w.job)
			},
		},
	}

	for _, c := range cases {
		t.Run(c.filter+" — "+c.what, func(t *testing.T) {
			w := newWorld(t)
			if !w.sees(t) {
				t.Fatal("the world is wrong: the job was not visible before anything was broken")
			}

			c.disrupt(t, &w)

			if w.sees(t) {
				t.Errorf("%s did not exclude the job, though %s.\n"+
					"Docs/01 §4.3 requires the platform to filter on all four of service area, "+
					"vehicle capability, verification state and job status.", c.filter, c.what)
			}
		})
	}
}

// TestAProviderWhoHasDeclaredNothingSeesNothing is SHIP-79's rule enforced, and the single most
// important behaviour in this file.
//
// **Eligibility is opt-in.** The alternative reading — an empty declaration matches everything —
// would make the provider who has not finished onboarding the widest-reaching provider on the
// platform, and Docs/04 §3 requires the service area declared before verification passes. Each of
// the four filters is checked separately, because "declared nothing" has four independent halves
// and a filter written with `NOT EXISTS` in the wrong place would pass three of them.
func TestAProviderWhoHasDeclaredNothingSeesNothing(t *testing.T) {
	cases := map[string]func(t *testing.T, w world){
		"has declared no service area": func(t *testing.T, w world) {
			declare(t, w.pool, w.provider, ProfileFields{States: &[]string{}, Postcodes: &[]string{}})
		},
		"has no vehicle at all": func(t *testing.T, w world) {
			exec(t, w.pool, `DELETE FROM vehicles WHERE provider_id = $1`, w.provider)
		},
		"has verified neither channel": func(t *testing.T, w world) {
			exec(t, w.pool,
				`UPDATE users SET email_verified_at = NULL, phone_verified_at = NULL WHERE id = $1`,
				w.provider)
		},
		"has done none of it": func(t *testing.T, w world) {
			declare(t, w.pool, w.provider, ProfileFields{States: &[]string{}, Postcodes: &[]string{}})
			exec(t, w.pool, `DELETE FROM vehicles WHERE provider_id = $1`, w.provider)
			exec(t, w.pool,
				`UPDATE users SET email_verified_at = NULL, phone_verified_at = NULL WHERE id = $1`,
				w.provider)
		},
	}

	for name, undeclare := range cases {
		t.Run("a provider who "+name, func(t *testing.T) {
			w := newWorld(t)
			undeclare(t, w)

			if jobs := w.feed(t); len(jobs) != 0 {
				t.Errorf("the feed carries %d jobs. An empty declaration matches nothing, not "+
					"everything: eligibility is opt-in (SHIP-79, Docs/04 §3).", len(jobs))
			}
		})
	}
}

// TestNotStatedNeverExcludes is the other half of the capability filter, and the one that would
// quietly empty the feed if it were wrong.
//
// 000300 lets a provider add a vehicle with a plate and nothing else; 000404 lets a customer
// publish without measuring anything. If "not stated" were read as "does not fit", the marketplace
// would show almost nothing — and it would look like a matching problem rather than like a NULL.
func TestNotStatedNeverExcludes(t *testing.T) {
	cases := map[string]func(t *testing.T, w world){
		"the vehicle states no capacity": func(t *testing.T, w world) {
			exec(t, w.pool, `UPDATE vehicles SET max_weight_kg = NULL, load_length_cm = NULL,
				load_width_cm = NULL, load_height_cm = NULL WHERE provider_id = $1`, w.provider)
		},
		"the job states no dimensions": func(t *testing.T, w world) {
			exec(t, w.pool, `UPDATE jobs SET weight_kg = NULL, length_cm = NULL,
				width_cm = NULL, height_cm = NULL WHERE id = $1`, w.job)
		},
		"neither side states anything": func(t *testing.T, w world) {
			exec(t, w.pool, `UPDATE vehicles SET max_weight_kg = NULL, load_length_cm = NULL,
				load_width_cm = NULL, load_height_cm = NULL WHERE provider_id = $1`, w.provider)
			exec(t, w.pool, `UPDATE jobs SET weight_kg = NULL, length_cm = NULL,
				width_cm = NULL, height_cm = NULL WHERE id = $1`, w.job)
		},
	}

	for name, unstate := range cases {
		t.Run(name, func(t *testing.T) {
			w := newWorld(t)
			unstate(t, w)

			if !w.sees(t) {
				t.Error("the job was excluded although nothing is known to not fit. A missing " +
					"measurement is not a mismatch; only a known one excludes.")
			}
		})
	}
}

// TestOneVehicleThatFitsIsEnough is what makes the capability filter a fleet question rather than
// a vehicle question.
//
// A provider whose motorcycle cannot take the sofa but whose truck can is eligible. Written
// because the obvious mistake — comparing the job against the *first* vehicle, or against some
// aggregate of the fleet — passes every other test in this file.
func TestOneVehicleThatFitsIsEnough(t *testing.T) {
	w := newWorld(t)

	exec(t, w.pool, `DELETE FROM vehicles WHERE provider_id = $1`, w.provider)
	addVehicle(t, w.pool, w.provider, "T1NY", Capacity{MaxWeightKg: 20, LengthCm: 40, WidthCm: 40, HeightCm: 40})
	addVehicle(t, w.pool, w.provider, "B1GG3R", Capacity{MaxWeightKg: 1200, LengthCm: 400, WidthCm: 200, HeightCm: 200})

	if !w.sees(t) {
		t.Error("a fleet holding one vehicle that fits was treated as unable to carry the job")
	}

	// And the small one alone is not.
	exec(t, w.pool, `DELETE FROM vehicles WHERE registration = 'B1GG3R'`)
	if w.sees(t) {
		t.Error("a fleet holding only a vehicle far too small was treated as able to carry the job")
	}
}

// TestANegotiatingJobIsStillBiddable is Docs/02 §1 held to, and it is the reading most likely to
// have been got wrong.
//
// §1: "Negotiating | One or more active bids or counter-offers exist; **job remains open to
// eligible bids**", and below the table: "Technically, the job remains available for eligible bids
// unless the customer closes it or awards a bid."
//
// Nothing can reach Negotiating until SHIP-90, so this test is the only thing standing between the
// document and a feed that drops every job the moment somebody bids on it — a defect that would
// surface long after this ticket and look like a bidding bug.
func TestANegotiatingJobIsStillBiddable(t *testing.T) {
	w := newWorld(t)

	transition(t, w.pool, w.job, w.customer, "Open", "Negotiating")

	jobs := w.feed(t)
	if len(jobs) != 1 {
		t.Fatalf("the feed carries %d jobs after the job began being negotiated, want 1 — "+
			"Docs/02 §1 keeps a Negotiating job open to eligible bids", len(jobs))
	}
	if jobs[0].Status != "Negotiating" {
		t.Errorf("the job's status reads %q, want Negotiating", jobs[0].Status)
	}
}

// TestAJobIsNotOfferedToItsOwnCustomer covers the clause 000500 handed to this ticket by name.
//
// It is unreachable through the product today — `users.role` is immutable (000005) and `jobs`
// refuses a job whose customer is not a customer account — so the row has to be made by hand. That
// is the point: the test says what the predicate does if the platform ever lets one account hold
// both roles, rather than leaving a clause nobody can explain.
func TestAJobIsNotOfferedToItsOwnCustomer(t *testing.T) {
	w := newWorld(t)

	// The provider becomes the job's customer directly. Nothing in the product can do this.
	exec(t, w.pool, `UPDATE jobs SET customer_id = $1 WHERE id = $2`, w.provider, w.job)

	if w.sees(t) {
		t.Error("a provider was offered a job they had themselves published")
	}
}

// TestEligibleForAgreesWithTheFeed is the claim the shared predicate makes, tested rather than
// asserted.
//
// `doc.go` requires eligibility to be decided in one place — "the feed itself is filtered here and
// a bid on an ineligible job is refused here" — and the two readers exist so SHIP-84 does not write
// a second definition. If they could disagree, a provider could be shown a job and then refused a
// bid on it, which is the worst of both.
func TestEligibleForAgreesWithTheFeed(t *testing.T) {
	service := newTestService()

	cases := map[string]func(t *testing.T, w world){
		"everything passes": func(t *testing.T, w world) {},
		"unverified": func(t *testing.T, w world) {
			exec(t, w.pool, `UPDATE users SET phone_verified_at = NULL WHERE id = $1`, w.provider)
		},
		"out of area": func(t *testing.T, w world) { declare(t, w.pool, w.provider, ProfileFields{States: &[]string{"WA"}}) },
		"no vehicle": func(t *testing.T, w world) {
			exec(t, w.pool, `DELETE FROM vehicles WHERE provider_id = $1`, w.provider)
		},
		"cancelled":             func(t *testing.T, w world) { transition(t, w.pool, w.job, w.customer, "Open", "Cancelled") },
		"being negotiated":      func(t *testing.T, w world) { transition(t, w.pool, w.job, w.customer, "Open", "Negotiating") },
		"past its deadline":     func(t *testing.T, w world) { setExpiry(t, w.pool, w.job, alreadyPast) },
		"too heavy for a truck": func(t *testing.T, w world) { exec(t, w.pool, `UPDATE jobs SET weight_kg = 9000 WHERE id = $1`, w.job) },
	}

	for name, arrange := range cases {
		t.Run(name, func(t *testing.T) {
			w := newWorld(t)
			arrange(t, w)

			permitted, err := service.EligibleFor(t.Context(), w.pool, w.provider, w.job)
			if err != nil {
				t.Fatalf("asking whether the provider may bid: %v", err)
			}
			if permitted != w.sees(t) {
				t.Errorf("EligibleFor says %t and the feed says %t. One predicate serves both, so "+
					"they cannot disagree — a provider shown a job and then refused a bid on it is "+
					"the defect this shares the clause to prevent.", permitted, w.sees(t))
			}
		})
	}
}

// TestAJobThatIsNotThereIsNotEligible is the missing-row reading, stated so it is not later
// "fixed" into an error.
func TestAJobThatIsNotThereIsNotEligible(t *testing.T) {
	w := newWorld(t)

	absent, err := uuid.NewV7()
	if err != nil {
		t.Fatalf("generating an id: %v", err)
	}

	permitted, err := newTestService().EligibleFor(t.Context(), w.pool, w.provider, absent)
	if err != nil {
		t.Fatalf("asking about a job that does not exist: %v", err)
	}
	if permitted {
		t.Error("a job that does not exist was reported as biddable")
	}
}

// TestTheFeedPagesWithoutRepeatingOrSkipping exercises the keyset, including the tie the cursor's
// second field exists for.
//
// Every job here is published in one sitting, which is exactly the case where `created_at` is not
// unique — and a cursor that could not break the tie would drop or repeat a job at the page
// boundary, silently.
func TestTheFeedPagesWithoutRepeatingOrSkipping(t *testing.T) {
	w := newWorld(t)
	service := newTestService()

	// Four more, so there are five in total and a page of two takes three requests.
	for i := range 4 {
		publishJob(t, w.pool, w.customer, jobFields{
			PickupState:      "VIC",
			PickupPostcode:   "3121",
			GoodsDescription: "Parcel " + strconv.Itoa(i),
		})
	}

	seen := map[uuid.UUID]int{}
	var cursor JobCursor
	for page := 0; page < 10; page++ {
		got, err := service.EligibleJobs(t.Context(), w.pool, w.provider,
			EligibilityQuery{Limit: 2, After: cursor})
		if err != nil {
			t.Fatalf("reading page %d: %v", page, err)
		}
		for _, job := range got.Jobs {
			seen[job.ID]++
		}
		if !got.HasMore {
			break
		}
		cursor = got.Next
	}

	if len(seen) != 5 {
		t.Errorf("paging saw %d distinct jobs, want 5", len(seen))
	}
	for id, times := range seen {
		if times != 1 {
			t.Errorf("%s was returned %d times; a keyset cursor must not repeat a row", id, times)
		}
	}
}

// TestAnEligibilityQueryNamesAProvider keeps the nil-provider case from reading the whole
// marketplace.
//
// The predicate binds `$1` in four places, and a nil UUID is a real value rather than a missing
// one — `j.customer_id <> $1` would be true for every job. The refusal is in the service so no
// caller can reach the store without one.
func TestAnEligibilityQueryNamesAProvider(t *testing.T) {
	w := newWorld(t)

	if _, err := newTestService().EligibleJobs(t.Context(), w.pool, uuid.Nil, EligibilityQuery{}); err == nil {
		t.Error("an eligibility query naming no provider was accepted")
	}

	permitted, err := newTestService().EligibleFor(t.Context(), w.pool, uuid.Nil, w.job)
	if err != nil {
		t.Fatalf("asking on behalf of nobody: %v", err)
	}
	if permitted {
		t.Error("a query naming no provider was told it may bid")
	}
}

// --- the guards on the cross-domain reach ---------------------------------------------------------

// TestTheBiddableStatusesAreRealJobStatuses pairs [biddableStatuses] with `ck_jobs_status`, and
// with the SQL that hard-codes them.
//
// Docs/10 §3.4 requires every enumeration held in two places to be paired with a test that reads
// the constraint out of `pg_constraint`. This one is held in *three*: the Go slice, the literal in
// [eligible], and the CHECK in a migration another domain owns. A rename in `jobs` produces no
// compile error here — this is what produces the failure instead.
func TestTheBiddableStatusesAreRealJobStatuses(t *testing.T) {
	pool := pgtest.DB(t)

	var definition string
	if err := pool.QueryRow(t.Context(),
		`SELECT pg_get_constraintdef(oid) FROM pg_constraint WHERE conname = 'ck_jobs_status'`,
	).Scan(&definition); err != nil {
		t.Fatalf("reading ck_jobs_status: %v", err)
	}
	accepted := quotedStrings(definition)

	// Every status this domain filters on is one `jobs` can actually hold.
	for _, status := range biddableStatuses {
		if !contains(accepted, status) {
			t.Errorf("fleet filters on the job status %q, which ck_jobs_status does not accept.\n"+
				"Docs/02 §1 is authoritative for these names, and `jobs` may have renamed one.\n"+
				"ck_jobs_status accepts: %v", status, accepted)
		}
	}

	// The SQL says the same two, so the literal and the slice cannot drift.
	inSQL := quotedStrings(statusClause(t, eligible))
	if strings.Join(inSQL, ",") != strings.Join(biddableStatuses, ",") {
		t.Errorf("the predicate filters on %v and biddableStatuses is %v; they are one decision",
			inSQL, biddableStatuses)
	}

	// And the list cannot silently widen into every status there is. These four exist and must
	// never be biddable: Docs/02 §1 makes a Draft invisible to providers and §2 closes a job to
	// bids once it is awarded.
	for _, status := range []string{"Draft", "Awarded", "Completed", "Cancelled"} {
		if !contains(accepted, status) {
			t.Fatalf("this test is checking nothing: ck_jobs_status does not accept %q", status)
		}
		if contains(biddableStatuses, status) {
			t.Errorf("%q is biddable, which Docs/02 §1 does not permit", status)
		}
	}
}

// TestOnlyTheEligibilityFilterReadsTheJobsTable confines the cross-domain reach to one file.
//
// The header of eligibility.go argues that reading another domain's table in SQL is acceptable
// *here*, and one of the three things it offers in exchange is that the reach stays auditable —
// "which parts of fleet reach into jobs" answerable by reading one file rather than by grepping.
// This is what makes that true tomorrow as well as today.
//
// Source parsing rather than a naming convention, for the reason SHIP-67's guard gives: the thing
// to catch is a query that does not exist yet, in a file nobody has written, and the source is the
// complete list by construction.
func TestOnlyTheEligibilityFilterReadsTheJobsTable(t *testing.T) {
	// `FROM jobs`, `JOIN jobs`, `UPDATE jobs`, `INTO jobs` — the table, not the word. Prose in
	// this package mentions the jobs *domain* constantly and must not trip it.
	reads := regexp.MustCompile(`(?i)\b(from|join|update|into)\s+jobs\b`)

	const permitted = "eligibility.go"
	found := false

	for _, file := range fleetSourceFiles(t) {
		source, err := os.ReadFile(file)
		if err != nil {
			t.Fatalf("reading %s: %v", file, err)
		}
		if !reads.Match(source) {
			continue
		}
		if filepath.Base(file) == permitted {
			found = true
			continue
		}
		t.Errorf("%s reads the `jobs` table. The eligibility filter's reach into another "+
			"domain's tables is confined to %s so that it stays auditable — see the header of "+
			"that file. If this query is genuinely needed, it belongs there.", file, permitted)
	}

	if !found {
		t.Errorf("no file reads the `jobs` table, so this test is checking nothing. "+
			"%s is meant to.", permitted)
	}
}

// TestNoProviderFacingShapeCarriesTheBudget is this package's copy of SHIP-67's guard, and it
// exists because that one cannot see this package.
//
// SHIP-67 parses `internal/jobs` and its own header says what it is waiting for: "SHIP-82's feed
// and SHIP-83's provider detail get types of their own", and "when SHIP-83 lands it adds the
// fourth". **SHIP-81 is earlier than either, and it is the first provider-facing shape anywhere
// outside `internal/jobs`** — so the guard reaches it for the first time here.
//
// The allow-list is empty and must stay empty. Nothing in this domain is the owning customer's own
// view of their job; every shape here is read by a provider, so a budget field anywhere in this
// package is a defect with no exception to argue for. That is a stronger statement than the jobs
// copy can make, which is why this is not simply the same test moved.
func TestNoProviderFacingShapeCarriesTheBudget(t *testing.T) {
	structs := 0

	for _, file := range fleetSourceFiles(t) {
		parsed, err := parser.ParseFile(token.NewFileSet(), file, nil, 0)
		if err != nil {
			t.Fatalf("parsing %s: %v", file, err)
		}

		ast.Inspect(parsed, func(node ast.Node) bool {
			spec, ok := node.(*ast.TypeSpec)
			if !ok {
				return true
			}
			structure, ok := spec.Type.(*ast.StructType)
			if !ok {
				return true
			}
			structs++

			for _, field := range structure.Fields.List {
				name, tag := fieldNameAndJSONTag(field)
				if mentionsBudget(name) || mentionsBudget(tag) {
					t.Errorf("%s.%s carries the customer's budget.\n"+
						"Every shape in this domain is read by a provider, and a customer's "+
						"budget is never exposed to one — not as an amount, a band, or a "+
						"\"budget supplied\" flag (Docs/01 §4.3, CLAUDE.md).\n"+
						"There is no allow-list here to add it to. Remove the field.",
						spec.Name.Name, name)
				}
			}
			return true
		})
	}

	if structs == 0 {
		t.Fatal("no structs were parsed, so this test is checking nothing")
	}

	// The column list is the other way the budget could arrive: `jobs.budget` sits three lines
	// from `weight_kg` in the same table, and a SELECT that named it would leak through a struct
	// field this test would then be too late to catch.
	for name, sql := range map[string]string{"eligibleJobColumns": eligibleJobColumns, "eligible": eligible} {
		if mentionsBudget(sql) {
			t.Errorf("%s names the budget column. The provider's feed reads `jobs` directly, so "+
				"the SELECT list is the disclosure boundary (Docs/01 §4.3).", name)
		}
	}
}

// --- helpers ------------------------------------------------------------------------------------

// jobFields is the part of a job these tests set. A subset on purpose: everything absent from it
// is a column the filter must tolerate being NULL.
//
// The second group is what SHIP-82 and SHIP-83 put on the wire rather than what SHIP-81 filters on.
// A job carrying only the first group is enough to test eligibility and not enough to tell whether
// the response carries what a provider needs to price the work — or whether it carries what they
// must never be given.
type jobFields struct {
	PickupSuburb     string
	PickupState      string
	PickupPostcode   string
	GoodsDescription string
	WeightKg         float64
	LengthCm         int
	WidthCm          int
	HeightCm         int

	PickupLine      string
	DropoffLine     string
	DropoffSuburb   string
	DropoffState    string
	DropoffPostcode string

	VehicleRequirement string
	HandlingNotes      string

	PickupWindowStart  time.Time
	PickupWindowEnd    time.Time
	DropoffWindowStart time.Time
	DropoffWindowEnd   time.Time

	// Budget is the customer's own maximum, in AUD, exactly as `jobs.budget` stores it (SHIP-67).
	//
	// **It is here so that the tests asserting a provider never sees it are asserting something.**
	// A fixture with no budget would let every such check pass against a response that leaked one,
	// which is why [TestTheProviderResponseCarriesNoBudgetInAnyForm] refuses to run until it has
	// read a non-NULL budget back out of the row.
	Budget float64

	// PickupLatitude and PickupLongitude are the geocoded doorstep — the street line written as
	// two numbers, which is why the shape that withholds the line has to withhold these too.
	PickupLatitude  float64
	PickupLongitude float64
}

// newVerifiedProvider is a provider who has met Docs/04 §3's automated baseline.
//
// Separate from [newProvider], which SHIP-78's tests use and which leaves both channels
// unverified. Keeping them apart matters: every test in this file would pass against a filter
// that ignored verification if the shared helper quietly verified everybody.
func newVerifiedProvider(t *testing.T, pool *pgxpool.Pool, email, phone string) uuid.UUID {
	t.Helper()

	id := newAccount(t, pool, email, phone, "provider")
	exec(t, pool,
		`UPDATE users SET email_verified_at = now(), phone_verified_at = now() WHERE id = $1`, id)
	return id
}

func newCustomer(t *testing.T, pool *pgxpool.Pool, email, phone string) uuid.UUID {
	t.Helper()
	return newAccount(t, pool, email, phone, "customer")
}

// declare replaces a provider's declaration through the domain, not by writing rows.
func declare(t *testing.T, pool *pgxpool.Pool, provider uuid.UUID, f ProfileFields) {
	t.Helper()

	if err := inTx(t, pool, func(ctx context.Context, r db.Runner) error {
		_, err := newTestService().Declare(ctx, r, provider, f)
		return err
	}); err != nil {
		t.Fatalf("declaring for %s: %v", provider, err)
	}
}

// addVehicle puts a vehicle in the fleet through the domain, then states its capacity.
//
// Two steps because [Service.Add] takes [VehicleFields] and these tests think in [Capacity]; going
// through the service for the insert keeps the plate normalisation and the ownership check in the
// path, which a raw INSERT would skip.
func addVehicle(t *testing.T, pool *pgxpool.Pool, provider uuid.UUID, registration string, c Capacity) {
	t.Helper()

	vehicle, err := newTestService().Add(t.Context(), pool, provider, VehicleFields{
		Registration: ptr(registration),
		Type:         ptr(TypeVan),
		MaxWeightKg:  ptr(c.MaxWeightKg),
		LengthCm:     ptr(c.LengthCm),
		WidthCm:      ptr(c.WidthCm),
		HeightCm:     ptr(c.HeightCm),
	})
	if err != nil {
		t.Fatalf("adding %s: %v", registration, err)
	}
	if vehicle.Capacity != c {
		t.Fatalf("%s stored capacity %+v, want %+v", registration, vehicle.Capacity, c)
	}
}

// draftJob writes a job in the only state 000402 permits one to be created in.
func draftJob(t *testing.T, pool *pgxpool.Pool, customer uuid.UUID, f jobFields) uuid.UUID {
	t.Helper()

	id, err := uuid.NewV7()
	if err != nil {
		t.Fatalf("generating an id: %v", err)
	}

	exec(t, pool, `
		INSERT INTO jobs (id, customer_id, pickup_suburb, pickup_state, pickup_postcode,
		                  goods_description, weight_kg, length_cm, width_cm, height_cm,
		                  pickup_line, dropoff_line, dropoff_suburb, dropoff_state, dropoff_postcode,
		                  vehicle_requirement, handling_notes,
		                  pickup_window_start, pickup_window_end,
		                  dropoff_window_start, dropoff_window_end,
		                  budget, pickup_latitude, pickup_longitude)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10,
		        $11, $12, $13, $14, $15, $16, $17, $18, $19, $20, $21, $22, $23, $24)`,
		id, customer,
		nullText(f.PickupSuburb), nullText(f.PickupState), nullText(f.PickupPostcode),
		nullText(f.GoodsDescription),
		nullFloat(f.WeightKg), nullInt(f.LengthCm), nullInt(f.WidthCm), nullInt(f.HeightCm),
		nullText(f.PickupLine), nullText(f.DropoffLine), nullText(f.DropoffSuburb),
		nullText(f.DropoffState), nullText(f.DropoffPostcode),
		nullText(f.VehicleRequirement), nullText(f.HandlingNotes),
		nullTime(f.PickupWindowStart), nullTime(f.PickupWindowEnd),
		nullTime(f.DropoffWindowStart), nullTime(f.DropoffWindowEnd),
		nullFloat(f.Budget), nullFloat(f.PickupLatitude), nullFloat(f.PickupLongitude))
	return id
}

// nullTime is [nullText] for an instant: the zero time is "the customer did not say".
func nullTime(t time.Time) any {
	if t.IsZero() {
		return nil
	}
	return t
}

// publishJob writes a job and moves it to Open, then gives it an explicit deadline.
//
// **The deadline is set rather than left to the trigger.** `jobs_open_gets_a_deadline` computes it
// from the database's now() and the filter compares it against the injectable clock, so a job
// relying on the trigger would be eligible or not depending on how far the machine's clock had
// drifted from [testInstant]. See the note on [publishInstant].
func publishJob(t *testing.T, pool *pgxpool.Pool, customer uuid.UUID, f jobFields) uuid.UUID {
	t.Helper()

	id := draftJob(t, pool, customer, f)
	transition(t, pool, id, customer, "Draft", "Open")
	setExpiry(t, pool, id, farFuture)
	return id
}

// transition moves a job the only way 000402 allows: a history row written in the same
// transaction, named by `shipper.job_status_transition`.
//
// This package cannot import `jobs` to use the guard's Go side, which is the boundary rule
// working. What it can do is satisfy the same trigger the guard satisfies — so these tests move
// jobs exactly as the platform does, rather than through a back door the platform does not have.
func transition(t *testing.T, pool *pgxpool.Pool, job, actor uuid.UUID, from, to string) {
	t.Helper()

	if err := inTx(t, pool, func(ctx context.Context, r db.Runner) error {
		entry, err := uuid.NewV7()
		if err != nil {
			return err
		}
		if _, err := r.Exec(ctx, `
			INSERT INTO job_status_history
				(id, job_id, from_status, to_status, actor_type, actor_id, actor_recorded_at)
			VALUES ($1, $2, $3, $4, 'customer', $5, $6)`,
			entry, job, from, to, actor, publishInstant); err != nil {
			return err
		}
		if _, err := r.Exec(ctx,
			`SELECT set_config('shipper.job_status_transition', $1::text, true)`, entry); err != nil {
			return err
		}
		_, err = r.Exec(ctx, `UPDATE jobs SET status = $2 WHERE id = $1`, job, to)
		return err
	}); err != nil {
		t.Fatalf("moving %s from %s to %s: %v", job, from, to, err)
	}
}

// setExpiry writes a deadline directly. It changes no status, so 000402's guard passes it through.
func setExpiry(t *testing.T, pool *pgxpool.Pool, job uuid.UUID, at time.Time) {
	t.Helper()
	exec(t, pool, `UPDATE jobs SET expires_at = $2 WHERE id = $1`, job, at)
}

// exec runs one statement and fails the test rather than returning an error, because every caller
// here is arranging a world rather than exercising one.
func exec(t *testing.T, pool *pgxpool.Pool, sql string, args ...any) {
	t.Helper()

	if _, err := pool.Exec(t.Context(), sql, args...); err != nil {
		t.Fatalf("%s: %v", strings.Join(strings.Fields(sql), " "), err)
	}
}

// fleetSourceFiles is every non-test Go file in this package's directory.
func fleetSourceFiles(t *testing.T) []string {
	t.Helper()

	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatalf("reading the package directory: %v", err)
	}

	var files []string
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		files = append(files, filepath.Join(".", name))
	}
	sort.Strings(files)

	if len(files) == 0 {
		t.Fatal("no source files were found, so these tests are checking nothing")
	}
	return files
}

// fieldNameAndJSONTag reads what a struct field is called in Go and on the wire.
//
// The same shape SHIP-67's guard uses, including the embedded case: an embedded field has no name
// of its own, so its type is reported instead — which is what a reader would call it, and what a
// leak through embedding would look like.
func fieldNameAndJSONTag(field *ast.Field) (name, tag string) {
	if len(field.Names) > 0 {
		name = field.Names[0].Name
	} else if ident, ok := field.Type.(*ast.Ident); ok {
		name = ident.Name
	}

	if field.Tag == nil {
		return name, ""
	}
	return name, field.Tag.Value
}

// mentionsBudget is the one definition of "carries the budget" in this package, so the struct
// check and the SQL check cannot disagree about what they are looking for.
func mentionsBudget(s string) bool { return strings.Contains(strings.ToLower(s), "budget") }

// quotedStrings pulls the single-quoted literals out of a SQL fragment, in order.
var quoted = regexp.MustCompile(`'([^']*)'`)

func quotedStrings(sql string) []string {
	var out []string
	for _, match := range quoted.FindAllStringSubmatch(sql, -1) {
		out = append(out, match[1])
	}
	return out
}

// statusClause isolates `j.status IN (…)` so [quotedStrings] reads the status list and not every
// other literal in the predicate.
var statusIn = regexp.MustCompile(`j\.status IN \(([^)]*)\)`)

func statusClause(t *testing.T, predicate string) string {
	t.Helper()

	match := statusIn.FindStringSubmatch(predicate)
	if match == nil {
		t.Fatal("the predicate has no `j.status IN (…)` clause, so the job status filter is gone")
	}
	return match[1]
}

func contains(haystack []string, needle string) bool {
	for _, s := range haystack {
		if s == needle {
			return true
		}
	}
	return false
}
