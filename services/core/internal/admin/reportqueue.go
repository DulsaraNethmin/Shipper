// SHIP-156: Docs/04 §5's second moderation queue, from the reading end.
//
// reports.go is what a party writes and this is what an administrator reads. The two are the same
// document's stages on **opposite credentials** — a report is raised on the ordinary mobile access
// token and read with an administrator session the mobile system cannot produce (SHIP-147) — which
// is exactly the split service.go and resolution.go already make between raising a dispute and
// settling one, and the reason this is a service of its own rather than two more methods on
// [Reports].
//
// The separation is not only tidiness. [NewReports] takes no [Jobs] collaborator *on purpose*, so
// that a report cannot move a job's status: there is nothing on the type that could. Folding the
// queue into it would put two ports over `jobs` and `job_messages` onto the intake service, and the
// enforcement that "a report moves nothing" would go from structural back to intended.
//
// # The *Done when* is "reports surface with the job and conversation in context", and it is two
// endpoints
//
// The two halves are served by two reads, and the split is [DisputeSummary]'s:
//
//	GET /v1/admin/reports        every report, oldest first, paged. Enough to triage — who
//	                             reported what about which job, when, and where that job has got
//	                             to — and deliberately **not** the four-thousand-character
//	                             description, because a page of fifty carrying one each is a queue
//	                             nobody loads twice.
//	GET /v1/admin/reports/{id}   one report opened: what the reporter wrote, the job it is about,
//	                             and the conversation on that job. This is the "in context" half.
//
// A single endpoint carrying the conversation on every entry was the alternative and it is the shape
// the queue exists to avoid: fifty reports on fifty jobs is fifty negotiations of up to two thousand
// characters a message, assembled to render a list somebody scans.
//
// # Why the queue carries the job's status and nothing else about the job
//
// A moderator working oldest-first needs to know whether they are looking at a live listing or a
// delivery that finished three weeks ago, and that is one column. `cancellationEntryResponse` carries
// `from_status` and [ExceptionEntry] carries `JobStatus` for the same reason on the two neighbouring
// queues, so this is the third instance of a settled decision rather than a new one.
//
// Everything else about the job — the listing text, the customer — is on the report screen, where
// there is one of it. The bids, the amounts and the status history are on `GET /v1/admin/jobs/{id}`,
// which is where [DisputeSummary] already recorded that they belong.
//
// # There is no filter, and that is a decision rather than an omission
//
// The dispute queue takes `state` and the exception queue takes `ground`, so the absence is worth
// accounting for. Neither of those is a convenience: `state` exists because a dispute *has* two
// states ordered by two different clocks, and `ground` exists because Docs/04 §5's fourth queue is
// four situations the document made one screen. A report has no state — `000805` records at length
// why there is no `resolved_at` and no status vocabulary — and Docs/04 §5's second queue is one
// situation.
//
// The narrowing somebody will eventually want is by reason: Docs/04 §6's escalation sentence names
// "urgent safety, suspected criminal activity, or credible threats", which are [ReasonSafety],
// [ReasonFraud] and [ReasonAbusive], and a moderator who must escalate those wants to find them. It
// is not built here because no document asks for it and the *Done when* does not, and it is cheap to
// add later: SHIP-157 widened SHIP-117's queue with a filter while keeping its path, its cursor, its
// ordering and its shape. Recorded rather than left to be re-derived.
//
// The blank line below keeps this a file note rather than a second package comment.

package admin

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

// ReportQuery is one page of Docs/04 §5's second moderation queue.
//
// Cursor paged rather than offset paged, per Docs/10 §4.5: reports arrive while somebody is working
// through the queue, and an offset would show them the same complaint twice or skip one. A skipped
// one here is somebody who reported a safety concern and was never read.
//
// **It reuses neither [QueueQuery] nor [DisputeQuery]**, and the reason is the field each of those
// carries: `Ground` and `State` are filters this queue deliberately does not have (see the file
// header), and a type carrying a field its only consumer must always leave empty is a type that
// invites somebody to fill it in.
type ReportQuery struct {
	// Limit is how many entries to return. Bounded by internal/pagination before it gets here.
	Limit int

	// After is where the previous page stopped. The zero value is the first page.
	After ReportCursor
}

// ReportCursor is the position of the last entry a caller saw.
//
// Two fields, and they are exactly `idx_reports_queue`'s key. `created_at` is not unique — two
// reports raised in the same millisecond are ordinary on a platform-wide queue — and a single-column
// cursor over a non-unique key either repeats a row or skips one. `000805` widened the index for
// this at creation rather than leaving the tie-break to be served from the heap, which is the
// widening `000804` records deliberately not making to `000800`'s.
type ReportCursor struct {
	RaisedAt time.Time
	ID       uuid.UUID
}

// Zero reports whether this is the first page.
func (c ReportCursor) Zero() bool { return c.ID == uuid.Nil && c.RaisedAt.IsZero() }

