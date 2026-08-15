package admin

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/DulsaraNethmin/Shipper/services/core/internal/httpx"
)

// SHIP-162 against a real PostgreSQL.
//
// The *Done when* is two claims, and they are checked in two different places for a reason worth
// stating. **"Notes attach to a user or a job"** is this file: the write, the read, the permission
// split, the closed subject list and the audit entry.
//
// **"And are never user-visible"** cannot be established here. Every test in this package drives an
// administrative handler, so a note not appearing in an administrative response would prove nothing,
// and the interesting claim is about *other domains'* endpoints. What this file can establish is
// the structural half — the shape is returned by these two handlers and there is nowhere else in
// this package it appears — and `scripts/verify/90-admin.sh` makes the behavioural one, by reading a
// job back as its customer and failing if the note's text is anywhere in the response.

// newNotesFixture is the enforcement fixture under another name.
//
// These endpoints need a moderator with a live session, a real database and the handler — which is
// exactly what [newEnforcementFixture] already assembles, and a second fixture differing only in
// which service it happens to exercise is a second place for the same wiring to drift.
func newNotesFixture(t *testing.T) enforcementFixture {
	t.Helper()
	return newEnforcementFixture(t, RoleModerator)
}

// addNote drives POST /v1/admin/notes through the guard and the handler.
func addNote(t *testing.T, f enforcementFixture, token, subjectType, subjectID, body string) (int, string) {
	t.Helper()

	payload, err := json.Marshal(addNoteRequest{
		SubjectType: subjectType, SubjectID: subjectID, Body: body,
	})
	if err != nil {
		t.Fatalf("encoding the request: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/v1/admin/notes", strings.NewReader(string(payload)))
	req.Header.Set(httpx.HeaderAuthorization, "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")

	rec := httptest.NewRecorder()
	RequireAdmin(f.auth)(f.handler.AddNote()).ServeHTTP(rec, req)
	return rec.Code, rec.Body.String()
}

// readNotes drives GET /v1/admin/notes through the guard and the handler.
func readNotes(t *testing.T, f enforcementFixture, token, subjectType, subjectID string) (int, string) {
	t.Helper()

	query := url.Values{"subject_type": {subjectType}, "subject_id": {subjectID}}
	req := httptest.NewRequest(http.MethodGet, "/v1/admin/notes?"+query.Encode(), nil)
	req.Header.Set(httpx.HeaderAuthorization, "Bearer "+token)

	rec := httptest.NewRecorder()
	RequireAdmin(f.auth)(f.handler.ReadNotes()).ServeHTTP(rec, req)
	return rec.Code, rec.Body.String()
}

func notesFrom(t *testing.T, body string) []noteResponse {
	t.Helper()

	var list noteListResponse
	if err := json.Unmarshal([]byte(body), &list); err != nil {
		t.Fatalf("decoding the notes: %v (%s)", err, body)
	}
	return list.Data
}

// TestANoteAttachesToAUserAndToAJob is the first half of the *Done when*.
//
// Both subject kinds, because a polymorphic subject with only one kind exercised is a column that
// happens to hold one value — and the second kind is where a `subject_type` dropped from the
// predicate would show up.
func TestANoteAttachesToAUserAndToAJob(t *testing.T) {
	f := newNotesFixture(t)

	userID := newAccount(t, f.pool, "noted-user@example.com", "+61400162a", "customer")
	jobID, _ := f.openJob(t, "162a")

	const userNote = "Rang about the damaged crates; sending photographs this afternoon."
	const jobNote = "Goods description edited twice before publication; watch for a re-list."

	status, body := addNote(t, f, f.token, "user", userID.String(), userNote)
	if status != http.StatusCreated {
		t.Fatalf("adding a note on a user: status = %d, want 201 (%s)", status, body)
	}

	var created noteResponse
	if err := json.Unmarshal([]byte(body), &created); err != nil {
		t.Fatalf("decoding the note: %v (%s)", err, body)
	}
	if created.SubjectType != "user" || created.SubjectID != userID.String() {
		t.Errorf("the note is attached to %s %s, want user %s",
			created.SubjectType, created.SubjectID, userID)
	}
	if created.AuthorID != f.admin.ID.String() {
		t.Errorf("author_id = %s, want the administrator who wrote it, %s",
			created.AuthorID, f.admin.ID)
	}

	if status, body := addNote(t, f, f.token, "job", jobID.String(), jobNote); status != http.StatusCreated {
		t.Fatalf("adding a note on a job: status = %d, want 201 (%s)", status, body)
	}

	// Each subject sees its own note and only its own. The negative direction is what a query
	// that dropped `subject_type` from its predicate would fail.
	status, body = readNotes(t, f, f.token, "user", userID.String())
	if status != http.StatusOK {
		t.Fatalf("reading a user's notes: status = %d, want 200 (%s)", status, body)
	}
	notes := notesFrom(t, body)
	if len(notes) != 1 || notes[0].Body != userNote {
		t.Fatalf("the user's notes are %v, want exactly the one written about them", notes)
	}

	status, body = readNotes(t, f, f.token, "job", jobID.String())
	if status != http.StatusOK {
		t.Fatalf("reading a job's notes: status = %d, want 200 (%s)", status, body)
	}
	notes = notesFrom(t, body)
	if len(notes) != 1 || notes[0].Body != jobNote {
		t.Fatalf("the job's notes are %v, want exactly the one written about it", notes)
	}
}

// TestNotesComeBackNewestFirst.
//
// A support history is read from the top: what somebody needs is what was last learned, and a
// console that showed the oldest note first would put the least useful line where the eye lands.
func TestNotesComeBackNewestFirst(t *testing.T) {
	f := newNotesFixture(t)
	userID := newAccount(t, f.pool, "ordered@example.com", "+61400162b", "customer")

	for _, body := range []string{"First contact, no answer.", "Second call, left a message.", "Reached them; sending a form."} {
		if status, resp := addNote(t, f, f.token, "user", userID.String(), body); status != http.StatusCreated {
			t.Fatalf("adding %q: status = %d (%s)", body, status, resp)
		}
		// The fixture clock is fixed, so the instants would collide. Advanced between notes so
		// the ordering has something to order by — which is honest about the limit `000802`
		// records: this read is not paged and its ordering is not total.
		f.clk.Advance(time.Minute)
	}

	status, body := readNotes(t, f, f.token, "user", userID.String())
	if status != http.StatusOK {
		t.Fatalf("status = %d, want 200 (%s)", status, body)
	}

	notes := notesFrom(t, body)
	if len(notes) != 3 {
		t.Fatalf("got %d notes, want 3", len(notes))
	}
	if notes[0].Body != "Reached them; sending a form." {
		t.Errorf("the first note is %q, want the most recent one", notes[0].Body)
	}
	if notes[2].Body != "First contact, no answer." {
		t.Errorf("the last note is %q, want the oldest one", notes[2].Body)
	}
}

// TestASubjectWithNoNotesIsAnEmptyListRatherThanA404.
//
// The subject is deliberately not looked up ([Notes.Add] records why), so "no notes" and "no such
// subject" are the same answer here. An empty **array** rather than `null`, because a console that
// iterates without checking crashes on the ordinary case rather than the rare one.
func TestASubjectWithNoNotesIsAnEmptyListRatherThanA404(t *testing.T) {
	f := newNotesFixture(t)

	status, body := readNotes(t, f, f.token, "user", uuid.New().String())
	if status != http.StatusOK {
		t.Fatalf("status = %d, want 200 (%s)", status, body)
	}
	if !strings.Contains(body, `"data":[]`) {
		t.Errorf("body = %s, want an empty array — null crashes a console that iterates", body)
	}
}

// TestANoteMayNameASubjectThatDoesNotExist.
//
// **The case the table most exists for.** Docs/05 §3.1 keeps records beyond the account, and support
// writing up why an account was closed *after* it was closed is exactly the note an existence check
// would refuse. Written down as a test so that somebody adding the check later has to delete an
// assertion that says why not.
func TestANoteMayNameASubjectThatDoesNotExist(t *testing.T) {
	f := newNotesFixture(t)

	gone := uuid.New()
	status, body := addNote(t, f, f.token, "user", gone.String(),
		"Account closed at their request last week; keeping this for the retention window.")
	if status != http.StatusCreated {
		t.Fatalf("status = %d, want 201 (%s) — a note outlives its subject, so the subject is "+
			"deliberately not checked against its table", status, body)
	}

	_, read := readNotes(t, f, f.token, "user", gone.String())
	if len(notesFrom(t, read)) != 1 {
		t.Errorf("the note about a subject that does not exist cannot be read back: %s", read)
	}
}

// TestSupportReadsNotesAndCannotWriteThem is the permission split, in both directions.
//
// `support` holds `users.read` and `jobs.read` and does **not** hold `notes.write`. That is the
// right way round and is the reason reading is gated on the subject's permission rather than on a
// note permission — see notes.go.
func TestSupportReadsNotesAndCannotWriteThem(t *testing.T) {
	f := newNotesFixture(t)
	userID := newAccount(t, f.pool, "perm-notes@example.com", "+61400162c", "customer")

	const note = "Confirmed the delivery address by phone; no further action."
	if status, body := addNote(t, f, f.token, "user", userID.String(), note); status != http.StatusCreated {
		t.Fatalf("the moderator could not add a note: %d (%s)", status, body)
	}

	// A support administrator, signed in on the same fixture's credentials service.
	anAdministrator(t, f.creds, "support-notes@example.com", RoleSupport)
	issued, _, err := signIn(t, f.creds, "support-notes@example.com", testPassword, "10.0.91.1")
	if err != nil {
		t.Fatalf("signing the support administrator in: %v", err)
	}

	status, body := readNotes(t, f, issued.Token, "user", userID.String())
	if status != http.StatusOK {
		t.Fatalf("a support administrator could not read notes: %d (%s)", status, body)
	}
	if notes := notesFrom(t, body); len(notes) != 1 || notes[0].Body != note {
		t.Errorf("the support administrator sees %v, want the note that was written", notes)
	}

	status, body = addNote(t, f, issued.Token, "user", userID.String(),
		"Adding my own note about this account.")
	if status != http.StatusForbidden {
		t.Fatalf("a support administrator added a note and got %d, want 403 (%s)", status, body)
	}
	if strings.Contains(body, PermissionNotesWrite.String()) {
		t.Errorf("the refusal names the permission it wanted: %s", body)
	}

	// And nothing was written, which a status code alone does not establish.
	_, after := readNotes(t, f, f.token, "user", userID.String())
	if len(notesFrom(t, after)) != 1 {
		t.Errorf("a refused note was written anyway: %s", after)
	}
}

// TestEveryBadNoteRequestIsRefusedAndNamesItsField.
//
// A subject kind outside the closed list, a missing or malformed identifier, and a body that records
// nothing. All 422 in validate.Errors' shape, because all are fields somebody typed.
func TestEveryBadNoteRequestIsRefusedAndNamesItsField(t *testing.T) {
	f := newNotesFixture(t)
	subject := uuid.New().String()

	for _, tc := range []struct{ name, subjectType, subjectID, body, field string }{
		{"a subject kind the platform does not have", "administrator", subject, "About a colleague.", "subject_type"},
		{"no subject kind at all", "", subject, "About something.", "subject_type"},
		{"a subject that is not an identifier", "user", "somebody", "About them.", "subject_id"},
		{"no subject at all", "user", "", "About them.", "subject_id"},
		{"an empty note", "user", subject, "", "body"},
		{"a note of nothing but spaces", "user", subject, "      ", "body"},
		{"a note longer than the column holds", "user", subject, strings.Repeat("x", maxNoteLength+1), "body"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			status, body := addNote(t, f, f.token, tc.subjectType, tc.subjectID, tc.body)
			if status != http.StatusUnprocessableEntity {
				t.Fatalf("status = %d, want 422 (%s)", status, body)
			}
			if !strings.Contains(body, `"`+tc.field+`"`) {
				t.Errorf("the refusal does not name %q: %s", tc.field, body)
			}
		})
	}
}

