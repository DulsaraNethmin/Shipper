// SHIP-160: taking a policy-breaching job off the marketplace.
//
// Docs/04 §6 step 4 lists the outcomes a moderator may select — "no action, warning, content
// removal, job cancellation, restriction, suspension, or escalation" — and this is the first of
// them the platform can perform. Docs/01 §4.6 names it: "remove or unpublish policy-breaching
// jobs".
//
// # Why they share a service, and where the line is
//
// [Moderation] is the queues, [Users] and [JobConsole] are the searches, and this is *acting on what
// they show*. The shapes differ in a way the constructors make visible: the reads take a pool and
// answer, and this opens a transaction, writes two rows, and refuses the whole thing if either
// fails.
//
// Folding it into a read service would widen that service's constructor with a writer's
// dependencies. A service per outcome would be a constructor per line of Docs/04 §6 — restricting
// an account (SHIP-161), a warning, an escalation all belong here as they arrive.
//
// # The rule both actions obey, and the mutation that proves it
//
// **The audit entry commits with the action or neither happens.** [Auditor.Record] takes the
// transaction, so a refusal to write the entry aborts the mutation — which is the opposite of the
// usual instinct about logging and is right for this table specifically: `Docs/09` lists SHIP-149
// and SHIP-150 among the things that must not be cut because audit is impossible to backfill, and
// the entries most worth having are the ones from the moments something was going wrong.
//
// That guarantee had never been *exercised* before this ticket. Wave 9 found that a swallowed
// `Auditor.Record` error passed `internal/admin`, `cmd/api` and `migrations` — all green — because
// every test drives a database where the insert succeeds, **so the error branch was never taken
// once**. [TestAPrivilegedActionIsRefusedWhenItsAuditEntryCannotBeWritten] takes it, against a real
// database, by making the insert fail.
//
// # It needs no migration
//
// The job moves through the one guarded function, which writes `job_status_history` itself. The
// **recorded reason** the *Done when* asks for lives in `audit_log.reason` as well, which is
// append-only and now searchable (SHIP-165).
//
// The blank line below keeps this a file note rather than a second package comment.

package admin

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/DulsaraNethmin/Shipper/services/core/internal/db"
)

// Reason bounds. The action takes free text and writes it into an append-only table.
//
// The floor is what makes the *Done when*'s "recorded reason" mean something: an empty string and a
// single character are both ways of satisfying a required field without recording anything, and this
// is the one field a later reader has no way to reconstruct. The ceiling is what stops the trail
// becoming a document store — an administrator with more to say than this writes an internal note
// (SHIP-162) and references it.
const (
	minReasonLength = 10
	maxReasonLength = 500
)

// checkReason reports why this reason could not be recorded, or nil.
//
// Trimmed before measuring, so that ten spaces is not a reason. Returned as a [validate.Errors] by
// the handler, which is where the field name belongs; this layer answers in domain terms because it
// is also reachable from a future task that has no request behind it.
func checkReason(reason string) error {
	trimmed := strings.TrimSpace(reason)
	switch {
	case trimmed == "":
		return ErrReasonRequired
	case len([]rune(trimmed)) < minReasonLength:
		return fmt.Errorf("%w: at least %d characters", ErrReasonTooShort, minReasonLength)
	case len([]rune(trimmed)) > maxReasonLength:
		return fmt.Errorf("%w: at most %d characters", ErrReasonTooLong, maxReasonLength)
	}
	return nil
}

// UnpublishCommand is one administrator taking a job off the marketplace (SHIP-160).
type UnpublishCommand struct {
	// JobID is the job to remove. Named in the path, never in the body.
	JobID uuid.UUID

	// ActorID is the administrator doing it, taken from the grant rather than from the request.
	// A body that named its own actor would be an audit trail a client writes.
	ActorID uuid.UUID

	// Reason is why, and it is required. Docs/01 §4.6 and SHIP-160's *Done when* both say so, and
	// so does `ck_job_status_history_admin_reason`.
	Reason string
}

// Enforcement performs the administrative outcomes of Docs/04 §6 (SHIP-160).
type Enforcement struct {
	jobs    Jobs
	auditor *Auditor
	pool    *pgxpool.Pool
}

