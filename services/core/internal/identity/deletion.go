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
// It is the *request*, the promise, and — since SHIP-170 — whether the promise's clock has started.
// It is not the execution: SHIP-171 pseudonymises the record and SHIP-172 removes the artefacts.
// Nothing here deletes or pseudonymises anything, and there is no worker task.
//
// # SHIP-170: the deferral, and the room SHIP-169 left for it
//
// Docs/05 §3.1: "Deletion during an active job is deferred, not refused. A request made between
// Awarded and Delivered is queued until the job closes, and the user is told why. Erasing a party
// mid-delivery would strand the counterparty." SHIP-169 declined to build it and said exactly what
// it would need — a port identity declares and cmd/api fills, because `internal/identity` may not
// learn what a job status is — and left `state` widenable and `complete_by` movable. Both are used
// here: 000106 widens the CHECK and the open-request index, [ActiveJobs] is the port, and
// `cmd/api/routes_identity.go` holds the statement that spans `jobs` and `bids`.
//
// **No job identifier is recorded on the request**, and that is a decision rather than an
// omission — 000106's header carries the argument. The deferral is re-evaluated through the port
// every time the request is touched, so it cannot go stale, and the row stays what 000105 built it
// to be: evidence about a person, tied to nothing that can be cancelled underneath it.
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
// **SHIP-170 keeps that rule and states it more precisely: the date is written at a state change
// and never at a read.** A deferred request's thirty days have not started — the platform cannot
// know when the delivery will close — so the date it carries is the earliest it could complete
// from where it stands, and it is re-recorded at the moment the deferral lifts. Two calls that
// change nothing still answer with the same date, which is the property SHIP-169's tests pin;
// what moves the date is an event, and an event leaves a row.
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
// There are two values, and 'completed' is still SHIP-171's; adding it is an ordinary migration
// plus a constant here, and the paired test fails until both have moved.
type DeletionState string

const (
	// DeletionRequested is a request that has been accepted and not yet executed. Its thirty
	// days are running.
	DeletionRequested DeletionState = "requested"

	// DeletionDeferred is a request waiting on a delivery the person is party to (SHIP-170).
	//
	// It has been accepted, exactly as [DeletionRequested] has — Docs/05 §3.1 says deferred,
	// **not refused** — and the difference is only that the clock has not started. It becomes
	// [DeletionRequested] when the delivery closes, which is re-evaluated through [ActiveJobs]
	// rather than scheduled.
	DeletionDeferred DeletionState = "deferred"
)

// Valid reports whether s is one of the states the database permits.
func (s DeletionState) Valid() bool {
	switch s {
	case DeletionRequested, DeletionDeferred:
		return true
	}
	return false
}

// Open reports whether a request in this state is still outstanding.
//
// Both states are open today, and that will stop being true at SHIP-171: a 'completed' request is
// history, and it must not stop the same account asking again. This is the Go statement of
// `uq_account_deletion_requests_open`'s predicate; [openDeletionStatesSQL] is the SQL one, and
// TestTheOpenStatesInSQLAreTheOnesTheDomainDeclares holds the two together.
func (s DeletionState) Open() bool {
	switch s {
	case DeletionRequested, DeletionDeferred:
		return true
	}
	return false
}

func (s DeletionState) String() string { return string(s) }

// DeletionStates is every state the Go side knows about, in the order 000105 and 000106 list them.
//
// Exported so the migration's pairing test can read it without this package exporting a slice
// somebody could append to: it is returned fresh each call rather than being a package variable,
// because a shared slice is one a caller can rewrite in place.
func DeletionStates() []DeletionState { return []DeletionState{DeletionRequested, DeletionDeferred} }

// OpenDeletionStates is every state a request is still outstanding in, in the same order.
func OpenDeletionStates() []DeletionState {
	open := make([]DeletionState, 0, 2)
	for _, state := range DeletionStates() {
		if state.Open() {
			open = append(open, state)
		}
	}
	return open
}

