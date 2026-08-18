// SHIP-164: Docs/04 §7's other two stages — investigation, and the outcome that unfreezes the job.
//
// SHIP-163 built intake and stopped there, deliberately: the complainant's half is a user endpoint
// and the administrative half needed SHIP-147's credential, which did not exist. This file is that
// half. An administrator finds an open dispute on Docs/04 §5's **sixth** moderation queue, opens it
// with everything the complainant sent, and records an outcome — which sets `resolved_at`, and
// setting `resolved_at` is what lets the job move again.
//
// # Docs/04 §7's five outcomes and Docs/02 §2's two destinations are different vocabularies
//
// **This is the decision the ticket turns on**, and `000804`'s header argues it at length against
// the schema. In short: §7 lists five possible outcomes of a dispute and §2 offers a disputed job
// exactly two ways out, and **three of the five map to neither** — directing the parties to settle
// externally, warning or restricting somebody, and referring a matter to an insurer are all things
// that can be true of a delivery that nevertheless completed *or* failed.
//
// Inventing a thirteenth job status was rejected: `Docs/02` §1 has twelve and the transition table
// is authoritative for the guard. Dropping the three that do not map was rejected: that is §7
// quietly reduced to the two rows §2 happens to have, in the field that records what an
// administrator decided.
//
// So a resolution carries **both**. [Outcome] is §7's and is stored on the dispute; [JobOutcome] is
// §2's and is applied through the one guarded transition, which is the only thing that chooses a job
// status anywhere in this platform. Every resolution does both, and neither is optional.
//
// # The resolution is one transaction, and that is this ticket's hardest invariant
//
// `service.go` already named the corrupt state a non-atomic resolve leaves behind, two waves before
// there was any code to leave it: *"The job says 'Disputed' and no dispute was open, which is the
// state a resolved dispute would leave behind if SHIP-164's outcome failed to move the job back."*
// A job frozen with nothing to resolve is a job nothing can unfreeze — `uq_disputes_open_per_job`
// admits a new dispute, but the 72-hour auto-complete (Docs/02 §6.1) stays stopped and no party can
// do anything about it.
//
// The mirror image is no better: a dispute marked resolved while the job stayed `Disputed` is an
// outcome the complainant is told about and a delivery that never moves.
//
// So the row lock, the `UPDATE`, the guarded transition and the audit entry are one transaction.
// [TestAResolutionIsRefusedWhenItsAuditEntryCannotBeWritten] takes the failing branch against a real
// database with the instrument enforcement_test.go established — an auditor that fails *before any
// SQL*, so the transaction stays healthy and only an actual rollback can keep the job frozen.
//
// # No fourth Kafka aggregate, and that is a decision rather than an omission
//
// Resolving a dispute emits nothing new. The move to `Completed` or `Cancelled` goes through
// `jobs.Transition`, which already emits `job.status_changed` inside this transaction, and
// `notifications/rules.go` already carries live rules for both — its own comment says so of this
// ticket: *"Opening lands here; resolving lands on Completed or Cancelled above."*
//
// A `dispute.resolved` event would be a second announcement of one state change, which is the
// failure `notifications.StatusRules` is written to prevent, and it would need a routing rule in a
// package this change does not own. `events.TopicPartitions` is a single constant applied to every
// topic, so there was never a per-topic number to choose here; what there was is the question of
// whether a dispute is a fourth aggregate, and the answer is no. This mirrors SHIP-163's own answer
// for the freeze, and it is the reversible direction — a topic can be added later and never removed.
//
// # What Docs/04 §7's investigation stage asks for that this does not build
//
// §7's investigation sentence is two clauses and only the first is served here.
//
//	"Review the job listing, awarded bid, messages, status history, proof of delivery, and
//	 relevant verification details. Give the other party a reasonable opportunity to respond."
//
// The **review** clause needs no new surface: `GET /v1/admin/jobs/{id}` (SHIP-152) already answers
// with the listing, every bid and every recorded transition in one snapshot, and
// `GET /v1/admin/verifications` (SHIP-153) is the verification half. What this ticket adds is the
// dispute itself — the complaint, the desired outcome and the evidence — which was reachable from
// nowhere.
//
// **The right-of-reply clause is not built, and it is declared unmet rather than met in reduced
// form.** `dispute.go` says why the column is absent and it is still true: there is no place to put
// a response, no notification telling the other party one is wanted, and no window after which the
// administrator may proceed without it. Building it is a second complainant-facing endpoint, a
// notification rule in a package this change does not own, and a policy decision about how long
// "reasonable" is — which is `Docs/04` §8's territory and an operations question. It is declared
// unmet in `Docs/11` §3 rather than met in reduced form, which is what CLAUDE.md asks of a clause a
// ticket cannot close.
//
// The blank line below keeps this a file note rather than a second package comment.

