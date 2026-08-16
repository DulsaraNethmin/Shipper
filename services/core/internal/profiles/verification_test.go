package profiles

import (
	"context"
	"errors"
	"regexp"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/DulsaraNethmin/Shipper/services/core/internal/clock"
	"github.com/DulsaraNethmin/Shipper/services/core/internal/db"
	"github.com/DulsaraNethmin/Shipper/services/core/internal/httpx"
	"github.com/DulsaraNethmin/Shipper/services/core/internal/testsupport/pgtest"
)

// SHIP-81a against a real PostgreSQL.
//
// Docs/06 §4.1 is the argument for not mocking it, and this domain is the clearest case in the
// repository so far: **the rule this ticket exists to enforce is a trigger.** A mocked store would
// accept every direct write to `state` that `provider_verification_change_is_guarded` refuses, so a
// suite written against one would pass with the guard deleted — which is precisely the mutation this
// branch was asked to run.

var testInstant = time.Date(2026, 8, 18, 4, 15, 0, 0, time.UTC)

func newTestService() *Service { return NewService(clock.NewFixed(testInstant)) }

// newAccount inserts a user with the role the test needs.
//
// `000200`'s trigger gives it a Pending verification record on the way in when the role is
// `provider`, which is the behaviour half of these tests are about.
func newAccount(t *testing.T, pool *pgxpool.Pool, email, phone, role string) uuid.UUID {
	t.Helper()

	id, err := uuid.NewV7()
	if err != nil {
		t.Fatalf("generating an id: %v", err)
	}
	if _, err := pool.Exec(t.Context(),
		`INSERT INTO users (id, email, phone, password_hash, role) VALUES ($1, $2, $3, 'x', $4)`,
		id, email, phone, role); err != nil {
		t.Fatalf("inserting %s: %v", email, err)
	}
	return id
}

func newProvider(t *testing.T, pool *pgxpool.Pool, email, phone string) uuid.UUID {
	t.Helper()
	return newAccount(t, pool, email, phone, "provider")
}

// newAdmin inserts an administrator, who is the ordinary actor on a decision.
//
// A separate table from `users` (SHIP-147): `ck_users_role` refuses 'admin', and Docs/06 §5.2 keeps
// the two credentials apart. `actor_id` has no foreign key, so this could be any UUID — it is a real
// row because a test that used an invented identifier would not notice the day one is added.
func newAdmin(t *testing.T, pool *pgxpool.Pool, email string) uuid.UUID {
	t.Helper()

	id, err := uuid.NewV7()
	if err != nil {
		t.Fatalf("generating an id: %v", err)
	}
	if _, err := pool.Exec(t.Context(),
		`INSERT INTO admin_users (id, email, name, password_hash)
		 VALUES ($1, $2, 'Verify Admin', '$argon2id$v=19$m=65536,t=3,p=4$c2FsdHNhbHQ$aGFzaGhhc2g')`,
		id, email); err != nil {
		t.Fatalf("inserting the administrator %s: %v", email, err)
	}
	return id
}

// inTx runs fn inside a transaction, which [Service.Decide] requires.
func inTx(t *testing.T, pool *pgxpool.Pool, fn func(ctx context.Context, r db.Runner) error) error {
	t.Helper()
	return db.InTx(t.Context(), pool, fn)
}

// decide moves a provider through the domain rather than by writing rows.
//
// It unwraps [Decision] to the record, because that is what almost every test here is about. The
// other half — the state the provider was moved *from* — is what SHIP-154 writes into its audit
// entry, and [TestADecisionReportsBothEnds] is where it is exercised.
func decide(t *testing.T, pool *pgxpool.Pool, provider uuid.UUID, to State, by Actor, reason string) (Verification, error) {
	t.Helper()

	decision, err := decided(t, pool, provider, to, by, reason)
	return decision.Verification, err
}

// decided is [decide] without the unwrapping, for the tests that care about both ends.
func decided(t *testing.T, pool *pgxpool.Pool, provider uuid.UUID, to State, by Actor, reason string) (Decision, error) {
	t.Helper()

	var out Decision
	err := inTx(t, pool, func(ctx context.Context, r db.Runner) error {
		var err error
		out, err = newTestService().Decide(ctx, r, provider, to, by, reason)
		return err
	})
	return out, err
}

