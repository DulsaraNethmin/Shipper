// SHIP-150: the write helper SHIP-149 named and did not build, and the actions it may record.
//
// # What was here before this ticket, and what was not
//
// `audit_log` has existed since `000003` — the table, its three indexes, and the two triggers that
// refuse `UPDATE` and `DELETE` from any connection. `Docs/11` §4 recorded the gap accurately: the
// table and its tests were done and "the Go write helper its title names" was not. Measured on the
// branch this was written from, the **only** `INSERT INTO audit_log` statements in the repository
// were four in `migrations/schema_test.go`, and the three non-test files mentioning the table
// mentioned it in comments. **No Go code wrote an audit entry.** So this ticket builds the helper
// first and applies it second, and the *Done when* — "all admin mutations write an audit entry" —
// is a claim about the second half that the first half had to exist for.
//
// # The entry commits with the thing it describes, or neither happens
//
// [Auditor.Record] takes a [db.Runner] rather than a pool, so it joins the caller's transaction
// (Docs/10 §3.2). That is the whole design. An audit trail written afterwards, best-effort, is one
// that is missing exactly the entries somebody will later want — the ones for actions that were
// interrupted, retried, or that failed halfway. A refusal to write the entry therefore **fails the
// mutation**: `Docs/09` lists SHIP-149 and SHIP-150 among the things that must not be cut because
// audit is "impossible to backfill", and a privileged action that succeeded with no record of it is
// the backfill nobody can perform.
//
// # created_at comes from the injected clock, and that is a decision rather than a habit
//
// `audit_log.created_at` is `DEFAULT now()` — the *database's* clock. `Docs/11` §9's rule is *one
// row, one clock*: if the domain injects a clock, the column takes its value from the domain. This
// domain injects one, so [Auditor.Record] supplies the instant and the column default is left as the
// safety net for a writer that never comes through Go — the operator's `psql` prompt, and
// `scripts/verify/90-admin.sh`, which both insert directly.
//
// The alternative was to take the default and never assert on the value, and it was rejected for a
// specific reason: an audit trail is read by ordering it, and an entry written by this service must
// be orderable against the session row, the dispute row and the status-history row it describes —
// all of which take the injected clock already. Two clocks across those rows would make a support
// timeline that disagrees with itself by however far the two have drifted.
//
// This is the same defect this package shipped and fixed one wave ago, one table along:
// `admin_sessions.created_at` was left to `DEFAULT now()` while its expiries came from the injected
// clock, and the suite passed for one idle window and then failed permanently. See
// [postgresStore.insertSession] and TestASessionIsInternallyConsistentWhateverTheWallClockSays.
//
// # Why the writer is in a domain, and the trigger for moving it
//
// `audit_log` is in the **shared** migration block, and `migrations/blocks.go` names it among "tables
// every domain reads". So an argument exists for `internal/audit` as infrastructure, next to
// `pagination` and `ratelimit`: a second domain that needed to append could then do so without
// importing `admin`, which the boundary lint refuses.
//
// It is here instead, for two reasons and one of them is not architectural. **The architectural
// one**: everything this ticket audits is an administrative action, the vocabulary below is
// administrative, and infrastructure that had to be told what an administrator is would not be
// infrastructure. **The practical one**: a new package under `internal/` fails the boundary lint
// until it is classified in `internal/boundaries/boundaries.go`, which is a shared file this branch
// may not edit (Docs/10 §9.2) — so promoting it is a change that has to be asked for rather than
// taken.
//
// **The revisit trigger is named rather than left to judgement: the first ticket outside `admin`
// that needs to append an entry.** SHIP-119's auto-complete and SHIP-68's expiry sweep are the
// likely candidates — both act as [AuditActorSystem], which is why that constant exists with nothing
// writing it. At that point this file moves to `internal/audit`, `admin` keeps its action catalogue,
// and the change is confined to two imports and one registration.
//
// The blank line below keeps this a file note rather than a second package comment.