// DeferralReason is what a person is told when their request is queued behind a delivery
// (SHIP-170).
//
// # Why the platform holds this sentence rather than the app
//
// CLAUDE.md: anything expected to change under operational pressure lives server-side, and policy
// copy is named in that list — Flutter has no over-the-air update path for Dart code, so a
// sentence shipped in a build is a sentence that cannot be corrected without a store release.
// Docs/05 §3.1 is the position it states, and the position is a legal one.
//
// # What it says, and what it deliberately does not
//
// It names the situation and the consequence, and **no job**: not an identifier, not an address,
// not a status, not a count. The endpoint is reachable by both halves of the marketplace, and a
// customer's budget is never exposed to a provider in any form (Docs/01 §4.3) — the safest
// explanation is one that never had a job to describe. "Your current delivery" is all a person
// needs to understand why they are waiting, and they can already see the delivery itself.
const DeferralReason = "Your account will be deleted once your current delivery is finished. " +
	"Deleting it now would leave the other party without the person carrying or receiving their " +
	"goods, so the request is held until the delivery closes and the thirty days start then."

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
// Nothing raises it from an endpoint today — the endpoint only creates or moves — and it exists
// because [postgresStore.lockOpenDeletionRequest] has to have something to say when the row it was
// told exists does not. Reaching it means the partial unique index and this package have come to
// disagree.
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
//
// # SHIP-170: what a request during a delivery does instead
//
// It is recorded as [DeletionDeferred] rather than refused, which is Docs/05 §3.1's own word. The
// caller still gets a 202 and a request that exists; what differs is the state, the explanation,
// and that the thirty days have not started.
//
// **The deferral is re-evaluated on every call, in both directions.** A request that was deferred
// becomes live once the delivery closes, and a live one is put back on hold if the person has since
// taken on a delivery — the second is what makes this a fact about the account's standing rather
// than about the instant somebody happened to tap. Docs/05 §3.1's reason is about *executing* a
// deletion mid-delivery, and SHIP-171 will ask the same question again before it does; keeping the
// stored state in step means the person is never told a date while carrying goods.
//
// This is what makes "queues until the job closes" true with no scheduled task and no job
// identifier on the row: the queue is a state, and reading the request is what drains it. What it
// does not do is move on its own — a person who never asks again stays deferred until SHIP-171
// looks. That is honest and is recorded in Docs/11 §3 rather than hidden here.
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

	var (
		request DeletionRequest
		created bool
	)

	// One transaction, which answers the question deletion.go asked SHIP-170 to decide.
	//
	// SHIP-169 left the INSERT and the SELECT unwrapped, and was right at the time: nothing
	// moved a request out of 'requested', so the row the SELECT read had been committed before
	// the INSERT conflicted with it and could not change underneath. **This ticket is what makes
	// that false.** The sequence is now insert-or-conflict, re-read, and possibly *write* — and
	// between the read and the write another request on the same account can move the same row,
	// so two callers could each decide the state from what they saw and one of them would lose.
	// The transaction and the FOR UPDATE in [postgresStore.lockOpenDeletionRequest] together are
	// what make the decision and the move one act.
	//
	// The port is called inside it too, per Docs/10 §3.2: the answer about the delivery and the
	// state written from it are one statement of what was true.
	err = db.InTx(ctx, s.pool, func(ctx context.Context, tx db.Runner) error {
		active, err := s.activeJobs.HasActiveJob(ctx, tx, userID)
		if err != nil {
			return err
		}

		state := DeletionRequested
		if active {
			state = DeletionDeferred
		}

		request, created, err = s.store.recordDeletionRequest(ctx, tx, DeletionRequest{
			ID:          id,
			UserID:      userID,
			State:       state,
			RequestedAt: requestedAt,
			CompleteBy:  requestedAt.Add(DeletionWindow),
		})
		return err
	})
	if err != nil {
		return DeletionRequest{}, false, err
	}

	// Info rather than debug, and it is the one line here worth keeping. A person asking to be
	// deleted is a rare, consequential act with a legal clock attached, and "when was this
	// requested" is the first question asked when the thirty days are nearly up. The completion
	// date is logged as it was stored, so the log and the row can be compared rather than
	// assumed equal. The state is logged with it because a deferred request's date is not yet a
	// promise, and a log line that did not say so would read as one.
	httpx.LoggerFrom(ctx).LogAttrs(ctx, slog.LevelInfo, "account deletion requested",
		slog.String("deletion_request_id", request.ID.String()),
		slog.String("state", request.State.String()),
		slog.Time("complete_by", request.CompleteBy),
		slog.Bool("created", created))

	return request, created, nil
}

// openDeletionStatesSQL is `uq_account_deletion_requests_open`'s predicate, written the way SQL
// needs it.
//
// A single-line constant with the states written out, following cmd/api's overduePickupStatuses:
// **the ON CONFLICT predicate cannot be parameterised**, because PostgreSQL infers a partial unique
// index by proving the index's own predicate from the one given, and a placeholder proves nothing.
// So there is a literal list in SQL and a [DeletionState.Open] in Go, and
// TestTheOpenStatesInSQLAreTheOnesTheDomainDeclares is what stops the two disagreeing — which is
// exactly the drift SHIP-171 will risk when it adds a state that must *not* join this list.
const openDeletionStatesSQL = `('requested', 'deferred')`

