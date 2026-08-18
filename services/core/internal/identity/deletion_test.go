package identity

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/DulsaraNethmin/Shipper/services/core/internal/clock"
	"github.com/DulsaraNethmin/Shipper/services/core/internal/db"
	"github.com/DulsaraNethmin/Shipper/services/core/internal/passwords"
	"github.com/DulsaraNethmin/Shipper/services/core/internal/testsupport/pgtest"
)

// SHIP-169 against a real PostgreSQL, per Docs/06 §4.1.
//
// # What these tests are shaped against
//
// The *Done when* is "a signed-in user can request deletion and receives a completion date", and
// the trivially passing implementation of that returns `now + 30 days` while rendering the
// response. It satisfies every reading of the sentence, answers with a plausible date every time,
// and leaves the platform holding no record of what it promised — so the date the person was told
// quietly becomes whichever day they last asked.
//
// **So the assertions below are on the stored row read back after the clock has moved**, not on
// what the function returned. A return value is the same shape under both implementations; a row
// is not.

// deletionRequestRow reads what the database actually holds for an account.
//
// It reads the columns directly rather than going back through the store, so the assertion is
// about the row rather than about the projection the code under test also uses.
func deletionRequestRow(t *testing.T, pool *pgxpool.Pool, userID uuid.UUID) (state string, requestedAt, completeBy time.Time) {
	t.Helper()

	if err := pool.QueryRow(t.Context(), `
		SELECT state, requested_at, complete_by
		  FROM account_deletion_requests
		 WHERE user_id = $1`, userID).Scan(&state, &requestedAt, &completeBy); err != nil {
		t.Fatalf("reading the stored deletion request: %v", err)
	}
	return state, requestedAt.UTC(), completeBy.UTC()
}

// deletionRequestCount is the side effect that separates one request from two.
func deletionRequestCount(t *testing.T, pool *pgxpool.Pool, userID uuid.UUID) int {
	t.Helper()

	var n int
	if err := pool.QueryRow(t.Context(),
		`SELECT count(*) FROM account_deletion_requests WHERE user_id = $1`,
		userID).Scan(&n); err != nil {
		t.Fatalf("counting deletion requests: %v", err)
	}
	return n
}

// deletionRequestedAt is the fixed instant these tests start from. A literal rather than
// time.Now(), so the arithmetic below can be written out and read rather than recomputed.
var deletionRequestedAt = time.Date(2026, 8, 16, 9, 30, 0, 0, time.UTC)

// TestRequestDeletionRecordsTheCompletionDate is SHIP-169's acceptance criterion, both halves: the
// request exists, and the person is told when it completes.
func TestRequestDeletionRecordsTheCompletionDate(t *testing.T) {
	clk := clock.NewFixed(deletionRequestedAt)
	svc, pool, _ := newTestServiceWithSMS(t, clk)
	user := registerFor(t, svc, "1690")

	request, created, err := svc.RequestDeletion(t.Context(), user.ID)
	if err != nil {
		t.Fatalf("requesting deletion: %v", err)
	}

	if !created {
		t.Error("the first request reports that it already existed")
	}

	t.Run("the caller is told a completion date thirty days out", func(t *testing.T) {
		want := deletionRequestedAt.Add(30 * 24 * time.Hour)
		if !request.CompleteBy.Equal(want) {
			t.Errorf("CompleteBy = %s, want %s — Docs/05 §3.1 commits to thirty days",
				request.CompleteBy, want)
		}
		if !request.RequestedAt.Equal(deletionRequestedAt) {
			t.Errorf("RequestedAt = %s, want %s", request.RequestedAt, deletionRequestedAt)
		}
		if request.State != DeletionRequested {
			t.Errorf("State = %q, want %q", request.State, DeletionRequested)
		}
		if request.ID.Version() != 7 {
			t.Errorf("the identifier is not a UUIDv7: %s", request.ID)
		}
	})

	// The row, not the return value. This is the half that a completion date computed while
	// rendering the response cannot satisfy, because it never writes one.
	t.Run("and the date is in the row, not only in the answer", func(t *testing.T) {
		state, requestedAt, completeBy := deletionRequestRow(t, pool, user.ID)

		if state != string(DeletionRequested) {
			t.Errorf("the stored state is %q, want %q", state, DeletionRequested)
		}
		if !requestedAt.Equal(deletionRequestedAt) {
			t.Errorf("the stored requested_at is %s, want %s", requestedAt, deletionRequestedAt)
		}
		if !completeBy.Equal(request.CompleteBy) {
			t.Errorf("the stored complete_by is %s and the caller was told %s; the platform is "+
				"holding a different date from the one it promised", completeBy, request.CompleteBy)
		}
	})
}

