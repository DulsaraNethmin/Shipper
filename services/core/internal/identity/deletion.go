// SHIP-169: a person asks for their account to be deleted, and is told when it will happen.
//
// Docs/05 §3.1 settles the model rather than leaving it to this ticket. "Delete the person, retain
// the transaction": the account holder is replaced by a stable pseudonym and the jobs, bids,
// transitions and audit entries they took part in survive, so the counterparty's own history stays
// coherent. Deletion is confirmed in-app, executes within thirty days, and the person is told when
// it will complete.
//
// # What this file is, and what it deliberately is not
//
// It is the *request* and the promise. It is not the execution: SHIP-170 defers a request made
// during an active job, SHIP-171 pseudonymises the record, and SHIP-172 removes the artefacts.
// Nothing here deletes, pseudonymises or schedules anything, and there is no worker task.
//
// **The deferral rule is Docs/05 §3.1's and it belongs to SHIP-170, which is a ticket of its own
// with a dependency this one does not have.** Its *Done when* is "a request during Awarded to
// Delivered queues until the job closes and explains why", and it depends on SHIP-57 as well as on
// this. It cannot be built here without `internal/identity` learning what a job status is, which
// crosses a domain boundary CLAUDE.md forbids — the port would be identity's to declare and
// cmd/api's to fill, and that is a design decision with a ticket attached rather than something to
// smuggle into a three-point endpoint. What this file does instead is leave room for it:
// `state` is a CHECK constraint 000105 documents SHIP-170 widening, and `complete_by` is a stored
// instant a deferral can move.
//
// # The one thing worth getting right here
//
// **The completion date is recorded when the request is made, and never recomputed.** An
// implementation that derives it at read time — now plus thirty days, worked out afresh whenever
// anybody asks — answers with a date that is always plausible and is different every day. The
// platform would then hold no record of what it told the person, and the promise would slide
// forward silently for as long as nobody executed it. 000105's `complete_by` is NOT NULL for that
// reason, and [Service.RequestDeletion] writes it once.
//
// The blank line below keeps this a file note rather than a second package comment.

package identity

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"github.com/google/uuid"

	"github.com/DulsaraNethmin/Shipper/services/core/internal/db"
	"github.com/DulsaraNethmin/Shipper/services/core/internal/httpx"
)

// DeletionWindow is how long the platform has to execute a request it has accepted.
//
// Thirty days, from Docs/05 §3.1: "Apple permits a reasonable delay and permits retaining what law
// requires. Shipper confirms the request in-app, executes within 30 days, and tells the user when
// it will complete." It is the figure the person is told, so it is stated once, here, rather than
// as an expression at the point of the insert — two copies is how the promise and the schedule
// come to disagree.
//
// A fixed duration rather than a calendar addition. Every instant this package handles is UTC,
// where a day is exactly twenty-four hours and the two are identical; a calendar addition would
// only start to differ if a local zone were introduced, which is the moment somebody should be
// making this decision again rather than inheriting it.
//
// It is a constant rather than configuration for the same reason the password floor is (see the
// note above minPasswordLength): thirty days is a published commitment in a privacy policy and a
// store listing, not a lever to pull under operational pressure. Moving it is a deploy *and* a
// document change, which is the correct amount of friction.
const DeletionWindow = 30 * 24 * time.Hour

// DeletionState is where a request has got to.
//
// The values are exactly the strings ck_account_deletion_requests_state permits in 000105, and
// migrations/account_deletion_requests_test.go holds the two lists to each other — Docs/10 §3.4's
// pairing, which is what stops a Go constant and a CHECK constraint drifting apart when they are
// widened on different branches.
//
// There is one value today and that is deliberate rather than unfinished. 'deferred' is SHIP-170's
// and 'completed' is SHIP-171's; adding either is an ordinary migration plus a constant here, and
// the paired test fails until both have moved.
type DeletionState string

