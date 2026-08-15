// SHIP-162: what support writes down, and never shows anybody outside the console.
//
// Docs/01 §4.6's fifth administrative capability — "add internal support notes" — and the *Done
// when* is two claims: notes attach to a user or a job, and they are **never user-visible**.
//
// # How "never user-visible" is made true rather than intended
//
// Not by a flag on a note, and not by remembering to exclude it. By four things, none of which is a
// promise:
//
//   - **The rows are in a table of their own** (`000802`), which no user-facing endpoint reads and
//     none joins. `000800`'s header had already taken this position from the other side: every
//     column of `disputes` is read back to the complainant, so a note written into one of them
//     would be a note the complainant reads.
//   - **Both routes declare `RequireAdmin`**, a credential system a user token cannot reach at all
//     (SHIP-147) — not a permission check that could be forgotten, a different sign-in.
//   - **No shape in this package carries a note beside anything user-facing.** [Note] is returned
//     by one endpoint and appears in no other response, so there is no shape a client could be
//     handed that has one on it.
//   - **`scripts/verify/90-admin.sh` reads the job back as its customer** after a note is written
//     on it, and fails if the note's text appears anywhere in that response. That is the check that
//     survives somebody adding a field later, because it is an assertion about the *customer's*
//     endpoint rather than about this one.
//
// # Reading is gated on the subject's read permission, not on a note permission
//
// permissions.go records the decision this is built on: `notes.write` exists and there is no
// `notes.read`, because "notes are never user-visible, so there is no corresponding read permission
// for anyone outside the console". Everyone in the console may read them; what the permission
// distinguishes is who may add one.
//
// So a note on a user needs `users.read` and a note on a job needs `jobs.read` — both held by every
// role, including `support`, which is the role that most needs to read them and which does **not**
// hold `notes.write`. That asymmetry is the right way round: a support administrator reads the
// history and a moderator adds to it.
//
// The alternative was a new `notes.read` permission. It was rejected because it would be a
// permission every role holds, gating a read of a table nobody outside the console can reach —
// which is a line in the catalogue that never says no to anybody, and permissions.go is explicit
// that each one is a decision somebody had to make.
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

	"github.com/DulsaraNethmin/Shipper/services/core/internal/db"
)

// NoteSubject is what a note is about, and it is `ck_admin_notes_subject_type`s two values.
//
// Paired with the constraint by TestNoteSubjectConstraintMatchesTheGoConstants in `migrations`, per
// Docs/10 §3.4: a kind the database accepts and Go has no constant for is a note nothing can write,
// and one Go knows and the database refuses is a write that fails at run time.
type NoteSubject string

const (
	// NoteSubjectUser is a `users` row — a customer or a provider.
	//
	// **Not an administrator.** A note about a colleague is a different thing with different
	// handling, and `ck_admin_notes_subject_type` does not accept one; the record of what an
	// administrator did is `audit_log`, which nothing can rewrite.
	NoteSubjectUser NoteSubject = "user"

	// NoteSubjectJob is a `jobs` row.
	NoteSubjectJob NoteSubject = "job"
)

// NoteSubjects is every subject kind, in the order the constraint lists them.
var NoteSubjects = []NoteSubject{NoteSubjectUser, NoteSubjectJob}

// Valid reports whether s is one of [NoteSubjects].
func (s NoteSubject) Valid() bool { return slices.Contains(NoteSubjects, s) }

// String is the stored form, which is also the wire form.
func (s NoteSubject) String() string { return string(s) }

// noteSubjectNames is [NoteSubjects] as strings, for a validation message.
func noteSubjectNames() []string {
	out := make([]string, 0, len(NoteSubjects))
	for _, s := range NoteSubjects {
		out = append(out, s.String())
	}
	return out
}

// Note body bounds, matching `ck_admin_notes_body`.
//
// The floor is one character after trimming, which the constraint expresses as a btrim compared
// against the empty string — written out in words rather than quoted, because gofmt rewrites a pair
// of adjacent single quotes in a doc comment into a typographic one and puts the file on `gofmt -l`
// permanently. postgres_users.go records the same trap.
//
// A note of four thousand spaces records nothing, and this is a table whose whole value is this
// column.
//
// The ceiling is generous compared with [maxReasonLength], and deliberately: a reason is a sentence
// justifying an action and a note is prose about a situation. The two are different fields with
// different jobs, and giving them one bound would have made the smaller one wrong.
const maxNoteLength = 4000

