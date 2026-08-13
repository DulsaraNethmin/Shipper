package admin

import (
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"

	"github.com/DulsaraNethmin/Shipper/services/core/internal/validate"
)

// The dispute vocabulary — what Docs/04 §7 asks intake to capture, as types.
//
// § 7 is three sentences and this file is most of what they mean. The intake sentence is the
// specification:
//
//	"Capture job, complainant, category, description, desired outcome, time of event, and
//	 evidence."
//
// Six of the seven are unambiguous. Two needed a reading, and both are recorded where the reading
// lives rather than in a commit message: the category list is below, and evidence is on [Intake].

// Party is which side of a job somebody is on.
//
// Narrower than jobs.ActorType's five and narrower than audit_log's three, because it answers one
// question: whether this caller is entitled to raise a dispute about this job. Docs/02 §2 permits
// "an eligible user/admin", and the two eligible users are the customer who owns the job and the
// provider who won it.
//
// **It is not users.role.** A provider who bid and lost is a provider and is not a party to the
// delivery; the customer of another job is a customer and is not a party to this one. The platform
// resolves this from the job and its accepted bid, through [JobParties], and never from a claim in
// a token or a field in a request.
//
// `admin` declares it rather than importing jobs.ActorType, because domains do not import each
// other (Docs/06 §4.1) and because these two values are not those five.
type Party string

const (
	PartyCustomer Party = "customer"
	PartyProvider Party = "provider"
)

// Parties is both, in the order Docs/02 §1 names them.
//
// Paired with ck_disputes_complainant_party by TestDisputePartyConstraintMatchesTheGoConstants,
// which is what Docs/10 §3.4 asks of every enumeration: a value the database accepts and Go has no
// constant for is a state no code handles.
var Parties = []Party{PartyCustomer, PartyProvider}

// Valid reports whether p is one of the two.
func (p Party) Valid() bool {
	for _, known := range Parties {
		if p == known {
			return true
		}
	}
	return false
}

func (p Party) String() string { return string(p) }

// Category is what a complainant says the dispute is about.
//
// # Docs/04 §7 names this field and enumerates no values, and this is the reading taken
//
// The six below are derived from Docs/02 §5's exception table — the only list in the documents of
// what actually goes wrong on a delivery — plus [CategoryOther]. Two of §5's seven rows are left
// out: a driver who has lost their portal link and a driver recording milestones with no signal are
// operational events with handling of their own, not things a party complains about.
//
// One string is shortened. §5 writes "Customer unavailable at pickup/delivery", and the slash has
// no legal form under the derived wire mapping below. The other four are §5's own wording.
//
// **The escape hatch is deliberate.** A closed list with no [CategoryOther] turns every complaint
// nobody anticipated into a mis-filed one, and the filing is what an administrator triages from. A
// dispute that arrives as "Other" with a description is worth more than one filed under the nearest
// wrong heading.
//
// The stored form is the document's sentence case per Docs/10 §3.4, and the constraint behind it is
// held to [Categories] by test.
type Category string

const (
	CategoryProviderNoShow      Category = "Provider fails to arrive"
	CategoryGoodsDiffer         Category = "Goods differ from listing"
	CategoryCustomerUnavailable Category = "Customer unavailable"
	CategoryLate                Category = "Delivery is late"
	CategoryGoodsDamaged        Category = "Goods damaged or missing"
	CategoryOther               Category = "Other"
)

// Categories is every category, in the order Docs/02 §5 lists the scenarios behind them.
var Categories = []Category{
	CategoryProviderNoShow,
	CategoryGoodsDiffer,
	CategoryCustomerUnavailable,
	CategoryLate,
	CategoryGoodsDamaged,
	CategoryOther,
}

// Valid reports whether c is one of the six.
func (c Category) Valid() bool {
	for _, known := range Categories {
		if c == known {
			return true
		}
	}
	return false
}

func (c Category) String() string { return string(c) }

// Wire is the category as a client names it: lower snake case, per Docs/10 §4.7.
//
// Derived rather than tabulated, which is the same call jobs.Status.Wire and delivery.Milestone.Wire
// both make and for the same reason: a six-entry table beside six constants is a second list that
// can disagree with the first, and a transformation cannot disagree with its input.
//
// It is also what decided the one shortened string above. "Customer unavailable at pickup/delivery"
// derives to a wire form containing a slash, which is not a legal enum value in any of the three
// client languages — and tabulating a hand-written exception for it would have reintroduced exactly
// the second list this function exists to avoid.
func (c Category) Wire() string {
	return strings.ReplaceAll(strings.ToLower(string(c)), " ", "_")
}

