package identity

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/DulsaraNethmin/Shipper/services/core/internal/clock"
	"github.com/DulsaraNethmin/Shipper/services/core/internal/db"
	"github.com/DulsaraNethmin/Shipper/services/core/internal/passwords"
)

// SHIP-171 against a real PostgreSQL, per Docs/06 §4.1.
//
// # What these tests are shaped against
//
// The *Done when* is "profile and contact data are irreversibly replaced by a stable pseudonym",
// and it has three clauses that fail in three different ways.
//
//   - The implementation that satisfies **"replaced"** and nothing else overwrites `users` and
//     leaves the copies. TestNothingInTheSchemaStillHoldsThePerson is what refuses it: it sweeps
//     every text column in every table rather than the ones somebody thought of.
//   - The implementation that satisfies **"stable"** and nothing else derives the pseudonym from a
//     sequence or a clock. It looks identical in the row.
//     TestThePseudonymIsAPureFunctionOfTheAccount is what refuses it.
//   - The implementation that satisfies **"irreversibly"** and nothing else keeps the old address
//     somewhere for support to look at. The same schema sweep refuses it, which is why that test is
//     a sweep and not a list.

// recordingPersonalDetails is the [PersonalDetails] port's test double.
//
// It records what it was asked to write rather than writing anything, because the two tables behind
// the real one belong to other domains and the statements that reach them live in cmd/worker. The
// **real** adapter is exercised against a real database in cmd/worker/tasks_identity_test.go, for
// the reason deletion_test.go gives about fakeActiveJobs: a double here proves the domain's
// behaviour and nothing about the adapter's, and wave 12 recorded what happens when only one of
// those is tested.
type recordingPersonalDetails struct {
	calls []Pseudonym
	err   error
}

func (r *recordingPersonalDetails) Replace(_ context.Context, _ db.Runner, _ uuid.UUID, with Pseudonym) (int, error) {
	if r.err != nil {
		return 0, r.err
	}
	r.calls = append(r.calls, with)
	return len(r.calls), nil
}

// TestThePseudonymIsAPureFunctionOfTheAccount is the *stable* clause, on its own.
//
// **This is the clause a row cannot demonstrate.** A pseudonym drawn from a sequence, from
// `gen_random_uuid()` or from the clock produces a row indistinguishable from this one; what
// separates them is whether computing it twice gives the same answer.
func TestThePseudonymIsAPureFunctionOfTheAccount(t *testing.T) {
	t.Parallel()

	id := uuid.MustParse("0198a1b2-c3d4-7e5f-8a9b-0c1d2e3f4a5b")

	first, second := PseudonymFor(id), PseudonymFor(id)
	if first != second {
		t.Errorf("PseudonymFor(%s) gave %+v then %+v — it is not stable, so a replay would "+
			"write a different pseudonym over the same account", id, first, second)
	}

	other := PseudonymFor(uuid.MustParse("0198a1b2-c3d4-7e5f-8a9b-0c1d2e3f4a5c"))
	if other.Token == first.Token {
		t.Errorf("two accounts share the token %q; uq_users_email and uq_users_phone are both "+
			"UNIQUE over NOT NULL columns, so the second account could never be pseudonymised",
			first.Token)
	}
	if other.Name == first.Name || other.TradingName == first.TradingName {
		t.Errorf("two accounts render identically as %q / %q, so a counterparty who dealt with "+
			"both sees one person", first.Name, first.TradingName)
	}

	t.Run("and it names nothing that could be presented at an endpoint", func(t *testing.T) {
		if plausibleEmail(first.Token) {
			t.Errorf("%q parses as an email address, so it could be handed to sign-in, to a "+
				"password reset, or to a verification resend", first.Token)
		}
		if validE164(first.Token) {
			t.Errorf("%q parses as a phone number, so it could be handed to the OTP endpoints",
				first.Token)
		}
		if !strings.Contains(first.Token, id.String()) {
			t.Errorf("the token %q does not carry the account identifier, so it is not derived "+
				"from the row", first.Token)
		}
	})
}