// recordDeletionRequest writes a request, or brings the open one the account already has into the
// state the caller has just worked out.
//
// **Three statements now, where SHIP-169 had two**, and the third is what made this a transaction.
// The INSERT goes first and the re-read runs only when the index refused it — the reverse, look
// then insert, is the race `uq_account_deletion_requests_open` exists to lose gracefully. What
// SHIP-170 adds is that the re-read is followed by a *write* when the deferral has moved, so the
// row has to be held between the two.
//
// `ON CONFLICT … DO NOTHING` names the index's own predicate, which is what PostgreSQL requires to
// infer a partial unique index. Without the `WHERE`, this is an inference failure at run time
// rather than at compile time — and since 000106 the predicate is both open states, so a deferred
// request is as unrepeatable as a live one.
//
// The caller supplies both the state it wants and the completion date to record with it; this
// function decides nothing about the delivery and asks nothing about it.
func (postgresStore) recordDeletionRequest(ctx context.Context, r db.Runner, req DeletionRequest) (DeletionRequest, bool, error) {
	row := r.QueryRow(ctx, `
		INSERT INTO account_deletion_requests (id, user_id, state, requested_at, complete_by)
		VALUES ($1, $2, $3, $4, $5)
		ON CONFLICT (user_id) WHERE state IN `+openDeletionStatesSQL+` DO NOTHING
		RETURNING `+deletionRequestColumns,
		req.ID, req.UserID, req.State, req.RequestedAt, req.CompleteBy)

	created, err := scanDeletionRequest(row)
	switch {
	case err == nil:
		return created, true, nil
	case !isNoRows(err):
		return DeletionRequest{}, false, err
	}

	existing, err := postgresStore{}.lockOpenDeletionRequest(ctx, r, req.UserID)
	if err != nil {
		return DeletionRequest{}, false, err
	}
	if existing.State == req.State {
		// Nothing has changed, so nothing is written. This is the ordinary repeat — the
		// person asking again, or asking twice — and it is what keeps the completion date
		// they were told from moving. `created` is false either way; the distinction the
		// endpoint publishes is whether a request was made, not whether a row was touched.
		return existing, false, nil
	}

	moved, err := postgresStore{}.moveDeletionRequest(ctx, r, existing.ID, req.State, req.CompleteBy)
	if err != nil {
		return DeletionRequest{}, false, err
	}
	return moved, false, nil
}

// moveDeletionRequest changes an open request's state and re-records what it is promised.
//
// # Why the completion date is rewritten and not left alone
//
// The thirty days are the window the platform has to act, and a deferred request is one it may not
// act on yet. Leaving the original date on a request that has been on hold would have the platform
// carrying a promise it knew it would miss — and 000105's `ck_account_deletion_requests_complete_by`
// would eventually be the only thing describing it, which is not what a person was told. So the
// date means the same thing in both states: thirty days from the moment the request last stood
// where it now stands. In [DeletionRequested] that is a commitment; in [DeletionDeferred] it is the
// earliest the platform could finish, and the state is what says which.
//
// **This is not the read-time derivation SHIP-169 exists to forbid.** The date is written by an
// event and read from the row; two reads that change nothing return the same value, which is the
// property [TestTheCompletionDateDoesNotMoveWithTheClock] pins and which a recomputing
// implementation cannot have.
//
// The row is already held by [postgresStore.lockOpenDeletionRequest] in the same transaction, so
// the WHERE names the identifier alone. `updated_at` is the trigger 000105 installed for exactly
// this ticket.
func (postgresStore) moveDeletionRequest(
	ctx context.Context,
	r db.Runner,
	id uuid.UUID,
	to DeletionState,
	completeBy time.Time,
) (DeletionRequest, error) {
	row := r.QueryRow(ctx, `
		UPDATE account_deletion_requests
		   SET state = $2, complete_by = $3
		 WHERE id = $1
		RETURNING `+deletionRequestColumns, id, to, completeBy)

	return scanDeletionRequest(row)
}

// deletionRequestColumns is the projection every read of a request shares, so that adding a field
// to [DeletionRequest] fails to compile in one place rather than returning a zero value from two.
const deletionRequestColumns = `id, user_id, state, requested_at, complete_by`

// lockOpenDeletionRequest reads the account's outstanding request and holds it for the rest of the
// transaction.
//
// Scoped by user_id in the statement rather than checked above it (Docs/07 §3): a predicate on the
// query is a decision no later caller can leave out. `uq_account_deletion_requests_open` is what
// makes "the" open request unambiguous — at most one row can match, in either open state, since
// 000106 widened the index with the CHECK.
//
// **FOR UPDATE is SHIP-170's addition and it is the half that makes the move safe.** SHIP-169 only
// ever read this row and handed it back; this ticket may write to it, and a read that did not lock
// would let two concurrent requests on one account each decide the state from what they saw. The
// lock is taken here rather than in [postgresStore.moveDeletionRequest] because the decision is
// made from what this read returned — locking at the write would be locking after the race.
//
// The lock is only meaningful inside a transaction, which is why [Service.RequestDeletion] opens
// one. Handed the pool it would be released at the end of the statement, which is the shape of a
// lock that is doing nothing.
func (postgresStore) lockOpenDeletionRequest(ctx context.Context, r db.Runner, userID uuid.UUID) (DeletionRequest, error) {
	row := r.QueryRow(ctx, `
		SELECT `+deletionRequestColumns+`
		  FROM account_deletion_requests
		 WHERE user_id = $1 AND state IN `+openDeletionStatesSQL+`
		   FOR UPDATE`, userID)

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