func stateOf(t *testing.T, pool *pgxpool.Pool, provider uuid.UUID) string {
	t.Helper()

	var state string
	if err := pool.QueryRow(t.Context(),
		`SELECT state FROM provider_verifications WHERE provider_id = $1`, provider).Scan(&state); err != nil {
		t.Fatalf("reading the state of %s: %v", provider, err)
	}
	return state
}

// TestTheFiveStatesAreTheConstraintsFiveStates is Docs/10 §3.4's pairing, in both directions.
//
// [States] is a Go copy of `ck_provider_verifications_state`, and neither is derived from the other
// — which is the point. A test that built its expectation by reading the constraint would agree with
// a constraint that had lost a state, and one that only checked the Go list would agree with a Go
// list that had. Both directions is what makes the pair honest.
//
// The five names themselves come from Docs/04 §4 and are written out here a third time on purpose:
// if this file, the migration and the document ever disagree, the document wins and this is where
// that argument starts.
func TestTheFiveStatesAreTheConstraintsFiveStates(t *testing.T) {
	pool := pgtest.DB(t)

	var definition string
	if err := pool.QueryRow(t.Context(),
		`SELECT pg_get_constraintdef(oid) FROM pg_constraint
		 WHERE conname = 'ck_provider_verifications_state'`,
	).Scan(&definition); err != nil {
		t.Fatalf("reading ck_provider_verifications_state: %v", err)
	}

	quoted := regexp.MustCompile(`'([^']*)'`)
	var accepted []string
	for _, match := range quoted.FindAllStringSubmatch(definition, -1) {
		accepted = append(accepted, match[1])
	}

	// Docs/04 §4, written out rather than derived.
	document := []string{"Pending", "Verified", "Restricted", "Rejected", "Suspended"}

	for _, state := range document {
		if !containsString(accepted, state) {
			t.Errorf("Docs/04 §4 gives the outcome %q and the CHECK does not accept it: %v", state, accepted)
		}
		if !State(state).Valid() {
			t.Errorf("Docs/04 §4 gives the outcome %q and profiles.States does not carry it: %v", state, States)
		}
	}
	for _, state := range accepted {
		if !containsString(document, state) {
			t.Errorf("the CHECK accepts %q, which is not one of Docs/04 §4's five outcomes", state)
		}
	}
	for _, state := range States {
		if !containsString(document, string(state)) {
			t.Errorf("profiles.States carries %q, which is not one of Docs/04 §4's five outcomes", state)
		}
	}
	if len(States) != len(document) {
		t.Errorf("profiles.States has %d entries and Docs/04 §4 has %d", len(States), len(document))
	}
}

// TestEveryProviderHasAPendingRecordFromRegistration.
//
// The decision this ticket had to take about who creates the record, demonstrated rather than
// asserted. SHIP-153's queue is "pending provider verifications listed oldest first", and a queue
// over a table that only holds *decided* providers lists nobody who is waiting — so Pending has to be
// a row.
func TestEveryProviderHasAPendingRecordFromRegistration(t *testing.T) {
	pool := pgtest.DB(t)
	provider := newProvider(t, pool, "pv-registered@example.com", "+61400200001")

	found, err := newTestService().VerificationFor(t.Context(), pool, provider)
	if err != nil {
		t.Fatalf("VerificationFor() = %v", err)
	}
	if found.State != StatePending {
		t.Errorf("a provider registers at %q, want Pending", found.State)
	}
	if found.Decided() {
		t.Errorf("a provider nobody has reviewed has a decision clock: %v", found.DecidedAt)
	}
	if found.Reason != "" {
		t.Errorf("a provider nobody has reviewed has the reason %q", found.Reason)
	}
	if found.SubmittedAt.IsZero() {
		t.Error("the record has no creation clock, so a review queue cannot order by it")
	}
}

