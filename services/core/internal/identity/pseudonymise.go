// SHIP-171: the thirty days run out, and the person stops being a person.
//
// Docs/05 §3.1's principle is "delete the person, retain the transaction", and this file is the
// first half of that sentence. The jobs, bids, transitions, audit entries and proof metadata the
// account took part in are untouched; what changes is that the columns naming a human being stop
// naming one, so the counterparty's own history stays coherent and nobody's record is silently
// rewritten because the other party left.
//
// SHIP-169 recorded the request and the promise. SHIP-170 held it while a delivery was in flight.
// This is what happens when the clock runs out, and it runs from cmd/worker rather than from a
// request: nobody presses a button to be erased on the thirtieth day, and Docs/05 §3.1 puts
// execution out of an ordinary administrator's reach entirely.
//
// # The three clauses of the criterion, and where each one lives
//
// "Profile and contact data are irreversibly replaced by a stable pseudonym."
//
//   - **Profile and contact data** is [Pseudonym]'s four fields against five tables — `users`,
//     `email_verification_tokens` and `phone_otps` here, `provider_profiles` and `notifications`
//     through [PersonalDetails]. The last two matter as much as the first: a copy of the address
//     sitting beside a foreign key to the account is a reverse mapping wherever it lives.
//   - **Irreversibly** is the absence of something rather than the presence of it. Nothing written
//     by this file records what it overwrote — not the request row, not the audit log, not a log
//     line, not a column added for the purpose. See [PseudonymFor].
//   - **A stable pseudonym** is [PseudonymFor] being a pure function of `users.id`, which is the
//     identifier Docs/05 §3.1 already calls the pseudonym: every retained row references it, and it
//     is what makes a customer's record of who carried their goods survive that provider leaving.
//
// # What this file deliberately does not do
//
// It does not delete the account row, and could not: sixteen migrations carry `REFERENCES users`
// and every one is ON DELETE RESTRICT. 000201 states the consequence in the schema — "a provider
// whose documents have been reviewed cannot be deleted at all".
//
// It does not remove artefacts. Message bodies, device tokens, verification documents and
// attributable images are SHIP-172's, which depends on this ticket.
//
// It does not touch `users.password_hash`, and that is a scope decision with a reason rather than an
// oversight — see [postgresStore.pseudonymiseUser].
//
// The blank line below keeps this a file note rather than a second package comment.

package identity

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/DulsaraNethmin/Shipper/services/core/internal/clock"
	"github.com/DulsaraNethmin/Shipper/services/core/internal/db"
)

// Pseudonym is what one account's identifying fields become.
//
// Four values rather than one, because the columns they replace are not interchangeable: an email
// address is unique across accounts and a name is not, a trading name is rendered to a customer and
// a phone number is dialled. What they share is that every one of them is derived from the same
// identifier by [PseudonymFor], so an account has one pseudonym expressed four ways rather than four
// unrelated placeholders.
type Pseudonym struct {
	// Token is the value written into the two contact columns and into every copy of them.
	//
	// **It is deliberately not a valid email address and not a valid phone number.** See
	// [PseudonymFor] for why that is the point rather than a shortcut.
	Token string

	// Name is what `users.name` becomes: something a support screen can render.
	Name string

	// TradingName is what `provider_profiles.display_name` becomes, for an account that had one.
	TradingName string
}

// pseudonymPrefix marks a value as a pseudonym rather than as anything it stands in for.
//
// A colon rather than an at sign or a plus, and that single character is doing the work: it makes
// the value fail [plausibleEmail] and [validE164] at once, so no endpoint that takes an address or a
// number will accept it and no mail system or handset can be reached with it.
const pseudonymPrefix = "deleted:"