package admin

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/DulsaraNethmin/Shipper/services/core/internal/clock"
	"github.com/DulsaraNethmin/Shipper/services/core/internal/db"
)

// Outcome is what an administrator decided a dispute came to — Docs/04 §7's five.
//
// # The strings are shortened from the document's sentences, and here is the rule
//
// The wire form is derived by lower-snake-casing the stored form (Docs/10 §4.7), exactly as
// [Category.Wire] derives its own, and a semicolon, a comma or a slash has no legal form under that
// mapping in any of the three client languages. `ck_disputes_category` records the same decision for
// the intake list — "Customer unavailable at pickup/delivery" became "Customer unavailable" for
// precisely this reason — and this file follows it rather than inventing a second convention.
//
// What each constant is short for is named on the constant, so a change to the reading shows up as a
// change to a comment beside a string rather than as two lists quietly drifting apart. The stored
// form is sentence case per Docs/10 §3.4, and it is held to `ck_disputes_outcome` in both directions
// by TestDisputeOutcomeConstraintMatchesTheGoConstants.
//
// # There is no "no action" outcome, and that is deliberate
//
// Docs/04 §6 step 4's moderation list opens with "no action", and §7's dispute list does not — the
// two are different lists for different processes, and this is §7's. A dispute that turns out to be
// unfounded resolves as [OutcomeDeliveryCompleted]: the delivery *was* as agreed, which is a
// finding rather than an absence of one, and the job completes. An outcome meaning "nothing
// happened" would be a resolution the trail cannot tell from an administrator who closed a queue
// item to make it go away.
type Outcome string

const (
	// OutcomeDeliveryCompleted is Docs/04 §7's "Delivery completed as agreed."
	//
	// The complaint was made and the delivery stands. Almost always paired with
	// [JobOutcomeCompleted], and not compulsorily — see [ResolutionCommand.JobOutcome].
	OutcomeDeliveryCompleted Outcome = "Delivery completed as agreed"

	// OutcomeIssueAcknowledged is Docs/04 §7's "Delivery issue acknowledged; parties directed to
	// resolve externally."
	//
	// Shortened to the finding. The clause after the semicolon is what the administrator *did*
	// about it, which is what the recorded reason says — and Docs/04 §7's closing sentence is
	// that "for MVP, Shipper records outcomes and supports resolution; it does not promise
	// compensation or adjudicate liability", which is this outcome written out.
	OutcomeIssueAcknowledged Outcome = "Delivery issue acknowledged"

	// OutcomeDeliveryFailed is Docs/04 §7's "Job cancelled / failed delivery recorded."
	//
	// **The slash in the document is the two vocabularies overlapping**, and shortening it is
	// what separates them: "job cancelled" is where the job goes, which is [JobOutcomeCancelled],
	// and "failed delivery recorded" is the finding, which is this. The document conflates them
	// because it was written before there was a transition table to be authoritative.
	OutcomeDeliveryFailed Outcome = "Failed delivery recorded"

	// OutcomeUserSanctioned is Docs/04 §7's "User warning, restriction, or suspension."
	//
	// Commas removed, all three measures kept. **Which measure it was is not recorded here**,
	// because it is recorded where the measure was taken: a restriction is
	// `POST /v1/admin/users/{id}/standing` with its own audit entry (SHIP-161) and a permanent
	// suspension is Docs/04 §9's two-person review with two of them (SHIP-166). A copy on this
	// row would be a fourth record of an act it does not perform, and one nothing keeps in step.
	OutcomeUserSanctioned Outcome = "Warning restriction or suspension"

	// OutcomeReferred is Docs/04 §7's "Referral to legal, insurer, or authorities where
	// required."
	//
	// Commas removed, all three destinations kept. Docs/04 §6's closing paragraph requires
	// urgent safety, suspected criminal activity and credible threats to follow an escalation
	// procedure approved by legal and operations; this is the dispute-shaped end of that, and the
	// escalation itself happens outside the platform.
	OutcomeReferred Outcome = "Referred to legal insurer or authorities"
)

