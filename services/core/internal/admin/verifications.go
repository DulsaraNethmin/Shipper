// SHIP-153 and SHIP-154: the verification review queue, and the decision taken from it.
//
// Docs/04 §5's **first** moderation queue — "new or changed provider verification submissions" — and
// Docs/04 §6's process ending in step 6, "record the decision, actor, timestamp, reason, and evidence
// reference". Docs/01 §4.6 names both halves: "review provider verification status" and the outcome
// an administrator selects.
//
// # These two tickets are the shortest path back to a working marketplace
//
// SHIP-81a backfilled every existing provider `Pending`, deliberately — nobody had reviewed anyone's
// documents, and Docs/04 §1 requires that table be an evidence trail rather than a convenience. The
// consequence was that **no provider on the platform was eligible to bid and nothing could change
// that through any API**: `profiles.Service.Decide` was exported and unrouted, because deciding
// somebody's standing is an administrator's act on the administrator credential. This file is where
// that ends.
//
// # Why a service of its own rather than a method on [Enforcement]
//
// [Enforcement] is Docs/04 §6 step 4's outcomes *against a job or an account* — the two actions that
// take something away — and it takes the job lifecycle port to do it. This takes a different port and
// answers a different question: whether somebody may trade at all, which Docs/04 §1 calls "an
// eligibility decision, not a guarantee of delivery quality". Folding it in would widen
// [NewEnforcement] with a collaborator two thirds of its methods do not use.
//
// **It carries both the queue and the decision**, unlike the split between [Moderation] and
// [Enforcement], and the reason is that the queue and the decision are one screen: a reviewer reads a
// row and acts on it in the same sitting, and the alternative is two services whose constructors
// take the same port.
//
// # The audit entry is written in the transaction that takes the decision
//
// This is the rule enforcement.go states and it is sharper here. `provider_verification_decisions`
// is the **provider's** evidence trail (Docs/04 §1) and `audit_log` is the **administrator's**
// accountability record (Docs/04 §9) — two tables, two readers, two questions. A decision that
// committed without its entry would be a state change nothing holds anybody to account for, in a
// table nothing can correct afterwards; the same reason SHIP-160 writes one reason into two tables.
//
// [Auditor.Record] takes the transaction, so a refusal to write the entry aborts the decision.
// [TestAPrivilegedActionIsRefusedWhenItsAuditEntryCannotBeWritten] takes that branch against a real
// database, and [TestAVerificationDecisionAndItsAuditEntryCommitTogether] takes it for this action
// specifically — because a guarantee that is never exercised is a guarantee nobody has checked.
//
// The blank line below keeps this a file note rather than a second package comment.

package admin

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/DulsaraNethmin/Shipper/services/core/internal/db"
)

// VerificationCommand is one administrator deciding a provider's verification standing.
//
// The handler's shape rather than the port's: it carries what a request supplies plus the actor the
// grant supplies, and [Verifications.Decide] validates it before anything reaches a transaction.
type VerificationCommand struct {
	// ProviderID is the provider. Named in the path.
	ProviderID uuid.UUID

	// ActorID is the administrator, taken from the grant.
	ActorID uuid.UUID

	// State is the outcome — one of Docs/04 §4's five.
	State string

	// Reason is why, and it is required for every outcome including a reinstatement to
	// `Verified`. A trail that records a rejection and not its reversal is the one that makes a
	// person look permanently unsuitable.
	Reason string
}

// Verifications serves Docs/04 §5's first queue and Docs/04 §6's decision (SHIP-153, SHIP-154).
type Verifications struct {
	records ProviderVerifications
	auditor *Auditor

	// states is Docs/04 §4's five outcomes, supplied by cmd/api from `profiles.States`.
	//
	// **Not a constant in this package**, which would be a hand-written copy of a list that is
	// already held to `ck_provider_verifications_state` by a test in both directions — Docs/10
	// §3.4's failure, where two lists agree by comment until somebody adds to one.
	// [JobConsole.Statuses] is the same arrangement for the job statuses and the same reasoning.
	states []string

	pool *pgxpool.Pool
}