package admin

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"

	"github.com/google/uuid"

	"github.com/DulsaraNethmin/Shipper/services/core/internal/clock"
	"github.com/DulsaraNethmin/Shipper/services/core/internal/db"
)

// AuditActorType is the kind of thing that acted, and it is `ck_audit_log_actor_type`s three values.
//
// Paired with the constraint by TestAuditActorTypeConstraintMatchesTheGoConstants in `migrations`,
// per Docs/10 §3.4: a value the database accepts and Go has no constant for is an entry nothing can
// write, and one Go knows and the database refuses is a write that fails at run time.
type AuditActorType string

const (
	// AuditActorAdmin is an administrator acting through the console. Every entry this ticket
	// writes is one of these.
	AuditActorAdmin AuditActorType = "admin"

	// AuditActorUser is a customer or a provider acting on their own record.
	//
	// **Nothing writes one today, and that is deliberate rather than unfinished.** See the note
	// on [AuditActions] about what is out of this ticket and why.
	AuditActorUser AuditActorType = "user"

	// AuditActorSystem is the platform acting with no person behind it — the expiry sweep, the
	// auto-complete timer. It is the one actor kind with no account, which is why
	// `audit_log.actor_id` is nullable and why `ck_audit_log_actor_id` requires it to be NULL for
	// exactly this value.
	AuditActorSystem AuditActorType = "system"
)

// AuditActorTypes is every actor kind.
var AuditActorTypes = []AuditActorType{AuditActorAdmin, AuditActorUser, AuditActorSystem}

// Valid reports whether t is one of [AuditActorTypes].
func (t AuditActorType) Valid() bool { return slices.Contains(AuditActorTypes, t) }

// String is the stored form.
func (t AuditActorType) String() string { return string(t) }

// AuditAction is what was done, as a stable identifier rather than a sentence.
//
// `000003` asks for exactly this: "a stable identifier rather than a sentence … free text would make
// the log unsearchable by action, which is how support will use it". A closed Go type is what makes
// that true of the writers as well as of the readers — a typo in a free-form string is an entry that
// no search will ever return, and nothing about the row itself looks wrong.
type AuditAction string

