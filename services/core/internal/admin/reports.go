// SHIP-155a: raising a report, and the rules around it.
//
// report.go holds the vocabulary and this holds what happens when somebody uses it. The two are
// split the way dispute.go and service.go are, and for the same reason: a reader looking for what a
// reason *is* and a reader looking for who may raise one are looking for different things.
//
// # There is no transaction here, and that is the difference from RaiseDispute
//
// Docs/10 §3.2 puts a transaction with the domain that owns the invariant. [Service.RaiseDispute]
// opens one because raising a dispute is two writes in two domains that have to agree — the dispute
// row and the job's move to 'Disputed'. **A report is one insert**, because it moves no job status
// (report.go's header, `000805`'s), and a single statement is already atomic with itself.
//
// So [Reports] takes no [Jobs] collaborator at all. That is not an omission to be filled in later:
// it is the enforcement. A report cannot move a job's status because this type holds nothing that
// could, and adding the field is the deliberate act that a reviewer would see.
//
// The blank line below keeps this a file note rather than a second package comment.

package admin

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"

	"github.com/DulsaraNethmin/Shipper/services/core/internal/db"
)

// Reports is Docs/04 §5's second moderation queue, from the producing end.
//
// A type of its own rather than a fourth method on [Service], for [Moderation]'s reason: they are
// different concerns with different lifetimes. [Service] is the dispute workflow, which owns a
// transaction spanning two domains; this is one insert and two lookups.
type Reports struct {
	parties  JobParties
	messages JobMessages
	store    postgresStore
}

// NewReports builds the report service.
//
// Both collaborators are required and it panics without either, in the same spirit as
// [NewService]: this is called once from the composition root, a missing collaborator is a
// programming mistake rather than a runtime condition, and the alternative is a service that starts
// and then answers every intake with a nil-pointer panic.
//
// Neither may be defaulted to something harmless, and the two failures are different:
//
//   - a nil [JobParties] is "nobody is checked", so any account could report any job — which is the
//     *Done when* clause this endpoint most exists to satisfy, and the one whose absence is silent;
//   - a nil [JobMessages] is "any message id is on any job", which would let a reporter file against
//     a conversation on somebody else's delivery and put it in a moderator's queue under this job.
func NewReports(parties JobParties, messages JobMessages) *Reports {
	if parties == nil {
		panic("admin: NewReports needs the party lookup; without it any account could report " +
			"any job, which is the one refusal this endpoint exists for (SHIP-155a)")
	}
	if messages == nil {
		panic("admin: NewReports needs the message lookup; without it a report could name a " +
			"message on somebody else's job and be queued under this one (`000805`)")
	}
	return &Reports{parties: parties, messages: messages}
}

// Raise records what a party to a job says is wrong with it, or with one message on it.
//
// It reports whether a report was raised. False means this idempotency key had already raised it and
// nothing was written — the retry path, which is half of what this endpoint exists for.
//
// # The refusals, in this order, and the order is the point
//
//  1. nothing recorded, and nothing said, without an idempotency key: [ErrNoIdempotencyKey];
//  2. a job the caller is not party to, or no job at all: [ErrNotAParty] / [ErrJobNotFound];
//  3. a message that is not on this job, or no such message: [ErrMessageNotOnJob].
//
// **The party check comes before the message check, and that ordering is a disclosure decision
// rather than a tidiness one.** Reversed, a stranger could send any message id against a job they
// have nothing to do with and learn from the answer whether that message exists and which job it is
// on. Checked in this order, a stranger gets the same 404 whatever they send, and only somebody
// already party to the job can distinguish the two — which is exactly the line [JobParties]'
// own documentation draws.
//
// # What a retry gets, and what a second report gets, are different answers
//
//	the same key again    the phone that lost its connection. It gets the report it already
//	                      raised, 200, and nothing is written. `uq_reports_idempotency` is what
//	                      makes that true after Redis has forgotten the response.
//	a different key       somebody reporting the same job again — a second complaint, or the
//	                      other party's. It is **raised**, not refused. Unlike a dispute, a report
//	                      freezes nothing, so there is no reason a job may carry only one; two
//	                      parties reporting one listing for two reasons are two things a moderator
//	                      wants to see (`000805`).
//
// r need not be a transaction, unlike [Service.RaiseDispute]'s — see the file header. It is still a
// [db.Runner] so the caller may join one if it ever has a reason to.
func (rp *Reports) Raise(
	ctx context.Context,
	r db.Runner,
	reporterID, jobID uuid.UUID,
	in ReportIntake,
) (Report, bool, error) {
	intake := in.normalise()
	problems := intake.problems()
	if err := problems.Err(); err != nil {
		return Report{}, false, err
	}

	// Checked in the domain and not left to the handler that read the header. A report written
	// with a NULL key is outside `uq_reports_idempotency` — the index is partial — so this is the
	// one input whose absence would silently remove a guarantee rather than failing loudly.
	if intake.Key == "" {
		return Report{}, false, fmt.Errorf("admin: reporting %s: %w", jobID, ErrNoIdempotencyKey)
	}

	party, isParty, err := rp.parties.PartyOn(ctx, r, jobID, reporterID)
	if err != nil {
		return Report{}, false, err
	}
	if !isParty {
		// One answer for "no such job" and "not your job", deliberately — see [ErrNotAParty].
		// The sentinel distinguishes them so a test can tell a refusal from a disappearance;
		// the wire does not.
		return Report{}, false, fmt.Errorf("admin: %s is not party to %s: %w",
			reporterID, jobID, ErrNotAParty)
	}
	if !party.Valid() {
		// The port answered with something that is not one of the two. A wiring failure rather
		// than a request failure, and reported rather than written to a column
		// `ck_reports_reporter_party` would refuse anyway.
		return Report{}, false, fmt.Errorf("admin: %q is not a party this domain recognises: %w",
			party, ErrPartyUnrecognised)
	}

	// The check no foreign key can make: `fk_reports_message` establishes that the message
	// exists, and nothing in the schema can establish that it is on *this* job. `000506`'s
	// header records `internal/bidding` making the same comparison in Go for the same reason.
	if intake.Subject == ReportSubjectMessage {
		onJob, err := rp.messages.MessageOnJob(ctx, r, jobID, intake.MessageID)
		if err != nil {
			return Report{}, false, err
		}
		if !onJob {
			return Report{}, false, fmt.Errorf("admin: %s is not a message on %s: %w",
				intake.MessageID, jobID, ErrMessageNotOnJob)
		}
	}

	id, err := uuid.NewV7()
	if err != nil {
		return Report{}, false, fmt.Errorf("admin: generating a report id: %w", err)
	}

	report, raised, err := rp.store.insertReport(ctx, r, Report{
		ID:    id,
		JobID: jobID,

		Subject:   intake.Subject,
		MessageID: intake.MessageID,

		ReporterID:    reporterID,
		ReporterParty: party,

		Reason:      intake.Reason,
		Description: intake.Description,

		Key: intake.Key,
	})
	if err != nil {
		return Report{}, false, err
	}
	if !raised {
		return rp.alreadyRaised(ctx, r, jobID, intake)
	}
	return report, true, nil
}