// TestNewPseudonymiserRefusesAMissingCollaborator keeps a wiring mistake at startup.
func TestNewPseudonymiserRefusesAMissingCollaborator(t *testing.T) {
	t.Parallel()

	clk := clock.NewFixed(deletionRequestedAt)

	for name, build := range map[string]func() (*Pseudonymiser, error){
		"no active-job lookup": func() (*Pseudonymiser, error) {
			return NewPseudonymiser(nil, &recordingPersonalDetails{}, clk)
		},
		"no personal details": func() (*Pseudonymiser, error) {
			return NewPseudonymiser(noDelivery, nil, clk)
		},
		"no clock": func() (*Pseudonymiser, error) {
			return NewPseudonymiser(noDelivery, &recordingPersonalDetails{}, nil)
		},
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := build(); err == nil {
				t.Error("a pseudonymiser was built without it")
			}
		})
	}
}

// dueDeletion registers an account, gives it every artefact that carries a copy of its contact
// details, and records a deletion request that is due.
//
// The artefacts are made through the service rather than by INSERT, so the rows are the ones the
// platform actually writes: registration issues an email verification token carrying the address,
// RequestOTP issues a code row carrying the number, and SignIn opens a device session.
func dueDeletion(t *testing.T, svc *Service, clk *clock.Fixed, suffix string) (User, uuid.UUID) {
	t.Helper()

	cmd := validRegistration()
	cmd.Name = "Deletable Person " + suffix
	cmd.Email = "pseudonym-" + suffix + "@example.com"
	cmd.Phone = "+6142000" + suffix

	user, err := svc.Register(t.Context(), cmd)
	if err != nil {
		t.Fatalf("registering: %v", err)
	}

	if _, err := svc.RequestOTP(t.Context(), user.Phone); err != nil {
		t.Fatalf("issuing a one-time code: %v", err)
	}

	if _, err := svc.SignIn(t.Context(), SignInCommand{
		Email: cmd.Email, Password: cmd.Password,
		DeviceLabel: "Deletable Handset", ClientIP: "198.51.100." + suffix[len(suffix)-1:],
	}); err != nil {
		t.Fatalf("signing in: %v", err)
	}

	request, _, err := svc.RequestDeletion(t.Context(), user.ID)
	if err != nil {
		t.Fatalf("requesting deletion: %v", err)
	}

	// The clock moves past the promise rather than the row being backdated, which is the
	// property the sweep is built on: `complete_by` is stored, so a test advances time and a
	// request falls due exactly as it would in production.
	clk.Instant = request.CompleteBy.Add(time.Minute)
	return user, request.ID
}

