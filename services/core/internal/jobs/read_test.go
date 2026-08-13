package jobs

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/DulsaraNethmin/Shipper/services/core/internal/testsupport/pgtest"
)

// SHIP-65 and SHIP-66 against a real PostgreSQL.
//
// The pagination tests in particular have to run against one. A keyset page is a row comparison
// and an index scan, and Docs/06 §4.1's argument applies exactly: a fake repository would return
// whatever slice it was asked for and prove nothing about whether the boundary between two pages
// falls in the right place.

// draftsFor creates n jobs for one customer, oldest first, and returns their ids in that order.
//
// created_at comes from the database rather than from the clock the service holds, so the rows are
// separated by however long the inserts take — usually well under a millisecond, which is the
// point: it is the tie-breaking half of the cursor that has to carry the ordering, not the
// timestamp.
func draftsFor(t *testing.T, pool *pgxpool.Pool, customer uuid.UUID, n int) []uuid.UUID {
	t.Helper()

	ids := make([]uuid.UUID, 0, n)
	for range n {
		ids = append(ids, newDraft(t, pool, customer))
	}
	return ids
}

// idsOf is a page's jobs in the order they came back.
func idsOf(page JobPage) []uuid.UUID {
	out := make([]uuid.UUID, 0, len(page.Jobs))
	for _, job := range page.Jobs {
		out = append(out, job.ID)
	}
	return out
}

// TestJobReturnsTheWholeJobToItsOwner is SHIP-65's acceptance criterion, and since SHIP-67 it is
// the whole of it.
//
// "Returns full job including budget" was met but for the budget until SHIP-67 brought the column
// together with the proof it cannot reach a provider (Docs/11 §3, §8). The budget is part of what
// this test round-trips now, which closes the gap Docs/11 §4 had recorded against SHIP-65.
//
// "In full" is checked by round-tripping a job with every field populated rather than by naming
// three of them, because the failure this guards against is a reader that quietly drops a column —
// which a spot check of the fields somebody happened to think of would pass.
func TestJobReturnsTheWholeJobToItsOwner(t *testing.T) {
	pool := pgtest.DB(t)
	service := newDraftService(t, &fakeGeocoder{})

	customer := newCustomer(t, pool, "read-owner@example.com", "+61400000650")

	created, err := service.CreateDraft(t.Context(), pool, customer, DraftFields{
		Pickup:             sydney(),
		Dropoff:            melbourne(),
		GoodsDescription:   text("Two-seater sofa, wrapped"),
		LengthCm:           number(190),
		WidthCm:            number(90),
		HeightCm:           number(85),
		WeightKg:           decimal(45.5),
		VehicleRequirement: text("Ute with a tailgate lifter"),
		HandlingNotes:      text("Second-floor walk-up, no lift."),
		PickupWindow:       &TimeWindow{Start: testInstant, End: testInstant.Add(24 * time.Hour)},
		BudgetCents:        money(150_000),
	})
	if err != nil {
		t.Fatalf("creating the job: %v", err)
	}

	read, err := service.Job(t.Context(), pool, customer, created.ID)
	if err != nil {
		t.Fatalf("reading the job: %v", err)
	}

	// Compared with what the store returns for the same row, so a column dropped from the
	// read path fails here rather than being invisible until a client notices.
	if want := reread(t, pool, created.ID); read != want {
		t.Errorf("the job read back differs from the stored row:\n got  %+v\n want %+v", read, want)
	}
	if read.Status != StatusDraft || read.CustomerID != customer {
		t.Errorf("job = %s owned by %s, want a Draft owned by %s", read.Status, read.CustomerID, customer)
	}
	if !read.Pickup.Resolved || read.Pickup.Latitude == 0 {
		t.Errorf("the pickup came back unresolved: %+v", read.Pickup)
	}
	// Named explicitly as well as compared, because this is the clause of SHIP-65's *Done when*
	// that went unmet for two waves: a struct comparison would still pass if the column were
	// dropped from both sides of it.
	if read.BudgetCents != 150_000 {
		t.Errorf("budget = %d cents, want 150000 — SHIP-65 returns the full job including budget",
			read.BudgetCents)
	}
}