// TestAMalformedSubjectIsRefusedOnTheReadToo.
//
// The write and the read validate through one function, because a body that accepted a kind the
// query refused would write notes nothing could read.
func TestAMalformedSubjectIsRefusedOnTheReadToo(t *testing.T) {
	f := newNotesFixture(t)

	for _, tc := range []struct{ name, subjectType, subjectID, field string }{
		{"a subject kind the platform does not have", "administrator", uuid.New().String(), "subject_type"},
		{"a subject that is not an identifier", "user", "somebody", "subject_id"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			status, body := readNotes(t, f, f.token, tc.subjectType, tc.subjectID)
			if status != http.StatusUnprocessableEntity {
				t.Fatalf("status = %d, want 422 (%s)", status, body)
			}
			if !strings.Contains(body, `"`+tc.field+`"`) {
				t.Errorf("the refusal does not name %q: %s", tc.field, body)
			}
		})
	}
}

// TestANoteIsRefusedWithoutACredential.
//
// Both operations. Reachable without one, an internal support history would be a public file on
// every customer the platform has had a problem with.
func TestANoteIsRefusedWithoutACredential(t *testing.T) {
	f := newNotesFixture(t)
	userID := newAccount(t, f.pool, "nocred-notes@example.com", "+61400162d", "customer")

	const note = "Escalated to the insurer; do not discuss the claim with the customer."
	if status, body := addNote(t, f, f.token, "user", userID.String(), note); status != http.StatusCreated {
		t.Fatalf("adding the note: %d (%s)", status, body)
	}

	if status, body := readNotes(t, f, "", "user", userID.String()); status != http.StatusUnauthorized {
		t.Errorf("reading notes with no credential: status = %d, want 401 (%s)", status, body)
	} else if strings.Contains(body, "insurer") {
		t.Errorf("a refused read returned the note anyway: %s", body)
	}

	if status, _ := addNote(t, f, "", "user", userID.String(), "Anonymous."); status != http.StatusUnauthorized {
		t.Errorf("adding a note with no credential: status = %d, want 401", status)
	}
}