// TestExecutingADueRequestReplacesProfileAndContactData is the criterion, first two clauses.
func TestExecutingADueRequestReplacesProfileAndContactData(t *testing.T) {
	clk := clock.NewFixed(deletionRequestedAt)
	svc, pool, _ := newTestServiceWithSMS(t, clk)
	user, requestID := dueDeletion(t, svc, clk, "1710")

	elsewhere := &recordingPersonalDetails{}
	executor, err := NewPseudonymiser(noDelivery, elsewhere, clk)
	if err != nil {
		t.Fatalf("building the pseudonymiser: %v", err)
	}

	var outcome DeletionOutcome
	if err := db.InTx(t.Context(), pool, func(ctx context.Context, r db.Runner) error {
		outcome, err = executor.Execute(ctx, r, requestID)
		return err
	}); err != nil {
		t.Fatalf("executing the request: %v", err)
	}
	if outcome != DeletionExecuted {
		t.Fatalf("outcome = %q, want %q", outcome, DeletionExecuted)
	}

	want := PseudonymFor(user.ID)

	t.Run("the account row names nobody", func(t *testing.T) {
		var name, email, phone string
		if err := pool.QueryRow(t.Context(),
			`SELECT name, email::text, phone FROM users WHERE id = $1`, user.ID).
			Scan(&name, &email, &phone); err != nil {
			t.Fatalf("reading the account: %v", err)
		}

		if name != want.Name {
			t.Errorf("users.name = %q, want %q", name, want.Name)
		}
		if email != want.Token {
			t.Errorf("users.email = %q, want %q", email, want.Token)
		}
		if phone != want.Token {
			t.Errorf("users.phone = %q, want %q", phone, want.Token)
		}
	})

	t.Run("and the row it hangs off is still there", func(t *testing.T) {
		// Docs/05 §3.1 retains the transaction. Sixteen migrations carry REFERENCES users
		// and every one is ON DELETE RESTRICT, so an implementation that deleted the account
		// would fail on the first of them — and if it did not, it would take the
		// counterparty's history with it.
		var role, status string
		if err := pool.QueryRow(t.Context(),
			`SELECT role, status FROM users WHERE id = $1`, user.ID).Scan(&role, &status); err != nil {
			t.Fatalf("the account row is gone: %v", err)
		}
		if role != string(user.Role) {
			t.Errorf("users.role = %q, want the unchanged %q — every job and bid hanging off "+
				"this account depends on it", role, user.Role)
		}
	})

	t.Run("the request is completed and the promise it kept is unmoved", func(t *testing.T) {
		var state string
		var completeBy, updatedAt time.Time
		if err := pool.QueryRow(t.Context(),
			`SELECT state, complete_by, updated_at FROM account_deletion_requests WHERE id = $1`,
			requestID).Scan(&state, &completeBy, &updatedAt); err != nil {
			t.Fatalf("reading the request: %v", err)
		}

		if state != string(DeletionCompleted) {
			t.Errorf("state = %q, want %q", state, DeletionCompleted)
		}
		// The date the person was told, not the moment the sweep ran. updated_at is what
		// says when it ran, which is the trigger 000105 installed for this ticket.
		if !completeBy.UTC().Equal(deletionRequestedAt.Add(DeletionWindow)) {
			t.Errorf("complete_by = %s, want the promised %s — a completed request holds the "+
				"promise that was kept", completeBy.UTC(), deletionRequestedAt.Add(DeletionWindow))
		}
	})

	t.Run("every session the account held is revoked", func(t *testing.T) {
		var live int
		var reason string
		if err := pool.QueryRow(t.Context(), `
			SELECT count(*) FILTER (WHERE revoked_at IS NULL),
			       coalesce(max(revoked_reason), '')
			  FROM device_sessions WHERE user_id = $1`, user.ID).Scan(&live, &reason); err != nil {
			t.Fatalf("reading the sessions: %v", err)
		}

		if live != 0 {
			t.Errorf("%d sessions are still live on a pseudonymised account, so a handset "+
				"holding a refresh token can go on using it", live)
		}
		if reason != revokedReasonAccountDeleted {
			t.Errorf("revoked_reason = %q, want %q", reason, revokedReasonAccountDeleted)
		}
	})

	t.Run("and the copies outside this package were asked for", func(t *testing.T) {
		if len(elsewhere.calls) != 1 {
			t.Fatalf("PersonalDetails.Replace was called %d times, want once", len(elsewhere.calls))
		}
		if elsewhere.calls[0] != want {
			t.Errorf("Replace got %+v, want %+v — the copies must carry the same pseudonym as "+
				"the original or the account has two", elsewhere.calls[0], want)
		}
	})
}

// TestNothingInTheSchemaStillHoldsThePerson is the *irreversibly* clause, and it is a sweep rather
// than a list on purpose.
//
// # Why it reads information_schema rather than naming the tables
//
// A test that checked `users`, `email_verification_tokens` and `phone_otps` would pass on the day it
// was written and would go on passing when somebody added a twelfth table with a `contact_email`
// column. The whole risk this clause carries is a copy **nobody was thinking about** — the sweep
// that produced this ticket's scope found `notifications.address` exactly that way, a column in
// another domain holding "an email address, or an E.164 number … resolved once, when the row is
// written, from users".
//
// So it asks PostgreSQL for every text-shaped column in the public schema and looks for the person
// in all of them, including columns that do not exist yet. A copy added later fails here.
func TestNothingInTheSchemaStillHoldsThePerson(t *testing.T) {
	clk := clock.NewFixed(deletionRequestedAt)
	svc, pool, _ := newTestServiceWithSMS(t, clk)
	user, requestID := dueDeletion(t, svc, clk, "1711")

	// The values as they were stored, read before anything is executed. Read from the row
	// rather than from the command, so normalisation cannot make the test look for a string
	// the database never held.
	var wasName, wasEmail, wasPhone string
	if err := pool.QueryRow(t.Context(),
		`SELECT name, email::text, phone FROM users WHERE id = $1`, user.ID).
		Scan(&wasName, &wasEmail, &wasPhone); err != nil {
		t.Fatalf("reading the account before: %v", err)
	}

	// The fixture is verified rather than assumed: a sweep for a value that was never stored
	// passes forever and proves nothing. This is wave 12's "check that your fixtures make the
	// subject reachable" applied to a test whose whole assertion is an absence.
	before := columnsHolding(t, pool, wasEmail)
	if len(before) < 2 {
		t.Fatalf("before executing, %q appears in %v — the fixture has to put the address in "+
			"more than one table or this test cannot fail", wasEmail, before)
	}

	executor, err := NewPseudonymiser(noDelivery, &recordingPersonalDetails{}, clk)
	if err != nil {
		t.Fatalf("building the pseudonymiser: %v", err)
	}
	if err := db.InTx(t.Context(), pool, func(ctx context.Context, r db.Runner) error {
		_, err := executor.Execute(ctx, r, requestID)
		return err
	}); err != nil {
		t.Fatalf("executing the request: %v", err)
	}

	for label, value := range map[string]string{
		"address": wasEmail,
		"number":  wasPhone,
		"name":    wasName,
	} {
		if found := columnsHolding(t, pool, value); len(found) > 0 {
			t.Errorf("the %s %q survives in %v — %q is not irreversible while a join "+
				"recovers the person from any one of them", label, value, found, "replaced")
		}
	}
}

