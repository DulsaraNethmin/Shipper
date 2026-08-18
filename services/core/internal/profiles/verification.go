package profiles

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/DulsaraNethmin/Shipper/services/core/internal/clock"
	"github.com/DulsaraNethmin/Shipper/services/core/internal/db"
	"github.com/DulsaraNethmin/Shipper/services/core/internal/validate"
)

// The provider verification record (SHIP-81a).
//
// Docs/04 §4 gives verification five outcomes and until this ticket they were in no table. This file
// is the domain half of `000200`; the schema half carries the argument for where the guard lives.
//
// # This domain owns the record and no other domain names it
//
// `internal/fleet`'s eligibility filter reads `provider_verifications` in its WHERE clause, exactly
// as it already reads `users` and `jobs` — three tables owned by three other domains, in one SQL
// predicate, with no Go import in either direction. That is the seam, and it was settled before this
// file was written rather than during it.
//
// **A Go port was considered and is forbidden by this ticket's own last clause.** Declaring
// `VerificationState(ctx, providerID)` in `fleet/ports.go` and calling it from Go means either an
// N+1 against a paginated feed or a fetch-then-filter, and either way it produces *a second
// eligibility answer* — the thing `eligibility.go` says in as many words must not happen: "It must
// not add a second eligibility check elsewhere, or there will be two answers to who may bid." One
// WHERE clause is the whole point, so nothing in this package is imported by `fleet` and nothing in
// `fleet` is imported here.
//
// # What this package does *not* decide
//
// Whether a state may bid. That reading belongs to the predicate, is written once, in
// `internal/fleet/eligibility.go`, and is deliberately not duplicated here as a `CanBid()` method —
// a helper on [State] would be the second answer arriving by the back door, and it would be the copy
// that drifted, because it is the one no test of the feed exercises.

// State is one of Docs/04 §4's five verification outcomes.
//
// The stored form is the wire form and both are the document's own spelling. There is no separate
// wire vocabulary here, unlike the job and bid statuses (SHIP-56a's generator): those have twelve and
// seven values that clients render as chips, and this has five that a provider sees once. When
// SHIP-153 puts them in front of an administrator the generator is where they should move, and
// `contracts/statuses.yaml` is where a fourth vocabulary would go.
type State string

// The five outcomes, in the order Docs/04 §4 lists them, which is also roughly the order a provider
// meets them.
const (
	// StatePending is where every provider starts: "cannot bid; information is incomplete or
	// awaiting review".
	//
	// **It is a state, not the absence of one.** `000200` gives every provider a row at
	// registration, and the reason is SHIP-153: its *Done when* is "pending provider verifications
	// listed oldest first", and a queue over a table holding only *decided* providers lists nobody
	// who is waiting.
	StatePending State = "Pending"

	// StateVerified is the one state that may bid: "may bid for supported job categories".
	StateVerified State = "Verified"

	// StateRestricted is "limited access pending clarification or document renewal".
	//
	// **It does not bid**, and that is the reading `internal/fleet` already took of the account-level
	// `restricted` standing: "an account nobody has finished clarifying does not bid, and the
	// conservative direction is also the reversible one". Docs/04 §1's first principle — do not allow
	// a provider to bid until baseline checks are complete — settles it in the same direction. What
	// "limited access" then means is everything short of bidding: the provider signs in, reads their
	// own record, sees the reason and re-submits.
	StateRestricted State = "Restricted"

	// StateRejected is "may not bid; reason must be communicated where appropriate".
	StateRejected State = "Rejected"

	// StateSuspended is "access disabled due to policy, safety, or repeated-performance concerns".
	//
	// Distinct from `users.status = 'suspended'`, which takes the account away entirely (SHIP-166's
	// two-person control). This one takes the *eligibility* away and leaves the account, which is
	// what lets a provider still read why.
	StateSuspended State = "Suspended"
)

// States is every outcome, for validation and for the contract.
//
// **It is a second copy of `ck_provider_verifications_state` and is held to it by test**, the
// discipline Docs/10 §3.4 requires of every enumeration living in two places and the same
// arrangement `fleet.ServiceAreaStates` and `fleet.biddableStatuses` are under.
// TestTheFiveStatesAreTheConstraintsFiveStates reads the CHECK out of `pg_constraint` and holds this
// list to it in both directions.
var States = []State{StatePending, StateVerified, StateRestricted, StateRejected, StateSuspended}

// Valid reports whether s is one of the five.
func (s State) Valid() bool {
	for _, known := range States {
		if s == known {
			return true
		}
	}
	return false
}

func (s State) String() string { return string(s) }

// ActorType is who took a decision.
//
// Two values, and there is deliberately no `provider`. A provider submits evidence (SHIP-81b) and
// never decides their own standing; a value that could appear here would be a way for the subject of
// a review to be recorded as its reviewer.
type ActorType string

const (
	// ActorAdmin is an `admin_users` row (SHIP-147). Every review under Docs/04 §3 is one.
	ActorAdmin ActorType = "admin"

	// ActorSystem is the platform acting with nobody behind it.
	//
	// It exists for the same reason `job_status_history` has one: the expiry sweeps act with no
	// account and must still be attributable. SHIP-159's expiry queue is the work that will use it.
	ActorSystem ActorType = "system"
)