// Outcomes is every outcome, in the order Docs/04 §7 lists them.
//
// Paired with `ck_disputes_outcome` by a test in `migrations`, in both directions, per Docs/10 §3.4:
// an outcome the database accepts and Go has no constant for is a state no code handles, and one Go
// knows and the database refuses is a resolution that fails at the `UPDATE`.
var Outcomes = []Outcome{
	OutcomeDeliveryCompleted,
	OutcomeIssueAcknowledged,
	OutcomeDeliveryFailed,
	OutcomeUserSanctioned,
	OutcomeReferred,
}

// Valid reports whether o is one of the five.
func (o Outcome) Valid() bool { return slices.Contains(Outcomes, o) }

func (o Outcome) String() string { return string(o) }

// Wire is the outcome as a client names it: lower snake case, per Docs/10 §4.7.
//
// Derived rather than tabulated, which is [Category.Wire]'s decision and the same reasoning: a
// five-entry table beside five constants is a second list that can disagree with the first, and a
// transformation cannot disagree with its input. It is also what decided every shortening above.
func (o Outcome) Wire() string {
	return strings.ReplaceAll(strings.ToLower(string(o)), " ", "_")
}

// OutcomeFromWire is [Outcome.Wire] read backwards: the outcome a client named, or false.
//
// Case-sensitive, like [CategoryFromWire]: the stored form sent verbatim is a client that has
// misread the contract rather than typed the wrong case.
func OutcomeFromWire(wire string) (Outcome, bool) {
	for _, known := range Outcomes {
		if known.Wire() == wire {
			return known, true
		}
	}
	return "", false
}

// OutcomesWire is every outcome in the form a client sends, in [Outcomes] order.
//
// Derived, so a validation message listing the accepted values cannot name one the handler would
// then refuse.
func OutcomesWire() []string {
	wire := make([]string, 0, len(Outcomes))
	for _, o := range Outcomes {
		wire = append(wire, o.Wire())
	}
	return wire
}

// JobOutcome is where a resolved dispute sends the job — Docs/02 §2's two rows out of `Disputed`.
//
// **Not a job status and not spelled like one.** CLAUDE.md's invariant is that job status is never a
// settable field, and this domain must not have a second opinion about the transition table. The two
// values name the two rows the document has; [Jobs] has a method for each, and `cmd/api` is where
// either becomes a `jobs.Status`. Nothing in this package can express a third.
type JobOutcome string

const (
	// JobOutcomeCompleted is Docs/02 §2's "Disputed → Completed, admin resolves dispute with
	// delivery accepted".
	JobOutcomeCompleted JobOutcome = "completed"

	// JobOutcomeCancelled is Docs/02 §2's "Disputed → Cancelled, admin resolves as
	// cancelled/failed delivery".
	JobOutcomeCancelled JobOutcome = "cancelled"
)

// JobOutcomes is both, in the order Docs/02 §2 lists them.
var JobOutcomes = []JobOutcome{JobOutcomeCompleted, JobOutcomeCancelled}

// Valid reports whether j is one of the two.
func (j JobOutcome) Valid() bool { return slices.Contains(JobOutcomes, j) }

func (j JobOutcome) String() string { return string(j) }