// columnsHolding names every text-shaped column in the public schema containing needle.
//
// `strpos` rather than equality, so a value embedded in prose is found as readily as a column that
// is a verbatim copy — a message body quoting the address is still the address.
//
// `::text` on every column, because `users.email` is `citext` and the comparison must be the same
// one on every column rather than depending on each column's type.
func columnsHolding(t *testing.T, pool *pgxpool.Pool, needle string) []string {
	t.Helper()

	if strings.TrimSpace(needle) == "" {
		t.Fatal("searching the schema for an empty string would match every row")
	}

	rows, err := pool.Query(t.Context(), `
		SELECT table_name, column_name
		  FROM information_schema.columns
		 WHERE table_schema = 'public'
		   AND data_type IN ('text', 'character varying', 'character', 'USER-DEFINED')
		 ORDER BY table_name, column_name`)
	if err != nil {
		t.Fatalf("listing the text columns: %v", err)
	}

	type column struct{ table, name string }
	columns, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (column, error) {
		var c column
		return c, r.Scan(&c.table, &c.name)
	})
	if err != nil {
		t.Fatalf("collecting the text columns: %v", err)
	}
	if len(columns) < 20 {
		t.Fatalf("only %d text columns were found, so this sweep is not seeing the schema", len(columns))
	}

	var found []string
	for _, c := range columns {
		var hits int
		q := fmt.Sprintf(
			`SELECT count(*) FROM %q WHERE strpos(%q::text, $1) > 0`, c.table, c.name)
		if err := pool.QueryRow(t.Context(), q, needle).Scan(&hits); err != nil {
			// An enum-backed USER-DEFINED column that cannot be cast is not a place a
			// contact detail can hide, so it is skipped rather than failing the sweep.
			continue
		}
		if hits > 0 {
			found = append(found, c.table+"."+c.name)
		}
	}
	return found
}

// TestACompletedRequestDoesNotStopTheSameAccountAskingAgain is the behavioural half of the
// open-state pairing, and it exists because a pairing guard is a text guard.
//
// TestTheOpenStatesInSQLAreTheOnesTheDomainDeclares reads [openDeletionStatesSQL] and compares it
// with [DeletionState.Open]; TestTheOpenStatesTheIndexCoversAreTheOnesTheDomainCallsOpen reads the
// index predicate out of `pg_indexes`. **Neither runs a query.** Adding 'completed' to the open
// list would make `ON CONFLICT` swallow every later request from an account that has already been
// deleted and hand back the executed one, and this is the only test in the repository that would
// notice the *behaviour* rather than the disagreement.
func TestACompletedRequestDoesNotStopTheSameAccountAskingAgain(t *testing.T) {
	clk := clock.NewFixed(deletionRequestedAt)
	svc, pool, _ := newTestServiceWithSMS(t, clk)
	user, requestID := dueDeletion(t, svc, clk, "1712")

	executor, err := NewPseudonymiser(noDelivery, &recordingPersonalDetails{}, clk)
	if err != nil {
		t.Fatalf("building the pseudonymiser: %v", err)
	}
	if err := db.InTx(t.Context(), pool, func(ctx context.Context, r db.Runner) error {
		_, err := executor.Execute(ctx, r, requestID)
		return err
	}); err != nil {
		t.Fatalf("executing the request: %v", err)
	}

	again, created, err := svc.RequestDeletion(t.Context(), user.ID)
	if err != nil {
		t.Fatalf("asking again after the account was pseudonymised: %v", err)
	}
	if !created {
		t.Fatalf("the second request was not created — the executed one was handed back as "+
			"though it were live (%s)", again.ID)
	}
	if again.ID == requestID {
		t.Error("asking again returned the completed request")
	}
	if again.State != DeletionRequested {
		t.Errorf("the new request is %q, want %q", again.State, DeletionRequested)
	}

	if n := deletionRequestCount(t, pool, user.ID); n != 2 {
		t.Errorf("the account holds %d requests, want 2 — one completed and one live", n)
	}
}

