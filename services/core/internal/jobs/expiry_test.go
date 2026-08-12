package jobs

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/DulsaraNethmin/Shipper/services/core/internal/db"
	"github.com/DulsaraNethmin/Shipper/services/core/internal/testsupport/pgtest"
)

// SHIP-68 — the deadline half. The sweep that acts on it is cmd/worker's, and is tested there.
//
// Docs/02 §6.3: an Open job leaves Open at the earlier of fourteen days after publication or its
// pickup date passing. These tests are against a real PostgreSQL because most of the rule is a
// trigger (000406), and Docs/06 §4.1 is the argument: "a mock happily accepts a write that the
// actual constraint would reject" — and would happily accept a job published with no deadline
// at all, which is the failure the trigger exists to make impossible.

// deadlineOf reads a job's expires_at straight out of the table, bypassing the domain.
func deadlineOf(t *testing.T, pool *pgxpool.Pool, job uuid.UUID) *time.Time {
	t.Helper()

	var at *time.Time
	if err := pool.QueryRow(t.Context(),
		`SELECT expires_at FROM jobs WHERE id = $1`, job).Scan(&at); err != nil {
		t.Fatalf("reading the deadline of %s: %v", job, err)
	}
	return at
}

// publishedAt is when the platform recorded a job becoming Open.
//
// The trigger computes its fourteen days from now(), which inside a transaction is the transaction
// start time — the same instant job_status_history.server_recorded_at defaults from. So the two are
// exactly equal rather than nearly, and the assertions below need no tolerance.
func publishedAt(t *testing.T, pool *pgxpool.Pool, job uuid.UUID) time.Time {
	t.Helper()

	var at time.Time
	if err := pool.QueryRow(t.Context(),
		`SELECT server_recorded_at FROM job_status_history
		  WHERE job_id = $1 AND to_status = 'Open' ORDER BY server_recorded_at DESC LIMIT 1`,
		job).Scan(&at); err != nil {
		t.Fatalf("reading when %s was published: %v", job, err)
	}
	return at
}

// TestADraftHasNoDeadline is the boundary the fourteen days is counted from.
//
// Docs/02 §6.3 starts the clock at publication, and Docs/01 §4.1 lets a draft sit indefinitely.
// A draft carrying a deadline would be a job that expired before anybody could see it.
func TestADraftHasNoDeadline(t *testing.T) {
	pool := pgtest.DB(t)

	customer := newCustomer(t, pool, "expiry-draft@example.com", "+61400000680")
	job := newDraft(t, pool, customer)

	if at := deadlineOf(t, pool, job); at != nil {
		t.Errorf("a draft has a deadline of %s, want none", at)
	}
}

// TestPublishingGivesAJobTheFourteenDayBackstop is the second half of Docs/02 §6.3's "earlier of".
//
// A job with no pickup window has no pickup date to pass, so only the backstop applies. That is
// the case the "operative rule" cannot cover, and it is why the backstop exists.
func TestPublishingGivesAJobTheFourteenDayBackstop(t *testing.T) {
	pool := pgtest.DB(t)

	customer := newCustomer(t, pool, "expiry-backstop@example.com", "+61400000681")
	job := newDraft(t, pool, customer)
	publish(t, pool, job, customer)

	at := deadlineOf(t, pool, job)
	if at == nil {
		t.Fatal("a published job has no deadline, so nothing would ever expire it")
	}
	if want := publishedAt(t, pool, job).Add(14 * 24 * time.Hour); !at.Equal(want) {
		t.Errorf("deadline = %s, want %s — fourteen days after publication (Docs/02 §6.3)", at, want)
	}
}

// TestAPickupDateBeforeTheBackstopWins is the operative half of Docs/02 §6.3.
//
// "A job whose pickup window has gone is dead regardless of how recently it was posted." The
// fourteen days is only a backstop for jobs with distant dates, which the second case here keeps
// honest: a pickup window beyond the backstop must not extend the job past it.
func TestAPickupDateBeforeTheBackstopWins(t *testing.T) {
	pool := pgtest.DB(t)
	service := newDraftService(t, &fakeGeocoder{})

	customer := newCustomer(t, pool, "expiry-pickup@example.com", "+61400000682")

	soon := time.Now().UTC().Add(48 * time.Hour).Truncate(time.Millisecond)
	distant := time.Now().UTC().Add(60 * 24 * time.Hour).Truncate(time.Millisecond)

	cases := map[string]struct {
		end  time.Time
		wins bool
	}{
		"a pickup window closing in two days": {soon, true},
		"a pickup window sixty days away":     {distant, false},
	}

	for name, c := range cases {
		created, err := service.CreateDraft(t.Context(), pool, customer, DraftFields{
			PickupWindow: &TimeWindow{End: c.end},
		})
		if err != nil {
			t.Fatalf("%s: creating the job: %v", name, err)
		}
		publish(t, pool, created.ID, customer)

		at := deadlineOf(t, pool, created.ID)
		if at == nil {
			t.Fatalf("%s: the published job has no deadline", name)
		}

		want := c.end
		if !c.wins {
			want = publishedAt(t, pool, created.ID).Add(14 * 24 * time.Hour)
		}
		if !at.Equal(want) {
			t.Errorf("%s: deadline = %s, want %s", name, at, want)
		}
	}
}