// TestTheCompletionDateDoesNotMoveWithTheClock is the assertion the ticket exists for.
//
// **Mutation-checked.** Answering with `s.clock.Now().Add(DeletionWindow)` instead of the stored
// `complete_by` — a completion date computed at read time rather than recorded when the request
// was made — makes this fail: the second call returns a date ten days later than the first, and
// the row it is supposedly reporting has not moved.
//
// The clock is advanced between the two calls rather than the two calls being made back to back,
// because with a fixed clock a recomputed date and a recorded one are the same value. That is the
// fixture trap wave 10 recorded: a mutation survives when the fixture cannot tell the two
// implementations apart.
func TestTheCompletionDateDoesNotMoveWithTheClock(t *testing.T) {
	clk := clock.NewFixed(deletionRequestedAt)
	svc, pool, _ := newTestServiceWithSMS(t, clk)
	user := registerFor(t, svc, "1691")

	first, _, err := svc.RequestDeletion(t.Context(), user.ID)
	if err != nil {
		t.Fatalf("the first request: %v", err)
	}

	clk.Advance(10 * 24 * time.Hour)

	second, created, err := svc.RequestDeletion(t.Context(), user.ID)
	if err != nil {
		t.Fatalf("the second request: %v", err)
	}

	if created {
		t.Error("asking twice created a second request")
	}

	t.Run("the caller is told the same date they were told the first time", func(t *testing.T) {
		if !second.CompleteBy.Equal(first.CompleteBy) {
			t.Errorf("the completion date moved from %s to %s after ten days passed; it is being "+
				"computed at read time rather than read from the row the person was promised",
				first.CompleteBy, second.CompleteBy)
		}
		if second.ID != first.ID {
			t.Errorf("the second answer names request %s and the first named %s", second.ID, first.ID)
		}
		if !second.RequestedAt.Equal(deletionRequestedAt) {
			t.Errorf("RequestedAt = %s, want the instant of the first request, %s",
				second.RequestedAt, deletionRequestedAt)
		}
	})

	// The stored row after the clock has moved, which is what separates "the answer is stable"
	// from "the promise is stable". A recomputed date could be made to agree with itself.
	t.Run("and the row still holds the original promise", func(t *testing.T) {
		_, requestedAt, completeBy := deletionRequestRow(t, pool, user.ID)

		want := deletionRequestedAt.Add(30 * 24 * time.Hour)
		if !completeBy.Equal(want) {
			t.Errorf("the stored complete_by is %s, want %s — the row was rewritten by a repeat "+
				"request", completeBy, want)
		}
		if !requestedAt.Equal(deletionRequestedAt) {
			t.Errorf("the stored requested_at is %s, want %s", requestedAt, deletionRequestedAt)
		}
	})
}

