package delivery

import (
	"strings"
	"time"
	"unicode"

	"github.com/google/uuid"

	"github.com/DulsaraNethmin/Shipper/services/core/internal/validate"
)

// The driver carrying one job (SHIP-105, SHIP-106).
//
// # A driver is a row rather than an account
//
// 000600 says it and this file is where it starts to bite: the driver portal is
// link-authenticated (Docs/07 §3), so there is no `users` row to point at and nothing to look a
// driver up by. What the platform holds about them is what the provider typed — a name and a
// number — and that is the whole of their identity for the length of one job. It is also why
// those two fields are immutable once written: 000401 lets job_status_history name an assignment
// as an actor, so editing the name would re-attribute work somebody else did.

// maxDriverName bounds the name a provider may write down.
//
// A bound against a runaway text field rather than a judgement that moves under operational
// pressure, which is why it is a constant here and not configuration (Docs/06 §5.3 draws that line,
// and `jobs` and `fleet` both draw it the same way). 000600 left it to this ticket deliberately:
// "No length bound here. A maximum is a validation limit ... SHIP-106 bounds it."
//
// Generous, because a driver is identified by whatever the provider writes down, which in a
// subcontracting yard is frequently a name and a company beside it. Long enough for
// "Sam Patel — Patel Bros Transport", far short of a paragraph.
//
// **There is deliberately no companion bound on the mobile.** It already has one that is not a
// judgement at all: [validE164] permits a '+' and at most fifteen digits, which is the standard's
// own limit and the shape ck_driver_assignments_mobile checks. A second length rule beside it would
// be a number somebody could change without changing anything.
const maxDriverName = 120

// Assignment is one driver on one job — a row of driver_assignments.
//
// UnassignedAt is the zero time while the driver is still on the job, which is what
// uq_driver_assignments_active counts. It is not a soft delete (Docs/10 §3.3 has none): the row
// stays a true statement about a period of the job's life, which is exactly what a milestone
// recorded during that period needs it to be.
type Assignment struct {
	ID    uuid.UUID
	JobID uuid.UUID

	DriverName   string
	DriverMobile string

	UnassignedAt time.Time

	CreatedAt time.Time
	UpdatedAt time.Time
}

// Live reports whether this driver is still on the job.
func (a Assignment) Live() bool { return a.UnassignedAt.IsZero() }

// Nomination is what a provider says about who is driving.
//
// # Self is not a shortcut for "fill the fields in for me"
//
// Docs/02 §1 has one status for two things a provider does — "has nominated a driver or
// self-assigned" — and the difference is where the details come from rather than what is recorded.
// A provider driving the job themselves is still a row in driver_assignments, because SHIP-107
// hangs the job-scoped link off that row and the link has to reach somebody.
//
// So Self fills the *mobile* from the account, which is a number the platform holds and has
// verified (SHIP-36), and asks for the name, which it does not hold at all: `users` has no name
// column, nothing in registration captures one, and the `profiles` domain that eventually will is
// still doc.go. That is stated here rather than worked around, because the alternatives are worse
// — an email local part is not a person's name, and a blank name is refused by
// ck_driver_assignments_name. When profiles lands, the name can default too and the wire shape does
// not change: an omitted `driver_name` becomes legal, which is an additive change (Docs/07 §6).
type Nomination struct {
	DriverName   string
	DriverMobile string

	// Self says the provider is driving the job themselves.
	//
	// A field rather than a separate endpoint, because it is the same intent with one detail
	// supplied differently, and two endpoints would be two places for the awarded-provider check
	// and the transition to drift apart.
	Self bool
}

// normalise trims what the provider typed into what the column should hold.
//
// The mobile is put into E.164 here rather than in SQL, for the reason 000002 gives about
// users.phone: a stored number that has not been normalised is a number two spellings of one
// handset can hide behind, and ck_driver_assignments_mobile compares strings.
func (n Nomination) normalise() Nomination {
	n.DriverName = collapse(n.DriverName)
	n.DriverMobile = normalisePhone(n.DriverMobile)
	return n
}