// TestADueRequestIsHeldWhileADeliveryIsInFlight is Docs/05 §3.1 applied at the moment of execution
// rather than at the moment of asking.
//
// ports.go promised exactly this — "SHIP-171 asks again before it executes, which is the check that
// actually protects the counterparty" — and the request carries no job identifier precisely so that
// the answer cannot be stale.
func TestADueRequestIsHeldWhileADeliveryIsInFlight(t *testing.T) {
	clk := clock.NewFixed(deletionRequestedAt)
	svc, pool, _ := newTestServiceWithSMS(t, clk)
	user, requestID := dueDeletion(t, svc, clk, "1713")

	carrying := &fakeActiveJobs{active: true}
	elsewhere := &recordingPersonalDetails{}
	executor, err := NewPseudonymiser(carrying, elsewhere, clk)
	if err != nil {
		t.Fatalf("building the pseudonymiser: %v", err)
	}

	var outcome DeletionOutcome
	if err := db.InTx(t.Context(), pool, func(ctx context.Context, r db.Runner) error {
		outcome, err = executor.Execute(ctx, r, requestID)
		return err
	}); err != nil {
		t.Fatalf("executing the request: %v", err)
	}

	if outcome != DeletionHeld {
		t.Errorf("outcome = %q, want %q — erasing a party mid-delivery strands the counterparty",
			outcome, DeletionHeld)
	}
	if len(elsewhere.calls) != 0 {
		t.Error("the copies were replaced for an account that is carrying a delivery")
	}

	var name string
	if err := pool.QueryRow(t.Context(), `SELECT name FROM users WHERE id = $1`, user.ID).
		Scan(&name); err != nil {
		t.Fatalf("reading the account: %v", err)
	}
	if strings.HasPrefix(name, "Deleted user") {
		t.Error("the account was pseudonymised while it was party to a delivery")
	}

	state, _, completeBy := deletionRequestRow(t, pool, user.ID)
	if state != string(DeletionDeferred) {
		t.Errorf("the request is %q, want %q", state, DeletionDeferred)
	}
	if !completeBy.Equal(clk.Now().UTC().Add(DeletionWindow)) {
		t.Errorf("complete_by = %s, want thirty days from the hold at %s — a date the platform "+
			"knew it would miss is not what anybody was told", completeBy, clk.Now().UTC())
	}
}