// TestAskingTwiceLeavesOneRequest is the observed side effect rather than the returned value.
//
// A repeat that answered correctly and wrote a second row would pass every assertion above: both
// calls would report the same date, because both would read a row, and the account would quietly
// hold two promises. The count is what tells the two apart.
func TestAskingTwiceLeavesOneRequest(t *testing.T) {
	clk := clock.NewFixed(deletionRequestedAt)
	svc, pool, _ := newTestServiceWithSMS(t, clk)
	user := registerFor(t, svc, "1692")

	for attempt := 1; attempt <= 3; attempt++ {
		if _, _, err := svc.RequestDeletion(t.Context(), user.ID); err != nil {
			t.Fatalf("request %d: %v", attempt, err)
		}
		clk.Advance(time.Hour)
	}

	if n := deletionRequestCount(t, pool, user.ID); n != 1 {
		t.Errorf("%d deletion requests after asking three times, want 1 — "+
			"uq_account_deletion_requests_open is what makes 'the' open request unambiguous", n)
	}
}

// TestRequestDeletionIsScopedToTheAccountThatAsked.
//
// The identifier comes from a signed token today, so a caller cannot name somebody else's account
// through the endpoint — which is exactly why the scoping is worth a test rather than being
// assumed from the handler. One account asking must not put another account's row in front of it,
// and must not stop another account asking.
func TestRequestDeletionIsScopedToTheAccountThatAsked(t *testing.T) {
	clk := clock.NewFixed(deletionRequestedAt)
	svc, pool, _ := newTestServiceWithSMS(t, clk)

	one := registerFor(t, svc, "1693")
	two := registerFor(t, svc, "1694")

	first, _, err := svc.RequestDeletion(t.Context(), one.ID)
	if err != nil {
		t.Fatalf("the first account: %v", err)
	}

	clk.Advance(48 * time.Hour)

	second, created, err := svc.RequestDeletion(t.Context(), two.ID)
	if err != nil {
		t.Fatalf("the second account: %v", err)
	}

	if !created {
		t.Fatal("the second account was handed the first account's request instead of making " +
			"its own")
	}
	if second.ID == first.ID || second.UserID != two.ID {
		t.Errorf("the second account's request is %s for user %s; the first was %s for %s",
			second.ID, second.UserID, first.ID, first.UserID)
	}

	// Each account's own date, from its own request, unaffected by the other's.
	if _, _, completeBy := deletionRequestRow(t, pool, one.ID); !completeBy.Equal(first.CompleteBy) {
		t.Errorf("the first account's stored completion date is %s, want %s", completeBy, first.CompleteBy)
	}
	if _, _, completeBy := deletionRequestRow(t, pool, two.ID); !completeBy.Equal(second.CompleteBy) {
		t.Errorf("the second account's stored completion date is %s, want %s", completeBy, second.CompleteBy)
	}
}

// TestTheResponseRendersTheStoredDate is the transport half of the same rule, and it is here
// because nothing else in the repository covers it.
//
// The service can read the row back perfectly and the handler can still render `time.Now()` on the
// way out, and **no other Go test would notice**: cmd/api's identity tests are deliberately
// database-free, so none of them ever produces a request to render, and
// TestResponsesMatchTheContract exercises only public GETs with no path parameter. That leaves the
// defect to `make verify`, which is a long way from the code. This is the layer in between,
// written as a pure function of a request so it needs neither a database nor a router.
//
// The instant is deliberately years in the past. A fixture near the current date would render to
// something a recomputed `now + 30 days` differs from only in the hours — true, but a difference a
// reader has to squint at, and one that would start agreeing if the fixture ever moved.
func TestTheResponseRendersTheStoredDate(t *testing.T) {
	promised := time.Date(2020, 1, 1, 8, 15, 0, 0, time.UTC)
	req := DeletionRequest{
		ID:          uuid.New(),
		UserID:      uuid.New(),
		State:       DeletionRequested,
		RequestedAt: promised,
		CompleteBy:  promised.Add(DeletionWindow),
	}

	got := deletionRequestFrom(req)

	if got.CompletesBy != timestamp(req.CompleteBy) {
		t.Errorf("completes_by rendered as %q and the request holds %q; the response is not "+
			"reporting the date that was recorded", got.CompletesBy, timestamp(req.CompleteBy))
	}
	if got.RequestedAt != timestamp(req.RequestedAt) {
		t.Errorf("requested_at rendered as %q, want %q", got.RequestedAt, timestamp(req.RequestedAt))
	}
	if got.ID != req.ID.String() {
		t.Errorf("id rendered as %q, want %q", got.ID, req.ID)
	}
	if got.State != string(DeletionRequested) {
		t.Errorf("state rendered as %q, want %q", got.State, DeletionRequested)
	}
}

