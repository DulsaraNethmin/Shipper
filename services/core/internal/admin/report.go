// SHIP-155a: reporting a job or a message, from the side that raises one.
//
// Docs/04 §5's **second** moderation queue is "reported jobs or messages" and Docs/04 §6 step 1 is
// "receive a report, automatic flag, or support request". This file is the first of those three.
// SHIP-156 is the queue that lists what this produces.
//
// # This is a user's endpoint in an administrative domain, and dispute.go already argued why
//
// service.go's header makes the case for `disputes` and every word of it holds here: intake is
// called by a customer or a provider, authenticated the ordinary way, and the route declares
// RequireUser. What makes the record this domain's rather than `jobs`' is what happens next —
// Docs/04 §5's queue, Docs/04 §6's process and Docs/04 §9's controls are all this domain's.
//
// # A report is not a dispute, and the difference is a status transition
//
// The two arrive through similar-looking endpoints and mean different things:
//
//	a dispute   is about a delivery that went wrong. Docs/02 §2 moves the job to 'Disputed' and
//	            §3 freezes automatic completion until an administrator resolves it. One at a time,
//	            per job, because a job is frozen once.
//	a report    is about content or conduct — a listing, a message, somebody's behaviour.
//	            **It moves nothing.** Docs/02 §2 has no transition for it, and Docs/04 §6 has an
//	            administrator choose between no action, warning, content removal, cancellation,
//	            restriction, suspension and escalation *after* reviewing it.
//
// **A report that froze a job would hand either party a unilateral freeze over the other's
// delivery, on their own say-so** — a denial of service dressed as moderation. `000805`'s header
// records the same decision from the schema's side, and the absence of any [Jobs] call in
// [Reports.Raise] is what makes it true rather than intended.
//
// # What a party is, here, is exactly what it is for a dispute — and that is a narrowing worth naming
//
// The ticket's *Done when* says the platform "resolves which side they were from the job and its
// accepted bid", which is [JobParties] verbatim, so this reuses it rather than declaring a second
// port. The consequence is that **a provider who bid and lost cannot report the listing they bid
// on**: `PartyOn` answers from the job's customer and the holder of the *accepted* bid, so on an
// Open job with no award the only party is the customer.
//
// That is what the ticket asks for and it is followed rather than widened, because widening it is a
// policy decision with a real cost on the other side — every provider who can see a job could
// report it, which is a queue a competitor can fill. **It is recorded here rather than left to be
// discovered** by whoever builds SHIP-156 and wonders why the queue is thin: if browsing providers
// should be able to report a listing, that is a row of its own and a second port, not a quiet
// change to this one.
//
// The blank line below keeps this a file note rather than a second package comment.

package admin

import (
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/DulsaraNethmin/Shipper/services/core/internal/validate"
)

// ReportSubject is what a report is about — `ck_reports_subject_type`s two values.
//
// Paired with the constraint by TestReportSubjectConstraintMatchesTheGoConstants in `migrations`,
// per Docs/10 §3.4: a kind the database accepts and Go has no constant for is a report nothing can
// read, and one Go knows and the database refuses is a write that fails at run time.
//
// # Why this is a stored discriminator rather than "is there a message id"
//
// `000805` has the full argument. In short: `moderation.go` already settled it for this domain's
// other union — an entry "says why it is here rather than leaving a console to infer it from which
// fields are blank" — and Docs/04 §5's queue will grow a third kind before it grows a third column.
type ReportSubject string

const (
	// ReportSubjectJob is the job itself: its listing, its goods, how it is described.
	ReportSubjectJob ReportSubject = "job"

	// ReportSubjectMessage is one message on the job's conversation (`000506`).
	//
	// The message must be on the job in the path. No foreign key can say so — it is a
	// comparison across two tables — so [Reports.Raise] asks [JobMessages] before it inserts.
	ReportSubjectMessage ReportSubject = "message"
)

// ReportSubjects is every subject kind, in the order the constraint lists them.
var ReportSubjects = []ReportSubject{ReportSubjectJob, ReportSubjectMessage}

// Valid reports whether s is one of [ReportSubjects].
func (s ReportSubject) Valid() bool { return slices.Contains(ReportSubjects, s) }

// String is the stored form, which is also the wire form.
//
// Unlike [Category] there is no case mapping to do: both values are already lower case single
// words, so a Wire method would be the identity function wearing a name that suggested otherwise.
func (s ReportSubject) String() string { return string(s) }

// reportSubjectNames is [ReportSubjects] as strings, for a validation message.
//
// Derived, so a message listing the accepted values cannot name one the handler would then refuse.
func reportSubjectNames() []string {
	out := make([]string, 0, len(ReportSubjects))
	for _, s := range ReportSubjects {
		out = append(out, s.String())
	}
	return out
}