// TestACustomerHasNoVerificationRecord — the trigger is conditional, and this is the condition.
//
// Most accounts on this platform are customers. A record for each of them would put every customer in
// SHIP-153's review queue, which is a queue of people nobody will ever review.
func TestACustomerHasNoVerificationRecord(t *testing.T) {
	pool := pgtest.DB(t)
	customer := newAccount(t, pool, "pv-customer@example.com", "+61400200002", "customer")

	var rows int
	if err := pool.QueryRow(t.Context(),
		`SELECT count(*) FROM provider_verifications WHERE provider_id = $1`, customer).Scan(&rows); err != nil {
		t.Fatalf("counting: %v", err)
	}
	if rows != 0 {
		t.Errorf("a customer has %d verification records, want 0", rows)
	}

	// And the read refuses them rather than answering an empty record, which is SHIP-78a's reading
	// applied to a surface built after it.
	if _, err := newTestService().VerificationFor(t.Context(), pool, customer); !errors.Is(err, ErrNotProvider) {
		t.Errorf("VerificationFor() for a customer = %v, want ErrNotProvider", err)
	}
}

// TestATransitionRecordsItsActorAndItsReason is the middle clause of SHIP-81a's *Done when*.
func TestATransitionRecordsItsActorAndItsReason(t *testing.T) {
	pool := pgtest.DB(t)
	provider := newProvider(t, pool, "pv-decided@example.com", "+61400200003")
	admin := newAdmin(t, pool, "pv-reviewer@example.com")

	const why = "Licence, registration and insurance all current and matching the account."

	after, err := decide(t, pool, provider, StateVerified, Actor{Type: ActorAdmin, ID: admin}, why)
	if err != nil {
		t.Fatalf("Decide() = %v", err)
	}
	if after.State != StateVerified {
		t.Errorf("the record says %q after a decision to Verify", after.State)
	}
	if after.Reason != why {
		t.Errorf("the reason read back is %q, want %q", after.Reason, why)
	}
	if !after.Decided() {
		t.Error("a decided record has no decision clock")
	}

	var (
		from, to, actorType, reason string
		actorID                     uuid.UUID
	)
	if err := pool.QueryRow(t.Context(),
		`SELECT from_state, to_state, actor_type, actor_id, reason
		 FROM provider_verification_decisions WHERE provider_id = $1`, provider,
	).Scan(&from, &to, &actorType, &actorID, &reason); err != nil {
		t.Fatalf("reading the decision: %v", err)
	}

	if from != "Pending" || to != "Verified" {
		t.Errorf("the decision records %s → %s, want Pending → Verified", from, to)
	}
	if actorType != "admin" || actorID != admin {
		t.Errorf("the decision records %s/%s, want admin/%s", actorType, actorID, admin)
	}
	if reason != why {
		t.Errorf("the decision records the reason %q, want %q", reason, why)
	}
}

// TestTheStateIsNotASettableField is the clause "no code sets the state directly", and it is the one
// that has to be demonstrated against the database rather than against the domain.
//
// Three ways of setting it, all refused: a bare UPDATE, an UPDATE in a transaction that has written
// no decision, and an UPDATE naming a decision that describes a different move.
func TestTheStateIsNotASettableField(t *testing.T) {
	pool := pgtest.DB(t)
	provider := newProvider(t, pool, "pv-direct@example.com", "+61400200004")

	// 1. A bare UPDATE, which is what a support query at a psql prompt looks like.
	if _, err := pool.Exec(t.Context(),
		`UPDATE provider_verifications SET state = 'Verified' WHERE provider_id = $1`, provider); err == nil {
		t.Fatal("a direct UPDATE set the verification state; SHIP-81a requires the guarded function")
	}
	if got := stateOf(t, pool, provider); got != "Pending" {
		t.Errorf("the state is %q after a refused direct UPDATE, want Pending", got)
	}

	// 2. Inside a transaction, which is what a second writer in this package would look like. The
	//    transaction-local setting is what the trigger reads, and nothing has set it.
	err := inTx(t, pool, func(ctx context.Context, r db.Runner) error {
		_, err := r.Exec(ctx,
			`UPDATE provider_verifications SET state = 'Verified' WHERE provider_id = $1`, provider)
		return err
	})
	if err == nil {
		t.Fatal("an UPDATE inside a transaction set the verification state with no decision recorded")
	}

	// 3. Naming a real decision that describes a *different* move. This is the case a guard that
	//    merely checked "some decision exists" would let through.
	admin := newAdmin(t, pool, "pv-direct-admin@example.com")
	if _, err := decide(t, pool, provider, StateRestricted, Actor{Type: ActorAdmin, ID: admin},
		"Waiting on a current insurance certificate."); err != nil {
		t.Fatalf("the first decision: %v", err)
	}

	err = inTx(t, pool, func(ctx context.Context, r db.Runner) error {
		var decision uuid.UUID
		if err := r.QueryRow(ctx,
			`SELECT id FROM provider_verification_decisions WHERE provider_id = $1`, provider,
		).Scan(&decision); err != nil {
			return err
		}
		if _, err := r.Exec(ctx,
			`SELECT set_config('shipper.provider_verification_decision', $1, true)`, decision.String()); err != nil {
			return err
		}
		_, err := r.Exec(ctx,
			`UPDATE provider_verifications SET state = 'Verified' WHERE provider_id = $1`, provider)
		return err
	})
	if err == nil {
		t.Fatal("a decision recording Pending → Restricted authorised a move to Verified")
	}
	if got := stateOf(t, pool, provider); got != "Restricted" {
		t.Errorf("the state is %q, want Restricted — the only decided move", got)
	}
}