// AddNoteCommand is one support note.
type AddNoteCommand struct {
	// Subject is what the note is about.
	Subject NoteSubject

	// SubjectID is which one. **Not checked against the subject's table**, and see the
	// [Notes.Add] note on why.
	SubjectID uuid.UUID

	// AuthorID is the administrator writing it, taken from the grant rather than from the
	// request. A body that named its own author would be a support history a client writes.
	AuthorID uuid.UUID

	// Body is the note.
	Body string
}

// Note is one support note as the console reads it.
//
// **This shape appears in exactly one response and will not appear in another.** See the file
// header: the way "never user-visible" is kept true is that there is no user-facing shape with a
// note on it, and adding one would be a deliberate act rather than an oversight.
type Note struct {
	ID uuid.UUID

	Subject   NoteSubject
	SubjectID uuid.UUID

	// AuthorID is the administrator who wrote it. Reported as an identifier rather than a name:
	// a name is a second read of `admin_users`, and a console that lists notes already knows its
	// own administrators. Resolving it is a change to this shape whenever somebody needs it.
	AuthorID uuid.UUID

	Body string

	CreatedAt time.Time
}

// Notes is the internal support history (SHIP-162).
type Notes struct {
	auditor *Auditor
	pool    *pgxpool.Pool
	store   postgresStore
}

// NewNotes builds the service.
//
// The pool may be nil, which every constructor in this package accepts. The auditor may not: adding
// a note is a privileged action and SHIP-150's rule is that every one of them writes an entry — see
// [Notes.Add] for what the entry names and why.
func NewNotes(auditor *Auditor, pool *pgxpool.Pool) (*Notes, error) {
	if auditor == nil {
		return nil, errors.New("admin: the notes service needs the audit writer; adding a note " +
			"is a privileged action and one that leaves no record cannot be reconstructed")
	}
	return &Notes{auditor: auditor, pool: pool}, nil
}