// ReportEntry is one report as the queue shows it.
//
// **Deliberately not [Report].** The description is what an administrator reads when they open one,
// and this shape has nowhere to put it — so no change to the queue's response mapping can acquire
// one, which is [DisputeSummary]'s arrangement and its reason.
//
// **No idempotency key either**, and that one is not about weight: it is the reporter's own value,
// and a shape that carried it would invite a console to treat it as an identifier the platform
// issued.
//
// **Nothing commercial, and nowhere in this shape to put any** (Docs/01 §4.3).
type ReportEntry struct {
	// ID is the report, and what the detail endpoint's path names.
	ID uuid.UUID

	// JobID is the job reported, or the job the reported message is on. **Not unique on this
	// queue** — `000805` allows a job any number of reports, and two parties complaining about
	// one listing for two reasons are two things a moderator wants to see.
	JobID uuid.UUID

	// JobStatus is where that job is now, hydrated through [ReportedJobs] rather than read from
	// `reports` — this domain holds no copy of another domain's status, and `000805` records why
	// there is no status column on the table.
	//
	// It is triage rather than a judgement: a report on an `Open` listing is a different urgency
	// from one on a job delivered a fortnight ago, and a moderator working oldest-first cannot
	// tell them apart from the report alone.
	JobStatus string

	// Subject is whether this is about the job or about one message on it, and
	// [ReportEntry.About] is the reading that keeps it in step with [ReportEntry.MessageID].
	Subject ReportSubject

	// MessageID is the reported message, and uuid.Nil when the subject is the job itself
	// (`ck_reports_subject`).
	MessageID uuid.UUID

	ReporterID    uuid.UUID
	ReporterParty Party

	Reason Reason

	// RaisedAt is the platform's clock and the only instant a report holds. What the queue is
	// ordered by, and what Docs/04 §8's acknowledgement target is measured from.
	RaisedAt time.Time
}

// About is what this entry points at: the message when there is one, otherwise the job.
//
// [Report.About]'s reading on the queue's shape, so the constraint and the code cannot come to
// disagree about it in two places.
func (e ReportEntry) About() uuid.UUID {
	if e.Subject == ReportSubjectMessage {
		return e.MessageID
	}
	return e.JobID
}

// ReportDetail is one report opened — the "in context" half of the *Done when*.
//
// Three parts and each is a different domain's: the complaint is `reports`, the job is `jobs`
// through [ReportedJobs], and the conversation is `job_messages` through [JobConversations]. One
// shape rather than three endpoints, on [JobDetail]'s reasoning — a console that had to make three
// calls to render one screen would be three chances to show a report beside somebody else's job.
type ReportDetail struct {
	// Report is what the reporter wrote, in full. Its [Report.Key] is never put on the wire.
	Report Report

	// Job is what was reported, or what the reported message was said about.
	Job ReportedJob

	// Conversation is every message on the job, in `idx_job_messages_conversation` order.
	//
	// Docs/04 §6 step 2 — "review the affected job, user profile, **communications**, and
	// history" — and the clause this ticket's *Done when* names. Empty is ordinary: most jobs
	// are never discussed, and a report about a listing is frequently raised before anybody has
	// said anything.
	//
	// **The whole job's, not one negotiation's**, and [JobConversations] argues why.
	Conversation []ConversationMessage
}

// ReportQueue is Docs/04 §5's second moderation queue, from the reading end (SHIP-156).
type ReportQueue struct {
	jobs          ReportedJobs
	conversations JobConversations
	store         postgresStore
	pool          *pgxpool.Pool
}

// NewReportQueue builds the queue service.
//
// The pool may be nil — the process starts with an unreachable database on purpose, so a rolling
// deployment during a failover does not take every instance down at once — and the two ports may
// not. Each nil is a different silence and both are worse than a refusal at startup:
//
//   - a nil [ReportedJobs] is a queue in which every entry's job status is blank, which reads as a
//     rendering fault rather than as the wiring mistake it is;
//   - a nil [JobConversations] is a report screen that shows every conversation as empty — and an
//     empty conversation is *ordinary* on this screen, so a moderator reviewing a complaint about
//     what somebody said would conclude nothing was said. That is the failure [NewModeration]
//     describes as indistinguishable from a quiet week, arriving at the one moment somebody is
//     deciding whether to suspend an account.
//
// It returns an error rather than panicking, which is [NewModeration]'s shape rather than
// [NewReports]': the constructors that answer a *console* are the ones cmd/api can report on at
// startup, and main.go already handles an error from every one of them.
func NewReportQueue(
	jobs ReportedJobs,
	conversations JobConversations,
	pool *pgxpool.Pool,
) (*ReportQueue, error) {
	if jobs == nil {
		return nil, errors.New("admin: the report queue needs the job lookup; without one every " +
			"entry's status is blank, which reads as a broken screen rather than a mis-wire")
	}
	if conversations == nil {
		return nil, errors.New("admin: the report queue needs the conversation lookup; without " +
			"one a report about something somebody said opens onto an empty conversation, " +
			"which is indistinguishable from nothing having been said (Docs/04 §6 step 2)")
	}
	return &ReportQueue{jobs: jobs, conversations: conversations, pool: pool}, nil
}