// Reason is what a reporter says is wrong.
//
// # No document enumerates these, and here is what they are derived from
//
// The same position Docs/04 §7 left `disputes.category` in, answered the same way. `000800` derived
// its six from Docs/02 §5's exception table — the only list of what goes wrong on a *delivery*.
// A report is about content and conduct rather than a delivery, so the sources are the documents
// that enumerate what Shipper refuses to carry and what it escalates:
//
//	[ReasonProhibitedGoods]  Docs/01 §2's out-of-scope list and Docs/05 §4's policy position.
//	                         SHIP-59 refuses these at publication by category; this catches the
//	                         prohibited load described in prose under a permitted one.
//	[ReasonMisleading]       Docs/03's Create-job row — "flag prohibited/unclear goods", against
//	                         "poor job data leads to poor bids".
//	[ReasonOffPlatform]      Docs/03's Negotiate row: "reduce off-platform activity".
//	[ReasonAbusive]          Docs/04 §6's escalation sentence: "credible threats".
//	[ReasonFraud]            the same sentence: "suspected criminal activity".
//	[ReasonSafety]           the same sentence: "urgent safety".
//	[ReasonOther]            the escape hatch — `000800`'s reasoning, that a closed list without
//	                         one turns every unanticipated complaint into a mis-filed one, and the
//	                         filing is what a moderator triages from.
//
// **One string is not the document's wording**, and it is the trap `000800` hit first. Docs/03
// writes "off-platform activity"; the hyphen derives to `off-platform_dealing`, which is not a legal
// identifier in any of the three client languages — exactly as §5's slash was not. The value is
// reworded rather than given a hand-written exception in [Reason.Wire], because that exception would
// be the second list the derivation exists to avoid.
//
// **One list serves both subject kinds.** A reason valid for only one would need a second list and a
// cross-check between them, for a distinction a moderator does not make: a message can describe
// prohibited goods, and a listing can be abusive.
type Reason string

const (
	ReasonProhibitedGoods Reason = "Prohibited goods"
	ReasonMisleading      Reason = "Misleading listing"
	ReasonOffPlatform     Reason = "Dealing outside Shipper"
	ReasonAbusive         Reason = "Abusive or threatening"
	ReasonFraud           Reason = "Suspected fraud"
	ReasonSafety          Reason = "Safety concern"
	ReasonOther           Reason = "Other"
)

// Reasons is every reason, in the order `ck_reports_reason` lists them.
var Reasons = []Reason{
	ReasonProhibitedGoods,
	ReasonMisleading,
	ReasonOffPlatform,
	ReasonAbusive,
	ReasonFraud,
	ReasonSafety,
	ReasonOther,
}

// Valid reports whether r is one of [Reasons].
func (r Reason) Valid() bool { return slices.Contains(Reasons, r) }

func (r Reason) String() string { return string(r) }

// Wire is the reason as a client names it: lower snake case, per Docs/10 §4.7.
//
// Derived rather than tabulated, which is [Category.Wire]'s call and jobs.Status.Wire's before it: a
// seven-entry table beside seven constants is a second list that can disagree with the first, and a
// transformation cannot disagree with its input.
func (r Reason) Wire() string {
	return strings.ReplaceAll(strings.ToLower(string(r)), " ", "_")
}

// ReasonFromWire is [Reason.Wire] read backwards: the reason a client named, or false.
//
// Case-sensitive on purpose, like [CategoryFromWire]: `Safety concern` sent verbatim is the stored
// form, and a client that sent it has misread the contract rather than typed the wrong case.
func ReasonFromWire(wire string) (Reason, bool) {
	for _, known := range Reasons {
		if known.Wire() == wire {
			return known, true
		}
	}
	return "", false
}

// ReasonsWire is every reason in the form a client sends, in [Reasons] order.
//
// Derived, so a validation message listing the accepted values cannot name one the handler would
// then refuse.
func ReasonsWire() []string {
	out := make([]string, 0, len(Reasons))
	for _, r := range Reasons {
		out = append(out, r.Wire())
	}
	return out
}

// maxReportDescription bounds what a reporter may write, matching `ck_reports_description`.
//
// The same ceiling [maxDescription] gives a dispute and [maxNoteLength] gives a note, and for the
// same reason: this is prose about a situation rather than a reason field. It is duplicated as a
// CHECK on purpose — the constraint makes the bound true of the table however it is written to, and
// this makes the refusal legible, naming the field and the limit rather than handing a client a
// constraint name inside a 500 (Docs/10 §4.6).
const maxReportDescription = 4000

// ReportIntake is a report as it arrives from a party to the job.
//
// The job is in the path and the reporter is whoever the token says is calling. Neither is a field
// here, and that is Docs/07 §3: a reporter id in the request would be an authorisation decision made
// from client input, and a job id in the body would be a second answer to a question the URL asks.
//
// **There is no OccurredAt**, unlike [Intake], and `000805` records why: Docs/04 §7 names "time of
// event" as a *dispute* intake field and nothing asks a report for one — the subject of a report is
// a thing that is still there to be opened, a listing or a message, rather than an incident that
// happened out of sight.
type ReportIntake struct {
	// Subject is whether this is about the job or about one message on it.
	Subject ReportSubject

	// MessageID is which message, when [ReportIntake.Subject] is [ReportSubjectMessage], and
	// must be uuid.Nil otherwise. `ck_reports_subject` refuses the other two combinations in the
	// table; [ReportIntake.problems] refuses them where the client can be told which field.
	MessageID uuid.UUID

	Reason      Reason
	Description string

	// Key is the client's idempotency key. It reaches the reports row, so it is part of what is
	// recorded rather than part of how the request was made — [Intake.Key]'s treatment, for its
	// reason: the uniqueness it buys is `uq_reports_idempotency`'s, not this struct's.
	Key string
}