// The catalogue. Every action this service records, and closed.
//
// # Why the list is this short, and what is deliberately not on it
//
// The *Done when* is "all admin mutations write an audit entry". Measured against
// `cmd/api/routes_golden.txt` on the branch this was written from, the administrator's served
// surface is five routes, and **three of them change state**: sign-in, sign-out, and creating an
// administrator. Of the six *mutating* permissions in permissions.go, only [PermissionAdminsManage]
// has an endpoint — SHIP-154, SHIP-160, SHIP-161, SHIP-162 and SHIP-164 are the other five and none
// is built. So this list is short because the console is, and it grows one line per ticket that adds
// a privileged action.
//
// **`POST /v1/jobs/{id}/disputes` is not audited, and that is a decision.** Docs/01 §5.1 asks for
// "audit logs for privileged actions", and intake is a customer or a provider reporting a problem
// with their own delivery — not a privileged action by any reading. It is already recorded twice
// over, in `disputes` with the complainant and in `job_status_history` with the actor, the reason
// and both clocks (SHIP-57a). A third copy in a table whose purpose is holding administrators to
// account would dilute exactly the search Docs/04 §9 exists to support. SHIP-164's *administrative*
// half — an administrator recording an outcome and unfreezing the job — is a privileged action and
// gets an entry, from that ticket.
//
// **A failed sign-in is not audited either, and that is the more interesting of the two.** It cannot
// be: `ck_audit_log_actor_id` requires a non-NULL actor for a non-system entry, and a sign-in that
// failed because the address is unknown has no account to name. Recording it as `system` would put
// every address anybody has ever guessed into a table support reads, which is the enumeration
// disclosure credentials.go spends an argon2id derivation to avoid — an oracle in the audit trail
// rather than in the response time, and a longer-lived one. Failed attempts are counted by the rate
// limiter and logged against the request id, which is where they belong.
const (
	// AuditActionAdministratorCreated is a new administrator account (SHIP-147, SHIP-148).
	//
	// **The most privileged action the console has**, because somebody who can create an
	// administrator can create one with any role. The target is the account created; the actor is
	// the [RoleOwner] who created it, and the role granted goes in the metadata — an entry saying
	// an account was created without saying what it may do would be the one field a reader most
	// needs.
	AuditActionAdministratorCreated AuditAction = "administrator.created"

	// AuditActionAdministratorSignedIn is a console session starting.
	//
	// A session is a row, starting one is a state change, and "who was in the console at the time"
	// is the first question asked of any trail that ever matters. Docs/04 §6 step 6 wants the
	// actor and the timestamp of a decision recorded; a decision has a person behind it, and the
	// only record of that person being present is this.
	//
	// The target is the administrator themselves rather than the session: `audit_log.target_id`
	// has no foreign key so it could hold either, and a support query asking "everything about
	// this administrator" wants the sign-ins to come back with the rest.
	AuditActionAdministratorSignedIn AuditAction = "administrator.signed_in"

	// AuditActionAdministratorSignedOut is a console session ending deliberately.
	//
	// **The least obvious of the three, and it is here for that reason.** A trail with sign-ins and
	// no sign-outs makes every session look open until it expired, which is wrong about exactly
	// the accounts that were being careful. A session that lapses on the idle window writes no
	// entry — nothing acts, so there is nothing to attribute — and that asymmetry is honest: this
	// records a person leaving, not a timer firing.
	AuditActionAdministratorSignedOut AuditAction = "administrator.signed_out"

	// AuditActionJobUnpublished is a policy-breaching job taken off the marketplace (SHIP-160).
	//
	// **The first entry in this catalogue whose target is not an administrator**, which is why
	// [AuditTargetJob] exists. The reason is required rather than optional: Docs/01 §4.6 says
	// "remove or unpublish policy-breaching jobs" and SHIP-160's *Done when* says "with a
	// recorded reason", and a removal nobody has to justify is the one an administrator can make
	// carelessly.
	//
	// The same reason is written twice, into two tables, and that is deliberate rather than
	// redundant. `job_status_history` records why the *job* moved, which is what a customer's
	// support conversation reads; `audit_log` records what the *administrator* did, which is what
	// Docs/04 §9's controls read. The two are joined by nothing but the job identifier, and a
	// reader of either should not have to find the other.
	AuditActionJobUnpublished AuditAction = "job.unpublished"
)

// AuditActions is the whole catalogue, in the order the constants declare it.
//
// It is what TestEveryAuditActionConstantIsInTheCatalogue compares against, and — more usefully —
// what TestEveryAdminMutationWritesAnAuditEntry drives: an action declared here with no mutation
// exercising it fails, and a mutation exercising an action that is not here fails. That pairing is
// what makes "all admin mutations" a checked claim rather than a count of the ones somebody
// remembered.
var AuditActions = []AuditAction{
	AuditActionAdministratorCreated,
	AuditActionAdministratorSignedIn,
	AuditActionAdministratorSignedOut,
	AuditActionJobUnpublished,
}

// Valid reports whether a is in the catalogue.
func (a AuditAction) Valid() bool { return slices.Contains(AuditActions, a) }

// String is the stored form, which is what a search matches on.
func (a AuditAction) String() string { return string(a) }

// The target kinds this service names.
//
// Strings rather than a closed type, because `audit_log.target_type` has no CHECK constraint and
// deliberately so: the table outlives its subjects (Docs/05 §3.1) and has to be able to name a kind
// of thing that no longer exists. What keeps them consistent is that they are constants here and
// nothing builds one by concatenation.
const (
	// AuditTargetAdministrator is an `admin_users` row.
	AuditTargetAdministrator = "administrator"

	// AuditTargetJob is a `jobs` row (SHIP-160).
	//
	// Named for the thing rather than for the table, which is the point of the column having no
	// CHECK constraint: `audit_log` outlives its subjects (Docs/05 §3.1), so a target kind has to
	// be able to name something the schema no longer has a table for.
	AuditTargetJob = "job"
)

