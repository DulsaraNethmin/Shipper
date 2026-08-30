package admin_test

import (
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/DulsaraNethmin/Shipper/services/core/internal/admin"
)

// The parts of SHIP-155a that need no database.
//
// The database side of these constants is checked in migrations/reports_test.go, which reads
// ck_reports_reason and ck_reports_subject_type out of pg_constraint and holds them to the Go lists
// in both directions (Docs/10 §3.4). What that pairing cannot see is a constant declared here and
// left out of the slice: the slice would agree with the database, and Valid would refuse a value the
// package itself defines.

func TestEveryReasonConstantIsInTheList(t *testing.T) {
	declared := []admin.Reason{
		admin.ReasonProhibitedGoods,
		admin.ReasonMisleading,
		admin.ReasonOffPlatform,
		admin.ReasonAbusive,
		admin.ReasonFraud,
		admin.ReasonSafety,
		admin.ReasonOther,
	}
	if len(declared) != len(admin.Reasons) {
		t.Errorf("%d constants and %d in admin.Reasons", len(declared), len(admin.Reasons))
	}
	for _, r := range declared {
		if !r.Valid() {
			t.Errorf("%q is a declared reason that admin.Reasons omits, so Valid refuses a "+
				"value this package defines", r)
		}
	}

	declaredSubjects := []admin.ReportSubject{admin.ReportSubjectJob, admin.ReportSubjectMessage}
	if len(declaredSubjects) != len(admin.ReportSubjects) {
		t.Errorf("%d subject constants and %d in admin.ReportSubjects",
			len(declaredSubjects), len(admin.ReportSubjects))
	}
	for _, s := range declaredSubjects {
		if !s.Valid() {
			t.Errorf("%q is a declared subject that admin.ReportSubjects omits", s)
		}
	}
}

// TestWhatIsNotAReportableSubject names the things that look like they belong here and do not.
//
// Docs/04 §5's second queue is "reported jobs or messages" — two subjects, one queue. A profile, a
// bid and a user are each a plausible third and none of them is this ticket: adding one is a value
// in `ck_reports_subject_type` and a row in the backlog, not a string a client can send.
func TestWhatIsNotAReportableSubject(t *testing.T) {
	for _, notASubject := range []admin.ReportSubject{"profile", "user", "bid", "Job", "MESSAGE", ""} {
		if notASubject.Valid() {
			t.Errorf("%q is accepted as a report subject; Docs/04 §5's second queue is jobs and "+
				"messages, and a third kind is a decision somebody records", notASubject)
		}
	}
}

// TestReasonWireFormsAreStableAndDistinct pins the seven strings a client sends and reads.
//
// [admin.Reason.Wire] is derived rather than tabulated, so it cannot disagree with the constants —
// which is the drift worth preventing and not the risk this test covers. **These strings are
// published.** A client already sending `prohibited_goods` cannot have it renamed underneath it, and
// writing all seven out is what makes a change to that function visible as a change to the contract
// rather than as a passing refactor.
//
// It is also what holds the one reworded value in place. Docs/03 writes "off-platform activity"; the
// stored form is "Dealing outside Shipper" because the derived wire form would otherwise contain a
// hyphen, which is not a legal identifier in a generated client — the same trap `000800` hit with a
// slash, and this test is where the decision is visible.
func TestReasonWireFormsAreStableAndDistinct(t *testing.T) {
	want := map[admin.Reason]string{
		admin.ReasonProhibitedGoods: "prohibited_goods",
		admin.ReasonMisleading:      "misleading_listing",
		admin.ReasonOffPlatform:     "dealing_outside_shipper",
		admin.ReasonAbusive:         "abusive_or_threatening",
		admin.ReasonFraud:           "suspected_fraud",
		admin.ReasonSafety:          "safety_concern",
		admin.ReasonOther:           "other",
	}

	if len(want) != len(admin.Reasons) {
		t.Fatalf("%d wire forms for %d reasons", len(want), len(admin.Reasons))
	}

	seen := map[string]admin.Reason{}
	for _, r := range admin.Reasons {
		got := r.Wire()
		if got != want[r] {
			t.Errorf("%q on the wire is %q, want %q", r, got, want[r])
		}
		if first, clash := seen[got]; clash {
			t.Errorf("%q and %q are both %q on the wire", first, r, got)
		}
		seen[got] = r

		// Every published form has to be legal in three client languages. The hyphen is the
		// case that caught this list, as the slash caught the dispute one.
		if strings.ContainsAny(got, "/ .-") {
			t.Errorf("%q is not a legal enum value in a generated client", got)
		}

		back, known := admin.ReasonFromWire(got)
		if !known || back != r {
			t.Errorf("ReasonFromWire(%q) = %q, %v; want %q back", got, back, known, r)
		}
	}
}

// TestTheStoredReasonIsNotAcceptedOnTheWire is the call [admin.CategoryFromWire] makes one table
// over.
//
// A client that sent "Safety concern" has misread the contract rather than typed the wrong case, and
// accepting both spellings would make two forms interchangeable in one direction and not the other.
func TestTheStoredReasonIsNotAcceptedOnTheWire(t *testing.T) {
	for _, r := range admin.Reasons {
		if _, known := admin.ReasonFromWire(string(r)); known && string(r) != r.Wire() {
			t.Errorf("the stored form %q was accepted as a wire value", r)
		}
	}
	for _, notAReason := range []string{
		"", "dangerous", "PROHIBITED_GOODS", "prohibited goods", "off-platform_dealing",
	} {
		if _, known := admin.ReasonFromWire(notAReason); known {
			t.Errorf("%q was accepted as a reason", notAReason)
		}
	}
}

// TestReasonsWireIsDerivedFromTheList stops a validation message naming a value the handler would
// then refuse.
func TestReasonsWireIsDerivedFromTheList(t *testing.T) {
	wire := admin.ReasonsWire()
	if len(wire) != len(admin.Reasons) {
		t.Fatalf("ReasonsWire() has %d entries for %d reasons", len(wire), len(admin.Reasons))
	}
	for i, r := range admin.Reasons {
		if wire[i] != r.Wire() {
			t.Errorf("ReasonsWire()[%d] = %q, want %q", i, wire[i], r.Wire())
		}
	}
}

// TestAReportPointsAtOneThing is [admin.Report.About], which is the Go reading of
// `ck_reports_subject`.
//
// SHIP-156 opens what this answers, so the two kinds have to resolve to different identifiers: a
// report about a message that answered with the job would open the conversation's *job* and lose
// which message was complained about.
func TestAReportPointsAtOneThing(t *testing.T) {
	job := uuid.MustParse("018f0000-0000-7000-8000-00000000000a")
	message := uuid.MustParse("018f0000-0000-7000-8000-00000000000b")

	aboutJob := admin.Report{JobID: job, Subject: admin.ReportSubjectJob}
	if got := aboutJob.About(); got != job {
		t.Errorf("a report about the job points at %s, want %s", got, job)
	}

	aboutMessage := admin.Report{JobID: job, Subject: admin.ReportSubjectMessage, MessageID: message}
	if got := aboutMessage.About(); got != message {
		t.Errorf("a report about a message points at %s, want the message %s", got, message)
	}
}
