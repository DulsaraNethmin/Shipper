// SHIP-166: two-person review for permanent suspension.
//
// Docs/04 §9's second required internal control — "two-person review for permanent account
// suspension where practical" — and this file is where "where practical" was decided.
//
// # It is practical, and the reason is that both halves already exist
//
// The clause is a hedge for a platform with one administrator. This one has three roles and a
// permission that two of them hold (`users.restrict`), so a second administrator is not a person
// somebody has to go and hire — and SHIP-147 built the sign-in that tells two of them apart. What
// was missing was somewhere to record a request and a rule that the approver is not the requester.
//
// # Why only suspension, and why reinstatement is not paired with it
//
// `users.status` has three values. `restricted` narrows what an account may *do* — a restricted
// provider may still sign in and read, and may not trade — and it is reversible in one action.
// **`suspended` is the one that takes the account away**: `identity.User.CanSignIn` refuses it at
// sign-in and the session service refuses it at refresh (SHIP-161), so a suspended person cannot
// reach the platform to ask why. That is what Docs/04 §9 means by permanent, and it is the only
// standing in this schema that means it.
//
// **Reinstatement stays a single administrator's action, deliberately.** A control that made undoing
// a mistake as slow as making one would leave somebody locked out while two people found each other,
// and the failure it would prevent — an account wrongly *restored* — is not the failure Docs/04 §9 is
// about.
//
// # The rule lives in two places and that is not duplication
//
// `ck_suspension_reviews_two_people` refuses `approved_by = requested_by` in the database, and
// [Suspensions.Approve] refuses it one statement earlier with a message naming what happened. The
// constraint is what makes this a control: application logic refusing to write is a convention, and
// a convention does not apply to a support query typed at a psql prompt or to the next endpoint
// somebody adds without reading this file. `000005_users_role_is_immutable` takes the same position
// for the same reason.
//
// **The mutation that establishes it is real rather than believed**: removing the Go check leaves the
// constraint, and TestOneAdministratorCannotCompleteATwoPersonReview fails on the refusal rather than
// passing quietly — which is the outcome a two-person rule has to have.
//
// # The suspension is applied by the approval, in one transaction
//
// `users.status` moves when the second administrator agrees, not when the first asks. A request that
// suspended the account immediately and waited for confirmation would be a control that does nothing:
// the account is already gone, and the second signature is paperwork.
//
// So [Suspensions.Approve] opens one transaction and writes four things — the review's approval, the
// account's standing, and an audit entry — and refuses the whole thing if any of them fails.
// SHIP-161's file header records why the entry commits with the action or neither happens, and every
// word of it applies here.
//
// The blank line below keeps this a file note rather than a second package comment.

package admin

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/DulsaraNethmin/Shipper/services/core/internal/db"
)

// SuspensionReviewStatus is where a review has got to.
type SuspensionReviewStatus string

const (
	// ReviewPending is a request waiting for a second administrator.
	ReviewPending SuspensionReviewStatus = "pending"

	// ReviewApproved is a request a second administrator agreed with. The suspension was applied
	// in the same transaction.
	ReviewApproved SuspensionReviewStatus = "approved"

	// ReviewWithdrawn is a request the platform no longer holds open.
	//
	// **There is deliberately no `rejected`.** A second administrator who disagrees says so to
	// the first, and the request is withdrawn by whoever made it; recording a rejection would
	// make this a workflow with two outcomes to route, and would put a disagreement between
	// colleagues into a record of things that were *done*, when nothing was.
	//
	// Nothing writes it yet — withdrawal has no endpoint. The status exists so that the shape a
	// later ticket needs is not a migration, and so that a pending review is not something only
	// an approval can clear.
	ReviewWithdrawn SuspensionReviewStatus = "withdrawn"
)

// SuspensionReviewStatuses is the closed set.
var SuspensionReviewStatuses = []SuspensionReviewStatus{
	ReviewPending, ReviewApproved, ReviewWithdrawn,
}

func (s SuspensionReviewStatus) String() string { return string(s) }

// SuspensionReview is one proposal to suspend an account permanently.
type SuspensionReview struct {
	ID uuid.UUID

	// UserID is the account proposed for suspension.
	UserID uuid.UUID

	// RequestedBy is the administrator who asked, taken from the grant rather than from the
	// request body — a body that named its own actor would be a control a client writes.
	RequestedBy uuid.UUID

	// Reason is why, required at the *request* rather than at the approval: the second
	// administrator is being asked to agree with a stated case, and a request carrying none would
	// make the control a formality.
	Reason string

	// ApprovedBy is the second administrator, and it is never [RequestedBy] — see the file note.
	// Zero while the review is pending.
	ApprovedBy uuid.UUID

	// ApprovedAt is when they agreed. Zero while the review is pending.
	ApprovedAt time.Time

	Status SuspensionReviewStatus

	CreatedAt time.Time
}