// TestARecordIsCreatedPendingAndNeverInAnyOtherState.
//
// `000402`'s `jobs_starts_as_a_draft` applied to this table, and for the same reason: a row inserted
// at 'Verified' has been verified by nobody, and there would be no decision naming who decided it.
func TestARecordIsCreatedPendingAndNeverInAnyOtherState(t *testing.T) {
	pool := pgtest.DB(t)
	provider := newProvider(t, pool, "pv-insert@example.com", "+61400200005")

	// The row already exists, so this is really two claims at once: the INSERT is refused on its
	// state before the primary key ever comes into it.
	if _, err := pool.Exec(t.Context(),
		`INSERT INTO provider_verifications (provider_id, state) VALUES ($1, 'Verified')`,
		provider); err == nil {
		t.Fatal("a verification record was created at Verified")
	}
	if got := stateOf(t, pool, provider); got != "Pending" {
		t.Errorf("the state is %q, want Pending", got)
	}
}

// TestADecisionThatChangesNothingIsRefused.
//
// A decision is an act by a person and Docs/04 §6.6 requires it be recorded with a reason; a second
// one that changed nothing would be a row asserting a review took place with no change to show for
// it. Both layers are exercised — the domain's refusal, and the CHECK underneath it.
func TestADecisionThatChangesNothingIsRefused(t *testing.T) {
	pool := pgtest.DB(t)
	provider := newProvider(t, pool, "pv-nochange@example.com", "+61400200006")
	admin := newAdmin(t, pool, "pv-nochange-admin@example.com")

	if _, err := decide(t, pool, provider, StatePending, Actor{Type: ActorAdmin, ID: admin},
		"Looked at it and left it alone."); !errors.Is(err, ErrAlreadyInState) {
		t.Errorf("re-deciding the current state = %v, want ErrAlreadyInState", err)
	}

	// And the layer underneath, reached by calling the function directly — which is what a fixture
	// or a repair script would do.
	if _, err := pool.Exec(t.Context(),
		`SELECT provider_verification_decide($1, 'Pending', 'system', NULL, 'no change')`,
		provider); err == nil {
		t.Error("the database recorded a decision from Pending to Pending")
	}

	var rows int
	if err := pool.QueryRow(t.Context(),
		`SELECT count(*) FROM provider_verification_decisions WHERE provider_id = $1`, provider).Scan(&rows); err != nil {
		t.Fatalf("counting decisions: %v", err)
	}
	if rows != 0 {
		t.Errorf("%d decisions were recorded for a provider nothing happened to", rows)
	}
}

// TestADecisionOutsideATransactionIsRefused.
//
// The domain refuses it with something legible, and the database would refuse it anyway — a
// transaction-local setting outside a transaction lasts only for the statement that set it, so the
// trigger would find nothing.
func TestADecisionOutsideATransactionIsRefused(t *testing.T) {
	pool := pgtest.DB(t)
	provider := newProvider(t, pool, "pv-notx@example.com", "+61400200007")
	admin := newAdmin(t, pool, "pv-notx-admin@example.com")

	_, err := newTestService().Decide(t.Context(), pool, provider, StateVerified,
		Actor{Type: ActorAdmin, ID: admin}, "Everything in order.")
	if !errors.Is(err, ErrNotInTransaction) {
		t.Errorf("Decide() against a pool = %v, want ErrNotInTransaction", err)
	}
	if got := stateOf(t, pool, provider); got != "Pending" {
		t.Errorf("the state is %q after a refused decision, want Pending", got)
	}
}