// Actor is who decided, in the shape `000200` stores.
//
// A type rather than two parameters, because the pair has a rule — 'system' names nobody and
// everything else must name somebody — and a rule about two values wants one thing to be checked.
type Actor struct {
	Type ActorType

	// ID is the administrator's `admin_users` id, and is the nil UUID for [ActorSystem].
	ID uuid.UUID
}

// valid reports whether this actor satisfies `ck_provider_verification_decisions_actor_id`.
func (a Actor) valid() bool {
	switch a.Type {
	case ActorSystem:
		return a.ID == uuid.Nil
	case ActorAdmin:
		return a.ID != uuid.Nil
	default:
		return false
	}
}

// maxReason bounds a decision's reason, and it is the same 500 as
// `ck_provider_verification_decisions_reason`.
//
// Docs/10 §3.1 puts coherence in the database and policy in the domain; a reason's length is
// coherence on both sides, so the pair agrees at one number and a test holds it there.
const maxReason = 500

// Verification is a provider's verification standing, as they read it.
//
// # It carries the reason and the decision's clock, and both come from the newest decision
//
// The record itself holds only the state (`000200`), for the reason `proofs` holds no actor: a second
// copy of a fact recorded elsewhere is a fact that can disagree with itself. So this is assembled by
// a join, and `DecidedAt` is empty for a provider nobody has decided anything about — which is every
// provider the day they register.
//
// **There is no administrator's identity here.** Docs/04 §4 requires a *reason* be communicated to a
// rejected provider and says nothing about a name, and the name of the person who rejected somebody
// is exactly the disclosure that turns a moderation decision into a personal one.
type Verification struct {
	ProviderID uuid.UUID

	State State

	// Reason is why the provider is in this state, from the decision that put them there. Empty for
	// a provider still in the state their record was created at.
	Reason string

	// DecidedAt is when that decision was taken, and is the zero time for the same providers.
	DecidedAt time.Time

	// SubmittedAt is when the provider's record was created, which is what SHIP-153 orders its queue
	// by. Carried because "how long have I been waiting" is the question a Pending screen answers.
	SubmittedAt time.Time
}

// Decided reports whether anything has been decided about this provider yet.
func (v Verification) Decided() bool { return !v.DecidedAt.IsZero() }

// Decision is what one decision did: where the provider was, and where they now are.
//
// # Why [Service.Decide] answers with both ends rather than with the record alone
//
// SHIP-154 is the first caller outside this package's own tests, and it writes an `audit_log` entry
// beside the decision. `user.standing_changed` established the shape that entry takes — `from` and
// `to` in the metadata, because "Restricted" alone does not say what changed and an append-only
// trail cannot be joined to a row's history afterwards.
//
// **The alternative was for the caller to read the state first, and it is wrong rather than merely
// wordier.** [Service.Decide] takes the row's lock and reads the state under it; a read the caller
// made before calling would be outside that lock, so two administrators deciding at once would both
// record a move from the state neither of them was moving from. The from-state is a fact this
// method already holds at the only instant it is true, so it hands it back rather than inviting a
// second read of it.
//
// `provider_verification_decisions` records both ends too, and that is not a duplication to remove:
// that table is the *provider's* evidence trail (Docs/04 §1) and `audit_log` is the
// *administrator's* accountability record (Docs/04 §9). SHIP-160 wrote the same reason into two
// tables for exactly this reason, and recorded why.
type Decision struct {
	// From is the state the provider held when the decision was taken, read under the row lock.
	From State

	// Verification is the record as it now stands, with the reason and clock of this decision.
	Verification Verification
}

// Service holds this domain's rules.
//
// It owns no connection, for the reason `fleet.Service` does not: the transaction belongs to whoever
// owns the invariant being protected (Docs/10 §3.2).
type Service struct {
	clock clock.Clock
	store postgresStore
}

// NewService builds the domain service.
//
// It panics without a clock, in the same spirit as `fleet.NewService` and `jobs.NewService`: this is
// called once from the composition root and a missing collaborator is a programming mistake rather
// than a runtime condition.
func NewService(c clock.Clock) *Service {
	if c == nil {
		panic("profiles: NewService needs a clock (Docs/10 §6.3)")
	}
	return &Service{clock: c}
}

// VerificationFor is a provider's own verification standing (SHIP-81a).
//
// # A provider reads their own state and no other provider's, and there is no parameter for whose
//
// The identifier comes from the token, so there is nothing in the request to widen and no filter to
// forget. That is the same arrangement `fleet.Service.Vehicles` takes and it is stronger than a check
// would be: a stranger's record is not refused here, it is never selected.
//
// A caller who is not a provider is [ErrNotProvider] rather than an empty record, which is SHIP-78a's
// reading applied to a surface built after it: an endpoint that answers a customer plausibly has
// relied on the client to keep them away.
//
// r is a reader rather than a transaction. One statement, no write, nothing to keep consistent with
// anything else.
func (s *Service) VerificationFor(ctx context.Context, r db.Runner, providerID uuid.UUID) (Verification, error) {
	if err := mustBeProvider(ctx, s.store, r, providerID); err != nil {
		return Verification{}, err
	}
	return s.store.verification(ctx, r, providerID)
}