// NewVerifications builds the service.
//
// The pool may be nil, which every constructor in this package accepts: the process starts with an
// unreachable database on purpose, and the endpoints answer [ErrAdminUnavailable] until it returns.
//
// The port, the auditor and the state list may not.
//
//   - A nil port would make the queue answer "nobody is waiting" to every request, which is the
//     worst available failure for a review queue: indistinguishable from a quiet week, and silent
//     for exactly as long as nobody checks. [NewModeration] refuses one for the same reason.
//   - A nil auditor is refused here rather than left to [Auditor.Record]'s own check, on
//     [NewEnforcement]'s argument: that method does refuse a nil receiver, but it refuses at the
//     moment somebody exercises a privileged action, which is precisely when a service must not be
//     discovering its own wiring.
//   - An empty state list would refuse **every** decision as unrecognised, which reads exactly like
//     a broken console rather than a mis-wire. Refused at startup, where the cause is visible.
func NewVerifications(
	records ProviderVerifications,
	states []string,
	auditor *Auditor,
	pool *pgxpool.Pool,
) (*Verifications, error) {

	if records == nil {
		return nil, errors.New("admin: the verification console needs a source of provider " +
			"verification records; without one the queue reports nobody waiting, which reads " +
			"exactly like a quiet week")
	}
	if auditor == nil {
		return nil, errors.New("admin: the verification console needs the audit writer; deciding " +
			"who may trade is a privileged action, and one that leaves no record cannot be " +
			"reconstructed afterwards")
	}
	if len(states) == 0 {
		return nil, errors.New("admin: the verification console needs Docs/04 §4's outcomes; with " +
			"none, every decision is refused as unrecognised")
	}

	held := make([]string, len(states))
	copy(held, states)

	return &Verifications{records: records, auditor: auditor, states: held, pool: pool}, nil
}

// States is Docs/04 §4's outcomes as this service was told them, sorted, as a fresh slice.
//
// A copy rather than the stored slice, for [Role.Permissions]' reason: a caller that appended to
// what it was handed would be widening the accepted set for the whole process.
//
// It exists so the handler can name the choices in a validation message without holding a second
// copy of the list — the same seam [JobConsole.Statuses] opened.
func (v *Verifications) States() []string {
	out := make([]string, len(v.states))
	copy(out, v.states)
	slices.Sort(out)
	return out
}

// KnowsState reports whether state is one of the outcomes cmd/api supplied.
//
// Exported for [JobConsole.KnowsStatus]'s reason: the handler validates a query parameter and a
// request body against it, and the alternative is a copy of Docs/04 §4's five outcomes in the
// transport layer.
func (v *Verifications) KnowsState(state string) bool { return slices.Contains(v.states, state) }

// AwaitingReview returns one page of the queue, oldest first (SHIP-153).
//
// It is a read and it opens no transaction: nothing here writes and a single statement is already
// consistent with itself. Docs/10 §3.2 puts a transaction with whoever owns an invariant, and a queue
// owns none.
//
// An unrecognised state is refused rather than ignored, on the reasoning [Users.Search] records for
// its standing filter — and it is sharpest here. An ignored filter answers an empty page, an empty
// page is what "nobody is waiting" looks like, and a review queue that quietly reads empty is one
// nobody opens again.
func (v *Verifications) AwaitingReview(ctx context.Context, q VerificationQuery) ([]VerificationEntry, error) {
	q.State = strings.TrimSpace(q.State)
	if !v.KnowsState(q.State) {
		return nil, fmt.Errorf("%w: %q", ErrVerificationStateUnrecognised, q.State)
	}
	if v.pool == nil {
		return nil, ErrAdminUnavailable
	}

	entries, err := v.records.VerificationsAwaitingReview(ctx, v.pool, q)
	if err != nil {
		return nil, fmt.Errorf("admin: reading the %s verification queue: %w", q.State, err)
	}
	return entries, nil
}