// TestAProviderReadsTheirOwnStateAndNoOtherProviders.
//
// There is no parameter for whose record to read, so the strongest available demonstration is that
// two providers in different states each read their own — which fails if the query ever stopped
// filtering, in either direction.
func TestAProviderReadsTheirOwnStateAndNoOtherProviders(t *testing.T) {
	pool := pgtest.DB(t)
	admin := newAdmin(t, pool, "pv-two-admin@example.com")

	verified := newProvider(t, pool, "pv-two-verified@example.com", "+61400200008")
	rejected := newProvider(t, pool, "pv-two-rejected@example.com", "+61400200009")

	if _, err := decide(t, pool, verified, StateVerified, Actor{Type: ActorAdmin, ID: admin},
		"All four documents current."); err != nil {
		t.Fatalf("verifying: %v", err)
	}
	if _, err := decide(t, pool, rejected, StateRejected, Actor{Type: ActorAdmin, ID: admin},
		"The licence is in a different name from the account."); err != nil {
		t.Fatalf("rejecting: %v", err)
	}

	svc := newTestService()

	first, err := svc.VerificationFor(t.Context(), pool, verified)
	if err != nil {
		t.Fatalf("VerificationFor(verified) = %v", err)
	}
	second, err := svc.VerificationFor(t.Context(), pool, rejected)
	if err != nil {
		t.Fatalf("VerificationFor(rejected) = %v", err)
	}

	if first.State != StateVerified || first.ProviderID != verified {
		t.Errorf("the verified provider reads %+v", first)
	}
	if second.State != StateRejected || second.ProviderID != rejected {
		t.Errorf("the rejected provider reads %+v", second)
	}
	if first.Reason == second.Reason {
		t.Errorf("both providers read the reason %q; the record is not scoped to the caller", first.Reason)
	}
}

// TestTheDecisionsAreAppendOnly.
//
// Docs/04 §9 forbids ordinary administrators deleting audit history, and a decision that can be
// updated is a rejection that can be rewritten as an approval after somebody complains.
func TestTheDecisionsAreAppendOnly(t *testing.T) {
	pool := pgtest.DB(t)
	provider := newProvider(t, pool, "pv-append@example.com", "+61400200010")
	admin := newAdmin(t, pool, "pv-append-admin@example.com")

	if _, err := decide(t, pool, provider, StateRejected, Actor{Type: ActorAdmin, ID: admin},
		"No insurance certificate supplied."); err != nil {
		t.Fatalf("rejecting: %v", err)
	}

	if _, err := pool.Exec(t.Context(),
		`UPDATE provider_verification_decisions SET reason = 'changed my mind' WHERE provider_id = $1`,
		provider); err == nil {
		t.Error("a decision was updated")
	}
	if _, err := pool.Exec(t.Context(),
		`DELETE FROM provider_verification_decisions WHERE provider_id = $1`, provider); err == nil {
		t.Error("a decision was deleted")
	}
}

// TestTheHistoryKeepsEveryDecisionAndTheRecordShowsTheNewest.
//
// The record carries one state and the history carries the path to it. This is what makes the
// [Verification] read a snapshot rather than a guess: the reason a provider is shown is the reason
// from the decision that produced their *current* state, not the first or the loudest.
func TestTheHistoryKeepsEveryDecisionAndTheRecordShowsTheNewest(t *testing.T) {
	pool := pgtest.DB(t)
	provider := newProvider(t, pool, "pv-history@example.com", "+61400200011")
	admin := newAdmin(t, pool, "pv-history-admin@example.com")

	steps := []struct {
		to     State
		reason string
	}{
		{StateVerified, "First review: everything current."},
		{StateRestricted, "Insurance expires next month; renew it."},
		{StateVerified, "Renewed certificate received."},
	}
	for _, step := range steps {
		if _, err := decide(t, pool, provider, step.to, Actor{Type: ActorAdmin, ID: admin}, step.reason); err != nil {
			t.Fatalf("deciding %s: %v", step.to, err)
		}
	}

	var rows int
	if err := pool.QueryRow(t.Context(),
		`SELECT count(*) FROM provider_verification_decisions WHERE provider_id = $1`, provider).Scan(&rows); err != nil {
		t.Fatalf("counting decisions: %v", err)
	}
	if rows != len(steps) {
		t.Errorf("%d decisions recorded, want %d — the history is not append-only in practice", rows, len(steps))
	}

	found, err := newTestService().VerificationFor(t.Context(), pool, provider)
	if err != nil {
		t.Fatalf("VerificationFor() = %v", err)
	}
	if found.State != StateVerified || found.Reason != steps[len(steps)-1].reason {
		t.Errorf("the record reads %q/%q, want the newest decision", found.State, found.Reason)
	}
}