// PseudonymFor is the whole of the stability and the irreversibility claim, so it is worth reading
// rather than skimming.
//
// # Stable
//
// It is a pure function of `users.id` and of nothing else. No counter, no clock, no random source:
// the same account pseudonymises to the same four values on a replay, on a restore, and on a second
// machine, and TestThePseudonymIsStable asserts exactly that. A value drawn from a sequence or from
// the current time would look identical in the row and would differ every time it was computed,
// which is the property that separates a pseudonym from a placeholder.
//
// # Irreversible
//
// The input is the account identifier, which is **not** personal data being reused: Docs/05 §3.1
// already calls it the pseudonym — "a deleted user becomes a stable pseudonymous identifier" — and
// every retained job, bid, transition and audit row references it and goes on doing so. Deriving
// from it is therefore not a reversal of anything; it is spelling out, in the columns that used to
// identify a person, the identifier that from now on identifies only a record.
//
// The reversal that would matter is a stored mapping from the pseudonym back to the address, and
// there is none — not on the request row, not in `audit_log`, not in a column added for the purpose,
// and not in a log line. Nothing in this file reads the old values into a variable it then writes
// anywhere, which is why the UPDATE statements below name columns and never return them.
//
// # Unique
//
// `uq_users_email` and `uq_users_phone` are both UNIQUE over NOT NULL columns, so two accounts may
// not pseudonymise to one value. The identifier is a primary key, so they cannot.
//
// # Why the contact values are not address-shaped
//
// The obvious pseudonym is `deleted-<id>@deleted.invalid`, which is unique, stable and undeliverable
// — RFC 2606 reserves the TLD. It was rejected because it is still *presentable*: the identifier is
// visible to a counterparty in ordinary API responses, so anybody could reconstruct that address and
// hand it to sign-in, password reset or a verification resend. `deleted:<id>` cannot be handed to any
// of them — [plausibleEmail] refuses it before the store is reached, and [validE164] refuses it for
// the same reason on every path that takes a number — so the pseudonymised account is unreachable
// through the front door by construction rather than by a check somebody remembered to add.
//
// The same string goes in both columns. They are two indexes over two columns, so there is no
// collision, and one token in both says plainly that neither is a contact channel any more.
func PseudonymFor(id uuid.UUID) Pseudonym {
	full := id.String()

	// The last group of a UUID, which for the v7 identifiers this service issues is the random
	// tail rather than the timestamp. The first eight hex digits would be the high half of a
	// millisecond clock, so two accounts registered inside the same minute would render
	// identically on a screen — which is the one thing this short form exists to avoid.
	short := full[len(full)-12:]

	return Pseudonym{
		Token:       pseudonymPrefix + full,
		Name:        "Deleted user " + short,
		TradingName: "Deleted provider " + short,
	}
}

// DeletionBatch bounds one pass, on the same reasoning as the other sweeps in cmd/worker: a pass
// holds one transaction, and an unbounded claim is a transaction whose length is decided by however
// much work happened to be due.
//
// Smaller than the job and bid sweeps deliberately. Each row here writes five tables and revokes
// however many sessions the account holds, where an expiry writes a status and an event, so the same
// count would be a much longer transaction.
const DeletionBatch = 25

// DueDeletionClaim is the claim query [Pseudonymiser.Execute] is driven from.
//
// Exported so cmd/worker can hand it to ClaimIDs, which is the arrangement every task uses: the
// query belongs to the domain that owns the table and the ceremony around it — FOR UPDATE, SKIP
// LOCKED, and the check that both are present — belongs to the worker.
//
// **It claims both open states rather than only [DeletionRequested]**, and interpolates
// [openDeletionStatesSQL] rather than writing the list a third time. A deferred request whose
// promised date has arrived is one this pass has to look at even though it will not execute it:
// deletion.go records that a person who never asks again "stays deferred until SHIP-171 looks", and
// this is the looking. What happens to each is [Pseudonymiser.Execute]'s decision, not the query's.
//
// Ordered by `complete_by` so the oldest promise is kept first, and served by
// `idx_account_deletion_requests_due`.
const DueDeletionClaim = `
	SELECT id
	  FROM account_deletion_requests
	 WHERE state IN ` + openDeletionStatesSQL + `
	   AND complete_by <= $1
	 ORDER BY complete_by
	 FOR UPDATE SKIP LOCKED
	 LIMIT $2`

// DeletionOutcome is what one pass decided about one claimed request.
//
// Three values rather than a bool, because a pass that claimed a hundred requests and executed none
// of them is either an idle marketplace or a bug, and "claimed" alone cannot tell them apart.
type DeletionOutcome string

const (
	// DeletionExecuted means the person was replaced by their pseudonym and the request is now
	// [DeletionCompleted].
	DeletionExecuted DeletionOutcome = "executed"

	// DeletionHeld means the account is party to a delivery, so the request was put back on
	// hold — Docs/05 §3.1's "deferred, not refused", applied at the moment of execution rather
	// than at the moment of asking.
	DeletionHeld DeletionOutcome = "held"

	// DeletionRestarted means a deferred request whose delivery has since closed became live
	// again, and its thirty days start now.
	DeletionRestarted DeletionOutcome = "restarted"
)