// TestTheDeadlineIsSetOnceAndSurvivesTheJobLeavingOpen keeps Docs/02 §6.3 counting from
// *publication* rather than from the most recent time a job happened to be Open.
//
// Negotiating → Open is an ordinary part of a job's life: it happens whenever the last active bid
// expires or is withdrawn (Docs/02 §2). If the deadline were recomputed there, a job with a slow
// trickle of bids would never expire at all — which is exactly the stale listing §6.3 is about.
//
// The extension in the middle is SHIP-70's mechanism, checked here because 000406 is what has to
// leave it alone: the trigger fills a NULL and never overwrites a value, so a deadline somebody
// moved deliberately stays moved.
func TestTheDeadlineIsSetOnceAndSurvivesTheJobLeavingOpen(t *testing.T) {
	pool := pgtest.DB(t)

	customer := newCustomer(t, pool, "expiry-cycle@example.com", "+61400000683")
	job := newDraft(t, pool, customer)
	publish(t, pool, job, customer)

	original := deadlineOf(t, pool, job)
	if original == nil {
		t.Fatal("the published job has no deadline")
	}

	// An extension, as SHIP-70 will make it: an ordinary UPDATE that does not touch status,
	// which 000402's guard lets straight through.
	extended := original.Add(7 * 24 * time.Hour)
	if _, err := pool.Exec(t.Context(),
		`UPDATE jobs SET expires_at = $2 WHERE id = $1`, job, extended); err != nil {
		t.Fatalf("extending the deadline: %v", err)
	}

	move(t, pool, job, StatusNegotiating, User(ActorCustomer, customer))
	move(t, pool, job, StatusOpen, User(ActorCustomer, customer))

	after := deadlineOf(t, pool, job)
	if after == nil {
		t.Fatal("the job lost its deadline on the way back to Open")
	}
	if !after.Equal(extended) {
		t.Errorf("deadline = %s after returning to Open, want the extended %s — the clock does "+
			"not restart every time a job is offered again", after, extended)
	}
}

// TestExpiryClaimTakesOnlyWhatIsDue holds the claim to its predicate.
//
// The rows it must not take are the ones that would each be a distinct defect: a draft (never
// published, so never expiring), an Open job whose deadline is still ahead of it, and a job that
// has left Open (which is somebody's live delivery).
func TestExpiryClaimTakesOnlyWhatIsDue(t *testing.T) {
	pool := pgtest.DB(t)

	customer := newCustomer(t, pool, "expiry-claim@example.com", "+61400000684")

	due := newDraft(t, pool, customer)
	publish(t, pool, due, customer)
	overdue := newDraft(t, pool, customer)
	publish(t, pool, overdue, customer)

	notYet := newDraft(t, pool, customer)
	publish(t, pool, notYet, customer)

	draft := newDraft(t, pool, customer)

	awarded := newDraft(t, pool, customer)
	publish(t, pool, awarded, customer)
	move(t, pool, awarded, StatusAwarded, User(ActorCustomer, customer))

	// Deadlines placed by hand, because the fourteen days is not something a test can wait for.
	// The two due jobs are given different ones so that "longest overdue first" is a total
	// order rather than an accident of insertion.
	now := time.Now().UTC()
	setDeadline(t, pool, overdue, now.Add(-72*time.Hour))
	setDeadline(t, pool, due, now.Add(-time.Hour))
	setDeadline(t, pool, notYet, now.Add(72*time.Hour))
	setDeadline(t, pool, awarded, now.Add(-72*time.Hour))

	var claimed []uuid.UUID
	if err := db.InTx(t.Context(), pool, func(ctx context.Context, r db.Runner) error {
		rows, err := r.Query(ctx, ExpiryClaim, now, ExpiryBatch)
		if err != nil {
			return err
		}
		claimed, err = pgx.CollectRows(rows, pgx.RowTo[uuid.UUID])
		return err
	}); err != nil {
		t.Fatalf("running the claim: %v", err)
	}

	if len(claimed) != 2 || claimed[0] != overdue || claimed[1] != due {
		t.Errorf("the claim took %v, want the two due jobs longest-overdue first: [%s %s].\n"+
			"draft=%s not-yet-due=%s awarded=%s", claimed, overdue, due, draft, notYet, awarded)
	}
}