// normalise trims what the client sent into what the columns should hold.
//
// Trimmed and **not collapsed**, which is [Intake.normalise]'s decision and the same one: a
// description is up to four thousand characters of somebody setting out what is wrong, and its line
// breaks are structure rather than keyboard noise. Collapsing them would flatten paragraphs into one
// wall of text in the field a moderator has to read carefully.
func (in ReportIntake) normalise() ReportIntake {
	in.Description = strings.TrimSpace(in.Description)
	return in
}

// problems reports what is wrong with an intake, in the error contract's shape.
//
// Every problem is gathered before answering, per internal/validate's own header: a form should not
// take one round trip per field.
//
// No clock is passed, unlike [Intake.problems]. Nothing here involves time — the one instant a
// report holds is the platform's own `created_at`.
func (in ReportIntake) problems() validate.Errors {
	var e validate.Errors

	switch {
	case in.Subject == "":
		e.Add("subject_type", validate.CodeRequired, "Say whether you are reporting the job or a message.")
	case !in.Subject.Valid():
		e.Add("subject_type", validate.CodeInvalid,
			"That is not something you can report. Use one of %s.", strings.Join(reportSubjectNames(), ", "))
	}

	// The Go half of ck_reports_subject, and it exists to make the refusal legible: the
	// constraint knows the row is wrong and cannot say which field the client should change.
	switch {
	case in.Subject == ReportSubjectMessage && in.MessageID == uuid.Nil:
		e.Add("message_id", validate.CodeRequired, "Say which message you are reporting.")

	case in.Subject == ReportSubjectJob && in.MessageID != uuid.Nil:
		// Refused rather than ignored. A client that named a message and asked to report the
		// job has contradicted itself, and picking either half for it would file the report
		// against something the reporter did not choose.
		e.Add("message_id", validate.CodeInvalid,
			"Remove this to report the job itself, or set subject_type to %s to report the message.",
			ReportSubjectMessage)
	}

	switch {
	case in.Reason == "":
		e.Add("reason", validate.CodeRequired, "Say what is wrong.")
	case !in.Reason.Valid():
		e.Add("reason", validate.CodeInvalid,
			"That is not a reason for reporting. Use one of %s.", strings.Join(ReasonsWire(), ", "))
	}

	// Required as well as bounded: a reason on its own is a category, and Docs/04 §6 step 2 has
	// somebody review the report before choosing an outcome. With nothing to read, the reason is
	// all they have.
	if e.Required("description", in.Description) {
		e.Length("description", in.Description, 1, maxReportDescription)
	}

	return e
}

// Report is one row of `reports` — what somebody said was wrong, and when they said it.
//
// **It is append-only in shape**, which is [Note]'s arrangement rather than [Dispute]'s. A dispute is
// rewritten because SHIP-164 moves one through investigation to an outcome; no ticket does that to a
// report, and Docs/04 §6's outcomes are recorded where they already are — `audit_log`, the
// enforcement rows, and the job's own history. `000805` has the reasoning and the consequence: no
// `resolved_at`, no status vocabulary, and no `updated_at` on the table.
//
// **There is no field an administrator writes into**, for the reason `000800` gives about disputes
// and `000802` gives about notes: every field here is read straight back to the reporter by the
// intake response, so a note written into one would be a note the reporter reads.
type Report struct {
	ID uuid.UUID

	// JobID is the job reported, or the job the reported message is on. Never zero on either
	// kind — see `000805`: it is the context SHIP-156 opens and the scope the party check ran
	// against.
	JobID uuid.UUID

	Subject ReportSubject

	// MessageID is the reported message, and is uuid.Nil when [Report.Subject] is
	// [ReportSubjectJob]. [Report.About] is the reading of `ck_reports_subject` that keeps the
	// two in step in one place.
	MessageID uuid.UUID

	ReporterID    uuid.UUID
	ReporterParty Party

	Reason      Reason
	Description string

	// Key is the idempotency key the report was recorded under. Never on the wire — it is the
	// client's own value coming back at it, and putting it in a response invites a client to
	// treat it as an identifier the platform issued.
	Key string

	CreatedAt time.Time
}

// About is what this report points at: the message when there is one, otherwise the job.
//
// The Go reading of `ck_reports_subject`, in one place, so the constraint and the code cannot come
// to disagree about which identifier a report is *about* — [Dispute.Open]'s arrangement, for its
// reason. SHIP-156 opens this.
func (r Report) About() uuid.UUID {
	if r.Subject == ReportSubjectMessage {
		return r.MessageID
	}
	return r.JobID
}