// Pseudonymiser executes deletion requests whose promised date has arrived (SHIP-171).
//
// A type of its own rather than a method on [Service], and that is a wiring decision worth stating.
// [NewService] refuses a nil hasher, issuer, limiter, email sender or SMS sender, because a service
// missing one of those would accept a registration it could not complete — all correct, and all
// irrelevant to a sweep that hashes nothing and sends nothing. cmd/worker would have had to build
// five collaborators it never calls, or [NewService] would have had to stop refusing them.
//
// The clock is the worker's rather than the database's, so a test advances a [clock.Fixed] and
// watches a request fall due instead of writing a date into the past to fake one.
type Pseudonymiser struct {
	activeJobs ActiveJobs
	elsewhere  PersonalDetails
	clock      clock.Clock
	store      postgresStore
}

// NewPseudonymiser builds the executor.
//
// Both ports are required. A nil [ActiveJobs] would execute every due request without asking
// whether somebody is mid-delivery, which is the exact defect Docs/05 §3.1 forbids and which nothing
// downstream would show — the account would simply be gone and the counterparty stranded. A nil
// [PersonalDetails] would leave the copies of the address behind, so "irreversibly replaced" would
// be false in a way that only a query nobody runs would reveal.
func NewPseudonymiser(jobs ActiveJobs, elsewhere PersonalDetails, clk clock.Clock) (*Pseudonymiser, error) {
	if jobs == nil {
		return nil, errors.New("identity: a pseudonymiser needs an active-job lookup")
	}
	if elsewhere == nil {
		return nil, errors.New("identity: a pseudonymiser needs somewhere to replace the copies")
	}
	if clk == nil {
		return nil, errors.New("identity: a pseudonymiser needs a clock")
	}
	return &Pseudonymiser{activeJobs: jobs, elsewhere: elsewhere, clock: clk}, nil
}

// Execute decides what to do with one claimed request and does it.
//
// The caller has already locked the row — [DueDeletionClaim] takes it FOR UPDATE inside the
// transaction this runs in — so everything below is one statement about what was true, in the sense
// Docs/10 §3.2 means: the answer about the delivery, the pseudonymisation, and the move of the
// request are indivisible.
//
// # The delivery is re-read here and not trusted from the row
//
// ports.go promised it: "SHIP-171 asks again before it executes, which is the check that actually
// protects the counterparty." A request recorded as live thirty days ago may belong to somebody who
// won a job last week, and the deferral holds no job identifier precisely so that it cannot go
// stale. So the question is asked again, and the answer decides all three outcomes:
//
//	party to a delivery            -> held, on hold again, thirty days re-recorded
//	deferred and no longer a party -> restarted, live, thirty days start now
//	live and no longer a party     -> executed
//
// The middle case is why the claim takes both open states. A deferral that lifts here does **not**
// execute in the same pass: the person is owed the window they were promised, and it starts when the
// deferral lifts rather than having run while they were waiting — which is SHIP-170's own rule
// applied by the sweep rather than by a request.
func (p *Pseudonymiser) Execute(ctx context.Context, r db.Runner, requestID uuid.UUID) (DeletionOutcome, error) {
	request, err := p.store.deletionRequestByID(ctx, r, requestID)
	if err != nil {
		return "", err
	}
	if !request.State.Open() {
		// Unreachable through the claim, which filters on the open states. Reaching it means
		// something outside this file moved a row between the claim and the read, inside a
		// transaction holding the lock on it — so it is reported rather than skipped.
		return "", fmt.Errorf("identity: deletion request %s is %s, which is not open",
			requestID, request.State)
	}

	active, err := p.activeJobs.HasActiveJob(ctx, r, request.UserID)
	if err != nil {
		return "", err
	}

	now := p.clock.Now().UTC()

	switch {
	case active:
		// The thirty days are re-recorded rather than left alone, which is
		// moveDeletionRequest's own rule: a date the platform knew it would miss is not what
		// anybody was told.
		if _, err := p.store.moveDeletionRequest(
			ctx, r, request.ID, DeletionDeferred, now.Add(DeletionWindow)); err != nil {
			return "", err
		}
		return DeletionHeld, nil

	case request.State == DeletionDeferred:
		if _, err := p.store.moveDeletionRequest(
			ctx, r, request.ID, DeletionRequested, now.Add(DeletionWindow)); err != nil {
			return "", err
		}
		return DeletionRestarted, nil
	}

	if err := p.pseudonymise(ctx, r, request.UserID, now); err != nil {
		return "", err
	}

	// **complete_by is not rewritten**, unlike every other move of this row. On a completed
	// request it is the promise the platform made and kept, and the trigger 000105 installed
	// puts *when* it was kept in `updated_at`. Overwriting it would replace the one durable
	// record of what the person was told with the moment a sweep happened to run.
	if err := p.store.completeDeletionRequest(ctx, r, request.ID); err != nil {
		return "", err
	}
	return DeletionExecuted, nil
}