// TestTheNoteShapeIsAClosedSetOfKeys.
//
// The same discipline the job and account shapes are held to (SHIP-83's axis). Here it guards a
// different risk: a note is prose about a person, and a field added to this shape carrying anything
// *from* the subject — an address, a phone number — would put Docs/01 §5.1's minimised data into a
// record that outlives the account.
func TestTheNoteShapeIsAClosedSetOfKeys(t *testing.T) {
	f := newNotesFixture(t)
	userID := newAccount(t, f.pool, "shape-notes@example.com", "+61400162e", "customer")

	status, body := addNote(t, f, f.token, "user", userID.String(),
		"Confirmed by phone; nothing outstanding.")
	if status != http.StatusCreated {
		t.Fatalf("status = %d, want 201 (%s)", status, body)
	}

	var got map[string]any
	if err := json.Unmarshal([]byte(body), &got); err != nil {
		t.Fatalf("decoding the note: %v (%s)", err, body)
	}
	assertKeys(t, "a note", got,
		"id", "subject_type", "subject_id", "author_id", "body", "created_at")
}

// TestTheNotesServiceRefusesToBeBuiltWithoutAnAuditWriter.
//
// Adding a note is a privileged action, and SHIP-150's rule is that every one of them writes an
// entry. A notes service with no writer would append to the support history leaving no record of
// who had been reading the account.
func TestTheNotesServiceRefusesToBeBuiltWithoutAnAuditWriter(t *testing.T) {
	if _, err := NewNotes(nil, nil); err == nil {
		t.Error("a notes service was built with no audit writer behind it")
	}
}

// TestEveryNoteSubjectHasAReadPermission.
//
// [ReadNotes] chooses the permission it checks from a value in the request, which is only safe while
// every subject kind maps to one. A kind added to [NoteSubjects] and not to [ReadPermissionFor]
// would reach a handler branch that refuses — written as a test so it fails here rather than as a
// 500 on the first console that asks for it.
func TestEveryNoteSubjectHasAReadPermission(t *testing.T) {
	for _, s := range NoteSubjects {
		permission, ok := ReadPermissionFor(s)
		if !ok {
			t.Errorf("%q is a note subject with no read permission; ReadNotes cannot serve it", s)
			continue
		}
		if !permission.Valid() {
			t.Errorf("%q maps to %q, which is not in the permission catalogue", s, permission)
		}
	}
}