// TestExpireEndsAnOpenJobAsThePlatform is what one claimed job becomes.
//
// Docs/02 §2's table has one Open → Cancelled row and describes it as "job expires unclaimed".
// Everything here goes through the guard: the history row 000402 requires, the actor, the reason,
// and the event — so an expiry is indistinguishable in the record from any other transition except
// for who made it and why.
func TestExpireEndsAnOpenJobAsThePlatform(t *testing.T) {
	pool := pgtest.DB(t)
	sink := &recordingSink{}
	service := newTestService(sink)

	customer := newCustomer(t, pool, "expiry-expire@example.com", "+61400000685")
	job := newDraft(t, pool, customer)
	publish(t, pool, job, customer)

	var expired Job
	if err := db.InTx(t.Context(), pool, func(ctx context.Context, r db.Runner) error {
		var err error
		expired, err = service.Expire(ctx, r, job)
		return err
	}); err != nil {
		t.Fatalf("expiring the job: %v", err)
	}

	if expired.Status != StatusCancelled {
		t.Errorf("the expired job is %s, want Cancelled", expired.Status)
	}
	if statusOf(t, pool, job) != StatusCancelled {
		t.Error("the row did not move")
	}

	history, err := service.History(t.Context(), pool, job)
	if err != nil {
		t.Fatalf("reading the history: %v", err)
	}
	last := history[len(history)-1]

	switch {
	case last.From != StatusOpen || last.To != StatusCancelled:
		t.Errorf("the recorded move is %s -> %s, want Open -> Cancelled", last.From, last.To)
	case last.Actor.Type != ActorSystem:
		t.Errorf("the actor is %s, want the platform", last.Actor.Type)
	case last.Actor.ID != uuid.Nil:
		// The platform has no account and must not claim one. Move.validate refuses it,
		// and this is the assertion that would notice if that stopped being true.
		t.Errorf("the platform's transition names account %s", last.Actor.ID)
	case last.Reason != ExpiryReason:
		t.Errorf("the reason is %q, want %q", last.Reason, ExpiryReason)
	}

	if len(sink.emitted) != 1 || sink.emitted[0].Type != EventStatusChanged {
		t.Errorf("emitted %d events, want one %s — a state change nothing downstream hears "+
			"about is what the outbox seam exists to prevent", len(sink.emitted), EventStatusChanged)
	}
}

// TestExpireRefusesAJobThatIsNotOpen leans on Docs/02 §2 rather than on a list of its own.
//
// The claim already excludes anything but Open, so this is a second line rather than the first —
// and it is the line that would matter if a future caller expired a job it had chosen some other
// way.
func TestExpireRefusesAJobThatIsNotOpen(t *testing.T) {
	pool := pgtest.DB(t)
	service := newTestService(&recordingSink{})

	customer := newCustomer(t, pool, "expiry-refuse@example.com", "+61400000686")
	job := newDraft(t, pool, customer)
	publish(t, pool, job, customer)
	move(t, pool, job, StatusAwarded, User(ActorCustomer, customer))

	err := db.InTx(t.Context(), pool, func(ctx context.Context, r db.Runner) error {
		_, err := service.Expire(ctx, r, job)
		return err
	})
	if err == nil {
		t.Fatal("an awarded job was expired; Docs/02 §6.2 makes ending it a support matter")
	}
	if statusOf(t, pool, job) != StatusAwarded {
		t.Error("the refused expiry moved the job anyway")
	}
}

// --- fixtures ----------------------------------------------------------------------------------

// setDeadline puts a job's expires_at where a test needs it.
//
// A plain UPDATE, which 000402's guard permits because it does not touch status and 000406's
// trigger ignores because the job is already Open. That is the same statement SHIP-70's extend
// endpoint will make, which is why the fixture is honest rather than a way around anything.
func setDeadline(t *testing.T, pool *pgxpool.Pool, job uuid.UUID, at time.Time) {
	t.Helper()

	if _, err := pool.Exec(t.Context(),
		`UPDATE jobs SET expires_at = $2 WHERE id = $1`, job, at); err != nil {
		t.Fatalf("setting the deadline of %s: %v", job, err)
	}
}