// TestJobRefusesAStrangerAndKeepsTheTwoRefusalsApart is the substantive half of SHIP-65: "for the
// owning customer only".
//
// The two sentinels are one 404 on the wire and must stay distinguishable in Go, so a test
// asserting "the stranger was refused" fails if the job silently stopped existing instead.
func TestJobRefusesAStrangerAndKeepsTheTwoRefusalsApart(t *testing.T) {
	pool := pgtest.DB(t)
	service := newDraftService(t, &fakeGeocoder{})

	owner := newCustomer(t, pool, "read-owner-2@example.com", "+61400000651")
	stranger := newCustomer(t, pool, "read-stranger@example.com", "+61400000652")
	job := newDraft(t, pool, owner)

	if _, err := service.Job(t.Context(), pool, stranger, job); !errors.Is(err, ErrNotJobOwner) {
		t.Errorf("a stranger's read = %v, want ErrNotJobOwner", err)
	}

	missing, _ := uuid.NewV7()
	if _, err := service.Job(t.Context(), pool, stranger, missing); !errors.Is(err, ErrJobNotFound) {
		t.Errorf("reading a job that does not exist = %v, want ErrJobNotFound", err)
	}
}

// A published job stays readable by its owner. Editing stops at Draft (SHIP-62); reading does not,
// and a customer who cannot see their own Open job cannot be shown its bids.
func TestJobIsReadableInEveryStatus(t *testing.T) {
	pool := pgtest.DB(t)
	service := newDraftService(t, &fakeGeocoder{})

	customer := newCustomer(t, pool, "read-published@example.com", "+61400000653")
	job := newDraft(t, pool, customer)
	publish(t, pool, job, customer)

	read, err := service.Job(t.Context(), pool, customer, job)
	if err != nil {
		t.Fatalf("reading a published job: %v", err)
	}
	if read.Status != StatusOpen {
		t.Errorf("status = %s, want Open", read.Status)
	}
}

// The read takes no row lock, and that is a property worth pinning rather than trusting to the
// comment on postgresStore.job.
//
// A GET that took FOR UPDATE would serialise every reader of a job behind whatever is writing it.
// Here the writer holds the row for the length of an open transaction and the reader still
// returns — which it could not do if it were waiting on the same lock.
func TestReadingAJobDoesNotWaitOnAWriter(t *testing.T) {
	pool := pgtest.DB(t)
	service := newDraftService(t, &fakeGeocoder{})

	customer := newCustomer(t, pool, "read-unlocked@example.com", "+61400000654")
	job := newDraft(t, pool, customer)

	holder, err := pool.Begin(t.Context())
	if err != nil {
		t.Fatalf("beginning a transaction: %v", err)
	}
	defer func() { _ = holder.Rollback(t.Context()) }()

	if _, err := service.store.lockJob(t.Context(), holder, job); err != nil {
		t.Fatalf("locking the job: %v", err)
	}

	done := make(chan error, 1)
	go func() {
		_, err := service.Job(context.Background(), pool, customer, job)
		done <- err
	}()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("reading a locked job: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the read blocked on the writer's row lock; a GET must not take FOR UPDATE")
	}
}

// TestJobsReturnsOnlyTheCallersOwnJobsNewestFirst is the first half of SHIP-66's acceptance
// criterion, and the half a bug would be worst in.
//
// Another customer's jobs are not refused here, they are never selected — the customer id comes
// from the token rather than from the request — so this is what proves the WHERE clause is present
// at all.
func TestJobsReturnsOnlyTheCallersOwnJobsNewestFirst(t *testing.T) {
	pool := pgtest.DB(t)
	service := newDraftService(t, &fakeGeocoder{})

	mine := newCustomer(t, pool, "list-mine@example.com", "+61400000660")
	theirs := newCustomer(t, pool, "list-theirs@example.com", "+61400000661")

	ours := draftsFor(t, pool, mine, 3)
	draftsFor(t, pool, theirs, 2)

	page, err := service.Jobs(t.Context(), pool, mine, JobQuery{})
	if err != nil {
		t.Fatalf("listing: %v", err)
	}

	if len(page.Jobs) != 3 {
		t.Fatalf("the list has %d jobs, want the caller's 3", len(page.Jobs))
	}
	if page.HasMore {
		t.Error("a page holding everything says there is more")
	}

	// Newest first, which is the reverse of the order they were created in.
	want := []uuid.UUID{ours[2], ours[1], ours[0]}
	for i, id := range idsOf(page) {
		if id != want[i] {
			t.Fatalf("job %d is %s, want %s — the list is not newest first", i, id, want[i])
		}
		if page.Jobs[i].CustomerID != mine {
			t.Fatalf("the list carries a job owned by %s", page.Jobs[i].CustomerID)
		}
	}
}