// Add writes one note, with its audit entry, in one transaction.
//
// # The subject is not checked against its table, and that is a decision
//
// A note may name a job or an account that does not exist, and nothing here refuses it. Three
// reasons, and the first is the one that decides it:
//
//   - **A note outlives its subject** (Docs/05 §3.1, and `000802`'s header). Support writing up why
//     an account was closed, after it was closed, is the case the table most exists for; an
//     existence check would refuse exactly that note.
//   - A check would be a read of `users` or `jobs` chosen by a value in the request, which is the
//     shape that becomes an existence oracle the moment somebody serves it to a wider audience.
//     The audience is an administrator today and the shape should not depend on that staying true.
//   - It would be a check against a row that can be deleted a second later, so it buys a guarantee
//     that does not hold.
//
// What it costs is a note attached to a typo, which is visible the moment somebody opens the
// subject and finds nothing. That is a better failure than a refused note.
//
// # The audit entry names the subject, not the note
//
// `target_type` and `target_id` are the *thing the note is about*, so that a search for "everything
// that happened to this account" (SHIP-165) returns the notes taken about it alongside the standing
// changes. A trail that named the note instead would answer "a note was added" to a query nobody
// runs, and would leave the account's own history with a gap where support's attention was.
//
// The note's identifier goes in the metadata, so an entry can still be traced to the row it caused.
//
// **The body does not.** An audit entry is append-only and a note is not (`000802`'s header records
// why the two differ), so copying the body into `audit_log` would create an uncorrectable copy of a
// correctable record — and would put free-form prose about a person into the one table the platform
// promises never to rewrite.
func (n *Notes) Add(ctx context.Context, cmd AddNoteCommand) (Note, error) {
	if !cmd.Subject.Valid() {
		return Note{}, fmt.Errorf("%w: %q", ErrNoteSubjectUnrecognised, cmd.Subject)
	}
	if cmd.SubjectID == uuid.Nil {
		return Note{}, ErrNoteSubjectMissing
	}
	if cmd.AuthorID == uuid.Nil {
		return Note{}, errors.New("admin: a note must name the administrator who wrote it")
	}

	body := strings.TrimSpace(cmd.Body)
	switch {
	case body == "":
		return Note{}, ErrNoteEmpty
	case len([]rune(body)) > maxNoteLength:
		return Note{}, fmt.Errorf("%w: at most %d characters", ErrNoteTooLong, maxNoteLength)
	}

	if n.pool == nil {
		return Note{}, ErrAdminUnavailable
	}

	id, err := uuid.NewV7()
	if err != nil {
		return Note{}, fmt.Errorf("admin: generating a note id: %w", err)
	}

	// The instant is the auditor's clock, so the note and the entry describing it agree —
	// Docs/11 §9's one row, one clock, applied across the two rows one action writes.
	note := Note{
		ID:        id,
		Subject:   cmd.Subject,
		SubjectID: cmd.SubjectID,
		AuthorID:  cmd.AuthorID,
		Body:      body,
		CreatedAt: n.auditor.clock.Now().UTC(),
	}

	if err := db.InTx(ctx, n.pool, func(ctx context.Context, tx db.Runner) error {
		if err := n.store.insertNote(ctx, tx, note); err != nil {
			return err
		}

		// Returned, never swallowed. See enforcement.go's header and
		// TestAPrivilegedActionIsRefusedWhenItsAuditEntryCannotBeWritten.
		if _, err := n.auditor.Record(ctx, tx, AuditEntry{
			Actor:      AdminActor(cmd.AuthorID),
			Action:     AuditActionNoteAdded,
			TargetType: noteTargetType(cmd.Subject),
			TargetID:   cmd.SubjectID,
			Metadata:   map[string]any{"note_id": id.String()},
		}); err != nil {
			return err
		}
		return nil
	}); err != nil {
		return Note{}, err
	}
	return note, nil
}

// For is every note on one subject, newest first.
//
// **Not paged**, and `000802`'s index records the same decision from the schema side: the notes on
// one account or one job are tens at most, a console shows them all, and a cursor would need the
// identifier as a tie-break and a wider index to support it. Whoever pages it does both.
func (n *Notes) For(ctx context.Context, subject NoteSubject, subjectID uuid.UUID) ([]Note, error) {
	if !subject.Valid() {
		return nil, fmt.Errorf("%w: %q", ErrNoteSubjectUnrecognised, subject)
	}
	if subjectID == uuid.Nil {
		return nil, ErrNoteSubjectMissing
	}
	if n.pool == nil {
		return nil, ErrAdminUnavailable
	}

	// A read, opening no transaction: one statement is already consistent with itself.
	notes, err := n.store.notesFor(ctx, n.pool, subject, subjectID)
	if err != nil {
		return nil, fmt.Errorf("admin: reading the notes on %s %s: %w", subject, subjectID, err)
	}
	return notes, nil
}

// noteTargetType maps a note's subject onto the audit trail's target vocabulary.
//
// A switch rather than a cast, even though the strings are identical today. They are two closed
// lists — [NoteSubjects] has two values and the audit target kinds have three — and a cast would
// compile for ever after either changed. This is the one place the correspondence is asserted, and
// the default is the honest answer for a value neither list should produce.
func noteTargetType(s NoteSubject) string {
	switch s {
	case NoteSubjectUser:
		return AuditTargetUser
	case NoteSubjectJob:
		return AuditTargetJob
	default:
		return string(s)
	}
}

// ReadPermissionFor is the permission a caller needs to read this subject's notes.
//
// The subject's own read permission rather than a note permission — see the file header. Both are
// held by every role, `support` included, which is the role that most needs to read a support
// history and which deliberately does not hold [PermissionNotesWrite].
func ReadPermissionFor(s NoteSubject) (Permission, bool) {
	switch s {
	case NoteSubjectUser:
		return PermissionUsersRead, true
	case NoteSubjectJob:
		return PermissionJobsRead, true
	default:
		return "", false
	}
}