// Decide records an outcome against a provider, with a reason (SHIP-154).
//
// # One transaction: the decision, its evidence row, the state change and the audit entry
//
// The port runs `profiles`' guarded transition, which writes the decision, names it to a trigger
// through a transaction-local setting and moves the state. This method adds the `audit_log` entry to
// the same transaction, so all four commit together or none of them does.
//
// **That is not a convenience and the failure it prevents is specific.** Docs/04 §1 requires every
// review and decision be recorded; a state change that committed while its entry did not would be a
// provider whose eligibility moved with nobody accountable for moving it, in the one table this
// platform promises never to rewrite. `Docs/09` lists audit among the things that must not be cut,
// because it is impossible to backfill.
//
// # A move to the outcome already held is refused rather than absorbed
//
// `profiles` refuses it and this reports it as [ErrVerificationUnchanged], which is the same
// position [Enforcement.SetStanding] takes about a standing an account already holds. A decision is
// an *act by a person*, Docs/04 §6.6 requires it be recorded with a reason, and a second one with no
// change to show for it would be a row asserting a review took place. On a queue two moderators are
// reading, it is also the ordinary answer to "somebody got there first".
//
// # What this does not do: notify the provider
//
// Docs/04 §6 step 5 asks for the affected user to be told "with a clear, non-sensitive reason where
// appropriate", and `GET /v1/provider/verification` already carries the state and the reason to the
// person it concerns. A push or an email is a `notifications` rule keyed on an event this domain does
// not emit, and adding one here would be a routing rule in a package this change does not own. It is
// named in `Docs/11` §4 rather than half-built.
func (v *Verifications) Decide(ctx context.Context, cmd VerificationCommand) (VerificationChange, error) {
	if cmd.ProviderID == uuid.Nil {
		return VerificationChange{}, ErrVerificationNotFound
	}
	if cmd.ActorID == uuid.Nil {
		return VerificationChange{}, errors.New(
			"admin: a verification decision must name the administrator taking it")
	}

	state := strings.TrimSpace(cmd.State)
	if !v.KnowsState(state) {
		return VerificationChange{}, fmt.Errorf("%w: %q", ErrVerificationStateUnrecognised, state)
	}
	if err := checkReason(cmd.Reason); err != nil {
		return VerificationChange{}, err
	}
	if v.pool == nil {
		return VerificationChange{}, ErrAdminUnavailable
	}

	reason := strings.TrimSpace(cmd.Reason)

	var change VerificationChange
	err := db.InTx(ctx, v.pool, func(ctx context.Context, tx db.Runner) error {
		moved, decided, err := v.records.DecideVerification(ctx, tx, VerificationDecision{
			ProviderID: cmd.ProviderID,
			To:         state,
			ActorID:    cmd.ActorID,
			Reason:     reason,
		})
		if err != nil {
			return fmt.Errorf("admin: deciding the verification of %s: %w", cmd.ProviderID, err)
		}

		switch moved {
		case VerificationDecided:
			// Fall through to the entry. Deliberately the only branch that writes one: an
			// entry for a refused decision would record a review that did not happen, in a
			// table with no way to take it back.
		case VerificationProviderNotFound:
			return ErrVerificationNotFound
		case VerificationAlreadyInState:
			return fmt.Errorf("%w: already %s", ErrVerificationUnchanged, state)
		default:
			return fmt.Errorf("admin: deciding the verification of %s: %w: %s",
				cmd.ProviderID, ErrVerificationMoveUnrecognised, moved)
		}

		// The error is returned, never logged and swallowed. See the file header.
		if _, err := v.auditor.Record(ctx, tx, AuditEntry{
			Actor:  AdminActor(cmd.ActorID),
			Action: AuditActionVerificationDecided,

			// The **provider**, as a `users` row, rather than the verification record.
			// `audit_log.target_id` has no foreign key so it could hold either, and a
			// support query asking "everything that happened to this account" should get
			// the verification decisions back beside the standing changes and the notes.
			// AuditActionNoteAdded settled that reading and this follows it.
			TargetType: AuditTargetUser,
			TargetID:   cmd.ProviderID,
			Reason:     reason,

			// Both ends, for [AuditActionUserStandingChanged]'s reason: "Restricted" alone
			// does not say what changed, and an append-only trail cannot be joined to the
			// record's own history afterwards.
			Metadata: map[string]any{
				"from": decided.From,
				"to":   decided.To,
			},
		}); err != nil {
			return err
		}

		change = decided
		return nil
	})
	if err != nil {
		return VerificationChange{}, err
	}
	return change, nil
}