// TestJobsFiltersByStatus is the second half: "filterable by status".
func TestJobsFiltersByStatus(t *testing.T) {
	pool := pgtest.DB(t)
	service := newDraftService(t, &fakeGeocoder{})

	customer := newCustomer(t, pool, "list-status@example.com", "+61400000662")
	jobs := draftsFor(t, pool, customer, 3)
	publish(t, pool, jobs[0], customer)
	publish(t, pool, jobs[1], customer)

	open, err := service.Jobs(t.Context(), pool, customer, JobQuery{Status: StatusOpen})
	if err != nil {
		t.Fatalf("listing the open jobs: %v", err)
	}
	if len(open.Jobs) != 2 {
		t.Fatalf("the Open list has %d jobs, want 2", len(open.Jobs))
	}
	for _, job := range open.Jobs {
		if job.Status != StatusOpen {
			t.Errorf("the Open list carries a %s job", job.Status)
		}
	}

	drafts, err := service.Jobs(t.Context(), pool, customer, JobQuery{Status: StatusDraft})
	if err != nil {
		t.Fatalf("listing the drafts: %v", err)
	}
	if len(drafts.Jobs) != 1 || drafts.Jobs[0].ID != jobs[2] {
		t.Errorf("the Draft list is %v, want just %s", idsOf(drafts), jobs[2])
	}

	// A status the customer has no jobs in is an empty page, not an error.
	none, err := service.Jobs(t.Context(), pool, customer, JobQuery{Status: StatusDelivered})
	if err != nil {
		t.Fatalf("listing a status with no jobs: %v", err)
	}
	if len(none.Jobs) != 0 || none.HasMore {
		t.Errorf("a status with no jobs returned %v", idsOf(none))
	}

	if _, err := service.Jobs(t.Context(), pool, customer, JobQuery{Status: "Nonsense"}); !errors.Is(err, ErrInvalidStatus) {
		t.Errorf("filtering on a status that does not exist = %v, want ErrInvalidStatus", err)
	}
}

// TestPagingReachesEveryJobExactlyOnce is the boundary the whole keyset design exists for.
//
// Five jobs in pages of two, walked to the end: every job appears once and none is skipped. The
// page size divides unevenly on purpose — a limit that divides the set exactly hides the
// off-by-one at the last page, which is where it lives.
func TestPagingReachesEveryJobExactlyOnce(t *testing.T) {
	pool := pgtest.DB(t)
	service := newDraftService(t, &fakeGeocoder{})

	customer := newCustomer(t, pool, "list-paging@example.com", "+61400000663")
	created := draftsFor(t, pool, customer, 5)

	seen := map[uuid.UUID]int{}
	var order []uuid.UUID

	query := JobQuery{Limit: 2}
	for pages := 0; ; pages++ {
		if pages > 5 {
			t.Fatal("paging did not terminate; the cursor is not advancing")
		}

		page, err := service.Jobs(t.Context(), pool, customer, query)
		if err != nil {
			t.Fatalf("page %d: %v", pages, err)
		}
		for _, job := range page.Jobs {
			seen[job.ID]++
			order = append(order, job.ID)
		}
		if !page.HasMore {
			if !page.Next.IsZero() {
				t.Error("the last page carries a cursor to a page that does not exist")
			}
			break
		}
		if len(page.Jobs) != 2 {
			t.Fatalf("a page before the last has %d jobs, want the limit of 2", len(page.Jobs))
		}
		query.After = page.Next
	}

	if len(order) != 5 {
		t.Fatalf("paging returned %d jobs, want 5: %v", len(order), order)
	}
	for _, id := range created {
		if seen[id] != 1 {
			t.Errorf("%s appeared %d times across the pages, want exactly 1", id, seen[id])
		}
	}
	for i := range order[1:] {
		if order[i] == order[i+1] {
			t.Errorf("the same job came back twice in a row at %d", i)
		}
	}
}