const (
	// DeletionRequested is a request that has been accepted and not yet executed.
	DeletionRequested DeletionState = "requested"
)

// Valid reports whether s is one of the states the database permits.
func (s DeletionState) Valid() bool {
	switch s {
	case DeletionRequested:
		return true
	}
	return false
}

func (s DeletionState) String() string { return string(s) }

// DeletionStates is every state the Go side knows about, in the order 000105 lists them.
//
// Exported so the migration's pairing test can read it without this package exporting a slice
// somebody could append to: it is returned fresh each call rather than being a package variable,
// because a shared slice is one a caller can rewrite in place.
func DeletionStates() []DeletionState { return []DeletionState{DeletionRequested} }

// DeletionRequest is one person's request, as the account_deletion_requests table holds it.
//
// CompleteBy is the instant the person was *told*, read back from the row rather than derived —
// see the file note. RequestedAt stands in for a created_at, because the row is the record of the
// request and its creation and the request are one event.
type DeletionRequest struct {
	ID     uuid.UUID
	UserID uuid.UUID

	State DeletionState

	RequestedAt time.Time
	CompleteBy  time.Time
}

// ErrDeletionRequestNotFound means the account has no open deletion request.
//
// Nothing raises it from an endpoint today — SHIP-169 only creates — and it exists because
// [postgresStore.openDeletionRequest] has to have something to say when the row it was told exists
// does not. Reaching it means the partial unique index and this package have come to disagree.
var ErrDeletionRequestNotFound = errors.New("identity: no open account deletion request")

// RequestDeletion records a request to delete the caller's account and returns the completion date
// they are told (SHIP-169).
//
// The bool reports whether this call created the request. False means the account already had an
// open one and it is being returned unchanged — with the date it has carried since it was made,
// which is the property this endpoint exists to keep.
//
// # Why a second request is not an error
//
// A person who taps twice, or comes back a week later having forgotten, has asked for a state that
// already holds. Refusing them would be refusing the thing they want, and would give a client
// nothing to render except an error where the answer — "on the fifteenth of September" — is
// already known. The stronger reason is that it must not create a *second* request: two rows would
// be two promises about one account, and whichever the execution happened to read would be the one
// that counted.
//
// # Why the race is settled by the database
//
// `uq_account_deletion_requests_open` refuses a second open row whatever the application believed.
// A read-then-insert loses to two taps on a slow connection arriving milliseconds apart, which is
// the same argument uq_users_email rests on and the same one Docs/06 §4.1 makes for the
// one-accepted-bid index. The idempotency middleware covers a *retry* of one request; two honest
// requests carry two keys and reach the handler twice.
func (s *Service) RequestDeletion(ctx context.Context, userID uuid.UUID) (DeletionRequest, bool, error) {
	if s.pool == nil {
		return DeletionRequest{}, false, errUnavailable
	}

	id, err := uuid.NewV7()
	if err != nil {
		return DeletionRequest{}, false, err
	}

	// Both instants come from one reading of the clock, so that the row cannot say it was
	// promised a window a fraction under DeletionWindow. Two calls to Now() would differ by
	// however long the arithmetic took, which is invisible until a test asserts the difference
	// is exactly thirty days — and then it is a flake nobody can reproduce.
	requestedAt := s.clock.Now().UTC()

	request, created, err := s.store.insertDeletionRequest(ctx, s.pool, DeletionRequest{
		ID:          id,
		UserID:      userID,
		State:       DeletionRequested,
		RequestedAt: requestedAt,
		CompleteBy:  requestedAt.Add(DeletionWindow),
	})
	if err != nil {
		return DeletionRequest{}, false, err
	}

	// Info rather than debug, and it is the one line here worth keeping. A person asking to be
	// deleted is a rare, consequential act with a legal clock attached, and "when was this
	// requested" is the first question asked when the thirty days are nearly up. The completion
	// date is logged as it was stored, so the log and the row can be compared rather than
	// assumed equal.
	httpx.LoggerFrom(ctx).LogAttrs(ctx, slog.LevelInfo, "account deletion requested",
		slog.String("deletion_request_id", request.ID.String()),
		slog.Time("complete_by", request.CompleteBy),
		slog.Bool("created", created))

	return request, created, nil
}