// Queue returns one page of Docs/04 §5's second moderation queue, oldest first.
//
// A read, and it opens no transaction: nothing here writes, and Docs/10 §3.2 puts a transaction with
// the domain that owns an invariant — a queue owns none. **That holds even though this is two
// statements**, and it is worth saying because [JobDirectory.OpenJob] wraps three for the opposite
// reason: there, a job header beside a bid list from a different instant is a screen somebody acts
// on. Here the second statement supplies a triage hint, and a status a second stale changes the
// order nobody reads the queue in rather than a decision anybody takes.
func (q *ReportQueue) Queue(ctx context.Context, query ReportQuery) ([]ReportEntry, error) {
	if q.pool == nil {
		return nil, ErrAdminUnavailable
	}

	entries, err := q.store.reportQueue(ctx, q.pool, query)
	if err != nil {
		return nil, fmt.Errorf("admin: reading the report queue: %w", err)
	}
	if len(entries) == 0 {
		return entries, nil
	}

	jobs, err := q.jobs.JobsForReports(ctx, q.pool, distinctJobsOf(entries))
	if err != nil {
		return nil, fmt.Errorf("admin: reading the jobs a page of reports is about: %w", err)
	}
	for i, entry := range entries {
		job, found := jobs[entry.JobID]
		if !found {
			// A contradiction rather than an absence, and answered as one:
			// `fk_reports_job` is ON DELETE RESTRICT, so nothing in this platform can
			// remove a job a report points at. [Reports.alreadyRaised] takes the same
			// position on the same kind of impossibility — an opaque 500 with its cause
			// logged, rather than a page that invents a blank status for it.
			return nil, fmt.Errorf("admin: report %s is about %s, which no job answered for: %w",
				entry.ID, entry.JobID, ErrReportedJobVanished)
		}
		entries[i].JobStatus = job.Status
	}
	return entries, nil
}

// distinctJobsOf is the jobs a page of entries names, each once.
//
// Deduplicated because a job may carry several reports (`000805`) and a page of five complaints
// about one listing is five copies of one identifier — which the `= ANY` would answer identically
// and which makes the parameter grow for nothing.
func distinctJobsOf(entries []ReportEntry) []uuid.UUID {
	seen := make(map[uuid.UUID]struct{}, len(entries))
	ids := make([]uuid.UUID, 0, len(entries))
	for _, entry := range entries {
		if _, already := seen[entry.JobID]; already {
			continue
		}
		seen[entry.JobID] = struct{}{}
		ids = append(ids, entry.JobID)
	}
	return ids
}

// Report is one report with the job and the conversation it is about — the *Done when*'s second half.
//
// A read and no transaction, for [ReportQueue.Queue]'s reason.
//
// # The conversation is read whichever kind of report this is
//
// A report about a *message* obviously needs it: reading the reported line without the exchange
// around it is how a moderator mistakes a quoted price for an offer. A report about the *job* needs
// it too, and less obviously — Docs/04 §6 step 2 asks a reviewer to read the communications on every
// review, and a party who reports a listing under [ReasonAbusive] has almost certainly been told
// something rather than read something.
//
// So there is no branch on [Report.Subject] here. The reported message, when there is one, is
// identified within the conversation by the report's own `message_id`.
func (q *ReportQueue) Report(ctx context.Context, reportID uuid.UUID) (ReportDetail, error) {
	if reportID == uuid.Nil {
		return ReportDetail{}, ErrReportNotFound
	}
	if q.pool == nil {
		return ReportDetail{}, ErrAdminUnavailable
	}

	report, found, err := q.store.reportByID(ctx, q.pool, reportID)
	if err != nil {
		return ReportDetail{}, err
	}
	if !found {
		// Disclosed plainly, unlike intake's 404. The caller is an administrator holding a
		// permission over the moderation queues and there is nothing here being kept from
		// them; the indistinguishability [ErrNotAParty] buys is a rule about *strangers*.
		return ReportDetail{}, ErrReportNotFound
	}

	jobs, err := q.jobs.JobsForReports(ctx, q.pool, []uuid.UUID{report.JobID})
	if err != nil {
		return ReportDetail{}, fmt.Errorf("admin: reading the job report %s is about: %w", reportID, err)
	}
	job, found := jobs[report.JobID]
	if !found {
		return ReportDetail{}, fmt.Errorf("admin: report %s is about %s, which no job answered for: %w",
			reportID, report.JobID, ErrReportedJobVanished)
	}

	conversation, err := q.conversations.ConversationOn(ctx, q.pool, report.JobID)
	if err != nil {
		return ReportDetail{}, fmt.Errorf("admin: reading the conversation on %s: %w", report.JobID, err)
	}

	return ReportDetail{Report: report, Job: job, Conversation: conversation}, nil
}