// TestDeletionWindowIsThirtyDays pins the published figure.
//
// A constant read by a test is a weak guard on its own — wave 10's note that "a pairing guard is a
// text guard" applies — so this is deliberately not the ticket's evidence. It is here because the
// figure appears in a privacy policy, a store listing and `contracts/paths/identity.yaml`, and a
// change to it should fail something that names the document rather than only shifting an
// expectation.
func TestDeletionWindowIsThirtyDays(t *testing.T) {
	if DeletionWindow != 30*24*time.Hour {
		t.Errorf("DeletionWindow = %s, want 720h — Docs/05 §3.1 commits to executing within "+
			"thirty days and tells the person so", DeletionWindow)
	}
}

// TestRequestDeletionWithoutADatabaseIsUnavailable.
//
// The pool is nil-able by design (see the note on Deps in cmd/api), and the difference between 503
// and 500 is what tells a mobile client to retry rather than to give up.
func TestRequestDeletionWithoutADatabaseIsUnavailable(t *testing.T) {
	svc, _, _ := newTestServiceWithSMS(t, clock.NewFixed(deletionRequestedAt))
	svc.pool = nil

	if _, _, err := svc.RequestDeletion(t.Context(), uuid.Nil); err != errUnavailable {
		t.Errorf("err = %v, want errUnavailable", err)
	}
}

// --- SHIP-170: the deferral -----------------------------------------------------------------

// noDelivery is the [ActiveJobs] every test that is not about the deferral runs with.
//
// A package-level value rather than a literal at each call site, so that the service helpers in
// register_test.go read as "nobody in this test is carrying a delivery" rather than as a struct
// somebody has to decode.
//
// **Its own stateless type rather than a zero [fakeActiveJobs]**, because it is shared by every
// test in the package and `fakeActiveJobs` counts its calls — one shared counter written from
// several tests is a data race `go test -race` would find, and a fixture that fails the race
// detector teaches nothing about the code.
var noDelivery = noActiveJobs{}

type noActiveJobs struct{}

func (noActiveJobs) HasActiveJob(context.Context, db.Runner, uuid.UUID) (bool, error) {
	return false, nil
}

// fakeActiveJobs is the port's test double, and it is deliberately controllable *between* calls.
//
// The deferral is re-evaluated on every request, so a double that answered one fixed value could
// only ever demonstrate half the ticket: what makes "queues until the job closes" a claim rather
// than a state is that the same account gets a different answer once the delivery ends. `active` is
// therefore a field a test moves, and `calls` is what proves the port was asked again rather than
// remembered.
//
// The **real** statement — the one that decides which jobs and which parties count — is exercised
// in cmd/api against a real database (TestTheActiveJobLookupSeesBothParties). A fake here would
// otherwise be a test of the domain's response and of nothing else, which is the shape wave 12
// recorded as "a mutation killed by the wrong layer".
type fakeActiveJobs struct {
	active bool
	err    error
	calls  int
}

func (f *fakeActiveJobs) HasActiveJob(context.Context, db.Runner, uuid.UUID) (bool, error) {
	f.calls++
	if f.err != nil {
		return false, f.err
	}
	return f.active, nil
}

