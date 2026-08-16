package identity

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/DulsaraNethmin/Shipper/services/core/internal/clock"
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