// TestADueDeferralBecomesLiveRatherThanExecuted closes the gap deletion.go recorded: "a person who
// never asks again stays deferred until SHIP-171 looks".
//
// The person is owed the window they were promised, and SHIP-170's rule is that it starts when the
// deferral lifts rather than having run while they were waiting — so a deferral that lifts here is
// **not** executed in the same pass.
func TestADueDeferralBecomesLiveRatherThanExecuted(t *testing.T) {
	clk := clock.NewFixed(deletionRequestedAt)
	svc, pool, _ := newTestServiceWithSMS(t, clk)

	hasher, err := passwords.NewHasher(testProfile)
	if err != nil {
		t.Fatalf("building the hasher: %v", err)
	}

	carrying := &fakeActiveJobs{active: true}
	deferring, err := NewService(pool, hasher, testServiceIssuer(t, clk), testLimiter(t),
		&recordingSender{}, &recordingTexter{}, carrying, clk)
	if err != nil {
		t.Fatalf("building the deferring service: %v", err)
	}
	_ = svc

	cmd := validRegistration()
	cmd.Email = "pseudonym-1714@example.com"
	cmd.Phone = "+61420001714"
	user, err := deferring.Register(t.Context(), cmd)
	if err != nil {
		t.Fatalf("registering: %v", err)
	}

	request, _, err := deferring.RequestDeletion(t.Context(), user.ID)
	if err != nil {
		t.Fatalf("requesting deletion: %v", err)
	}
	if request.State != DeletionDeferred {
		t.Fatalf("the fixture request is %q, want %q", request.State, DeletionDeferred)
	}

	// The delivery closes, and the promised date arrives with nobody having asked again.
	carrying.active = false
	clk.Instant = request.CompleteBy.Add(time.Minute)

	elsewhere := &recordingPersonalDetails{}
	executor, err := NewPseudonymiser(carrying, elsewhere, clk)
	if err != nil {
		t.Fatalf("building the pseudonymiser: %v", err)
	}

	var outcome DeletionOutcome
	if err := db.InTx(t.Context(), pool, func(ctx context.Context, r db.Runner) error {
		outcome, err = executor.Execute(ctx, r, request.ID)
		return err
	}); err != nil {
		t.Fatalf("executing the request: %v", err)
	}

	if outcome != DeletionRestarted {
		t.Fatalf("outcome = %q, want %q", outcome, DeletionRestarted)
	}
	if len(elsewhere.calls) != 0 {
		t.Error("a deferral that lifted was executed in the same pass, so the thirty days the " +
			"person was promised never ran")
	}

	state, _, completeBy := deletionRequestRow(t, pool, user.ID)
	if state != string(DeletionRequested) {
		t.Errorf("the request is %q, want %q", state, DeletionRequested)
	}
	if !completeBy.Equal(clk.Now().UTC().Add(DeletionWindow)) {
		t.Errorf("complete_by = %s, want thirty days from the lift at %s", completeBy, clk.Now().UTC())
	}
}

// TestOnlyDueOpenRequestsAreClaimed is the query the whole sweep rests on, and the reason
// `cmd/worker` can register a sixth task without changing what any other verify section does.
//
// Three rows, three answers: a request thirty days from its date is not due, one whose date has
// passed is, and a completed one is never claimed again however long ago it was executed.
func TestOnlyDueOpenRequestsAreClaimed(t *testing.T) {
	clk := clock.NewFixed(deletionRequestedAt)
	svc, pool, _ := newTestServiceWithSMS(t, clk)

	due, dueRequest := dueDeletion(t, svc, clk, "1716")

	// **The fresh request is made after the clock has moved**, not before it. Both accounts
	// would otherwise have been promised the same instant — `dueDeletion` advances past its own
	// request's `complete_by`, which is thirty days from whatever the clock said when it ran —
	// and this test would have been asserting that a due request is due. Caught by the assertion
	// below failing on the first run, which is the fixture check wave 12 asks for.
	fresh := registerFor(t, svc, "1715")
	if _, _, err := svc.RequestDeletion(t.Context(), fresh.ID); err != nil {
		t.Fatalf("requesting deletion for the fresh account: %v", err)
	}

	executor, err := NewPseudonymiser(noDelivery, &recordingPersonalDetails{}, clk)
	if err != nil {
		t.Fatalf("building the pseudonymiser: %v", err)
	}

	claim := func(at time.Time) []uuid.UUID {
		t.Helper()

		var claimed []uuid.UUID
		if err := db.InTx(t.Context(), pool, func(ctx context.Context, r db.Runner) error {
			rows, err := r.Query(ctx, DueDeletionClaim, at, DeletionBatch)
			if err != nil {
				return err
			}
			claimed, err = pgx.CollectRows(rows, pgx.RowTo[uuid.UUID])
			return err
		}); err != nil {
			t.Fatalf("claiming: %v", err)
		}
		return claimed
	}

	claimed := claim(clk.Now())
	if !contains(claimed, dueRequest) {
		t.Errorf("the request whose date has passed was not claimed: %v", claimed)
	}
	for _, id := range claimed {
		var owner uuid.UUID
		if err := pool.QueryRow(t.Context(),
			`SELECT user_id FROM account_deletion_requests WHERE id = $1`, id).Scan(&owner); err != nil {
			t.Fatalf("reading a claimed request: %v", err)
		}
		if owner == fresh.ID {
			t.Error("a request thirty days from its promised date was claimed — every section " +
				"that starts cmd/worker would then be executing deletions it never mentioned")
		}
	}

	if err := db.InTx(t.Context(), pool, func(ctx context.Context, r db.Runner) error {
		_, err := executor.Execute(ctx, r, dueRequest)
		return err
	}); err != nil {
		t.Fatalf("executing: %v", err)
	}
	_ = due

	if again := claim(clk.Now().Add(365 * 24 * time.Hour)); contains(again, dueRequest) {
		t.Error("a completed request was claimed again a year later, so the sweep would " +
			"re-execute it forever")
	}
}