// newDeletionService is newTestServiceWithSMS with the active-job port under the test's control.
func newDeletionService(t *testing.T, clk clock.Clock, jobs ActiveJobs) (*Service, *pgxpool.Pool) {
	t.Helper()

	pool := pgtest.DB(t)

	hasher, err := passwords.NewHasher(testProfile)
	if err != nil {
		t.Fatalf("building the hasher: %v", err)
	}

	svc, err := NewService(pool, hasher, testServiceIssuer(t, clk), testLimiter(t),
		&recordingSender{}, &recordingTexter{}, jobs, clk)
	if err != nil {
		t.Fatalf("building the service: %v", err)
	}
	return svc, pool
}

// deletionRequestState is the stored state alone, for the assertions that are about the row rather
// than about the answer.
func deletionRequestState(t *testing.T, pool *pgxpool.Pool, userID uuid.UUID) string {
	t.Helper()

	state, _, _ := deletionRequestRow(t, pool, userID)
	return state
}

// TestARequestDuringADeliveryIsDeferred is SHIP-170's acceptance criterion, first half.
//
// **Deferred, not refused**, which is Docs/05 §3.1's own word and the thing a plausible
// implementation gets wrong: refusing would answer an error, record nothing, and leave a person
// carrying a delivery unable to ask at all — Apple requires in-app deletion to be *offered*.
//
// The assertion is on the stored row as well as on the answer, for deletion_test.go's standing
// reason: a return value is the same shape under an implementation that wrote the state and one
// that decorated the response on the way out.
func TestARequestDuringADeliveryIsDeferred(t *testing.T) {
	jobs := &fakeActiveJobs{active: true}
	svc, pool := newDeletionService(t, clock.NewFixed(deletionRequestedAt), jobs)
	user := registerFor(t, svc, "1700")

	request, created, err := svc.RequestDeletion(t.Context(), user.ID)
	if err != nil {
		t.Fatalf("requesting deletion during a delivery: %v", err)
	}

	if !created {
		t.Error("the request was not created; a deferral is a request that exists, not one that was refused")
	}
	if request.State != DeletionDeferred {
		t.Errorf("State = %q, want %q — Docs/05 §3.1 defers a request made between Awarded and "+
			"Delivered rather than refusing it", request.State, DeletionDeferred)
	}
	if jobs.calls != 1 {
		t.Errorf("the active-job port was asked %d times, want 1", jobs.calls)
	}

	if state := deletionRequestState(t, pool, user.ID); state != string(DeletionDeferred) {
		t.Errorf("the stored state is %q, want %q — the deferral has to be in the row, or "+
			"SHIP-171 has nothing to read before it executes", state, DeletionDeferred)
	}
}

// TestTheDeferralExplainsWhy is the criterion's second half — "and explains why".
//
// On the rendered response rather than on the constant, because a state carried in a field no
// response includes explains nothing to anybody. The assertion is in **both** directions: a live
// request must carry no explanation, or a client cannot tell the two apart by the field's presence,
// which is what `omitempty` is for.
func TestTheDeferralExplainsWhy(t *testing.T) {
	promised := time.Date(2020, 1, 1, 8, 15, 0, 0, time.UTC)
	base := DeletionRequest{
		ID:          uuid.New(),
		UserID:      uuid.New(),
		RequestedAt: promised,
		CompleteBy:  promised.Add(DeletionWindow),
	}

	t.Run("a deferred request says why it is waiting", func(t *testing.T) {
		deferred := base
		deferred.State = DeletionDeferred

		got := deletionRequestFrom(deferred)

		if got.State != string(DeletionDeferred) {
			t.Errorf("state rendered as %q, want %q", got.State, DeletionDeferred)
		}
		if got.DeferralReason != DeferralReason {
			t.Errorf("deferral_reason = %q, want the platform's own words", got.DeferralReason)
		}
		// The explanation has to be about the deferral rather than about the delivery.
		// A sentence naming a job would put job data on a screen both halves of the
		// marketplace reach — Docs/01 §4.3 — and there is nothing a person could do with it.
		for _, forbidden := range []string{"job", "Job", "$"} {
			if strings.Contains(got.DeferralReason, forbidden) {
				t.Errorf("the deferral explanation contains %q: %q", forbidden, got.DeferralReason)
			}
		}
	})

	t.Run("a live request carries no explanation at all", func(t *testing.T) {
		live := base
		live.State = DeletionRequested

		got := deletionRequestFrom(live)

		if got.DeferralReason != "" {
			t.Errorf("deferral_reason = %q on a request that is not deferred; a client "+
				"branching on the field being present would render an explanation for "+
				"a request that has none", got.DeferralReason)
		}
	})
}

