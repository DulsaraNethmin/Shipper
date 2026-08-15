package migrations_test

import (
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/DulsaraNethmin/Shipper/services/core/internal/admin"
	"github.com/DulsaraNethmin/Shipper/services/core/internal/testsupport/pgtest"
)

// SHIP-162's schema, and the pairing Docs/10 §3.4 asks of every closed list.
//
// `admin_notes` is the table SHIP-162 adds. What is worth a test rather than a comment is the same
// thing `ck_users_status` was: a closed vocabulary written down in two places that cannot import
// each other's definition of it.

// TestNoteSubjectConstraintMatchesTheGoConstants.
//
// The failure it catches is quiet in both directions. A subject kind Go can name and the column
// refuses is a note that fails at run time, in the one moment somebody is trying to write down what
// they have just learned. A kind the column accepts and Go cannot name is a note nothing can write
// and — worse — one that a hand-written row could put in the table where the read would return it
// with a `subject_type` no client has a branch for.
func TestNoteSubjectConstraintMatchesTheGoConstants(t *testing.T) {
	pool := pgtest.DB(t)

	inDatabase := constraintLiterals(t, pool, "ck_admin_notes_subject_type")

	for _, subject := range admin.NoteSubjects {
		if !inDatabase[subject.String()] {
			t.Errorf("admin.NoteSubjects has %q and ck_admin_notes_subject_type refuses it, so a "+
				"note about one fails at the moment somebody writes it", subject)
		}
		delete(inDatabase, subject.String())
	}
	for leftover := range inDatabase {
		t.Errorf("ck_admin_notes_subject_type accepts %q and admin.NoteSubjects has no such "+
			"subject, so nothing in this service can write or read a note about one", leftover)
	}
}

// TestANoteMustRecordSomething is `ck_admin_notes_body`, from the side the Go check cannot cover.
//
// The service trims and refuses an empty body before it gets here. This is the half that holds for a
// connection which never went through the service at all — an operator at a `psql` prompt, a repair
// script — which is what makes it a constraint rather than a convention.
func TestANoteMustRecordSomething(t *testing.T) {
	pool := pgtest.DB(t)

	author := anAdminUser(t, pool, "note-constraint@example.com")

	for _, tc := range []struct{ name, body string }{
		{"empty", ""},
		{"nothing but spaces", "     "},
		{"nothing but a newline", "\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := pool.Exec(t.Context(), `
				INSERT INTO admin_notes (id, subject_type, subject_id, author_id, body)
				VALUES ($1, 'user', $2, $3, $4)`,
				uuid.Must(uuid.NewV7()), uuid.New(), author, tc.body)
			if err == nil {
				t.Errorf("a note with %s in it was accepted; this table's whole value is what "+
					"is in that column", tc.name)
			}
		})
	}
}

// TestANoteSurvivesTheSubjectItIsAbout.
//
// `subject_id` is deliberately not a foreign key — `000802`'s header has the reasoning, and Docs/05
// §3.1 is the document behind it. **The case the table most exists for** is the note explaining why
// an account was closed, which has to be writable after it was, and readable afterwards.
//
// Asserted as a property of the schema rather than of the service, because it is the schema that
// would have made it impossible: a foreign key with ON DELETE RESTRICT would have made the note
// block the deletion, and CASCADE would have deleted the record of why.
func TestANoteSurvivesTheSubjectItIsAbout(t *testing.T) {
	pool := pgtest.DB(t)

	author := anAdminUser(t, pool, "note-orphan@example.com")
	gone := uuid.New()

	if _, err := pool.Exec(t.Context(), `
		INSERT INTO admin_notes (id, subject_type, subject_id, author_id, body)
		VALUES ($1, 'user', $2, $3, 'Account closed at their request; keeping this for retention.')`,
		uuid.Must(uuid.NewV7()), gone, author); err != nil {
		t.Fatalf("a note about an account that does not exist was refused: %v", err)
	}

	var found int
	if err := pool.QueryRow(t.Context(),
		`SELECT count(*) FROM admin_notes WHERE subject_id = $1`, gone).Scan(&found); err != nil {
		t.Fatalf("reading the note back: %v", err)
	}
	if found != 1 {
		t.Errorf("the note about a subject that does not exist is not there")
	}
}

// TestANoteMustNameAnAuthorThatExists is the other half of the same decision.
//
// Unlike the subject, the author is **always** an administrator of this platform and must always be
// nameable: a support note whose author cannot be identified is a note nobody can weigh. So this one
// *is* a foreign key, and the two columns differing is the decision `000802` records.
func TestANoteMustNameAnAuthorThatExists(t *testing.T) {
	pool := pgtest.DB(t)

	_, err := pool.Exec(t.Context(), `
		INSERT INTO admin_notes (id, subject_type, subject_id, author_id, body)
		VALUES ($1, 'user', $2, $3, 'Written by nobody in particular.')`,
		uuid.Must(uuid.NewV7()), uuid.New(), uuid.New())
	if err == nil {
		t.Error("a note was accepted naming an author who is not an administrator of this platform")
	}
}

// anAdminUser inserts an administrator to author a note.
//
// The bootstrap path `000801`'s header describes — one INSERT by an operator — because these tests
// are about the schema rather than about the service that usually writes it.
func anAdminUser(t *testing.T, pool *pgxpool.Pool, email string) uuid.UUID {
	t.Helper()

	// `ck_admin_users_password_hash` requires a PHC string, so the placeholder every other
	// fixture in this package uses would be refused here. A development value, not a secret —
	// nothing signs in as this account.
	const hash = `$argon2id$v=19$m=8192,t=1,p=1$aV5lLp4R4eGpoUzF2u+TDw$` +
		`F2Vdnh8pmtZWQShAmx1p0L2X4qWMvmSFdnqSNhOJWZA`

	id := uuid.Must(uuid.NewV7())
	if _, err := pool.Exec(t.Context(), `
		INSERT INTO admin_users (id, email, name, password_hash, role)
		VALUES ($1, $2, 'Fixture', $3, 'moderator')`, id, email, hash); err != nil {
		t.Fatalf("inserting an administrator: %v", err)
	}
	return id
}