// AuditActor is who acted.
//
// A struct rather than two arguments, so that the pairing rule `ck_audit_log_actor_id` enforces —
// an account-backed actor names an account, `system` must not — is expressible in one place and
// checked once. See [AuditActor.problem].
type AuditActor struct {
	Type AuditActorType

	// ID is the account that acted. Zero for [AuditActorSystem] and required for the other two.
	ID uuid.UUID
}

// AdminActor is the administrator behind a console action.
func AdminActor(id uuid.UUID) AuditActor { return AuditActor{Type: AuditActorAdmin, ID: id} }

// SystemActor is the platform acting with nobody behind it.
//
// Unused today — no scheduled task in this domain writes an entry yet — and present because the
// pairing rule is easiest to get wrong at the call site that first needs it, and a constructor that
// cannot name an account is the shape that makes it right by construction.
func SystemActor() AuditActor { return AuditActor{Type: AuditActorSystem} }

// problem reports why this actor could not be recorded, or nil.
func (a AuditActor) problem() error {
	if !a.Type.Valid() {
		return fmt.Errorf("%q is not an actor kind; use one of %v", a.Type, AuditActorTypes)
	}
	if a.Type == AuditActorSystem && a.ID != uuid.Nil {
		return errors.New("a system actor has no account, so it must not name one")
	}
	if a.Type != AuditActorSystem && a.ID == uuid.Nil {
		return fmt.Errorf("a %s actor must name the account that acted", a.Type)
	}
	return nil
}

// AuditEntry is one thing that happened, in the terms `audit_log` records it.
//
// There is no `CreatedAt` field and no `ID` field. Both are the [Auditor]'s to supply — the identifier
// so that no caller can choose one, and the instant so that every entry in a transaction shares the
// clock the rest of that transaction used. A caller able to set either could write an entry dated
// before the action it describes, and an append-only table has no way to correct it.
type AuditEntry struct {
	// Actor is who did it.
	Actor AuditActor

	// Action is what they did. Must be in [AuditActions].
	Action AuditAction

	// TargetType and TargetID are what it was done to. Deliberately not a foreign key in the
	// schema — an audit entry outlives its subject (Docs/05 §3.1) — so nothing checks that the
	// row still exists, or ever did.
	TargetType string
	TargetID   uuid.UUID

	// Reason is why, where there is one beyond the action itself.
	//
	// Optional here and required by the actions that need it, which is where the requirement
	// belongs: Docs/01 §3 asks for a reason on a change to a commercial record, and SHIP-160's and
	// SHIP-161's *Done when* both say "with a recorded reason". Creating an administrator has no
	// reason beyond the act, and a mandatory field with nothing to put in it becomes a placeholder
	// string that makes the column useless for the actions that do need it.
	Reason string

	// Metadata is anything else worth keeping — the before and after of a changed field, the
	// evidence reference behind a moderation decision (`000003`).
	//
	// Nil is ordinary and becomes `{}`, which is the column default and never NULL: a reader
	// should be able to index into the object without checking, and jsonb NULL is the one value
	// that makes that a run-time failure rather than an empty answer.
	Metadata map[string]any
}

// problem reports why this entry could not be recorded, or nil.
//
// Every check here mirrors a constraint in `000003`, and that duplication is the point rather than a
// cost. The database refusal is the one that is true of a `psql` prompt; this one names the field in
// a message a developer reads at the call site, before the transaction is aborted and every other
// statement in it rolled back with a message about a check constraint.
func (e AuditEntry) problem() error {
	if err := e.Actor.problem(); err != nil {
		return err
	}
	if !e.Action.Valid() {
		return fmt.Errorf("%q is not an action in the catalogue; add it to AuditActions", e.Action)
	}
	if e.TargetType == "" {
		return errors.New("an entry must say what kind of thing it was done to")
	}
	if e.TargetID == uuid.Nil {
		return errors.New("an entry must name what it was done to")
	}
	return nil
}