// jobOutcomeNames is both values as a client sends them, for a refusal that names the choices.
func jobOutcomeNames() []string {
	out := make([]string, 0, len(JobOutcomes))
	for _, j := range JobOutcomes {
		out = append(out, j.String())
	}
	return out
}

// DisputeState is which half of Docs/04 §5's sixth queue a caller is asking for.
//
// Two values and no more, because `disputes` holds no status enumeration — `000800` refused one and
// `000804` did not add one. Open or resolved is the whole distinction the table makes, and it is
// [Dispute.Open] read as a filter.
type DisputeState string

const (
	// DisputeStateOpen is Docs/04 §5's sixth queue as the document names it: "open disputes".
	DisputeStateOpen DisputeState = "open"

	// DisputeStateResolved is the settled half — what was decided, and by whom.
	DisputeStateResolved DisputeState = "resolved"
)

// DisputeStates is both, in the order a dispute passes through them.
var DisputeStates = []DisputeState{DisputeStateOpen, DisputeStateResolved}

// Valid reports whether s is one of the two.
func (s DisputeState) Valid() bool { return slices.Contains(DisputeStates, s) }

func (s DisputeState) String() string { return string(s) }

// disputeStateNames is both values, for a refusal that names the choices.
func disputeStateNames() []string {
	out := make([]string, 0, len(DisputeStates))
	for _, s := range DisputeStates {
		out = append(out, s.String())
	}
	return out
}

// DisputeQuery is one page of the dispute queue.
type DisputeQuery struct {
	// State is which half to list. **Defaulted to [DisputeStateOpen] by the handler rather than
	// required**, which is the opposite of [VerificationQuery.State] and is a deliberate
	// difference rather than an inconsistency.
	//
	// The verification queue refuses an absent state because Docs/04 §5's *first* queue is "new
	// or **changed**" submissions — the document names no default, every provider has a record,
	// and an ignored filter would answer an empty page that reads exactly like a quiet week.
	// Docs/04 §5's **sixth** queue is named "Open disputes", so there is a default and it is the
	// document's. A console that forgets the parameter gets the queue rather than silence, which
	// is the failure mode the other endpoint's rule exists to prevent.
	State DisputeState

	// Limit is how many entries to return. Bounded by internal/pagination before it gets here.
	Limit int

	// After is where the previous page stopped. The zero value is the first page.
	After DisputeCursor
}

// DisputeCursor is the position of the last entry a caller saw.
//
// Two fields, because neither clock is unique: several disputes are resolved in one sitting when an
// administrator works through a morning's queue, and a single-column cursor would repeat a row or
// skip one at exactly the page boundary. `At` is `created_at` on the open queue and `resolved_at` on
// the resolved one — the column that half is ordered by, whichever it is.
type DisputeCursor struct {
	At time.Time
	ID uuid.UUID
}

// Zero reports whether this is the first page.
func (c DisputeCursor) Zero() bool { return c.ID == uuid.Nil && c.At.IsZero() }

// DisputeSummary is one dispute as a queue shows it.
//
// **Deliberately not [Dispute].** The account, the desired outcome and the evidence are what an
// administrator reads when they open one, and a page of fifty carrying four thousand characters each
// is a queue nobody loads twice. More usefully it is structural rather than a saving: there is
// nowhere on this struct to put a description, so no change to the queue's response mapping can
// acquire one.
//
// **No budget, no bid and no amount**, on [VerificationEntry]'s reasoning and Docs/01 §4.3's
// invariant. A dispute is frequently about price and the administrator resolving it may genuinely
// need the number; what they open for that is `GET /v1/admin/jobs/{id}` (SHIP-152), which is where
// that decision belongs and where it has been recorded. Nothing commercial has ever been on this
// shape and nothing here adds one.
type DisputeSummary struct {
	ID    uuid.UUID
	JobID uuid.UUID

	ComplainantID    uuid.UUID
	ComplainantParty Party

	Category Category

	// OccurredAt is the complainant's clock and RaisedAt is the platform's — `000800`'s two
	// columns, both carried, because the gap between them is what triage reads.
	OccurredAt time.Time
	RaisedAt   time.Time

	// ResolvedAt, Outcome and ResolvedBy are set together or not at all
	// (`ck_disputes_resolution`). Zero on the open queue.
	ResolvedAt time.Time
	Outcome    Outcome
	ResolvedBy uuid.UUID
}