// Pending reports whether this review is still waiting for a second administrator.
func (r SuspensionReview) Pending() bool { return r.Status == ReviewPending }

// SuspensionRequest is one administrator proposing a permanent suspension.
type SuspensionRequest struct {
	// UserID is the account. Named in the path, never in the body.
	UserID uuid.UUID

	// ActorID is the administrator asking, taken from the grant.
	ActorID uuid.UUID

	// Reason is why, and it is required — Docs/01 §4.6 and SHIP-161's *Done when* both say so of
	// the action, and this is the moment the case is stated.
	Reason string
}

// SuspensionApproval is a second administrator agreeing with a request.
//
// **There is no reason on it, and that is deliberate.** The reason is the requester's case, recorded
// once; a second free-text field would invite two accounts of one decision, and the audit entry that
// records the approval carries the review's reason so that the trail has the why in it either way.
type SuspensionApproval struct {
	// ReviewID is the review being approved. Named in the path.
	ReviewID uuid.UUID

	// ActorID is the approving administrator, taken from the grant. **This is the value the whole
	// control turns on**: it comes from a verified session and never from the request.
	ActorID uuid.UUID
}

// Suspensions performs Docs/04 §9's two-person review (SHIP-166).
//
// A service of its own beside [Enforcement] rather than two more methods on it. [Enforcement] is the
// outcomes one administrator may take alone, and its file header records that "nothing below
// anticipates" this ticket — "there is no pending state and no approval column, because a half-built
// approval is worse than none". This is the whole of it, in one place, and the split is what keeps
// that statement true of that file.
type Suspensions struct {
	auditor *Auditor
	pool    *pgxpool.Pool
	store   postgresStore
}

// NewSuspensions builds the service.
//
// The pool may be nil — the process starts with an unreachable database on purpose. The auditor may
// not, on exactly [NewEnforcement]'s reasoning: a nil auditor refuses at the moment somebody
// exercises a privileged action, which is when a service must not be discovering its own wiring.
func NewSuspensions(auditor *Auditor, pool *pgxpool.Pool) (*Suspensions, error) {
	if auditor == nil {
		return nil, errors.New("admin: the suspension review needs the audit writer; a control " +
			"that leaves no record of who asked and who agreed is not a control")
	}
	return &Suspensions{auditor: auditor, pool: pool}, nil
}

// Request records one administrator's proposal to suspend an account permanently.
//
// **It changes no standing.** The account is untouched until a second administrator agrees, which is
// the difference between this and a control that suspends first and collects a signature afterwards.
//
// One pending review per account, enforced by `uq_suspension_reviews_one_pending` rather than by a
// SELECT — two moderators opening the same account at the same moment is the ordinary case and a
// check-then-insert loses that race. Two pending reviews would also let one administrator approve
// the other's request while their own waited, which satisfies the letter of a two-person rule with
// one person driving both halves.
func (s *Suspensions) Request(ctx context.Context, cmd SuspensionRequest) (SuspensionReview, error) {
	if cmd.UserID == uuid.Nil {
		return SuspensionReview{}, ErrUserNotFound
	}
	if cmd.ActorID == uuid.Nil {
		return SuspensionReview{}, errors.New("admin: a suspension request must name the " +
			"administrator making it")
	}
	if err := checkReason(cmd.Reason); err != nil {
		return SuspensionReview{}, err
	}
	if s.pool == nil {
		return SuspensionReview{}, ErrAdminUnavailable
	}

	reason := strings.TrimSpace(cmd.Reason)

	id, err := uuid.NewV7()
	if err != nil {
		return SuspensionReview{}, fmt.Errorf("admin: generating a review id: %w", err)
	}

	var review SuspensionReview
	err = db.InTx(ctx, s.pool, func(ctx context.Context, tx db.Runner) error {
		// Locked and read before anything is written, so that the standing recorded in the
		// entry is true rather than merely recent — the same lock SHIP-161 takes and for the
		// same reason.
		current, found, err := s.store.lockUserStanding(ctx, tx, cmd.UserID)
		if err != nil {
			return err
		}
		if !found {
			return ErrUserNotFound
		}
		if current == StandingSuspended {
			// Already gone. Asking for a review of a suspension that has happened is the
			// ordinary outcome of two moderators reading the same screen, and the console's
			// right response is to reload.
			return fmt.Errorf("%w: already %s", ErrStandingUnchanged, current)
		}

		review, err = s.store.insertSuspensionReview(ctx, tx, SuspensionReview{
			ID:          id,
			UserID:      cmd.UserID,
			RequestedBy: cmd.ActorID,
			Reason:      reason,
			Status:      ReviewPending,
		})
		if err != nil {
			return err
		}

		// The error is returned, never logged and swallowed. See enforcement.go's file header,
		// and TestAPrivilegedActionIsRefusedWhenItsAuditEntryCannotBeWritten, which takes that
		// branch against a real database.
		if _, err := s.auditor.Record(ctx, tx, AuditEntry{
			Actor:      AdminActor(cmd.ActorID),
			Action:     AuditActionUserSuspensionRequested,
			TargetType: AuditTargetUser,
			TargetID:   cmd.UserID,
			Reason:     reason,
			Metadata: map[string]any{
				"review_id": review.ID.String(),
				"from":      current.String(),
			},
		}); err != nil {
			return err
		}
		return nil
	})
	if err != nil {
		return SuspensionReview{}, err
	}
	return review, nil
}