// Auditor appends to the immutable record.
//
// **There is no method that reads, updates or removes an entry, and there will not be one.**
// CLAUDE.md's invariant is that audit entries are append-only and ordinary administrators cannot
// delete them; `000003` enforces it with triggers that refuse both from any connection, and
// permissions.go records that no permission authorises it. A reader arrives with SHIP-165, which is
// a query rather than a method on this type. This is the write side and it has one verb.
type Auditor struct {
	clock clock.Clock
	store postgresStore
}

// NewAuditor builds the writer.
//
// The clock is required and may not be defaulted to the system one. An auditor that quietly fell
// back to `time.Now` would be the two-clock defect Docs/11 §9 names, reintroduced through a
// convenience — and it would be invisible, because every entry would still look plausible.
func NewAuditor(clk clock.Clock) (*Auditor, error) {
	if clk == nil {
		return nil, errors.New("admin: the audit writer needs a clock (Docs/10 §6.3)")
	}
	return &Auditor{clock: clk}, nil
}

// Record appends one entry inside the caller's transaction, and returns its identifier.
//
// # A failure here fails the caller, deliberately
//
// The error is returned rather than logged and swallowed, so a privileged action whose entry could
// not be written does not happen at all. That is the opposite of the usual instinct — "do not let
// logging break the feature" — and it is right for this table specifically: SHIP-149's entry in
// `Docs/09`s do-not-cut list is there because audit "is impossible to backfill", and the entries
// most worth having are the ones from the moments something was going wrong.
//
// # The identifier is generated here and is a v7
//
// Time-ordered, so entries written in one transaction sort in the order they were appended even
// where they share a `created_at` to the microsecond. A v4 would leave two entries about the same
// action in an arbitrary order for ever, in a table nothing can rewrite.
func (a *Auditor) Record(ctx context.Context, r db.Runner, e AuditEntry) (uuid.UUID, error) {
	if a == nil {
		// A nil auditor is a wiring mistake rather than a condition, and it must not be the
		// quiet kind: NewCredentials refuses one, so reaching here means something built the
		// service by hand. Answering "recorded, identifier zero" would be a silent hole in the
		// trail exactly where somebody had bypassed the constructor.
		return uuid.Nil, errors.New("admin: no audit writer, so a privileged action cannot be recorded")
	}
	if err := e.problem(); err != nil {
		return uuid.Nil, fmt.Errorf("admin: refusing to record an audit entry: %w", err)
	}

	id, err := uuid.NewV7()
	if err != nil {
		return uuid.Nil, fmt.Errorf("admin: generating an audit entry id: %w", err)
	}

	metadata, err := marshalMetadata(e.Metadata)
	if err != nil {
		return uuid.Nil, err
	}

	if err := a.store.insertAuditEntry(ctx, r, auditRow{
		ID:         id,
		ActorType:  e.Actor.Type,
		ActorID:    e.Actor.ID,
		Action:     e.Action,
		TargetType: e.TargetType,
		TargetID:   e.TargetID,
		Reason:     e.Reason,
		Metadata:   metadata,
		CreatedAt:  a.clock.Now().UTC(),
	}); err != nil {
		return uuid.Nil, err
	}
	return id, nil
}

// marshalMetadata renders the extra facts as the jsonb the column holds.
//
// Nil and empty both become `{}` rather than NULL. `000003` makes the column `NOT NULL DEFAULT
// '{}'::jsonb`, so NULL is not writable anyway; doing it here means a reader never has to ask
// whether an entry from before some particular change has an object or a null.
func marshalMetadata(m map[string]any) (string, error) {
	if len(m) == 0 {
		return "{}", nil
	}

	encoded, err := json.Marshal(m)
	if err != nil {
		// A value that will not marshal is a caller passing something a JSON column cannot
		// hold, which is a defect at the call site rather than a condition to work around.
		return "", fmt.Errorf("admin: encoding audit metadata: %w", err)
	}
	return string(encoded), nil
}