// Open reports whether this dispute is still awaiting an outcome.
func (s DisputeSummary) Open() bool { return s.ResolvedAt.IsZero() }

// ResolutionCommand is one administrator recording an outcome against a dispute.
type ResolutionCommand struct {
	// DisputeID is the dispute. Named in the path.
	//
	// **The dispute rather than the job**, unlike every other administrative action on a job.
	// One job may accumulate several disputes over its life while never having two open at once
	// (`uq_disputes_open_per_job`), so "the dispute on this job" is unambiguous only while one is
	// open — and a resolution addressed that way could not be replayed against the row it
	// actually settled once a second dispute had been raised.
	DisputeID uuid.UUID

	// ActorID is the administrator, taken from the grant rather than from the request. A body
	// that named its own actor would be an audit trail a client writes.
	ActorID uuid.UUID

	// Outcome is Docs/04 §7's finding, recorded on the dispute.
	Outcome Outcome

	// JobOutcome is Docs/02 §2's destination for the job, applied through the guarded transition.
	//
	// **Required, and not derived from [Outcome].** Three of §7's five outcomes are true of a
	// delivery that completed and equally of one that failed, so deriving would mean inventing an
	// answer for the majority of the list. Even the two that look obvious are not: a delivery
	// acknowledged as having gone wrong may still be one the customer accepts, and a dispute
	// found to be unfounded may still end in a cancellation the parties agreed between
	// themselves. The administrator says where the job goes, every time.
	JobOutcome JobOutcome

	// Reason is why, and it is required. It is written into `job_status_history.reason`, which
	// `ck_job_status_history_admin_reason` compels of an administrator's transition, and into
	// `audit_log.reason`, which is append-only. There is no third copy on the dispute —
	// `000804`'s header says why.
	Reason string
}

// Resolution is what a resolution did, for the response and for the entry.
type Resolution struct {
	DisputeID uuid.UUID

	// JobID is read from the dispute rather than supplied, so a console cannot resolve one
	// dispute and name another job.
	JobID uuid.UUID

	Outcome    Outcome
	JobOutcome JobOutcome

	// ResolvedAt is the injected clock's instant, shared with the audit entry the same
	// transaction writes — Docs/11 §9's one row, one clock, across the rows one action produces.
	ResolvedAt time.Time

	// ResolvedBy is the administrator. Recorded on the dispute *and* as the audit entry's actor,
	// which are two readers rather than one fact twice: `audit_log` answers "what has this
	// administrator done" and the column answers "who settled this" without a join to a table
	// support may not be looking at.
	ResolvedBy uuid.UUID
}

// DisputeWorkflow serves Docs/04 §7's investigation and outcome stages (SHIP-164).
//
// A service of its own rather than methods on [Service], for [Moderation]'s reason and one more.
// [Service] is intake: it owns no pool, takes a `db.Runner` from the handler, and its transaction
// spans this domain and `jobs` on behalf of a *user*. This owns a pool, an auditor and a privileged
// action. Folding them together would put the audit writer into the constructor of the one endpoint
// in this package that deliberately writes no entry.
type DisputeWorkflow struct {
	jobs    Jobs
	auditor *Auditor
	pool    *pgxpool.Pool
	store   postgresStore

	// clock is what `disputes.resolved_at` is written from.
	//
	// **Taken as its own collaborator rather than read off the auditor**, which is what
	// [Notes.Add] does one file along. The composition root hands the same `clock.Clock` to both,
	// so Docs/11 §9's one row, one clock still holds across the dispute row and the audit entry
	// describing it — and this way [Resolve] does not dereference a collaborator it is about to
	// discover is missing. That is not hypothetical: it is exactly the shape
	// [TestAResolutionIsRefusedWhenItsAuditEntryCannotBeWritten] constructs on purpose, and
	// reading the instant through a nil auditor turned that test's refusal into a panic.
	clock clock.Clock
}