// Approve is the second administrator agreeing, and it applies the suspension.
//
// # The one check this whole ticket exists for
//
// **The approver may not be the requester.** It is checked here, against the review as read under a
// row lock, and it is checked again by `ck_suspension_reviews_two_people` — which is what makes it a
// control rather than a convention, because a constraint applies to a repair script and a psql
// prompt and this function does not.
//
// The actor comes from the grant, never from the request body. A control whose second signature a
// client could name is a control with one participant.
//
// # Everything commits together or nothing does
//
// The review's approval, the account's standing and the audit entry are one transaction. A partial
// write here is the worst available outcome: a review marked approved beside an account still active
// reads, to the next person, as a suspension that was agreed and then quietly reversed.
func (s *Suspensions) Approve(ctx context.Context, cmd SuspensionApproval) (SuspensionReview, error) {
	if cmd.ReviewID == uuid.Nil {
		return SuspensionReview{}, ErrSuspensionReviewNotFound
	}
	if cmd.ActorID == uuid.Nil {
		return SuspensionReview{}, errors.New("admin: approving a suspension must name the " +
			"administrator doing it")
	}
	if s.pool == nil {
		return SuspensionReview{}, ErrAdminUnavailable
	}

	var approved SuspensionReview
	err := db.InTx(ctx, s.pool, func(ctx context.Context, tx db.Runner) error {
		review, found, err := s.store.lockSuspensionReview(ctx, tx, cmd.ReviewID)
		if err != nil {
			return err
		}
		if !found {
			return ErrSuspensionReviewNotFound
		}
		if !review.Pending() {
			return fmt.Errorf("%w: already %s", ErrSuspensionReviewSettled, review.Status)
		}

		// Docs/04 §9. Two people, or it is not a two-person review.
		if review.RequestedBy == cmd.ActorID {
			return ErrSameAdministrator
		}

		approved, err = s.store.approveSuspensionReview(ctx, tx, review.ID, cmd.ActorID)
		if err != nil {
			return err
		}

		current, found, err := s.store.lockUserStanding(ctx, tx, review.UserID)
		if err != nil {
			return err
		}
		if !found {
			return ErrUserNotFound
		}
		if current == StandingSuspended {
			return fmt.Errorf("%w: already %s", ErrStandingUnchanged, current)
		}
		if err := s.store.setUserStanding(ctx, tx, review.UserID, StandingSuspended); err != nil {
			return err
		}

		// One entry, naming both administrators. The trail's question afterwards is "who
		// suspended this account", and a two-person control answers it with two names or it
		// has not recorded what happened.
		if _, err := s.auditor.Record(ctx, tx, AuditEntry{
			Actor:      AdminActor(cmd.ActorID),
			Action:     AuditActionUserSuspensionApproved,
			TargetType: AuditTargetUser,
			TargetID:   review.UserID,

			// The requester's case, carried rather than restated. The approval has no reason
			// of its own, so this is the only account of why, and a trail that recorded the
			// approval without it would say what happened and not why.
			Reason: review.Reason,

			Metadata: map[string]any{
				"review_id":    review.ID.String(),
				"requested_by": review.RequestedBy.String(),
				"from":         current.String(),
				"to":           StandingSuspended.String(),
			},
		}); err != nil {
			return err
		}
		return nil
	})
	if err != nil {
		return SuspensionReview{}, err
	}
	return approved, nil
}

// Pending returns the reviews waiting for a second administrator, oldest first.
//
// **Without it the control does not work**, which is why a read is in a ticket about a write: a
// second administrator has to be able to find a request they did not make. Oldest first because an
// account waiting on a suspension somebody has already argued for is the entry closest to being
// forgotten.
//
// It is not filtered to reviews the caller may approve. Somebody's own pending request is shown to
// them — they need to know it is outstanding — and the refusal happens where it is enforced.
func (s *Suspensions) Pending(ctx context.Context, limit int) ([]SuspensionReview, error) {
	if s.pool == nil {
		return nil, ErrAdminUnavailable
	}

	reviews, err := s.store.pendingSuspensionReviews(ctx, s.pool, limit)
	if err != nil {
		return nil, fmt.Errorf("admin: reading the pending suspension reviews: %w", err)
	}
	return reviews, nil
}