func contains(ids []uuid.UUID, want uuid.UUID) bool {
	for _, id := range ids {
		if id == want {
			return true
		}
	}
	return false
}

// TestPseudonymisingTwiceWritesTheSameValues is the *stable* clause where it can be observed: in the
// row, across two executions.
//
// A replay cannot happen through the claim — [DeletionCompleted] is not open — so the store is
// driven directly. That is the point: the property is of the writer rather than of the guard in
// front of it, and a restore, a repair script or a psql prompt is not behind the guard.
func TestPseudonymisingTwiceWritesTheSameValues(t *testing.T) {
	clk := clock.NewFixed(deletionRequestedAt)
	svc, pool, _ := newTestServiceWithSMS(t, clk)
	user, requestID := dueDeletion(t, svc, clk, "1717")

	executor, err := NewPseudonymiser(noDelivery, &recordingPersonalDetails{}, clk)
	if err != nil {
		t.Fatalf("building the pseudonymiser: %v", err)
	}
	if err := db.InTx(t.Context(), pool, func(ctx context.Context, r db.Runner) error {
		_, err := executor.Execute(ctx, r, requestID)
		return err
	}); err != nil {
		t.Fatalf("executing: %v", err)
	}

	first := accountFingerprint(t, pool, user.ID)

	clk.Instant = clk.Now().Add(72 * time.Hour)
	if err := db.InTx(t.Context(), pool, func(ctx context.Context, r db.Runner) error {
		return executor.pseudonymise(ctx, r, user.ID, clk.Now().UTC())
	}); err != nil {
		t.Fatalf("pseudonymising a second time: %v", err)
	}

	if second := accountFingerprint(t, pool, user.ID); second != first {
		t.Errorf("a replay wrote %q where the first pass wrote %q — the pseudonym is derived "+
			"from something that moves", second, first)
	}
}

func accountFingerprint(t *testing.T, pool *pgxpool.Pool, userID uuid.UUID) string {
	t.Helper()

	var name, email, phone string
	if err := pool.QueryRow(t.Context(),
		`SELECT name, email::text, phone FROM users WHERE id = $1`, userID).
		Scan(&name, &email, &phone); err != nil {
		t.Fatalf("reading the account: %v", err)
	}
	return strings.Join([]string{name, email, phone}, "|")
}

// TestAFailureInOneStoreLeavesNothingHalfDone is why every write is in the caller's transaction.
//
// An account pseudonymised in `users` and still named in `notifications` is worse than one that was
// not touched: the request would be completed, so no later pass would try again, and the copy would
// stay for as long as the row did.
func TestAFailureInOneStoreLeavesNothingHalfDone(t *testing.T) {
	clk := clock.NewFixed(deletionRequestedAt)
	svc, pool, _ := newTestServiceWithSMS(t, clk)
	user, requestID := dueDeletion(t, svc, clk, "1718")

	refusing := &recordingPersonalDetails{err: errUnavailable}
	executor, err := NewPseudonymiser(noDelivery, refusing, clk)
	if err != nil {
		t.Fatalf("building the pseudonymiser: %v", err)
	}

	if err := db.InTx(t.Context(), pool, func(ctx context.Context, r db.Runner) error {
		_, err := executor.Execute(ctx, r, requestID)
		return err
	}); err == nil {
		t.Fatal("a failure reaching the copies was reported as a successful execution")
	}

	var email string
	if err := pool.QueryRow(t.Context(),
		`SELECT email::text FROM users WHERE id = $1`, user.ID).Scan(&email); err != nil {
		t.Fatalf("reading the account: %v", err)
	}
	if email != "pseudonym-1718@example.com" {
		t.Errorf("users.email = %q — the account was pseudonymised inside a transaction that "+
			"then failed, so the rollback did not carry it", email)
	}

	state, _, _ := deletionRequestRow(t, pool, user.ID)
	if state != string(DeletionRequested) {
		t.Errorf("the request is %q — a failed execution must leave it open for the next pass",
			state)
	}
}