// NewDisputeWorkflow builds the service.
//
// The pool may be nil, which every constructor in this package accepts: the process starts with an
// unreachable database on purpose and the endpoints answer [ErrAdminUnavailable] until it returns.
//
// The lifecycle port and the auditor may not.
//
//   - A nil lifecycle is a resolution that records an outcome and never unfreezes the job, which is
//     the exact corrupt state this file exists to prevent — and it would fail at the first
//     resolution rather than at startup.
//   - A nil auditor is refused here rather than left to [Auditor.Record]'s own check, on
//     [NewEnforcement]'s argument: that method does refuse a nil receiver, but it refuses at the
//     moment somebody exercises a privileged action, which is precisely when a service must not be
//     discovering its own wiring.
//   - A nil clock is refused for [NewAuditor]'s reason: falling back to `time.Now` would be the
//     two-clock defect Docs/11 §9 names, reintroduced through a convenience, and every row would
//     still look plausible.
func NewDisputeWorkflow(
	jobs Jobs,
	auditor *Auditor,
	clk clock.Clock,
	pool *pgxpool.Pool,
) (*DisputeWorkflow, error) {

	if jobs == nil {
		return nil, errors.New("admin: the dispute workflow needs the job lifecycle; resolving a " +
			"dispute is what unfreezes the job, and one that records an outcome without moving " +
			"the job leaves a delivery nothing can complete (Docs/02 §3, §6.1)")
	}
	if auditor == nil {
		return nil, errors.New("admin: the dispute workflow needs the audit writer; resolving a " +
			"dispute is a privileged action, and one that leaves no record cannot be " +
			"reconstructed afterwards")
	}
	if clk == nil {
		return nil, errors.New("admin: the dispute workflow needs a clock (Docs/10 §6.3); " +
			"`disputes.resolved_at` and the audit entry describing it take the same one")
	}
	return &DisputeWorkflow{jobs: jobs, auditor: auditor, clock: clk, pool: pool}, nil
}

// Queue returns one page of Docs/04 §5's sixth moderation queue.
//
// It is a read and it opens no transaction: nothing here writes and a single statement is already
// consistent with itself. Docs/10 §3.2 puts a transaction with whoever owns an invariant, and a
// queue owns none.
//
// # The two halves are ordered oppositely, and both orderings are the document's
//
// Open disputes come **oldest first**, because Docs/04 §8 sets an acknowledgement target of two
// business days and a resolution target of ten, so the oldest entry is the one closest to breaching
// one. `idx_disputes_open` was built for that in `000800` and read by nothing until this ticket.
//
// Resolved disputes come **newest first**, because a settled dispute is looked up to see what was
// decided and the decision somebody is asking about is overwhelmingly a recent one — the same split
// the account and job searches make against the review queues. `idx_disputes_resolved` is `000804`'s.
//
// An unrecognised state is refused rather than ignored, on [Verifications.AwaitingReview]'s
// reasoning: an ignored filter answers a page that looks like an answer to a question nobody asked.
func (w *DisputeWorkflow) Queue(ctx context.Context, q DisputeQuery) ([]DisputeSummary, error) {
	if !q.State.Valid() {
		return nil, fmt.Errorf("%w: %q", ErrDisputeStateUnrecognised, q.State)
	}
	if w.pool == nil {
		return nil, ErrAdminUnavailable
	}

	entries, err := w.store.disputeQueue(ctx, w.pool, q)
	if err != nil {
		return nil, fmt.Errorf("admin: reading the %s dispute queue: %w", q.State, err)
	}
	return entries, nil
}