// alreadyRaised answers a request whose idempotency key has already raised something on this job.
//
// Split out because it is the retry path and the retry path is half the ticket — [Service.alreadyRaised]'s
// arrangement, for its reason.
//
// The row is read back rather than reconstructed from the request. What the client is told is what
// the platform holds, which is the point: a reporter's account is not amended by a retry that
// happens to disagree with it.
func (rp *Reports) alreadyRaised(
	ctx context.Context,
	r db.Runner,
	jobID uuid.UUID,
	intake ReportIntake,
) (Report, bool, error) {
	existing, found, err := rp.store.reportRaisedBy(ctx, r, jobID, intake.Key)
	if err != nil {
		return Report{}, false, err
	}
	if !found {
		// The index refused the insert and the row is not there to be read. Nothing in this
		// platform deletes a report, so this is a contradiction rather than a race, and it
		// becomes an opaque 500 with its cause logged rather than a reply that invents an
		// answer. [ErrDisputeVanished]'s reasoning, one table over.
		return Report{}, false, fmt.Errorf("admin: %s refused a duplicate on %s and holds no row for it: %w",
			intake.Key, jobID, ErrReportVanished)
	}

	// The same call the middleware makes on a fingerprint mismatch, and **wider than the dispute
	// version by one field**. A dispute is discriminated by its category alone because a job
	// carries at most one open dispute; a job carries any number of reports, about itself and
	// about each of its messages, so the *subject* is as much a part of "which action was this"
	// as the reason. A client that reused one key to report two different messages has made two
	// actions, and answering with the first would tell it that something it never sent had been
	// raised.
	if existing.Reason != intake.Reason ||
		existing.Subject != intake.Subject ||
		existing.MessageID != intake.MessageID {
		return Report{}, false, fmt.Errorf("admin: %s already raised %s on %s about %s, not %s about %s: %w",
			intake.Key, existing.Reason, jobID, existing.About(),
			intake.Reason, intake.subjectID(jobID), ErrIdempotencyKeyReused)
	}

	return existing, false, nil
}

// subjectID is what this intake points at, for a message that has to name it.
//
// [Report.About]'s reading, on the request side — the intake has no [Report] to ask yet.
func (in ReportIntake) subjectID(jobID uuid.UUID) uuid.UUID {
	if in.Subject == ReportSubjectMessage {
		return in.MessageID
	}
	return jobID
}

// ReportsOn is every report raised about a job, newest first.
//
// A read rather than a lock, and no transaction: one statement is atomic on its own.
//
// **This is not SHIP-156.** That ticket's queue is every report on the platform, paged and oldest
// first, and it reads `idx_reports_queue`; this reads `idx_reports_job` and answers "what has been
// reported about this one". It exists because this package's own tests need to establish that a
// refused intake left nothing behind, which is the assertion that would otherwise be made by
// reaching into the table — and [Service.OpenDispute] is here for the same reason.
func (rp *Reports) ReportsOn(ctx context.Context, r db.Runner, jobID uuid.UUID) ([]Report, error) {
	reports, err := rp.store.reportsOn(ctx, r, jobID)
	if err != nil && !errors.Is(err, db.ErrNoRows) {
		return nil, err
	}
	return reports, nil
}