// mustBeProvider refuses a caller who is not a provider account.
//
// The same helper `fleet.Service.mustBeProvider` is, written here rather than shared because domains
// do not import each other — and the duplication is two statements against one table both are
// already permitted to read. It reads `users.role` rather than the token's claim, which is the
// distinction SHIP-78a settled: the claim is evidence about the token and the column is the fact.
//
// **A package-level function rather than a method, since SHIP-81b.** [Documents] is a second type in
// this package that has to ask the same question of the same table, and a method on [Service] would
// have made "is this caller a provider" reachable only by holding a service that can also move a
// verification state. One refusal, one implementation, and neither type has to own the other.
//
// The nil identifier cannot match a row, so it is the same refusal as a customer's. A request with no
// subject cannot reach a handler in any case — `RequireUser` is what puts one in the context — so
// this is the guard against a caller inside the service passing the zero value.
func mustBeProvider(ctx context.Context, store postgresStore, r db.Runner, providerID uuid.UUID) error {
	provider, err := store.isProvider(ctx, r, providerID)
	if err != nil {
		return err
	}
	if !provider {
		return fmt.Errorf("profiles: %s: %w", providerID, ErrNotProvider)
	}
	return nil
}

// Decide moves a provider's verification state, recording who decided and why (SHIP-81a).
//
// # This is the one guarded transition, and it is one line of Go because the guard is in the database
//
// `provider_verification_decide()` writes the decision, names it to the trigger through a
// transaction-local setting, and moves the state — so there is exactly one implementation of a
// transition and every caller is the same caller: this method, SHIP-154's reviewer when it arrives,
// and `make verify`'s own fixtures. `000200`'s header argues why that differs from the jobs
// precedent, where the protocol lives in Go and anything that is not Go has to reproduce it by hand.
//
// What is *here* rather than in SQL is the part a client can get wrong and must be told about in the
// error contract's shape: a state this platform has never heard of, a reason that is blank or a
// novel, an actor claiming to be an administrator with no identity. A constraint name is a worse
// explanation than a field error (Docs/10 §4.6), and the constraints are still there underneath as
// the layer that survives a rewrite of this one.
//
// # A move to the state the provider is already in is refused rather than absorbed
//
// `fleet.Service.Deactivate` absorbs a repeat, because the caller asked for an outcome that already
// holds. This is the opposite case: a decision is an *act by a person*, Docs/04 §6.6 requires it be
// recorded with a reason, and a second decision that changed nothing would be a row asserting a
// review took place with no change to show for it. `ck_provider_verification_decisions_moves` is the
// second layer, and this refusal exists one statement earlier so that an administrator is told what
// happened rather than handed a constraint name (`000803`'s arrangement).
//
// r must be a transaction. The decision row, the setting and the update are one act, and
// `set_config(…, true)` outside a transaction lasts only for the statement that set it — so a caller
// holding a pool is refused by the database's own trigger, and refused here first with something
// legible.
func (s *Service) Decide(
	ctx context.Context,
	r db.Runner,
	providerID uuid.UUID,
	to State,
	by Actor,
	reason string,
) (Decision, error) {

	if _, inTx := r.(pgx.Tx); !inTx {
		return Decision{}, fmt.Errorf("profiles: deciding %s: %w", providerID, ErrNotInTransaction)
	}
	if providerID == uuid.Nil {
		return Decision{}, fmt.Errorf("profiles: a decision names no provider: %w", ErrNoSuchProvider)
	}

	tidy := strings.Join(strings.Fields(reason), " ")

	var problems validate.Errors
	if !to.Valid() {
		// The five are not listed in the message, for the reason `fleet`'s vehicle type does not
		// list the eleven: the contract publishes them, and a message enumerating them is another
		// copy to keep in step (Docs/10 §3.4).
		problems.Add("state", validate.CodeNotAllowed, "That is not a verification outcome.")
	}
	if problems.Required("reason", tidy) {
		problems.Length("reason", tidy, 1, maxReason)
	}
	if !by.valid() {
		// Not a field error: no client supplies the actor. It is the composition root's, taken from
		// the administrator's own credential, so a bad one is a wiring fault rather than a request
		// somebody can correct.
		return Decision{}, fmt.Errorf("profiles: %q with id %s: %w", by.Type, by.ID, ErrActorNotRecorded)
	}
	if err := problems.Err(); err != nil {
		return Decision{}, err
	}

	current, err := s.store.lockVerification(ctx, r, providerID)
	if err != nil {
		return Decision{}, err
	}
	if current == to {
		return Decision{}, fmt.Errorf("profiles: %s is already %s: %w", providerID, to, ErrAlreadyInState)
	}

	if err := s.store.decide(ctx, r, providerID, to, by, tidy); err != nil {
		return Decision{}, err
	}

	after, err := s.store.verification(ctx, r, providerID)
	if err != nil {
		return Decision{}, err
	}
	return Decision{From: current, Verification: after}, nil
}