// CategoryFromWire is [Category.Wire] read backwards: the category a client named, or false.
//
// Case-sensitive on purpose, like delivery's: `Goods damaged or missing` sent verbatim is the
// stored form, and a client that sent it has misread the contract rather than typed the wrong case.
func CategoryFromWire(wire string) (Category, bool) {
	for _, known := range Categories {
		if known.Wire() == wire {
			return known, true
		}
	}
	return "", false
}

// CategoriesWire is every category in the form a client sends, in [Categories] order.
//
// Derived, so a validation message listing the accepted values cannot name one the handler would
// then refuse.
func CategoriesWire() []string {
	wire := make([]string, 0, len(Categories))
	for _, c := range Categories {
		wire = append(wire, c.Wire())
	}
	return wire
}

// The bounds on what a complainant may write.
//
// Generous and finite. Somebody describing an incident that cost them a day is not filling in a
// milestone note, so the description is four thousand characters; the desired outcome is what they
// want done about it, which is a sentence rather than an account.
//
// They are duplicated as CHECK constraints in 000800 on purpose. The constraints are what make the
// bound true of the table however it is written to; these are what make the refusal legible — a
// client that wrote too much is told which field and what the limit is, rather than handed a
// constraint name in a 500 (Docs/10 §4.6).
const (
	maxDescription    = 4000
	maxDesiredOutcome = 1000

	// maxEvidenceItems and maxEvidenceItem bound the list Docs/04 §7 asks for. Twenty references
	// is more than any pilot-scale complaint will carry, and five hundred characters is a
	// sentence describing where something is rather than the thing itself.
	maxEvidenceItems = 20
	maxEvidenceItem  = 500
)

// Intake is a dispute as it arrives from a complainant — Docs/04 §7's seven fields, minus the two
// the platform supplies.
//
// The job is in the path and the complainant is whoever the token says is calling. Neither is a
// field here, and that is Docs/07 §3: a complainant id in the request would be an authorisation
// decision made from client input, and a job id in the body would be a second answer to a question
// the URL already asks.
//
// # OccurredAt is required, and that is a decision rather than a default
//
// Docs/04 §7 names "time of event" as an intake field, distinct from the moment the report arrives —
// which the platform records for itself in created_at. Defaulting it to now() would write the
// report's time into the incident's column on every intake that omitted it, and support could not
// afterwards tell that value from one somebody meant. So it is required, and a client with nothing
// better to send sends now() knowingly.
//
// It is not corrected and not bounded backwards, on the same reasoning milestones.actor_recorded_at
// carries (Docs/02 §3.1): a device with a wrong clock still described something that happened. A
// time in the *future* is refused, because that one cannot be a late report and is a client defect.
//
// # Evidence is references in the complainant's own words, and here is why
//
// Docs/04 §7 asks for evidence, and there is nothing to attach. Verification evidence uploads to
// private object storage through short-lived pre-signed URLs (Docs/04 §3.1) and delivery proof does
// the same from SHIP-114; neither exists, so no complainant can produce an object reference and no
// endpoint would give them one.
//
// Capturing nothing would drop a field the ticket's *Done when* names. Capturing an upload path
// would be building SHIP-114 inside SHIP-163. So what is captured is what a complainant can supply
// today — "photographed the crate at the depot", "the driver's message of 14/08" — as text the
// platform stores and does not resolve. When uploads exist, an attachment is a row in a table of
// its own pointing at this one, and nothing here changes.
type Intake struct {
	Category       Category
	Description    string
	DesiredOutcome string
	OccurredAt     time.Time
	Evidence       []string

	// Key is the client's idempotency key. It reaches the disputes row, so it is part of what is
	// recorded rather than part of how the request was made — the same treatment
	// delivery.Recording.Key gets, and for the same reason: the uniqueness it buys is
	// uq_disputes_idempotency's, not this struct's.
	Key string
}

// normalise trims what the client sent into what the columns should hold.
//
// # The account is trimmed and not collapsed, and the difference is deliberate
//
// delivery collapses a milestone reason — a phrase typed on a phone, where a run of spaces is
// keyboard noise. A description is up to four thousand characters of somebody setting out what
// happened to them, and its line breaks are structure rather than noise. Collapsing them would
// flatten four paragraphs into one wall of text, in the one field an administrator has to read
// carefully. So both text fields lose only their leading and trailing whitespace.
//
// Evidence items *are* collapsed, and blanks are dropped rather than reported. Each is a short
// reference rather than an account, a client that sent an empty row in a list has an empty row in
// its form rather than a complaint about the platform's validation — and ck_disputes_evidence
// refuses the empty string, so dropping it here is what stops a blank field becoming a 500.
func (in Intake) normalise() Intake {
	in.Description = strings.TrimSpace(in.Description)
	in.DesiredOutcome = strings.TrimSpace(in.DesiredOutcome)

	evidence := make([]string, 0, len(in.Evidence))
	for _, e := range in.Evidence {
		if collapsed := collapse(e); collapsed != "" {
			evidence = append(evidence, collapsed)
		}
	}
	in.Evidence = evidence

	return in
}