// pseudonymise replaces every copy of one person's identifying details.
//
// Five tables, three of them this package's own and two behind [PersonalDetails]. All in the
// caller's transaction: a pass that replaced `users` and failed on `notifications` would leave an
// account that reads as deleted beside a table that still says who it was, and the request would
// stay open so the next pass would try again against a half-pseudonymised row.
//
// **Nothing here reads the old values.** Every statement is an UPDATE naming columns, with no
// RETURNING and no prior SELECT, so there is no variable in this process holding the address that
// was replaced and nothing that could be logged, wrapped into an error, or written to a row by a
// later change to this function.
func (p *Pseudonymiser) pseudonymise(ctx context.Context, r db.Runner, userID uuid.UUID, at time.Time) error {
	as := PseudonymFor(userID)

	if err := p.store.pseudonymiseUser(ctx, r, userID, as); err != nil {
		return err
	}
	if err := p.store.pseudonymiseContactCopies(ctx, r, userID, as); err != nil {
		return err
	}
	if _, err := p.store.revokeSessionsForDeletedAccount(ctx, r, userID, at); err != nil {
		return err
	}
	if _, err := p.elsewhere.Replace(ctx, r, userID, as); err != nil {
		return err
	}
	return nil
}

// deletionRequestByID reads one request the caller already holds a lock on.
//
// No FOR UPDATE and no user_id predicate, which is the difference between this and
// [postgresStore.lockOpenDeletionRequest]: the claim query took the lock, and it claimed by
// identifier rather than by account because a sweep does not have an account in hand.
func (postgresStore) deletionRequestByID(ctx context.Context, r db.Runner, id uuid.UUID) (DeletionRequest, error) {
	row := r.QueryRow(ctx, `
		SELECT `+deletionRequestColumns+`
		  FROM account_deletion_requests
		 WHERE id = $1`, id)

	request, err := scanDeletionRequest(row)
	if isNoRows(err) {
		return DeletionRequest{}, ErrDeletionRequestNotFound
	}
	if err != nil {
		return DeletionRequest{}, err
	}
	return request, nil
}

// completeDeletionRequest records that the request was executed.
//
// A separate statement from [postgresStore.moveDeletionRequest] rather than a call to it with the
// existing date, because the two say different things: that one moves a promise, and this one closes
// it. Passing the row's own `complete_by` back in would work and would put the rule "the promise is
// not rewritten" in the caller, where the next person to add a state would have to notice it.
//
// Guarded on the state still being open, so a second execution of the same identifier writes nothing
// and says so. It cannot happen through the claim — a completed request is not claimed — and the
// guard is here for the same reason [postgresStore.revokeDeviceSession] has one.
func (postgresStore) completeDeletionRequest(ctx context.Context, r db.Runner, id uuid.UUID) error {
	tag, err := r.Exec(ctx, `
		UPDATE account_deletion_requests
		   SET state = $2
		 WHERE id = $1 AND state IN `+openDeletionStatesSQL, id, DeletionCompleted)
	if err != nil {
		return fmt.Errorf("identity: completing a deletion request: %w", err)
	}
	if tag.RowsAffected() != 1 {
		return fmt.Errorf("identity: deletion request %s was not open when it was completed", id)
	}
	return nil
}