// The filter and the cursor have to work together: a page-two request that dropped the filter
// would return jobs the caller did not ask for, and one that dropped the cursor would loop.
func TestPagingKeepsTheStatusFilter(t *testing.T) {
	pool := pgtest.DB(t)
	service := newDraftService(t, &fakeGeocoder{})

	customer := newCustomer(t, pool, "list-paging-filter@example.com", "+61400000664")
	jobs := draftsFor(t, pool, customer, 4)
	for _, job := range jobs[:3] {
		publish(t, pool, job, customer)
	}

	query := JobQuery{Status: StatusOpen, Limit: 2}
	first, err := service.Jobs(t.Context(), pool, customer, query)
	if err != nil {
		t.Fatalf("the first page: %v", err)
	}
	if !first.HasMore || len(first.Jobs) != 2 {
		t.Fatalf("the first page has %d jobs, has_more=%v", len(first.Jobs), first.HasMore)
	}

	query.After = first.Next
	second, err := service.Jobs(t.Context(), pool, customer, query)
	if err != nil {
		t.Fatalf("the second page: %v", err)
	}
	if len(second.Jobs) != 1 || second.HasMore {
		t.Fatalf("the second page has %d jobs, has_more=%v, want the last Open job", len(second.Jobs), second.HasMore)
	}
	if second.Jobs[0].Status != StatusOpen {
		t.Errorf("the second page dropped the filter: it carries a %s job", second.Jobs[0].Status)
	}
	if second.Jobs[0].ID == first.Jobs[0].ID || second.Jobs[0].ID == first.Jobs[1].ID {
		t.Error("the second page repeated a job from the first; the cursor was not applied")
	}
}

// A limit above the maximum is narrowed rather than refused, and one below is honoured. Refusing
// would turn a server-side tuning change into a broken client.
func TestTheLimitIsClampedRatherThanRefused(t *testing.T) {
	pool := pgtest.DB(t)
	service := newDraftService(t, &fakeGeocoder{})

	customer := newCustomer(t, pool, "list-limit@example.com", "+61400000665")
	draftsFor(t, pool, customer, 3)

	one, err := service.Jobs(t.Context(), pool, customer, JobQuery{Limit: 1})
	if err != nil {
		t.Fatalf("listing: %v", err)
	}
	if len(one.Jobs) != 1 || !one.HasMore {
		t.Errorf("limit 1 returned %d jobs, has_more=%v", len(one.Jobs), one.HasMore)
	}

	huge, err := service.Jobs(t.Context(), pool, customer, JobQuery{Limit: 10_000})
	if err != nil {
		t.Fatalf("listing with an enormous limit: %v", err)
	}
	if len(huge.Jobs) != 3 || huge.HasMore {
		t.Errorf("an enormous limit returned %d jobs, has_more=%v", len(huge.Jobs), huge.HasMore)
	}
}

// A customer with no jobs gets an empty page rather than an error, and the cursor stays zero.
func TestAnEmptyListIsAPageWithNothingInIt(t *testing.T) {
	pool := pgtest.DB(t)
	service := newDraftService(t, &fakeGeocoder{})

	customer := newCustomer(t, pool, "list-empty@example.com", "+61400000666")

	page, err := service.Jobs(t.Context(), pool, customer, JobQuery{})
	if err != nil {
		t.Fatalf("listing: %v", err)
	}
	if len(page.Jobs) != 0 || page.HasMore || !page.Next.IsZero() {
		t.Errorf("an empty list is %+v", page)
	}
}