// problems reports what is wrong with an intake, in the error contract's shape.
//
// Every problem is gathered before answering, per internal/validate's own header: a five-field form
// should not take five round trips to fill in, on a phone, at a depot.
//
// now is passed rather than read, so the one bound that involves the clock is testable without
// sleeping (Docs/10 §6.3).
func (in Intake) problems(now time.Time) validate.Errors {
	var e validate.Errors

	switch {
	case in.Category == "":
		e.Add("category", validate.CodeRequired, "Say what this dispute is about.")
	case !in.Category.Valid():
		e.Add("category", validate.CodeInvalid,
			"That is not a dispute category. Use one of %s.", strings.Join(CategoriesWire(), ", "))
	}

	if e.Required("description", in.Description) {
		e.Length("description", in.Description, 1, maxDescription)
	}
	if e.Required("desired_outcome", in.DesiredOutcome) {
		e.Length("desired_outcome", in.DesiredOutcome, 1, maxDesiredOutcome)
	}

	switch {
	case in.OccurredAt.IsZero():
		e.Add("occurred_at", validate.CodeRequired,
			"Say when this happened. It is not the same as when you are reporting it.")

	case in.OccurredAt.After(now):
		// The one bound in this file. A late report is ordinary and a report of something that
		// has not happened yet is a client defect — a picker set to the wrong year, a clock in
		// the wrong direction — and accepting it would put a date in the incident column that
		// nothing downstream can order against the rest of the job's history.
		e.Add("occurred_at", validate.CodeOutOfRange,
			"That is in the future. Send when the problem happened, not when you expect it to.")
	}

	if len(in.Evidence) > maxEvidenceItems {
		e.Add("evidence", validate.CodeTooLong,
			"Describe up to %d pieces of evidence. Put the rest in the description.", maxEvidenceItems)
	}
	for i, item := range in.Evidence {
		if utf8.RuneCountInString(item) > maxEvidenceItem {
			// Indexed, because the client renders one input per item and the message has to
			// land beside the one that is wrong (Docs/10 §4.6 — the field is the dotted path
			// into the JSON the client sent).
			e.Add(fieldEvidenceItem(i), validate.CodeTooLong,
				"Keep this to %d characters or fewer.", maxEvidenceItem)
		}
	}

	return e
}

// fieldEvidenceItem is the dotted path to one evidence entry, as the client serialised it.
func fieldEvidenceItem(i int) string {
	return "evidence." + strconv.Itoa(i)
}

// collapse trims a value and reduces internal runs of whitespace to single spaces.
//
// The same treatment delivery gives a milestone reason. What arrives from a phone keyboard carries
// stray spaces and line breaks that are not part of what somebody wrote, and a description stored
// with them reads as a different string to every later comparison.
func collapse(s string) string {
	return strings.Join(strings.Fields(s), " ")
}

// Dispute is one row of disputes — what somebody said went wrong, and when.
//
// It is the record Docs/04 §7 asks intake to produce, and it is not evidence in the append-only
// sense that a milestone or an audit entry is: SHIP-164 writes to it again as the dispute moves
// through investigation to an outcome. What is append-only is the audit trail of what
// administrators did to it, which is audit_log and a different table for exactly that reason.
//
// **There is no field an administrator writes into.** SHIP-162's internal notes are never
// user-visible, and every field here is read straight back to the complainant by the intake
// response — so a note written into one of them would be a note the complainant reads. Notes get
// rows of their own.
type Dispute struct {
	ID    uuid.UUID
	JobID uuid.UUID

	ComplainantID    uuid.UUID
	ComplainantParty Party

	Category       Category
	Description    string
	DesiredOutcome string

	// OccurredAt is the complainant's clock and CreatedAt is the platform's. Two columns, never
	// one (Docs/10 §3.3): the first is when the problem happened and the second is when it was
	// reported, and support reasons about the gap between them.
	OccurredAt time.Time

	Evidence []string

	// Key is the idempotency key the intake was recorded under. Never on the wire — it is the
	// client's own value coming back at it, and putting it in a response invites a client to
	// treat it as an identifier the platform issued.
	Key string

	// ResolvedAt is zero while the dispute is open. SHIP-164 sets it, and setting it is also
	// what unfreezes the job.
	ResolvedAt time.Time

	CreatedAt time.Time
	UpdatedAt time.Time
}

// Open reports whether the dispute is still awaiting an outcome.
//
// The Go reading of uq_disputes_open_per_job's predicate. One place, so the index and the code
// cannot come to disagree about what "open" means.
func (d Dispute) Open() bool { return d.ResolvedAt.IsZero() }