// TestTheDeferralLiftsWhenTheDeliveryCloses is "queues until the job closes", which is the clause
// no single call can demonstrate.
//
// Three things are asserted and the third is the one worth having: the state moves, the completion
// date is re-recorded from the moment it moved, and **the account still holds exactly one request**.
// An implementation that answered correctly and inserted a second row would pass the first two and
// leave the platform holding two promises — the observed side effect is what tells them apart, and
// it is the shape wave 12 recorded as the strongest assertion available.
func TestTheDeferralLiftsWhenTheDeliveryCloses(t *testing.T) {
	clk := clock.NewFixed(deletionRequestedAt)
	jobs := &fakeActiveJobs{active: true}
	svc, pool := newDeletionService(t, clk, jobs)
	user := registerFor(t, svc, "1701")

	deferred, _, err := svc.RequestDeletion(t.Context(), user.ID)
	if err != nil {
		t.Fatalf("the deferred request: %v", err)
	}
	if deferred.State != DeletionDeferred {
		t.Fatalf("the fixture did not defer: State = %q", deferred.State)
	}

	// The delivery finishes, and time passes before the person asks again.
	clk.Advance(5 * 24 * time.Hour)
	jobs.active = false

	live, created, err := svc.RequestDeletion(t.Context(), user.ID)
	if err != nil {
		t.Fatalf("the request after the delivery closed: %v", err)
	}

	if created {
		t.Error("the deferral lifting created a second request")
	}
	if live.ID != deferred.ID {
		t.Errorf("the lifted request is %s and the deferred one was %s", live.ID, deferred.ID)
	}
	if live.State != DeletionRequested {
		t.Errorf("State = %q after the delivery closed, want %q — the queue is drained by "+
			"re-reading the request, so a state that never moves is a request that waits forever",
			live.State, DeletionRequested)
	}

	t.Run("the thirty days start when the deferral lifts", func(t *testing.T) {
		want := deletionRequestedAt.Add(5 * 24 * time.Hour).Add(DeletionWindow)
		if !live.CompleteBy.Equal(want) {
			t.Errorf("CompleteBy = %s, want %s — a deferred request's clock has not started, "+
				"so the window runs from the moment it does", live.CompleteBy, want)
		}
		if !live.RequestedAt.Equal(deletionRequestedAt) {
			t.Errorf("RequestedAt = %s, want the instant of the original request, %s — "+
				"when somebody asked is evidence and does not move", live.RequestedAt, deletionRequestedAt)
		}
	})

	t.Run("and the row says so", func(t *testing.T) {
		state, requestedAt, completeBy := deletionRequestRow(t, pool, user.ID)

		if state != string(DeletionRequested) {
			t.Errorf("the stored state is %q, want %q", state, DeletionRequested)
		}
		if !completeBy.Equal(live.CompleteBy) {
			t.Errorf("the row holds %s and the person was told %s", completeBy, live.CompleteBy)
		}
		if !requestedAt.Equal(deletionRequestedAt) {
			t.Errorf("the stored requested_at moved to %s", requestedAt)
		}
	})

	t.Run("one row, across the whole lifecycle", func(t *testing.T) {
		if n := deletionRequestCount(t, pool, user.ID); n != 1 {
			t.Errorf("%d deletion requests after a deferral lifted, want 1 — two rows are two "+
				"promises about one account, and whichever SHIP-171 read would be the one that counted", n)
		}
	})
}