// insertDeletionRequest writes a request, or hands back the open one the account already has.
//
// Two statements rather than one, and the ordering is what makes it correct: the INSERT goes first
// and the SELECT runs only when the index refused it. The reverse — look, then insert — is the
// race `uq_account_deletion_requests_open` exists to lose gracefully.
//
// `ON CONFLICT … DO NOTHING` names the index's own predicate, which is what PostgreSQL requires to
// infer a partial unique index. Without the `WHERE`, this is an inference failure at run time
// rather than at compile time.
//
// No transaction wraps the pair. The row the SELECT reads was committed before the INSERT that
// conflicted with it, and nothing in this service moves a request out of 'requested' — SHIP-170
// and SHIP-171 both will, and whichever arrives first should read this note and decide whether the
// two statements need to become one.
func (postgresStore) insertDeletionRequest(ctx context.Context, r db.Runner, req DeletionRequest) (DeletionRequest, bool, error) {
	row := r.QueryRow(ctx, `
		INSERT INTO account_deletion_requests (id, user_id, state, requested_at, complete_by)
		VALUES ($1, $2, $3, $4, $5)
		ON CONFLICT (user_id) WHERE state = 'requested' DO NOTHING
		RETURNING `+deletionRequestColumns,
		req.ID, req.UserID, req.State, req.RequestedAt, req.CompleteBy)

	created, err := scanDeletionRequest(row)
	switch {
	case err == nil:
		return created, true, nil
	case !isNoRows(err):
		return DeletionRequest{}, false, err
	}

	existing, err := postgresStore{}.openDeletionRequest(ctx, r, req.UserID)
	if err != nil {
		return DeletionRequest{}, false, err
	}
	return existing, false, nil
}

// deletionRequestColumns is the projection every read of a request shares, so that adding a field
// to [DeletionRequest] fails to compile in one place rather than returning a zero value from two.
const deletionRequestColumns = `id, user_id, state, requested_at, complete_by`

// openDeletionRequest reads the account's outstanding request, if it has one.
//
// Scoped by user_id in the statement rather than checked above it (Docs/07 §3): a predicate on the
// query is a decision no later caller can leave out. `uq_account_deletion_requests_open` is what
// makes "the" open request unambiguous — at most one row can match.
func (postgresStore) openDeletionRequest(ctx context.Context, r db.Runner, userID uuid.UUID) (DeletionRequest, error) {
	row := r.QueryRow(ctx, `
		SELECT `+deletionRequestColumns+`
		  FROM account_deletion_requests
		 WHERE user_id = $1 AND state = $2`, userID, DeletionRequested)

	request, err := scanDeletionRequest(row)
	if isNoRows(err) {
		return DeletionRequest{}, ErrDeletionRequestNotFound
	}
	if err != nil {
		return DeletionRequest{}, err
	}
	return request, nil
}

// scanDeletionRequest reads one row in the order deletionRequestColumns declares.
//
// The instants are normalised to UTC on the way out. pgx returns a timestamptz in the session's
// zone, and every other timestamp this package hands upwards is UTC — a completion date that
// rendered in local time on one deployment and UTC on another is the sort of difference a client
// discovers by displaying the wrong day.
func scanDeletionRequest(row interface{ Scan(...any) error }) (DeletionRequest, error) {
	var req DeletionRequest
	if err := row.Scan(
		&req.ID, &req.UserID, &req.State, &req.RequestedAt, &req.CompleteBy,
	); err != nil {
		return DeletionRequest{}, err
	}
	req.RequestedAt = req.RequestedAt.UTC()
	req.CompleteBy = req.CompleteBy.UTC()
	return req, nil
}