// pseudonymiseUser replaces the account's own profile and contact columns.
//
// # What is replaced
//
// `name` is profile data, `email` and `phone` are the two contact channels, and Docs/05 §3.1 puts
// all three in the irreversibly-deleted column. `role` stays, because it is not a fact about a
// person — it is which half of the marketplace the retained rows belong to, and every job and bid
// hanging off this account would stop making sense without it. `status`, `created_at` and the two
// verification timestamps stay for the same reason: they describe the record, not the human.
//
// # What is not replaced, and why `password_hash` is the interesting one
//
// **It is left alone, deliberately.** It is neither profile nor contact data — it is a one-way
// function of a secret the person chose — and the two things it could be used for are both closed by
// the columns above: the address it belongs to no longer exists, and the value that replaced it
// cannot be presented to sign-in at all, because [plausibleEmail] refuses it before the store is
// reached. Overwriting it with a value the hasher cannot parse would turn an unreachable sign-in
// into a 500 if anybody ever widened that validator, which is a worse failure than the one it fixes;
// overwriting it with a hash of something random would be the only correct way to do it and would
// make this function non-deterministic, which is the one property [PseudonymFor] exists to have.
//
// It is recorded in Docs/11 §3 as the one column left holding anything derived from the person, and
// handed to SHIP-172 rather than quietly narrowed here.
//
// # Why there is no RETURNING
//
// Every other write in this package returns the row it wrote, so a caller reads what is stored
// rather than what it hoped. Not this one: the projection would carry the values being replaced
// through a scan, into a struct, and one careless log line later into a file. The count is enough —
// there is exactly one row, and if there is not, something has gone wrong that a scan would not have
// told us either.
func (postgresStore) pseudonymiseUser(ctx context.Context, r db.Runner, userID uuid.UUID, as Pseudonym) error {
	tag, err := r.Exec(ctx, `
		UPDATE users
		   SET name = $2, email = $3, phone = $4
		 WHERE id = $1`, userID, as.Name, as.Token, as.Token)
	if err != nil {
		return fmt.Errorf("identity: pseudonymising an account: %w", err)
	}
	if tag.RowsAffected() != 1 {
		return fmt.Errorf("identity: pseudonymising %s changed %d rows, want 1",
			userID, tag.RowsAffected())
	}
	return nil
}

// pseudonymiseContactCopies replaces the two copies of the contact channels this package keeps.
//
// `email_verification_tokens.email` and `phone_otps.phone` are verbatim copies, held so that a token
// issued before an address changed still says which address it confirms. That is a good reason for
// the column and no reason at all to keep the value after the account has been pseudonymised: a
// single join recovers the person from either one, which would make "irreversibly replaced" false
// while `users` looked entirely clean.
//
// The rows are updated rather than deleted. They are consumed-or-expired credentials with a
// `consumed_reason` history, and 000101 and 000102 both keep them for that; removing them is the
// artefact-cascade shape SHIP-172 owns, and replacing the identifying column is what this ticket
// claims. Neither table has a unique index on the column, so every row of an account collapses onto
// one token without conflict.
func (postgresStore) pseudonymiseContactCopies(ctx context.Context, r db.Runner, userID uuid.UUID, as Pseudonym) error {
	if _, err := r.Exec(ctx, `
		UPDATE email_verification_tokens SET email = $2 WHERE user_id = $1`,
		userID, as.Token); err != nil {
		return fmt.Errorf("identity: pseudonymising the email verification tokens: %w", err)
	}

	if _, err := r.Exec(ctx, `
		UPDATE phone_otps SET phone = $2 WHERE user_id = $1`,
		userID, as.Token); err != nil {
		return fmt.Errorf("identity: pseudonymising the phone codes: %w", err)
	}
	return nil
}

// revokeSessionsForDeletedAccount ends every session the account still holds.
//
// Not contact data and not profile data, so this is beyond the criterion rather than part of it —
// and it is here because the criterion would otherwise be met by an account somebody can still use.
// A handset holding a refresh token issued last week keeps working after the name, address and
// number are gone: `refreshDeviceSession` reads `revoked_at` and knows nothing about pseudonyms.
//
// A mark rather than a delete, per 000104, so the reason survives the session: `account_deleted` is
// the fourth value on `ck_device_sessions_revoked_reason` and 000107 is what added it.
//
// Guarded on `revoked_at IS NULL` so a session the person had already signed out of keeps the reason
// it ended with — three sessions of which one was revoked by its owner should not read afterwards as
// three deletions.
func (postgresStore) revokeSessionsForDeletedAccount(ctx context.Context, r db.Runner, userID uuid.UUID, at time.Time) (int64, error) {
	tag, err := r.Exec(ctx, `
		UPDATE device_sessions
		   SET revoked_at = $2, revoked_reason = $3
		 WHERE user_id = $1 AND revoked_at IS NULL`, userID, at, revokedReasonAccountDeleted)
	if err != nil {
		return 0, fmt.Errorf("identity: revoking the sessions of a pseudonymised account: %w", err)
	}
	return tag.RowsAffected(), nil
}