// TestALiveRequestGoesBackOnHoldWhenADeliveryStarts is the other direction, and it is the one a
// reader would not assume.
//
// Docs/05 §3.1's sentence is about a request *made* during a delivery, but its reason —
// "erasing a party mid-delivery would strand the counterparty" — is about *executing* one. A person
// who asks to be deleted and then wins a job is in exactly the state the rule exists for, and a
// stored state that stayed live would tell them a date the platform must not keep. So the state is
// a fact about where the account stands now, re-read on every call, rather than a fact about the
// instant somebody tapped.
func TestALiveRequestGoesBackOnHoldWhenADeliveryStarts(t *testing.T) {
	clk := clock.NewFixed(deletionRequestedAt)
	jobs := &fakeActiveJobs{active: false}
	svc, pool := newDeletionService(t, clk, jobs)
	user := registerFor(t, svc, "1702")

	live, _, err := svc.RequestDeletion(t.Context(), user.ID)
	if err != nil {
		t.Fatalf("the first request: %v", err)
	}
	if live.State != DeletionRequested {
		t.Fatalf("the fixture did not start live: State = %q", live.State)
	}

	clk.Advance(2 * 24 * time.Hour)
	jobs.active = true

	held, created, err := svc.RequestDeletion(t.Context(), user.ID)
	if err != nil {
		t.Fatalf("the request after a delivery started: %v", err)
	}

	if created {
		t.Error("going on hold created a second request")
	}
	if held.State != DeletionDeferred {
		t.Errorf("State = %q after the account took on a delivery, want %q", held.State, DeletionDeferred)
	}
	if held.ID != live.ID {
		t.Errorf("the held request is %s and the original was %s", held.ID, live.ID)
	}
	if state := deletionRequestState(t, pool, user.ID); state != string(DeletionDeferred) {
		t.Errorf("the stored state is %q, want %q", state, DeletionDeferred)
	}
	if n := deletionRequestCount(t, pool, user.ID); n != 1 {
		t.Errorf("%d deletion requests, want 1", n)
	}
}

// TestADeferredRequestAskedAgainDoesNotMoveItsDate is SHIP-169's property, checked in the state
// SHIP-170 added.
//
// It is the assertion that separates "the date is written by an event" from "the date is worked out
// whenever somebody asks". The clock moves ten days between two calls that change nothing, and a
// recomputing implementation answers ten days later the second time.
func TestADeferredRequestAskedAgainDoesNotMoveItsDate(t *testing.T) {
	clk := clock.NewFixed(deletionRequestedAt)
	svc, pool := newDeletionService(t, clk, &fakeActiveJobs{active: true})
	user := registerFor(t, svc, "1703")

	first, _, err := svc.RequestDeletion(t.Context(), user.ID)
	if err != nil {
		t.Fatalf("the first request: %v", err)
	}

	clk.Advance(10 * 24 * time.Hour)

	second, created, err := svc.RequestDeletion(t.Context(), user.ID)
	if err != nil {
		t.Fatalf("the second request: %v", err)
	}

	if created {
		t.Error("asking again while deferred created a second request")
	}
	if second.State != DeletionDeferred {
		t.Errorf("State = %q, want %q — the delivery has not closed", second.State, DeletionDeferred)
	}
	if !second.CompleteBy.Equal(first.CompleteBy) {
		t.Errorf("the completion date moved from %s to %s across two calls that changed nothing; "+
			"it is being computed at read time", first.CompleteBy, second.CompleteBy)
	}

	if _, _, completeBy := deletionRequestRow(t, pool, user.ID); !completeBy.Equal(first.CompleteBy) {
		t.Errorf("the row holds %s and the person was told %s", completeBy, first.CompleteBy)
	}
}