// NewEnforcement builds the service.
//
// The pool may be nil, which every constructor in this package accepts: the process starts with an
// unreachable database on purpose and the endpoints answer [ErrAdminUnavailable] until it returns.
//
// The lifecycle port and the auditor may not. A nil lifecycle would make unpublishing a job a
// nil-pointer panic at the first request rather than a refusal to start. **A nil auditor is the
// worse of the two and is the reason this check is written out rather than left to
// [Auditor.Record]**: that method does refuse a nil receiver, but it refuses at the moment somebody
// exercises a privileged action, which is exactly when a service must not be discovering its own
// wiring. The composition root decides this once.
func NewEnforcement(jobs Jobs, auditor *Auditor, pool *pgxpool.Pool) (*Enforcement, error) {
	if jobs == nil {
		return nil, errors.New("admin: enforcement needs the job lifecycle; unpublishing a job " +
			"is a guarded transition and this domain has no second opinion about it")
	}
	if auditor == nil {
		return nil, errors.New("admin: enforcement needs the audit writer; every action here is " +
			"privileged, and one that leaves no record cannot be reconstructed afterwards")
	}
	return &Enforcement{jobs: jobs, auditor: auditor, pool: pool}, nil
}

// Unpublish removes a policy-breaching job, with a recorded reason (SHIP-160).
//
// # What "removed" means, and why it is a status rather than a flag
//
// Docs/02 §2 has one row for it — `Open / Negotiating → Cancelled`, "customer cancels before award;
// **admin may intervene**" — so an unpublished job is a cancelled job, moved by an administrator,
// with a reason on the transition. There is no `unpublished` status and no `hidden` column: a second
// way for a job to be off the marketplace would be a second thing every feed, every eligibility
// query and every expiry sweep had to know about, and one of them would eventually not.
//
// It follows that an **awarded** job cannot be unpublished. Docs/02 §2 offers no route from Awarded
// to Cancelled, because a provider has committed; Docs/02 §6.2 makes ending it after that a support
// case. The answer is [ErrJobNotUnpublishable], and the path an administrator takes instead is a
// dispute they then resolve (SHIP-164).
//
// # The customer is notified, and this domain emits nothing to arrange it
//
// SHIP-160's *Done when* ends "and the customer notified". The guarded transition emits
// `job.status_changed` inside this transaction, and `notifications.StatusRules` routes a move to
// `Cancelled` to the job's customer **and** the awarded provider, by email, under the headline "A
// job has been cancelled." So the notification is a consequence of the move rather than a second
// thing to remember.
//
// **No new domain event was added, and that is the finding rather than a shortcut.** An
// `admin.job_unpublished` event would have been a second announcement of one state change — the
// failure `notifications.StatusRules` is written to prevent, where a recipient who learns the
// platform emails twice starts ignoring the first one — and it would have needed a routing rule in
// a package this branch does not own.
//
// What the customer is *not* told is the reason. Docs/04 §6 step 5 asks for "a clear, non-sensitive
// reason where appropriate", and the reason here is an administrator's internal note about a policy
// breach; rendering it into an email is a copy decision belonging to whoever writes that template.
// It is recorded in two places either way.
func (e *Enforcement) Unpublish(ctx context.Context, cmd UnpublishCommand) error {
	if cmd.JobID == uuid.Nil {
		return ErrJobNotFound
	}
	if cmd.ActorID == uuid.Nil {
		return errors.New("admin: unpublishing a job must name the administrator doing it")
	}
	if err := checkReason(cmd.Reason); err != nil {
		return err
	}
	if e.pool == nil {
		return ErrAdminUnavailable
	}

	reason := strings.TrimSpace(cmd.Reason)

	// One transaction: the transition, its history row, its outbox event and the audit entry.
	// Docs/10 §3.2 puts the boundary with whoever owns the invariant, and the invariant here is
	// that a job does not leave the marketplace without a record of who removed it and why.
	return db.InTx(ctx, e.pool, func(ctx context.Context, tx db.Runner) error {
		moved, err := e.jobs.Unpublish(ctx, tx, cmd.JobID, cmd.ActorID, reason)
		if err != nil {
			return fmt.Errorf("admin: unpublishing %s: %w", cmd.JobID, err)
		}

		switch moved {
		case JobMoved:
			// Fall through to the entry. Deliberately the only branch that writes one: an
			// entry for a refused action would record something that did not happen, in a
			// table with no way to take it back.
		case JobNotFound:
			return ErrJobNotFound
		case JobAlreadyRemoved:
			return ErrJobAlreadyUnpublished
		case JobNotRemovable:
			return ErrJobNotUnpublishable
		default:
			return fmt.Errorf("admin: unpublishing %s: %w: %s",
				cmd.JobID, ErrJobMoveUnrecognised, moved)
		}

		// The error is returned, never logged and swallowed. See the file header, and see
		// TestAPrivilegedActionIsRefusedWhenItsAuditEntryCannotBeWritten, which is the test
		// that takes this branch.
		if _, err := e.auditor.Record(ctx, tx, AuditEntry{
			Actor:      AdminActor(cmd.ActorID),
			Action:     AuditActionJobUnpublished,
			TargetType: AuditTargetJob,
			TargetID:   cmd.JobID,
			Reason:     reason,
		}); err != nil {
			return err
		}
		return nil
	})
}