// Dispute is one dispute with everything the complainant sent — Docs/04 §7's investigation read.
//
// A read and no transaction, for [Queue]'s reason. It answers for a resolved dispute as readily as
// an open one: an outcome that could not be read back afterwards is not a *documented* outcome, and
// the audit entry naming the dispute would point at something nothing could open.
func (w *DisputeWorkflow) Dispute(ctx context.Context, disputeID uuid.UUID) (Dispute, error) {
	if disputeID == uuid.Nil {
		return Dispute{}, ErrDisputeNotFound
	}
	if w.pool == nil {
		return Dispute{}, ErrAdminUnavailable
	}

	d, found, err := w.store.disputeByID(ctx, w.pool, disputeID)
	if err != nil {
		return Dispute{}, err
	}
	if !found {
		// Disclosed plainly, unlike the 404 on intake. The caller is an administrator holding a
		// permission over disputes and there is nothing here being kept from them; the
		// indistinguishability intake needs is a rule about *strangers*, not about the console.
		return Dispute{}, ErrDisputeNotFound
	}
	return d, nil
}

// Resolve records an outcome and unfreezes the job, in one transaction (SHIP-164).
//
// # The order inside the transaction, and why it is this order
//
//  1. **Lock the dispute row.** `SELECT … FOR UPDATE`, so two administrators reaching the same
//     queue entry at once resolve it once. The second waits, reads the row the first committed, and
//     is answered [ErrDisputeAlreadyResolved] — which is the ordinary outcome of two people working
//     one queue rather than an error either of them made.
//  2. **Write the outcome.** `resolved_at`, `outcome` and `resolved_by` together, which
//     `ck_disputes_resolution` requires; the row leaves `idx_disputes_open` at this point.
//  3. **Move the job**, through the guarded transition and the port. This is the step that
//     unfreezes it, and the one the *Done when* is about.
//  4. **Write the audit entry.** Last, because it records what happened and the switch above can
//     still refuse — an entry written before a refusal would record a resolution that did not
//     occur, in a table with no way to take it back.
//
// **All four commit together or none of them does**, and the middle two are the pair that matters:
// see the file header for the corrupt state either one alone leaves behind.
//
// # A refused transition is a refusal rather than a fault
//
// The port answers with a [JobMove] and a nil error for anything Docs/02 §2 does not permit, so a
// job that is not `Disputed` comes back as [ErrJobNotResolvable] rather than as a 500. That is
// reachable: `service.go`'s `JobAlreadyDisputed` branch admits a dispute onto a job already frozen,
// and nothing stops an administrator unpublishing or an auto-complete having moved a job that a
// stale console still shows as disputed.
func (w *DisputeWorkflow) Resolve(ctx context.Context, cmd ResolutionCommand) (Resolution, error) {
	if cmd.DisputeID == uuid.Nil {
		return Resolution{}, ErrDisputeNotFound
	}
	if cmd.ActorID == uuid.Nil {
		return Resolution{}, errors.New(
			"admin: resolving a dispute must name the administrator doing it")
	}
	if !cmd.Outcome.Valid() {
		return Resolution{}, fmt.Errorf("%w: %q", ErrDisputeOutcomeUnrecognised, cmd.Outcome)
	}
	if !cmd.JobOutcome.Valid() {
		return Resolution{}, fmt.Errorf("%w: %q", ErrJobOutcomeUnrecognised, cmd.JobOutcome)
	}
	if err := checkReason(cmd.Reason); err != nil {
		return Resolution{}, err
	}
	if w.pool == nil {
		return Resolution{}, ErrAdminUnavailable
	}

	reason := strings.TrimSpace(cmd.Reason)

	// The injected clock, which the composition root also hands the auditor — so the dispute row
	// and the entry describing it share an instant (Docs/11 §9: one row, one clock, across the
	// rows one action writes).
	at := w.clock.Now().UTC()

	var resolved Resolution
	err := db.InTx(ctx, w.pool, func(ctx context.Context, tx db.Runner) error {
		dispute, found, err := w.store.lockDispute(ctx, tx, cmd.DisputeID)
		if err != nil {
			return err
		}
		if !found {
			return ErrDisputeNotFound
		}
		if !dispute.Open() {
			return fmt.Errorf("%w: settled as %q", ErrDisputeAlreadyResolved, dispute.Outcome)
		}

		if err := w.store.resolveDispute(ctx, tx, cmd.DisputeID, cmd.Outcome, cmd.ActorID, at); err != nil {
			return err
		}

		move, err := w.moveJob(ctx, tx, dispute.JobID, cmd)
		if err != nil {
			return err
		}
		switch move {
		case JobMoved:
			// Fall through. Deliberately the only branch that reaches the entry.
		case JobNotFound:
			// fk_disputes_job is ON DELETE RESTRICT, so this is a contradiction rather than a
			// race — reported rather than assumed away.
			return fmt.Errorf("admin: %s vanished while its dispute was resolved: %w",
				dispute.JobID, ErrJobNotFound)
		case JobAlreadyResolved:
			return fmt.Errorf("%w: the job is already %s", ErrJobNotResolvable, cmd.JobOutcome)
		case JobNotResolvable:
			return ErrJobNotResolvable
		default:
			return fmt.Errorf("admin: resolving the dispute on %s: %w: %s",
				dispute.JobID, ErrJobMoveUnrecognised, move)
		}

		// The error is returned, never logged and swallowed. See the file header, and see
		// TestAResolutionIsRefusedWhenItsAuditEntryCannotBeWritten, which takes this branch.
		if _, err := w.auditor.Record(ctx, tx, AuditEntry{
			Actor:  AdminActor(cmd.ActorID),
			Action: AuditActionDisputeResolved,

			// The **job**, not the dispute. `audit_log.target_id` has no foreign key so it
			// could hold either, and a support query asking "everything that happened to this
			// job" should get the resolution back beside the unpublish and the notes.
			// AuditActionNoteAdded settled that reading; the dispute's own identifier is in
			// the metadata, so an entry can still be traced to the row it settled.
			TargetType: AuditTargetJob,
			TargetID:   dispute.JobID,
			Reason:     reason,

			// Both vocabularies, because either alone is an incomplete account of what was
			// decided — and `disputes` holds no column for where the job went, deliberately
			// (`000804`), so this is the one place an administrator's own choice of
			// destination is recorded as a choice rather than inferred from the job's status.
			Metadata: map[string]any{
				"dispute_id":  cmd.DisputeID.String(),
				"outcome":     cmd.Outcome.String(),
				"job_outcome": cmd.JobOutcome.String(),
			},
		}); err != nil {
			return err
		}

		resolved = Resolution{
			DisputeID:  cmd.DisputeID,
			JobID:      dispute.JobID,
			Outcome:    cmd.Outcome,
			JobOutcome: cmd.JobOutcome,
			ResolvedAt: at,
			ResolvedBy: cmd.ActorID,
		}
		return nil
	})
	if err != nil {
		return Resolution{}, err
	}
	return resolved, nil
}