// TestABadDecisionIsRefusedBeforeTheDatabaseSeesIt.
//
// The refusals a caller can act on, in the error contract's shape. Docs/10 §4.6: a constraint name in
// a 500 explains nothing, so a state this platform has never heard of and a reason that is blank are
// field errors rather than a violation.
func TestABadDecisionIsRefusedBeforeTheDatabaseSeesIt(t *testing.T) {
	pool := pgtest.DB(t)
	provider := newProvider(t, pool, "pv-bad@example.com", "+61400200012")
	admin := newAdmin(t, pool, "pv-bad-admin@example.com")

	cases := []struct {
		name   string
		to     State
		by     Actor
		reason string
		field  string
		errIs  error
	}{
		{"a state that does not exist", State("Approved"), Actor{Type: ActorAdmin, ID: admin}, "fine", "state", nil},
		{"no reason at all", StateVerified, Actor{Type: ActorAdmin, ID: admin}, "   ", "reason", nil},
		{"an administrator with no identity", StateVerified, Actor{Type: ActorAdmin}, "fine", "", ErrActorNotRecorded},
		{"the system claiming an identity", StateVerified, Actor{Type: ActorSystem, ID: admin}, "fine", "", ErrActorNotRecorded},
		{"an actor nobody recognises", StateVerified, Actor{Type: ActorType("provider"), ID: provider}, "fine", "", ErrActorNotRecorded},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, err := decide(t, pool, provider, c.to, c.by, c.reason)
			if err == nil {
				t.Fatal("the decision was accepted")
			}

			if c.errIs != nil {
				if !errors.Is(err, c.errIs) {
					t.Fatalf("Decide() = %v, want %v", err, c.errIs)
				}
				return
			}

			var api *httpx.Error
			if !errors.As(err, &api) {
				t.Fatalf("Decide() = %v, want a validation error naming %s", err, c.field)
			}
			named := false
			for _, detail := range api.Details {
				if detail.Field == c.field {
					named = true
				}
			}
			if !named {
				t.Errorf("no field error names %s; got %+v", c.field, api.Details)
			}
		})
	}

	if got := stateOf(t, pool, provider); got != "Pending" {
		t.Errorf("the state is %q after five refused decisions, want Pending", got)
	}
}

// TestAReasonIsCollapsedAndBounded.
//
// Collapsed for the reason `fleet` collapses a trading name — a reason typed on a phone with a
// stray newline is one sentence — and bounded at the same 500 the CHECK holds, which is Docs/10
// §3.4's pairing in the direction a test can reach.
func TestAReasonIsCollapsedAndBounded(t *testing.T) {
	pool := pgtest.DB(t)
	provider := newProvider(t, pool, "pv-reason@example.com", "+61400200013")
	admin := newAdmin(t, pool, "pv-reason-admin@example.com")

	after, err := decide(t, pool, provider, StateVerified, Actor{Type: ActorAdmin, ID: admin},
		"  Licence  and\n insurance   current. ")
	if err != nil {
		t.Fatalf("Decide() = %v", err)
	}
	if after.Reason != "Licence and insurance current." {
		t.Errorf("the stored reason is %q, want it collapsed", after.Reason)
	}

	long := make([]byte, maxReason+1)
	for i := range long {
		long[i] = 'x'
	}
	if _, err := decide(t, pool, provider, StateRejected, Actor{Type: ActorAdmin, ID: admin}, string(long)); err == nil {
		t.Error("a reason longer than the column accepts was stored")
	}
}

func containsString(haystack []string, needle string) bool {
	for _, s := range haystack {
		if s == needle {
			return true
		}
	}
	return false
}