// problems reports what is wrong with a nomination, in the error contract's shape.
//
// Gathered rather than returned one at a time: a provider filling this in on a phone, in a yard,
// should be told about both fields at once (Docs/10 §4.6).
func (n Nomination) problems() validate.Errors {
	var e validate.Errors

	if e.Required("driver_name", n.DriverName) {
		e.Length("driver_name", n.DriverName, 1, maxDriverName)
	}

	switch {
	case n.Self && n.DriverMobile != "":
		// Refused rather than silently preferred, either way round. A provider who sent
		// both has two numbers in mind and the platform must not guess which; and a
		// self-assignment that quietly ignored the number supplied would send SHIP-107's
		// link somewhere the provider did not expect.
		e.Add("driver_mobile", validate.CodeNotAllowed,
			"Leave this out when you are driving the job yourself — we use the mobile on your account.")

	case n.Self:
		// Nothing to check. The number comes from the account, and it was normalised and
		// verified before it was stored.

	case e.Required("driver_mobile", n.DriverMobile):
		if !validE164(n.DriverMobile) {
			e.Add("driver_mobile", validate.CodeInvalid,
				"Enter a mobile number the driver can be reached on, like 0412 345 678.")
		}
	}

	return e
}

// collapse trims a value and reduces every internal run of whitespace to one space.
//
// The same treatment `jobs` gives free text, and it is not cosmetic here: ck_driver_assignments_name
// refuses a name with no non-whitespace character, and a form field somebody tabbed through returns
// a tab. Collapsing first means the validator reports a required field rather than the database
// reporting a constraint.
func collapse(s string) string { return strings.Join(strings.Fields(s), " ") }

// normalisePhone puts a submitted number into E.164, assuming Australia where the number does not
// say otherwise.
//
// **This is a second copy of identity's function of the same name, and that is the rule rather
// than an oversight.** Domains do not import each other (Docs/06 §4.1), and a shared package for
// it would be infrastructure — a decision recorded in internal/boundaries, which a domain branch
// does not edit (Docs/10 §9.2). What keeps the two honest is not the code being shared but the
// column: ck_driver_assignments_mobile and the users.phone convention are the same regexp, and
// 000600 wrote it out deliberately for that reason.
//
// The Australian default is the product's rather than the standard's — Docs/01 §8 puts the pilot in
// one Australian metropolitan area, and a driver there gives their number with a leading zero.
func normalisePhone(submitted string) string {
	// Only the punctuation people actually write a phone number with is removed. Anything else
	// is kept, so that validE164 sees it and refuses the number — dropping unrecognised
	// characters would silently turn `0412 34a 678` into a valid-looking number belonging to
	// somebody else.
	const separators = " -(). –—"

	var b strings.Builder
	for _, r := range strings.TrimSpace(submitted) {
		if !strings.ContainsRune(separators, r) {
			b.WriteRune(r)
		}
	}
	digits := b.String()

	switch {
	case strings.HasPrefix(digits, "+"):
		return digits
	case strings.HasPrefix(digits, "0"):
		// 0412 345 678 -> +61412345678. The trunk prefix is a domestic dialling convention
		// and is not part of the international number.
		return "+61" + digits[1:]
	case strings.HasPrefix(digits, "61") && len(digits) > 2:
		// 61412345678, which is what a client that stripped the + sends.
		return "+" + digits
	default:
		// Nothing to infer, including the empty string. It is returned unchanged so the
		// validator refuses it and the provider is told what shape is wanted, rather than
		// being handed a guess.
		return digits
	}
}

// validE164 reports whether a normalised number is a well-formed international number.
//
// The shape is checked and the allocation is not: whether +61499999999 is answered by anybody is
// not something a validator can know, and 000600 says the same about the column.
func validE164(phone string) bool {
	if !strings.HasPrefix(phone, "+") {
		return false
	}
	digits := phone[1:]
	if len(digits) < 8 || len(digits) > 15 {
		return false
	}
	if digits[0] == '0' {
		// A country code never starts with zero, so this is an unrecognised trunk prefix —
		// usually a number from outside Australia typed in its domestic form.
		return false
	}
	for _, r := range digits {
		if !unicode.IsDigit(r) {
			return false
		}
	}
	return true
}