// moveJob runs the destination the administrator chose, through the port.
//
// A switch over two named methods rather than one method taking a status, which is [Jobs]' own
// decision written out: a port shaped `Move(jobID, status)` would be the transition table's second
// opinion arriving through the back door, with this domain choosing the target. Here the choice is
// between two methods the port declares, so a third destination is a change to the port rather than
// a new string somebody passes.
//
// A [JobOutcome] with no case is an error rather than a silently skipped move. [JobOutcome.Valid]
// has already refused anything outside the two, so reaching the default means the list and this
// switch have come apart — which is a programming mistake and not a request failure.
func (w *DisputeWorkflow) moveJob(
	ctx context.Context,
	tx db.Runner,
	jobID uuid.UUID,
	cmd ResolutionCommand,
) (JobMove, error) {
	reason := strings.TrimSpace(cmd.Reason)

	switch cmd.JobOutcome {
	case JobOutcomeCompleted:
		return w.jobs.ResolveAsCompleted(ctx, tx, jobID, cmd.ActorID, reason)
	case JobOutcomeCancelled:
		return w.jobs.ResolveAsCancelled(ctx, tx, jobID, cmd.ActorID, reason)
	default:
		return JobMoveUnrecognised, fmt.Errorf(
			"admin: %q is not a destination this domain has a move for: %w",
			cmd.JobOutcome, ErrJobOutcomeUnrecognised)
	}
}