// TestAFailingActiveJobLookupRecordsNothing is the error branch, made real.
//
// **The failure happens before any SQL**, which is wave 10's rule for proving an error branch is
// checked: PostgreSQL aborts a transaction as soon as a statement raises, so a fault injected into
// the *database* would reach the caller through the COMMIT whether or not the code looked at it.
// A port that returns an error cannot.
//
// What it protects: an implementation that read the lookup's error and carried on would record a
// live request for somebody who might be mid-delivery — the failure Docs/05 §3.1 names, arriving
// through the one path where nothing looks wrong.
func TestAFailingActiveJobLookupRecordsNothing(t *testing.T) {
	broken := &fakeActiveJobs{err: errors.New("jobs: the database went away")}
	svc, pool := newDeletionService(t, clock.NewFixed(deletionRequestedAt), broken)
	user := registerFor(t, svc, "1704")

	if _, _, err := svc.RequestDeletion(t.Context(), user.ID); err == nil {
		t.Fatal("the request succeeded while the active-job lookup was failing, so the platform " +
			"cannot know whether it has just promised to erase somebody mid-delivery")
	}

	if n := deletionRequestCount(t, pool, user.ID); n != 0 {
		t.Errorf("%d deletion requests were recorded despite the lookup failing, want 0", n)
	}
}

// TestNewServiceRefusesWithoutAnActiveJobLookup.
//
// A missing collaborator is a wiring mistake rather than a transient condition, and this one fails
// silently in the worst direction: every request would be recorded live, the responses would look
// ordinary, and nothing would show until SHIP-171 erased a party mid-delivery.
func TestNewServiceRefusesWithoutAnActiveJobLookup(t *testing.T) {
	hasher, err := passwords.NewHasher(testProfile)
	if err != nil {
		t.Fatalf("building the hasher: %v", err)
	}

	_, err = NewService(nil, hasher, testServiceIssuer(t, clock.System{}), testLimiter(t),
		&recordingSender{}, &recordingTexter{}, nil, clock.System{})
	if err == nil {
		t.Fatal("a service was built with no active-job lookup")
	}
}

// TestTheOpenStatesInSQLAreTheOnesTheDomainDeclares holds the one list that had to be written twice.
//
// [openDeletionStatesSQL] cannot be parameterised — PostgreSQL infers a partial unique index by
// proving its predicate, and a placeholder proves nothing — so the open states exist as a SQL
// literal and as [DeletionState.Open]. **This is the guard SHIP-171 needs**: adding 'completed' to
// [DeletionStates] without keeping it out of the open list would make an executed request block the
// same account from ever asking again, and nothing else in the build would notice.
//
// It is a text guard and wave 10's note applies — a test that reads a constant does not test the
// query that interpolates it. What tests the query is every deletion test above, which runs the
// real statement against a real index.
func TestTheOpenStatesInSQLAreTheOnesTheDomainDeclares(t *testing.T) {
	inSQL := map[string]bool{}
	for _, quoted := range strings.Split(strings.Trim(openDeletionStatesSQL, "()"), ",") {
		inSQL[strings.Trim(strings.TrimSpace(quoted), "'")] = true
	}

	inGo := map[string]bool{}
	for _, state := range OpenDeletionStates() {
		inGo[state.String()] = true
	}

	if len(inSQL) != len(inGo) {
		t.Fatalf("openDeletionStatesSQL names %d states and OpenDeletionStates() has %d: %v vs %v",
			len(inSQL), len(inGo), inSQL, inGo)
	}
	for state := range inGo {
		if !inSQL[state] {
			t.Errorf("%q is an open state and openDeletionStatesSQL does not name it — the "+
				"partial index would stop covering a row the domain thinks is open", state)
		}
	}
	for state := range inSQL {
		if !inGo[state] {
			t.Errorf("openDeletionStatesSQL names %q and DeletionState.Open does not — an "+
				"executed request would block the same account from ever asking again", state)
		}
	}
}
